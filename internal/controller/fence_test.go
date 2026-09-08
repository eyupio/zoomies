package controller

import (
	"strings"
	"testing"

	"github.com/eyupio/zoomies/internal/config"
	"github.com/eyupio/zoomies/internal/scheduler"
	"github.com/eyupio/zoomies/internal/store"
)

// fence puts the harness's fleet behind the fence, the way a restore does.
func (h *harness) fence(reason string) {
	h.t.Helper()
	if err := h.st.SetRecoveryFence(h.ctx, true, reason); err != nil {
		h.t.Fatalf("SetRecoveryFence: %v", err)
	}
	if err := h.c.LoadFence(h.ctx); err != nil {
		h.t.Fatalf("LoadFence: %v", err)
	}
}

// The fence's whole value is the difference between deciding and doing. A
// controller that stopped deciding would leave an operator recovering a fleet
// with nothing to look at, and no way to tell "the scheduler wants nothing"
// from "the scheduler is not allowed to want anything".
func TestAFencedFleetStillDecidesAndCreatesNothing(t *testing.T) {
	h := newHarness(t)
	h.fleet()
	h.deliverJob(jobEvent{Action: "queued", JobID: 1, Labels: []string{"self-hosted", "linux", "x64", "demo"}})

	h.fence("restored from a backup taken on Tuesday")
	if err := h.c.Reconcile(h.ctx); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}

	runners, _, err := h.st.ListRunners(h.ctx, store.RunnerFilter{}, store.Page{Limit: 50})
	if err != nil {
		t.Fatal(err)
	}
	if len(runners) != 0 {
		t.Fatalf("a fenced fleet created %d runners", len(runners))
	}

	// And the plan is there, saying what it would have done.
	plan, at := h.c.getLastPlan()
	if plan == nil || at.IsZero() {
		t.Fatal("a fenced pass recorded no plan, so the Overview has nothing to show")
	}
	var wanted int
	for _, pp := range plan.Pools {
		for _, a := range pp.Actions {
			if a.Kind == scheduler.ActionCreate {
				wanted++
			}
		}
	}
	if wanted == 0 {
		t.Error("the fenced pass decided to do nothing at all; the plan is meant to be what it would do")
	}

	// Lifting it does the work that was waiting.
	if err := h.c.Unfence(h.ctx); err != nil {
		t.Fatalf("Unfence: %v", err)
	}
	if err := h.c.Reconcile(h.ctx); err != nil {
		t.Fatalf("Reconcile after lifting: %v", err)
	}
	runners, _, err = h.st.ListRunners(h.ctx, store.RunnerFilter{}, store.Page{Limit: 50})
	if err != nil {
		t.Fatal(err)
	}
	if len(runners) == 0 {
		t.Error("lifting the fence did not let the fleet act")
	}
}

// A fenced controller looks exactly like a healthy one with nothing to do, so
// the drawer is the only thing that says otherwise. It is the highest
// consequence entry the drawer can carry, because everything else on it is
// something that went wrong and this is something somebody chose.
func TestTheFenceIsOnTheProblemsList(t *testing.T) {
	h := newHarness(t)
	h.fence("restored from /var/backups/zoomies/zoomies-20260908-181718")

	var p Problem
	all, err := h.c.Problems(h.ctx)
	if err != nil {
		t.Fatalf("Problems: %v", err)
	}
	for _, item := range all {
		if item.Code == "recovery.fenced" {
			p = item
		}
	}
	if p.Code == "" {
		t.Fatalf("the fence is not on the problems list: %v", h.problemCodes())
	}
	if p.Severity != config.SeverityError {
		t.Errorf("severity = %s; a fleet doing nothing is not a warning", p.Severity)
	}
	// The reason travels with it, because "fenced" without "why" sends an
	// operator looking for a fault that is not there.
	if !strings.Contains(p.Detail, "zoomies-20260908-181718") {
		t.Errorf("the detail does not say why it is fenced: %q", p.Detail)
	}
	// The fix names the checks rather than only the route: an operator who
	// lifts the fence without making them has spent the fence for nothing.
	for _, want := range []string{"external URL", "agents", "runners", "/api/v1/recovery/unfence"} {
		if !strings.Contains(p.Fix, want) {
			t.Errorf("the fix does not mention %q: %q", want, p.Fix)
		}
	}

	if err := h.c.Unfence(h.ctx); err != nil {
		t.Fatalf("Unfence: %v", err)
	}
	if contains(h.problemCodes(), "recovery.fenced") {
		t.Error("the fence is still on the list after being lifted")
	}
}

// A write that failed must leave a fenced controller rather than one that
// believes it is free: the fence is the safe side of its own question.
func TestUnfenceKeepsTheFenceWhenItCannotWriteIt(t *testing.T) {
	h := newHarness(t)
	h.fence("restored")
	if err := h.st.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if err := h.c.Unfence(h.ctx); err == nil {
		t.Fatal("Unfence reported success with no database to write to")
	}
	if !h.c.Fenced().Fenced {
		t.Error("the controller believes it is unfenced after a write that failed")
	}
}
