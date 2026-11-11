package cacheservice

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

var errNoDir = errors.New("cacheservice: Options.Dir is required")

// entry is a finished cache entry: one archive, filed under the ref that wrote
// it. Its bytes are blobs/<id> and its description is index/<id>.json.
type entry struct {
	ID      string    `json:"id"`
	Key     string    `json:"key"`
	Version string    `json:"version"`
	Scope   string    `json:"scope"`
	Size    int64     `json:"size"`
	Created time.Time `json:"created"`

	// Used is when a restore last read it, and owner is the service whose folder
	// holds it. Both live in memory only: eviction orders by Used without a
	// stat of every entry under the lock, and across a pool's services it needs
	// to know whose files to remove.
	Used  time.Time `json:"-"`
	owner *Service
}

// pending is a reservation: a key a job has said it is about to save. The
// bytes arrive under pending/<id>/ and move into blobs/ only when the job
// finalises, so a reader can never be handed half an archive.
type pending struct {
	ID      string
	Key     string
	Version string
	Scope   string
	Created time.Time
	// Size is the assembled archive's length, or -1 until the upload is whole.
	Size int64

	// data is the bytes of a whole upload, and blocks the bytes of each staged
	// block by its file name. Both are kept so that the bytes a reservation holds
	// are known without touching the disk, and so a commit can be refused on the
	// sum of what it names before a byte is copied.
	data   int64
	blocks map[string]int64
}

// stagedBytes is what this reservation holds on disk now.
func (p *pending) stagedBytes() int64 {
	n := p.data
	for _, b := range p.blocks {
		n += b
	}
	return n
}

func newID() string {
	var b [16]byte
	// crypto/rand does not fail on the platforms Zoomies ships for; if it did,
	// an all-zero id would be refused by the reservation below as a duplicate.
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}

// load learns what an earlier run left behind. An entry whose bytes are gone,
// and bytes with no entry, are both removed: either is a half-finished save
// and only costs a rebuild. Reservations are not kept across a restart.
//
// Anything modified within PendingTTL is left alone. Another service may be
// running over this folder (a second network's listener used to start one), and
// a blob it has just renamed into place has no index yet, nor a pending folder
// the others' reservation: deleting either would fail a live job's save. What is
// left is removed by the next start once it has gone stale.
func (s *Service) load() {
	fresh := func(fi os.FileInfo) bool { return s.now().Sub(fi.ModTime()) < s.opts.PendingTTL }
	items, _ := os.ReadDir(s.indexDir())
	known := map[string]bool{}
	for _, it := range items {
		raw, err := os.ReadFile(filepath.Join(s.indexDir(), it.Name()))
		if err != nil {
			continue
		}
		var e entry
		if json.Unmarshal(raw, &e) != nil || e.ID == "" || strings.ContainsAny(e.ID, `./\`) {
			_ = os.Remove(filepath.Join(s.indexDir(), it.Name()))
			continue
		}
		fi, err := os.Stat(s.blobPath(e.ID))
		if err != nil {
			_ = os.Remove(filepath.Join(s.indexDir(), it.Name()))
			continue
		}
		e.Used, e.owner = fi.ModTime(), s
		if fi.Size() != e.Size {
			e.Size = fi.Size()
		}
		s.entries[e.ID] = &e
		known[e.ID] = true
	}
	blobs, _ := os.ReadDir(s.blobDir())
	for _, b := range blobs {
		if known[b.Name()] {
			continue
		}
		if fi, err := b.Info(); err == nil && fresh(fi) {
			continue
		}
		_ = os.RemoveAll(filepath.Join(s.blobDir(), b.Name()))
	}
	stale, _ := os.ReadDir(s.pendingDir())
	for _, p := range stale {
		if fi, err := p.Info(); err == nil && fresh(fi) {
			continue
		}
		_ = os.RemoveAll(filepath.Join(s.pendingDir(), p.Name()))
	}
}

func (s *Service) blobPath(id string) string      { return filepath.Join(s.blobDir(), id) }
func (s *Service) pendingPath(id string) string   { return filepath.Join(s.pendingDir(), id) }
func (s *Service) pendingData(id string) string   { return filepath.Join(s.pendingPath(id), "data") }
func (s *Service) pendingBlocks(id string) string { return filepath.Join(s.pendingPath(id), "blocks") }
func (s *Service) indexPath(id string) string     { return filepath.Join(s.indexDir(), id+".json") }
func (s *Service) pendingExpired(p *pending) bool { return s.now().Sub(p.Created) > s.opts.PendingTTL }
func (s *Service) dropPendingLocked(id string) {
	delete(s.pending, id)
	_ = os.RemoveAll(s.pendingPath(id))
}

// abandonPendingLocked gives up a reservation that did not become an entry and
// counts it. It is separate from dropPendingLocked, which a save that worked
// also calls to tidy its scratch folder, so that every started save is
// counted as finished or abandoned exactly once. A reservation already gone
// (an upload that failed and was dropped, then finalised) is not counted twice.
func (s *Service) abandonPendingLocked(id string) {
	if _, ok := s.pending[id]; ok {
		s.count.failed.Add(1)
	}
	s.dropPendingLocked(id)
}
func sameSlot(a, b string, c, d string) bool { return a == c && b == d }

// reserve starts a save. It refuses a key that is already stored, or already
// being saved, in the same slot (key, version and the ref being written), which
// is what GitHub does and what the toolkit expects: the second job carries on
// without saving.
func (s *Service) reserve(sc scope, key, version string) (*pending, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for id, p := range s.pending {
		if s.pendingExpired(p) {
			s.abandonPendingLocked(id)
		}
	}
	if !sc.canWrite() {
		return nil, false
	}
	for _, e := range s.entries {
		if e.Scope == sc.write && sameSlot(e.Key, e.Version, key, version) {
			s.count.dupes.Add(1)
			return nil, false
		}
	}
	for _, p := range s.pending {
		if p.Scope == sc.write && sameSlot(p.Key, p.Version, key, version) {
			s.count.dupes.Add(1)
			return nil, false
		}
	}
	// Open reservations are bounded across the pool: each is a folder and a
	// claim on disk, and a job that never writes would otherwise hold it for
	// the whole TTL.
	if n, staged := s.pendingTotalsLocked(); n >= s.maxPending() || staged >= s.maxPendingBytes() {
		s.log.Warn("a cache save was refused: too many uploads are open", "open", n, "staged_bytes", staged)
		return nil, false
	}
	p := &pending{ID: newID(), Key: key, Version: version, Scope: sc.write, Created: s.now(), Size: -1}
	if err := os.MkdirAll(s.pendingPath(p.ID), 0o750); err != nil {
		return nil, false
	}
	s.pending[p.ID] = p
	s.count.started.Add(1)
	return p, true
}

// find picks the entry a restore should be given: the primary key exactly, then
// each restore key as a prefix in the order given, always among entries of the
// same version and of a ref this job may read. Within one key the job's own ref
// wins, then the newest.
func (s *Service) find(sc scope, key string, restore []string, version string) *entry {
	s.mu.Lock()
	defer s.mu.Unlock()
	best := func(match func(string) bool) *entry {
		var cand []*entry
		for _, e := range s.entries {
			if e.Version == version && sc.read[e.Scope] && match(e.Key) {
				cand = append(cand, e)
			}
		}
		sort.Slice(cand, func(i, j int) bool {
			ownI, ownJ := cand[i].Scope == sc.write, cand[j].Scope == sc.write
			if ownI != ownJ {
				return ownI
			}
			if !cand[i].Created.Equal(cand[j].Created) {
				return cand[i].Created.After(cand[j].Created)
			}
			return cand[i].ID < cand[j].ID
		})
		if len(cand) == 0 {
			return nil
		}
		return cand[0]
	}
	if e := best(func(k string) bool { return k == key }); e != nil {
		return e
	}
	for _, p := range restore {
		if p == "" {
			continue
		}
		if e := best(func(k string) bool { return strings.HasPrefix(k, p) }); e != nil {
			return e
		}
	}
	return nil
}

// finalise turns a whole upload into an entry. It fails, leaving nothing
// behind, when the bytes are not the size the job said they would be.
func (s *Service) finalise(sc scope, key, version string, size int64) (*entry, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !sc.canWrite() {
		return nil, false
	}
	var p *pending
	for _, c := range s.pending {
		if c.Scope == sc.write && sameSlot(c.Key, c.Version, key, version) {
			p = c
			break
		}
	}
	if p == nil || p.Size < 0 || p.Size != size {
		if p != nil {
			s.abandonPendingLocked(p.ID)
		}
		return nil, false
	}
	if err := os.Rename(s.pendingData(p.ID), s.blobPath(p.ID)); err != nil {
		s.abandonPendingLocked(p.ID)
		return nil, false
	}
	e := &entry{ID: p.ID, Key: key, Version: version, Scope: p.Scope, Size: size, Created: s.now(), owner: s}
	e.Used = e.Created
	raw, _ := json.Marshal(e)
	if err := os.WriteFile(s.indexPath(e.ID), raw, 0o640); err != nil {
		_ = os.Remove(s.blobPath(e.ID))
		s.abandonPendingLocked(p.ID)
		return nil, false
	}
	s.dropPendingLocked(p.ID)
	s.count.finalised.Add(1)
	s.entries[e.ID] = e
	s.evictLocked(e.ID)
	return e, true
}

// evictLocked brings the finished entries back under the ceiling, least
// recently used first, never the entry that was just written. The ceiling is
// shared by every service of a Budget, and the bytes of uploads in flight count
// against it: they become entries when they finish, so leaving them out would
// let a pool run well over its limit while a few large saves were open.
func (s *Service) evictLocked(keep string) {
	limit := s.limit()
	if limit <= 0 {
		return
	}
	_, total := s.pendingTotalsLocked()
	var all []*entry
	for _, m := range s.group() {
		for _, e := range m.entries {
			total += e.Size
			all = append(all, e)
		}
	}
	if total <= limit {
		return
	}
	sort.Slice(all, func(i, j int) bool {
		if !all[i].Used.Equal(all[j].Used) {
			return all[i].Used.Before(all[j].Used)
		}
		return all[i].ID < all[j].ID
	})
	for _, e := range all {
		if total <= limit {
			break
		}
		if e.ID == keep {
			continue
		}
		o := e.owner
		_ = os.Remove(o.blobPath(e.ID))
		_ = os.Remove(o.indexPath(e.ID))
		delete(o.entries, e.ID)
		o.count.evictions.Add(1)
		o.count.evictedBytes.Add(uint64(max(e.Size, 0)))
		total -= e.Size
	}
	s.log.Info("evicted cache entries to stay under the cache service's size limit", "limit_bytes", limit)
}

// touch records a read as a use. The time is kept in memory for eviction and
// written to the blob as well, so the order survives a restart.
func (s *Service) touch(id string) {
	now := s.now()
	s.mu.Lock()
	if e, ok := s.entries[id]; ok {
		e.Used = now
	}
	s.mu.Unlock()
	_ = os.Chtimes(s.blobPath(id), now, now)
}
