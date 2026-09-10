package controller

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/eyupio/zoomies/internal/agent"
	"github.com/eyupio/zoomies/internal/store"
)

// The housekeeping cadences. They share one goroutine and one ticker: none of
// this work is urgent, and three timers would be three things to reason about
// at shutdown instead of one.
const (
	housekeepingTick = 30 * time.Second
	sampleInterval   = time.Minute
	pruneInterval    = time.Hour
)

// backgroundLoop runs the periodic work: the fleet sampler behind the
// Overview's sparklines, retention pruning, credential expiry, the image
// refresh that keeps a moving tag current on the hosts, the release check
// behind the update notice, and the sweep that re-offers tasks whose agent
// never answered.
func (c *Controller) backgroundLoop(ctx context.Context) {
	ticker := time.NewTicker(housekeepingTick)
	defer ticker.Stop()

	var lastSample, lastPrune, lastImageRefresh, lastUpdateCheck time.Time
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}

		now := c.Now()
		c.sweepTasks(ctx, now)
		c.checkHostHealth(ctx)
		c.expireStaleQueuedJobs(ctx, now)
		// A host going quiet is a problem and a capacity change, and neither
		// waits for the next reconcile pass to be told about.
		c.publishDerived(ctx)

		if now.Sub(lastSample) >= sampleInterval {
			lastSample = now
			if err := c.sample(ctx); err != nil {
				c.log.Warn("could not record a fleet sample", "error", err)
			}
		}
		if now.Sub(lastPrune) >= pruneInterval {
			lastPrune = now
			c.prune(ctx)
		}
		// The zero time is deliberately allowed to fire on the first tick: a
		// controller that has just come back up has no idea what moved while it
		// was down, and the pass is idempotent.
		if d := c.cfg().Images.RefreshInterval; d > 0 && now.Sub(lastImageRefresh) >= d {
			lastImageRefresh = now
			c.refreshPoolImages(ctx)
		}
		if d := c.cfg().Updates.CheckInterval; d > 0 && now.Sub(lastUpdateCheck) >= d {
			lastUpdateCheck = now
			c.checkForRelease(ctx)
		}
	}
}

// refreshPoolImages prewarms every pool's image again on the hosts that can run
// it, so that a pool naming a moving tag picks up a rebuilt image without
// anyone touching the pool.
//
// Prewarming is otherwise only triggered by a pool being created, edited or
// prewarmed by hand, which means a moving image -- :dev on each main build or
// :latest on each full release -- would reach a host once and never again.
//
// Nothing here can affect scheduling: PrewarmPool records its outcome per host
// and queues an idempotent task, and a host that cannot reach the registry
// fails that task alone. The runners it is already running are untouched, and
// so is the image any of them was created from; a container keeps the image it
// started with until it is replaced.
func (c *Controller) refreshPoolImages(ctx context.Context) {
	pools, err := c.st.ListPools(ctx)
	if err != nil {
		c.log.Warn("could not list pools to refresh their images", "error", err)
		return
	}
	var refreshed, hosts int
	for _, p := range pools {
		n, err := c.PrewarmPool(ctx, p)
		switch {
		case errors.Is(err, ErrPrewarmUnsupported):
			// A process-backend pool has no image to pull; its runners fetch
			// the actions/runner archive themselves.
			continue
		case err != nil:
			c.log.Warn("could not refresh a pool's image", "pool", p.ID, "error", err)
			continue
		}
		if n > 0 {
			refreshed++
			hosts += n
		}
	}
	if refreshed > 0 {
		c.log.Debug("queued an image refresh", "pools", refreshed, "hosts", hosts)
	}
}

// sweepTasks re-offers tasks whose lease expired without a result.
//
// Delivery is at-least-once and every task is idempotent, so a redelivery
// costs the agent a no-op. The alternative -- assuming one delivery is enough
// -- means a runner that was never created and nothing to say why.
func (c *Controller) sweepTasks(ctx context.Context, now time.Time) {
	for hostID, q := range c.queues.all() {
		requeued, dropped := q.sweep(now)
		if requeued > 0 {
			c.log.Warn("re-queued tasks a host never reported on",
				"host", hostID, "tasks", requeued)
		}
		if len(dropped) == 0 {
			continue
		}
		c.log.Error("gave up redelivering tasks to a host",
			"host", hostID, "tasks", len(dropped), "attempts", maxTaskAttempts)
		for _, task := range dropped {
			// A dropped create is noticed by the runner's provision timeout,
			// which says so on the Runners page. A dropped stop is noticed by
			// nothing: the runner would sit in draining for ever, counted
			// against its pool's maximum and holding a slot on its host, so
			// it is failed here and the next pass frees the slot.
			if task.Kind != agent.TaskStopRunner {
				continue
			}
			reason := fmt.Sprintf("the host never confirmed stopping this runner after %d deliveries; it is presumed gone", maxTaskAttempts)
			if err := c.failRunnerID(ctx, task.RunnerID, reason); err != nil &&
				!errors.Is(err, store.ErrNotFound) && !errors.Is(err, store.ErrInvalidTransition) {
				c.log.Warn("could not fail a runner whose stop was never confirmed", "runner", task.RunnerID, "error", err)
			}
		}
	}
}

// sample writes one point for the Overview's sparklines. RecordSample keys on
// the minute, so a restart mid-minute overwrites rather than double-counts.
func (c *Controller) sample(ctx context.Context) error {
	counts, err := c.st.CountRunnersByPool(ctx)
	if err != nil {
		return err
	}
	var s store.FleetSample
	s.At = c.Now()
	for _, pc := range counts {
		s.IdleRunners += pc.Idle
		s.BusyRunners += pc.Busy
		s.TotalRunners += pc.Live()
	}
	stats, err := c.st.StatsSince(ctx, c.Now().Add(-time.Hour), false)
	if err != nil {
		return err
	}
	s.QueuedJobs, s.RunningJobs = stats.Queued, stats.Running
	// The fleet's own figures are recorded alongside, so the Overview's
	// sparkline can follow its "other runners" toggle rather than making the
	// line disagree with the tile above it.
	fleet, err := c.st.StatsSince(ctx, c.Now().Add(-time.Hour), true)
	if err != nil {
		return err
	}
	s.FleetQueuedJobs, s.FleetRunningJobs = fleet.Queued, fleet.Running
	return c.st.RecordSample(ctx, s)
}

// prune enforces the retention windows and expires credentials.
//
// Every deletion is logged at debug with a count, because "where did my job
// history go?" should be answerable from the log rather than from reading this
// function.
func (c *Controller) prune(ctx context.Context) {
	now := c.Now()
	r := c.cfg().Retention

	type job struct {
		what   string
		window time.Duration
		fn     func(context.Context, time.Time) (int64, error)
	}
	for _, j := range []job{
		{"jobs", r.Jobs, c.st.PruneJobs},
		{"runners", r.Runners, func(ctx context.Context, before time.Time) (int64, error) {
			// Each pruned row is announced, or the Runners page keeps showing
			// runners that no longer exist until it is reloaded.
			ids, err := c.st.PruneRunners(ctx, before)
			c.publishRunnersDeleted(ids)
			return int64(len(ids)), err
		}},
		{"fleet samples", r.Samples, c.st.PruneSamples},
		{"webhook deliveries", r.Webhooks, c.st.PruneDeliveries},
		// Scaling history is decision history, so it follows the audit window.
		// The audit rows themselves have no prune: an audit trail a process can
		// quietly delete is not one, so store deliberately offers no way.
		{"scaling events", r.Audit, c.st.PruneScalingEvents},
	} {
		// A zero or negative window means "keep everything", which is what an
		// operator who cleared the setting meant.
		if j.window <= 0 {
			continue
		}
		n, err := j.fn(ctx, now.Add(-j.window))
		if err != nil {
			c.log.Warn("could not prune history", "what", j.what, "error", err)
			continue
		}
		if n > 0 {
			c.log.Debug("pruned history", "what", j.what, "rows", n, "older_than", j.window)
		}
	}

	if n, err := c.st.PruneSessions(ctx, now); err != nil {
		c.log.Warn("could not prune expired sessions", "error", err)
	} else if n > 0 {
		c.log.Debug("pruned expired sessions", "rows", n)
	}
	if n, err := c.st.PruneJoinTokens(ctx, now); err != nil {
		c.log.Warn("could not prune expired join tokens", "error", err)
	} else if n > 0 {
		c.log.Debug("pruned expired join tokens", "rows", n)
	}
}
