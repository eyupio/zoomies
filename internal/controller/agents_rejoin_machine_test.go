package controller

import (
	"testing"
	"time"

	"github.com/eyupio/zoomies/internal/store"
)

// A host that joins again has its row replaced under the same id, and the
// machine's link to it went with the old row. The machine loop then read a
// ready machine with no host as one to release, and destroyed a rented VM that
// had just come back, with whatever it was running.
func TestARentedHostJoiningAgainKeepsItsMachine(t *testing.T) {
	h := newHarness(t)
	_, row := h.machineFleet(t)
	h.machinePass(t)
	var m *store.Machine
	for _, got := range h.machines() {
		if got.ProviderID == row.ID {
			m = got
		}
	}
	if m == nil {
		t.Fatal("no machine was bought")
	}
	m = h.drive(t, m.ID, store.MachineEnrolling, 8)
	first, err := h.c.Join(h.ctx, joinRequest(m.Name, h.tokenFromBootstrap(t, m)), "203.0.113.9")
	if err != nil {
		t.Fatalf("joining as the machine: %v", err)
	}
	h.machinePass(t)
	h.drive(t, m.ID, store.MachineReady, 3)

	_, token, err := h.c.Auth().CreateJoinToken(h.ctx, time.Hour, nil, 2, "usr_test")
	if err != nil {
		t.Fatalf("CreateJoinToken: %v", err)
	}
	again := joinRequest(m.Name, token)
	again.PreviousHostID, again.PreviousToken = first.HostID, first.AgentToken
	if _, err := h.c.Join(h.ctx, again, "203.0.113.9"); err != nil {
		t.Fatalf("joining again: %v", err)
	}

	for range 3 {
		h.machinePass(t)
		h.c.machines.calls.Wait()
	}
	got := h.machineByID(t, m.ID)
	if got.HostID != first.HostID || got.State != store.MachineReady {
		t.Fatalf("after its host joined again the machine is %q with host %q; want ready and still host %s",
			got.State, got.HostID, first.HostID)
	}
}
