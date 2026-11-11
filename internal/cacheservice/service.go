// Package cacheservice is the host-local half of the Actions cache.
//
// A runner whose node is wrapped to reach it (deploy/node-wrapper.go)
// restores and saves what actions/cache and the setup-* actions keep, here and
// not in GitHub's service, so a workflow changes nothing and a fresh runner no
// longer starts cold. It speaks the toolkit's v2 protocol: four
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
	// Budget, when set, makes this service share MaxBytes with every other
	// service built on the same Budget, and its size ceiling is the Budget's.
	// A pool whose cache is split by repository runs one service for each, and
	// without a shared budget each could keep the whole limit.
	Budget *Budget
	// MaxPending bounds the reservations that are open at once, across the
	// services of a Budget; zero means 64. A job that reserves and never writes
	// would otherwise cost a folder each, for ever.
	MaxPending int
	// MaxPendingBytes bounds the bytes staged by uploads that are not finished,
	// across the services of a Budget; zero means the size ceiling, or the
	// toolkit's own ceiling when there is none.
	MaxPendingBytes int64
	// MaxBlocks bounds the blocks one upload may stage or commit; zero means
	// 50000, which is Azure's own limit and so never refuses a real client.
	MaxBlocks int
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

	count counters

	// mu is the lock of the Budget when there is one, so that evicting across
	// the services of a pool is one critical section and not a lock-ordering
	// problem between them.
	mu      *sync.Mutex
	budget  *Budget
	entries map[string]*entry   // finished entries by id
	pending map[string]*pending // reservations by id
}

// Budget is one size ceiling shared by several services. It is how a pool with
// a cache for each repository stays inside the one limit the pool was given:
// entries are evicted, least recently used first, across all of them.
type Budget struct {
	mu      sync.Mutex
	max     int64
	members []*Service
}

// NewBudget returns a budget of max bytes of finished entries; zero or less
// means none.
func NewBudget(max int64) *Budget { return &Budget{max: max} }

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
	s := &Service{opts: opts, log: log, now: now, budget: opts.Budget,
		entries: map[string]*entry{}, pending: map[string]*pending{}}
	if s.budget != nil {
		s.mu = &s.budget.mu
	} else {
		s.mu = &sync.Mutex{}
	}
	for _, d := range []string{s.blobDir(), s.indexDir(), s.pendingDir()} {
		if err := os.MkdirAll(d, 0o750); err != nil {
			return nil, err
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.budget != nil {
		s.budget.members = append(s.budget.members, s)
	}
	s.load()
	// A folder left over from a run with a larger limit would otherwise sit over
	// this one until the next save happened to evict.
	s.evictLocked("")
	return s, nil
}

// group is every service whose entries count against this one's ceiling.
func (s *Service) group() []*Service {
	if s.budget != nil {
		return s.budget.members
	}
	return []*Service{s}
}

// limit is the ceiling on finished entries; zero or less means none.
func (s *Service) limit() int64 {
	if s.budget != nil {
		return s.budget.max
	}
	return s.opts.MaxBytes
}

// entryLimit is the most one archive may be: the ceiling, and never more than
// the toolkit's own.
func (s *Service) entryLimit() int64 {
	l := s.limit()
	if l <= 0 || l > maxEntryBytes {
		return maxEntryBytes
	}
	return l
}

func (s *Service) maxPending() int {
	if s.opts.MaxPending > 0 {
		return s.opts.MaxPending
	}
	return 64
}

func (s *Service) maxBlocks() int {
	if s.opts.MaxBlocks > 0 {
		return s.opts.MaxBlocks
	}
	return 50000
}

func (s *Service) maxPendingBytes() int64 {
	if s.opts.MaxPendingBytes > 0 {
		return s.opts.MaxPendingBytes
	}
	return s.entryLimit()
}

// pendingTotalsLocked counts the open reservations and the bytes they have
// staged, across the group.
func (s *Service) pendingTotalsLocked() (n int, bytes int64) {
	for _, m := range s.group() {
		for _, p := range m.pending {
			n++
			bytes += p.stagedBytes()
		}
	}
	return n, bytes
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
