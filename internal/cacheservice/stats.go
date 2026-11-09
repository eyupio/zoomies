package cacheservice

import "sync/atomic"

// Stats is what the service has done since it started and what it holds now.
//
// It exists so an operator can see whether a pool's cache is earning its disk:
// a service that serves nothing and a service that evicts what it has just
// stored look identical from the outside, and both cost the same space. The
// counters are cumulative for this Service value, so a reader that polls it
// takes differences and treats a fall as a restart; the two gauges at the end
// are the folder as it is now.
type Stats struct {
	// RestoreHits are lookups answered with an entry, whether the key matched
	// exactly or a restore key matched as a prefix: either way the job got a
	// cache. RestoreMisses are lookups that found nothing, which the toolkit
	// reads as a cold build.
	RestoreHits   uint64 `json:"restore_hits"`
	RestoreMisses uint64 `json:"restore_misses"`
	// PutsStarted are saves the service agreed to take. Each ends as exactly one
	// of PutsFinalised or PutsFailed, or is still in flight, so the three can be
	// checked against one another.
	PutsStarted   uint64 `json:"puts_started"`
	PutsFinalised uint64 `json:"puts_finalised"`
	// PutsFailed are reservations that never became an entry: an upload that
	// could not be stored, bytes that were not the size the job promised, or a
	// reservation nobody finished before it expired.
	PutsFailed uint64 `json:"puts_failed"`
	// PutsDuplicate are saves the service declined because the same key and
	// version was already stored, or already being saved, for that ref. It is
	// the protocol's own answer and the normal one for a cache that is warm, so
	// it is kept apart from PutsFailed, which is not.
	PutsDuplicate uint64 `json:"puts_duplicate"`
	// Evictions and EvictedBytes are entries removed to stay under the size
	// limit. A pool whose evictions keep pace with its saves is too small for
	// what it keeps.
	Evictions    uint64 `json:"evictions"`
	EvictedBytes uint64 `json:"evicted_bytes"`
	// Entries and StoredBytes are the finished entries held now.
	Entries     int64 `json:"entries"`
	StoredBytes int64 `json:"stored_bytes"`
}

// counters holds the service's running totals. They are atomics, not fields of
// the service guarded by its mutex, because two of them are bumped by request
// handlers that otherwise never take the lock, and a lookup must not wait
// behind a save just to be counted.
type counters struct {
	hits, misses                      atomic.Uint64
	started, finalised, failed, dupes atomic.Uint64
	evictions, evictedBytes           atomic.Uint64
}

// Stats returns a snapshot. The counters are read one after another rather than
// together, so a snapshot taken mid-request may show a save as started and not
// yet finished, which is true; it never shows one finished that has not started,
// because a save is counted as started first.
func (s *Service) Stats() Stats {
	out := Stats{
		RestoreHits:   s.count.hits.Load(),
		RestoreMisses: s.count.misses.Load(),
		PutsFinalised: s.count.finalised.Load(),
		PutsFailed:    s.count.failed.Load(),
		PutsDuplicate: s.count.dupes.Load(),
		Evictions:     s.count.evictions.Load(),
		EvictedBytes:  s.count.evictedBytes.Load(),
	}
	// Started is read last so that finalised and failed, read before it, can
	// never exceed it in the one snapshot.
	out.PutsStarted = s.count.started.Load()
	s.mu.Lock()
	defer s.mu.Unlock()
	out.Entries = int64(len(s.entries))
	for _, e := range s.entries {
		out.StoredBytes += e.Size
	}
	return out
}
