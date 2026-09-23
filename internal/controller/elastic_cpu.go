package controller

import (
	"context"
	"encoding/json"
	"math"
	"slices"
	"time"

	"github.com/eyupio/zoomies/internal/agent"
	"github.com/eyupio/zoomies/internal/backend"
	"github.com/eyupio/zoomies/internal/scheduler"
	"github.com/eyupio/zoomies/internal/store"
)

const (
	elasticDemandPercent = 80.0
	elasticHoldPercent   = 60.0
)

// elasticCPUTargets makes one host-wide decision from one coherent heartbeat.
// Every live runner is charged its guarantee before any spare CPU is lent, and
// one compatible queued start is protected so a fast job cannot starve the
// next job out of the machine.
func (c *Controller) elasticCPUTargets(ctx context.Context, h *store.Host, req agent.HeartbeatRequest, now time.Time) []agent.ElasticCPUDirective {
	// Nothing measured is no decision at all: there is nothing to judge the
	// host by, so nothing is recorded as if there were.
	if h == nil || !h.Usage.Fresh(now) || h.Usage.CPUPercent == nil {
		return nil
	}
	alloc := h.Allocatable()
	if !alloc.CPUsKnown || alloc.CPUs <= 0 {
		return nil
	}

	runners, err := c.st.ListRunnersForHost(ctx, h.ID)
	if err != nil {
		c.log.Warn("could not plan elastic CPU for a host", "host", h.ID, "error", err)
		return nil
	}
	// This runs on every heartbeat of every host, so it stops as soon as there
	// is nothing to decide: before the fleet-wide pool read when the host runs
	// nothing, and before any plan or record when nothing here is elastic.
	if !slices.ContainsFunc(runners, func(r *store.Runner) bool { return r != nil && r.State.Live() }) {
		return nil
	}
	pools, err := c.st.ListPools(ctx)
	if err != nil {
		c.log.Warn("could not list pools while planning elastic CPU", "host", h.ID, "error", err)
		return nil
	}
	poolByID := make(map[string]*store.Pool, len(pools))
	for _, p := range pools {
		poolByID[p.ID] = p
	}
	reports := make(map[string]agent.RunnerReport, len(req.Runners))
	for _, rep := range req.Runners {
		reports[rep.RunnerID] = rep
	}

	workloads := make([]scheduler.ElasticCPUWorkload, 0, len(runners))
	poolByRunner := make(map[string]*store.Pool, len(runners))
	lent := 0.0
	for _, r := range runners {
		if r == nil || !r.State.Live() {
			continue
		}
		p := poolByID[r.PoolID]
		if p == nil {
			continue
		}
		base := scheduler.Reserve(p, h).CPUs
		if p.Automatic() && r.AllocatedCPUs > 0 {
			// An automatic runner keeps the host share it was actually launched
			// with. Recomputing it after a host-capacity edit would corrupt both
			// the committed ledger and any factor sent to the agent.
			base = r.AllocatedCPUs
		}
		poolByRunner[r.ID] = p
		if rep, ok := reports[r.ID]; ok {
			lent += lentInUse(rep.Stats, base, now)
		}
		eligible := p.Automatic() && p.CPUBurst.Observes() && (p.Backend == store.BackendDocker || p.Backend == store.BackendPodman) && r.State == store.RunnerBusy && r.AllocatedCPUs > 0
		w := scheduler.ElasticCPUWorkload{ID: r.ID, BaseCPUs: base, MaxCPUs: base}
		if eligible && base > 0 {
			w.MaxCPUs = p.CPUBurst.MaxCPUs
			if w.MaxCPUs <= 0 || w.MaxCPUs > alloc.CPUs {
				w.MaxCPUs = alloc.CPUs
			}
			if rep, ok := reports[r.ID]; ok {
				w.Demanding = elasticCPUDemanding(r, rep.Stats, base, now)
			}
		}
		workloads = append(workloads, w)
	}

	elastic, demanding := false, false
	for _, w := range workloads {
		if p := poolByRunner[w.ID]; p != nil && p.CPUBurst.Observes() && w.BaseCPUs > 0 && w.MaxCPUs > w.BaseCPUs {
			elastic = true
			demanding = demanding || w.Demanding
		}
	}
	if !elastic {
		return nil
	}

	supported := slices.Contains(req.Features, agent.FeatureElasticCPU)
	if elasticHostBusy(h, lent) {
		// A host too busy to lend is still a decision, and it is recorded as
		// one. Observe mode exists so an operator can read how often a boost
		// would happen before switching one on; a busy heartbeat that left no
		// trace would count only the calm ones, and overstate the answer.
		for _, w := range workloads {
			p := poolByRunner[w.ID]
			if p == nil || !p.CPUBurst.Observes() || w.BaseCPUs <= 0 || w.MaxCPUs <= w.BaseCPUs {
				continue
			}
			c.metrics.elasticCPUDecisions.WithLabelValues(p.Name, string(p.CPUBurst.Mode), "host_busy").Inc()
			c.metrics.elasticCPUFactor.WithLabelValues(p.Name, string(p.CPUBurst.Mode)).Observe(1)
		}
		return nil
	}

	// The start reserve only shapes a plan that lends something. With no
	// runner demanding, every one stays at its guarantee whatever the reserve
	// is, so the fleet-wide read of queued jobs it needs is skipped.
	reserve := 0.0
	if demanding {
		reserve = c.elasticStartReserve(ctx, h, poolByID)
	}
	targets := scheduler.ElasticCPUPlan(alloc.CPUs, reserve, workloads)
	directives := make([]agent.ElasticCPUDirective, 0, len(workloads))
	for _, w := range workloads {
		p := poolByRunner[w.ID]
		if p == nil || !p.CPUBurst.Observes() || w.BaseCPUs <= 0 || w.MaxCPUs <= w.BaseCPUs {
			continue
		}
		target := targets[w.ID]
		outcome := "base"
		if target > w.BaseCPUs+0.01 {
			outcome = "burst"
		}
		if p.CPUBurst.Enforces() && !supported {
			outcome = "unsupported_agent"
		}
		c.metrics.elasticCPUDecisions.WithLabelValues(p.Name, string(p.CPUBurst.Mode), outcome).Inc()
		c.metrics.elasticCPUFactor.WithLabelValues(p.Name, string(p.CPUBurst.Mode)).Observe(target / w.BaseCPUs)
		if !p.CPUBurst.Enforces() || !supported {
			continue
		}
		if target <= w.BaseCPUs+0.01 {
			continue
		}
		directives = append(directives, agent.ElasticCPUDirective{
			RunnerID: w.ID, CPUFactor: target / w.BaseCPUs, BaseCPUs: w.BaseCPUs,
			TargetCPUs: target, Reason: "squirrel_spotted",
		})
	}
	return directives
}

// elasticHostBusy reports whether the host is too busy to lend CPU: throttled,
// high on CPU or load, or at its memory reserve.
//
// lent is the CPU this plan has already lent and the runners are using, and
// the CPU test is made without it. A boost is lent from the host's spare CPU,
// so a boost being used is a host whose CPU goes up -- and judged on the raw
// figure, a boost that did its job pushed the host over the line, the next
// heartbeat withdrew it, the host fell quiet, and the one after lent it again.
// Every other heartbeat, for as long as the job ran: a CPU-bound runner lent
// twice its guarantee averaged half as much again, and paid a quota change
// each time. What the line is for is CPU the plan did not account for --
// outside work, a runner with no limit, the daemon -- and that is what is
// left once the lent CPU in use is taken out.
//
// The same is why the sustained-CPU hold is not consulted here: it is decided
// on the raw figure, and a host held only because of what it was lent is the
// same flap a heartbeat later.
func elasticHostBusy(h *store.Host, lent float64) bool {
	cpu := *h.Usage.CPUPercent
	if h.CPUs > 0 {
		cpu = max(cpu-lent/float64(h.CPUs)*100, 0)
	}
	return h.Throttle.Active() || cpu >= 85 ||
		h.Usage.LoadAverage1 != nil && h.CPUs > 0 && *h.Usage.LoadAverage1 >= 2*float64(h.CPUs) ||
		h.Usage.MemoryAvailableMB != nil && *h.Usage.MemoryAvailableMB <= h.MemoryReserve()
}

// lentInUse is how much CPU a runner is using beyond its guarantee because it
// was lent it: its use above base, and never more than the boost it was given.
// A runner at or below its creation quota contributes nothing, so a runner
// with no limit that is using more than its share is left counted as the load
// it is, not written off as ours.
func lentInUse(st backend.Stats, base float64, now time.Time) float64 {
	if base <= 0 || st.CPUAllocationFactor <= 1 || st.SampledAt == nil || now.Sub(*st.SampledAt) > store.HostUsageMaxAge {
		return 0
	}
	used := st.CPUPercent / 100
	return min(max(used-base, 0), (st.CPUAllocationFactor-1)*base)
}

func elasticCPUDemanding(r *store.Runner, current backend.Stats, base float64, now time.Time) bool {
	if current.SampledAt == nil || now.Sub(*current.SampledAt) > store.HostUsageMaxAge {
		return false
	}
	if current.CPUPercent >= base*elasticDemandPercent {
		return true
	}
	var previous backend.Stats
	if len(r.ResourceSample) == 0 || json.Unmarshal(r.ResourceSample, &previous) != nil {
		return false
	}
	// Once lent CPU is doing useful work, keep it until usage falls below a
	// lower threshold. Separate enter/leave thresholds stop quota flapping on
	// jobs that hover around the demand boundary.
	if previous.CPUAllocationFactor > 1 && current.CPUPercent >= base*elasticHoldPercent {
		return true
	}
	if previous.CPUThrottling == nil || current.CPUThrottling == nil {
		return false
	}
	return current.CPUThrottling.ThrottledPeriods > previous.CPUThrottling.ThrottledPeriods ||
		current.CPUThrottling.ThrottledNanoseconds > previous.CPUThrottling.ThrottledNanoseconds
}

func (c *Controller) elasticStartReserve(ctx context.Context, h *store.Host, pools map[string]*store.Pool) float64 {
	queued, err := c.st.ListQueuedJobs(ctx)
	if err != nil {
		return 0
	}
	reserve := 0.0
	for _, job := range queued {
		p := pools[job.PoolID]
		if p == nil || !scheduler.HostCouldRun(h, p) {
			continue
		}
		reserve = math.Max(reserve, scheduler.Reserve(p, h).CPUs)
	}
	return reserve
}
