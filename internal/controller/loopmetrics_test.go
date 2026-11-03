package controller

import (
	"context"
	"runtime/pprof"
	"testing"
	"time"

	"github.com/eyupio/zoomies/internal/events"
)

// stepCount is how many times a step of a loop has been timed.
func stepCount(t *testing.T, c *Controller, loop, step string) uint64 {
	t.Helper()
	n, _, _ := gatherHistogram(t, c, "zoomies_loop_step_seconds", map[string]string{"loop": loop, "step": step})
	return n
}

// waitFor polls until cond holds, or fails the test. The loops under test run
// on goroutines of their own, so a test can only ask "has it happened yet".
func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// A slow pass used to be one number. Which phase of it was slow is the only
// thing that says whether to look at the database, at GitHub or at the size of
// the fleet, so every phase has its own.
func TestEveryPhaseOfASchedulingPassIsTimed(t *testing.T) {
	h := newHarness(t)

	if err := h.c.Reconcile(h.ctx); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}

	for _, step := range []string{stepHostCleanup, stepSnapshot, stepDecide, stepApply, stepFallbacks, stepDrain} {
		if got := stepCount(t, h.c, loopReconcile, step); got != 1 {
			t.Errorf("the %q phase of the pass was timed %d times, want 1", step, got)
		}
	}
}

// The views worked out for the event stream are called from seven places and
// are the largest single thing a pass does on a big fleet, so what is wanted is
// each view's own cost, whoever called.
func TestEachViewForTheEventStreamIsTimedOnItsOwn(t *testing.T) {
	h := newHarness(t)
	h.listen()

	h.c.publishDerived(h.ctx)

	for _, step := range []string{stepAll, stepStats, stepProblems, stepKennel, stepUpdates, stepTransfer, stepHosts, stepMachines} {
		if got := stepCount(t, h.c, loopDerived, step); got != 1 {
			t.Errorf("the %q view was timed %d times, want 1", step, got)
		}
	}
}

// Whether the views are being worked out at all depends on whether anyone is
// watching, and a busy controller with nobody watching is a different problem
// from one with a tab left open. Both halves have to be visible: the skip as a
// count, and the work as the timings.
func TestTheViewsAreCountedAsSkippedWhileNobodyIsWatching(t *testing.T) {
	h := newHarness(t)
	if n := h.c.Events().Subscribers(); n != 0 {
		t.Fatalf("the harness starts with %d subscribers, want 0", n)
	}

	h.c.publishDerived(h.ctx)
	h.c.publishDerived(h.ctx)

	if got, _ := gatherValue(t, h.c, "zoomies_derived_skipped_total", nil); got != 2 {
		t.Errorf("zoomies_derived_skipped_total = %v after two skipped runs, want 2", got)
	}
	if got := stepCount(t, h.c, loopDerived, stepAll); got != 0 {
		t.Errorf("the views were timed %d times with nobody watching, want 0", got)
	}

	h.listen()
	h.c.publishDerived(h.ctx)

	if got, _ := gatherValue(t, h.c, "zoomies_derived_skipped_total", nil); got != 2 {
		t.Errorf("zoomies_derived_skipped_total = %v after a run with a watcher, want it to stay 2", got)
	}
	if got := stepCount(t, h.c, loopDerived, stepAll); got != 1 {
		t.Errorf("the views were timed %d times with a watcher, want 1", got)
	}
}

func TestTheSubscriberGaugeFollowsTheEventStream(t *testing.T) {
	h := newHarness(t)

	if got, ok := gatherValue(t, h.c, "zoomies_event_subscribers", nil); !ok || got != 0 {
		t.Fatalf("zoomies_event_subscribers = %v (present %v) with nobody connected, want 0", got, ok)
	}

	ctx, cancel := context.WithCancel(h.ctx)
	h.c.Events().Subscribe(ctx, events.SubscribeOptions{})
	if got, _ := gatherValue(t, h.c, "zoomies_event_subscribers", nil); got != 1 {
		t.Errorf("zoomies_event_subscribers = %v with one client connected, want 1", got)
	}

	cancel()
	waitFor(t, "the gauge to see the client leave", func() bool {
		got, _ := gatherValue(t, h.c, "zoomies_event_subscribers", nil)
		return got == 0
	})
}

// A pass the timer starts is the controller keeping its own pace, and one a
// nudge starts is the fleet moving under it. Told apart they say which of the
// two is driving the load; together they said nothing.
func TestAPassIsCountedUnderWhatStartedIt(t *testing.T) {
	h := newHarness(t)

	for _, trigger := range []string{triggerStartup, triggerTimer, triggerNudge} {
		h.c.reconcileNow(h.ctx, trigger)
	}
	if err := h.c.Reconcile(h.ctx); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}

	for _, trigger := range []string{triggerStartup, triggerTimer, triggerNudge, triggerRequest} {
		if got, _ := gatherValue(t, h.c, "zoomies_reconcile_passes_total", map[string]string{"trigger": trigger}); got != 1 {
			t.Errorf("zoomies_reconcile_passes_total{trigger=%q} = %v, want 1", trigger, got)
		}
	}
}

// The wiring, which the test above cannot see: the loop has to pass the right
// trigger for the right case. It runs the real loop on a short interval, so the
// timer fires by itself, and asks for a pass with a nudge.
func TestTheLoopSaysWhetherATimerOrANudgeStartedAPass(t *testing.T) {
	h := newHarness(t)
	h.cfg.Scheduler.Interval = 40 * time.Millisecond

	ctx, cancel := context.WithCancel(h.ctx)
	done := make(chan struct{})
	go func() { defer close(done); h.c.reconcileLoop(ctx) }()
	t.Cleanup(func() { cancel(); <-done })

	passes := func(trigger string) float64 {
		v, _ := gatherValue(t, h.c, "zoomies_reconcile_passes_total", map[string]string{"trigger": trigger})
		return v
	}
	waitFor(t, "the startup pass", func() bool { return passes(triggerStartup) == 1 })
	waitFor(t, "a pass the timer started", func() bool { return passes(triggerTimer) >= 1 })

	h.c.Nudge()
	waitFor(t, "a pass the nudge started", func() bool { return passes(triggerNudge) >= 1 })

	if got := passes(triggerStartup); got != 1 {
		t.Errorf("%v startup passes after the loop had been running, want exactly 1", got)
	}
}

// A burst of wake-ups costs one pass, which is the design. What the count has
// to do is not lose the ones the full channel turned away, because the gap
// between nudges and the passes they started is the folding.
func TestNudgesAreCountedEvenWhenTheyShareAPass(t *testing.T) {
	h := newHarness(t)

	for range 5 {
		h.c.Nudge()
	}

	if got, _ := gatherValue(t, h.c, "zoomies_reconcile_nudges_total", nil); got != 5 {
		t.Errorf("zoomies_reconcile_nudges_total = %v after five nudges, want 5", got)
	}
	if got := len(h.c.nudges); got != 1 {
		t.Errorf("%d tokens waiting after five nudges, want the one the channel holds", got)
	}
}

// Everything on the housekeeping beat shares one tick, so a slow tick is only
// useful if it says which job it was.
func TestEveryHousekeepingJobIsTimedOnItsOwn(t *testing.T) {
	h := newHarness(t)

	// A zero housekeeping record is a controller that has just started: the
	// minute sampler and the hourly prune are both due.
	h.c.housekeep(h.ctx, &housekeeping{})

	for _, step := range []string{stepSweepTasks, stepHostHealth, stepThrottles, stepAutoAdjust, stepExpireQueued, stepExpireRunning, stepSample, stepPrune} {
		if got := stepCount(t, h.c, loopHousekeeping, step); got != 1 {
			t.Errorf("the %q job of the housekeeping tick was timed %d times, want 1", step, got)
		}
	}
}

// The two loops that run on every beat are the ones whose cost is paid whether
// or not there is any work, so each is timed whether or not it found any.
func TestTheLoopsThatRunOnEveryBeatAreTimedWhenIdle(t *testing.T) {
	h := newHarness(t)

	ctx, cancel := context.WithCancel(h.ctx)
	done := make(chan struct{}, 2)
	go func() { h.c.jobAttentionLoop(ctx); done <- struct{}{} }()
	go func() { h.c.enrichmentLoop(ctx); done <- struct{}{} }()
	t.Cleanup(func() { cancel(); <-done; <-done })

	waitFor(t, "the job attention loop to have run once", func() bool {
		return stepCount(t, h.c, loopJobAttention, stepRefresh) >= 1
	})
	waitFor(t, "the enrichment claim to have run once", func() bool {
		return stepCount(t, h.c, loopEnrichment, stepClaim) >= 1
	})
}

// A CPU profile that names only the store's query function cannot say which
// loop was asking, because most of them ask. The label is what lets it.
func TestEveryLoopRunsUnderItsOwnProfilerLabel(t *testing.T) {
	h := newHarness(t)

	got := make(chan string, 1)
	h.c.spawn("labelled-loop", h.ctx, func(ctx context.Context) {
		v, _ := pprof.Label(ctx, "loop")
		got <- v
	})

	select {
	case v := <-got:
		if v != "labelled-loop" {
			t.Errorf("the loop ran under the profiler label %q, want %q", v, "labelled-loop")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("the spawned loop never ran")
	}
}

// A request is counted by the class of its status and timed, except where the
// connection is held open for its purpose, which has no latency to speak of.
func TestARequestIsCountedByClassAndTimedUnlessItIsLongLived(t *testing.T) {
	h := newHarness(t)

	h.c.ObserveRequest("hosts", 200, 12*time.Millisecond, false)
	h.c.ObserveRequest("hosts", 404, 3*time.Millisecond, false)
	h.c.ObserveRequest("events", 200, 40*time.Minute, true)
	h.c.ObserveRequest("hosts", 799, time.Millisecond, false)

	for _, want := range []struct {
		group, status string
		n             float64
	}{{"hosts", "2xx", 1}, {"hosts", "4xx", 1}, {"events", "2xx", 1}, {"hosts", "other", 1}} {
		got, _ := gatherValue(t, h.c, "zoomies_api_requests_total", map[string]string{"group": want.group, "status": want.status})
		if got != want.n {
			t.Errorf("zoomies_api_requests_total{group=%q,status=%q} = %v, want %v", want.group, want.status, got, want.n)
		}
	}
	if n, _, _ := gatherHistogram(t, h.c, "zoomies_api_request_seconds", map[string]string{"group": "hosts"}); n != 3 {
		t.Errorf("%d timed requests in the hosts group, want 3", n)
	}
	if _, _, found := gatherHistogram(t, h.c, "zoomies_api_request_seconds", map[string]string{"group": "events"}); found {
		t.Error("a long-lived stream was timed, and a 40 minute stream would have set the group's p99 for good")
	}
}
