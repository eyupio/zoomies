package controller

import (
	"context"
	"fmt"
	"slices"
	"time"

	"github.com/eyupio/zoomies/internal/analysis"
	"github.com/eyupio/zoomies/internal/scheduler"
	"github.com/eyupio/zoomies/internal/store"
)

const (
	// performanceWindow is how far back the load and the waiting are read: a day,
	// which holds a working day's shape without being one afternoon's.
	performanceWindow = 24 * time.Hour
	// performanceJobsWindow is how far back job peaks are read. A week, because a
	// memory figure is sized from the heaviest ordinary run, and one day of jobs
	// is too few of a pool that runs a job once a day.
	performanceJobsWindow = 7 * 24 * time.Hour
)

// PerformanceView is the recommendation, with what it was read from.
type PerformanceView struct {
	scheduler.PerformanceRecommendation
	// PoolID is the pool it was asked about, empty for a pool not yet made.
	PoolID string `json:"pool_id,omitempty"`
	// Since and Until are the load and wait window; JobsSince where the job
	// peaks start.
	Since     time.Time `json:"since"`
	Until     time.Time `json:"until"`
	JobsSince time.Time `json:"jobs_since"`
}

// PoolPerformance says whether the runner size could be smaller to give the
// hosts more slots, for one pool or, with p nil, for a pool about to be made
// that will be sized by its hosts. It reads only: nothing is changed, and the
// change itself is the operator's, made through the settings and the pool.
func (c *Controller) PoolPerformance(ctx context.Context, p *store.Pool) (*PerformanceView, error) {
	now := c.Now()
	since, jobsSince := now.Add(-performanceWindow), now.Add(-performanceJobsWindow)
	poolID := ""
	if p != nil {
		poolID = p.ID
	}
	pools, err := c.fleetPools(ctx)
	if err != nil {
		return nil, fmt.Errorf("listing pools: %w", err)
	}
	hosts, err := c.performanceHosts(ctx, p, now)
	if err != nil {
		return nil, err
	}
	fleet := c.cfg().Runners
	ev := scheduler.PerformanceEvidence{
		Ask:         scheduler.AskOf(enabledPools(pools), fleet.DefaultCPUs, int64(fleet.DefaultMemoryMB)),
		MinimumCPUs: fleet.MinimumCPUs, MinimumMemoryMB: int64(fleet.MinimumMemoryMB),
	}
	if p != nil {
		ev.PoolMax = p.MaxRunners
	}
	for _, other := range enabledPools(pools) {
		if (p == nil || other.ID != p.ID) && other.Resources.CPUs == 0 && other.Resources.MemoryMB == 0 {
			ev.Followers = append(ev.Followers, other.Name)
		}
	}
	for _, h := range hosts {
		ph, err := c.performanceHost(ctx, h, since)
		if err != nil {
			return nil, err
		}
		ev.Hosts = append(ev.Hosts, ph)
	}

	finding, err := c.AnalyseQueueWait(ctx, AnalysisWindow{Since: since, Until: now}, poolID)
	if err != nil {
		return nil, fmt.Errorf("reading what jobs waited for: %w", err)
	}
	ev.WaitSeconds = float64(finding.Evidence.WaitSeconds)
	for _, cs := range finding.Evidence.Fleet {
		// Both are time with no room for another runner: the pool at its maximum
		// and every host full.
		if cs.Cause == analysis.CausePoolCeiling || cs.Cause == analysis.CauseNoRoom {
			ev.CeilingSeconds += float64(cs.Seconds)
		}
	}

	peaks, err := c.st.PoolJobPeaks(ctx, poolID, jobsSince)
	if err != nil {
		return nil, fmt.Errorf("reading what jobs used: %w", err)
	}
	for _, pk := range peaks {
		if pk.OOMKilled {
			ev.OOMKilled++
		}
		if pk.MemoryMB > 0 {
			ev.PeakMemoryMB = append(ev.PeakMemoryMB, pk.MemoryMB)
		}
	}
	return &PerformanceView{
		PerformanceRecommendation: scheduler.RecommendPerformance(ev),
		PoolID:                    poolID, Since: since, Until: now, JobsSince: jobsSince,
	}, nil
}

// performanceHosts are the machines the recommendation is about: the ones the
// pool can be placed on, or every machine present for a pool not yet made.
func (c *Controller) performanceHosts(ctx context.Context, p *store.Pool, now time.Time) ([]*store.Host, error) {
	if p == nil {
		all, err := c.fleetHosts(ctx)
		if err != nil {
			return nil, fmt.Errorf("listing hosts: %w", err)
		}
		var out []*store.Host
		for _, h := range all {
			if scheduler.HostPresent(h, now) && !h.Retired && !h.Embedded {
				out = append(out, h)
			}
		}
		return out, nil
	}
	room, err := c.poolRoom(ctx, p)
	if err != nil {
		return nil, fmt.Errorf("counting the room the pool has: %w", err)
	}
	var out []*store.Host
	for _, r := range room.Placeable() {
		h, err := c.st.GetHost(ctx, r.HostID)
		if err != nil {
			return nil, fmt.Errorf("reading host %s: %w", r.HostID, err)
		}
		out = append(out, h)
	}
	return out, nil
}

// performanceHost is a host with what its runners were measured to use over the
// window: the cores in use divided by the runners at work, in each minute with
// at least one at work and a CPU figure.
func (c *Controller) performanceHost(ctx context.Context, h *store.Host, since time.Time) (scheduler.PerformanceHost, error) {
	samples, err := c.st.ListHostSamples(ctx, since, h.ID)
	if err != nil {
		return scheduler.PerformanceHost{}, fmt.Errorf("reading host %s's samples: %w", h.Name, err)
	}
	out := scheduler.PerformanceHost{Host: h}
	sum := 0.0
	for _, s := range samples {
		if s.ActiveRunners <= 0 || s.CPUPercent == nil || s.CPUs <= 0 {
			continue
		}
		sum += *s.CPUPercent / 100 * float64(s.CPUs) / float64(s.ActiveRunners)
		out.Samples++
	}
	if out.Samples > 0 {
		out.CPUPerRunner = sum / float64(out.Samples)
	}
	return out, nil
}

func enabledPools(pools []*store.Pool) []*store.Pool {
	return slices.DeleteFunc(slices.Clone(pools), func(p *store.Pool) bool { return !p.Enabled })
}
