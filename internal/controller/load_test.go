package controller

import (
	"encoding/json"
	"fmt"
	"testing"
	"time"
)

func stepOf(t *testing.T, v ControllerLoadView, loop, step string) StepLoadView {
	t.Helper()
	for _, s := range v.Steps {
		if s.Loop == loop && s.Step == step {
			return s
		}
	}
	t.Fatalf("no %s/%s step in %+v", loop, step, v.Steps)
	return StepLoadView{}
}

func groupOf(t *testing.T, v ControllerLoadView, name string) RouteGroupLoadView {
	t.Helper()
	for _, g := range v.API {
		if g.Group == name {
			return g
		}
	}
	t.Fatalf("no %q route group in %+v", name, v.API)
	return RouteGroupLoadView{}
}

// The view exists to answer "which part of the controller is taking its time", so
// the longest comes first and each step says how often it ran and how long it
// took in all.
func TestTheLoadViewListsTheLongestStepsFirstWithTheirCountAndTime(t *testing.T) {
	h := newHarness(t)
	snapshot := h.c.metrics.loopStep.WithLabelValues(loopReconcile, stepSnapshot)
	snapshot.Observe(2)
	snapshot.Observe(0.5)
	sample := h.c.metrics.loopStep.WithLabelValues(loopHousekeeping, stepSample)
	for i := 0; i < 3; i++ {
		sample.Observe(0.01)
	}

	v := h.c.ControllerLoad()
	if len(v.Steps) < 2 || v.Steps[0].Loop != loopReconcile || v.Steps[0].Step != stepSnapshot {
		t.Fatalf("steps = %+v, want the step with the most time first", v.Steps)
	}
	s := stepOf(t, v, loopReconcile, stepSnapshot)
	if s.Count != 2 || s.Seconds != 2.5 || s.MeanSeconds != 1.25 {
		t.Errorf("snapshot = %d runs, %v s, mean %v, want 2 runs of 2.5 s in all, mean 1.25", s.Count, s.Seconds, s.MeanSeconds)
	}
	if got := stepOf(t, v, loopHousekeeping, stepSample); got.Count != 3 || got.Seconds != 0.03 {
		t.Errorf("sample = %d runs, %v s, want 3 runs and 0.03 s", got.Count, got.Seconds)
	}
}

// A mean hides the one run that took all the time, which is the shape of a CPU
// spike. The slowest bucket's edge is the nearest thing the histogram keeps to a
// maximum, and it is only an upper bound, so it is named as one.
func TestTheSlowestRunOfAStepIsBoundedByTheTimingBucketItFellIn(t *testing.T) {
	h := newHarness(t)
	fast := h.c.metrics.loopStep.WithLabelValues(loopReconcile, stepDecide)
	fast.Observe(0.002)
	fast.Observe(0.004)
	fast.Observe(2) // lands in the bucket whose edge is 2.5 seconds
	v := h.c.ControllerLoad()
	s := stepOf(t, v, loopReconcile, stepDecide)
	if s.SlowestAtMostSeconds == nil || *s.SlowestAtMostSeconds != 2.5 || s.SlowestOverSeconds != nil {
		t.Fatalf("slowest = at most %v, over %v, want at most 2.5", s.SlowestAtMostSeconds, s.SlowestOverSeconds)
	}
}

func TestARunSlowerThanTheLastBucketSaysHowFarTheBucketsReachAndNotAnEdgeItDoesNotHave(t *testing.T) {
	h := newHarness(t)
	h.c.metrics.loopStep.WithLabelValues(loopReconcile, stepApply).Observe(45)
	s := stepOf(t, h.c.ControllerLoad(), loopReconcile, stepApply)
	if s.SlowestAtMostSeconds != nil || s.SlowestOverSeconds == nil || *s.SlowestOverSeconds != 30 {
		t.Fatalf("slowest = at most %v, over %v, want no upper bound and over 30", s.SlowestAtMostSeconds, s.SlowestOverSeconds)
	}
}

// The derived loop times its whole pass as well as its parts, so adding a loop's
// steps up counts the pass twice. The view marks the step that contains the
// others rather than leaving a reader to find out from a total that is too big.
func TestTheStepThatCoversALoopsWholePassIsMarkedSoItIsNotAddedToTheRest(t *testing.T) {
	h := newHarness(t)
	h.c.metrics.loopStep.WithLabelValues(loopDerived, stepAll).Observe(0.3)
	h.c.metrics.loopStep.WithLabelValues(loopDerived, stepStats).Observe(0.1)
	v := h.c.ControllerLoad()
	if !stepOf(t, v, loopDerived, stepAll).Whole {
		t.Error("the step that times the whole derived pass is not marked as containing the others")
	}
	if stepOf(t, v, loopDerived, stepStats).Whole {
		t.Error("a part of the pass is marked as the whole of it")
	}
}

// A share of the time the controller has been running is what makes a total
// comparable between a controller up for a minute and one up for a week.
func TestAStepsShareIsItsTimeOverTheTimeTheControllerHasBeenRunning(t *testing.T) {
	h := newHarness(t)
	h.advance(100 * time.Second)
	h.c.metrics.loopStep.WithLabelValues(loopReconcile, stepSnapshot).Observe(5)
	v := h.c.ControllerLoad()
	if v.UptimeSeconds < 100 {
		t.Fatalf("uptime = %v s after the clock moved 100 s", v.UptimeSeconds)
	}
	want := 100 * 5 / v.UptimeSeconds
	got := stepOf(t, v, loopReconcile, stepSnapshot).UptimeSharePercent
	if got < want-0.01 || got > want+0.01 {
		t.Fatalf("share = %v%%, want %v%% of %v s", got, want, v.UptimeSeconds)
	}
}

func TestTheSchedulingPassesAreReportedByWhatStartedThemAgainstTheNudgesThatWereAbsorbed(t *testing.T) {
	h := newHarness(t)
	h.c.metrics.reconcilePasses.WithLabelValues(triggerTimer).Add(3)
	h.c.metrics.reconcilePasses.WithLabelValues(triggerNudge).Add(2)
	h.c.metrics.nudges.Add(17)
	v := h.c.ControllerLoad()
	if v.ReconcilePasses[triggerTimer] != 3 || v.ReconcilePasses[triggerNudge] != 2 || v.Nudges != 17 {
		t.Fatalf("passes = %v, nudges = %d, want 3 timer, 2 nudge and 17 wake-ups", v.ReconcilePasses, v.Nudges)
	}
	if _, ok := v.ReconcilePasses[triggerStartup]; ok {
		t.Error("a trigger that started no pass is listed")
	}
}

func TestRouteGroupsCarryTheirRequestsServerErrorsAndTimings(t *testing.T) {
	h := newHarness(t)
	h.c.ObserveRequest("hosts", 200, 100*time.Millisecond, false)
	h.c.ObserveRequest("hosts", 503, 300*time.Millisecond, false)
	// A long-lived route is counted and never timed: its length is its purpose.
	h.c.ObserveRequest("events", 200, time.Hour, true)

	v := h.c.ControllerLoad()
	hosts := groupOf(t, v, "hosts")
	if hosts.Requests != 2 || hosts.ServerErrors != 1 || hosts.Timed != 2 || hosts.Seconds != 0.4 || hosts.MeanSeconds != 0.2 {
		t.Errorf("hosts = %+v, want 2 requests, 1 server error, 2 timed taking 0.4 s", hosts)
	}
	events := groupOf(t, v, "events")
	if events.Requests != 1 || events.Timed != 0 || events.Seconds != 0 {
		t.Errorf("events = %+v, want one request counted and none timed", events)
	}
	if v.API[0].Group != "hosts" {
		t.Errorf("first group = %q, want the one with the most time", v.API[0].Group)
	}
}

func TestOnlyTheBusiestRouteGroupsAreListedAndTheRestAreCounted(t *testing.T) {
	h := newHarness(t)
	for i := 0; i < loadAPIGroups+5; i++ {
		h.c.ObserveRequest(fmt.Sprintf("group%02d", i), 200, time.Duration(i+1)*time.Millisecond, false)
	}
	v := h.c.ControllerLoad()
	if len(v.API) != loadAPIGroups || v.APIGroupsNotShown != 5 {
		t.Fatalf("listed %d groups, %d not shown; want %d and 5", len(v.API), v.APIGroupsNotShown, loadAPIGroups)
	}
	if v.API[0].Group != fmt.Sprintf("group%02d", loadAPIGroups+4) {
		t.Errorf("busiest group = %q, want the one that took longest", v.API[0].Group)
	}
}

// The machine the controller runs on is what the process share is read against,
// so one answer carries both and a reader does not have to ask twice and hope the
// two were taken together.
func TestTheLoadViewCarriesTheMachineTheControllerRunsOn(t *testing.T) {
	h := newHarness(t)
	h.c.machine.read = reading(68, 36)
	h.c.machine.readProcess = func(int) (float64, bool) { return 20, true }
	h.c.sampleMachine()
	m := h.c.ControllerLoad().Machine
	if m.Usage == nil || m.Usage.CPUPercent == nil || *m.Usage.CPUPercent != 68 ||
		m.Usage.ProcessCPUPercent == nil || *m.Usage.ProcessCPUPercent != 20 {
		t.Fatalf("machine = %+v, want the reading the tick took, with the controller's own 20%%", m.Usage)
	}
}

// A controller that has done nothing yet must still answer with lists and not
// nulls, because a client that loops over them would otherwise have to guard it.
func TestAControllerThatHasDoneNothingAnswersWithEmptyListsAndNotNulls(t *testing.T) {
	h := newHarness(t)
	v := h.c.ControllerLoad()
	if v.Steps == nil || v.API == nil || v.ReconcilePasses == nil {
		t.Fatalf("view = %+v, want empty lists and an empty map", v)
	}
	if v.Goroutines < 1 {
		t.Errorf("goroutines = %d, want at least this one", v.Goroutines)
	}
}

// Eli is offered this view, and what one tool may put in front of the model is
// cut from the front when it is too long, which leaves JSON with no beginning. So
// the view has to fit however busy the controller is. This is the largest one the
// caps allow: every step and route group at the cap, every optional field set,
// every number as many digits as the rounding leaves.
func TestTheLargestLoadViewFitsWhatEliMayBeShownOfOneTool(t *testing.T) {
	edge, over := 29.999, 30.0
	view := ControllerLoadView{
		StartedAt: time.Date(2026, 11, 4, 12, 30, 45, 123456789, time.UTC), UptimeSeconds: 12345678.9, Goroutines: 123456,
		ReconcilePasses: map[string]uint64{triggerStartup: 99999999, triggerTimer: 99999999, triggerNudge: 99999999, triggerRequest: 99999999},
		Nudges:          99999999, StepsNotShown: 99, APIGroupsNotShown: 99,
	}
	for i := 0; i < loadSteps; i++ {
		view.Steps = append(view.Steps, StepLoadView{
			Loop: loopHousekeeping, Step: stepControllerMachine, Whole: true, Count: 99999999,
			Seconds: 12345678.1234, MeanSeconds: 12345.123456, SlowestAtMostSeconds: &edge, SlowestOverSeconds: &over, UptimeSharePercent: 100,
		})
	}
	for i := 0; i < loadAPIGroups; i++ {
		view.API = append(view.API, RouteGroupLoadView{
			Group: "job_attention_group", Requests: 99999999, ServerErrors: 99999999, Timed: 99999999,
			Seconds: 12345678.1234, MeanSeconds: 12345.123456, SlowestAtMostSeconds: &edge, SlowestOverSeconds: &over, UptimeSharePercent: 100,
		})
	}
	f := 99.99
	mem, cpus := int64(123456789), 128
	view.Machine = ControllerMachineView{CPUs: cpus, MemoryMB: mem, UsageFresh: true, Usage: &ControllerMachineUsage{
		CPUPercent: &f, IOWaitPercent: &f, StealPercent: &f, ProcessCPUPercent: &f, LoadAverage1: &f, MemoryAvailableMB: &mem,
		SampledAt: time.Date(2026, 11, 4, 12, 30, 45, 123456789, time.UTC),
	}}
	raw, err := json.Marshal(view)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("the largest view is %d bytes against a ceiling of %d", len(raw), assistantToolResultBytes)
	if len(raw) >= assistantToolResultBytes {
		t.Fatalf("the largest load view is %d bytes, which is over the %d an assistant's tool result may be: lower loadSteps or loadAPIGroups", len(raw), assistantToolResultBytes)
	}
}

func TestOnlyTheLongestStepsAreListedAndTheRestAreCounted(t *testing.T) {
	h := newHarness(t)
	for i := 0; i < loadSteps+4; i++ {
		h.c.metrics.loopStep.WithLabelValues(loopReconcile, fmt.Sprintf("step%02d", i)).Observe(float64(i+1) / 1000)
	}
	v := h.c.ControllerLoad()
	if len(v.Steps) != loadSteps || v.StepsNotShown < 4 {
		t.Fatalf("listed %d steps, %d not shown; want %d listed and at least the 4 extra left out", len(v.Steps), v.StepsNotShown, loadSteps)
	}
	if v.Steps[0].Step != fmt.Sprintf("step%02d", loadSteps+3) {
		t.Errorf("longest step = %q, want the one that took longest", v.Steps[0].Step)
	}
}
