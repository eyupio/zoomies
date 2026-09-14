package provider

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/eyupio/zoomies/internal/store"
)

// newFakeMachine mints an identity the way the controller does, so the fake's
// tests exercise the real name grammar a cluster-wide sweep filters on.
func newFakeMachine(t *testing.T) (Owner, MachineSpec) {
	t.Helper()
	owner, spec := newSpec()
	return owner, spec
}

func mustAllocate(t *testing.T, f Fake, spec MachineSpec) MachineRef {
	t.Helper()
	ref, err := f.Allocate(context.Background(), spec)
	if err != nil {
		t.Fatalf("Allocate: %v", err)
	}
	return ref
}

// The single most important thing the fake enforces: one identity is one
// machine. A fake that quietly built a second one would let every restart and
// ambiguity test in the controller pass while the real failure -- two machines,
// one row, one invoice -- went unnoticed.
func TestTheFakeBuildsOneMachinePerIdentityHoweverOftenCreateIsCalled(t *testing.T) {
	ctx := context.Background()
	f := NewFake()
	owner, spec := newFakeMachine(t)
	ref := mustAllocate(t, f, spec)

	for range 3 {
		if _, err := f.Create(ctx, ref, spec); err != nil {
			t.Fatalf("Create: %v", err)
		}
	}
	if got := len(f.Machines()); got != 1 {
		t.Fatalf("the fake holds %d machines after three creates for one ref", got)
	}
	ms, err := f.List(ctx, owner)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(ms) != 1 || ms[0].Ref != ref {
		t.Errorf("List = %+v, want the one machine at %+v", ms, ref)
	}
}

// Names are unique per provider because the name is the primary ownership mark:
// two machines answering to one name make a sweep's evidence meaningless.
func TestTheFakeRefusesASecondMachineWearingAnExistingName(t *testing.T) {
	ctx := context.Background()
	f := NewFake()
	_, spec := newFakeMachine(t)
	ref := mustAllocate(t, f, spec)
	if _, err := f.Create(ctx, ref, spec); err != nil {
		t.Fatalf("Create: %v", err)
	}
	_, err := f.Allocate(ctx, spec)
	if KindOf(err) != FailureConflict {
		t.Fatalf("Allocate for a name in use = %v, want a conflict", err)
	}
	if !errors.Is(err, ErrConflict) {
		t.Error("the refusal does not match ErrConflict")
	}
}

// Allocate mints identity and creates nothing. That ordering is what lets the
// controller write the identity down before anything can exist to be lost.
func TestAllocatingTwiceMintsTwoIdentitiesAndCreatesNothing(t *testing.T) {
	ctx := context.Background()
	f := NewFake()
	_, spec := newFakeMachine(t)
	first := mustAllocate(t, f, spec)
	second := mustAllocate(t, f, spec)
	if first.ID == second.ID {
		t.Errorf("two allocates returned one identity, %q", first.ID)
	}
	if got := len(f.Machines()); got != 0 {
		t.Fatalf("the fake holds %d machines after two allocates and no create", got)
	}
	if _, err := f.Inspect(ctx, first); !errors.Is(err, ErrNotFound) {
		t.Errorf("Inspect of an allocated-but-uncreated ref = %v, want not found", err)
	}
}

// The two knobs the whole design turns on. Both answers are identical to the
// caller and the truths behind them are opposite, which is exactly why a
// reconciler may not decide between them without looking.
func TestAnAmbiguousAnswerHidesBothOutcomesEquallyWell(t *testing.T) {
	for _, tc := range []struct {
		name      string
		arrange   func(Fake)
		wantFound bool
	}{
		{"the work happened and the answer was lost", func(f Fake) { f.SetAmbiguousAfterWork("create") }, true},
		{"nothing happened and the answer was lost", func(f Fake) { f.SetAmbiguousBeforeWork("create") }, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			f := NewFake()
			_, spec := newFakeMachine(t)
			ref := mustAllocate(t, f, spec)
			tc.arrange(f)

			op, err := f.Create(ctx, ref, spec)
			if KindOf(err) != FailureAmbiguous {
				t.Fatalf("Create = %v, want an ambiguous outcome", err)
			}
			if Retryable(err) {
				t.Error("an ambiguous create reads as retryable; the retry is what buys the second machine")
			}
			if !op.Zero() {
				t.Error("an ambiguous create handed back a handle; the point of the case is that there is nothing to follow")
			}
			_, err = f.Inspect(ctx, ref)
			if found := err == nil; found != tc.wantFound {
				t.Errorf("Inspect after the ambiguous create found the machine = %t, want %t (err %v)", found, tc.wantFound, err)
			}
		})
	}
}

// A quota is how the fake reproduces the refusal that is retryable but not
// solvable: there is no capacity, and asking again immediately only spends the
// next window on refusals.
func TestTheFakeStopsCreatingOnceItsQuotaIsSpent(t *testing.T) {
	ctx := context.Background()
	f := NewFake()
	f.SetQuota(2)
	for i := range 3 {
		_, spec := newFakeMachine(t)
		ref := mustAllocate(t, f, spec)
		_, err := f.Create(ctx, ref, spec)
		switch {
		case i < 2 && err != nil:
			t.Fatalf("create %d: %v", i, err)
		case i == 2:
			if KindOf(err) != FailureQuota {
				t.Fatalf("the third create = %v, want a quota refusal", err)
			}
			if !Retryable(err) {
				t.Error("a quota refusal is not retryable; capacity does come back")
			}
		}
	}
	// Somebody else's machines are not ours to be charged for.
	f.PlantForeign(store.NewMachineName(store.NewID(store.PrefixMachine)))
	if got := len(f.Machines()); got != 3 {
		t.Fatalf("the fake holds %d machines, want two of ours and one foreign", got)
	}
}

// A provider that ignores its deadline holds the reconcile pass open. The fake
// waits against the caller's context so a test can prove the deadline is the
// thing that ends the call.
func TestASlowCallIsEndedByTheCallersDeadlineAndNotItsOwn(t *testing.T) {
	f := NewFake()
	f.SetDelay("inspect", time.Minute)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()

	start := time.Now()
	_, err := f.Inspect(ctx, MachineRef{ID: "1", Name: "zoomies-mach-none"})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Inspect = %v, want the caller's deadline", err)
	}
	if elapsed := time.Since(start); elapsed > 10*time.Second {
		t.Fatalf("Inspect took %s; it served its own delay rather than the caller's deadline", elapsed)
	}
}

// An operation that finishes the instant it is issued cannot show the case that
// matters -- a create still running when the next pass comes round -- so the
// fake can hold one open for a counted number of polls.
func TestAnAsynchronousOperationIsUnfinishedUntilItHasBeenPolledEnough(t *testing.T) {
	ctx := context.Background()
	f := NewFake()
	f.SetAsync("create", 2)
	_, spec := newFakeMachine(t)
	ref := mustAllocate(t, f, spec)
	op, err := f.Create(ctx, ref, spec)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	for i := range 2 {
		st, err := f.Operation(ctx, op)
		if err != nil {
			t.Fatalf("poll %d: %v", i, err)
		}
		if st.Done {
			t.Fatalf("poll %d reported done; the create was asked to take two polls", i)
		}
	}
	st, err := f.Operation(ctx, op)
	if err != nil {
		t.Fatalf("Operation: %v", err)
	}
	if !st.Done || !st.OK {
		t.Fatalf("Operation = %+v, want a finished, successful create", st)
	}
	m, err := f.Inspect(ctx, ref)
	if err != nil {
		t.Fatalf("Inspect: %v", err)
	}
	if m.Phase != PhaseRunning {
		t.Errorf("phase = %q after the create finished, want %q", m.Phase, PhaseRunning)
	}

	// After a restart the controller re-asks about handles it stored, so a
	// finished operation has to keep answering, and one nobody issued has to
	// answer too.
	if again, err := f.Operation(ctx, op); err != nil || !again.Done {
		t.Errorf("re-polling a finished operation = %+v, %v", again, err)
	}
	if _, err := f.Operation(ctx, OperationRef{Kind: OpCreate, Handle: "never-issued"}); !errors.Is(err, ErrNotFound) {
		t.Errorf("Operation on an unknown handle = %v, want not found", err)
	}
}

// A machine somebody destroyed from a console is the case where the row and the
// world disagree, and the fake has to be able to produce it without a delete.
func TestAResourceThatVanishesIsNotFoundRatherThanStale(t *testing.T) {
	ctx := context.Background()
	f := NewFake()
	_, spec := newFakeMachine(t)
	ref := mustAllocate(t, f, spec)
	if _, err := f.Create(ctx, ref, spec); err != nil {
		t.Fatalf("Create: %v", err)
	}
	f.Vanish(ref.ID)
	if _, err := f.Inspect(ctx, ref); !errors.Is(err, ErrNotFound) {
		t.Errorf("Inspect after the machine vanished = %v, want not found", err)
	}
	// Deleting what is already gone is the desired end state, not a failure.
	op, err := f.Delete(ctx, ref)
	if err != nil || !op.Zero() {
		t.Errorf("Delete of a vanished machine = %+v, %v; want a zero operation and no error", op, err)
	}
}

// The sweep needs both halves: our own machines with no row, and somebody
// else's wearing our naming grammar. One is an orphan to report, the other is
// another fleet's machine to leave completely alone, and a List that returned
// only the first could not tell them apart.
func TestTheSweepSeesOurOrphansAndSomebodyElsesMachines(t *testing.T) {
	ctx := context.Background()
	f := NewFake()
	owner, spec := newFakeMachine(t)
	ref := mustAllocate(t, f, spec)
	if _, err := f.Create(ctx, ref, spec); err != nil {
		t.Fatalf("Create: %v", err)
	}
	orphanName := store.NewMachineName(store.NewID(store.PrefixMachine))
	orphanOwner := owner
	orphanOwner.MachineID = store.NewID(store.PrefixMachine)
	orphanOwner.Fingerprint = store.NewSecret(12)
	f.PlantOrphan(orphanName, orphanOwner)
	foreignName := store.NewMachineName(store.NewID(store.PrefixMachine))
	f.PlantForeign(foreignName)

	ms, err := f.List(ctx, owner)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(ms) != 3 {
		t.Fatalf("List returned %d machines, want ours, the orphan and the foreign one", len(ms))
	}
	for _, m := range ms {
		switch m.Ref.Name {
		case spec.Name:
			if !m.Owner.Matches(owner) {
				t.Errorf("our own machine does not carry our marks: %+v", m.Owner)
			}
		case orphanName:
			if m.Owner.MachineID == owner.MachineID {
				t.Error("the orphan carries the same machine mark as a machine we have a row for")
			}
		case foreignName:
			if m.Owner.Matches(owner) || m.Owner.Zero() {
				t.Errorf("a foreign machine must carry somebody else's marks, not ours and not none: %+v", m.Owner)
			}
		default:
			t.Errorf("List returned a machine nobody planted: %q", m.Ref.Name)
		}
	}
}

// A failure that keeps applying and one that applies once are different tests:
// the first is a provider that is down, the second is the flake that a single
// retry fixes.
func TestAFailureInjectedOnceAppliesOnce(t *testing.T) {
	ctx := context.Background()
	f := NewFake()
	f.SetFailureOnce("list", FailureUnreachable, "the connection was refused")
	if _, err := f.List(ctx, Owner{}); KindOf(err) != FailureUnreachable {
		t.Fatalf("the first List = %v, want unreachable", err)
	}
	if _, err := f.List(ctx, Owner{}); err != nil {
		t.Fatalf("the second List = %v, want the one-shot failure to be spent", err)
	}

	f.SetFailure("list", FailureAuth, "the token was rejected")
	for i := range 2 {
		if _, err := f.List(ctx, Owner{}); KindOf(err) != FailureAuth {
			t.Fatalf("List %d = %v, want the standing failure", i, err)
		}
	}
	f.ClearFailures()
	if _, err := f.List(ctx, Owner{}); err != nil {
		t.Fatalf("List after ClearFailures = %v", err)
	}
}

// Preflight reports and never fails, because an operator whose credential is
// wrong needs the page to say which credential and what to change -- not a
// status code from the one call that was meant to explain it.
func TestPreflightReportsAFailureAsSomethingToFix(t *testing.T) {
	f := NewFake()
	if r := f.Preflight(context.Background()); !r.OK() || r.Version == "" {
		t.Fatalf("a healthy preflight = %+v", r)
	}
	f.SetFailure("preflight", FailureAuth, "the token was rejected")
	r := f.Preflight(context.Background())
	if r.OK() {
		t.Fatal("a preflight that could not authenticate reads as OK")
	}
	if len(r.Findings) == 0 {
		t.Fatal("a failed preflight produced no findings; the page would have nothing to show")
	}
	if !strings.Contains(r.Findings[0].Detail, "rejected") {
		t.Errorf("the finding does not carry the provider's own words: %+v", r.Findings[0])
	}
}

// The fake exists to be asked what happened. Calls are what a "there was no
// second create" assertion reads, and payloads are kept whole because what a
// test wants to ask of one is what is NOT in it.
func TestTheFakeRecordsEveryCallAndKeepsEveryPayloadWhole(t *testing.T) {
	ctx := context.Background()
	f := NewFake()
	_, spec := newFakeMachine(t)
	ref := mustAllocate(t, f, spec)
	if _, err := f.Create(ctx, ref, spec); err != nil {
		t.Fatalf("Create: %v", err)
	}
	b, ok := f.(Bootstrapper)
	if !ok {
		t.Fatal("the default fake cannot bootstrap")
	}
	payload := Bootstrap{
		Files:    []File{{Path: "/etc/zoomies/zoomies.env", Mode: 0o600, Content: []byte("ZOOMIES_JOIN_TOKEN=join_abc\n")}},
		Commands: [][]string{{"/bin/chmod", "0600", "/etc/zoomies/zoomies.env"}},
	}
	if _, err := b.Bootstrap(ctx, ref, payload); err != nil {
		t.Fatalf("Bootstrap: %v", err)
	}

	calls := f.Calls()
	want := []string{"allocate " + spec.Name, "create " + spec.Name, "bootstrap " + spec.Name}
	if len(calls) != len(want) {
		t.Fatalf("Calls() = %v, want %v", calls, want)
	}
	for i := range want {
		if calls[i] != want[i] {
			t.Errorf("call %d = %q, want %q", i, calls[i], want[i])
		}
	}
	got := f.Bootstraps()
	if len(got) != 1 || len(got[0].Files) != 1 || string(got[0].Files[0].Content) != string(payload.Files[0].Content) {
		t.Fatalf("Bootstraps() = %+v, want the payload as it was sent", got)
	}
}

// A capability a provider does not have has to be absent from its type. If the
// fake kept the method and refused, every controller test would exercise a path
// that cannot exist in a real provider written to this contract.
func TestACapabilityTurnedOffIsAbsentFromTheTypeRatherThanRefusing(t *testing.T) {
	for _, tc := range []struct {
		name                  string
		opts                  []FakeOption
		power, boot, discover bool
	}{
		{"everything", nil, true, true, true},
		{"no power", []FakeOption{FakeWithoutPower()}, false, true, true},
		{"no bootstrap", []FakeOption{FakeWithoutBootstrap()}, true, false, true},
		{"no discovery", []FakeOption{FakeWithoutDiscovery()}, true, true, false},
		{"nothing optional at all", []FakeOption{FakeWithoutPower(), FakeWithoutBootstrap(), FakeWithoutDiscovery()}, false, false, false},
		{"power alone", []FakeOption{FakeWithoutBootstrap(), FakeWithoutDiscovery()}, true, false, false},
		{"bootstrap alone", []FakeOption{FakeWithoutPower(), FakeWithoutDiscovery()}, false, true, false},
		{"discovery alone", []FakeOption{FakeWithoutPower(), FakeWithoutBootstrap()}, false, false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := NewFake(tc.opts...)
			if _, ok := f.(PowerController); ok != tc.power {
				t.Errorf("implements PowerController = %t, want %t", ok, tc.power)
			}
			if _, ok := f.(Bootstrapper); ok != tc.boot {
				t.Errorf("implements Bootstrapper = %t, want %t", ok, tc.boot)
			}
			if _, ok := f.(Discoverer); ok != tc.discover {
				t.Errorf("implements Discoverer = %t, want %t", ok, tc.discover)
			}
			// And what it says about itself agrees with what it is, which is
			// the check the registry makes before handing it to anyone.
			if err := CheckCapabilities(f); err != nil {
				t.Errorf("%v", err)
			}
		})
	}
}

// Stopping a machine instead of destroying it is the only thing a fake with
// power can do that one without cannot, so it is worth proving it works rather
// than only that the method is there.
func TestAFakeThatCanStopAMachineKeepsItAfterStopping(t *testing.T) {
	ctx := context.Background()
	f := NewFake()
	_, spec := newFakeMachine(t)
	ref := mustAllocate(t, f, spec)
	op, err := f.Create(ctx, ref, spec)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if _, err := f.Operation(ctx, op); err != nil {
		t.Fatalf("Operation: %v", err)
	}
	power, ok := f.(PowerController)
	if !ok {
		t.Fatal("the default fake cannot stop a machine")
	}
	stop, err := power.Stop(ctx, ref, true)
	if err != nil {
		t.Fatalf("Stop: %v", err)
	}
	if _, err := f.Operation(ctx, stop); err != nil {
		t.Fatalf("Operation: %v", err)
	}
	m, err := f.Inspect(ctx, ref)
	if err != nil {
		t.Fatalf("Inspect: %v", err)
	}
	if m.Phase != PhaseStopped {
		t.Errorf("phase = %q after a stop, want %q: a stop that destroyed the machine is a delete", m.Phase, PhaseStopped)
	}
	start, err := power.Start(ctx, ref)
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	if _, err := f.Operation(ctx, start); err != nil {
		t.Fatalf("Operation: %v", err)
	}
	if m, _ := f.Inspect(ctx, ref); m.Phase != PhaseRunning {
		t.Errorf("phase = %q after a start, want %q", m.Phase, PhaseRunning)
	}
}
