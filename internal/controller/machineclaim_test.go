package controller

import (
	"context"
	"errors"
	"testing"

	"github.com/eyupio/zoomies/internal/agent"
	"github.com/eyupio/zoomies/internal/auth"
	"github.com/eyupio/zoomies/internal/provider"
	"github.com/eyupio/zoomies/internal/store"
)

// claimingFake is a provider whose machines cannot be pushed into, so they
// enrol by asking: it has no Bootstrap, and it says every machine claims. It
// stands for a Proxmox container, which is the one real machine of that kind.
type claimingFake struct {
	provider.Fake
	// noAddress makes the provider unable to say where a machine is yet.
	noAddress bool
	// pushed counts payloads the controller tried to push into a machine.
	pushed *int
	// tamper changes what Inspect reports, after the machine exists, which is how
	// a test puts somebody else's resource at a machine's identifier.
	tamper *func(*provider.Machine)
}

func (c claimingFake) Claims(provider.MachineRef) bool { return true }

// Bootstrap exists because the real provider's does: a Proxmox provider
// declares the guest agent and implements Bootstrapper for VMs and containers
// alike, and the controller has to prefer Claims over it. A push is a failure.
func (c claimingFake) Bootstrap(context.Context, provider.MachineRef, provider.Bootstrap) (provider.OperationRef, error) {
	*c.pushed++
	return provider.OperationRef{}, errors.New("a machine that claims was pushed into")
}

// The harness's fake declares a metadata bootstrap once it loses its own, which
// would put a credential in the create; a claiming provider says what the real
// one does, that nothing travels with the create.
func (c claimingFake) Capabilities() provider.Capabilities {
	caps := c.Fake.Capabilities()
	caps.Bootstrap = provider.BootstrapGuestAgent
	return caps
}

func (c claimingFake) Start(ctx context.Context, ref provider.MachineRef) (provider.OperationRef, error) {
	return c.Fake.(provider.PowerController).Start(ctx, ref)
}

func (c claimingFake) Stop(ctx context.Context, ref provider.MachineRef, graceful bool) (provider.OperationRef, error) {
	return c.Fake.(provider.PowerController).Stop(ctx, ref, graceful)
}

func (c claimingFake) Inspect(ctx context.Context, ref provider.MachineRef) (provider.Machine, error) {
	m, err := c.Fake.Inspect(ctx, ref)
	if c.noAddress {
		m.Address = ""
	}
	if c.tamper != nil && *c.tamper != nil {
		(*c.tamper)(&m)
	}
	return m, err
}

// claimingMachine runs a machine of a claiming provider to the point its agent
// would ask for its credential, and returns it with the address the provider
// reports for it.
func (h *harness) claimingMachine(t *testing.T, noAddress bool) (*store.Machine, string) {
	t.Helper()
	m, addr, _ := h.claimingMachinePushes(t, noAddress)
	return m, addr
}

func (h *harness) claimingMachinePushes(t *testing.T, noAddress bool) (*store.Machine, string, *int) {
	t.Helper()
	m, addr, pushed, _ := h.claimingMachineTamperable(t, noAddress)
	return m, addr, pushed
}

func (h *harness) claimingMachineTamperable(t *testing.T, noAddress bool) (*store.Machine, string, *int, *func(*provider.Machine)) {
	t.Helper()
	// Discovery goes too: the wrapper embeds the fake as an interface, which
	// carries none of its optional methods, and the registry holds a provider's
	// declared capabilities to its method set.
	inner := provider.NewFake(provider.FakeWithoutBootstrap(), provider.FakeWithoutDiscovery())
	pushed, tamper := new(int), new(func(*provider.Machine))
	h.fakeFactory.fake = claimingFake{Fake: inner, noAddress: noAddress, pushed: pushed, tamper: tamper}
	h.machineFleet(t)
	h.machinePass(t)
	m := h.drive(t, h.onlyMachine(t).ID, store.MachineEnrolling, 8)
	machines := inner.Machines()
	if len(machines) != 1 {
		t.Fatalf("the provider holds %d machines, want 1", len(machines))
	}
	return m, machines[0].Address, pushed, tamper
}

// Nothing is pushed into a machine that claims, and nothing is minted for it
// until it asks: a credential nobody has asked for is a credential that can only
// be wasted or stolen.
func TestAMachineThatClaimsWaitsForItsAgentAndHasNoCredentialYet(t *testing.T) {
	h := newHarness(t)
	m, _, pushed := h.claimingMachinePushes(t, false)
	if m.JoinTokenID != "" {
		t.Errorf("a credential was minted for a machine that has not asked: %s", m.JoinTokenID)
	}
	if *pushed != 0 {
		t.Errorf("the controller pushed into a machine that claims %d time(s)", *pushed)
	}
	if got := m.Message; got == "" {
		t.Error("the machine says nothing about what it is waiting for")
	}
}

func TestAClaimFromTheAddressTheProviderReportsEnrolsTheMachine(t *testing.T) {
	h := newHarness(t)
	m, addr := h.claimingMachine(t, false)

	resp, err := h.c.Join(h.ctx, joinRequest(m.Name, agent.ClaimToken), addr)
	if err != nil {
		t.Fatalf("the machine's own claim was refused: %v", err)
	}
	got := h.machineByID(t, m.ID)
	if got.HostID != resp.HostID {
		t.Fatalf("the machine is linked to host %q, want the one that claimed, %q", got.HostID, resp.HostID)
	}
	tok, err := h.st.GetJoinToken(h.ctx, got.JoinTokenID)
	if err != nil {
		t.Fatalf("GetJoinToken: %v", err)
	}
	// Exactly the credential a pushed payload carries: single use, spent, and
	// scoped to this machine's name.
	if tok.UsedAt == nil || tok.MachineID != m.ID || tok.ExpectedName != m.Name {
		t.Errorf("the token is %+v, want one spent by and scoped to machine %s as %s", tok, m.ID, m.Name)
	}
	h.machinePass(t)
	if got := h.machineByID(t, m.ID); got.State != store.MachineReady {
		t.Errorf("the machine is %s after its agent enrolled, want ready", got.State)
	}
}

func TestAClaimFromAnotherAddressIsRefusedAndMintsNothing(t *testing.T) {
	h := newHarness(t)
	m, _ := h.claimingMachine(t, false)

	_, err := h.c.Join(h.ctx, joinRequest(m.Name, agent.ClaimToken), "203.0.113.9")
	if !errors.Is(err, auth.ErrInvalidInput) {
		t.Fatalf("a claim from a stranger returned %v, want a refusal its sender can act on", err)
	}
	if got := h.machineByID(t, m.ID); got.JoinTokenID != "" || got.HostID != "" {
		t.Errorf("a refused claim left a credential or a host behind: %+v", got)
	}
}

// The caller is anonymous, so what it is told must not say whether a machine of
// that name exists: the name is readable by anyone who can audit the hypervisor,
// and an answer that differed would let them test which are waiting.
func TestEveryRefusedClaimGivesTheSameAnswer(t *testing.T) {
	h := newHarness(t)
	m, addr := h.claimingMachine(t, false)

	_, wrongAddress := h.c.Join(h.ctx, joinRequest(m.Name, agent.ClaimToken), "203.0.113.9")
	_, unknownName := h.c.Join(h.ctx, joinRequest("zoomies-nobody", agent.ClaimToken), addr)
	if wrongAddress == nil || unknownName == nil {
		t.Fatalf("a claim that should be refused was not: %v, %v", wrongAddress, unknownName)
	}
	if wrongAddress.Error() != unknownName.Error() {
		t.Errorf("the refusals differ, which says whether a machine exists:\n  wrong address: %v\n  unknown name:  %v", wrongAddress, unknownName)
	}
}

func TestAClaimBeforeTheProviderKnowsWhereTheMachineIsIsRefused(t *testing.T) {
	h := newHarness(t)
	m, addr := h.claimingMachine(t, true)
	if _, err := h.c.Join(h.ctx, joinRequest(m.Name, agent.ClaimToken), addr); !errors.Is(err, auth.ErrInvalidInput) {
		t.Fatalf("a claim the provider cannot vouch for returned %v, want a refusal", err)
	}
	if got := h.machineByID(t, m.ID); got.JoinTokenID != "" {
		t.Error("a credential was minted for a claim nothing vouched for")
	}
}

func TestAMachineClaimsOnce(t *testing.T) {
	h := newHarness(t)
	m, addr := h.claimingMachine(t, false)
	if _, err := h.c.Join(h.ctx, joinRequest(m.Name, agent.ClaimToken), addr); err != nil {
		t.Fatalf("first claim: %v", err)
	}
	if _, err := h.c.Join(h.ctx, joinRequest(m.Name, agent.ClaimToken), addr); err == nil {
		t.Fatal("a second claim for the same machine was answered")
	}
}

// A machine the provider pushes into has a credential written into it. A claim
// for one is the same request from a caller with no business making it, and is
// refused whatever address it comes from.
func TestAClaimForAMachineThatIsPushedToIsRefused(t *testing.T) {
	h := newHarness(t)
	h.machineFleet(t)
	h.machinePass(t)
	m := h.drive(t, h.onlyMachine(t).ID, store.MachineEnrolling, 6)
	addr := h.fake.Machines()[0].Address

	if _, err := h.c.Join(h.ctx, joinRequest(m.Name, agent.ClaimToken), addr); !errors.Is(err, auth.ErrInvalidInput) {
		t.Fatalf("a claim for a machine that was pushed to returned %v, want a refusal", err)
	}
}

// Somebody who can read the sentinel off the wire gets nothing from it on its
// own: it names no machine, and the name is the claim's only subject.
func TestAClaimNamingAHostThatIsNotAMachineIsRefused(t *testing.T) {
	h := newHarness(t)
	h.claimingMachine(t, false)
	if _, err := h.c.Join(h.ctx, joinRequest("builder-07", agent.ClaimToken), "203.0.113.9"); !errors.Is(err, auth.ErrInvalidInput) {
		t.Fatalf("a claim naming no machine returned %v, want a refusal", err)
	}
}

// A fenced fleet may hold a restored database whose machines belong to a
// controller that is still running, so it mints nothing for a machine's agent
// either, exactly as it pushes nothing into a VM.
func TestAFencedFleetAnswersNoClaim(t *testing.T) {
	h := newHarness(t)
	m, addr := h.claimingMachine(t, false)
	h.fence("restored from a backup")

	if _, err := h.c.Join(h.ctx, joinRequest(m.Name, agent.ClaimToken), addr); !errors.Is(err, auth.ErrInvalidInput) {
		t.Fatalf("a claim on a fenced fleet returned %v, want a refusal", err)
	}
	if got := h.machineByID(t, m.ID); got.JoinTokenID != "" {
		t.Error("a credential was minted while the fleet was fenced")
	}
}

// The address alone proves a caller is at the machine's VMID, not that what is
// there is the machine. A container destroyed out of band and its VMID reused
// answers at an address of its own, and whoever is inside it can read the
// machine's name off the console: the same check that stands between a sweep and
// a delete has to stand between a claim and a credential.
func TestAClaimIsRefusedWhenTheResourceAtTheMachinesIdentifierIsSomebodyElses(t *testing.T) {
	for name, tamper := range map[string]func(*provider.Machine){
		"a resource with another name": func(m *provider.Machine) { m.Ref.Name = "somebody-elses-container" },
		"a resource marked as another machine's": func(m *provider.Machine) {
			m.Owner = provider.Owner{MachineID: "mach_someone_else", ProviderID: m.Owner.ProviderID}
		},
	} {
		t.Run(name, func(t *testing.T) {
			h := newHarness(t)
			m, addr, _, hook := h.claimingMachineTamperable(t, false)
			*hook = tamper

			if _, err := h.c.Join(h.ctx, joinRequest(m.Name, agent.ClaimToken), addr); !errors.Is(err, auth.ErrInvalidInput) {
				t.Fatalf("a claim from somebody else's resource returned %v, want a refusal", err)
			}
			got := h.machineByID(t, m.ID)
			if got.JoinTokenID != "" || got.HostID != "" {
				t.Errorf("a refused claim left a credential or a host behind: %+v", got)
			}
			// Refused rather than acted on: an anonymous request does not get to
			// quarantine a machine, which is the machine loop's own call.
			if got.State == store.MachineQuarantined {
				t.Error("a claim quarantined the machine; only a pass of the machine loop may")
			}
		})
	}
}
