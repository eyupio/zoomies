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

// A cancellation can arrive before GitHub assigns runner_name. The job then
// has no runner to clean up directly, even though its demand may already have
// caused a detached JIT mint to start. The next pass must stop that surplus
// capacity, and a JIT response arriving afterwards must not put a create task
// behind the stop and resurrect it.
func TestCancellationBeforeAssignmentStopsAProvisioningRunner(t *testing.T) {
	h := newHarness(t)
	_, _, host := h.fleet()
	h.gh.SetDelay("", "generate-jitconfig", 500*time.Millisecond)
	labels := []string{"self-hosted", "linux", "x64", "demo"}
	h.deliverJob(jobEvent{Action: "queued", JobID: 8030, Name: "build", Workflow: "CI", Labels: labels})

	if err := h.c.Reconcile(h.ctx); err != nil {
		t.Fatal(err)
	}
	runner := h.onlyRunner()
	if runner.State != store.RunnerProvisioning {
		t.Fatalf("runner state = %q before cancellation", runner.State)
	}

	// GitHub has not assigned a runner, so the completed event deliberately
	// carries no RunnerName.
	h.deliverJob(jobEvent{Action: "completed", JobID: 8030, Name: "build", Workflow: "CI",
		Labels: labels, Conclusion: "cancelled"})
	if err := h.c.Reconcile(h.ctx); err != nil {
		t.Fatal(err)
	}
	if got := h.runnerByID(t, runner.ID); got.State != store.RunnerDraining {
		t.Fatalf("cancelled demand left provisioning runner in %q, want draining", got.State)
	}
	if !h.hasTaskOfKind(host.ID, agent.TaskStopRunner) {
		t.Fatal("cancelled demand did not queue a stop task")
	}

	h.c.lifecycleCalls.Wait()
	got := h.runnerByID(t, runner.ID)
	if got.State != store.RunnerRemoved {
		t.Fatalf("late credential mint left runner in %q, want removed", got.State)
	}
	if h.hasTaskOfKind(host.ID, agent.TaskCreateRunner) {
		t.Fatal("late credential mint queued create after cancellation")
	}
	if len(h.gh.Runners()) != 0 {
		t.Fatalf("late credential mint leaked %d GitHub registrations", len(h.gh.Runners()))
	}
}

func TestCancelledStageDoesNotCancelItsWorkflowSiblings(t *testing.T) {
	h := newHarness(t)
	_, pool, host := h.fleet()
	remote := h.gh.AddQueuedJob("acme/widgets", "CI", "remote", pool.Labels)
	h.gh.StartJob(remote.ID, "some-runner") // the run itself is still in progress

	r := h.runnerRow(pool, host, store.RunnerIdle)
	h.deliverJob(jobEvent{Action: "queued", JobID: 8040, RunID: remote.RunID, Name: "matrix-a", Workflow: "CI", Labels: pool.Labels})
	h.deliverJob(jobEvent{Action: "in_progress", JobID: 8040, RunID: remote.RunID, Name: "matrix-a", Workflow: "CI", Labels: pool.Labels, RunnerName: r.Name})
	h.deliverJob(jobEvent{Action: "queued", JobID: 8041, RunID: remote.RunID, Name: "matrix-b", Workflow: "CI", Labels: pool.Labels})

	h.deliverJob(jobEvent{Action: "completed", JobID: 8040, RunID: remote.RunID, Name: "matrix-a", Workflow: "CI", Labels: pool.Labels, RunnerName: r.Name, Conclusion: "cancelled"})

	sibling, err := h.st.GetJobByGitHubID(h.ctx, 8041)
	if err != nil {
		t.Fatal(err)
	}
	if sibling.State != store.JobQueued || sibling.Provisioning != "" {
		t.Fatalf("one cancelled stage changed its sibling: %+v", sibling)
	}
}

func TestCancelledWorkflowRunCancelsQueuedAndExecutingSiblings(t *testing.T) {
	h := newHarness(t)
	_, pool, host := h.fleet()
	remote := h.gh.AddQueuedJob("acme/widgets", "CI", "remote", pool.Labels)
	h.gh.CompleteJob(remote.ID, "cancelled") // authoritative whole-run result

	r1 := h.runnerRow(pool, host, store.RunnerIdle)
	r2 := h.runnerRow(pool, host, store.RunnerIdle)
	for _, j := range []struct {
		id     int64
		name   string
		runner *store.Runner
	}{{8050, "matrix-a", r1}, {8051, "matrix-b", r2}} {
		h.deliverJob(jobEvent{Action: "queued", JobID: j.id, RunID: remote.RunID, Name: j.name, Workflow: "CI", Labels: pool.Labels})
		h.deliverJob(jobEvent{Action: "in_progress", JobID: j.id, RunID: remote.RunID, Name: j.name, Workflow: "CI", Labels: pool.Labels, RunnerName: j.runner.Name})
	}
	h.deliverJob(jobEvent{Action: "queued", JobID: 8052, RunID: remote.RunID, Name: "deploy", Workflow: "CI", Labels: pool.Labels})

	h.deliverJob(jobEvent{Action: "completed", JobID: 8050, RunID: remote.RunID, Name: "matrix-a", Workflow: "CI", Labels: pool.Labels, RunnerName: r1.Name, Conclusion: "cancelled"})

	for _, id := range []int64{8050, 8051, 8052} {
		j, err := h.st.GetJobByGitHubID(h.ctx, id)
		if err != nil {
			t.Fatal(err)
		}
		if j.State != store.JobCompleted || j.Conclusion != "cancelled" {
			t.Fatalf("job %d after run cancellation = %s/%q", id, j.State, j.Conclusion)
		}
	}
	if got := h.runnerByID(t, r2.ID); got.State != store.RunnerRemoved {
		t.Fatalf("runner executing a cancelled run was left %s", got.State)
	}
	if !h.hasTaskOfKind(host.ID, agent.TaskRemoveRunner) {
		t.Fatal("cancelled run did not immediately queue runner removal")
	}
}

func TestZoomiesRunCancellationStopsWorkBeforeGitHubCompletionEvents(t *testing.T) {
	h := newHarness(t)
	_, pool, host := h.fleet()
	pool.Ephemeral = false
	if err := h.st.UpdatePool(h.ctx, pool); err != nil {
		t.Fatal(err)
	}
	r := h.runnerRow(pool, host, store.RunnerIdle)
	const runID = 8060
	h.deliverJob(jobEvent{Action: "queued", JobID: 8061, RunID: runID, Name: "build", Workflow: "CI", Labels: pool.Labels})
	h.deliverJob(jobEvent{Action: "in_progress", JobID: 8061, RunID: runID, Name: "build", Workflow: "CI", Labels: pool.Labels, RunnerName: r.Name})
	h.deliverJob(jobEvent{Action: "queued", JobID: 8062, RunID: runID, Name: "deploy", Workflow: "CI", Labels: pool.Labels})

	job, err := h.st.GetJobByGitHubID(h.ctx, 8061)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := h.c.CancelJobWorkflow(h.ctx, job.ID, false); err != nil {
		t.Fatal(err)
	}

	// GitHub has accepted the run-scoped request but has not sent completion
	// events. Job states therefore remain authoritative, while local work is
	// already stopped and queued demand cannot create replacement capacity.
	stillRunning, _ := h.st.GetJobByGitHubID(h.ctx, 8061)
	queued, _ := h.st.GetJobByGitHubID(h.ctx, 8062)
	if stillRunning.State != store.JobInProgress {
		t.Fatalf("local cancellation invented terminal state %q before GitHub", stillRunning.State)
	}
	if queued.State != store.JobQueued || queued.Provisioning != "paused" {
		t.Fatalf("queued sibling was not suppressed while awaiting GitHub: %+v", queued)
	}
	if !h.hasTaskOfKind(host.ID, agent.TaskRemoveRunner) {
		t.Fatal("accepted run cancellation did not immediately queue removal of its executing runner")
	}
	h.c.lifecycleCalls.Wait()
	if got := h.runnerByID(t, r.ID); got.State != store.RunnerRemoved {
		t.Fatalf("executing runner after accepted run cancellation = %s", got.State)
	}
}
