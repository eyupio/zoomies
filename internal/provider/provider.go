// Package provider defines what Zoomies asks an infrastructure provider to do,
// and what it promises in return.
//
// A provider rents machines. It is asked for one when a pool has queued work
// and no host in the fleet can run it, and asked to destroy one when the fleet
// no longer needs it. That is the whole of its job: a provider performs
// infrastructure operations and decides nothing. How many machines should
// exist, how long each step may take, when to give up and what may be deleted
// all belong to one reconciler in the controller, for every provider, so that a
// second provider is a new package rather than a new branch in the controller.
//
// The package holds the contract and nothing else -- no SQL, no HTTP, no
// controller. It imports internal/store for the domain vocabulary a provider
// has to speak and internal/config for the finding type its preflight reports
// in, and a test holds that import set exactly where it is: a contract that has
// learned a particular hypervisor's words has stopped being one.
//
// NewFake is an in-memory provider that obeys the same rules, and
// RunContractTests is the suite every provider must pass. Both are exported
// from the production package on purpose; see fake.go and conformance.go.
package provider

import (
	"context"
	"crypto/subtle"
	"log/slog"
	"net/http"
	"time"

	"github.com/eyupio/zoomies/internal/config"
	"github.com/eyupio/zoomies/internal/store"
)

// ContractVersion is the provider contract this build speaks. It is bumped when
// a change would make a provider written for the previous version behave
// wrongly rather than merely miss a field: adding a field to MachineSpec, a
// method to an optional capability interface, or a FailureKind never raises it.
//
// The policy, stated once here because it is what a provider author needs:
//
//   - The controller refuses to build a provider whose declared range does not
//     contain ContractVersion, names both numbers and says which side to
//     upgrade. Machines that provider already owns stay visible, drainable and
//     deletable: a version mismatch must never strand a running machine.
//   - An unknown FailureKind reads as FailureInternal, which retries nothing
//     and asks for a person.
//   - An unknown Phase reads as PhaseUnknown, and PhaseUnknown never authorises
//     a delete. What we do not recognise, we leave alone.
//
// One version is supported, because every provider is in-tree. Accepting a
// range is a decision for whenever an out-of-tree provider exists, and
// docs/providers.md records that it was considered and deferred.
const ContractVersion = 1

// Provider is the infrastructure surface Zoomies uses. It is an interface so
// that tests can run the whole reconciler against a fake with no network, and
// so that a second provider is a new package rather than a new branch in the
// controller.
//
// Every method takes ctx first and must honour its deadline. The reconciler
// sets one on every call from Capabilities().Deadlines, floored by the
// operator's configuration; a provider that blocks past it can wedge the fleet.
//
// Nothing here reads a clock for its own decisions, opens a database, or
// retries. Backoff is the caller's, as it is in internal/github.
type Provider interface {
	Kind() store.ProviderKind
	Capabilities() Capabilities

	// Preflight checks configuration, credentials and prerequisites without
	// changing anything, and never returns an error: it reports what an
	// operator must fix. backend.Probe is the model -- "an agent on a host with
	// no Docker must still start and say so".
	Preflight(ctx context.Context) Report

	// Allocate picks the identity a Create will use and creates nothing. The
	// controller persists what this returns BEFORE calling Create, so a create
	// whose outcome is unknown is reconciled by identity rather than retried.
	// A provider whose identity is only knowable after creation returns a ref
	// carrying Name alone and must make that name findable through List.
	Allocate(ctx context.Context, spec MachineSpec) (MachineRef, error)

	// Create starts building the machine at ref. It returns as soon as the work
	// is under way; the OperationRef is how it is followed. Calling it twice for
	// one ref must not build two machines.
	Create(ctx context.Context, ref MachineRef, spec MachineSpec) (OperationRef, error)

	// Inspect reports one machine. It returns ErrNotFound when the resource is
	// gone, which callers read as "already deleted", not as a failure. The
	// Machine it returns carries the ownership marks as the provider read them
	// back, never as the caller supplied them.
	Inspect(ctx context.Context, ref MachineRef) (Machine, error)

	// List returns every machine the provider can see that carries this owner's
	// controller mark, plus any carrying our name grammar and somebody else's
	// mark -- the caller needs both to tell an orphan from a foreign resource.
	// It is the ownership sweep behind the orphan review page, so it must be
	// cheap: one call where the API allows one.
	List(ctx context.Context, owner Owner) ([]Machine, error)

	// Delete removes a machine. Deleting one that is already gone returns a zero
	// OperationRef and no error, because the desired end state has been reached.
	// It never verifies ownership itself -- the controller does that, against
	// the store and a fresh Inspect, before it calls this.
	Delete(ctx context.Context, ref MachineRef) (OperationRef, error)

	// Operation reports an asynchronous operation's progress. It is the whole of
	// restart recovery: a controller that comes back holding only a stored
	// OperationRef asks this rather than acting again. An unknown handle is
	// ErrNotFound, never a panic.
	Operation(ctx context.Context, op OperationRef) (OperationStatus, error)
}

// PowerController is implemented by providers that can stop a machine without
// destroying it. Optional, because a provider that can only create and delete is
// still useful and pretending otherwise would have the reconciler issue stops
// that silently do nothing.
//
// It is a separate interface found by type assertion rather than a method on
// Provider that answers "unsupported", for the reason backend.TimedCreator is:
// a method that is always going to refuse is a method the reconciler will keep
// calling.
type PowerController interface {
	Start(ctx context.Context, ref MachineRef) (OperationRef, error)
	Stop(ctx context.Context, ref MachineRef, graceful bool) (OperationRef, error)
}

// Bootstrapper is implemented by providers that can push the enrolment payload
// into the guest themselves. A provider that cannot declares
// Capabilities().Bootstrap = BootstrapMetadata, and the payload travels in the
// machine's creation metadata instead.
type Bootstrapper interface {
	Bootstrap(ctx context.Context, ref MachineRef, p Bootstrap) (OperationRef, error)
}

// Discoverer fills the guided form: the nodes, storages, networks and templates
// this credential can actually see. Optional because a provider with no such
// concepts should not have to fake one.
type Discoverer interface {
	Discover(ctx context.Context) (Discovery, error)
}

// Factory builds a Provider from one stored configuration row. The controller
// holds one Registry of factories and builds a provider per row, cached until
// the row's updated_at moves, so a credential change takes effect without a
// restart.
type Factory interface {
	Kind() store.ProviderKind
	// Describe is the capability set without an instance, for the page that
	// lists the kinds a build supports. It must agree with what New builds:
	// Registry.New refuses a factory whose description and provider disagree.
	Describe() Capabilities
	// Settings is the schema the configuration form, the validator and the
	// documentation share, so that a new setting cannot appear in one of the
	// three and be missing from the other two.
	Settings() []SettingSpec
	// Validate checks the non-secret answers offline: no network, no credential.
	// It is what the form runs as somebody types, which is why it may not dial
	// anything -- Preflight is the call that does.
	Validate(settings map[string]string) []config.Finding
	New(ctx context.Context, cfg Config) (Provider, error)
}

// Config carries what a factory needs and nothing more. The credential arrives
// as plaintext, unsealed by the controller for the life of this call: the store
// keeps it sealed and unsealing is the caller's job, exactly as
// github.AppFactory takes an already-decrypted private key.
type Config struct {
	ProviderID string
	Owner      Owner
	Endpoint   string
	// Settings holds the non-secret answers, keyed by SettingSpec.Key.
	Settings map[string]string
	// Credential is PLAINTEXT. A provider keeps it for the life of the client
	// it builds, never writes it anywhere, and never logs it.
	Credential string
	CAPEM      string
	// Insecure records that certificate verification was turned off. It is
	// carried rather than refused because a homelab hypervisor's certificate is
	// usually its own, and the validator warns about it instead.
	Insecure   bool
	Deadlines  Deadlines
	HTTPClient *http.Client
	Logger     *slog.Logger
}

// BootstrapMode says how the enrolment payload reaches the guest.
type BootstrapMode string

const (
	// BootstrapGuestAgent means the provider writes the payload into a running
	// guest itself, and implements Bootstrapper.
	BootstrapGuestAgent BootstrapMode = "guest_agent"
	// BootstrapMetadata means the payload travels in the machine's creation
	// metadata, so it is readable by anyone who can read that metadata.
	BootstrapMetadata BootstrapMode = "metadata"
	// BootstrapNone means the provider cannot carry a payload at all; the image
	// has to arrive already able to enrol.
	BootstrapNone BootstrapMode = "none"
)

// Capabilities is what a provider can do, declared rather than discovered.
//
// The three Can* flags describe the optional interfaces above, and a provider
// whose flag and type disagree is refused when it is built: a flag that says
// yes where the type says no would have the reconciler call a method that is
// not there, and one that says no where the type says yes silently loses a
// capability an operator paid for.
type Capabilities struct {
	MinContract, MaxContract int
	Kind                     store.ProviderKind
	Label                    string
	Bootstrap                BootstrapMode
	CanStartStop             bool // true iff this Provider also implements PowerController
	CanDiscover              bool // ... Discoverer
	// CanMarkOwnership reports whether the provider can write our marks onto
	// the resource itself. A provider that cannot leaves Machine.Owner zero,
	// and the store stays the only record of who made it.
	CanMarkOwnership bool
	// AsyncOperations reports whether Create and Delete return an OperationRef
	// worth polling. A synchronous provider returns a zero one and sets this
	// false, so the reconciler does not poll a handle that will never move.
	AsyncOperations bool
	// CostUnit is what Machine.Cost is measured in, e.g. "machine-hour". Empty
	// when the provider has no opinion, which is not the same as free.
	CostUnit  string
	Deadlines Deadlines
}

// Deadlines are the provider's own honest budgets. The controller applies the
// larger of these and the operator's configuration, so a provider may ask for
// longer but never for less supervision than the operator asked for.
type Deadlines struct {
	Call      time.Duration // one API request
	Allocate  time.Duration
	Create    time.Duration // the whole asynchronous create, not the request that starts it
	Bootstrap time.Duration
	Delete    time.Duration
}

// Owner is what a create stamps on the resource and what a delete verifies
// against the row. None of it is authenticated: anybody with provider access can
// forge it. That is why the store is the authority and this is tamper-evidence
// -- it catches a recycled identifier, another controller's resource, and one
// somebody made by hand, which are the three cases that actually happen.
type Owner struct {
	ControllerID string    // ctl_... -- the lease holder that created it
	ProviderID   string    // prv_...
	MachineID    string    // mach_...
	Fingerprint  string    // minted with the row, never reused
	CreatedAt    time.Time // the controller's clock, never the provider's
}

// Zero reports whether a resource carried no marks at all, which is a different
// thing from carrying somebody else's: one is an unmarked resource, the other is
// a resource with an owner, and only the second is evidence of a second fleet.
func (o Owner) Zero() bool {
	return o.ControllerID == "" && o.ProviderID == "" && o.MachineID == "" && o.Fingerprint == ""
}

// Matches reports whether marks read back from a resource are the ones we wrote.
//
// It compares the controller, the machine and the fingerprint, and the
// fingerprint in constant time: the mark is readable by anybody with audit
// rights on the infrastructure, so nothing here is a secret, but a comparison
// that leaks its answer through timing is a habit worth not having in the one
// function that stands between a sweep and a delete.
func (o Owner) Matches(want Owner) bool {
	if o.ControllerID != want.ControllerID || o.MachineID != want.MachineID {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(o.Fingerprint), []byte(want.Fingerprint)) == 1
}

// MachineRef is the durable identity of one machine. Everything in it is minted
// by Allocate and written to the row before any call that could create anything.
type MachineRef struct {
	// Zone is the node or region the machine lives in. Empty for a provider
	// with no such concept.
	Zone string
	// ID is the provider's native identity. Empty when only Name is knowable
	// before the resource exists, in which case List must find it by Name.
	ID string
	// Name is ours, minted by store.NewMachineName. It is unique per provider
	// and is the primary ownership mark, because it is the one thing a
	// cluster-wide sweep can filter on without reading every resource's config.
	Name string
}

// Zero reports whether a ref names nothing at all. A ref with only a Name is
// not zero: that is the shape a provider returns when identity is only knowable
// after creation.
func (r MachineRef) Zero() bool { return r.Zone == "" && r.ID == "" && r.Name == "" }

// OpKind says which operation a handle belongs to, so a controller that comes
// back holding a handle knows what finishing it means.
type OpKind string

const (
	OpCreate    OpKind = "create"
	OpStart     OpKind = "start"
	OpStop      OpKind = "stop"
	OpBootstrap OpKind = "bootstrap"
	OpDelete    OpKind = "delete"
)

// OperationRef is a provider-side handle for asynchronous work, durable across a
// controller restart. That durability is the whole point: the reconciler stores
// the handle on the machine row before it considers the call done, so a
// controller that comes back holding nothing but the handle asks what happened
// rather than doing the work again. A good handle encodes everything needed to
// find the task -- the zone included -- because nothing else about it is stored.
type OperationRef struct {
	Kind   OpKind
	Handle string // opaque to everything but the provider that issued it
}

// Zero reports whether there is nothing to follow: a synchronous provider's
// answer, and a delete of a resource that was already gone.
func (o OperationRef) Zero() bool { return o.Handle == "" }

// OperationStatus is one asynchronous operation's progress. Done and OK are
// separate because "finished" and "worked" are different questions, and a
// caller that conflated them would treat a failed create as a machine.
type OperationStatus struct {
	Done bool
	OK   bool
	// Detail is the provider's own completion text, shown verbatim to an
	// operator: it is usually the only sentence that says which storage was
	// full or which privilege was missing.
	Detail string
}

// Phase is the provider's view of one machine. It is coarse on purpose: the
// reconciler wants to know whether the resource exists and whether it is
// running, and every finer distinction a particular provider offers is detail
// for an operator rather than an input to a decision.
type Phase string

const (
	// PhaseUnknown is a machine we could not classify. It never authorises a
	// delete and never reads as gone, because "we could not tell" and "it is
	// not there" lead to opposite decisions.
	PhaseUnknown  Phase = "unknown"
	PhaseCreating Phase = "creating"
	PhaseStopped  Phase = "stopped"
	PhaseRunning  Phase = "running"
	PhaseDeleting Phase = "deleting"
	// PhaseGone is the provider saying the resource does not exist. It is the
	// only phase that lets a delete be recorded as complete.
	PhaseGone Phase = "gone"
)

// ParsePhase reads a phase a provider reported, answering PhaseUnknown for
// anything this build does not recognise -- a phase from a newer contract
// included. The alternative, carrying the unrecognised string through, would
// let a comparison somewhere downstream decide something about it.
func ParsePhase(s string) Phase {
	switch Phase(s) {
	case PhaseCreating, PhaseStopped, PhaseRunning, PhaseDeleting, PhaseGone:
		return Phase(s)
	}
	return PhaseUnknown
}

// Known reports whether this phase is one the contract defines.
func (p Phase) Known() bool { return ParsePhase(string(p)) == p && p != PhaseUnknown }

// Gone reports whether the provider is sure the resource no longer exists. Only
// PhaseGone is: an unknown phase is a machine we failed to read, and reading it
// as gone would let a delete be recorded complete on a machine still running.
func (p Phase) Gone() bool { return p == PhaseGone }

// AuthorisesDelete reports whether a delete may be issued against a machine in
// this phase. An unknown phase does not, which is the forward-compatibility rule
// that costs a stuck machine and saves somebody else's.
func (p Phase) AuthorisesDelete() bool {
	switch p {
	case PhaseCreating, PhaseStopped, PhaseRunning, PhaseDeleting:
		return true
	}
	return false
}

// Machine is one machine as the provider sees it now. It carries no timestamp
// the provider invented: a provider's clock is never authoritative, for the
// reason the controller already discards an agent's own sample time. The
// controller stamps observed_at itself.
type Machine struct {
	Ref   MachineRef
	Phase Phase
	// Owner is the marks as read back from the resource, zero when it carries
	// none. It is never the marks the caller supplied, because a value echoed
	// back proves nothing about what is written on the machine.
	Owner Owner
	// Locked is non-empty when another operation holds the resource: a reason
	// to wait, not a failure, and not a reason to create a second machine.
	Locked string
	// Address is where the guest will reach the controller from, when known.
	Address string
	// Detail is one operator-facing sentence.
	Detail string
	// Cost is per Capabilities().CostUnit, zero when the provider has no
	// opinion.
	Cost float64
}

// MachineSpec is what to build. It carries NO provider credential and has no
// field one could be put in: the guest gets a single-use join token and nothing
// else, and the type is the enforcement.
type MachineSpec struct {
	// Name is the machine's name as Zoomies will know it, minted by
	// store.NewMachineName before anything is created.
	Name  string
	Owner Owner
	Shape Shape
	// Bootstrap is rendered by the controller: files and commands only.
	Bootstrap Bootstrap
}

// Shape is the one machine shape a provider offers, as the operator configured
// it. Placement carries the provider's own answers -- which node, which storage,
// which template -- keyed by SettingSpec.Key, so that a provider with concepts
// this package has never heard of needs no field here and no migration.
type Shape struct {
	CPUs      float64
	MemoryMB  int64
	DiskMB    int64
	Platform  store.Platform
	Placement map[string]string
}

// Bootstrap is what goes inside the guest: files, and the commands that install
// them. Nothing here is a provider credential.
//
// Commands are argv arrays rather than shell lines, and deliberately so: a
// payload that is never pasted into a shell has no quoting to get wrong and no
// injection class to police.
type Bootstrap struct {
	Files    []File
	Commands [][]string
}

// File is one file to write into the guest. Mode is carried because some
// transports have no way to set one, and the enrolment file must not be
// world-readable -- a provider that cannot set a mode writes the file and then
// runs a command that fixes it.
type File struct {
	Path    string
	Mode    uint32
	Content []byte
}

// SettingKind is how one setting is asked for, so the form, the CLI and the
// documentation render it the same way.
type SettingKind string

const (
	SettingText   SettingKind = "text"
	SettingNumber SettingKind = "number"
	SettingBool   SettingKind = "bool"
	SettingChoice SettingKind = "choice"
	SettingList   SettingKind = "list"
	// SettingSecret is never echoed back to a client and never written to an
	// audit row.
	SettingSecret SettingKind = "secret"
)

// SettingSpec is one answer a provider needs, described once so that the form,
// the validator and the documentation cannot drift apart.
type SettingSpec struct {
	Key      string
	Label    string
	Kind     SettingKind
	Required bool
	// Advanced puts the setting behind a disclosure, because a form that asks
	// eleven questions to do the common thing gets abandoned.
	Advanced bool
	// Help is one operator-facing sentence.
	Help string
	// Consequence says what choosing it means, in the voice the pool form
	// already uses for a choice that costs something.
	Consequence string
	// Discovers names the Discovery list that fills the choices, e.g. "nodes",
	// so a provider that implements Discoverer offers a menu rather than asking
	// somebody to type an identifier they have to go and look up.
	Discovers string
	Default   string
}

// Report is a preflight result. Its findings are config.Finding so the problems
// drawer, the startup print and the setup wizard render them with no
// translation, and so that "what is wrong with this provider" reads the same as
// "what is wrong with this configuration".
type Report struct {
	// Reachable is whether we got an answer at all. It is separate from the
	// findings because a provider that cannot be reached has one problem, not
	// eleven, and listing the eleven checks we could not run would bury it.
	Reachable bool
	// Version is the provider's own version string, kept for the qualification
	// record: "it worked" is worth little without "against what".
	Version  string
	Findings []config.Finding
}

// OK reports whether preflight found nothing that stops this provider being
// used. Warnings do not: they weaken the posture and say so, exactly as they do
// at startup.
func (r Report) OK() bool {
	if !r.Reachable {
		return false
	}
	for _, f := range r.Findings {
		if f.Severity == config.SeverityError {
			return false
		}
	}
	return true
}

// Discovery is what a credential can actually see, for the guided form. The
// lists are named for the concepts the first providers have; a provider with no
// such concept leaves them empty rather than inventing one.
type Discovery struct {
	Nodes     []Choice
	Storages  []Choice
	Bridges   []Choice
	Templates []Choice
}

// Choice is one option in a guided form. Consequence is what picking it means,
// because a list of identifiers with no consequences is a list somebody guesses
// at.
type Choice struct{ Value, Label, Consequence string }
