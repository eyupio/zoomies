package controller

import (
	"context"
	"strings"
	"testing"

	"github.com/eyupio/zoomies/internal/provider"
	"github.com/eyupio/zoomies/internal/store"
)

// Two machines reserved in one pass are stepped at the same moment, and a
// provider that hands out its lowest free identifier (Proxmox) offers both the
// same one until the first of them exists. The loser used to fail for good on
// the unique index, on its first attempt, and count against the breaker: four
// machines asked for at once stood the provider down. It is a race between
// siblings and nothing is wrong with either, so the loser allocates again.
func TestAMachineOfferedTheIdItsSiblingJustTookAllocatesAgain(t *testing.T) {
	h := newHarness(t)
	_, row := h.machineFleet(t)

	// The fake provider mints "101" first, and the sibling already holds it.
	sibling := &store.Machine{ProviderID: row.ID, State: store.MachinePlanned}
	if err := h.st.CreateMachine(h.ctx, sibling); err != nil {
		t.Fatalf("CreateMachine: %v", err)
	}
	if err := h.st.SetMachineResource(h.ctx, sibling.ID, "zone-a", "101", "ours", h.c.controllerID()); err != nil {
		t.Fatalf("SetMachineResource: %v", err)
	}
	if _, err := h.st.TransitionMachine(h.ctx, sibling.ID, store.MachineCreating, ""); err != nil {
		t.Fatalf("TransitionMachine: %v", err)
	}
	loser := &store.Machine{ProviderID: row.ID, State: store.MachinePlanned}
	if err := h.st.CreateMachine(h.ctx, loser); err != nil {
		t.Fatalf("CreateMachine: %v", err)
	}

	h.machinePass(t)
	got := h.machineByID(t, loser.ID)
	if got.State != store.MachinePlanned || got.Attempts != 0 {
		t.Fatalf("the machine that lost the race is %s after %d attempts (%s), want planned and uncounted",
			got.State, got.Attempts, got.ProviderError)
	}

	h.machinePass(t)
	got = h.machineByID(t, loser.ID)
	if got.ResourceID == "" || got.ResourceID == "101" {
		t.Fatalf("the machine names %q (state %s, error %q), want an id of its own from the next pass",
			got.ResourceID, got.State, got.ProviderError)
	}
}

// startRefused is a provider whose start is taken and then fails as a task,
// which is how a hypervisor says a guest would not power on.
type startRefused struct {
	provider.Fake
	starts *int
}

const refusedStartHandle = "start-that-fails"

func (s startRefused) Start(context.Context, provider.MachineRef) (provider.OperationRef, error) {
	*s.starts++
	return provider.OperationRef{Kind: provider.OpStart, Handle: refusedStartHandle}, nil
}

func (s startRefused) Stop(ctx context.Context, ref provider.MachineRef, graceful bool) (provider.OperationRef, error) {
	return s.Fake.(provider.PowerController).Stop(ctx, ref, graceful)
}

// Inspect holds the machine stopped: the fake's create leaves one running, and
// a guest that would not power on is not.
func (s startRefused) Inspect(ctx context.Context, ref provider.MachineRef) (provider.Machine, error) {
	m, err := s.Fake.Inspect(ctx, ref)
	if m.Phase == provider.PhaseRunning {
		m.Phase = provider.PhaseStopped
	}
	return m, err
}

func (s startRefused) Operation(ctx context.Context, op provider.OperationRef) (provider.OperationStatus, error) {
	if op.Handle == refusedStartHandle {
		return provider.OperationStatus{Done: true, Detail: "kvm: cannot allocate memory"}, nil
	}
	return s.Fake.Operation(ctx, op)
}

// A start task that ends in failure used to be read as "not running yet": the
// start was issued again on every pass, uncounted and with no backoff, until
// the create timeout gave up with a sentence that named no cause. The
// hypervisor had said why the first time, and that is what an operator needs.
func TestAStartThatFailsSaysWhatTheProviderSaidAndBacksOff(t *testing.T) {
	h := newHarness(t)
	inner := provider.NewFake(provider.FakeWithoutBootstrap(), provider.FakeWithoutDiscovery())
	starts := new(int)
	h.fakeFactory.fake = startRefused{Fake: inner, starts: starts}
	h.machineFleet(t)
	h.machinePass(t)
	m := h.drive(t, h.onlyMachine(t).ID, store.MachineStarting, 6)

	// The start is issued, its task is read, and one more pass finds a backoff.
	for range 3 {
		h.machinePass(t)
	}

	m = h.machineByID(t, m.ID)
	if !strings.Contains(m.ProviderError, "cannot allocate memory") {
		t.Fatalf("the provider's own words did not reach the machine row: %q (attempts %d)", m.ProviderError, m.Attempts)
	}
	if *starts != 1 {
		t.Fatalf("the start was issued %d times in three passes, want once and then a backoff", *starts)
	}
}
