package controller

import (
	"maps"
	"slices"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/eyupio/zoomies/internal/backend"
	"github.com/eyupio/zoomies/internal/store"
)

// The cache services on a host count what they do, and the agent sends the
// totals with every heartbeat. The controller keeps the latest of them in memory
// and the metrics endpoint turns them into series at scrape time. Nothing here
// decides anything: it is how an operator learns whether a pool's cache is being
// used, and what it costs, before any rule about it exists.

// noteCaches records what a host's agent said its cache services have done. A
// beat that says nothing clears the host: an agent that has not started a
// service yet, and one restarted since it did, both have nothing to report, and
// holding the old totals would show a cache that no longer exists.
func (c *Controller) noteCaches(hostID string, caches []backend.CacheStats) {
	c.cacheMu.Lock()
	defer c.cacheMu.Unlock()
	if len(caches) == 0 {
		delete(c.cacheHosts, hostID)
		return
	}
	if c.cacheHosts == nil {
		c.cacheHosts = map[string][]backend.CacheStats{}
	}
	c.cacheHosts[hostID] = slices.Clone(caches)
}

// cacheReportsFor is the latest report of each host in live, forgetting every
// other host: a deleted host would otherwise stand in the map for the life of
// the process.
func (c *Controller) cacheReportsFor(live map[string]bool) map[string][]backend.CacheStats {
	c.cacheMu.Lock()
	defer c.cacheMu.Unlock()
	for id := range c.cacheHosts {
		if !live[id] {
			delete(c.cacheHosts, id)
		}
	}
	return maps.Clone(c.cacheHosts)
}

// collectCaches writes the cache series, one set for each host and pool that has
// reported.
//
// The counters are the agent's own running totals, so the host is a label and
// not something to sum away: when an agent restarts its totals start again at
// zero, and only a series of its own lets rate() see that as a reset rather than
// as a pool whose counters went backwards.
//
// A report for a pool this controller no longer has is not exported. The pool
// label is the pool's name, and an agent is not trusted to mint one, which
// keeps the series bounded by the pools and hosts there are. Two pools that
// share a name are summed into one series, because a scrape that emitted the
// same series twice would fail whole.
func (f *fleetCollector) collectCaches(hosts []*store.Host, pools []*store.Pool,
	gauge, counter func(*prometheus.Desc, float64, ...string)) {
	live := make(map[string]bool, len(hosts))
	for _, h := range hosts {
		live[h.ID] = true
	}
	reports := f.c.cacheReportsFor(live)
	if len(reports) == 0 {
		return
	}
	names := make(map[string]string, len(pools))
	for _, p := range pools {
		names[p.ID] = p.Name
	}
	for _, h := range hosts {
		byPool := map[string]*backend.CacheStats{}
		for _, r := range reports[h.ID] {
			name, ok := names[r.PoolID]
			if !ok {
				continue
			}
			sum := byPool[name]
			if sum == nil {
				sum = &backend.CacheStats{PoolID: name}
				byPool[name] = sum
			}
			sum.RestoreHits += r.RestoreHits
			sum.RestoreMisses += r.RestoreMisses
			sum.PutsStarted += r.PutsStarted
			sum.PutsFinalised += r.PutsFinalised
			sum.PutsFailed += r.PutsFailed
			sum.PutsDuplicate += r.PutsDuplicate
			sum.Evictions += r.Evictions
			sum.StoredBytes += r.StoredBytes
		}
		for _, name := range slices.Sorted(maps.Keys(byPool)) {
			s := byPool[name]
			// Every result is emitted, zeroes included, so a rate has a series
			// to start from before the first miss or the first refusal.
			counter(descCacheRestores, float64(s.RestoreHits), name, h.ID, "hit")
			counter(descCacheRestores, float64(s.RestoreMisses), name, h.ID, "miss")
			counter(descCachePuts, float64(s.PutsStarted), name, h.ID, "started")
			counter(descCachePuts, float64(s.PutsFinalised), name, h.ID, "finalised")
			counter(descCachePuts, float64(s.PutsDuplicate), name, h.ID, "duplicate")
			counter(descCachePuts, float64(s.PutsFailed), name, h.ID, "failed")
			counter(descCacheEvictions, float64(s.Evictions), name, h.ID)
			gauge(descCacheStoredBytes, float64(max(s.StoredBytes, 0)), name, h.ID)
		}
	}
}
