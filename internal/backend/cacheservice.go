package backend

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"github.com/eyupio/zoomies/internal/cacheservice"
)

// defaultCacheServiceBytes bounds what a pool's cache service keeps when the
// pool has set no size limit of its own. The service evicts, so an unbounded
// one would fill the host's disk with entries nobody asked it to keep.
const defaultCacheServiceBytes = 10 << 30

// cacheServices runs a pool's cache service on the host and says where runners
// find it.
//
// There is one service for each cache identity (cacheIdentity), which is the
// same name the pool cache and the tool cache are kept under, so a service is
// shared with exactly the runners the pool cache is shared with and no others.
// Each listens on its own port, on the host's address on the network the
// runners are attached to and on nothing else: a runner reaches the host there,
// and a machine on the host's LAN does not.
//
// The cache is only an accelerator, so every failure here is a service that is
// not offered and a runner that uses GitHub's cache as it did before. Nothing
// in this file may fail a create.
type cacheServices struct {
	sharedDir     string
	sharedProblem string
	log           *slog.Logger
	// gateway is the host's address on a network; a field so a test need not
	// have a daemon.
	gateway func(ctx context.Context, network string) (string, error)

	mu      sync.Mutex
	running map[string]*runningCacheService
	warned  map[string]bool
	// retired is what services that stopped on their own had counted, by pool,
	// so a service that is started again does not take the pool's totals back
	// to zero. A counter that falls is read downstream as the agent restarting,
	// and a stopped listener is not that.
	retired map[string]cacheservice.Stats
}

type runningCacheService struct {
	srv *http.Server
	ln  net.Listener
	url string
	svc *cacheservice.Service
	// poolID and identity say whose service this is, so its figures can be
	// reported under the pool and its folder is counted once.
	poolID, identity string
}

// CacheStats is what a pool's cache services have done on this host and what
// they hold, summed over the services the pool has (one for each cache
// identity, so a repository-scoped pool has several). It is what an agent puts
// in its heartbeat, so the controller can say whether a cache is earning its
// disk.
//
// The counters are cumulative for the agent process and the two gauges are the
// folders as they are now. Nothing here is a setting or a decision: it is the
// service's own account of what it did.
type CacheStats struct {
	PoolID string `json:"pool_id"`
	cacheservice.Stats
}

// CacheStatser is implemented by a backend that runs cache services. It is
// optional, like the other capabilities in this package, so a backend with
// nothing to report is simply absent from the heartbeat.
type CacheStatser interface {
	CacheStats() []CacheStats
}

func newCacheServices(sharedDir, sharedProblem string, log *slog.Logger, gateway func(context.Context, string) (string, error)) *cacheServices {
	return &cacheServices{
		sharedDir: sharedDir, sharedProblem: sharedProblem, log: log, gateway: gateway,
		running: map[string]*runningCacheService{}, warned: map[string]bool{},
		retired: map[string]cacheservice.Stats{},
	}
}

// urlFor returns the address, inside the runner, of the service this spec's
// pool should use, starting it if it is not running. It returns "" when the
// pool has not asked for one, or when this host cannot offer one, and says why
// once for each pool and reason.
func (c *cacheServices) urlFor(ctx context.Context, spec Spec, network string) string {
	if !spec.Cache.Enabled || !spec.Cache.Service {
		return ""
	}
	identity, err := cacheIdentity(spec)
	if err != nil {
		c.warnOnce(spec, "identity", err)
		return ""
	}
	switch {
	case runningInContainer():
		// The service listens on the host's side of the runner network. An
		// agent in a container is not there, and the address it could offer
		// is one the runners may not be able to route to.
		c.warnOnce(spec, "container", errors.New("the agent runs in a container, and the cache service listens on the host's side of the runner network"))
		return ""
	case c.sharedDir == "":
		reason := c.sharedProblem
		if reason == "" {
			reason = "this host has no shared folder to keep it in"
		}
		c.warnOnce(spec, "shared", errors.New(reason))
		return ""
	}
	host, err := c.gateway(ctx, network)
	if err != nil {
		c.warnOnce(spec, "gateway", err)
		return ""
	}

	key := identity + "|" + host
	c.mu.Lock()
	defer c.mu.Unlock()
	if r, ok := c.running[key]; ok {
		return r.url
	}
	limit := spec.Cache.SizeLimit
	if limit <= 0 {
		limit = defaultCacheServiceBytes
	}
	svc, err := cacheservice.New(cacheservice.Options{
		Dir:      filepath.Join(c.sharedDir, "cache", "service", identity),
		MaxBytes: limit,
		Log:      c.log.With("cache_service", identity),
	})
	if err != nil {
		c.warnOnceLocked(spec, "start", err)
		return ""
	}
	ln, err := net.Listen("tcp", net.JoinHostPort(host, "0"))
	if err != nil {
		c.warnOnceLocked(spec, "listen", fmt.Errorf("listening on %s: %w", host, err))
		return ""
	}
	r := &runningCacheService{
		ln: ln,
		srv: &http.Server{
			Handler: svc,
			// No read or write deadline: an archive is hundreds of megabytes
			// and the runner's pace is not ours to set. A stalled header is.
			ReadHeaderTimeout: 10 * time.Second,
			IdleTimeout:       2 * time.Minute,
		},
		url: "http://" + ln.Addr().String() + "/",
		svc: svc, poolID: spec.PoolID, identity: identity,
	}
	c.running[key] = r
	c.log.Info("started a cache service for a pool's runners",
		"address", ln.Addr().String(), "identity", identity, "limit_bytes", limit)
	go func() {
		if err := r.srv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
			c.log.Warn("a cache service stopped; the next runner will start it again", "identity", identity, "error", err)
			c.mu.Lock()
			if c.running[key] == r {
				delete(c.running, key)
				c.retireLocked(r)
			}
			c.mu.Unlock()
		}
	}()
	return r.url
}

// retireLocked keeps a stopped service's counters. Its gauges go with it: the
// next service started on the same folder reads what is there.
func (c *cacheServices) retireLocked(r *runningCacheService) {
	st := r.svc.Stats()
	st.Entries, st.StoredBytes = 0, 0
	c.retired[r.poolID] = addCounters(c.retired[r.poolID], st)
}

// addCounters adds b's counters to a. The gauges are not summed here, because
// two services over one folder describe the same bytes.
func addCounters(a, b cacheservice.Stats) cacheservice.Stats {
	a.RestoreHits += b.RestoreHits
	a.RestoreMisses += b.RestoreMisses
	a.PutsStarted += b.PutsStarted
	a.PutsFinalised += b.PutsFinalised
	a.PutsFailed += b.PutsFailed
	a.PutsDuplicate += b.PutsDuplicate
	a.Evictions += b.Evictions
	a.EvictedBytes += b.EvictedBytes
	return a
}

// stats reports every pool that has had a service on this host, ordered by pool
// so two reads of an unchanged host are equal.
//
// Counters are summed over a pool's services. Gauges are summed over its cache
// identities, but not over the services of one identity: the same identity
// listens on a second address when runners are attached to a second network,
// and both services keep the one folder, so adding their figures would count
// every byte twice. The larger of the two is taken, since each only knows what
// it saw written.
func (c *cacheServices) stats() []CacheStats {
	if c == nil {
		return nil
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	byPool := map[string]cacheservice.Stats{}
	for pool, st := range c.retired {
		byPool[pool] = st
	}
	type held struct{ entries, bytes int64 }
	byIdentity := map[string]map[string]held{} // pool, then identity
	for _, r := range c.running {
		st := r.svc.Stats()
		byPool[r.poolID] = addCounters(byPool[r.poolID], st)
		if byIdentity[r.poolID] == nil {
			byIdentity[r.poolID] = map[string]held{}
		}
		g := byIdentity[r.poolID][r.identity]
		g.entries, g.bytes = max(g.entries, st.Entries), max(g.bytes, st.StoredBytes)
		byIdentity[r.poolID][r.identity] = g
	}
	out := make([]CacheStats, 0, len(byPool))
	for pool, st := range byPool {
		for _, g := range byIdentity[pool] {
			st.Entries += g.entries
			st.StoredBytes += g.bytes
		}
		out = append(out, CacheStats{PoolID: pool, Stats: st})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].PoolID < out[j].PoolID })
	return out
}

func (c *cacheServices) warnOnce(spec Spec, reason string, err error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.warnOnceLocked(spec, reason, err)
}

func (c *cacheServices) warnOnceLocked(spec Spec, reason string, err error) {
	k := spec.PoolID + "|" + reason
	if c.warned[k] {
		return
	}
	c.warned[k] = true
	c.log.Warn("this pool's runners will use GitHub's cache: the host cannot offer them a cache service",
		"pool", spec.PoolName, "reason", reason, "error", err,
		"fix", "this does not fail a job; turn cache.service off for the pool to stop this message, or see docs/persistent-caches.md")
}

// close stops every service. In-flight transfers get a moment to finish; a
// runner that loses one has a cache miss, which is all a lost cache is.
func (c *cacheServices) close() {
	c.mu.Lock()
	all := make([]*runningCacheService, 0, len(c.running))
	for _, r := range c.running {
		all = append(all, r)
	}
	c.running = map[string]*runningCacheService{}
	c.mu.Unlock()
	for _, r := range all {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		_ = r.srv.Shutdown(ctx)
		cancel()
	}
}
