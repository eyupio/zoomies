package controller

import (
	"testing"
	"time"

	"github.com/eyupio/zoomies/internal/agent"
	"github.com/eyupio/zoomies/internal/store"
)

func TestTerminalJobWithoutStartDeliveryRemovesItsEphemeralRunner(t *testing.T) {
	for _, conclusion := range []string{"cancelled", "failure", "success"} {
		t.Run(conclusion, func(t *testing.T) {
			h := newHarness(t)
			_, pool, host := h.fleet()
			r := h.runnerRow(pool, host, store.RunnerRegistering)
			h.deliverJob(jobEvent{Action: "completed", JobID: 8010, Name: "build", Workflow: "CI",
				Labels: pool.Labels, Conclusion: conclusion, RunnerName: r.Name})
			got := h.runnerByID(t, r.ID)
			if got.State != store.RunnerRemoved || !h.hasTaskOfKind(host.ID, agent.TaskRemoveRunner) {
				t.Fatalf("completion without in_progress left runner %s without removal", got.State)
			}
			if got.JobsHandled != 1 {
				t.Fatalf("jobs handled = %d", got.JobsHandled)
			}
		})
	}
}

func TestCancelledDrainingRunnerIsRemovedAndPersistentDrainIsPreserved(t *testing.T) {
	for _, ephemeral := range []bool{true, false} {
		t.Run(map[bool]string{true: "ephemeral", false: "persistent"}[ephemeral], func(t *testing.T) {
			h := newHarness(t)
			_, pool, host := h.fleet()
			pool.Ephemeral = ephemeral
			if err := h.st.UpdatePool(h.ctx, pool); err != nil {
				t.Fatal(err)
			}
			_, runner := startJobOnRunner(t, h, host.ID, 8020, pool.Labels)
			if _, err := h.st.TransitionRunner(h.ctx, runner.ID, store.RunnerDraining, "operator drain"); err != nil {
				t.Fatal(err)
			}
			h.deliverJob(jobEvent{Action: "completed", JobID: 8020, Name: "build", Workflow: "CI", Conclusion: "cancelled", RunnerName: runner.Name})
			got := h.runnerByID(t, runner.ID)
			want := store.RunnerDraining
			if ephemeral {
				want = store.RunnerRemoved
			}
			if got.State != want || got.CurrentJobID != "" || got.JobsHandled != 1 {
				t.Fatalf("runner after cancellation: %+v", got)
			}
		})
	}
}

func TestHostRemovalFailureRetriesUntilConfirmed(t *testing.T) {
	h := newHarness(t)
	_, _, host := h.fleet()
	_, runner := startJobOnRunner(t, h, host.ID, 8021, []string{"self-hosted", "linux", "x64", "demo"})
	h.deliverJob(jobEvent{Action: "completed", JobID: 8021, Name: "build", Workflow: "CI", Conclusion: "cancelled", RunnerName: runner.Name})
	h.c.queues = newTaskQueues()
	if err := h.c.ReportResult(h.ctx, host.ID, agent.TaskResult{TaskID: "failed-remove", Kind: agent.TaskRemoveRunner, RunnerID: runner.ID, Error: "daemon unavailable"}); err != nil {
		t.Fatal(err)
	}
	if err := h.c.Reconcile(h.ctx); err != nil {
		t.Fatal(err)
	}
	if !h.hasTaskOfKind(host.ID, agent.TaskRemoveRunner) {
		t.Fatal("failed removal was abandoned")
	}
	if err := h.c.ReportResult(h.ctx, host.ID, agent.TaskResult{TaskID: "removed", Kind: agent.TaskRemoveRunner, RunnerID: runner.ID, OK: true, State: store.RunnerRemoved}); err != nil {
		t.Fatal(err)
	}
	h.c.queues = newTaskQueues()
	if err := h.c.Reconcile(h.ctx); err != nil {
		t.Fatal(err)
	}
	if h.hasTaskOfKind(host.ID, agent.TaskRemoveRunner) {
		t.Fatal("confirmed cleanup keeps being queued")
	}
}

func TestHostCleanupRecoveryDoesNotKillAnUnfinishedJob(t *testing.T) {
	h := newHarness(t)
	_, pool, host := h.fleet()
	_, runner := startJobOnRunner(t, h, host.ID, 8022, pool.Labels)
	if _, err := h.st.TransitionRunner(h.ctx, runner.ID, store.RunnerFailed, "host went away"); err != nil {
		t.Fatal(err)
	}
	if err := h.st.RecordCleanupFailure(h.ctx, runner.ID, "earlier removal failed"); err != nil {
		t.Fatal(err)
	}
	h.c.queues = newTaskQueues()
	if err := h.c.recoverHostCleanup(h.ctx); err != nil {
		t.Fatal(err)
	}
	if h.hasTaskOfKind(host.ID, agent.TaskRemoveRunner) {
		t.Fatal("cleanup queued while job is still in progress")
	}
}

func TestExhaustedRemoveLeaseRemainsRetryable(t *testing.T) {
	h := newHarness(t)
	_, pool, host := h.fleet()
	r := h.runnerRow(pool, host, store.RunnerRemoved)
	h.c.enqueueLifecycle(h.ctx, host.ID, agent.Task{Kind: agent.TaskRemoveRunner, RunnerID: r.ID})
	q := h.c.queues.get(host.ID)
	for attempt := 0; attempt < maxTaskAttempts; attempt++ {
		q.take(20, h.c.Now())
		h.advance(removeLease + time.Second)
		h.c.sweepTasks(h.ctx, h.c.Now())
	}
	if got := h.runnerByID(t, r.ID); got.CleanupError == "" {
		t.Fatal("unacknowledged removal was silently forgotten")
	}
	if err := h.c.recoverHostCleanup(h.ctx); err != nil {
		t.Fatal(err)
	}
	if !h.hasTaskOfKind(host.ID, agent.TaskRemoveRunner) {
		t.Fatal("exhausted cleanup was not retried")
	}
}

func TestCancelledJobCleanupSurvivesLostTaskQueue(t *testing.T) {
	h := newHarness(t)
	_, _, host := h.fleet()
	_, runner := startJobOnRunner(t, h, host.ID, 8011, []string{"self-hosted", "linux", "x64", "demo"})
	h.deliverJob(jobEvent{Action: "completed", JobID: 8011, Name: "build", Workflow: "CI", Conclusion: "cancelled", RunnerName: runner.Name})
	// Task queues are deliberately in memory. Simulate a controller restart
	// after recording completion but before the agent accepted removal.
	h.c.queues = newTaskQueues()
	if err := h.c.Reconcile(h.ctx); err != nil {
		t.Fatal(err)
	}
	if !h.hasTaskOfKind(host.ID, agent.TaskRemoveRunner) {
		t.Fatal("lost removal was not reconstructed")
	}
}
