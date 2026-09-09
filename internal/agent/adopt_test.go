package agent

import (
	"context"
	"testing"
	"time"

	"github.com/eyupio/zoomies/internal/backend"
)

// removedHandles is what the backend was asked to destroy, in order.
func removedHandles(be *fakeBackend) []backend.Handle {
	be.mu.Lock()
	defer be.mu.Unlock()
	out := make([]backend.Handle, len(be.removed))
	copy(out, be.removed)
	return out
}

// seedRunning puts a workload on the backend as though an earlier agent had
// created it and the job were still running.
func seedRunning(be *fakeBackend, runnerID, name string, handle backend.Handle) {
	be.mu.Lock()
	defer be.mu.Unlock()
	be.workloads = append(be.workloads, backend.Workload{
		Handle:   handle,
		Name:     name,
		RunnerID: runnerID,
		Status:   backend.Status{Phase: backend.PhaseRunning, StartedAt: time.Now().Add(-time.Hour)},
	})
}

// The agent's unit restarts always, and on a single VM the agent lives inside
// the controller, so an agent starting over live workloads is an everyday
// event. A fresh agent knows nothing and the reconciler removes what nothing
// claims -- so before this, every job running on the host was destroyed two
// minutes after the agent came back.
func TestARestartedAgentKeepsTheRunnersAlreadyOnItsHost(t *testing.T) {
	a, _, be, clock := newAgent(t, 4)
	ctx := context.Background()
	seedRunning(be, "run_live", "zoomies-live", "wl-live")

	a.adoptExisting(ctx)

	// The reconciler runs as though a task poll had already succeeded, which
	// is what opens the reaping path at all.
	a.polled.Store(true)
	if _, err := a.ReconcileOnce(ctx); err != nil {
		t.Fatalf("ReconcileOnce: %v", err)
	}
	clock.advance(orphanGrace + time.Minute)
	if _, err := a.ReconcileOnce(ctx); err != nil {
		t.Fatalf("ReconcileOnce: %v", err)
	}

	if got := removedHandles(be); len(got) != 0 {
		t.Fatalf("a restarted agent removed live workloads: %v", got)
	}
	if _, ok := a.snapshot("run_live"); !ok {
		t.Error("the runner was not adopted, so the next pass would reap it")
	}
}

// Without adoption the same fixture is destroyed, which is the defect this
// change exists to fix. Proving it here means the test above cannot quietly
// stop testing anything.
func TestWithoutAdoptionARestartWouldReapALiveRunner(t *testing.T) {
	a, _, be, clock := newAgent(t, 4)
	ctx := context.Background()
	seedRunning(be, "run_live", "zoomies-live", "wl-live")

	// No adoptExisting: this is the old startup.
	a.polled.Store(true)
	if _, err := a.ReconcileOnce(ctx); err != nil {
		t.Fatalf("ReconcileOnce: %v", err)
	}
	clock.advance(orphanGrace + time.Minute)
	if _, err := a.ReconcileOnce(ctx); err != nil {
		t.Fatalf("ReconcileOnce: %v", err)
	}

	if got := removedHandles(be); len(got) != 1 || got[0] != "wl-live" {
		t.Fatalf("removed = %v, want the live workload: the fixture no longer reproduces the defect", got)
	}
}

// Adoption is also what stops the agent recognising genuine litter, so the
// controller says which runners it has no row for and only those are released.
func TestOnlyTheRunnersTheControllerDisownsAreReaped(t *testing.T) {
	a, _, be, clock := newAgent(t, 4)
	ctx := context.Background()
	seedRunning(be, "run_live", "zoomies-live", "wl-live")
	seedRunning(be, "run_gone", "zoomies-gone", "wl-gone")
	a.adoptExisting(ctx)
	a.polled.Store(true)

	// The controller knows run_live and has no row for run_gone.
	a.releaseUnknown([]string{"run_gone"})

	if _, err := a.ReconcileOnce(ctx); err != nil {
		t.Fatalf("ReconcileOnce: %v", err)
	}
	clock.advance(orphanGrace + time.Minute)
	if _, err := a.ReconcileOnce(ctx); err != nil {
		t.Fatalf("ReconcileOnce: %v", err)
	}

	got := removedHandles(be)
	if len(got) != 1 || got[0] != "wl-gone" {
		t.Fatalf("removed = %v, want only the workload the controller disowned", got)
	}
	if _, ok := a.snapshot("run_live"); !ok {
		t.Error("the runner the controller still knows was released as well")
	}
}

// A workload with no runner id is not this controller's to manage under a name
// it can use, so adoption leaves it to the orphan path as before.
func TestAWorkloadWithNoRunnerIdIsNotAdopted(t *testing.T) {
	a, _, be, _ := newAgent(t, 4)
	ctx := context.Background()
	seedRunning(be, "", "stray", "wl-stray")

	a.adoptExisting(ctx)

	if _, ok := a.snapshot(""); ok {
		t.Fatal("a workload carrying no runner id was adopted")
	}
}

// A backend that cannot be listed says nothing about what is on it, and
// starting anyway is the same position an agent is in a moment before its
// first successful list. It must not take the agent down.
func TestAdoptionSurvivesABackendThatCannotBeListed(t *testing.T) {
	a, _, be, _ := newAgent(t, 4)
	be.mu.Lock()
	be.listErr = context.DeadlineExceeded
	be.mu.Unlock()

	a.adoptExisting(context.Background())

	if _, ok := a.snapshot("run_live"); ok {
		t.Fatal("something was adopted from a backend that could not be listed")
	}
}

// seedSidecar puts an abandoned docker-in-docker daemon on the backend, ahead
// of anything already there. The order matters: it is what an agent looking a
// runner up by id would meet first, which is exactly the mistake being ruled
// out.
func seedSidecar(be *fakeBackend, runnerID, name string, handle backend.Handle) {
	be.mu.Lock()
	defer be.mu.Unlock()
	be.workloads = append([]backend.Workload{{
		Handle:   handle,
		Name:     name,
		RunnerID: runnerID,
		Sidecar:  true,
		Status:   backend.Status{Phase: backend.PhaseRunning, StartedAt: time.Now().Add(-time.Hour)},
	}}, be.workloads...)
}

// A sidecar carries its runner's id, so adopting by id alone binds the
// runner's slot to the daemon container. Every stop and remove the controller
// later sends for that runner then goes to the daemon, and the container
// running somebody's job is left behind with nothing tracking it.
func TestARestartedAgentAdoptsTheRunnerAndNotItsSidecar(t *testing.T) {
	a, _, be, _ := newAgent(t, 4)
	seedRunning(be, "run_live", "zoomies-live", "wl-live")
	seedSidecar(be, "run_live", "zoomies-live-dind", "wl-dind")

	a.adoptExisting(context.Background())

	r, ok := a.snapshot("run_live")
	if !ok {
		t.Fatal("the runner was not adopted, so the next pass would reap it")
	}
	if r.handle != "wl-live" {
		t.Fatalf("the runner's slot points at %q, which is its sidecar, not the runner", r.handle)
	}
}

// resolve is the other way a runner is found on the host: it runs when a stop
// or remove task arrives for a runner the agent has no memory of, which is the
// moment sending the command to the wrong container costs the most.
func TestResolvingARunnerIgnoresItsSidecar(t *testing.T) {
	a, _, be, _ := newAgent(t, 4)
	seedRunning(be, "run_live", "zoomies-live", "wl-live")
	seedSidecar(be, "run_live", "zoomies-live-dind", "wl-dind")

	_, handle, found, err := a.resolve(context.Background(), "run_live")
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if !found {
		t.Fatal("the runner is on the host and must be found")
	}
	if handle != "wl-live" {
		t.Fatalf("resolved to %q, which is the sidecar; a stop would spare the runner", handle)
	}
}

// Adoption has to happen on the startup path, not merely exist as a method.
// The tests above call adoptExisting directly, so removing the one call in Run
// leaves them all green while every job on a restarting host is destroyed
// again. This one starts the agent the way its unit does and asks whether the
// runner that was already here survived the start.
func TestStartingTheAgentAdoptsWhatIsAlreadyRunning(t *testing.T) {
	a, _, be, _ := newAgent(t, 4)
	if err := a.Join(context.Background(), "join-token"); err != nil {
		t.Fatalf("Join: %v", err)
	}
	seedRunning(be, "run_live", "zoomies-live", "wl-live")

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- a.Run(ctx) }()
	t.Cleanup(func() {
		cancel()
		select {
		case <-done:
		case <-time.After(10 * time.Second):
			t.Error("agent did not shut down within 10s")
		}
	})

	deadline := time.Now().Add(5 * time.Second)
	for {
		if _, ok := a.snapshot("run_live"); ok {
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("the started agent never adopted the runner already on its host, so the reconciler will reap it")
		}
		time.Sleep(5 * time.Millisecond)
	}
}
