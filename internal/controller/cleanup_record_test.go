package controller

import (
	"strings"
	"testing"

	"github.com/eyupio/zoomies/internal/agent"
	"github.com/eyupio/zoomies/internal/store"
)

// The host and GitHub finish independently. One side succeeding must not
// erase the other side's failure, including when both failed first.
func TestHostCleanupDoesNotHideARegistrationThatStillExists(t *testing.T) {
	for _, hostFailsFirst := range []bool{false, true} {
		name := "registration failure only"
		if hostFailsFirst {
			name = "both sides failed"
		}
		t.Run(name, func(t *testing.T) {
			h := newHarness(t)
			_, pool, host := h.fleet()
			r := h.runnerRow(pool, host, store.RunnerIdle)
			if err := h.st.SetRunnerGitHubID(h.ctx, r.ID, 4242); err != nil {
				t.Fatal(err)
			}
			h.gh.SetError("/actions/runners/4242", 403, "Resource not accessible by integration")
			if _, err := h.c.removeRunner(h.ctx, h.runnerByID(t, r.ID), "scaling down", pool); err != nil {
				t.Fatal(err)
			}
			if hostFailsFirst {
				if err := h.c.ReportResult(h.ctx, host.ID, agent.TaskResult{
					TaskID: "remove_failed", Kind: agent.TaskRemoveRunner, RunnerID: r.ID,
					OK: false, Error: "container is in use",
				}); err != nil {
					t.Fatal(err)
				}
			}
			if err := h.c.ReportResult(h.ctx, host.ID, agent.TaskResult{
				TaskID: "remove_succeeded", Kind: agent.TaskRemoveRunner, RunnerID: r.ID,
				OK: true, State: store.RunnerRemoved,
			}); err != nil {
				t.Fatal(err)
			}
			got := h.runnerByID(t, r.ID)
			if !strings.Contains(got.CleanupError, "registration") || got.CleanupFailedAt == nil {
				t.Fatalf("host success hid the registration failure: error=%q failed_at=%v", got.CleanupError, got.CleanupFailedAt)
			}
			if got.CleanedUpAt != nil {
				t.Fatal("cleanup was marked complete while GitHub still refuses deletion")
			}
			if !contains(h.problemCodes(), "runners.cleanup_failed") {
				t.Fatal("the unfinished registration cleanup disappeared from Problems")
			}
			h.gh.ClearErrors()
			h.c.deleteRegistration(h.ctx, got, pool)
			got = h.runnerByID(t, r.ID)
			if got.CleanupError != "" || got.CleanupFailedAt != nil {
				t.Fatalf("successful registration retry did not clear its own failure: %q", got.CleanupError)
			}
		})
	}
}

func TestRegistrationReapingDoesNotHideAFailedHostRemoval(t *testing.T) {
	h := newHarness(t)
	_, pool, host := h.fleet()
	r := h.runnerRow(pool, host, store.RunnerRemoved)
	h.gh.AddRunner(r.Name, pool.Labels)
	if err := h.st.RecordRegistrationCleanupFailure(h.ctx, r.ID, "registration deletion refused"); err != nil {
		t.Fatal(err)
	}
	if err := h.c.ReportResult(h.ctx, host.ID, agent.TaskResult{
		TaskID: "remove_failed", Kind: agent.TaskRemoveRunner, RunnerID: r.ID,
		OK: false, Error: "container is in use",
	}); err != nil {
		t.Fatal(err)
	}
	h.c.reap(h.ctx)
	if len(h.gh.Runners()) != 0 {
		t.Fatal("the reaper did not delete the registration")
	}
	got := h.runnerByID(t, r.ID)
	if !strings.Contains(got.CleanupError, "container is in use") || strings.Contains(got.CleanupError, "registration") {
		t.Fatalf("the reaper must clear only its own error: %q", got.CleanupError)
	}
	if got.CleanedUpAt != nil || !contains(h.problemCodes(), "runners.cleanup_failed") {
		t.Fatal("the reaper hid the host's unfinished cleanup")
	}
}

// A remove that fails on a runner the fleet has already finished with used to
// vanish.
//
// The result arrives for a terminal row, the state machine refuses to move a
// removed runner to failed -- correctly, it cannot -- and before this the
// refusal was logged at debug and that was the end of it. Meanwhile the
// container is still on the host, holding its writable layer and, on a
// docker-in-docker pool, a privileged sidecar. Nobody was told.
func TestARemoveThatFailsOnATerminalRunnerIsRecorded(t *testing.T) {
	h := newHarness(t)
	_, pool, host := h.fleet()
	r := h.runnerRow(pool, host, store.RunnerIdle)
	if _, err := h.st.TransitionRunner(h.ctx, r.ID, store.RunnerRemoved, "removed"); err != nil {
		t.Fatalf("TransitionRunner: %v", err)
	}

	if err := h.c.ReportResult(h.ctx, host.ID, agent.TaskResult{
		TaskID: "task_1", Kind: agent.TaskRemoveRunner, RunnerID: r.ID,
		OK: false, Error: "the daemon refused: container is in use",
	}); err != nil {
		t.Fatalf("ReportResult: %v", err)
	}

	got := h.runnerByID(t, r.ID)
	if got.CleanupError == "" {
		t.Fatal("the failed remove left no record on the row; the container is on the host and nobody is told")
	}
	if !strings.Contains(got.CleanupError, "container is in use") {
		t.Errorf("cleanup_error = %q, want it to carry what the agent said", got.CleanupError)
	}
	if got.CleanupAttempts != 1 {
		t.Errorf("cleanup_attempts = %d, want 1", got.CleanupAttempts)
	}
	if got.CleanupFailedAt == nil {
		t.Error("cleanup_failed_at is unset, so the problem has no age")
	}
	// The row must not be dragged back through the state machine: its slot is
	// free and the scheduler has moved on.
	if got.State != store.RunnerRemoved {
		t.Errorf("state = %q, want it to stay removed: recording a tidying problem must not change capacity", got.State)
	}
}

// And it must be somewhere an operator looks, not only in a column.
func TestARunnerThatWillNotCleanUpRaisesAProblem(t *testing.T) {
	h := newHarness(t)
	_, pool, host := h.fleet()
	r := h.runnerRow(pool, host, store.RunnerIdle)
	if _, err := h.st.TransitionRunner(h.ctx, r.ID, store.RunnerRemoved, "removed"); err != nil {
		t.Fatalf("TransitionRunner: %v", err)
	}
	if contains(h.problemCodes(), "runners.cleanup_failed") {
		t.Fatalf("problems = %v; a runner that cleaned up fine is not a problem", h.problemCodes())
	}

	if err := h.c.ReportResult(h.ctx, host.ID, agent.TaskResult{
		TaskID: "task_1", Kind: agent.TaskRemoveRunner, RunnerID: r.ID,
		OK: false, Error: "the daemon refused",
	}); err != nil {
		t.Fatalf("ReportResult: %v", err)
	}
	if !contains(h.problemCodes(), "runners.cleanup_failed") {
		t.Fatalf("problems = %v, want runners.cleanup_failed", h.problemCodes())
	}
}

// A retry that works clears the complaint, and closes the runner's life.
// Otherwise the problems panel would keep naming a runner that is long gone,
// and an operator would learn to ignore it.
func TestACleanupThatEventuallyWorksClearsTheRecord(t *testing.T) {
	h := newHarness(t)
	_, pool, host := h.fleet()
	r := h.runnerRow(pool, host, store.RunnerIdle)
	if _, err := h.st.TransitionRunner(h.ctx, r.ID, store.RunnerDraining, "draining"); err != nil {
		t.Fatalf("TransitionRunner: %v", err)
	}
	if err := h.st.RecordCleanupFailure(h.ctx, r.ID, "remove_runner failed: the daemon refused"); err != nil {
		t.Fatalf("RecordCleanupFailure: %v", err)
	}

	if err := h.c.ReportResult(h.ctx, host.ID, agent.TaskResult{
		TaskID: "task_2", Kind: agent.TaskRemoveRunner, RunnerID: r.ID,
		OK: true, State: store.RunnerRemoved,
	}); err != nil {
		t.Fatalf("ReportResult: %v", err)
	}

	got := h.runnerByID(t, r.ID)
	if got.CleanupError != "" {
		t.Errorf("cleanup_error = %q, want it cleared once the removal worked", got.CleanupError)
	}
	if got.CleanedUpAt == nil {
		t.Error("cleaned_up_at is unset; the cleanup interval never closed")
	}
	// The count survives. How many tries it took is the difference between a
	// blip and a host worth looking at.
	if got.CleanupAttempts != 1 {
		t.Errorf("cleanup_attempts = %d, want the history kept", got.CleanupAttempts)
	}
	if contains(h.problemCodes(), "runners.cleanup_failed") {
		t.Fatalf("problems = %v; the problem should clear with the record", h.problemCodes())
	}
}

// A log task that fails says nothing about cleanup: the container is where it
// was, doing what it was doing. Recording it would send an operator looking for
// litter that is not there.
//
// The runner is terminal on purpose. That is the case where the guard is
// load-bearing -- on a live runner the failure would be an ordinary transition
// to failed and never reach the cleanup record at all.
func TestAFailedLogTaskIsNotACleanupFailure(t *testing.T) {
	h := newHarness(t)
	_, pool, host := h.fleet()
	r := h.runnerRow(pool, host, store.RunnerIdle)
	if _, err := h.st.TransitionRunner(h.ctx, r.ID, store.RunnerRemoved, "removed"); err != nil {
		t.Fatalf("TransitionRunner: %v", err)
	}

	if err := h.c.ReportResult(h.ctx, host.ID, agent.TaskResult{
		TaskID: "task_3", Kind: agent.TaskStreamLogs, RunnerID: r.ID,
		OK: false, Error: "the viewer went away",
	}); err != nil {
		t.Fatalf("ReportResult: %v", err)
	}
	if got := h.runnerByID(t, r.ID); got.CleanupError != "" {
		t.Errorf("cleanup_error = %q after a failed log relay, want none", got.CleanupError)
	}
}

// A registration GitHub will not delete is the other way something is left
// behind, and the one the roadmap named: it fills an organisation's runner
// list with dead entries. Before this it was a log line and a counter, so an
// operator's first sign of it was the list itself.
func TestARegistrationThatWillNotDeleteIsRecordedOnTheRunner(t *testing.T) {
	h := newHarness(t)
	inst, pool, host := h.fleet()
	r := h.runnerRow(pool, host, store.RunnerIdle)
	if err := h.st.SetRunnerGitHubID(h.ctx, r.ID, 4242); err != nil {
		t.Fatalf("SetRunnerGitHubID: %v", err)
	}
	h.gh.AddRunner(r.Name, pool.Labels)
	// The fake matches on the path alone, so the runner's own id is what makes
	// this refuse the delete and nothing else.
	h.gh.SetError("/actions/runners/4242", 403, "Resource not accessible by integration")

	fresh := h.runnerByID(t, r.ID)
	if _, err := h.c.removeRunner(h.ctx, fresh, "scaling down", pool); err != nil {
		t.Fatalf("removeRunner: %v", err)
	}

	got := h.runnerByID(t, r.ID)
	if got.CleanupError == "" {
		t.Fatal("a refused registration delete left no record; the ghost is invisible until somebody reads the runner list")
	}
	if !strings.Contains(got.CleanupError, "registration") {
		t.Errorf("cleanup_error = %q, want it to name the registration", got.CleanupError)
	}
	if got.RegistrationDeletedAt != nil {
		t.Error("registration_deleted_at is set for a deletion that was refused")
	}
	if !contains(h.problemCodes(), "runners.cleanup_failed") {
		t.Fatalf("problems = %v, want runners.cleanup_failed", h.problemCodes())
	}
	_ = inst
}

// And the ordinary case: a deletion that worked is stamped, so an unstamped
// terminal runner is exactly the set worth asking about.
func TestADeletedRegistrationIsStamped(t *testing.T) {
	h := newHarness(t)
	_, pool, host := h.fleet()
	r := h.runnerRow(pool, host, store.RunnerIdle)
	if err := h.st.SetRunnerGitHubID(h.ctx, r.ID, 4243); err != nil {
		t.Fatalf("SetRunnerGitHubID: %v", err)
	}
	h.gh.AddRunner(r.Name, pool.Labels)

	fresh := h.runnerByID(t, r.ID)
	if _, err := h.c.removeRunner(h.ctx, fresh, "scaling down", pool); err != nil {
		t.Fatalf("removeRunner: %v", err)
	}

	got := h.runnerByID(t, r.ID)
	if got.RegistrationDeletedAt == nil {
		t.Fatal("registration_deleted_at is unset after a successful delete")
	}
	if got.CleanupError != "" {
		t.Errorf("cleanup_error = %q after a clean removal, want none", got.CleanupError)
	}
	if got.CleanedUpAt == nil {
		t.Error("cleaned_up_at is unset; nothing marks the end of the runner's life")
	}
}
