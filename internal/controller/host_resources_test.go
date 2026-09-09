package controller

import (
	"strings"
	"testing"

	"github.com/eyupio/zoomies/internal/store"
)

// An operator can see what a host is and what is left on it; without this they
// could not see what the fleet has already promised away, which is the number
// that explains a host with free slots taking nothing.
//
// It is the scheduler's own sum rather than a second one computed for the page:
// a figure on screen that disagreed with the one the pass placed against would
// be worse than no figure, because it would be believed.
func TestTheHostViewCarriesWhatTheFleetHasPromisedAway(t *testing.T) {
	h := newHarness(t)
	inst := h.installation()
	pool := h.pool(inst, "linux-x64")
	pool.Resources = store.Resources{CPUs: 2, MemoryMB: 4096}
	if err := h.st.UpdatePool(h.ctx, pool); err != nil {
		t.Fatalf("UpdatePool: %v", err)
	}
	host := h.host("vm-1")
	h.runnerRow(pool, host, store.RunnerIdle)
	h.runnerRow(pool, host, store.RunnerBusy)
	// A failed runner is not charged for, exactly as it does not hold a slot.
	failed := h.runnerRow(pool, host, store.RunnerIdle)
	if _, err := h.st.TransitionRunner(h.ctx, failed.ID, store.RunnerFailed, "failed"); err != nil {
		t.Fatalf("TransitionRunner: %v", err)
	}

	// Before a pass has run there is no answer, and saying zero would read as
	// an idle machine rather than as a question nobody has asked yet.
	if view := h.c.HostView(host); view.ReservedKnown {
		t.Error("a controller that has decided nothing reported a reservation anyway")
	}
	if err := h.c.Reconcile(h.ctx); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}

	view := h.c.HostView(host)
	if !view.ReservedKnown {
		t.Fatal("a pass has run and the host still reports no reservation")
	}
	if view.ReservedCPUs != 4 || view.ReservedMemoryMB != 8192 {
		t.Errorf("reserved = %v CPUs / %d MB, want the two live runners' 4 and 8192 (the failed one is not charged)",
			view.ReservedCPUs, view.ReservedMemoryMB)
	}
	if !view.ResourcesKnown {
		t.Error("a host that reported its machine says its resources are unknown")
	}
	if view.AllocatableMemoryMB != 64*1024-store.MinHostReserveMemoryMB {
		t.Errorf("allocatable memory = %d, want the machine less the floor", view.AllocatableMemoryMB)
	}
}

// A host whose agent predates the resource fields is placed by slots alone,
// which is what keeps an upgrade from emptying a fleet -- and the page has to
// say so, because a card with every figure missing reads as broken rather than
// as old.
func TestAHostThatHasNotMeasuredItselfSaysSo(t *testing.T) {
	h := newHarness(t)
	silent := &store.Host{Name: "old-agent", Capacity: 2, Backends: store.StringSlice{"docker"},
		Labels: store.StringMap{}, OS: "linux", Arch: "amd64", LastHeartbeat: h.c.Now()}
	if err := h.st.CreateHost(h.ctx, silent); err != nil {
		t.Fatalf("CreateHost: %v", err)
	}

	if h.c.HostView(silent).ResourcesKnown {
		t.Error("a host that has reported nothing claims its resources are known")
	}
	problems := h.problemCodes()
	if !contains(problems, "host.resources_unknown") {
		t.Fatalf("problems = %v, want host.resources_unknown", problems)
	}
	all, err := h.c.Problems(h.ctx)
	if err != nil {
		t.Fatalf("Problems: %v", err)
	}
	for _, p := range all {
		if p.Code == "host.resources_unknown" {
			if !strings.Contains(p.Detail, "old-agent") {
				t.Errorf("the note does not name the host: %q", p.Detail)
			}
			if p.Severity != "info" {
				t.Errorf("severity = %q, want info: nothing is wrong with an old agent, it is just unmeasured", p.Severity)
			}
		}
	}
}

// A pool's resources become cgroup limits on the container backends and
// nothing at all on the process backend. The reservation still holds the room,
// so the fleet does not oversubscribe -- but the room is bookkeeping, and
// saying so is the difference between a limit and a wish.
func TestAProcessPoolWithLimitsIsWarnedAbout(t *testing.T) {
	h := newHarness(t)
	inst := h.installation()
	pool := h.pool(inst, "on-metal")
	pool.Backend = store.BackendProcess
	pool.Resources = store.Resources{MemoryMB: 4096}
	if err := h.st.UpdatePool(h.ctx, pool); err != nil {
		t.Fatalf("UpdatePool: %v", err)
	}

	if !contains(h.problemCodes(), "pool.resources_unenforced") {
		t.Fatalf("problems = %v, want pool.resources_unenforced", h.problemCodes())
	}

	// The same pool on a backend that enforces them raises nothing: the
	// warning is about the backend, not about setting limits.
	pool.Backend = store.BackendDocker
	if err := h.st.UpdatePool(h.ctx, pool); err != nil {
		t.Fatalf("UpdatePool: %v", err)
	}
	if contains(h.problemCodes(), "pool.resources_unenforced") {
		t.Errorf("problems = %v; a docker pool enforces its limits and needs no warning", h.problemCodes())
	}
}
