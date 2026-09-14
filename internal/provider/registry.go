package provider

import (
	"context"
	"fmt"
	"slices"

	"github.com/eyupio/zoomies/internal/store"
)

// Registry holds the provider factories a build supports, keyed by kind, the
// way backend.Registry holds the backends an agent has. One registry is built
// at startup and a provider is built from it per configuration row.
type Registry struct {
	factories map[store.ProviderKind]Factory
}

// NewRegistry builds a registry, refusing a factory this build cannot work
// with rather than discovering it during the first create.
//
// It refuses two things. A kind that is not one the store knows, because a row
// with that kind could never be written and a factory for it is dead code with
// a typo in it. And a contract range that does not contain ContractVersion: the
// message names both numbers and says which side to upgrade, because "version
// mismatch" without the numbers sends an operator to the release notes of the
// wrong half.
//
// It cannot check here that a factory's declared capabilities match the
// provider it builds -- that needs an instance, and building one needs a
// credential and a context. Registry.New makes that check at the moment the
// instance exists, which is the earliest it can be made and still before the
// reconciler has called anything.
func NewRegistry(fs ...Factory) (*Registry, error) {
	r := &Registry{factories: make(map[store.ProviderKind]Factory, len(fs))}
	for _, f := range fs {
		if f == nil {
			continue
		}
		kind := f.Kind()
		if !kind.Valid() {
			return nil, fmt.Errorf("provider: %q is not a provider kind this build knows", kind)
		}
		if err := checkContract(kind, f.Describe()); err != nil {
			return nil, err
		}
		if _, dup := r.factories[kind]; dup {
			return nil, fmt.Errorf("provider: two factories are registered for %q; one of them is the wrong kind", kind)
		}
		r.factories[kind] = f
	}
	return r, nil
}

// Get returns the factory for a kind, or ErrUnsupported when this build has
// none. The message names the kind, because the row that asked for it is the
// thing an operator has to go and change.
func (r *Registry) Get(k store.ProviderKind) (Factory, error) {
	f, ok := r.factories[k]
	if !ok {
		return nil, fmt.Errorf("%w: this build has no %q provider", ErrUnsupported, k)
	}
	return f, nil
}

// Kinds returns the registered kinds, sorted, so that a list rendered to an
// operator does not reshuffle itself between requests.
func (r *Registry) Kinds() []store.ProviderKind {
	out := make([]store.ProviderKind, 0, len(r.factories))
	for k := range r.factories {
		out = append(out, k)
	}
	slices.Sort(out)
	return out
}

// New builds the provider for one configuration row and checks that what it
// says about itself is true before handing it to anyone.
//
// The checks are here rather than in the reconciler because a capability that
// lies is a programming error in a provider, and the honest moment to catch one
// is when the provider is built: a reconciler that discovered it would discover
// it mid-create, on a machine somebody is paying for.
func (r *Registry) New(ctx context.Context, kind store.ProviderKind, cfg Config) (Provider, error) {
	f, err := r.Get(kind)
	if err != nil {
		return nil, err
	}
	p, err := f.New(ctx, cfg)
	if err != nil {
		return nil, err
	}
	if p == nil {
		return nil, fmt.Errorf("provider: the %q factory returned no provider and no error", kind)
	}
	if p.Kind() != kind {
		return nil, fmt.Errorf("provider: the %q factory built a %q provider", kind, p.Kind())
	}
	if err := checkContract(kind, p.Capabilities()); err != nil {
		return nil, err
	}
	if err := CheckCapabilities(p); err != nil {
		return nil, err
	}
	if err := checkDescription(f, p); err != nil {
		return nil, err
	}
	return p, nil
}

// CheckCapabilities reports whether a provider's declared capabilities agree
// with its type.
//
// Each optional capability is a separate interface found by type assertion, so
// the flag and the method set can disagree, and both directions are bugs worth
// naming. A flag that says yes where the type says no has the reconciler call a
// method that is not there; a flag that says no where the type says yes loses a
// capability silently, and the machine an operator expected to be stopped is
// deleted and bought again instead.
//
// It is exported because the conformance suite runs it against every provider
// and a provider's own tests may want it directly.
func CheckCapabilities(p Provider) error {
	caps := p.Capabilities()
	_, power := p.(PowerController)
	if caps.CanStartStop != power {
		return fmt.Errorf("provider: %s says CanStartStop=%t but %s PowerController; make the two agree",
			p.Kind(), caps.CanStartStop, implements(power))
	}
	_, discover := p.(Discoverer)
	if caps.CanDiscover != discover {
		return fmt.Errorf("provider: %s says CanDiscover=%t but %s Discoverer; make the two agree",
			p.Kind(), caps.CanDiscover, implements(discover))
	}
	_, boot := p.(Bootstrapper)
	if wantBoot := caps.Bootstrap == BootstrapGuestAgent; wantBoot != boot {
		return fmt.Errorf("provider: %s declares bootstrap mode %q but %s Bootstrapper; a provider that pushes the payload itself implements it, and one that does not says so",
			p.Kind(), caps.Bootstrap, implements(boot))
	}
	return nil
}

func implements(ok bool) string {
	if ok {
		return "implements"
	}
	return "does not implement"
}

// checkDescription holds a factory's description and its instances together. A
// page that lists what a kind can do is rendered from Describe() without
// building anything, so a description that disagrees with the provider is a
// promise made to an operator that the fleet then breaks.
func checkDescription(f Factory, p Provider) error {
	d, c := f.Describe(), p.Capabilities()
	switch {
	case d.CanStartStop != c.CanStartStop:
		return fmt.Errorf("provider: the %s factory describes CanStartStop=%t and builds %t", p.Kind(), d.CanStartStop, c.CanStartStop)
	case d.CanDiscover != c.CanDiscover:
		return fmt.Errorf("provider: the %s factory describes CanDiscover=%t and builds %t", p.Kind(), d.CanDiscover, c.CanDiscover)
	case d.Bootstrap != c.Bootstrap:
		return fmt.Errorf("provider: the %s factory describes bootstrap mode %q and builds %q", p.Kind(), d.Bootstrap, c.Bootstrap)
	}
	return nil
}

// checkContract is the version handshake, in one place so that the factory and
// the instance are held to the same range.
func checkContract(kind store.ProviderKind, caps Capabilities) error {
	if caps.MinContract > caps.MaxContract {
		return fmt.Errorf("%w: the %s provider declares contract range %d..%d, which contains nothing",
			ErrUnsupported, kind, caps.MinContract, caps.MaxContract)
	}
	if ContractVersion < caps.MinContract {
		return fmt.Errorf("%w: the %s provider speaks contract %d..%d and this build speaks %d; upgrade Zoomies",
			ErrUnsupported, kind, caps.MinContract, caps.MaxContract, ContractVersion)
	}
	if ContractVersion > caps.MaxContract {
		return fmt.Errorf("%w: the %s provider speaks contract %d..%d and this build speaks %d; upgrade the provider",
			ErrUnsupported, kind, caps.MinContract, caps.MaxContract, ContractVersion)
	}
	return nil
}
