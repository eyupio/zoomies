package provider

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/eyupio/zoomies/internal/config"
	"github.com/eyupio/zoomies/internal/store"
)

// RunContractTests exercises every rule the contract states against any
// implementation.
//
// It is exported from the production package, rather than being a test helper
// somewhere, so that a provider's own test file is one line and the suite is
// the definition of done -- including for a provider written outside this
// package, which cannot import another package's tests.
//
// open is called once per case and returns a provider pointed at whatever that
// provider needs: the fake needs nothing, and a real one is expected to skip
// itself when its infrastructure is not there. Every case cleans up the
// machines it made, because the second thing a suite like this must not do is
// leave a resource behind on somebody's cluster; the first is create one it
// cannot find again.
func RunContractTests(t *testing.T, name string, open func(*testing.T) Provider) {
	t.Helper()
	t.Run(name, func(t *testing.T) {
		t.Run("what it says it can do is what its type can do", func(t *testing.T) {
			p := open(t)
			if err := CheckCapabilities(p); err != nil {
				t.Errorf("%v", err)
			}
			if p.Kind() != p.Capabilities().Kind {
				t.Errorf("Kind() = %q but Capabilities().Kind = %q; they name the same provider",
					p.Kind(), p.Capabilities().Kind)
			}
			if !p.Kind().Valid() {
				t.Errorf("Kind() = %q, which is not a kind the store can hold: no row could ever select this provider", p.Kind())
			}
		})

		t.Run("its contract range contains the version this build speaks", func(t *testing.T) {
			p := open(t)
			if err := checkContract(p.Kind(), p.Capabilities()); err != nil {
				t.Errorf("%v", err)
			}
		})

		// A deadline that is zero is a call with no supervision, and one that
		// gives a whole create less time than a single request is a create that
		// is cancelled halfway by arithmetic rather than by a decision.
		t.Run("its deadlines are positive and grow with the work they cover", func(t *testing.T) {
			p := open(t)
			d := p.Capabilities().Deadlines
			for _, c := range []struct {
				name string
				d    time.Duration
			}{
				{"Call", d.Call}, {"Allocate", d.Allocate}, {"Create", d.Create}, {"Delete", d.Delete},
			} {
				if c.d <= 0 {
					t.Errorf("Deadlines.%s = %s; a call with no deadline can wedge the fleet", c.name, c.d)
				}
			}
			if d.Call > d.Allocate {
				t.Errorf("Deadlines.Call = %s is longer than Allocate = %s", d.Call, d.Allocate)
			}
			if d.Allocate > d.Create {
				t.Errorf("Deadlines.Allocate = %s is longer than Create = %s", d.Allocate, d.Create)
			}
			if _, ok := p.(Bootstrapper); ok && d.Bootstrap <= 0 {
				t.Errorf("Deadlines.Bootstrap = %s on a provider that bootstraps", d.Bootstrap)
			}
		})

		// Preflight is the call an operator runs against a provider that is not
		// working. Returning an error instead of findings would leave the page
		// with a status code where the remedy should be.
		t.Run("preflight reports what to fix rather than failing", func(t *testing.T) {
			p := open(t)
			ctx, cancel := contextFor(t, p.Capabilities().Deadlines.Call)
			defer cancel()
			r := p.Preflight(ctx)
			for i, f := range r.Findings {
				switch {
				case f.Code == "":
					t.Errorf("finding %d has no code; nothing can document or suppress it", i)
				case f.Title == "":
					t.Errorf("finding %s has no title", f.Code)
				case f.Severity != config.SeverityError && f.Severity != config.SeverityWarning && f.Severity != config.SeverityInfo:
					t.Errorf("finding %s has severity %q, which is not one of the three", f.Code, f.Severity)
				}
			}
		})

		// The rule the whole design rests on: identity before existence. A
		// provider that creates during Allocate leaves the controller with a
		// resource it has not written down.
		t.Run("allocate picks an identity and creates nothing", func(t *testing.T) {
			p := open(t)
			ctx, cancel := contextFor(t, p.Capabilities().Deadlines.Allocate)
			defer cancel()
			owner, spec := newSpec()
			ref, err := p.Allocate(ctx, spec)
			if err != nil {
				t.Fatalf("Allocate: %v", err)
			}
			if ref.Zero() {
				t.Fatal("Allocate returned a ref naming nothing; there is no identity to persist")
			}
			if ref.Name != spec.Name {
				t.Errorf("Allocate returned name %q for a machine the caller named %q", ref.Name, spec.Name)
			}
			if _, err := p.Inspect(ctx, ref); !errors.Is(err, ErrNotFound) {
				t.Errorf("Inspect after Allocate = %v, want not found: an allocate that created something is an unrecorded machine", err)
			}
			ms, err := p.List(ctx, owner)
			if err != nil {
				t.Fatalf("List: %v", err)
			}
			if n := countNamed(ms, spec.Name); n != 0 {
				t.Errorf("List found %d machines named %q after an allocate alone", n, spec.Name)
			}
		})

		// A create that is re-issued after an outcome nobody heard must not
		// buy a second machine. This is the property that makes "a timeout is
		// not evidence that creation failed" safe to act on.
		t.Run("creating twice for one identity builds one machine", func(t *testing.T) {
			p := open(t)
			ctx, cancel := contextFor(t, p.Capabilities().Deadlines.Create)
			defer cancel()
			owner, spec := newSpec()
			ref := mustCreate(t, ctx, p, spec)
			if _, err := p.Create(ctx, ref, spec); err != nil {
				t.Fatalf("second Create for the same ref: %v", err)
			}
			ms, err := p.List(ctx, owner)
			if err != nil {
				t.Fatalf("List: %v", err)
			}
			if n := countNamed(ms, spec.Name); n != 1 {
				t.Errorf("List found %d machines named %q after two creates for one ref; a second machine is a second bill nobody is tracking", n, spec.Name)
			}
		})

		t.Run("a machine it created is in the list for its owner", func(t *testing.T) {
			p := open(t)
			ctx, cancel := contextFor(t, p.Capabilities().Deadlines.Create)
			defer cancel()
			owner, spec := newSpec()
			mustCreate(t, ctx, p, spec)
			ms, err := p.List(ctx, owner)
			if err != nil {
				t.Fatalf("List: %v", err)
			}
			if countNamed(ms, spec.Name) != 1 {
				t.Errorf("a machine this owner created is not in its own sweep; nothing would ever find it again")
			}
			for _, m := range ms {
				if m.Ref.Name != spec.Name {
					continue
				}
				if !m.Phase.Known() {
					t.Errorf("phase %q is not one the contract defines; an unrecognised phase is read as unknown, which authorises nothing", m.Phase)
				}
				if p.Capabilities().CanMarkOwnership && !m.Owner.Matches(owner) {
					t.Errorf("the marks read back from the resource are not the ones we wrote: %+v", m.Owner)
				}
			}
		})

		// "Already deleted" is the desired end state, not a failure, and the
		// two answers below are what a delete that crossed with somebody's
		// console gets.
		t.Run("deleting a machine that is already gone is success", func(t *testing.T) {
			p := open(t)
			ctx, cancel := contextFor(t, p.Capabilities().Deadlines.Delete)
			defer cancel()
			_, spec := newSpec()
			ref := mustCreate(t, ctx, p, spec)
			deleteMachine(t, ctx, p, ref)

			op, err := p.Delete(ctx, ref)
			if err != nil {
				t.Errorf("Delete of a machine that is gone = %v, want no error", err)
			}
			if !op.Zero() {
				t.Errorf("Delete of a machine that is gone returned handle %q; there is nothing to follow", op.Handle)
			}
			if _, err := p.Inspect(ctx, ref); !errors.Is(err, ErrNotFound) {
				t.Errorf("Inspect of a deleted machine = %v, want not found: a delete is not complete until an inspect cannot find it", err)
			}
		})

		// After a restart the controller asks about handles the provider may
		// have forgotten, and it must get an answer rather than a panic.
		t.Run("an operation handle it never issued is not found", func(t *testing.T) {
			p := open(t)
			ctx, cancel := contextFor(t, p.Capabilities().Deadlines.Call)
			defer cancel()
			_, err := p.Operation(ctx, OperationRef{Kind: OpCreate, Handle: "handle-that-was-never-issued"})
			if !errors.Is(err, ErrNotFound) {
				t.Errorf("Operation on an unknown handle = %v, want not found", err)
			}
		})

		// A provider that ignores its context is a provider that holds the
		// reconcile pass open for as long as its own timeouts allow.
		t.Run("every call honours a context that is already cancelled", func(t *testing.T) {
			p := open(t)
			ctx, cancel := context.WithCancel(context.Background())
			cancel()
			_, spec := newSpec()
			ref := MachineRef{Zone: "zone", ID: "1", Name: spec.Name}
			calls := map[string]func() error{
				"Allocate":  func() error { _, err := p.Allocate(ctx, spec); return err },
				"Create":    func() error { _, err := p.Create(ctx, ref, spec); return err },
				"Inspect":   func() error { _, err := p.Inspect(ctx, ref); return err },
				"List":      func() error { _, err := p.List(ctx, spec.Owner); return err },
				"Delete":    func() error { _, err := p.Delete(ctx, ref); return err },
				"Operation": func() error { _, err := p.Operation(ctx, OperationRef{Kind: OpCreate, Handle: "h"}); return err },
			}
			if pc, ok := p.(PowerController); ok {
				calls["Start"] = func() error { _, err := pc.Start(ctx, ref); return err }
				calls["Stop"] = func() error { _, err := pc.Stop(ctx, ref, true); return err }
			}
			if b, ok := p.(Bootstrapper); ok {
				calls["Bootstrap"] = func() error { _, err := b.Bootstrap(ctx, ref, Bootstrap{}); return err }
			}
			if d, ok := p.(Discoverer); ok {
				calls["Discover"] = func() error { _, err := d.Discover(ctx); return err }
			}
			for name, call := range calls {
				if err := call(); err == nil {
					t.Errorf("%s returned no error for a cancelled context", name)
				}
			}
		})

		// The forward-compatibility rule, checked here because it is the one a
		// provider can break by inventing a phase of its own.
		t.Run("a phase this build does not know is unknown, and unknown is not gone", func(t *testing.T) {
			if got := ParsePhase("a-phase-from-a-later-contract"); got != PhaseUnknown {
				t.Errorf("ParsePhase of an unrecognised phase = %q, want %q", got, PhaseUnknown)
			}
			if PhaseUnknown.Gone() {
				t.Error("an unknown phase reads as gone; a machine we failed to read would be recorded as deleted")
			}
			if PhaseUnknown.AuthorisesDelete() {
				t.Error("an unknown phase authorises a delete; we would destroy a machine we could not identify")
			}
		})
	})
}

// contextFor bounds a case by the provider's own budget for the call, so that a
// suite run against real infrastructure fails with a deadline rather than
// hanging a CI job.
func contextFor(t *testing.T, d time.Duration) (context.Context, context.CancelFunc) {
	t.Helper()
	if d <= 0 {
		d = time.Minute
	}
	return context.WithTimeout(context.Background(), d)
}

// newSpec mints one machine's identity the way the controller does, so that the
// suite exercises the real name grammar a sweep filters on.
func newSpec() (Owner, MachineSpec) {
	id := store.NewID(store.PrefixMachine)
	owner := Owner{
		ControllerID: store.NewID(store.PrefixController),
		ProviderID:   store.NewID(store.PrefixProvider),
		MachineID:    id,
		Fingerprint:  store.NewSecret(12),
		CreatedAt:    time.Now().UTC(),
	}
	return owner, MachineSpec{
		Name:  store.NewMachineName(id),
		Owner: owner,
		Shape: Shape{CPUs: 2, MemoryMB: 2048, DiskMB: 20480, Platform: store.Platform{OS: "ubuntu", OSVersion: "24.04", Arch: "amd64"}},
	}
}

// mustCreate allocates, creates and waits, and registers the cleanup before the
// machine exists -- the same ordering the controller uses, and for the same
// reason: a resource created before anything was written down is a resource
// nothing will clean up.
func mustCreate(t *testing.T, ctx context.Context, p Provider, spec MachineSpec) MachineRef {
	t.Helper()
	ref, err := p.Allocate(ctx, spec)
	if err != nil {
		t.Fatalf("Allocate: %v", err)
	}
	t.Cleanup(func() {
		cleanup, cancel := contextFor(t, p.Capabilities().Deadlines.Delete)
		defer cancel()
		if op, err := p.Delete(cleanup, ref); err == nil {
			awaitOp(t, cleanup, p, op)
		}
	})
	op, err := p.Create(ctx, ref, spec)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	awaitOp(t, ctx, p, op)
	return ref
}

func deleteMachine(t *testing.T, ctx context.Context, p Provider, ref MachineRef) {
	t.Helper()
	op, err := p.Delete(ctx, ref)
	if err != nil {
		t.Fatalf("Delete: %v", err)
	}
	awaitOp(t, ctx, p, op)
}

// awaitOp follows an operation to its end. A zero handle is a synchronous
// provider's answer and there is nothing to follow.
func awaitOp(t *testing.T, ctx context.Context, p Provider, op OperationRef) {
	t.Helper()
	if op.Zero() {
		return
	}
	for {
		st, err := p.Operation(ctx, op)
		if err != nil {
			t.Fatalf("Operation(%s): %v", op.Handle, err)
		}
		if st.Done {
			if !st.OK {
				t.Fatalf("operation %s finished badly: %s", op.Handle, st.Detail)
			}
			return
		}
		select {
		case <-ctx.Done():
			t.Fatalf("operation %s did not finish within the provider's own deadline: %s", op.Handle, st.Detail)
		case <-time.After(10 * time.Millisecond):
		}
	}
}

func countNamed(ms []Machine, name string) int {
	n := 0
	for _, m := range ms {
		if m.Ref.Name == name && !m.Phase.Gone() {
			n++
		}
	}
	return n
}
