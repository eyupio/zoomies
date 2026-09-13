package store

import (
	"context"
	"testing"
	"time"
)

// The three predicates the fleet's arithmetic rests on.
//
// Pending and Offers are read by the machine-demand calculation to decide how
// much capacity is already on its way, so an error here is not a wrong label:
// it is a fleet that buys a second machine because it forgot about the first,
// or one that never buys because it counted a machine that will never arrive.
// They are spelled out state by state rather than derived, because deriving
// them from the lifecycle order is exactly the change that would silently
// reclassify a state somebody adds later.
func TestWhatEachMachineStateCountsAs(t *testing.T) {
	for _, tc := range []struct {
		state    MachineState
		pending  bool
		offers   bool
		terminal bool
	}{
		// Nothing exists yet, but something is coming: a reservation counts.
		{MachinePlanned, true, true, false},
		{MachineCreating, true, true, false},
		{MachineStarting, true, true, false},
		{MachineBootstrapping, true, true, false},
		{MachineEnrolling, true, true, false},
		// It has arrived, so its capacity is a host row rather than a promise.
		{MachineReady, false, true, false},
		// On its way out: its slots are being given up, so counting them would
		// keep a shortfall closed with capacity that is leaving.
		{MachineDraining, false, false, false},
		{MachineDeleting, false, false, false},
		// Gone, and the only state that is finished with.
		{MachineDeleted, false, false, true},
		// Both of these still need attention and may still have a resource
		// behind them, which is why neither is terminal.
		{MachineFailed, false, false, false},
		{MachineQuarantined, false, false, false},
	} {
		t.Run(string(tc.state), func(t *testing.T) {
			if !tc.state.Valid() {
				t.Fatalf("%s is not a state the store recognises", tc.state)
			}
			if got := tc.state.Pending(); got != tc.pending {
				t.Errorf("Pending() = %v, want %v", got, tc.pending)
			}
			if got := tc.state.Offers(); got != tc.offers {
				t.Errorf("Offers() = %v, want %v", got, tc.offers)
			}
			if got := tc.state.Terminal(); got != tc.terminal {
				t.Errorf("Terminal() = %v, want %v", got, tc.terminal)
			}
			// Pending capacity is capacity on offer. A state that was pending
			// and not offering would be one the fleet waits for and never
			// counts, which is how a queue stalls behind machines it already
			// has.
			if tc.state.Pending() && !tc.state.Offers() {
				t.Error("a pending machine must count towards what the fleet has coming")
			}
		})
	}

	if MachineState("melting").Valid() {
		t.Error("an unknown state was accepted; the CHECK constraint and this list would then disagree")
	}
}

// The two small enums the operation columns are constrained to. An unknown
// value must not be accepted here, because the database's CHECK constraint
// would refuse the write afterwards and the refusal would name a column rather
// than the thing that was wrong.
func TestOnlyRealOperationsAndOutcomesAreAccepted(t *testing.T) {
	for _, k := range []MachineOpKind{MachineOpCreate, MachineOpStart, MachineOpStop, MachineOpBootstrap, MachineOpDelete} {
		if !k.Valid() {
			t.Errorf("%s is not accepted as an operation", k)
		}
	}
	if MachineOpKind("reboot").Valid() {
		t.Error("an operation the machine cannot be doing was accepted")
	}
	if !MachineOpNone.Valid() {
		t.Error("the absent operation is what an idle machine's column holds and has to be legal")
	}

	for _, o := range []MachineOpOutcome{MachineOpSucceeded, MachineOpFailed, MachineOpReleased} {
		if !o.Valid() {
			t.Errorf("%s is not accepted as an outcome", o)
		}
	}
	if MachineOpOutcome("probably").Valid() {
		t.Error("an outcome nothing can act on was accepted")
	}
	// Releasing a claim is not a verdict, so it must be a legal outcome: a
	// guard that refused after the claim was taken has to hand the machine
	// back without counting an attempt against it.
	if !MachineOpReleased.Valid() {
		t.Error("giving a claim back is not accepted as an outcome")
	}
}

// The three lookups the reconciler reaches for: by the resource it believes it
// owns, by provider, and the idle clock it stamps.
//
// Finding a machine by its resource is how an ambiguous create is resolved --
// the identity was written before anything was built, so the question after a
// lost response is "whose is this?" rather than "make another".
func TestAMachineIsFoundByTheResourceItNames(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	p := seedProvider(t, s)

	m := seedMachine(t, s, p.ID, MachinePlanned)
	if err := s.SetMachineResource(ctx, m.ID, "pve-1", "143", "fingerprint", "ctl_one"); err != nil {
		t.Fatalf("SetMachineResource: %v", err)
	}

	found, err := s.GetMachineByResource(ctx, p.ID, "pve-1", "143")
	if err != nil {
		t.Fatalf("GetMachineByResource: %v", err)
	}
	if found.ID != m.ID {
		t.Errorf("found %s, want %s", found.ID, m.ID)
	}

	// A resource on another node is a different resource, however familiar the
	// identifier looks: VM 143 exists on every node of a cluster.
	if _, err := s.GetMachineByResource(ctx, p.ID, "pve-2", "143"); err == nil {
		t.Error("a machine on one node answered for the same id on another")
	}
	if _, err := s.GetMachineByResource(ctx, p.ID, "pve-1", "144"); err == nil {
		t.Error("a machine answered for an id it does not have")
	}
}

// What a pass reads is every machine of one provider that it can still do
// something about -- which is all of them except the ones already confirmed
// gone. A deleted machine's row is kept for the record of what was rented and
// what it cost, but putting it in front of the reconcile pass on every tick
// would mean the list grows without bound on a fleet that is working normally.
//
// A machine belonging to another provider is never in it. Two providers are
// two hypervisors, and a pass that mixed them would ask one of them about a
// resource the other owns.
func TestAPassSeesEveryMachineItCanStillActOnAndNoOthers(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	mine := seedProvider(t, s)
	theirs := &Provider{Kind: ProviderFake, Name: "another-lab", MachineCapacity: 2,
		MachineBackend: BackendDocker, MaxMachines: 4, MaxCreatesInFlight: 1, Enabled: true}
	if err := s.CreateProvider(ctx, theirs); err != nil {
		t.Fatalf("CreateProvider: %v", err)
	}

	want := map[string]bool{}
	for _, state := range []MachineState{MachinePlanned, MachineReady, MachineQuarantined, MachineFailed} {
		want[seedMachine(t, s, mine.ID, state).ID] = true
	}
	// Neither of these may appear: one is finished with, the other is not ours.
	gone := seedMachine(t, s, mine.ID, MachineDeleted)
	elsewhere := seedMachine(t, s, theirs.ID, MachineReady)

	got, err := s.ListMachinesForProvider(ctx, mine.ID)
	if err != nil {
		t.Fatalf("ListMachinesForProvider: %v", err)
	}
	if len(got) != len(want) {
		t.Fatalf("listed %d machines, want %d; every pass reads this list and a missing row is a machine "+
			"nothing is tending", len(got), len(want))
	}
	for _, m := range got {
		switch m.ID {
		case gone.ID:
			t.Error("a machine already confirmed gone is still put in front of every pass")
		case elsewhere.ID:
			t.Error("another provider's machine was listed for this one")
		default:
			if !want[m.ID] {
				t.Errorf("%s was listed and is not one of this provider's", m.ID)
			}
		}
	}
	// A failed or quarantined machine still needs attention -- and may still
	// have a resource costing money behind it -- so neither is filtered out
	// with the deleted ones.
	if len(want) != 4 {
		t.Fatal("the test no longer covers the two states that are finished with in the fleet's eyes but not in the provider's")
	}
}

// The idle clock is stamped once when a machine's host empties and cleared the
// moment a runner lands on it. Clearing matters more than stamping: a machine
// that took work and kept a stale idle stamp would be drained out from under
// the job it had just been given.
func TestTheIdleClockIsClearedTheMomentWorkArrives(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	p := seedProvider(t, s)
	m := seedMachine(t, s, p.ID, MachineReady)

	since := s.Now().Add(-20 * time.Minute).UTC().Truncate(time.Millisecond)
	if err := s.SetMachineIdleSince(ctx, m.ID, &since); err != nil {
		t.Fatalf("SetMachineIdleSince: %v", err)
	}
	got, err := s.GetMachine(ctx, m.ID)
	if err != nil {
		t.Fatalf("GetMachine: %v", err)
	}
	if got.IdleSince == nil || !got.IdleSince.Equal(since) {
		t.Fatalf("idle_since = %v, want %s", got.IdleSince, since)
	}

	if err := s.SetMachineIdleSince(ctx, m.ID, nil); err != nil {
		t.Fatalf("clearing SetMachineIdleSince: %v", err)
	}
	if got, err = s.GetMachine(ctx, m.ID); err != nil {
		t.Fatalf("GetMachine: %v", err)
	}
	if got.IdleSince != nil {
		t.Errorf("idle_since = %v after work arrived, want it cleared", got.IdleSince)
	}
}
