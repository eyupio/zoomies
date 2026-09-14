package controller

import (
	"testing"
	"time"

	"github.com/eyupio/zoomies/internal/provider"
	"github.com/eyupio/zoomies/internal/store"
)

// A restart is the pass where nothing was in memory, and these are the
// instants it can happen at.
//
// The table is the acceptance criterion "controller restart at every operation
// boundary" written out: one row per boundary, each one carrying the machine to
// that exact instant, restarting the controller there, and letting the new one
// carry on. Every row ends on the same two assertions -- one resource per
// machine, and exactly one create per machine at the provider -- because the
// failure every boundary could produce is the same failure: two machines, one
// row, one of them invisible and both of them on the bill.
func TestAMachineSurvivesARestartAtEveryOperationBoundary(t *testing.T) {
	cases := []struct {
		name string
		// reach carries the machine to the boundary and returns its id.
		reach func(t *testing.T, h *harness) string
		// want is where the machine should end up once a fresh controller has
		// had a few passes at it.
		want store.MachineState
	}{{
		// Nothing was created: the identity is written before the call, and
		// this row has none. Retrying costs nothing.
		name: "after the reservation, before anything was allocated",
		reach: func(t *testing.T, h *harness) string {
			h.machineFleet(t)
			h.fake.SetFailureOnce("allocate", provider.FailureUnreachable, "dial tcp: connection refused")
			h.machinePass(t)
			return h.onlyMachine(t).ID
		},
		want: store.MachineCreating,
	}, {
		// The identity is persisted and the create went out. A new controller
		// holds nothing but the row and has to ask what happened.
		name: "after the create was issued",
		reach: func(t *testing.T, h *harness) string {
			h.machineFleet(t)
			h.machinePass(t)
			return h.onlyMachine(t).ID
		},
		want: store.MachineBootstrapping,
	}, {
		// The request went out and the answer never came back. This is the one
		// the whole design turns on: retrying it buys a second machine.
		//
		// It stays creating, and that is the right answer: the machine exists,
		// the fleet has adopted it, and it moves on when the provider says it
		// is running. What must not happen is a second create, which is what
		// the shared assertion below is for.
		name: "after a create whose answer was never heard",
		reach: func(t *testing.T, h *harness) string {
			h.machineFleet(t)
			h.fake.SetAmbiguousAfterWork("create")
			h.machinePass(t)
			h.fake.ClearFailures()
			return h.onlyMachine(t).ID
		},
		want: store.MachineCreating,
	}, {
		// The clone is still running. The handle is the only record of it, and
		// it is on the row rather than in the pass that issued it.
		name: "while the create is still running",
		reach: func(t *testing.T, h *harness) string {
			h.machineFleet(t)
			h.fake.SetAsync("create", 2)
			h.machinePass(t)
			return h.onlyMachine(t).ID
		},
		want: store.MachineBootstrapping,
	}, {
		name: "after the machine came up and before the agent was installed",
		reach: func(t *testing.T, h *harness) string {
			h.machineFleet(t)
			h.machinePass(t)
			id := h.onlyMachine(t).ID
			h.drive(t, id, store.MachineBootstrapping, 4)
			return id
		},
		want: store.MachineEnrolling,
	}, {
		// The token was minted and the payload was written. A new controller
		// must not mint a second machine for a guest that is already joining.
		name: "after the agent was installed and before it joined",
		reach: func(t *testing.T, h *harness) string {
			h.machineFleet(t)
			h.machinePass(t)
			id := h.onlyMachine(t).ID
			h.drive(t, id, store.MachineEnrolling, 6)
			return id
		},
		want: store.MachineEnrolling,
	}, {
		// The agent joined and the link was never written. The token names the
		// machine and the host, so the link is recoverable from the rows alone.
		name: "after the agent joined and before the link was written",
		reach: func(t *testing.T, h *harness) string {
			h.machineFleet(t)
			h.machinePass(t)
			id := h.onlyMachine(t).ID
			m := h.drive(t, id, store.MachineEnrolling, 6)
			h.joinAsMachine(t, m)
			return id
		},
		want: store.MachineReady,
	}, {
		// A delete that was issued cannot be un-issued, and a restart in the
		// middle of one must finish it rather than start again elsewhere.
		name: "after a delete was issued",
		reach: func(t *testing.T, h *harness) string {
			_, row := h.machineFleet(t)
			m := h.readyMachine(t, row)
			h.beginDrainFor(t, m)
			h.machinePass(t)
			return m.ID
		},
		want: store.MachineDeleted,
	}}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := newHarness(t)
			id := tc.reach(t, h)

			// The restart itself: a fresh controller over the same store, with
			// the same provider still holding whatever it was holding.
			h.c = h.restart()

			for range 8 {
				if h.machineByID(t, id).State == tc.want {
					break
				}
				h.pastBackoff(t, id)
				h.machinePass(t)
			}
			m := h.machineByID(t, id)
			if m.State != tc.want {
				t.Fatalf("after the restart the machine is %s, want %s (message: %s, provider error: %s)",
					m.State, tc.want, m.Message, m.ProviderError)
			}
			h.assertOneResourcePerMachine(t)
		})
	}
}

// A create that succeeded after the fleet gave up must not put a machine back
// into service. The row is failed and the resource is reported, because a
// resource that exists and is not being watched is worse than one that is
// simply gone.
func TestALateCreateSuccessDoesNotResurrectAMachineTheFleetGaveUpOn(t *testing.T) {
	h := newHarness(t)
	h.machineFleet(t)
	h.fake.SetFailure("create", provider.FailureConfig, "the template id in this provider's settings does not exist")

	h.machinePass(t)
	m := h.onlyMachine(t)
	if m.State != store.MachineFailed {
		t.Fatalf("a create refused for a reason only a person can fix left the machine %s, want failed", m.State)
	}

	// The provider comes good, and the resource turns up anyway.
	h.fake.ClearFailures()
	h.fake.PlantOrphan(m.Name, provider.Owner{
		ControllerID: m.OwnerControllerID, ProviderID: m.ProviderID,
		MachineID: m.ID, Fingerprint: m.OwnerFingerprint,
	})
	h.advance(time.Minute)
	h.machinePass(t)

	if got := h.machineByID(t, m.ID); got.State == store.MachineReady {
		t.Fatal("a machine the fleet gave up on came back into service on its own")
	}
}

// A machine that failed for a reason nothing but a person can change is failed
// at once rather than retried five times. There is nothing to wait for, and the
// only thing four more attempts buy is four more minutes before somebody reads
// the reason.
func TestAFailureOnlyAPersonCanFixIsNotRetried(t *testing.T) {
	h := newHarness(t)
	h.machineFleet(t)
	h.fake.SetFailure("allocate", provider.FailurePermission,
		"the API token is missing VM.Allocate on /vms")

	h.machinePass(t)
	m := h.onlyMachine(t)
	if m.State != store.MachineFailed {
		t.Fatalf("a refused privilege left the machine %s, want failed", m.State)
	}
	if got := h.callsTo("allocate"); got != 1 {
		t.Fatalf("the provider was asked %d times for something it will never allow, want once", got)
	}
}
