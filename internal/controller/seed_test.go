package controller

import (
	"strings"
	"testing"
	"time"

	"github.com/eyupio/zoomies/internal/store"
)

// The Playwright suite and a demo instance both need a fleet with something in
// it. This is that fixture, and it has to be the same fixture every time.
func TestSeedDemoBuildsAFleet(t *testing.T) {
	h := newHarness(t)

	if err := h.c.SeedDemo(h.ctx); err != nil {
		t.Fatalf("SeedDemo: %v", err)
	}

	pools, err := h.st.ListPools(h.ctx)
	if err != nil {
		t.Fatalf("ListPools: %v", err)
	}
	if len(pools) != 2 {
		t.Fatalf("seeded %d pools, want 2", len(pools))
	}

	hosts, err := h.st.ListHosts(h.ctx)
	if err != nil {
		t.Fatalf("ListHosts: %v", err)
	}
	if len(hosts) != 3 {
		t.Fatalf("seeded %d hosts, want 3", len(hosts))
	}

	runners := h.runners()
	if len(runners) != 12 {
		t.Fatalf("seeded %d runners, want 12", len(runners))
	}
	states := map[store.RunnerState]int{}
	for _, r := range runners {
		states[r.State]++
	}
	for _, want := range []store.RunnerState{
		store.RunnerProvisioning, store.RunnerRegistering, store.RunnerIdle,
		store.RunnerBusy, store.RunnerDraining, store.RunnerFailed, store.RunnerRemoved,
	} {
		if states[want] == 0 {
			t.Fatalf("no seeded runner is %q; the UI has nothing to render for that state", want)
		}
	}

	jobs, total, err := h.st.ListJobs(h.ctx, store.JobFilter{}, store.Page{Limit: 100})
	if err != nil {
		t.Fatalf("ListJobs: %v", err)
	}
	if total != 51 {
		t.Fatalf("seeded %d jobs, want 51", total)
	}
	var completed, queued, running, unmatched, elsewhere int
	for _, j := range jobs {
		switch j.State {
		case store.JobCompleted:
			completed++
		case store.JobQueued:
			queued++
		case store.JobInProgress:
			running++
		}
		if !j.Matched {
			unmatched++
		}
		// A job on a vendor's runner: no pool claimed it and no runner here
		// ran it. The Overview's panels are built to tell these apart, so the
		// fixture has to contain some -- one finished, one still running.
		if !j.Matched && j.PoolID == "" && j.RunnerID == "" && j.State != store.JobQueued {
			elsewhere++
		}
	}
	if completed == 0 || queued == 0 || running == 0 {
		t.Fatalf("job mix = %d completed, %d queued, %d running; the Overview needs all three",
			completed, queued, running)
	}
	if unmatched == 0 {
		t.Fatal("no seeded job is unmatched, so the problems drawer has nothing to show")
	}
	if elsewhere != 2 {
		t.Fatalf("%d jobs ran somewhere other than this fleet, want 2 (one finished, one running)", elsewhere)
	}

	// The Overview's history and the audit page both need rows.
	evs, err := h.st.ListScalingEvents(h.ctx, "", 50)
	if err != nil {
		t.Fatalf("ListScalingEvents: %v", err)
	}
	if len(evs) == 0 {
		t.Fatal("no scaling history was seeded")
	}
	audit, auditTotal, err := h.st.ListAudit(h.ctx, store.AuditFilter{}, store.Page{Limit: 50})
	if err != nil {
		t.Fatalf("ListAudit: %v", err)
	}
	if auditTotal == 0 || len(audit) == 0 {
		t.Fatal("no audit rows were seeded")
	}

	// Stats has to work over the fixture, since that is what the Overview does.
	if _, err := h.c.Stats(h.ctx, 24*time.Hour); err != nil {
		t.Fatalf("Stats over the fixture: %v", err)
	}
}

// Seeding twice must not double the fixture: the Playwright suite restarts the
// binary and would otherwise accumulate a fleet.
func TestSeedDemoIsIdempotent(t *testing.T) {
	h := newHarness(t)
	if err := h.c.SeedDemo(h.ctx); err != nil {
		t.Fatalf("first SeedDemo: %v", err)
	}
	before := len(h.runners())

	if err := h.c.SeedDemo(h.ctx); err != nil {
		t.Fatalf("second SeedDemo: %v", err)
	}
	if got := len(h.runners()); got != before {
		t.Fatalf("runners went from %d to %d on a second seed", before, got)
	}
}

// Fixtures appearing in somebody's real fleet would be indistinguishable from
// a compromise, so an instance that is already in use is refused.
func TestSeedDemoRefusesARealFleet(t *testing.T) {
	h := newHarness(t)
	inst := h.installation()
	h.pool(inst, "production-linux")

	err := h.c.SeedDemo(h.ctx)
	if err == nil {
		t.Fatal("SeedDemo seeded an instance that already had a real pool")
	}
	if !strings.Contains(err.Error(), "production-linux") || !strings.Contains(err.Error(), SeedEnvVar) {
		t.Fatalf("error = %q, want it to name the pool and how to turn seeding off", err)
	}
	if got := len(h.runners()); got != 0 {
		t.Fatalf("the refused seed still wrote %d runners", got)
	}
}

// The Jobs page's drawer, its failed filter and the problems drawer's "runner
// lost" entry all need a fixture behind them, and the timeline needs one job
// whose story it can tell in full.
func TestSeedDemoGivesJobsStepsATimelineAndOneLostRunner(t *testing.T) {
	h := newHarness(t)
	if err := h.c.SeedDemo(h.ctx); err != nil {
		t.Fatalf("SeedDemo: %v", err)
	}

	failed, total, err := h.st.ListJobs(h.ctx, store.JobFilter{FailedOnly: true}, store.Page{Limit: 100})
	if err != nil {
		t.Fatalf("ListJobs: %v", err)
	}
	if total == 0 {
		t.Fatal("the seed has no failed job for the failed filter to find")
	}
	var lost *store.Job
	for _, j := range failed {
		if j.Conclusion == "failure" && j.FailedStep() == nil {
			t.Errorf("failed job %s names no failed step", j.ID)
		}
		if j.RunnerFault != "" {
			lost = j
		}
	}
	if lost == nil {
		t.Fatal("no seeded job lost its runner, so the runner_lost badge has no fixture")
	}

	events, err := h.c.JobEvents(h.ctx, lost.ID)
	if err != nil {
		t.Fatalf("JobEvents: %v", err)
	}
	kinds := kindsOfEvents(events)
	for _, want := range []store.JobEventKind{store.JobEventQueued, store.JobEventClaimed, store.JobEventStarted, store.JobEventRunnerLost, store.JobEventCompleted} {
		found := false
		for _, k := range kinds {
			found = found || k == want
		}
		if !found {
			t.Errorf("the lost-runner job's timeline %v lacks %s", kinds, want)
		}
	}
	for i := 1; i < len(events); i++ {
		if events[i].At.Before(events[i-1].At) {
			t.Errorf("timeline out of order at %d: %v after %v", i, events[i].At, events[i-1].At)
		}
	}

	running, _, err := h.st.ListJobs(h.ctx, store.JobFilter{States: []store.JobState{store.JobInProgress}}, store.Page{})
	if err != nil {
		t.Fatalf("ListJobs running: %v", err)
	}
	for _, j := range running {
		if len(j.Steps) == 0 || j.FailedStep() != nil {
			t.Errorf("running job %s has steps %+v, want some in flight and none failed", j.ID, j.Steps)
		}
	}
}

// A pool was the only thing the guard looked at. A production controller that
// has an installation, a host and an administrator but no pool yet -- the
// state every fresh install passes through -- got a fixture fleet if its
// compose file kept ZOOMIES_SEED_DEMO from a trial run.
func TestSeedDemoRefusesAnInstanceWithRealStateButNoPool(t *testing.T) {
	cases := []struct {
		name string
		set  func(h *harness)
		want string
	}{
		{"an installation", func(h *harness) { h.installation() }, "installation"},
		{"a host", func(h *harness) { h.host("build-1") }, `host "build-1"`},
		{"an account", func(h *harness) {
			if err := h.st.CreateUser(h.ctx, &store.User{Username: "root", Role: store.RoleAdmin}); err != nil {
				h.t.Fatalf("CreateUser: %v", err)
			}
		}, "1 account"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := newHarness(t)
			tc.set(h)
			err := h.c.SeedDemo(h.ctx)
			if err == nil {
				t.Fatalf("SeedDemo seeded an instance that already had %s", tc.name)
			}
			if !strings.Contains(err.Error(), tc.want) || !strings.Contains(err.Error(), SeedEnvVar) {
				t.Fatalf("error = %q, want it to name %s and how to turn seeding off", err, tc.want)
			}
			pools, err := h.st.ListPools(h.ctx)
			if err != nil || len(pools) != 0 {
				t.Fatalf("the refused seed still wrote %d pools (%v)", len(pools), err)
			}
		})
	}
}

// And the demo's own fixtures are not "real state": a seeded instance is
// still recognised as seeded on its next start.
func TestSeedDemoStillRecognisesItsOwnFixtures(t *testing.T) {
	h := newHarness(t)
	if err := h.c.SeedDemo(h.ctx); err != nil {
		t.Fatalf("first SeedDemo: %v", err)
	}
	if err := h.c.SeedDemo(h.ctx); err != nil {
		t.Fatalf("second SeedDemo refused its own fixtures: %v", err)
	}
}

// The demo fleet has to stay alive for as long as somebody is looking at it.
//
// A seeded host has no agent behind it, so its heartbeat is written once. Ninety
// seconds later store.HeartbeatTimeout has passed, every demo host is unhealthy
// and every demo pool reports that it has nowhere to run -- the fleet goes dark
// while the page is open. It also made the Playwright suite's answers depend on
// how long the suite had been running.
func TestDemoHostsKeepBeating(t *testing.T) {
	h := newHarness(t)

	if err := h.c.SeedDemo(h.ctx); err != nil {
		t.Fatalf("SeedDemo: %v", err)
	}

	// Far enough past the timeout that nothing is healthy any more.
	future := time.Now().Add(store.HeartbeatTimeout * 3)
	h.c.clock = func() time.Time { return future }

	hosts, err := h.st.ListHosts(h.ctx)
	if err != nil {
		t.Fatalf("ListHosts: %v", err)
	}
	for _, host := range hosts {
		if host.Healthy(future) {
			t.Fatalf("host %s was still healthy before the beat; the fixture proves nothing", host.ID)
		}
	}

	h.c.beatDemoHosts(h.ctx)

	hosts, err = h.st.ListHosts(h.ctx)
	if err != nil {
		t.Fatalf("ListHosts: %v", err)
	}
	if len(hosts) == 0 {
		t.Fatal("no hosts to check")
	}
	for _, host := range hosts {
		if !host.Healthy(future) {
			t.Errorf("demo host %s is unhealthy after a beat", host.ID)
		}
	}
}

// And it never touches a host it did not create: an operator who set
// ZOOMIES_SEED_DEMO on an instance that has since grown a real agent must not
// have that agent's silence covered up.
func TestDemoHeartbeatLeavesRealHostsAlone(t *testing.T) {
	h := newHarness(t)

	real := &store.Host{
		ID:            "hst_realone",
		Name:          "a-real-host",
		Capacity:      2,
		LastHeartbeat: time.Now().Add(-store.HeartbeatTimeout * 2),
	}
	if err := h.st.CreateHost(h.ctx, real); err != nil {
		t.Fatalf("CreateHost: %v", err)
	}

	h.c.beatDemoHosts(h.ctx)

	got, err := h.st.GetHost(h.ctx, real.ID)
	if err != nil {
		t.Fatalf("GetHost: %v", err)
	}
	if got.Healthy(time.Now()) {
		t.Fatal("a real host was marked alive by the demo heartbeat")
	}
}

// The demo fleet holds one runner in provisioning and one in registering so
// both states appear in the grid. On a real fleet those states last seconds, so
// left alone the fixture ages into a fleet reporting runners that are stuck --
// on every screenshot, and in the back half of a Playwright run but not the
// front half, which is the worst kind of failure to chase.
func TestTheDemoFleetDoesNotAgeIntoAFleetWithAProblem(t *testing.T) {
	h := newHarness(t)
	if err := h.c.SeedDemo(h.ctx); err != nil {
		t.Fatalf("SeedDemo: %v", err)
	}

	// An hour into an instance somebody left open on a second monitor.
	future := time.Now().Add(time.Hour)
	h.c.clock = func() time.Time { return future }

	if got := h.problemCodes(); !contains(got, "runners.not_progressing") {
		t.Fatalf("problems = %v; an hour-old fixture should be stuck before the beat, or this test proves nothing", got)
	}

	h.c.freshenDemoRunners(h.ctx)

	if got := h.problemCodes(); contains(got, "runners.not_progressing") {
		t.Fatalf("problems = %v, want the demo's starting runners to read as freshly started", got)
	}
	// The states themselves survive: freshening a clock must not quietly
	// finish the runners the fixture exists to show.
	states := map[store.RunnerState]int{}
	for _, r := range h.runners() {
		states[r.State]++
	}
	if states[store.RunnerProvisioning] != 1 || states[store.RunnerRegistering] != 1 {
		t.Fatalf("states = %v, want one provisioning and one registering runner still there", states)
	}
}

// And it never touches a runner it did not seed: a real runner that is genuinely
// stuck must not have its clock wound back by an instance someone set
// ZOOMIES_SEED_DEMO on.
func TestDemoRefreshLeavesRealRunnersAlone(t *testing.T) {
	h := newHarness(t)
	_, pool, host := h.fleet()
	r := h.runnerRow(pool, host, store.RunnerRegistering)

	future := r.CreatedAt.Add(time.Hour)
	h.c.clock = func() time.Time { return future }
	h.c.freshenDemoRunners(h.ctx)

	if got := h.problemCodes(); !contains(got, "runners.not_progressing") {
		t.Fatalf("problems = %v, want a real runner stuck for an hour to still be reported", got)
	}
}
