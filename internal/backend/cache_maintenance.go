package backend

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/eyupio/zoomies/internal/cacheservice"
	"github.com/eyupio/zoomies/internal/store"
)

// CachePolicy contains only the settings needed for host maintenance. It never
// carries a runner's credentials, environment or image-pull authorisation.
type CachePolicy struct {
	PoolID     string            `json:"pool_id"`
	Backend    store.BackendKind `json:"backend"`
	Repository string            `json:"repository,omitempty"`
	Cache      store.CacheConfig `json:"cache"`
}

// CacheStorage is an apparent-size measurement, not a quota or a placement
// estimate. Deferred explains why an over-limit cache could not be pruned.
type CacheStorage struct {
	PoolID       string    `json:"pool_id"`
	PackageBytes int64     `json:"package_bytes"`
	ToolsBytes   int64     `json:"tools_bytes"`
	ServiceBytes int64     `json:"service_bytes"`
	TotalBytes   int64     `json:"total_bytes"`
	SizeLimit    int64     `json:"size_limit"`
	MeasuredAt   time.Time `json:"measured_at"`
	Deferred     string    `json:"deferred,omitempty"`
}

type cacheMaintainer interface {
	SetCachePolicies([]CachePolicy)
	MaintainCaches(context.Context)
}

func (r *Registry) SetCachePolicies(policies []CachePolicy) {
	for _, b := range r.backends {
		if m, ok := b.(cacheMaintainer); ok {
			m.SetCachePolicies(policies)
		}
	}
}

func (r *Registry) MaintainCaches(ctx context.Context) {
	for _, b := range r.backends {
		if ctx.Err() != nil {
			return
		}
		if m, ok := b.(cacheMaintainer); ok {
			m.MaintainCaches(ctx)
		}
	}
}

func (b *DockerBackend) SetCachePolicies(policies []CachePolicy) {
	b.cacheGate.Lock()
	defer b.cacheGate.Unlock()
	b.cachePolicies = nil
	for _, p := range policies {
		if p.Backend == b.Kind() && p.Cache.Enabled {
			b.cachePolicies = append(b.cachePolicies, p)
		}
	}
	if len(b.cachePolicies) == 0 {
		b.cacheStorage = nil
	}
	if b.cacheSvcs != nil {
		b.cacheSvcs.mu.Lock()
		b.cacheSvcs.policyLimits = make(map[string]int64, len(b.cachePolicies))
		for _, p := range policies {
			if p.Cache.Enabled {
				b.cacheSvcs.policyLimits[p.PoolID] = p.Cache.SizeLimit
			}
		}
		b.cacheSvcs.mu.Unlock()
	}
}

// A start can be between preparing a bind and creating its container. The
// inventory cannot see that interval, so maintenance must also hold this gate.
func (b *DockerBackend) beginCacheUse() func() {
	b.cacheGate.Lock()
	b.cacheBusy++
	b.cacheGate.Unlock()
	return func() { b.cacheGate.Lock(); b.cacheBusy--; b.cacheGate.Unlock() }
}

// MaintainCaches also runs when placement is disk-blocked and no new runner
// will arrive to trigger the old create-time prune.
func (b *DockerBackend) MaintainCaches(ctx context.Context) {
	unlock := b.lockCachePeers()
	defer unlock()
	if len(b.cachePolicies) == 0 {
		return
	}
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	var inventory []ContainerSummary
	var inventoryErr error
	busy := false
	for _, peer := range b.cacheBackendPeers() {
		rows, err := peer.api.ContainerList(ctx, nil)
		inventory = append(inventory, rows...)
		inventoryErr = errors.Join(inventoryErr, err)
		busy = busy || peer.cacheBusy > 0
	}
	var out []CacheStorage
	for _, p := range b.cachePolicies {
		if ctx.Err() != nil {
			break
		}
		st := CacheStorage{PoolID: p.PoolID, SizeLimit: p.Cache.SizeLimit}
		spec := Spec{PoolID: p.PoolID, Repository: p.Repository, Cache: p.Cache}
		identity, err := cacheIdentity(spec)
		if err != nil {
			st.Deferred = "Cache identity could not be resolved"
			out = append(out, st)
			continue
		}
		packageDir, measurable := cacheDirectory(spec)
		toolsDir, serviceDir := "", ""
		if b.sharedDir != "" {
			toolsDir = filepath.Join(b.sharedDir, "cache", "tools", identity)
			serviceDir = filepath.Join(b.sharedDir, "cache", "service", identity)
		}
		if packageDir != "" && (pathsOverlap(packageDir, toolsDir) || pathsOverlap(packageDir, serviceDir)) {
			st.Deferred = "Package cache overlaps tool or service storage; choose a separate cache source"
			out = append(out, st)
			continue
		}
		if b.sharedDir == "" && (p.Cache.Tools || p.Cache.Service) {
			st.Deferred = "Tool or service storage is unavailable; total size is unknown"
			out = append(out, st)
			continue
		}
		roots := []*os.Root{}
		var entries []cacheDiskEntry
		for _, part := range []struct{ path, kind string }{{packageDir, "package"}, {toolsDir, "tools"}} {
			if part.path == "" {
				continue
			}
			root, err := openCacheRoot(part.path)
			if errors.Is(err, fs.ErrNotExist) {
				continue
			}
			if err != nil {
				st.Deferred = "Cache directory could not be safely read"
				break
			}
			roots = append(roots, root)
			items, err := readCacheEntries(ctx, root, part.kind)
			if err != nil {
				st.Deferred = "Cache size could not be measured"
				break
			}
			entries = append(entries, items...)
		}
		for _, e := range entries {
			if e.kind == "package" {
				st.PackageBytes += e.size
			} else {
				st.ToolsBytes += e.size
			}
		}
		st.ServiceBytes, err = measuredCacheTree(ctx, serviceDir)
		if err != nil {
			st.Deferred = "Cache service storage could not be measured"
		}
		if st.Deferred == "" {
			st.MeasuredAt = time.Now().UTC()
			switch {
			case !measurable:
				st.Deferred = "Package cache is a daemon volume; its size is unknown"
			case inventoryErr != nil:
				st.Deferred = "Container inventory unavailable; cleanup deferred"
			case busy:
				st.Deferred = "A runner start or tool fill is in progress"
			default:
				for _, c := range inventory {
					if phase := phaseFromState(c.State); phase == PhaseExited || phase == PhaseFailed || phase == PhaseGone {
						continue
					}
					used := c.Labels[LabelPoolID] == p.PoolID || c.Labels[LabelCacheVolume] == packageDir
					for _, mount := range c.Mounts {
						for _, dir := range []string{packageDir, toolsDir} {
							if dir != "" && pathsOverlap(mount.Source, dir) {
								used = true
							}
						}
					}
					if used {
						st.Deferred = "A container is still using this pool's cache"
						break
					}
				}
			}
		}
		if st.Deferred == "" && st.SizeLimit > 0 {
			// The service evicts through its own index. Reserve its existing bytes
			// while evicting package/tool entries; then give it the remaining room.
			sort.Slice(entries, func(i, j int) bool {
				if entries[i].mtime == entries[j].mtime {
					return entries[i].kind+entries[i].name < entries[j].kind+entries[j].name
				}
				return entries[i].mtime < entries[j].mtime
			})
			total := st.PackageBytes + st.ToolsBytes + st.ServiceBytes
			for _, e := range entries {
				if total <= st.SizeLimit {
					break
				}
				if ctx.Err() != nil {
					st.Deferred = "Cache cleanup timed out"
					break
				}
				if err := e.root.RemoveAll(e.name); err != nil {
					st.Deferred = "A cache entry could not be removed"
					break
				}
				total -= e.size
				if e.kind == "package" {
					st.PackageBytes -= e.size
				} else {
					st.ToolsBytes -= e.size
				}
			}
		}
		// Even while jobs hold package/tools, their size reduces what the service
		// may retain. It supports live eviction; package files do not.
		if !st.MeasuredAt.IsZero() && measurable {
			limit := st.SizeLimit
			if limit <= 0 {
				limit = defaultCacheServiceBytes
			} else {
				limit = max(0, limit-st.PackageBytes-st.ToolsBytes)
			}
			if err := b.cacheServiceOwner(spec).applyStorageBudget(ctx, spec, serviceDir, limit); err != nil {
				st.Deferred = "Cache service budget could not be updated"
			} else if n, err := measuredCacheTree(ctx, serviceDir); err == nil {
				st.ServiceBytes = n
			} else {
				st.MeasuredAt = time.Time{}
				st.Deferred = "Cache service storage could not be measured"
			}
		}
		for _, root := range roots {
			_ = root.Close()
		}
		st.TotalBytes = st.PackageBytes + st.ToolsBytes + st.ServiceBytes
		if st.SizeLimit > 0 && st.TotalBytes > st.SizeLimit && st.Deferred == "" {
			st.Deferred = "Open cache transfers or metadata still exceed the limit"
		}
		out = append(out, st)
	}
	// Do not replace an earlier measurement with a false zero on cancellation.
	if ctx.Err() == nil {
		b.cacheStorage = out
	}
}

func pathsOverlap(a, b string) bool {
	if a == "" || b == "" {
		return false
	}
	a, b = filepath.Clean(a), filepath.Clean(b)
	return a == b || strings.HasPrefix(a, b+string(filepath.Separator)) || strings.HasPrefix(b, a+string(filepath.Separator))
}

type cacheDiskEntry struct {
	root        *os.Root
	name, kind  string
	size, mtime int64
}

// OpenRoot pins the directory during eviction and contains nested symlinks.
// Refuse a symlink at or above the root rather than following it into data
// that the pool's configured path does not own.
func openCacheRoot(dir string) (*os.Root, error) {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return nil, err
	}
	// Windows can expand short names and restore drive/component casing even
	// when no link exists. Inspect the components themselves instead of using
	// a lexical comparison with EvalSymlinks as a symlink detector.
	for component := abs; ; component = filepath.Dir(component) {
		info, err := os.Lstat(component)
		if err != nil {
			return nil, err
		}
		if info.Mode()&(os.ModeSymlink|os.ModeIrregular) != 0 {
			return nil, fmt.Errorf("cache root contains a symbolic link or unsupported filesystem entry")
		}
		if filepath.Dir(component) == component {
			break
		}
	}
	before, err := os.Lstat(abs)
	if err != nil {
		return nil, err
	}
	root, err := os.OpenRoot(abs)
	if err != nil {
		return nil, err
	}
	after, err := root.Stat(".")
	if err != nil || !os.SameFile(before, after) {
		_ = root.Close()
		return nil, fmt.Errorf("cache root changed during inspection")
	}
	return root, nil
}

func readCacheEntries(ctx context.Context, root *os.Root, kind string) ([]cacheDiskEntry, error) {
	names, err := fs.ReadDir(root.FS(), ".")
	if err != nil {
		return nil, err
	}
	var out []cacheDiskEntry
	for _, name := range names {
		e := cacheDiskEntry{root: root, name: name.Name(), kind: kind}
		err := fs.WalkDir(root.FS(), name.Name(), func(_ string, d fs.DirEntry, err error) error {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			if err != nil {
				return err
			}
			fi, err := d.Info()
			if err != nil {
				return err
			}
			if !d.IsDir() {
				e.size += fi.Size()
			}
			e.mtime = max(e.mtime, fi.ModTime().UnixNano())
			return nil
		})
		if err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, nil
}

func measuredCacheTree(ctx context.Context, dir string) (int64, error) {
	if dir == "" {
		return 0, nil
	}
	root, err := openCacheRoot(dir)
	if errors.Is(err, fs.ErrNotExist) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	defer root.Close()
	entries, err := readCacheEntries(ctx, root, "")
	var total int64
	for _, e := range entries {
		total += e.size
	}
	return total, err
}

func (c *cacheServices) applyStorageBudget(ctx context.Context, spec Spec, dir string, remaining int64) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	configured := spec.Cache.SizeLimit
	if configured <= 0 {
		configured = defaultCacheServiceBytes
	}
	budget := c.budgetLocked(spec.PoolID, configured)
	identity, err := cacheIdentity(spec)
	if err != nil {
		return err
	}
	owned := c.services[identity]
	if owned == nil && dir != "" {
		// Learn old entries after an agent restart without needing a new runner
		// or opening a listener. Do not create directories merely to measure them.
		root, err := openCacheRoot(dir)
		if errors.Is(err, fs.ErrNotExist) {
			budget.SetMaxBytes(remaining)
			return nil
		}
		if err != nil {
			return err
		}
		_ = root.Close()
		svc, err := cacheservice.New(cacheservice.Options{Dir: dir, Budget: budget, Log: c.log})
		if err != nil {
			return err
		}
		owned = &ownedCacheService{svc: svc, poolID: spec.PoolID}
		c.services[identity] = owned
	}
	if owned != nil {
		st := owned.svc.Stats()
		physical, err := measuredCacheTree(ctx, dir)
		if err != nil {
			return err
		}
		// Index files cost disk too. Leave room for the metadata already present;
		// subsequent passes remeasure it alongside pending uploads.
		remaining = max(0, remaining-max(0, physical-st.StoredBytes))
	}
	budget.SetMaxBytes(remaining)
	return nil
}

func (b *DockerBackend) cacheBackend() *DockerBackend { return b }

func (b *DockerBackend) cacheBackendPeers() []*DockerBackend {
	if len(b.cachePeers) == 0 {
		return []*DockerBackend{b}
	}
	return b.cachePeers
}

// Every backend takes the gates in the registry's stable order, including a
// backend whose current pool list is empty but whose old jobs can still run.
func (b *DockerBackend) lockCachePeers() func() {
	peers := b.cacheBackendPeers()
	for _, peer := range peers {
		peer.cacheGate.Lock()
	}
	return func() {
		for i := len(peers) - 1; i >= 0; i-- {
			peers[i].cacheGate.Unlock()
		}
	}
}

// A pool switched between daemons still has one service over its shared
// folder. Reuse its owner rather than reopening its index and sweeping a
// live upload as an abandoned reservation. Caller holds all cache gates.
func (b *DockerBackend) cacheServiceOwner(spec Spec) *cacheServices {
	identity, err := cacheIdentity(spec)
	if err != nil {
		return b.cacheSvcs
	}
	for _, peer := range b.cacheBackendPeers() {
		if peer.sharedDir != b.sharedDir || peer.cacheSvcs == nil {
			continue
		}
		c := peer.cacheSvcs
		c.mu.Lock()
		_, found := c.services[identity]
		c.mu.Unlock()
		if found {
			return c
		}
	}
	return b.cacheSvcs
}

func (b *DockerBackend) cacheServiceURL(ctx context.Context, spec Spec, network string) string {
	unlock := b.lockCachePeers()
	defer unlock()
	return b.cacheServiceOwner(spec).urlForGateway(ctx, spec, network, b.cacheSvcs.gateway)
}
