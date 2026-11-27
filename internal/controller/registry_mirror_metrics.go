package controller

import (
	"github.com/prometheus/client_golang/prometheus"

	"github.com/eyupio/zoomies/internal/store"
)

// collectRegistryMirrors writes the registry mirror series, one set for each host
// whose agent says its mirror is on.
//
// The counters are the agent's own running totals, so the host is a label and not
// something to sum away: when an agent restarts its totals start again at zero,
// and only a series of its own lets rate() see that as a reset. Nothing an agent
// says is allowed to be below zero, because a counter that falls reads as a
// restart and takes the host's history with it. A host that has been deleted is
// not reported, and the totals are held in memory for the scrape and nowhere else,
// so a controller restart leaves them absent until each host's next heartbeat.
func (f *fleetCollector) collectRegistryMirrors(hosts []*store.Host, gauge, counter func(*prometheus.Desc, float64, ...string)) {
	live := make(map[string]bool, len(hosts))
	for _, h := range hosts {
		live[h.ID] = true
	}
	reports := f.c.registryMirrorReportsFor(live)
	for _, h := range hosts {
		r, ok := reports[h.ID]
		if !ok {
			continue
		}
		// Every result is emitted, zeroes included, so a rate has a series to
		// start from before the first miss.
		counter(descMirrorLookups, nonNegative(r.Hits), h.ID, "hit")
		counter(descMirrorLookups, nonNegative(r.Misses), h.ID, "miss")
		counter(descMirrorServedBytes, nonNegative(r.BytesServed), h.ID)
		counter(descMirrorFetchedBytes, nonNegative(r.BytesFetched), h.ID)
		counter(descMirrorUpstreamErrors, nonNegative(r.UpstreamErrors), h.ID)
		counter(descMirrorEvictions, nonNegative(r.Evictions), h.ID)
		gauge(descMirrorStoredBytes, nonNegative(r.BytesStored), h.ID)
		gauge(descMirrorUpstreamFailing, nonNegative(r.UpstreamFailingSeconds), h.ID)
	}
}

func nonNegative(n int64) float64 { return float64(max(n, 0)) }
