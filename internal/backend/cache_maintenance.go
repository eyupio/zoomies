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

// cachePass is one pool's cache as a maintenance pass found it: what was
// measured before the gates were taken, and what eviction may then act on.
type cachePass struct {
	policy                           CachePolicy
	spec                             Spec
	st                               CacheStorage
	packageDir, toolsDir, serviceDir string
	measurable                       bool
	roots                            []*os.Root
	entries                          []cacheDiskEntry
	// settled says the pool's line is already written and nothing may be
	// evicted for it: its folders could not be told apart or resolved.
	settled bool
	// budgetFailed says the cache service was not told its room, so what it
	// holds was not changed by this pass and is not read again.
	budgetFailed bool
}

// MaintainCaches also runs when placement is disk-blocked and no new runner
// will arrive to trigger the old create-time prune.
//
// It measures first and takes the gates second. A package cache is millions of
// small files on a busy host, and a runner start waits on the same gates, so a
// pass that walked every tree while holding them held every start on the host
// for as long as the walk took. What is measured without the gates can be a
// little stale by the time they are taken; whether anything is using the cache
// is asked only once they are held, so a stale size costs an eviction that is
// slightly too large or too small, never a file deleted under a job.
func (b *DockerBackend) MaintainCaches(ctx context.Context) {
	b.cacheGate.Lock()
	policies := append([]CachePolicy(nil), b.cachePolicies...)
	b.cacheGate.Unlock()
	if len(policies) == 0 {
		return
	}
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	passes := make([]*cachePass, 0, len(policies))
	defer func() {
		for _, pass := range passes {
			for _, root := range pass.roots {
				_ = root.Close()
			}
		}
	}()
	for _, p := range policies {
		if ctx.Err() != nil {
			break
		}
		passes = append(passes, b.measureCache(ctx, p))
	}
	b.evictCaches(ctx, passes)
	out := make([]CacheStorage, 0, len(passes))
	for _, pass := range passes {
		st := &pass.st
		if !pass.settled && !st.MeasuredAt.IsZero() && pass.measurable && !pass.budgetFailed {
			// The service evicts through its own index as its budget shrinks,
			// so what it holds is read again, and again without the gates.
			if n, err := measuredCacheTree(ctx, pass.serviceDir); err == nil {
				st.ServiceBytes = n
			} else {
				st.MeasuredAt = time.Time{}
				st.Deferred = "Cache service storage could not be measured"
			}
		}
		st.TotalBytes = st.PackageBytes + st.ToolsBytes + st.ServiceBytes
		if st.SizeLimit > 0 && st.TotalBytes > st.SizeLimit && st.Deferred == "" {
			st.Deferred = "Open cache transfers or metadata still exceed the limit"
		}
		out = append(out, *st)
	}
	b.cacheGate.Lock()
	defer b.cacheGate.Unlock()
	// Do not replace an earlier measurement with a false zero on cancellation,
	// nor bring one back for policies withdrawn while this pass was measuring.
	if ctx.Err() == nil && len(b.cachePolicies) > 0 {
		b.cacheStorage = out
	}
}

// measureCache reads one pool's cache from disk. It holds no gate and deletes
// nothing.
func (b *DockerBackend) measureCache(ctx context.Context, p CachePolicy) *cachePass {
	pass := &cachePass{
		policy: p,
		spec:   Spec{PoolID: p.PoolID, Repository: p.Repository, Cache: p.Cache},
		st:     CacheStorage{PoolID: p.PoolID, SizeLimit: p.Cache.SizeLimit},
	}
	st := &pass.st
	identity, err := cacheIdentity(pass.spec)
	if err != nil {
		st.Deferred, pass.settled = "Cache identity could not be resolved", true
		return pass
	}
	pass.packageDir, pass.measurable = cacheDirectory(pass.spec)
	if b.sharedDir != "" {
		pass.toolsDir = filepath.Join(b.sharedDir, "cache", "tools", identity)
		pass.serviceDir = filepath.Join(b.sharedDir, "cache", "service", identity)
	}
	if pass.packageDir != "" && (pathsOverlap(pass.packageDir, pass.toolsDir) || pathsOverlap(pass.packageDir, pass.serviceDir)) {
		st.Deferred, pass.settled = "Package cache overlaps tool or service storage; choose a separate cache source", true
		return pass
	}
	if b.sharedDir == "" && (p.Cache.Tools || p.Cache.Service) {
		st.Deferred, pass.settled = "Tool or service storage is unavailable; total size is unknown", true
		return pass
	}
	for _, part := range []struct{ path, kind string }{{pass.packageDir, "package"}, {pass.toolsDir, "tools"}} {
		if part.path == "" {
			continue
		}
		root, err := openCacheRoot(part.path)
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil {
			st.Deferred = cacheRootRefusal(err)
			break
		}
		pass.roots = append(pass.roots, root)
		items, err := readCacheEntries(ctx, root, part.kind)
		if err != nil {
			st.Deferred = "Cache size could not be measured"
			break
		}
		pass.entries = append(pass.entries, items...)
	}
	for _, e := range pass.entries {
		if e.kind == "package" {
			st.PackageBytes += e.size
		} else {
			st.ToolsBytes += e.size
		}
	}
	st.ServiceBytes, err = measuredCacheTree(ctx, pass.serviceDir)
	if err != nil {
		st.Deferred = "Cache service storage could not be measured"
	}
	if st.Deferred == "" {
		st.MeasuredAt = time.Now().UTC()
	}
	return pass
}

// evictCaches is the part of a pass that needs every backend's gate: asking
// whether anything is using a cache, deleting from it, and telling the cache
// service how much room is left.
func (b *DockerBackend) evictCaches(ctx context.Context, passes []*cachePass) {
	unlock := b.lockCachePeers()
	defer unlock()
	var inventory []ContainerSummary
	var inventoryErr error
	busy := false
	for _, peer := range b.cacheBackendPeers() {
		rows, err := peer.api.ContainerList(ctx, nil)
		inventory = append(inventory, rows...)
		inventoryErr = errors.Join(inventoryErr, err)
		busy = busy || peer.cacheBusy > 0
	}
	for _, pass := range passes {
		if pass.settled {
			continue
		}
		st, p := &pass.st, pass.policy
		if !st.MeasuredAt.IsZero() {
			switch {
			case !pass.measurable:
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
					used := c.Labels[LabelPoolID] == p.PoolID || c.Labels[LabelCacheVolume] == pass.packageDir
					for _, mount := range c.Mounts {
						for _, dir := range []string{pass.packageDir, pass.toolsDir} {
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
			entries := pass.entries
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
		if !st.MeasuredAt.IsZero() && pass.measurable {
			limit := st.SizeLimit
			if limit <= 0 {
				limit = defaultCacheServiceBytes
			} else {
				limit = max(0, limit-st.PackageBytes-st.ToolsBytes)
			}
			if err := b.cacheServiceOwner(pass.spec).applyStorageBudget(pass.spec, pass.serviceDir, limit, st.ServiceBytes); err != nil {
				st.Deferred, pass.budgetFailed = "Cache service budget could not be updated", true
			}
		}
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

// errCacheRootIsALink is a cache directory that is itself a symbolic link, or
// something that is not a folder at all.
var errCacheRootIsALink = errors.New("the cache directory is a symbolic link or an unsupported filesystem entry")

// cacheRootRefusal is the line an operator reads when a cache could not be
// opened, saying what to change where there is something to change.
func cacheRootRefusal(err error) string {
	if errors.Is(err, errCacheRootIsALink) {
		return "Cache directory is a symbolic link; point the cache at the folder it leads to"
	}
	return "Cache directory could not be safely read"
}

// OpenRoot pins the directory during eviction and contains nested symlinks.
//
// The pool's own directory may not be a link: it is the one name here that
// work on the host could have a hand in, and following it would evict from
// data the pool's configured path does not own. A link above it is the
// operator's own (a data directory kept on another disk, or /var on macOS), so
// it is followed. Refusing those too left every size limit on such a host
// unenforced, with nothing but a line in the log to say so.
func openCacheRoot(dir string) (*os.Root, error) {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return nil, err
	}
	info, err := os.Lstat(abs)
	if err != nil {
		return nil, err
	}
	if info.Mode()&(os.ModeSymlink|os.ModeIrregular) != 0 {
		return nil, errCacheRootIsALink
	}
	// Windows can expand short names and restore drive/component casing even
	// when no link exists, and cannot resolve every path it can open. Inspect
	// the components themselves, and resolve only where one of them is a link.
	linked := false
	for component := filepath.Dir(abs); ; component = filepath.Dir(component) {
		info, err := os.Lstat(component)
		if err != nil {
			return nil, err
		}
		if info.Mode()&os.ModeIrregular != 0 {
			return nil, errCacheRootIsALink
		}
		linked = linked || info.Mode()&os.ModeSymlink != 0
		if filepath.Dir(component) == component {
			break
		}
	}
	if linked {
		parent, err := filepath.EvalSymlinks(filepath.Dir(abs))
		if err != nil {
			return nil, err
		}
		abs = filepath.Join(parent, filepath.Base(abs))
	}
	before, err := os.Lstat(abs)
	if err != nil {
		return nil, err
	}
	if before.Mode()&(os.ModeSymlink|os.ModeIrregular) != 0 {
		return nil, errCacheRootIsALink
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

// weigh adds one file or folder of a walk to the entry it belongs to.
//
// A job writing to the cache deletes its temporary files as it goes. One that
// went between the listing and the look at it weighs nothing; it is not a tree
// that could not be measured, which is what failing the walk made of it: the
// pool's size read as unknown and its limit went unenforced, on exactly the
// hosts busy enough to need it.
func (e *cacheDiskEntry) weigh(d fs.DirEntry, err error) error {
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	fi, err := d.Info()
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if !d.IsDir() {
		e.size += fi.Size()
	}
	e.mtime = max(e.mtime, fi.ModTime().UnixNano())
	return nil
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
			return e.weigh(d, err)
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

// applyStorageBudget takes the service folder's size as the pass measured it,
// rather than walking it again while every backend's gate is held.
func (c *cacheServices) applyStorageBudget(spec Spec, dir string, remaining, physical int64) error {
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
