package controller

import "time"

// The names the loop timings are labelled with.
//
// They are constants, and the only values the "loop" and "step" labels ever
// take, so the series stay a fixed set however busy the fleet is. A job, a
// host or a repository must never become one of them: that is what the label
// test in metrics_test.go is there to refuse.
const (
	loopReconcile    = "reconcile"
	loopHousekeeping = "housekeeping"
	loopDerived      = "derived"
	loopJobAttention = "job_attention"
	loopEnrichment   = "enrichment"
)

// The steps of those loops. A step is a stretch of one loop that does work on
// its own and can be slow on its own, which is the only reason to time it.
const (
	// The scheduling pass, in the order it runs them.
	stepHostCleanup = "host_cleanup"
	stepSnapshot    = "snapshot"
	stepDecide      = "decide"
	stepApply       = "apply"
	stepFallbacks   = "fallbacks"
	stepDrain       = "drain"

	// The housekeeping tick.
	stepSweepTasks    = "sweep_tasks"
	stepHostHealth    = "host_health"
	stepThrottles     = "throttles"
	stepAutoAdjust    = "auto_adjust"
	stepExpireQueued  = "expire_queued"
	stepExpireRunning = "expire_running"
	stepExpireMerges  = "expire_merges"
	stepSample        = "sample"
	stepMachine       = "machine"
	stepPrune         = "prune"
	stepImageRefresh  = "image_refresh"
	stepUpdateCheck   = "update_check"
	stepToolchainScan = "toolchain_scan"

	// What is computed for the event stream.
	stepAll      = "all"
	stepStats    = "stats"
	stepProblems = "problems"
	stepKennel   = "kennel"
	stepUpdates  = "updates"
	stepTransfer = "transfer"
	stepHosts    = "hosts"
	stepMachines = "machines"
	// stepControllerMachine is the controller's own machine, which is not one of
	// the provider machines above.
	stepControllerMachine = "controller_machine"

	// The two loops that run a step on every beat.
	stepRefresh = "refresh"
	stepClaim   = "claim"
	stepWork    = "work"
)

// What started a scheduling pass. A pass the timer starts is the controller
// keeping its own pace; a pass a nudge starts is the fleet changing under it;
// and a burst of nudges that costs one pass is the coalescing working, which
// is the ratio of the two counters.
const (
	triggerStartup = "startup"
	triggerTimer   = "timer"
	triggerNudge   = "nudge"
	// triggerRequest is a pass somebody asked for by calling Reconcile.
	triggerRequest = "request"
)

// stepTimer starts timing one step of a loop and returns the function that
// records how long it took, so a step is timed with a pair of lines:
//
//	done := c.stepTimer(loopReconcile, stepSnapshot)
//	snap, err := c.snapshot(ctx)
//	done()
//
// It reads the wall clock and not the controller's own. These are the lengths
// of real work, and a test that moves the controller's clock to expire a lease
// must not make a step look as if it took an hour.
func (c *Controller) stepTimer(loop, step string) func() {
	started := time.Now()
	return func() {
		c.metrics.loopStep.WithLabelValues(loop, step).Observe(time.Since(started).Seconds())
	}
}

// ObserveRequest records one request the API served: always counted, by route
// group and status class, and timed unless the route holds its connection open
// for its purpose.
//
// The group and the class are what keep this bounded. The group comes from the
// route that matched and never from the path that was asked for, so a scanner
// walking made-up paths adds nothing, and the class is one of five, so a
// status code a handler invents adds nothing either.
func (c *Controller) ObserveRequest(group string, status int, took time.Duration, longLived bool) {
	c.metrics.apiRequests.WithLabelValues(group, statusClass(status)).Inc()
	if !longLived {
		c.metrics.apiDuration.WithLabelValues(group).Observe(took.Seconds())
	}
}

// statusClasses are the five classes HTTP has, written out so that naming one
// allocates nothing: this runs for every request the controller serves.
var statusClasses = [...]string{"1xx", "2xx", "3xx", "4xx", "5xx"}

// statusClass is "2xx" for 200 to 299 and so on, and "other" for a number HTTP
// has no class for, which only a bug would write.
func statusClass(code int) string {
	if code < 100 || code > 599 {
		return "other"
	}
	return statusClasses[code/100-1]
}
