package provider

import (
	"context"
	"errors"
	"go/ast"
	"go/build"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/eyupio/zoomies/internal/config"
	"github.com/eyupio/zoomies/internal/store"
)

// The contract is only a contract for as long as it is one package's worth of
// vocabulary. Once it imports the controller, the scheduler or a provider of
// its own, "write a provider" stops meaning "write a leaf package" and a second
// provider becomes a change to the middle of the system.
func TestTheProviderContractImportsNothingBeyondItsDomainTypes(t *testing.T) {
	pkg, err := build.ImportDir(".", 0)
	if err != nil {
		t.Fatalf("reading this package: %v", err)
	}
	want := map[string]bool{
		"github.com/eyupio/zoomies/internal/store":  true,
		"github.com/eyupio/zoomies/internal/config": true,
	}
	got := map[string]bool{}
	for _, path := range pkg.Imports {
		if isStdlib(path) {
			continue
		}
		got[path] = true
	}
	for path := range got {
		if !want[path] {
			t.Errorf("internal/provider imports %s; the contract may only speak the domain types", path)
		}
	}
	for path := range want {
		if !got[path] {
			t.Errorf("internal/provider no longer imports %s; if that is deliberate, this list is what has to change", path)
		}
	}
}

func isStdlib(path string) bool {
	first, _, _ := strings.Cut(path, "/")
	return !strings.Contains(first, ".")
}

// A contract that has learned one hypervisor's words has stopped being one: the
// second provider would then be written against the first provider's vocabulary
// rather than against the contract, which is exactly the shape this package
// exists to avoid.
func TestTheContractNamesNothingProxmoxSpecific(t *testing.T) {
	// Comments count, not only code. A type named after one provider is caught
	// by review; a comment explaining the contract in one provider's terms is
	// what actually teaches the next author the wrong vocabulary.
	banned := regexp.MustCompile(`(?i)proxmox|\bpve\b|vmid|upid|qemu`)

	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("reading this package: %v", err)
	}
	files := 0
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		files++
		fset := token.NewFileSet()
		file, err := parser.ParseFile(fset, name, nil, parser.ParseComments|parser.SkipObjectResolution)
		if err != nil {
			t.Fatalf("parsing %s: %v", name, err)
		}
		report := func(pos token.Pos, what, text string) {
			if m := banned.FindString(text); m != "" {
				t.Errorf("%s: %s names %q, which belongs to one provider and not to the contract",
					fset.Position(pos), what, m)
			}
		}
		ast.Inspect(file, func(n ast.Node) bool {
			switch x := n.(type) {
			case *ast.Ident:
				report(x.Pos(), "identifier "+x.Name, x.Name)
			case *ast.BasicLit:
				if x.Kind == token.STRING {
					report(x.Pos(), "string literal", x.Value)
				}
			}
			return true
		})
		for _, group := range file.Comments {
			for _, c := range group.List {
				report(c.Pos(), "comment", c.Text)
			}
		}
	}
	if files == 0 {
		t.Fatal("no source files were scanned at all; the walk is wrong, not the contract")
	}
}

// Each optional capability is its own interface, found by type assertion. A
// method on Provider that a provider without the capability had to implement
// would be a method that always refuses -- and a method that always refuses is
// one the reconciler keeps calling.
func TestEveryOptionalCapabilityIsItsOwnInterface(t *testing.T) {
	required := reflect.TypeOf((*Provider)(nil)).Elem()
	for _, name := range []string{"Start", "Stop", "Bootstrap", "Discover"} {
		if _, ok := required.MethodByName(name); ok {
			t.Errorf("Provider has %s; an optional capability on the required interface has to be answered by every provider", name)
		}
	}
	for _, tc := range []struct {
		iface reflect.Type
		want  []string
	}{
		{reflect.TypeOf((*PowerController)(nil)).Elem(), []string{"Start", "Stop"}},
		{reflect.TypeOf((*Bootstrapper)(nil)).Elem(), []string{"Bootstrap"}},
		{reflect.TypeOf((*Discoverer)(nil)).Elem(), []string{"Discover"}},
	} {
		if got := tc.iface.NumMethod(); got != len(tc.want) {
			t.Errorf("%s has %d methods, want %d", tc.iface, got, len(tc.want))
		}
		for _, name := range tc.want {
			if _, ok := tc.iface.MethodByName(name); !ok {
				t.Errorf("%s has no %s", tc.iface, name)
			}
		}
	}
}

// The marks are tamper-evidence, and what they have to catch is a recycled
// identifier: the same machine ID at the same provider, made by somebody else
// after ours was destroyed. The fingerprint is the half that settles that.
func TestOwnerMatchesOnlyWhenEveryMarkAgrees(t *testing.T) {
	ours := Owner{ControllerID: "ctl_1", ProviderID: "prv_1", MachineID: "mach_1", Fingerprint: "abcdef"}
	for _, tc := range []struct {
		name string
		read Owner
		want bool
	}{
		{"the marks we wrote", ours, true},
		{"another controller's machine", Owner{ControllerID: "ctl_2", MachineID: "mach_1", Fingerprint: "abcdef"}, false},
		{"a recycled identifier with a fresh fingerprint", Owner{ControllerID: "ctl_1", MachineID: "mach_1", Fingerprint: "fedcba"}, false},
		{"another machine of ours", Owner{ControllerID: "ctl_1", MachineID: "mach_2", Fingerprint: "abcdef"}, false},
		{"a resource wearing no marks at all", Owner{}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.read.Matches(ours); got != tc.want {
				t.Errorf("Matches = %t, want %t", got, tc.want)
			}
		})
	}
	if !(Owner{}).Zero() {
		t.Error("an owner with no marks is not zero; an unmarked resource and a foreign one would be treated alike")
	}
	if ours.Zero() {
		t.Error("marks we wrote read as no marks at all")
	}
}

// A ref carrying only a name is the shape a provider returns when its native
// identity is only knowable after creation. Reading it as "no identity" would
// throw away the one thing that makes such a provider recoverable.
func TestARefCarryingOnlyANameIsStillAnIdentity(t *testing.T) {
	if (MachineRef{Name: "zoomies-mach-abc"}).Zero() {
		t.Error("a ref with a name reads as zero")
	}
	if !(MachineRef{}).Zero() {
		t.Error("a ref naming nothing does not read as zero")
	}
	if !(OperationRef{Kind: OpCreate}).Zero() {
		t.Error("an operation with no handle does not read as zero; a synchronous provider's answer would be polled forever")
	}
}

// The forward-compatibility rule, in the direction that costs money if it is
// wrong: what we cannot read, we leave alone.
func TestAPhaseThisBuildDoesNotKnowIsNeitherGoneNorDeletable(t *testing.T) {
	for _, tc := range []struct {
		in                     string
		want                   Phase
		gone, authorisesDelete bool
	}{
		{"running", PhaseRunning, false, true},
		{"stopped", PhaseStopped, false, true},
		{"creating", PhaseCreating, false, true},
		{"deleting", PhaseDeleting, false, true},
		{"gone", PhaseGone, true, false},
		{"unknown", PhaseUnknown, false, false},
		{"hibernating-from-a-later-contract", PhaseUnknown, false, false},
		{"", PhaseUnknown, false, false},
	} {
		t.Run(tc.in, func(t *testing.T) {
			got := ParsePhase(tc.in)
			if got != tc.want {
				t.Fatalf("ParsePhase(%q) = %q, want %q", tc.in, got, tc.want)
			}
			if got.Gone() != tc.gone {
				t.Errorf("%q.Gone() = %t, want %t", got, got.Gone(), tc.gone)
			}
			if got.AuthorisesDelete() != tc.authorisesDelete {
				t.Errorf("%q.AuthorisesDelete() = %t, want %t", got, got.AuthorisesDelete(), tc.authorisesDelete)
			}
		})
	}
}

// A preflight that could not reach the provider has one problem, not none: a
// report with no findings and no answer must not read as "everything is fine".
func TestAPreflightThatReachedNothingIsNotOK(t *testing.T) {
	for _, tc := range []struct {
		name string
		r    Report
		want bool
	}{
		{"reached, nothing to fix", Report{Reachable: true}, true},
		{"reached, a warning", Report{Reachable: true, Findings: []config.Finding{{Severity: config.SeverityWarning}}}, true},
		{"reached, something to fix", Report{Reachable: true, Findings: []config.Finding{{Severity: config.SeverityError}}}, false},
		{"never reached", Report{}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.r.OK(); got != tc.want {
				t.Errorf("OK() = %t, want %t", got, tc.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// The registry
// ---------------------------------------------------------------------------

// lyingFactory describes one thing and builds another. Every field is what a
// real factory would get right, which is the point: the disagreement is the
// only thing under test.
type lyingFactory struct {
	caps  Capabilities
	build func() Provider
}

func (f lyingFactory) Kind() store.ProviderKind { return store.ProviderFake }
func (f lyingFactory) Describe() Capabilities   { return f.caps }
func (f lyingFactory) Settings() []SettingSpec  { return nil }
func (f lyingFactory) Validate(map[string]string) []config.Finding {
	return nil
}
func (f lyingFactory) New(context.Context, Config) (Provider, error) { return f.build(), nil }

func fakeCaps(opts ...FakeOption) Capabilities { return NewFake(opts...).Capabilities() }

// A version mismatch is an operator's problem to solve, so the refusal has to
// name both numbers and say which half to upgrade. "Unsupported" alone sends
// somebody to the release notes of the wrong side.
func TestARegistryRefusesAFactoryFromAnotherContract(t *testing.T) {
	for _, tc := range []struct {
		name     string
		min, max int
		want     string
	}{
		{"a provider written for a later contract", ContractVersion + 1, ContractVersion + 2, "upgrade Zoomies"},
		{"a provider written for an earlier one", ContractVersion - 2, ContractVersion - 1, "upgrade the provider"},
		{"a range containing nothing", 2, 1, "contains nothing"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			caps := fakeCaps()
			caps.MinContract, caps.MaxContract = tc.min, tc.max
			_, err := NewRegistry(lyingFactory{caps: caps, build: func() Provider { return NewFake() }})
			if !errors.Is(err, ErrUnsupported) {
				t.Fatalf("NewRegistry = %v, want an unsupported-contract refusal", err)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("the refusal is %q; it does not say %q, so it does not say what to do", err, tc.want)
			}
		})
	}
}

// A provider that claims a capability its type does not have would have the
// reconciler call a method that is not there; one that hides a capability it
// does have gets a machine deleted and bought again where a stop would have
// done. Both are caught where the provider is built, not mid-create.
func TestARegistryRefusesAProviderWhoseCapabilitiesDisagreeWithItsType(t *testing.T) {
	for _, tc := range []struct {
		name  string
		caps  Capabilities
		build func() Provider
		want  string
	}{
		{
			name:  "says it can stop machines and cannot",
			caps:  fakeCaps(),
			build: func() Provider { return NewFake(FakeWithoutPower()) },
			want:  "CanStartStop",
		},
		{
			name:  "can stop machines and says it cannot",
			caps:  fakeCaps(FakeWithoutPower()),
			build: func() Provider { return NewFake() },
			want:  "CanStartStop",
		},
		{
			name:  "says it can fill the form and cannot",
			caps:  fakeCaps(),
			build: func() Provider { return NewFake(FakeWithoutDiscovery()) },
			want:  "CanDiscover",
		},
		{
			name:  "claims to push the payload into the guest and cannot",
			caps:  fakeCaps(),
			build: func() Provider { return NewFake(FakeWithoutBootstrap()) },
			want:  "bootstrap mode",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r, err := NewRegistry(lyingFactory{caps: tc.caps, build: tc.build})
			if err != nil {
				t.Fatalf("NewRegistry: %v", err)
			}
			_, err = r.New(context.Background(), store.ProviderFake, Config{})
			if err == nil {
				t.Fatal("the registry built a provider whose capabilities do not match its type")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("the refusal is %q; it does not name %q", err, tc.want)
			}
		})
	}
}

// The factory's description is what the "what can this build do" page renders
// without building anything, so a description that disagrees with the provider
// is a promise made to an operator that the fleet then breaks.
func TestARegistryRefusesAFactoryThatDescribesOneProviderAndBuildsAnother(t *testing.T) {
	// Both halves are internally consistent; only the two sides disagree.
	r, err := NewRegistry(lyingFactory{
		caps:  fakeCaps(FakeWithoutPower()),
		build: func() Provider { return NewFake(FakeWithoutPower()) },
	})
	if err != nil {
		t.Fatalf("NewRegistry: %v", err)
	}
	if _, err := r.New(context.Background(), store.ProviderFake, Config{}); err != nil {
		t.Fatalf("a factory that agrees with itself was refused: %v", err)
	}

	r, err = NewRegistry(lyingFactory{
		caps:  fakeCaps(FakeWithoutDiscovery()),
		build: func() Provider { return NewFake() },
	})
	if err != nil {
		t.Fatalf("NewRegistry: %v", err)
	}
	// The instance is self-consistent, so only the comparison against the
	// description can catch this one.
	if _, err := r.New(context.Background(), store.ProviderFake, Config{}); err == nil {
		t.Fatal("a factory described one provider and built another, and the registry accepted it")
	}
}

func TestARegistryHasNoFactoryForAKindThisBuildCannotSpeak(t *testing.T) {
	r, err := NewRegistry(NewFakeFactory())
	if err != nil {
		t.Fatalf("NewRegistry: %v", err)
	}
	if _, err := r.Get(store.ProviderProxmox); !errors.Is(err, ErrUnsupported) {
		t.Errorf("Get for an unregistered kind = %v, want unsupported", err)
	}
	if _, err := r.New(context.Background(), store.ProviderProxmox, Config{}); !errors.Is(err, ErrUnsupported) {
		t.Errorf("New for an unregistered kind = %v, want unsupported", err)
	}
	if got := r.Kinds(); len(got) != 1 || got[0] != store.ProviderFake {
		t.Errorf("Kinds() = %v, want just the fake", got)
	}
	if _, err := r.Get(store.ProviderFake); err != nil {
		t.Errorf("Get for the registered kind: %v", err)
	}
}

// Two factories for one kind means one of them is never reached, and which one
// depends on argument order -- a bug that only shows up as "the provider did
// not behave like the code I was reading".
func TestARegistryRefusesTwoFactoriesForOneKind(t *testing.T) {
	if _, err := NewRegistry(NewFakeFactory(), NewFakeFactory()); err == nil {
		t.Fatal("two factories for one kind were accepted")
	}
	if _, err := NewRegistry(nil, NewFakeFactory()); err != nil {
		t.Fatalf("a nil factory should be skipped, not refused: %v", err)
	}
}

// The registry is what the controller holds, so its answers have to be stable:
// a list that reshuffles between requests makes a page that flickers.
func TestARegistryListsItsKindsInAStableOrder(t *testing.T) {
	r, err := NewRegistry(NewFakeFactory())
	if err != nil {
		t.Fatalf("NewRegistry: %v", err)
	}
	first := r.Kinds()
	for range 5 {
		if got := r.Kinds(); !reflect.DeepEqual(got, first) {
			t.Fatalf("Kinds() = %v then %v", first, got)
		}
	}
}

// The fake's own factory is the one every other package's tests will build a
// registry from, so it has to be a well-formed factory in its own right.
func TestTheFakeFactoryBuildsWhatItDescribes(t *testing.T) {
	for _, opts := range [][]FakeOption{
		nil,
		{FakeWithoutPower()},
		{FakeWithoutBootstrap()},
		{FakeWithoutDiscovery()},
		{FakeWithoutPower(), FakeWithoutBootstrap(), FakeWithoutDiscovery()},
	} {
		r, err := NewRegistry(NewFakeFactory(opts...))
		if err != nil {
			t.Fatalf("NewRegistry: %v", err)
		}
		p, err := r.New(context.Background(), store.ProviderFake, Config{Deadlines: Deadlines{Call: time.Second}})
		if err != nil {
			t.Fatalf("New: %v", err)
		}
		if err := CheckCapabilities(p); err != nil {
			t.Errorf("%v", err)
		}
	}
	if got := NewFakeFactory().Validate(map[string]string{}); len(got) != 1 {
		t.Errorf("Validate of an empty settings map returned %d findings, want the missing zone", len(got))
	}
	if got := NewFakeFactory().Validate(map[string]string{"zone": "zone-a"}); len(got) != 0 {
		t.Errorf("Validate of a complete settings map returned %v", got)
	}
	if len(NewFakeFactory().Settings()) == 0 {
		t.Error("the factory offers no settings at all; the form would have nothing to render")
	}
}

// Belt and braces on the layout: the suite lives in the production package so
// that a provider's own test is one line, and a file that moved would only be
// noticed when somebody tried to write the second provider.
func TestTheConformanceSuiteShipsWithThePackage(t *testing.T) {
	if _, err := os.Stat(filepath.Join(".", "conformance.go")); err != nil {
		t.Fatalf("conformance.go is not in the production package: %v", err)
	}
}
