package controller

import (
	"github.com/eyupio/zoomies/internal/scheduler"
	"github.com/eyupio/zoomies/internal/store"
	"time"
)

type placementIntent struct {
	At      time.Time
	Version uint64
	Starts  map[string]map[string]int
}

// Publish immutable admission intent. Boosts can reserve the selected hosts
// rather than every host compatible with the same queued job. Unknown or stale
// intent still uses the conservative queue-based fallback.
func (c *Controller) rememberPlacement(s scheduler.Snapshot, p scheduler.Plan, version uint64) {
	v := &placementIntent{At: s.Now, Version: version, Starts: map[string]map[string]int{}}
	add := func(host, pool string) {
		if v.Starts[host] == nil {
			v.Starts[host] = map[string]int{}
		}
		v.Starts[host][pool]++
	}
	for pool, rs := range s.Runners {
		for _, r := range rs {
			if r.State == store.RunnerProvisioning || r.State == store.RunnerRegistering {
				add(r.HostID, pool)
			}
		}
	}
	for _, pp := range p.Pools {
		for _, a := range pp.Actions {
			if a.Kind == scheduler.ActionCreate {
				add(a.HostID, pp.PoolID)
			}
		}
	}
	c.placement.Store(v)
}
