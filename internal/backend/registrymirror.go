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

	"github.com/eyupio/zoomies/internal/registrymirror"
)

// defaultRegistryMirrorBytes bounds what the mirror keeps when the operator has
// set no size. The mirror evicts, but an unbounded one would fill the host's
// disk with images nobody asked it to keep, so there is no unbounded mirror.
const defaultRegistryMirrorBytes = 10 << 30

// registryMirrorBytes is the bound the mirror is given for what an operator set.
// Zero is how they say "the default"; config.Validate has already refused a
// positive size too small to be useful.
func registryMirrorBytes(set int64) int64 {
	if set <= 0 {
		return defaultRegistryMirrorBytes
	}
	return set
}

// RegistryMirrorStatser is implemented by a backend that runs a registry mirror.
// It is optional, like the other capabilities in this package, so a backend with
// nothing to report is simply absent from the heartbeat. It returns nil until the
// mirror has been started.
type RegistryMirrorStatser interface {
	RegistryMirrorStats() *registrymirror.Stats
}

// registryMirrorOptions is everything a host's mirror is told.
type registryMirrorOptions struct {
	// Enabled is agent.registry_mirror.enabled. Off offers nothing and touches
	// nothing: no folder, no listener, no question put to the daemon.
	Enabled bool
	// MaxBytes is agent.registry_mirror.max_bytes; zero is the default.
	MaxBytes int64
	// SharedDir is this host's shared folder, where the mirror keeps what it
	// holds, and SharedProblem is why there is none when an operator can act on
	// it.
	SharedDir, SharedProblem string
	Log                      *slog.Logger
	// Gateway is the host's address on a network; a field so a test need not
	// have a daemon.
	Gateway func(ctx context.Context, network string) (string, error)
	// Upstream, TokenURL and Service point the mirror at a registry other than
	// Docker Hub. They exist so a test never reaches Docker Hub; empty is Docker
	// Hub, which is the only registry a daemon's registry-mirrors covers.
	Upstream, TokenURL, Service string
}

// registryMirror runs a host's registry mirror and says where a docker-in-docker
// sidecar finds it.
//
// There is one mirror for the host: what it holds is public and content
// addressed, so unlike the cache service it keeps no identity for a pool or a
// repository, and any sidecar that can reach it can read all of it. That is
// nothing the sidecar could not pull itself. It is listened on once for each
// network the host's sidecars are attached to, on the host's address on that
// network and on nothing else, so a sidecar reaches the host there and a machine
// on the host's LAN does not. A second mirror over the same folder would sweep
// the first one's downloads away as leftovers and count every byte twice.
//
// The mirror is only an accelerator, so every failure here is a mirror that is
// not offered and a sidecar that pulls from Docker Hub as it did before. A
// registry mirror that cannot be reached is something dockerd falls back from,
// and nothing in this file may fail a create.
type registryMirror struct {
	opts registryMirrorOptions

	mu sync.Mutex
	// closed is set by close, after which nothing is started: a sidecar being
	// created as the agent shuts down would otherwise open a listener nobody will
	// ever stop.
	closed bool
	// mirror is the one mirror the host has, made when the first sidecar asks.
	// It outlives its listeners, so a listener that stopped on its own and is
	// started again carries the counters on and the totals never fall (a counter
	// that falls is read downstream as the agent restarting).
	mirror *registrymirror.Mirror
	// running is the listeners, by the address they are on.
	running map[string]*runningRegistryMirror
	warned  map[string]bool
}

type runningRegistryMirror struct {
	srv *http.Server
	ln  net.Listener
	url string
}

func newRegistryMirror(opts registryMirrorOptions) *registryMirror {
	return &registryMirror{opts: opts, running: map[string]*runningRegistryMirror{}, warned: map[string]bool{}}
}

// urlFor returns the address, as a sidecar on this network reaches it, of the
// host's mirror, starting it if it is not running. It returns "" when the mirror
// is off or this host cannot offer one, and says why once for each reason.
//
// The address is a bare http://host:port: it is what dockerd is given as
// --registry-mirror, which wants no trailing slash.
func (m *registryMirror) urlFor(ctx context.Context, network string) string {
	if m == nil || !m.opts.Enabled {
		return ""
	}
	switch {
	case runningInContainer():
		// The mirror listens on the host's side of the sidecar's network. An agent
		// in a container is not there, and the address it could offer is one the
		// sidecar may not be able to route to.
		m.warnOnce("container", errors.New("the agent runs in a container, and the mirror listens on the host's side of the sidecar's network"))
		return ""
	case m.opts.SharedDir == "":
		reason := m.opts.SharedProblem
		if reason == "" {
			reason = "this host has no shared folder to keep it in"
		}
		m.warnOnce("shared", errors.New(reason))
		return ""
	}
	host, err := m.opts.Gateway(ctx, network)
	if err != nil {
		m.warnOnce("gateway", err)
		return ""
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	if r, ok := m.running[host]; ok {
		return r.url
	}
	if m.closed {
		return ""
	}
	if m.mirror == nil {
		mirror, err := registrymirror.New(registrymirror.Config{
			Dir:      filepath.Join(m.opts.SharedDir, "registry-mirror"),
			MaxBytes: registryMirrorBytes(m.opts.MaxBytes),
			Upstream: m.opts.Upstream, TokenURL: m.opts.TokenURL, Service: m.opts.Service,
			Log: m.opts.Log.With("registry_mirror", "docker hub"),
		})
		if err != nil {
			m.warnOnceLocked("start", err)
			return ""
		}
		m.mirror = mirror
	}
	ln, err := net.Listen("tcp", net.JoinHostPort(host, "0"))
	if err != nil {
		m.warnOnceLocked("listen", fmt.Errorf("listening on %s: %w", host, err))
		return ""
	}
	r := &runningRegistryMirror{
		ln: ln,
		srv: &http.Server{
			Handler: m.mirror,
			// No read or write deadline: an image layer is hundreds of megabytes
			// and the sidecar's pace is not ours to set. A stalled header is.
			ReadHeaderTimeout: 10 * time.Second,
			IdleTimeout:       2 * time.Minute,
		},
		url: "http://" + ln.Addr().String(),
	}
	m.running[host] = r
	m.opts.Log.Info("started a registry mirror for this host's docker-in-docker sidecars",
		"address", ln.Addr().String(), "limit_bytes", registryMirrorBytes(m.opts.MaxBytes))
	go func() {
		if err := r.srv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
			m.opts.Log.Warn("the registry mirror stopped; the next sidecar will start it again", "error", err)
			m.mu.Lock()
			if m.running[host] == r {
				delete(m.running, host)
			}
			m.mu.Unlock()
		}
	}()
	return r.url
}

// stats is what the mirror has done and holds, or nil when it has not been
// started, so a host that does not run one has nothing to put in its heartbeat.
func (m *registryMirror) stats() *registrymirror.Stats {
	if m == nil {
		return nil
	}
	m.mu.Lock()
	mirror := m.mirror
	m.mu.Unlock()
	if mirror == nil {
		return nil
	}
	st := mirror.Stats()
	return &st
}

func (m *registryMirror) warnOnce(reason string, err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.warnOnceLocked(reason, err)
}

func (m *registryMirror) warnOnceLocked(reason string, err error) {
	if m.warned[reason] {
		return
	}
	m.warned[reason] = true
	m.opts.Log.Warn("this host's docker-in-docker sidecars will pull from Docker Hub directly: the host cannot offer them a registry mirror",
		"reason", reason, "error", err,
		"fix", "this does not fail a job; set agent.registry_mirror.enabled to false to stop this message, or see docs/configuration.md")
}

// close stops the mirror. In-flight transfers get a moment to finish; a sidecar
// that loses one pulls it from Docker Hub, which is all a lost mirror costs.
func (m *registryMirror) close() {
	if m == nil {
		return
	}
	m.mu.Lock()
	m.closed = true
	all := make([]*runningRegistryMirror, 0, len(m.running))
	for _, r := range m.running {
		all = append(all, r)
	}
	m.running = map[string]*runningRegistryMirror{}
	m.mu.Unlock()
	for _, r := range all {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		_ = r.srv.Shutdown(ctx)
		cancel()
	}
}
