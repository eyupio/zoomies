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
	if total != 52 {
		t.Fatalf("seeded %d jobs, want 52", total)
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
		// fixture has to contain some -- one finished, one still running, and
		// one still queued, which is the one the default view used to show.
		if store.HostedJob(j.Labels) {
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
	if elsewhere != 3 {
		t.Fatalf("%d jobs belong to somebody else's runners, want 3 (one finished, one running, one queued)", elsewhere)
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

// The Providers and Machines pages open on nothing until the fixture rents
// something, and the two halves of what they render -- a machine that became a
// host, and one the fleet is still waiting for -- have to be two different
// machines.
func TestSeedDemoRentsTwoUnalikeMachinesFromOneProvider(t *testing.T) {
	h := newHarness(t)
	if err := h.c.SeedDemo(h.ctx); err != nil {
		t.Fatalf("SeedDemo: %v", err)
	}

	providers, err := h.st.ListProviders(h.ctx)
	if err != nil {
		t.Fatalf("ListProviders: %v", err)
	}
	if len(providers) != 1 {
		t.Fatalf("seeded %d providers, want 1", len(providers))
	}
	p := providers[0]
	switch {
	case p.Kind != store.ProviderProxmox:
		t.Errorf("the demo provider is %q, want the one kind this build can actually rent from", p.Kind)
	case !p.Enabled || p.Paused:
		t.Errorf("the demo provider is enabled=%v paused=%v, want one that would buy a machine", p.Enabled, p.Paused)
	case len(p.CredentialsEnc) == 0:
		t.Error("the demo provider has no sealed credential, so its card renders the no-credential badge")
	case p.LastCheckAt == nil || p.LastCheckError != "":
		t.Errorf("the demo provider's preflight is %v / %q, want one that passed", p.LastCheckAt, p.LastCheckError)
	case p.MaxMachines == 0:
		t.Error("the demo provider's ceiling is zero, which rents nothing")
	}
	// The answers docs/proxmox.md asks for. A provider with half of them blank
	// is what the page would teach somebody a configured one looks like.
	for _, key := range []string{"nodes", "template_id", "storage", "bridge", "vmid_min", "vmid_max"} {
		if p.Settings[key] == "" {
			t.Errorf("the demo provider has no %s, which is a setting Proxmox cannot be used without", key)
		}
	}

	machines := h.machines()
	if len(machines) != 2 {
		t.Fatalf("seeded %d machines, want 2", len(machines))
	}
	states := map[store.MachineState]int{}
	for _, m := range machines {
		states[m.State]++
		if m.ProviderID != p.ID {
			t.Errorf("machine %s belongs to %q, not to the seeded provider", m.ID, m.ProviderID)
		}
		if !m.Owns() {
			t.Errorf("machine %s owns no resource, so it counts against nothing and can never be deleted", m.ID)
		}
	}
	if states[store.MachineReady] != 1 {
		t.Errorf("machine states = %v, want exactly one ready machine for the host link", states)
	}
	if pending := len(machines) - states[store.MachineReady]; pending != 1 {
		t.Errorf("machine states = %v, want one machine still on its way for the lifecycle band", states)
	}
}

// The ready machine's host link is what HostCard's provider badge and "delete
// the machine instead" are drawn from, and it is only true if a token this
// machine minted was spent by that host.
func TestTheDemoReadyMachineIsEnrolledTheWayARealOneIs(t *testing.T) {
	h := newHarness(t)
	if err := h.c.SeedDemo(h.ctx); err != nil {
		t.Fatalf("SeedDemo: %v", err)
	}

	m := h.machineByID(t, demoMachineReadyID)
	if m.HostID == "" {
		t.Fatal("the ready demo machine has no host, so the Hosts page has no rented host to badge")
	}
	host, err := h.st.GetHost(h.ctx, m.HostID)
	if err != nil {
		t.Fatalf("GetHost %s: %v", m.HostID, err)
	}
	// The link has to be findable from the host as well: that is the read the
	// API makes before it refuses to forget a host Zoomies is paying for.
	back, err := h.st.GetMachineByHost(h.ctx, host.ID)
	if err != nil {
		t.Fatalf("GetMachineByHost %s: %v", host.ID, err)
	}
	if back.ID != m.ID {
		t.Errorf("host %s says it is machine %s, want %s", host.ID, back.ID, m.ID)
	}
	if m.Address != host.Address {
		t.Errorf("machine address %q and host address %q are the same computer", m.Address, host.Address)
	}

	tok, err := h.st.GetJoinToken(h.ctx, m.JoinTokenID)
	if err != nil {
		t.Fatalf("GetJoinToken %s: %v", m.JoinTokenID, err)
	}
	switch {
	case tok.MachineID != m.ID || tok.ExpectedName != m.Name:
		t.Errorf("the token is scoped to machine %q / name %q, want %q / %q",
			tok.MachineID, tok.ExpectedName, m.ID, m.Name)
	case tok.UsedAt == nil || tok.UsedByID != host.ID:
		t.Errorf("the token was spent at %v by %q, want it spent by %s", tok.UsedAt, tok.UsedByID, host.ID)
	case tok.Usable(tok.CreatedAt):
		t.Error("the machine's join token is still usable; a credential that did its job is spent")
	}
}

// The timeline is the panel the machine page exists for, and a fixture whose
// phases share one instant draws it as a column of zeroes.
func TestTheDemoMachineOnItsWayHasATimelineWithRealDurations(t *testing.T) {
	h := newHarness(t)
	if err := h.c.SeedDemo(h.ctx); err != nil {
		t.Fatalf("SeedDemo: %v", err)
	}

	m := h.machineByID(t, demoMachineBuildingID)
	if m.State.Terminal() || !m.State.Pending() {
		t.Fatalf("machine %s is %s, want one the fleet is still waiting for", m.ID, m.State)
	}
	entries := machineTimeline(m)
	// Planned, creating, created, starting: the clone and the boot are the two
	// halves an operator is trying to tell apart when they open this page.
	if len(entries) < 4 {
		t.Fatalf("the timeline has %d phases (%v), want the phases either side of the clone", len(entries), entries)
	}
	for i := 1; i < len(entries); i++ {
		gap := entries[i].At.Sub(entries[i-1].At)
		if gap <= 0 {
			t.Errorf("phase %s is %s after %s; a timeline that does not move draws every bar at zero",
				entries[i].Phase, gap, entries[i-1].Phase)
		}
	}
	if _, ok := map[string]bool{"starting": true}[entries[len(entries)-1].Phase]; !ok {
		t.Errorf("the last phase is %q, want the one the live row counts from", entries[len(entries)-1].Phase)
	}
}

// Everything the two pages render comes back through the views the API
// renders, so the fixture is checked the way a request would see it rather
// than as rows.
func TestTheDemoMachinesRenderWithTheirProviderPoolAndHost(t *testing.T) {
	h := newHarness(t)
	if err := h.c.SeedDemo(h.ctx); err != nil {
		t.Fatalf("SeedDemo: %v", err)
	}

	view, err := h.c.MachineRenderer(h.ctx)
	if err != nil {
		t.Fatalf("MachineRenderer: %v", err)
	}
	machines := h.machines()
	for _, m := range machines {
		got := view.View(m)
		switch {
		case got.ProviderName == "" || got.Kind == "":
			t.Errorf("machine %s renders provider %q / kind %q; the page has nothing to name it by",
				m.ID, got.ProviderName, got.Kind)
		case got.PoolName == "":
			t.Errorf("machine %s renders no pool, so the page cannot answer why it exists", m.ID)
		case len(got.Timeline) == 0:
			t.Errorf("machine %s renders an empty timeline", m.ID)
		}
	}
	if got := view.View(h.machineByID(t, demoMachineReadyID)); got.HostName == "" {
		t.Error("the ready machine renders no host name, so the link off the machine page has no label")
	}

	providers, err := h.st.ListProviders(h.ctx)
	if err != nil {
		t.Fatalf("ListProviders: %v", err)
	}
	p := h.c.ProviderView(providers[0], machines)
	if p.Owned != 2 {
		t.Errorf("the provider owns %d machines, want both of them counted against its ceiling", p.Owned)
	}
	if len(p.Machines) != 2 {
		t.Errorf("the provider's machines by state = %v, want the two states apart", p.Machines)
	}
	if !p.CredentialsConfigured {
		t.Error("the provider renders as having no credential")
	}
	if p.Held != "" {
		t.Errorf("the provider is held: %q; a demo fleet opens on one that is not", p.Held)
	}
}

// A machine may only be deleted on an observation younger than a minute, and a
// demo has no hypervisor behind it to make a second one. Left alone the fixture
// ages into a page where every machine reads "nothing has confirmed this
// resource is ours recently enough to act on" -- the heartbeat's own failure
// mode, on the half of the fleet that spends money.
func TestTheDemoFleetDoesNotAgeIntoMachinesNothingCanAccountFor(t *testing.T) {
	h := newHarness(t)
	if err := h.c.SeedDemo(h.ctx); err != nil {
		t.Fatalf("SeedDemo: %v", err)
	}

	// An hour into an instance somebody left open on a second monitor.
	future := time.Now().Add(time.Hour)
	h.c.clock = func() time.Time { return future }

	m := h.machineByID(t, demoMachineReadyID)
	if ok, _ := machineSafeToDelete(m, future); ok {
		t.Fatal("an hour-old observation was still good enough to delete on, so this test proves nothing")
	}

	h.c.freshenDemoMachines(h.ctx)

	m = h.machineByID(t, demoMachineReadyID)
	if ok, why := machineSafeToDelete(m, future); !ok {
		t.Errorf("the demo machine still cannot be deleted an hour in: %s", why)
	}
	p, err := h.st.GetProvider(h.ctx, demoProviderID)
	if err != nil {
		t.Fatalf("GetProvider: %v", err)
	}
	if p.LastSweepAt == nil || future.Sub(*p.LastSweepAt) > time.Minute {
		t.Errorf("the demo provider was last swept at %v, want a sweep as recent as the fleet's clock", p.LastSweepAt)
	}
}

// And it never touches a machine it did not seed: an operator who set
// ZOOMIES_SEED_DEMO on an instance with a real provider must not have a
// resource nothing could account for quietly vouched for.
func TestDemoRefreshLeavesRealMachinesAlone(t *testing.T) {
	h := newHarness(t)
	row := h.providerRow(t, "real-pve")
	m := &store.Machine{
		ID:             "mach_realone",
		ProviderID:     row.ID,
		Name:           "zoomies-mach-realone",
		State:          store.MachineReady,
		ResourceZone:   "zone-a",
		ResourceID:     "4242",
		OwnershipError: "the guest at 4242 carries somebody else's owner tag",
	}
	if err := h.st.CreateMachine(h.ctx, m); err != nil {
		t.Fatalf("CreateMachine: %v", err)
	}

	h.c.freshenDemoMachines(h.ctx)

	got := h.machineByID(t, m.ID)
	if got.OwnershipVerifiedAt != nil || got.OwnershipError == "" {
		t.Errorf("a real machine was vouched for by the demo refresh: verified=%v error=%q",
			got.OwnershipVerifiedAt, got.OwnershipError)
	}
	fresh, err := h.st.GetProvider(h.ctx, row.ID)
	if err != nil {
		t.Fatalf("GetProvider: %v", err)
	}
	if fresh.LastSweepAt != nil {
		t.Errorf("a real provider was recorded as swept at %v by the demo refresh", fresh.LastSweepAt)
	}
}

// The provider, its machines and the credential between them are fixtures too,
// and a fixture that is mistakable for a real row is one the prober, the
// poller and the reap would treat as somebody's fleet.
func TestTheProviderAndMachineFixturesAreRecognisedAsFixtures(t *testing.T) {
	h := newHarness(t)
	if err := h.c.SeedDemo(h.ctx); err != nil {
		t.Fatalf("SeedDemo: %v", err)
	}

	ids := []string{demoProviderID, demoMachineReadyID, demoMachineBuildingID, demoJoinTokenID}
	for _, m := range h.machines() {
		ids = append(ids, m.ID)
	}
	for _, id := range ids {
		if !IsDemoID(id) {
			t.Errorf("fixture %q is not recognised as demo data", id)
		}
		if store.LooksGenerated(id) {
			t.Errorf("fixture %q has the shape of a real identifier", id)
		}
	}
	// And the rows are really there under those identifiers, or the list above
	// is a list of constants that agree with each other and nothing else.
	if _, err := h.st.GetProvider(h.ctx, demoProviderID); err != nil {
		t.Errorf("GetProvider %s: %v", demoProviderID, err)
	}
	if _, err := h.st.GetJoinToken(h.ctx, demoJoinTokenID); err != nil {
		t.Errorf("GetJoinToken %s: %v", demoJoinTokenID, err)
	}
}
