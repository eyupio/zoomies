package backend

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"path/filepath"
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
}

type runningCacheService struct {
	srv *http.Server
	ln  net.Listener
	url string
}

func newCacheServices(sharedDir, sharedProblem string, log *slog.Logger, gateway func(context.Context, string) (string, error)) *cacheServices {
	return &cacheServices{
		sharedDir: sharedDir, sharedProblem: sharedProblem, log: log, gateway: gateway,
		running: map[string]*runningCacheService{}, warned: map[string]bool{},
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
			}
			c.mu.Unlock()
		}
	}()
	return r.url
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
