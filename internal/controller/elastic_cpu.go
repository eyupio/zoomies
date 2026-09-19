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
	if h == nil || !h.Usage.Fresh(now) || h.Usage.CPUPercent == nil || h.Usage.CPUHeld || h.Throttle.Active() || *h.Usage.CPUPercent >= 85 ||
		h.Usage.LoadAverage1 != nil && h.CPUs > 0 && *h.Usage.LoadAverage1 >= 2*float64(h.CPUs) ||
		h.Usage.MemoryAvailableMB != nil && *h.Usage.MemoryAvailableMB <= h.MemoryReserve() {
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

	reserve := c.elasticStartReserve(ctx, h, poolByID)
	targets := scheduler.ElasticCPUPlan(alloc.CPUs, reserve, workloads)
	supported := slices.Contains(req.Features, agent.FeatureElasticCPU)
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
