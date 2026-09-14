package provider

import (
	"context"
	"fmt"
	"slices"
	"sync"
	"time"

	"github.com/eyupio/zoomies/internal/config"
	"github.com/eyupio/zoomies/internal/store"
)

// The fake provider lives in the production package rather than a _test.go
// file, for the reason github.FakeGitHub does: the store, controller and API
// tests all need a provider to run the whole system against, and three subtly
// different fakes would be worse than one honest one. "Honest" means it
// enforces the rules that actually bite -- a name is unique, one ref is one
// machine however many times Create is called, a resource that is gone is gone
// -- so a test that passes against it has some claim to passing against real
// infrastructure.
//
// It is in-memory rather than an HTTP server on purpose. The wire is not what
// is under test here; the reconciler's behaviour around ambiguity, restart and
// ownership is. A provider's own HTTP-level fake belongs to that provider's
// package, where the wire IS what is under test.
//
// Its limitations, so that a test does not read more into a pass than is there:
//
//   - Time does not pass. SetDelay is the only wait, and nothing ages: an idle
//     machine never becomes idle by itself.
//   - Nothing is durable. A fake is one test's worth of state; a restart in a
//     test restarts the controller around a fake that remembers everything, as
//     real infrastructure would.
//   - Concurrency is serialised behind one mutex, so it can show a reconciler
//     exceeding a create limit but cannot reproduce a hypervisor's own races.
//   - Costs, addresses and versions are made up, and no bytes ever leave the
//     process.

// Fake is an in-memory provider with knobs for the cases that are hard to reach
// against real infrastructure.
//
// It is an interface rather than a struct because the optional capabilities
// have to be ABSENT FROM THE TYPE when they are turned off -- that is the rule
// the contract states, and a struct with a flag on it would satisfy
// PowerController whatever the flag said, which is precisely the "no
// PowerController" path the reconciler needs exercised.
type Fake interface {
	Provider

	// Machines is every machine the fake is holding, live ones only.
	Machines() []Machine
	// Calls is every call made to the fake, in order, as "create <name>". It is
	// what a call-budget assertion and a "there was no second create" assertion
	// read.
	Calls() []string
	// Bootstraps is every payload pushed into a guest, kept whole so that a
	// test can scan every byte of one for a credential that should never have
	// been in it.
	Bootstraps() []Bootstrap

	// PlantOrphan puts a machine wearing our marks behind the fake's back: a
	// resource with no row, which the ownership sweep must report and never
	// delete.
	PlantOrphan(name string, owner Owner)
	// PlantForeign puts a machine wearing our naming grammar and somebody
	// else's marks: another fleet's machine, which must be quarantined rather
	// than adopted or deleted.
	PlantForeign(name string)
	// Vanish removes a resource without a delete, the way a person with a
	// console does.
	Vanish(machineID string)

	// SetFailure makes every call to an operation fail; SetFailureOnce makes
	// the next one fail. The operation names are the method names in lower
	// case: "allocate", "create", "inspect", "list", "delete", "operation",
	// "start", "stop", "bootstrap", "discover", "preflight".
	SetFailure(op string, k FailureKind, message string)
	SetFailureOnce(op string, k FailureKind, message string)
	ClearFailures()
	// SetQuota caps how many machines the fake will hold; creates past it are
	// refused with FailureQuota.
	SetQuota(max int)
	// SetDelay makes an operation take time, honoured against ctx.Done() so a
	// test can prove a deadline is enforced.
	SetDelay(op string, d time.Duration)
	// SetAsync makes an operation's handle stay unfinished for n polls, which
	// is how a create that takes minutes is reproduced in microseconds.
	SetAsync(op string, polls int)

	// SetAmbiguousAfterWork does the work and then answers FailureAmbiguous:
	// the request went out, the machine exists, and we never heard. It is the
	// case the whole design turns on, because retrying it buys a second
	// machine.
	SetAmbiguousAfterWork(op string)
	// SetAmbiguousBeforeWork answers FailureAmbiguous having done nothing: the
	// same answer, the opposite truth. A reconciler that can tell these two
	// apart without asking the provider is guessing.
	SetAmbiguousBeforeWork(op string)
}

// FakeOption turns off one of the fake's optional capabilities.
type FakeOption func(*fakeOptions)

type fakeOptions struct{ power, bootstrap, discovery bool }

// FakeWithoutPower builds a fake that cannot stop a machine without destroying
// it, so the reconciler's "delete rather than stop" path is exercised.
func FakeWithoutPower() FakeOption { return func(o *fakeOptions) { o.power = false } }

// FakeWithoutBootstrap builds a fake that cannot push the enrolment payload
// into the guest, and declares BootstrapMetadata instead.
func FakeWithoutBootstrap() FakeOption { return func(o *fakeOptions) { o.bootstrap = false } }

// FakeWithoutDiscovery builds a fake whose configuration form has to ask for
// identifiers rather than offer them.
func FakeWithoutDiscovery() FakeOption { return func(o *fakeOptions) { o.discovery = false } }

// NewFake returns a fake provider carrying every capability except the ones the
// options took away. What it returns is a different concrete type for each set
// of capabilities, which is the only way an absent capability can be genuinely
// absent.
func NewFake(opts ...FakeOption) Fake {
	o := fakeOptions{power: true, bootstrap: true, discovery: true}
	for _, opt := range opts {
		opt(&o)
	}
	c := newFakeCore(o)
	p, b, d := powerOps{c}, bootstrapOps{c}, discoverOps{c}
	switch {
	case o.power && o.bootstrap && o.discovery:
		return fakeAll{c, p, b, d}
	case o.power && o.bootstrap:
		return fakePowerBootstrap{c, p, b}
	case o.power && o.discovery:
		return fakePowerDiscovery{c, p, d}
	case o.bootstrap && o.discovery:
		return fakeBootstrapDiscovery{c, b, d}
	case o.power:
		return fakePower{c, p}
	case o.bootstrap:
		return fakeBootstrap{c, b}
	case o.discovery:
		return fakeDiscovery{c, d}
	default:
		return c
	}
}

// The capability sets, one concrete type each. They carry no logic: the
// embedded *fakeCore answers Provider and the knobs at depth one, which
// shadows the same methods reached through the operation helpers, and each
// helper contributes exactly the optional methods its capability names.
type (
	fakeAll struct {
		*fakeCore
		powerOps
		bootstrapOps
		discoverOps
	}
	fakePowerBootstrap struct {
		*fakeCore
		powerOps
		bootstrapOps
	}
	fakePowerDiscovery struct {
		*fakeCore
		powerOps
		discoverOps
	}
	fakeBootstrapDiscovery struct {
		*fakeCore
		bootstrapOps
		discoverOps
	}
	fakePower struct {
		*fakeCore
		powerOps
	}
	fakeBootstrap struct {
		*fakeCore
		bootstrapOps
	}
	fakeDiscovery struct {
		*fakeCore
		discoverOps
	}
)

type powerOps struct{ *fakeCore }
type bootstrapOps struct{ *fakeCore }
type discoverOps struct{ *fakeCore }

// fakeMachine is one machine the fake is holding.
type fakeMachine struct {
	ref     MachineRef
	phase   Phase
	owner   Owner
	address string
	detail  string
	// planted marks a machine that arrived behind the fake's back, so that a
	// foreign fleet's machines do not eat the quota we set for ours.
	planted bool
}

// fakeOp is one asynchronous operation. apply is the effect the operation has
// when it finishes, held until then so that a create in flight is a machine
// that exists and is not yet running.
type fakeOp struct {
	kind   OpKind
	polls  int
	ok     bool
	detail string
	apply  func()
}

type fakeFailure struct {
	kind    FailureKind
	message string
	once    bool
}

// fakeAmbiguity is which side of the work an operation loses its answer on.
type fakeAmbiguity int

const (
	ambiguousAfterWork fakeAmbiguity = iota + 1
	ambiguousBeforeWork
)

type fakeCore struct {
	caps Capabilities
	zone string

	mu         sync.Mutex
	nextID     int
	nextOp     int
	machines   []*fakeMachine
	ops        map[string]*fakeOp
	calls      []string
	bootstraps []Bootstrap
	failures   map[string]*fakeFailure
	ambiguous  map[string]fakeAmbiguity
	delays     map[string]time.Duration
	async      map[string]int
	quota      int
}

func newFakeCore(o fakeOptions) *fakeCore {
	boot := BootstrapMetadata
	if o.bootstrap {
		boot = BootstrapGuestAgent
	}
	return &fakeCore{
		caps: Capabilities{
			MinContract:      ContractVersion,
			MaxContract:      ContractVersion,
			Kind:             store.ProviderFake,
			Label:            "Fake provider",
			Bootstrap:        boot,
			CanStartStop:     o.power,
			CanDiscover:      o.discovery,
			CanMarkOwnership: true,
			AsyncOperations:  true,
			CostUnit:         "machine-hour",
			Deadlines: Deadlines{
				Call:      5 * time.Second,
				Allocate:  5 * time.Second,
				Create:    30 * time.Second,
				Bootstrap: 30 * time.Second,
				Delete:    30 * time.Second,
			},
		},
		zone:      "zone-a",
		ops:       map[string]*fakeOp{},
		failures:  map[string]*fakeFailure{},
		ambiguous: map[string]fakeAmbiguity{},
		delays:    map[string]time.Duration{},
		async:     map[string]int{},
	}
}

func (f *fakeCore) Kind() store.ProviderKind   { return f.caps.Kind }
func (f *fakeCore) Capabilities() Capabilities { return f.caps }

// ---------------------------------------------------------------------------
// The knobs
// ---------------------------------------------------------------------------

func (f *fakeCore) Machines() []Machine {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]Machine, 0, len(f.machines))
	for _, m := range f.machines {
		out = append(out, m.view())
	}
	return out
}

func (f *fakeCore) Calls() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.calls)
}

func (f *fakeCore) Bootstraps() []Bootstrap {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.bootstraps)
}

func (f *fakeCore) PlantOrphan(name string, owner Owner) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.plant(name, owner)
}

func (f *fakeCore) PlantForeign(name string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.plant(name, Owner{
		ControllerID: "ctl_somebodyelse",
		ProviderID:   "prv_somebodyelse",
		MachineID:    "mach_somebodyelse",
		Fingerprint:  "somebodyelses",
	})
}

func (f *fakeCore) plant(name string, owner Owner) {
	f.nextID++
	f.machines = append(f.machines, &fakeMachine{
		ref:     MachineRef{Zone: f.zone, ID: fmt.Sprintf("%d", 100+f.nextID), Name: name},
		phase:   PhaseRunning,
		owner:   owner,
		planted: true,
	})
}

func (f *fakeCore) Vanish(machineID string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.machines = slices.DeleteFunc(f.machines, func(m *fakeMachine) bool { return m.ref.ID == machineID })
}

func (f *fakeCore) SetFailure(op string, k FailureKind, message string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.failures[op] = &fakeFailure{kind: k, message: message}
}

func (f *fakeCore) SetFailureOnce(op string, k FailureKind, message string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.failures[op] = &fakeFailure{kind: k, message: message, once: true}
}

func (f *fakeCore) ClearFailures() {
	f.mu.Lock()
	defer f.mu.Unlock()
	clear(f.failures)
	clear(f.ambiguous)
}

func (f *fakeCore) SetQuota(max int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.quota = max
}

func (f *fakeCore) SetDelay(op string, d time.Duration) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.delays[op] = d
}

func (f *fakeCore) SetAsync(op string, polls int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.async[op] = polls
}

func (f *fakeCore) SetAmbiguousAfterWork(op string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.ambiguous[op] = ambiguousAfterWork
}

func (f *fakeCore) SetAmbiguousBeforeWork(op string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.ambiguous[op] = ambiguousBeforeWork
}

// ---------------------------------------------------------------------------
// The contract
// ---------------------------------------------------------------------------

func (f *fakeCore) Preflight(ctx context.Context) Report {
	// Preflight reports rather than fails, so an injected failure comes back as
	// a finding: an operator with a wrong credential must still get a page that
	// says which credential and what to change.
	if err := f.begin(ctx, "preflight", ""); err != nil {
		return Report{Findings: []config.Finding{{
			Code:     "provider.preflight_failed",
			Severity: config.SeverityError,
			Setting:  "provider.credential",
			Title:    "The fake provider refused the connection check",
			Detail:   err.Error(),
			Fix:      "This is the in-memory fake; a test asked it to refuse.",
		}}}
	}
	return Report{Reachable: true, Version: "fake-1"}
}

func (f *fakeCore) Allocate(ctx context.Context, spec MachineSpec) (MachineRef, error) {
	if err := f.begin(ctx, "allocate", spec.Name); err != nil {
		return MachineRef{}, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if m := f.findLocked(MachineRef{Name: spec.Name}); m != nil {
		return MachineRef{}, f.fail("allocate", FailureConflict, spec.Name,
			fmt.Sprintf("a machine named %q already exists", spec.Name),
			"Names are unique per provider; the caller has reused one.")
	}
	// Identity is minted and nothing is created: the counter moving is the only
	// trace an allocate leaves, which is what lets the caller persist the
	// identity before anything can exist to be lost.
	f.nextID++
	return MachineRef{Zone: f.zone, ID: fmt.Sprintf("%d", 100+f.nextID), Name: spec.Name}, nil
}

func (f *fakeCore) Create(ctx context.Context, ref MachineRef, spec MachineSpec) (OperationRef, error) {
	if err := f.begin(ctx, "create", ref.Name); err != nil {
		return OperationRef{}, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()

	// One ref is one machine, however many times this is called. It is the
	// rule that makes a re-issued create after an unknown outcome safe.
	if m := f.findLocked(ref); m != nil {
		return f.opLocked(OpCreate, true, "machine already exists", nil), nil
	}
	if f.quota > 0 && f.liveLocked() >= f.quota {
		return OperationRef{}, f.fail("create", FailureQuota, ref.Name,
			fmt.Sprintf("no capacity for another machine: %d of %d in use", f.liveLocked(), f.quota),
			"Free a machine or raise the provider's ceiling.")
	}
	m := &fakeMachine{ref: ref, phase: PhaseCreating, owner: spec.Owner, detail: "creating"}
	f.machines = append(f.machines, m)
	op := f.opLocked(OpCreate, true, "created", func() {
		m.phase = PhaseRunning
		m.address = "198.51.100." + m.ref.ID
		m.detail = "running"
	})
	if f.ambiguous["create"] == ambiguousAfterWork {
		// The machine exists and the caller will never hear so. Anything but an
		// Inspect from here buys a second machine.
		return OperationRef{}, f.fail("create", FailureAmbiguous, ref.Name,
			"the connection dropped after the request went out",
			"Look for the machine by the identity you allocated; do not create another.")
	}
	return op, nil
}

func (f *fakeCore) Inspect(ctx context.Context, ref MachineRef) (Machine, error) {
	if err := f.begin(ctx, "inspect", ref.Name); err != nil {
		return Machine{}, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	m := f.findLocked(ref)
	if m == nil {
		return Machine{}, f.fail("inspect", FailureNotFound, ref.Name, "no such machine", "")
	}
	return m.view(), nil
}

func (f *fakeCore) List(ctx context.Context, owner Owner) ([]Machine, error) {
	if err := f.begin(ctx, "list", ""); err != nil {
		return nil, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []Machine
	for _, m := range f.machines {
		// Ours by mark, or somebody else's wearing our name grammar. The second
		// is the half a sweep cannot do without: it is how a foreign fleet's
		// machine is told from an orphan of our own.
		if m.owner.ControllerID == owner.ControllerID || store.IsMachineName(m.ref.Name) {
			out = append(out, m.view())
		}
	}
	return out, nil
}

func (f *fakeCore) Delete(ctx context.Context, ref MachineRef) (OperationRef, error) {
	if err := f.begin(ctx, "delete", ref.Name); err != nil {
		return OperationRef{}, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	m := f.findLocked(ref)
	if m == nil {
		// The desired end state has been reached, so there is nothing to
		// follow and nothing to report.
		return OperationRef{}, nil
	}
	m.phase = PhaseDeleting
	m.detail = "deleting"
	op := f.opLocked(OpDelete, true, "deleted", func() {
		f.machines = slices.DeleteFunc(f.machines, func(x *fakeMachine) bool { return x == m })
	})
	if f.ambiguous["delete"] == ambiguousAfterWork {
		return OperationRef{}, f.fail("delete", FailureAmbiguous, ref.Name,
			"the connection dropped after the request went out",
			"Inspect the machine: a delete that was issued cannot be un-issued.")
	}
	return op, nil
}

func (f *fakeCore) Operation(ctx context.Context, ref OperationRef) (OperationStatus, error) {
	if err := f.begin(ctx, "operation", ref.Handle); err != nil {
		return OperationStatus{}, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	op, ok := f.ops[ref.Handle]
	if !ok {
		// An unknown handle is not-found rather than a panic: after a restart
		// the controller asks about handles the provider may have forgotten.
		return OperationStatus{}, f.fail("operation", FailureNotFound, ref.Handle,
			"no such operation", "")
	}
	if op.polls > 0 {
		op.polls--
		return OperationStatus{Detail: "in progress"}, nil
	}
	if op.apply != nil {
		op.apply()
		op.apply = nil
	}
	return OperationStatus{Done: true, OK: op.ok, Detail: op.detail}, nil
}

func (p powerOps) Start(ctx context.Context, ref MachineRef) (OperationRef, error) {
	return p.power(ctx, "start", OpStart, ref, PhaseRunning, "started")
}

func (p powerOps) Stop(ctx context.Context, ref MachineRef, graceful bool) (OperationRef, error) {
	detail := "stopped"
	if graceful {
		detail = "shut down"
	}
	return p.power(ctx, "stop", OpStop, ref, PhaseStopped, detail)
}

func (f *fakeCore) power(ctx context.Context, name string, kind OpKind, ref MachineRef, to Phase, detail string) (OperationRef, error) {
	if err := f.begin(ctx, name, ref.Name); err != nil {
		return OperationRef{}, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	m := f.findLocked(ref)
	if m == nil {
		return OperationRef{}, f.fail(name, FailureNotFound, ref.Name, "no such machine", "")
	}
	return f.opLocked(kind, true, detail, func() { m.phase, m.detail = to, detail }), nil
}

func (b bootstrapOps) Bootstrap(ctx context.Context, ref MachineRef, payload Bootstrap) (OperationRef, error) {
	if err := b.begin(ctx, "bootstrap", ref.Name); err != nil {
		return OperationRef{}, err
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	m := b.findLocked(ref)
	if m == nil {
		return OperationRef{}, b.fail("bootstrap", FailureNotFound, ref.Name, "no such machine", "")
	}
	// Kept whole, because what a test wants to ask of a payload is what is NOT
	// in it.
	b.bootstraps = append(b.bootstraps, payload)
	return b.opLocked(OpBootstrap, true, "bootstrapped", nil), nil
}

func (d discoverOps) Discover(ctx context.Context) (Discovery, error) {
	if err := d.begin(ctx, "discover", ""); err != nil {
		return Discovery{}, err
	}
	return Discovery{
		Nodes: []Choice{{Value: d.zone, Label: d.zone, Consequence: "Machines are created here."}},
	}, nil
}

// ---------------------------------------------------------------------------
// Plumbing
// ---------------------------------------------------------------------------

// begin records the call, serves the delay against the caller's deadline, and
// applies whatever failure a test asked for. It is one function so that every
// operation gets the same treatment and a new one cannot quietly skip the
// cancellation check.
func (f *fakeCore) begin(ctx context.Context, op, ref string) error {
	f.mu.Lock()
	call := op
	if ref != "" {
		call = op + " " + ref
	}
	f.calls = append(f.calls, call)
	delay := f.delays[op]
	ambiguity := f.ambiguous[op]
	failure := f.failures[op]
	if failure != nil && failure.once {
		delete(f.failures, op)
	}
	f.mu.Unlock()

	if err := ctx.Err(); err != nil {
		// Returned untouched: a cancellation is the caller's own doing and
		// classifying it as a provider failure would have the reconciler blame
		// the hypervisor for its own shutdown.
		return err
	}
	if delay > 0 {
		t := time.NewTimer(delay)
		defer t.Stop()
		select {
		case <-t.C:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	if failure != nil {
		return f.fail(op, failure.kind, ref, failure.message, "")
	}
	if ambiguity == ambiguousBeforeWork {
		return f.fail(op, FailureAmbiguous, ref,
			"the connection dropped before the request went out",
			"Look before acting: this answer is the same whether or not the work happened.")
	}
	return nil
}

func (f *fakeCore) fail(op string, kind FailureKind, ref, message, remedy string) error {
	return &Error{Kind: kind, Op: op, Ref: ref, Message: message, Remedy: remedy}
}

// opLocked mints an operation handle. polls comes from SetAsync, so an
// operation a test made slow is unfinished for exactly as long as it asked.
func (f *fakeCore) opLocked(kind OpKind, ok bool, detail string, apply func()) OperationRef {
	f.nextOp++
	handle := fmt.Sprintf("op-%s-%d", kind, f.nextOp)
	f.ops[handle] = &fakeOp{kind: kind, polls: f.async[string(kind)], ok: ok, detail: detail, apply: apply}
	return OperationRef{Kind: kind, Handle: handle}
}

// findLocked matches by native identifier, and by name when the caller has
// nothing else -- the shape a provider that only knows its own name after
// creation has to support.
func (f *fakeCore) findLocked(ref MachineRef) *fakeMachine {
	for _, m := range f.machines {
		switch {
		case ref.ID != "" && m.ref.ID == ref.ID:
			return m
		case ref.ID == "" && ref.Name != "" && m.ref.Name == ref.Name:
			return m
		}
	}
	return nil
}

// liveLocked counts the machines this fake made, which is what a quota bounds.
// Planted ones belong to somebody else and are not ours to be charged for.
func (f *fakeCore) liveLocked() int {
	n := 0
	for _, m := range f.machines {
		if !m.planted {
			n++
		}
	}
	return n
}

func (m *fakeMachine) view() Machine {
	return Machine{
		Ref:     m.ref,
		Phase:   m.phase,
		Owner:   m.owner,
		Address: m.address,
		Detail:  m.detail,
	}
}

// ---------------------------------------------------------------------------
// The factory
// ---------------------------------------------------------------------------

// FakeFactory builds fakes, so that a test can exercise the registry, the
// version handshake and the settings schema without a real provider.
type FakeFactory struct {
	opts []FakeOption
	caps Capabilities
}

// NewFakeFactory returns a factory whose fakes carry the capabilities these
// options leave. Its description is taken from a fake it builds, so the factory
// cannot describe one thing and build another -- a test that wants that
// disagreement has to write its own factory, and one does.
func NewFakeFactory(opts ...FakeOption) *FakeFactory {
	return &FakeFactory{opts: opts, caps: NewFake(opts...).Capabilities()}
}

func (f *FakeFactory) Kind() store.ProviderKind { return store.ProviderFake }
func (f *FakeFactory) Describe() Capabilities   { return f.caps }

func (f *FakeFactory) Settings() []SettingSpec {
	return []SettingSpec{{
		Key:         "zone",
		Label:       "Zone",
		Kind:        SettingChoice,
		Required:    true,
		Help:        "Where machines are created.",
		Consequence: "Every machine this provider buys is created here.",
		Discovers:   "nodes",
		Default:     "zone-a",
	}}
}

// Validate is offline, as the contract requires: it reads the answers and
// dials nothing.
func (f *FakeFactory) Validate(settings map[string]string) []config.Finding {
	if settings["zone"] == "" {
		return []config.Finding{{
			Code:     "provider.zone_missing",
			Severity: config.SeverityError,
			Setting:  "provider.settings.zone",
			Title:    "This provider has no zone",
			Detail:   "Machines cannot be created without somewhere to create them.",
			Fix:      "Choose a zone.",
		}}
	}
	return nil
}

func (f *FakeFactory) New(_ context.Context, _ Config) (Provider, error) {
	return NewFake(f.opts...), nil
}

// DiscoverDraft answers as the built fake would, so a test of the wizard's
// draft discovery exercises the same path a real driver takes.
func (f *FakeFactory) DiscoverDraft(ctx context.Context, _ Config) (Discovery, error) {
	d, ok := NewFake(f.opts...).(Discoverer)
	if !ok {
		return Discovery{}, ErrUnsupported
	}
	return d.Discover(ctx)
}
