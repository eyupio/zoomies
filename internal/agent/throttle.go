package agent

import (
	"context"
	"errors"
	"fmt"
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
	factor := 1.0
	if d != nil && d.CPUFactor > 0 {
		factor = d.CPUFactor
	}
	a.mu.Lock()
	changed := a.cpuFactor != factor
	a.cpuFactor = factor
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
	}
	a.mu.Lock()
	factor := a.cpuFactor
	var todo []candidate
	for _, r := range a.runners {
		if r.terminal || r.hostRemoved || !r.phase.Live() || r.resources.CPUs <= 0 {
			// A runner with no CPU limit has no quota to scale; it is the
			// controller's business to say so through its host problems.
			continue
		}
		if r.appliedCPUFactor != nil && *r.appliedCPUFactor == factor {
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
		// Hundredths, which is the daemon's own granularity for --cpus and
		// what the pool field accepts; a longer float would be a quota that
		// never compares equal to the one written.
		res.CPUs = math.Round(res.CPUs*factor*100) / 100
		todo = append(todo, candidate{
			runnerID: r.runnerID, handle: r.handle, updater: u, res: res,
			warned: r.failedCPUFactor != nil && *r.failedCPUFactor == factor,
		})
	}
	a.mu.Unlock()

	if announce {
		switch {
		case len(todo) == 0 && factor < 1:
			a.log.Info("the controller throttled this host, but no runner here has a CPU limit to reduce; running jobs continue at full speed until the effective capacity takes effect",
				"cpu_factor", factor)
		case factor < 1:
			a.log.Info(fmt.Sprintf("throttling %d runners to %d%% of their CPU allocation", len(todo), int(math.Round(factor*100))))
		case len(todo) > 0:
			a.log.Info(fmt.Sprintf("restoring %d runners to their full CPU allocation", len(todo)))
		}
	}

	for _, c := range todo {
		err := c.updater.UpdateResources(ctx, c.handle, c.res)
		a.mu.Lock()
		r, ok := a.runners[c.runnerID]
		if ok {
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
