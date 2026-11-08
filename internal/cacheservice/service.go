// Package cacheservice is the host-local half of the Actions cache.
//
// A runner that is told to use it (through ACTIONS_RESULTS_URL) restores and
// saves what actions/cache, the setup-* actions and Buildx's type=gha cache
// keep, here and not in GitHub's service, so a workflow changes nothing and a
// fresh runner no longer starts cold. It speaks the toolkit's v2 protocol: four
// JSON calls under /twirp/ and one blob URL per entry.
//
// It is only a cache. Every answer it cannot give is a miss, a lost upload is
// a slower build and never a failed one, and nothing here reads the clock,
// the database or GitHub: the agent owns when it runs and where it keeps its
// folder.
package cacheservice

import (
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// Options say where the service keeps its entries and how much it may keep.
type Options struct {
	// Dir is the folder every entry lives under. It is created if missing.
	Dir string
	// MaxBytes is a ceiling on the bytes of finished entries; zero means no
	// ceiling. The oldest-used entries go first, and only after an upload has
	// finished, so a download in flight is never pulled out from under a reader.
	MaxBytes int64
	// PendingTTL is how long a reservation that was never finished is kept
	// before its bytes are reclaimed. Zero means an hour.
	PendingTTL time.Duration
	Log        *slog.Logger
	// Now is the clock; nil means time.Now. It is injected so expiry is testable.
	Now func() time.Time
}

// Service is an http.Handler.
type Service struct {
	opts Options
	log  *slog.Logger
	now  func() time.Time

	mu      sync.Mutex
	entries map[string]*entry   // finished entries by id
	pending map[string]*pending // reservations by id
}

// New opens the folder and learns what an earlier run left in it.
func New(opts Options) (*Service, error) {
	if opts.Dir == "" {
		return nil, errNoDir
	}
	if opts.PendingTTL <= 0 {
		opts.PendingTTL = time.Hour
	}
	log := opts.Log
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}
	now := opts.Now
	if now == nil {
		now = time.Now
	}
	s := &Service{opts: opts, log: log, now: now,
		entries: map[string]*entry{}, pending: map[string]*pending{}}
	for _, d := range []string{s.blobDir(), s.indexDir(), s.pendingDir()} {
		if err := os.MkdirAll(d, 0o750); err != nil {
			return nil, err
		}
	}
	s.load()
	return s, nil
}

func (s *Service) blobDir() string    { return filepath.Join(s.opts.Dir, "blobs") }
func (s *Service) indexDir() string   { return filepath.Join(s.opts.Dir, "index") }
func (s *Service) pendingDir() string { return filepath.Join(s.opts.Dir, "pending") }

const (
	twirpPrefix = "/twirp/github.actions.results.api.v1.CacheService/"
	blobPrefix  = "/_blob/"
)

// ServeHTTP routes the two kinds of request the toolkit makes.
func (s *Service) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	switch {
	case strings.HasPrefix(r.URL.Path, twirpPrefix):
		s.twirp(w, r, strings.TrimPrefix(r.URL.Path, twirpPrefix))
	case strings.HasPrefix(r.URL.Path, blobPrefix):
		s.blob(w, r, strings.TrimPrefix(r.URL.Path, blobPrefix))
	default:
		http.NotFound(w, r)
	}
}
