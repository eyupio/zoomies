package controller

import (
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/eyupio/zoomies/internal/config"
	"github.com/eyupio/zoomies/internal/events"
	"github.com/eyupio/zoomies/internal/provider"
	"github.com/eyupio/zoomies/internal/store"
)

// ---------------------------------------------------------------------------
// The machine harness
// ---------------------------------------------------------------------------

// machineFleet is the setup every test in these files starts from: providers
// switched on with a ceiling, one provider row that can serve the pool, one
// pool and one queued job. There is deliberately no host: a fleet with nowhere
// to put a runner is the whole reason a machine gets bought.
func (h *harness) machineFleet(t *testing.T) (*store.Pool, *store.Provider) {
	t.Helper()
	h.enableProviders(t)
	inst := h.installation()
	pool := h.pool(inst, "linux-x64")
	row := h.providerRow(t, "lab")
	h.queueWork(t)
	return pool, row
}

// queueWork puts one job in the queue the way GitHub does, so that the pool's
// demand is the scheduler's own reading rather than something a test asserted.
func (h *harness) queueWork(t *testing.T) {
	t.Helper()
	h.deliverJob(jobEvent{Action: "queued", JobID: time.Now().UnixNano(),
		Labels: []string{"self-hosted", "linux", "x64", "demo"}})
}

// enableProviders turns the machine loop's configuration on with a ceiling, so
// that a test is exercising the fleet rather than the validator's refusal to
// rent anything without one.
func (h *harness) enableProviders(t *testing.T) {
	t.Helper()
	h.c.UpdateConfig(func(c *config.Config) {
		c.Provider.Enabled = true
		c.Provider.MaxMachines = 10
		c.Provider.MaxCreatesInFlight = 10
	})
	h.cfg = h.c.Config()
}

// providerRow writes a provider that offers two docker slots per machine.
func (h *harness) providerRow(t *testing.T, name string) *store.Provider {
	t.Helper()
	p := &store.Provider{
		Kind:               store.ProviderFake,
		Name:               name,
		Endpoint:           "https://" + name + ".test",
		Settings:           store.StringMap{"zone": "zone-a"},
		MachineLabels:      store.StringMap{},
		MachineCapacity:    2,
		MachineBackend:     store.BackendDocker,
		MaxMachines:        5,
		MaxCreatesInFlight: 5,
		IdleTimeout:        store.Duration(15 * time.Minute),
		Enabled:            true,
	}
	if err := h.st.CreateProvider(h.ctx, p); err != nil {
		t.Fatalf("CreateProvider: %v", err)
	}
	return p
}

// machinePass runs one machine pass and waits for the provider calls it
// issued, which outlive it. Without the wait a test would be asserting on a
// pass that has claimed the work and not yet done it.
func (h *harness) machinePass(t *testing.T) {
	t.Helper()
	if err := h.c.ReconcileMachines(h.ctx); err != nil {
		t.Fatalf("ReconcileMachines: %v", err)
	}
	h.c.machines.calls.Wait()
}

// machines returns every machine row, oldest first.
func (h *harness) machines() []*store.Machine {
	h.t.Helper()
	ms, _, err := h.st.ListMachines(h.ctx, store.MachineFilter{IncludeDeleted: true}, store.Page{Limit: 100})
	if err != nil {
		h.t.Fatalf("ListMachines: %v", err)
	}
	slices.SortFunc(ms, func(a, b *store.Machine) int { return strings.Compare(a.ID, b.ID) })
	return ms
}

// onlyMachine asserts there is exactly one machine and returns it fresh.
func (h *harness) onlyMachine(t *testing.T) *store.Machine {
	t.Helper()
	ms := h.machines()
	if len(ms) != 1 {
		t.Fatalf("expected exactly one machine, got %d", len(ms))
	}
	return ms[0]
}

// machineByID re-reads one machine, for a test that has to see a column move.
func (h *harness) machineByID(t *testing.T, id string) *store.Machine {
	t.Helper()
	m, err := h.st.GetMachine(h.ctx, id)
	if err != nil {
		t.Fatalf("GetMachine %s: %v", id, err)
	}
	return m
}

// drive runs passes until the machine reaches a state, or gives up saying
// where it actually got to. It is a helper rather than a loop in each test
// because the number of passes a lifecycle takes is an implementation detail --
// what a test means is "let this run".
func (h *harness) drive(t *testing.T, id string, want store.MachineState, passes int) *store.Machine {
	t.Helper()
	var m *store.Machine
	for range passes {
		m = h.machineByID(t, id)
		if m.State == want {
			return m
		}
		// A machine waiting out a backoff is waiting for a clock, and a test
		// has one it can move.
		h.pastBackoff(t, id)
		h.machinePass(t)
	}
	m = h.machineByID(t, id)
	if m.State != want {
		t.Fatalf("machine %s reached %s after %d passes, want %s (message: %s, provider error: %s)",
			id, m.State, passes, want, m.Message, m.ProviderError)
	}
	return m
}

// pastBackoff moves the controller's clock past whatever a machine is waiting
// out, which is the honest way for a test to reach the next attempt: the wait
// is real, and a test that wrote the columns itself would be asserting on its
// own arithmetic rather than on the fleet's.
func (h *harness) pastBackoff(t *testing.T, id string) {
	t.Helper()
	m := h.machineByID(t, id)
	now := h.c.Now()
	if m.NextAttemptAt != nil && m.NextAttemptAt.After(now) {
		h.advance(m.NextAttemptAt.Sub(now) + time.Second)
	}
}

// callsTo counts how many times an operation was made against the fake.
func (h *harness) callsTo(op string) int {
	n := 0
	for _, call := range h.fake.Calls() {
		if call == op || strings.HasPrefix(call, op+" ") {
			n++
		}
	}
	return n
}

// assertOneResourcePerMachine is the invariant every restart test ends on: no
// two live rows share an identity, no two share a name, and the provider was
// asked to create each machine exactly once.
//
// It is assertOneLiveRowPerName's twin, and it is the assertion that catches
// the failure this whole design exists to prevent: one row, two machines, one
// of them invisible and both of them on the bill.
func (h *harness) assertOneResourcePerMachine(t *testing.T) {
	t.Helper()
	seenResource := map[string]string{}
	seenName := map[string]string{}
	for _, m := range h.machines() {
		if m.DeletedAt != nil {
			continue
		}
		if m.ResourceID != "" {
			key := m.ProviderID + "/" + m.ResourceZone + "/" + m.ResourceID
			if was, dup := seenResource[key]; dup {
				t.Errorf("machines %s and %s both name resource %s", was, m.ID, key)
			}
			seenResource[key] = m.ID
		}
		if was, dup := seenName[m.Name]; dup {
			t.Errorf("machines %s and %s are both called %s", was, m.ID, m.Name)
		}
		seenName[m.Name] = m.ID
	}
	// A create ISSUED twice for one identity is not in itself the bug -- a
	// create whose answer was lost and whose resource could not be found twice
	// is deliberately re-issued with the same identity, which is a retry of one
	// machine rather than the purchase of a second. The bug is a second
	// machine, so that is what is counted: the provider must not be holding
	// more machines than the fleet has rows naming one.
	if got := len(h.fake.Machines()); got > len(seenResource) {
		t.Errorf("the provider holds %d machines and the fleet has %d rows naming one; a resource with no row is a machine nobody is watching",
			got, len(seenResource))
	}
}

// ---------------------------------------------------------------------------
// The lifecycle
// ---------------------------------------------------------------------------

// The whole point of the feature, in one test: a pool with work and nowhere to
// put it ends with a machine that has become a host. Every state in between is
// reached by observing the provider rather than by assuming the last call
// worked.
func TestAQueueWithNowhereToRunBuysAMachineAndEnrolsIt(t *testing.T) {
	h := newHarness(t)
	_, row := h.machineFleet(t)

	h.machinePass(t)
	m := h.onlyMachine(t)
	if m.State != store.MachineCreating {
		t.Fatalf("after one pass the machine is %s, want creating: %s", m.State, m.Message)
	}
	if m.ResourceID == "" {
		t.Fatal("the machine has no resource identity; it must be written before anything is created")
	}
	if m.ProviderID != row.ID {
		t.Fatalf("the machine belongs to %s, want %s", m.ProviderID, row.ID)
	}

	m = h.drive(t, m.ID, store.MachineEnrolling, 8)
	if m.JoinTokenID == "" {
		t.Fatal("the machine reached enrolling with no join token; nothing could ever join with it")
	}
	if got := len(h.fake.Bootstraps()); got != 1 {
		t.Fatalf("the guest was bootstrapped %d times, want once", got)
	}

	// The agent inside the guest joins with what it was given.
	h.joinAsMachine(t, m)
	h.machinePass(t)

	m = h.machineByID(t, m.ID)
	if m.State != store.MachineReady {
		t.Fatalf("the machine is %s after its agent joined, want ready: %s", m.State, m.Message)
	}
	if m.HostID == "" {
		t.Fatal("a ready machine with no host: the link is the only thing that grants deletion authority")
	}
	h.assertOneResourcePerMachine(t)
}

// joinAsMachine enrols an agent the way the guest would, with the token that
// machine's payload carried.
func (h *harness) joinAsMachine(t *testing.T, m *store.Machine) {
	t.Helper()
	token := h.tokenFromBootstrap(t, m)
	resp, err := h.c.Join(h.ctx, joinRequest(m.Name, token), "203.0.113.9")
	if err != nil {
		t.Fatalf("joining as machine %s: %v", m.Name, err)
	}
	if resp.HostID == "" {
		t.Fatal("the join returned no host id")
	}
}

// tokenFromBootstrap digs the join token out of the payload the controller
// pushed into the guest, which is the only place the plaintext ever exists.
func (h *harness) tokenFromBootstrap(t *testing.T, m *store.Machine) string {
	t.Helper()
	for _, b := range h.fake.Bootstraps() {
		for _, f := range b.Files {
			if f.Path != machineEnvPath {
				continue
			}
			for line := range strings.SplitSeq(string(f.Content), "\n") {
				name, value, ok := strings.Cut(line, "=")
				if ok && name == "ZOOMIES_AGENT_NAME" && value != m.Name {
					break
				}
				if ok && name == "ZOOMIES_JOIN_TOKEN" {
					return value
				}
			}
		}
	}
	t.Fatalf("no join token was written into machine %s's guest", m.Name)
	return ""
}

// A machine is bought once for a shortfall, not once per pass. A reservation
// that did not count as capacity would have every pass buy the queue again,
// and the fleet would notice on the invoice rather than on the page.
func TestASecondPassBuysNothingWhileTheFirstMachineIsStillComing(t *testing.T) {
	h := newHarness(t)
	h.machineFleet(t)

	for range 3 {
		h.machinePass(t)
	}
	if got := len(h.machines()); got != 1 {
		t.Fatalf("three passes bought %d machines for one shortfall, want 1", got)
	}
	h.assertOneResourcePerMachine(t)
}

// A create that takes minutes is the normal case, not the exception. The pass
// that finds one already under way must poll it, never start another.
func TestASlowCreateDoesNotStartASecondOne(t *testing.T) {
	h := newHarness(t)
	h.machineFleet(t)
	h.fake.SetAsync("create", 5)

	for range 5 {
		h.machinePass(t)
	}
	if got := h.callsTo("create"); got != 1 {
		t.Fatalf("the provider was asked to create %d times while one create was still running, want 1", got)
	}
	m := h.onlyMachine(t)
	if m.State != store.MachineCreating {
		t.Fatalf("the machine is %s while its create is still running, want creating", m.State)
	}
	h.assertOneResourcePerMachine(t)
}

// The machine loop must never be able to hold up the scheduling pass. A
// hypervisor that accepts a connection and answers in four minutes would
// otherwise stop every pool placing runners for four minutes, which is the
// argument that moved capacity-demand delivery out of the pass as well.
func TestASlowProviderDoesNotHoldTheReconcilePass(t *testing.T) {
	h := newHarness(t)
	h.machineFleet(t)
	h.fake.SetDelay("allocate", 2*time.Second)

	done := make(chan struct{})
	go func() {
		defer close(done)
		_ = h.c.ReconcileMachines(h.ctx)
	}()
	defer func() {
		<-done
		h.c.machines.calls.Wait()
	}()

	started := time.Now()
	if err := h.c.Reconcile(h.ctx); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	if took := time.Since(started); took > time.Second {
		t.Fatalf("a scheduling pass took %s while a provider call was in flight; the machine loop is holding reconcileMu", took)
	}
}

// Two passes running at once must not be able to exceed the create budget
// between them. The claim is in the database rather than in memory precisely so
// that the answer does not depend on which goroutine read first.
func TestConcurrentMachinePassesNeverExceedTheCreateLimit(t *testing.T) {
	h := newHarness(t)
	_, row := h.machineFleet(t)
	row.MaxCreatesInFlight = 1
	if err := h.st.UpdateProvider(h.ctx, row); err != nil {
		t.Fatalf("UpdateProvider: %v", err)
	}

	done := make(chan struct{}, 2)
	for range 2 {
		go func() {
			defer func() { done <- struct{}{} }()
			_ = h.c.ReconcileMachines(h.ctx)
		}()
	}
	<-done
	<-done
	h.c.machines.calls.Wait()

	if got := len(h.machines()); got > 1 {
		t.Fatalf("two concurrent passes bought %d machines against a limit of one in flight", got)
	}
	h.assertOneResourcePerMachine(t)
}

// A provider that is full says so and stands down, and -- the half that matters
// -- keeps draining and deleting while it does. A kill switch that also stopped
// those would strand running machines nobody is watching.
func TestAProviderOutOfCapacitySaysSoAndStandsDown(t *testing.T) {
	h := newHarness(t)
	_, row := h.machineFleet(t)
	h.fake.SetFailure("create", provider.FailureQuota, "no capacity for another machine: 12 of 12 in use")

	h.machinePass(t)
	m := h.onlyMachine(t)
	if m.ProviderError == "" {
		t.Fatal("a refused create recorded nothing on the machine")
	}
	if !slices.Contains(h.problemCodes(), "provider.quota_exhausted") {
		t.Fatalf("a provider that is full raised %v, want provider.quota_exhausted", h.problemCodes())
	}
	// Jobs are queued, so this is an outage rather than a note.
	if got := h.problem(t, "provider.quota_exhausted").Severity; got != config.SeverityError {
		t.Fatalf("with jobs queued the entry is %s, want error", got)
	}

	// And the drain half still runs: a machine that is already ready is
	// released even while the provider refuses new ones.
	h.fake.ClearFailures()
	ready := h.readyMachine(t, row)
	h.pauseProvider(t, row, "full")
	h.beginDrainFor(t, ready)
	h.machinePass(t)
	if got := h.machineByID(t, ready.ID); got.State != store.MachineDeleting && got.State != store.MachineDeleted {
		t.Fatalf("a draining machine on a paused provider is %s; drains and deletes must never be held", got.State)
	}
}

// A bootstrap that fails says what the guest said, on the guest's own column.
// Two systems complain about a machine -- the hypervisor and the thing inside
// it -- and an operator reading one error has to know which half to go to.
func TestABootstrapThatFailsSaysWhatTheGuestSaid(t *testing.T) {
	h := newHarness(t)
	h.machineFleet(t)
	h.machinePass(t)
	m := h.onlyMachine(t)
	m = h.drive(t, m.ID, store.MachineBootstrapping, 6)

	h.fake.SetFailure("bootstrap", provider.FailureRefused, "dpkg: zoomies-agent is not installed in this template")
	h.machinePass(t)

	m = h.machineByID(t, m.ID)
	if !strings.Contains(m.ProviderError+m.BootstrapError, "not installed in this template") {
		t.Fatalf("the guest's own words did not reach the machine row: provider=%q bootstrap=%q",
			m.ProviderError, m.BootstrapError)
	}
	if m.State == store.MachineDeleted {
		t.Fatal("a machine whose bootstrap failed was removed; the resource is still running and still being paid for")
	}
}

// Three machines that never reach ready stand the provider down. This is what
// stops a bad template burning fifty machines while nobody is looking.
func TestThreeFailedBootstrapsStandTheProviderDown(t *testing.T) {
	h := newHarness(t)
	_, row := h.machineFleet(t)
	h.fake.SetFailure("allocate", provider.FailureUnreachable, "dial tcp: connection refused")

	for range breakerThreshold {
		h.machinePass(t)
		for _, m := range h.machines() {
			h.pastBackoff(t, m.ID)
		}
	}
	got, err := h.st.GetProvider(h.ctx, row.ID)
	if err != nil {
		t.Fatalf("GetProvider: %v", err)
	}
	if got.ConsecutiveFailures < breakerThreshold {
		t.Fatalf("the provider recorded %d failures in a row, want at least %d", got.ConsecutiveFailures, breakerThreshold)
	}
	if got.PausedUntil == nil {
		t.Fatal("three failures in a row did not stand the provider down; a bad template would burn machines until somebody noticed")
	}
	// A second before the stand-down expires, nothing may be bought from it.
	if held := h.c.provisioningHeld(got, got.PausedUntil.Add(-time.Second)); held == "" {
		t.Fatal("a stood-down provider is not held, so the next pass buys another machine")
	}
}

// A delete is finished when the provider cannot find the resource, and not
// before. A 200 from a delete call is not evidence: a fleet that took it as one
// would stop counting a machine that is still running and still being billed.
func TestADeleteIsNotCompleteUntilAnInspectCannotFindIt(t *testing.T) {
	h := newHarness(t)
	_, row := h.machineFleet(t)
	m := h.readyMachine(t, row)
	h.beginDrainFor(t, m)

	m = h.drive(t, m.ID, store.MachineDeleted, 8)
	if m.DeletedAt == nil {
		t.Fatal("a deleted machine has no confirmation stamp; only an inspect that could not find the resource writes one")
	}
	if h.callsTo("inspect") == 0 {
		t.Fatal("the machine was recorded deleted without the provider being asked whether it was gone")
	}
	for _, got := range h.fake.Machines() {
		if got.Ref.Name == m.Name {
			t.Fatalf("machine %s is recorded deleted and the provider still holds it", m.Name)
		}
	}
}

// readyMachine drives one machine all the way to ready, so that the tests about
// the second half of a machine's life do not each repeat the first half.
func (h *harness) readyMachine(t *testing.T, row *store.Provider) *store.Machine {
	t.Helper()
	h.machinePass(t)
	var m *store.Machine
	for _, got := range h.machines() {
		if got.ProviderID == row.ID && got.State != store.MachineDeleted {
			m = got
		}
	}
	if m == nil {
		t.Fatal("no machine was bought")
	}
	m = h.drive(t, m.ID, store.MachineEnrolling, 8)
	h.joinAsMachine(t, m)
	h.machinePass(t)
	return h.drive(t, m.ID, store.MachineReady, 3)
}

// beginDrainFor starts a drain the way a scale-down decision would.
func (h *harness) beginDrainFor(t *testing.T, m *store.Machine) {
	t.Helper()
	if _, err := h.st.TransitionMachine(h.ctx, m.ID, store.MachineDraining, "the test asked for it"); err != nil {
		t.Fatalf("TransitionMachine(draining): %v", err)
	}
}

// pauseProvider presses the operator's kill switch.
func (h *harness) pauseProvider(t *testing.T, row *store.Provider, reason string) {
	t.Helper()
	if err := h.st.SetProviderPaused(h.ctx, row.ID, true, reason); err != nil {
		t.Fatalf("SetProviderPaused: %v", err)
	}
}

// A machine with runners on it is not released, however idle the fleet thinks
// it is. The drain waits for the work, which is the difference between scaling
// down and killing somebody's build.
func TestADrainWaitsForTheRunnersOnTheMachine(t *testing.T) {
	h := newHarness(t)
	pool, row := h.machineFleet(t)
	m := h.readyMachine(t, row)

	host, err := h.st.GetHost(h.ctx, m.HostID)
	if err != nil {
		t.Fatalf("GetHost: %v", err)
	}
	h.seedRunner(t, pool, host, store.RunnerBusy)
	h.beginDrainFor(t, m)

	h.machinePass(t)
	if got := h.machineByID(t, m.ID); got.State != store.MachineDraining {
		t.Fatalf("a machine running a job is %s, want draining until the job finishes", got.State)
	}
	if h.callsTo("delete") != 0 {
		t.Fatal("a machine still running a job was deleted")
	}
}

// A provider kind this build has no driver for is refused with a sentence
// naming it, rather than being discovered inside the first create.
func TestAProviderThisBuildCannotMakeSaysSoRatherThanFailingLater(t *testing.T) {
	h := newHarness(t)
	h.enableProviders(t)
	row := &store.Provider{
		Kind: store.ProviderProxmox, Name: "pve", Endpoint: "https://pve.test:8006",
		MachineCapacity: 2, MachineBackend: store.BackendDocker,
		MaxMachines: 1, MaxCreatesInFlight: 1, Enabled: true,
	}
	if err := h.st.CreateProvider(h.ctx, row); err != nil {
		t.Fatalf("CreateProvider: %v", err)
	}

	h.machinePass(t)
	if !slices.Contains(h.problemCodes(), "provider.contract_unsupported") {
		t.Fatalf("a provider this build cannot make raised %v, want provider.contract_unsupported", h.problemCodes())
	}
}

// A provider whose machines no pool could ever use is money for nothing, and it
// is the one provider fault that looks exactly like nothing being wrong.
func TestAProviderNoPoolCouldUseSaysSo(t *testing.T) {
	h := newHarness(t)
	h.enableProviders(t)
	inst := h.installation()
	h.pool(inst, "linux-x64")
	row := h.providerRow(t, "lab")
	row.MachineBackend = store.BackendProcess
	row.MachineLabels = store.StringMap{"class": "gpu"}
	if err := h.st.UpdateProvider(h.ctx, row); err != nil {
		t.Fatalf("UpdateProvider: %v", err)
	}

	h.machinePass(t)
	if !slices.Contains(h.problemCodes(), "provider.unservable") {
		t.Fatalf("an unservable provider raised %v, want provider.unservable", h.problemCodes())
	}
}

// Every machine frame carries the resource's GET shape, because the UI drops it
// straight into its cache. A store row would repaint the machine with no
// provider name, no host name and no timeline.
func TestAMachineFrameCarriesTheShapeTheAPIReturns(t *testing.T) {
	h := newHarness(t)
	h.machineFleet(t)
	sub := h.listen(events.KindMachineUpdated)

	h.machinePass(t)
	frame := nextOfKind(t, sub, events.KindMachineUpdated)
	for _, field := range []string{"id", "provider_id", "provider_name", "name", "state", "timeline", "safe_to_delete"} {
		if _, ok := frame[field]; !ok {
			t.Errorf("a machine.updated frame has no %q; it is not the shape GET /machines/{id} returns", field)
		}
	}
	if _, leaked := frame["owner_fingerprint"]; leaked {
		t.Error("a machine frame carries the ownership fingerprint, which is the mark a delete is checked against")
	}
}

// A drain is an intent, not an act. Work that comes back before the delete
// starts takes the machine back: nothing has been deleted, its host is merely
// cordoned, and uncordoning costs nothing where building another costs minutes.
func TestWorkComingBackTakesADrainingMachineBack(t *testing.T) {
	h := newHarness(t)
	_, row := h.machineFleet(t)
	m := h.readyMachine(t, row)
	h.beginDrainFor(t, m)
	// A drain cordons the host first, which is what puts the pool back to
	// having nowhere to run: without that the fleet is not short of anything
	// and there is nothing for the machine to come back for.
	if err := h.st.SetHostCordoned(h.ctx, m.HostID, true); err != nil {
		t.Fatalf("SetHostCordoned: %v", err)
	}

	h.queueWork(t)
	h.machinePass(t)

	got := h.machineByID(t, m.ID)
	if got.State != store.MachineReady {
		t.Fatalf("a draining machine with work back for its pools is %s, want ready", got.State)
	}
	if len(h.machines()) != 1 {
		t.Fatalf("the fleet bought %d machines rather than taking back the one it was releasing", len(h.machines()))
	}
	host, err := h.st.GetHost(h.ctx, got.HostID)
	if err != nil {
		t.Fatalf("GetHost: %v", err)
	}
	if host.Cordoned {
		t.Fatal("the machine came back into service with its host still cordoned, so nothing can be placed on it")
	}
}

// A delete that has not confirmed is a resource somebody may still be paying
// for, so it is said out loud rather than assumed. Silence here is how a fleet
// stops counting a machine that is still running.
func TestADeleteThatHasNotConfirmedIsReported(t *testing.T) {
	h := newHarness(t)
	_, row := h.machineFleet(t)
	m := h.readyMachine(t, row)
	h.beginDrainFor(t, m)
	h.fake.SetFailure("delete", provider.FailureUnreachable, "dial tcp: i/o timeout")

	h.machinePass(t)
	if got := h.machineByID(t, m.ID); got.State != store.MachineDeleting {
		t.Fatalf("a machine whose delete could not be issued is %s, want deleting", got.State)
	}
	h.advance(h.cfg.Provider.DeleteTimeout + time.Minute)

	if !slices.Contains(h.problemCodes(), "provider.delete_pending") {
		t.Fatalf("a delete that has not confirmed raised %v, want provider.delete_pending", h.problemCodes())
	}
}
