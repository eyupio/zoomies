package agent

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"math"

	"github.com/eyupio/zoomies/internal/backend"
	"github.com/eyupio/zoomies/internal/store"
)

// applyThrottleDirective takes the controller's throttle from a heartbeat and
// moves every runner on this host to it.
//
// A nil directive is an older controller, and it is left alone rather than
// read as "restore everything": that controller never throttled anything, and
// reconciling every adopted runner against it would be a request per runner
// for nothing. The one exception is a factor that was standing when the nil
// arrived -- a downgraded controller -- which is restored, because a throttle
// nobody will ever lift is a job slowed for ever.
func (a *Agent) applyThrottleDirective(ctx context.Context, d *ThrottleDirective) {
	a.applyResourceDirectives(ctx, d, nil)
}

// applyResourceDirectives atomically replaces the host-pressure and elastic
// decisions from one heartbeat, then reconciles each live workload once. The
// pressure factor wins when it is below one; elasticity is never allowed to
// fight the protection that keeps an overloaded host alive.
func (a *Agent) applyResourceDirectives(ctx context.Context, d *ThrottleDirective, elastic []ElasticCPUDirective) {
	factor := 1.0
	if d != nil && d.CPUFactor > 0 {
		factor = d.CPUFactor
	}
	next := make(map[string]float64, len(elastic))
	for _, directive := range elastic {
		if directive.RunnerID != "" && directive.CPUFactor > 1 {
			next[directive.RunnerID] = directive.CPUFactor
		}
	}
	a.mu.Lock()
	changed := a.cpuFactor != factor || !maps.Equal(a.elasticCPU, next)
	a.cpuFactor = factor
	a.elasticCPU = next
	a.mu.Unlock()
	if d == nil && !changed {
		return
	}
	if changed {
		level := 0
		if d != nil {
			level = d.Level
		}
		a.log.Debug("throttle directive changed", "level", level, "cpu_factor", factor)
	}
	a.applyThrottle(ctx, changed)
}

// applyThrottle brings every live runner's CPU quota to the standing factor.
//
// It is the one lever a host has left once every runner on it is busy: a
// smaller effective capacity only reaches new work, and the jobs already
// running are what is overwhelming the machine. The backend's update is
// idempotent, so a runner whose applied factor is unknown -- one adopted
// after a restart -- costs an inspect and no more when it already matches.
//
// Nothing here can fail a task or the heartbeat. A daemon that refuses --
// Podman without the endpoint, a rootless daemon that cannot set a quota --
// is logged once per runner per factor and tried again on the next beat, so
// a daemon that recovers is not waited on by anything but time.
func (a *Agent) applyThrottle(ctx context.Context, announce bool) {
	type candidate struct {
		runnerID string
		handle   backend.Handle
		updater  backend.ResourceUpdater
		res      store.Resources
		warned   bool
		factor   float64
	}
	a.mu.Lock()
	hostFactor := a.cpuFactor
	now := a.now()
	var todo []candidate
	for _, r := range a.runners {
		factor := 1.0
		if elastic := a.elasticCPU[r.runnerID]; elastic > 1 {
			factor = elastic
		}
		if hostFactor < 1 {
			factor = hostFactor
		}
		// Container-running is not GitHub-ready. Keep its existing quota
		// during a bounded grace rather than further starving registration.
		if factor < 1 && !r.createdAt.IsZero() && a.opts.BootstrapCPUGrace > 0 && now.Sub(r.createdAt) < a.opts.BootstrapCPUGrace {
			factor = 1
		}
		if r.terminal || r.hostRemoved || !r.phase.Live() || r.resources.CPUs <= 0 {
			// A runner with no CPU limit has no quota to scale; it is the
			// controller's business to say so through its host problems.
			continue
		}
		if r.appliedCPUFactor != nil && *r.appliedCPUFactor == factor {
			continue
		}
		// Serialise updates even if the grace expires while one is in
		// flight: an older response must not overwrite a newer quota.
		// The next heartbeat reconciles the standing factor.
		if r.pendingCPUFactor != nil {
			continue
		}
		b, err := a.opts.Backends.Get(r.kind)
		if err != nil {
			continue
		}
		u, ok := b.(backend.ResourceUpdater)
		if !ok {
			continue
		}
		res := r.resources
		// A restore sends the base exactly as the container was created with,
		// so a pool's own figure is never rounded up over its own limit; a
		// changed figure is rounded to hundredths, the daemon's granularity for
		// --cpus, so the quota the backend compares against is one it could
		// have written.
		if factor != 1 {
			res.CPUs = math.Round(res.CPUs*factor*100) / 100
		}
		f := factor
		r.pendingCPUFactor = &f
		todo = append(todo, candidate{
			runnerID: r.runnerID, handle: r.handle, updater: u, res: res,
			factor: factor,
			warned: r.failedCPUFactor != nil && *r.failedCPUFactor == factor,
		})
	}
	a.mu.Unlock()

	if announce {
		switch {
		case len(todo) == 0 && hostFactor < 1:
			a.log.Info("the controller throttled this host; no runner needs a quota update yet (new runners keep their allocation during startup grace)",
				"cpu_factor", hostFactor)
		case hostFactor < 1:
			a.log.Info(fmt.Sprintf("throttling %d runners to %d%% of their CPU allocation", len(todo), int(math.Round(hostFactor*100))))
		case len(todo) > 0:
			boosted := 0
			for _, c := range todo {
				if c.factor > 1 {
					boosted++
				}
			}
			if boosted > 0 {
				a.log.Info(fmt.Sprintf("adjusting elastic CPU for %d runners", boosted))
			} else {
				a.log.Info(fmt.Sprintf("restoring %d runners to their full CPU allocation", len(todo)))
			}
		}
	}

	for _, c := range todo {
		factor := c.factor
		err := c.updater.UpdateResources(ctx, c.handle, c.res)
		a.mu.Lock()
		r, ok := a.runners[c.runnerID]
		if ok {
			// Claimed above and released here whatever happened, so a refusal
			// is retried on the next beat rather than held off for ever by a
			// marker nothing clears.
			if r.pendingCPUFactor != nil && *r.pendingCPUFactor == factor {
				r.pendingCPUFactor = nil
			}
			switch {
			case err == nil:
				f := factor
				r.appliedCPUFactor = &f
				r.failedCPUFactor = nil
			case errors.Is(err, backend.ErrNotFound):
				// Finished between the beat and the update; the reconciler
				// will say so, and there is nothing left to throttle.
			default:
				f := factor
				r.failedCPUFactor = &f
			}
		}
		a.mu.Unlock()
		switch {
		case err == nil:
			a.log.Debug("runner CPU quota set", "runner", c.runnerID, "handle", c.handle, "cpus", c.res.CPUs, "cpu_factor", factor)
		case errors.Is(err, backend.ErrNotFound):
			a.log.Debug("runner was gone before its CPU quota could be changed", "runner", c.runnerID, "handle", c.handle)
		case c.warned:
			a.log.Debug("runner CPU quota still could not be changed", "runner", c.runnerID, "handle", c.handle, "error", err)
		default:
			a.log.Warn("could not change a runner's CPU quota; its job continues at its full allocation and the change will be retried on the next heartbeat",
				"runner", c.runnerID, "handle", c.handle, "cpus", c.res.CPUs, "cpu_factor", factor, "error", err)
		}
	}
}
