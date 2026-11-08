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
func (s *Service) load() {
	items, _ := os.ReadDir(s.indexDir())
	known := map[string]bool{}
	for _, it := range items {
		raw, err := os.ReadFile(filepath.Join(s.indexDir(), it.Name()))
		if err != nil {
			continue
		}
		var e entry
		if json.Unmarshal(raw, &e) != nil || e.ID == "" {
			_ = os.Remove(filepath.Join(s.indexDir(), it.Name()))
			continue
		}
		if _, err := os.Stat(s.blobPath(e.ID)); err != nil {
			_ = os.Remove(filepath.Join(s.indexDir(), it.Name()))
			continue
		}
		s.entries[e.ID] = &e
		known[e.ID] = true
	}
	blobs, _ := os.ReadDir(s.blobDir())
	for _, b := range blobs {
		if !known[b.Name()] {
			_ = os.RemoveAll(filepath.Join(s.blobDir(), b.Name()))
		}
	}
	stale, _ := os.ReadDir(s.pendingDir())
	for _, p := range stale {
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
			s.dropPendingLocked(id)
		}
	}
	for _, e := range s.entries {
		if e.Scope == sc.write && sameSlot(e.Key, e.Version, key, version) {
			return nil, false
		}
	}
	for _, p := range s.pending {
		if p.Scope == sc.write && sameSlot(p.Key, p.Version, key, version) {
			return nil, false
		}
	}
	p := &pending{ID: newID(), Key: key, Version: version, Scope: sc.write, Created: s.now(), Size: -1}
	if err := os.MkdirAll(s.pendingPath(p.ID), 0o750); err != nil {
		return nil, false
	}
	s.pending[p.ID] = p
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
	var p *pending
	for _, c := range s.pending {
		if c.Scope == sc.write && sameSlot(c.Key, c.Version, key, version) {
			p = c
			break
		}
	}
	if p == nil || p.Size < 0 || p.Size != size {
		if p != nil {
			s.dropPendingLocked(p.ID)
		}
		return nil, false
	}
	if err := os.Rename(s.pendingData(p.ID), s.blobPath(p.ID)); err != nil {
		s.dropPendingLocked(p.ID)
		return nil, false
	}
	e := &entry{ID: p.ID, Key: key, Version: version, Scope: p.Scope, Size: size, Created: s.now()}
	raw, _ := json.Marshal(e)
	if err := os.WriteFile(s.indexPath(e.ID), raw, 0o640); err != nil {
		_ = os.Remove(s.blobPath(e.ID))
		s.dropPendingLocked(p.ID)
		return nil, false
	}
	s.dropPendingLocked(p.ID)
	s.entries[e.ID] = e
	s.evictLocked(e.ID)
	return e, true
}

// evictLocked brings the finished entries back under MaxBytes, least recently
// used first, never the entry that was just written. A blob's modification time
// is its last use: a restore touches it.
func (s *Service) evictLocked(keep string) {
	limit := s.opts.MaxBytes
	if limit <= 0 {
		return
	}
	var total int64
	type used struct {
		e    *entry
		when time.Time
	}
	var all []used
	for _, e := range s.entries {
		total += e.Size
		when := e.Created
		if fi, err := os.Stat(s.blobPath(e.ID)); err == nil {
			when = fi.ModTime()
		}
		all = append(all, used{e, when})
	}
	if total <= limit {
		return
	}
	sort.Slice(all, func(i, j int) bool {
		if !all[i].when.Equal(all[j].when) {
			return all[i].when.Before(all[j].when)
		}
		return all[i].e.ID < all[j].e.ID
	})
	for _, u := range all {
		if total <= limit {
			break
		}
		if u.e.ID == keep {
			continue
		}
		_ = os.Remove(s.blobPath(u.e.ID))
		_ = os.Remove(s.indexPath(u.e.ID))
		delete(s.entries, u.e.ID)
		total -= u.e.Size
	}
	s.log.Info("evicted cache entries to stay under the cache service's size limit", "limit_bytes", limit)
}

func (s *Service) touch(id string) {
	now := s.now()
	_ = os.Chtimes(s.blobPath(id), now, now)
}
