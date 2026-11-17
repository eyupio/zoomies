// Package registrymirror is a read-only pull-through mirror for the public
// images of one container registry, and nothing about the fleet: no store, no
// config, no controller, no clock of its own. The agent decides where and when
// it runs and where its folder is.
package registrymirror

import (
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// Config is everything a Mirror is told. Only Dir and MaxBytes are required.
type Config struct {
	// Upstream is the registry's base URL. Empty is Docker Hub.
	Upstream string
	// TokenURL is the registry's token service. Empty is Docker Hub's.
	TokenURL string
	// Service is the token service's service name. Empty is Docker Hub's.
	Service string
	// Dir holds what the mirror keeps. It is created when missing.
	Dir string
	// MaxBytes bounds what the mirror keeps. It must be positive.
	MaxBytes int64
	// TagTTL is how long a tag's digest is trusted before upstream is asked
	// again. Zero is five minutes.
	TagTTL time.Duration
	// IdleTimeout is how long a transfer may make no progress. Zero is thirty
	// seconds.
	IdleTimeout time.Duration
	// Client talks to the registry and its token service. Nil builds one with
	// timeouts.
	Client *http.Client
	// RedirectClient fetches the place a registry redirects a blob to. Nil
	// builds one that refuses private, loopback and link-local addresses.
	RedirectClient *http.Client
	// Now is the clock the tag cache reads. Nil is time.Now.
	Now func() time.Time
	// Log receives what went wrong. Nil discards it.
	Log *slog.Logger
}

// Stats is what a mirror has done since it started, except BytesStored, which
// is what it holds now.
type Stats struct {
	Requests       int64
	Hits           int64
	Misses         int64
	UpstreamErrors int64
	Evictions      int64
	BytesServed    int64
	BytesFetched   int64
	BytesStored    int64
}

// Mirror serves the read subset of the registry HTTP API.
type Mirror struct {
	cfg      Config
	client   *http.Client
	redirect *http.Client
	tokens   *tokenSource
	store    *store

	tagMu   sync.Mutex
	tags    map[tagKey]tagEntry
	maxTags int

	flightMu sync.Mutex
	flights  map[string]*flight

	requests, hits, misses, upstreamErrors atomic.Int64
	bytesServed, bytesFetched              atomic.Int64
}

// New checks the configuration and prepares the store.
func New(cfg Config) (*Mirror, error) {
	if cfg.Dir == "" {
		return nil, errors.New("registrymirror: Dir is required: the mirror has nowhere to keep anything")
	}
	if cfg.MaxBytes <= 0 {
		return nil, errors.New("registrymirror: MaxBytes must be positive: there is no unbounded mirror")
	}
	if cfg.Upstream == "" {
		cfg.Upstream = defaultUpstream
	}
	cfg.Upstream = strings.TrimRight(cfg.Upstream, "/")
	if cfg.TokenURL == "" {
		cfg.TokenURL = defaultTokenURL
	}
	if cfg.Service == "" {
		cfg.Service = defaultService
	}
	if cfg.TagTTL <= 0 {
		cfg.TagTTL = defaultTagTTL
	}
	if cfg.IdleTimeout <= 0 {
		cfg.IdleTimeout = defaultIdleTimeout
	}
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	if cfg.Log == nil {
		cfg.Log = slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	if cfg.Client == nil {
		cfg.Client = &http.Client{Transport: newTransport()}
	}
	if cfg.RedirectClient == nil {
		cfg.RedirectClient = newGuardedClient()
	}
	st, err := newStore(cfg.Dir, cfg.MaxBytes, cfg.Now)
	if err != nil {
		return nil, err
	}
	m := &Mirror{cfg: cfg, client: noRedirects(cfg.Client), redirect: cfg.RedirectClient, store: st, tags: map[tagKey]tagEntry{}, maxTags: defaultMaxTags, flights: map[string]*flight{}}
	m.tokens = &tokenSource{client: m.client, url: cfg.TokenURL, service: cfg.Service, now: cfg.Now, cache: map[string]cachedToken{}}
	return m, nil
}

// countingWriter counts what is written to a client, for Stats.
type countingWriter struct {
	http.ResponseWriter
	n *atomic.Int64
}

// Unwrap lets http.NewResponseController reach the real writer, to flush it.
func (c countingWriter) Unwrap() http.ResponseWriter { return c.ResponseWriter }

func (c countingWriter) Write(b []byte) (int, error) {
	n, err := c.ResponseWriter.Write(b)
	c.n.Add(int64(n))
	return n, err
}

// ServeHTTP answers one registry request.
func (m *Mirror) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		writeError(w, http.StatusMethodNotAllowed, "UNSUPPORTED", "this mirror only reads")
		return
	}
	rt, apiErr := parseRoute(r.URL.Path)
	if apiErr != nil {
		apiErr.write(w)
		return
	}
	w = countingWriter{ResponseWriter: w, n: &m.bytesServed}
	switch rt.kind {
	case routePing:
		w.Header().Set("Docker-Distribution-API-Version", "registry/2.0")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte("{}"))
	case routeBlob:
		m.requests.Add(1)
		m.serveBlob(w, r, rt)
	default:
		m.requests.Add(1)
		m.serveManifest(w, r, rt)
	}
}

// fail answers a request the mirror could not serve, and counts what is the
// registry's doing rather than the caller's.
func (m *Mirror) fail(w http.ResponseWriter, e *apiError) {
	if e.status == http.StatusTooManyRequests || e.status >= 500 {
		m.upstreamErrors.Add(1)
	}
	e.write(w)
}

// Stats reports what the mirror has done.
func (m *Mirror) Stats() Stats {
	return Stats{
		Requests:       m.requests.Load(),
		Hits:           m.hits.Load(),
		Misses:         m.misses.Load(),
		UpstreamErrors: m.upstreamErrors.Load(),
		Evictions:      m.store.evicted(),
		BytesServed:    m.bytesServed.Load(),
		BytesFetched:   m.bytesFetched.Load(),
		BytesStored:    m.store.stored(),
	}
}

type routeKind int

const (
	routePing routeKind = iota
	routeManifest
	routeBlob
)

type route struct {
	kind routeKind
	name string
	ref  string // a tag or a digest for a manifest; a digest for a blob
}

type apiError struct {
	status     int
	code       string
	message    string
	retryAfter string
}

func (e *apiError) write(w http.ResponseWriter) {
	if e.retryAfter != "" {
		w.Header().Set("Retry-After", e.retryAfter)
	}
	writeError(w, e.status, e.code, e.message)
}

// parseRoute names what a path asks for, or says why it cannot be served. The
// name is everything between /v2/ and the last /manifests/ or /blobs/, because
// a repository name has slashes in it.
func parseRoute(path string) (route, *apiError) {
	if path == "/v2/" || path == "/v2" {
		return route{kind: routePing}, nil
	}
	rest, ok := strings.CutPrefix(path, "/v2/")
	if !ok {
		return route{}, &apiError{status: http.StatusNotFound, code: "UNSUPPORTED", message: "this mirror serves manifests and blobs"}
	}
	kind, marker := routeManifest, "/manifests/"
	i := strings.LastIndex(rest, marker)
	if j := strings.LastIndex(rest, "/blobs/"); j > i {
		kind, marker, i = routeBlob, "/blobs/", j
	}
	if i < 0 {
		return route{}, &apiError{status: http.StatusNotFound, code: "UNSUPPORTED", message: "this mirror serves manifests and blobs"}
	}
	name, ref := rest[:i], rest[i+len(marker):]
	if !validName(name) {
		return route{}, &apiError{status: http.StatusBadRequest, code: "NAME_INVALID", message: "invalid repository name"}
	}
	switch {
	case kind == routeBlob && !validDigest(ref):
		return route{}, &apiError{status: http.StatusBadRequest, code: "DIGEST_INVALID", message: "a blob is named by a sha256 digest"}
	case kind == routeManifest && isDigest(ref) && !validDigest(ref):
		return route{}, &apiError{status: http.StatusBadRequest, code: "DIGEST_INVALID", message: "a digest must be sha256 and 64 hex digits"}
	case kind == routeManifest && !isDigest(ref) && !validTag(ref):
		return route{}, &apiError{status: http.StatusBadRequest, code: "TAG_INVALID", message: "invalid tag"}
	}
	return route{kind: kind, name: name, ref: ref}, nil
}

func writeError(w http.ResponseWriter, status int, code, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{"errors": []map[string]string{{"code": code, "message": message}}})
}
