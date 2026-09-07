package controller

import (
	"testing"
	"time"

	"github.com/eyupio/zoomies/internal/agent"
	"github.com/eyupio/zoomies/internal/backend"
	"github.com/eyupio/zoomies/internal/store"
)

// lostRunnerOnAJob builds the situation the late-report path exists for: a
// runner executing a job, whose host went quiet long enough that the
// controller gave up and failed it as lost.
//
// It goes through failRunnerID rather than writing the row, because the point
// of the fixture is the state the reclaim actually leaves behind -- including
// the cleared current_job_id, which is why the job has to be found from the
// other end.
func lostRunnerOnAJob(t *testing.T, h *harness) (*store.Runner, *store.Job) {
	t.Helper()
	_, pool, host := h.fleet()
	r := h.runnerRow(pool, host, store.RunnerIdle)

	h.deliverJob(jobEvent{Action: "queued", JobID: 8801, Name: "build", Workflow: "CI",
		Labels: []string{"self-hosted", "linux", "x64", "demo"}})
	h.deliverJob(jobEvent{Action: "in_progress", JobID: 8801, Name: "build", Workflow: "CI",
		Labels: []string{"self-hosted", "linux", "x64", "demo"}, RunnerName: r.Name})

	job, err := h.st.GetJobByGitHubID(h.ctx, 8801)
	if err != nil {
		t.Fatalf("GetJobByGitHubID: %v", err)
	}
	if job.RunnerID != r.ID {
		t.Fatalf("job.RunnerID = %q, want the runner %q; the fixture needs the job linked", job.RunnerID, r.ID)
	}
	if err := h.st.AssignRunnerJob(h.ctx, r.ID, job.ID); err != nil {
		t.Fatalf("AssignRunnerJob: %v", err)
	}
	if err := h.c.failRunnerID(h.ctx, r.ID, "host went quiet"); err != nil {
		t.Fatalf("failRunnerID: %v", err)
	}
	lost, err := h.st.GetRunner(h.ctx, r.ID)
	if err != nil {
		t.Fatalf("GetRunner: %v", err)
	}
	if lost.State != store.RunnerFailed {
		t.Fatalf("state = %q, want failed", lost.State)
	}
	_ = host
	return lost, job
}

func liveReport(r *store.Runner) []agent.RunnerReport {
	return []agent.RunnerReport{{RunnerID: r.ID, Phase: backend.PhaseRunning, ObservedAt: time.Now()}}
}

// "Lost" is a guess made from silence. A network partition, a long agent
// restart or a paused VM all end with the host back and the container exactly
// where it was, still executing its job -- and before this the report was
// dropped as an illegal transition, so the row stayed failed, the agent kept
// the workload because the controller never disowned it, and the container ran
// for ever on a host nobody was accounting for.
//
// The row is not resurrected: the fleet has already told an operator, and this
// job's timeline, that the runner was gone. What it must do is stop lying in
// the message and leave the job alone to finish.
func TestARunnerThatComesBackStillWorkingKeepsItsJob(t *testing.T) {
	h := newHarness(t)
	r, job := lostRunnerOnAJob(t, h)

	if err := h.c.ReportRunners(h.ctx, r.HostID, liveReport(r)); err != nil {
		t.Fatalf("ReportRunners: %v", err)
	}

	after, err := h.st.GetRunner(h.ctx, r.ID)
	if err != nil {
		t.Fatalf("GetRunner: %v", err)
	}
	if after.State != store.RunnerFailed {
		t.Fatalf("state = %q, want it to stay failed: a terminal row is what the timeline already reported", after.State)
	}
	if after.Message == r.Message {
		t.Fatalf("message = %q, unchanged; an operator reading the row is still told it is gone", after.Message)
	}
	if h.hasTaskOfKind(r.HostID, agent.TaskRemoveRunner) {
		t.Fatal("a remove task was queued while the job was still running; that fails a job for tidiness")
	}

	var returned int
	for _, e := range h.timeline(job.ID) {
		if e.Kind == store.JobEventRunnerReturned {
			returned++
		}
	}
	if returned != 1 {
		t.Fatalf("runner_returned entries = %d, want exactly 1", returned)
	}
}

// A host reports every runner on every heartbeat, so the timeline entry needs
// a guard: without one, a job that takes ten minutes to finish collects one
// identical line per heartbeat and the timeline becomes unreadable.
func TestARunnerThatKeepsReportingWritesOneReturnedEntry(t *testing.T) {
	h := newHarness(t)
	r, job := lostRunnerOnAJob(t, h)

	for range 5 {
		if err := h.c.ReportRunners(h.ctx, r.HostID, liveReport(r)); err != nil {
			t.Fatalf("ReportRunners: %v", err)
		}
	}

	var returned int
	for _, e := range h.timeline(job.ID) {
		if e.Kind == store.JobEventRunnerReturned {
			returned++
		}
	}
	if returned != 1 {
		t.Fatalf("runner_returned entries = %d after five heartbeats, want exactly 1", returned)
	}
}

// The other half. Once the job is over, the container is genuinely litter: the
// row is terminal, the slot has already been given to a replacement, and
// nothing else in the system will ever remove it -- the agent keeps it tracked
// precisely because the controller still has a row for it.
func TestARunnerThatComesBackWithNoJobLeftIsRemoved(t *testing.T) {
	h := newHarness(t)
	r, _ := lostRunnerOnAJob(t, h)

	h.deliverJob(jobEvent{Action: "completed", JobID: 8801, Name: "build", Workflow: "CI",
		Labels: []string{"self-hosted", "linux", "x64", "demo"}, RunnerName: r.Name, Conclusion: "success"})

	if err := h.c.ReportRunners(h.ctx, r.HostID, liveReport(r)); err != nil {
		t.Fatalf("ReportRunners: %v", err)
	}

	after, err := h.st.GetRunner(h.ctx, r.ID)
	if err != nil {
		t.Fatalf("GetRunner: %v", err)
	}
	if after.State != store.RunnerRemoved {
		t.Fatalf("state = %q, want removed once nothing is running on it", after.State)
	}
	if !h.hasTaskOfKind(r.HostID, agent.TaskRemoveRunner) {
		t.Fatal("no remove task was queued; the container would run on the host for ever")
	}
}

// A removed row is already the answer: the heartbeat names it as unknown, the
// agent releases it and the reconciler collects the workload. Reaching the
// late-report path for one would enqueue a second removal for a runner whose
// removal is already in flight.
func TestAReportForAnAlreadyRemovedRunnerQueuesNothing(t *testing.T) {
	h := newHarness(t)
	_, pool, host := h.fleet()
	r := h.runnerRow(pool, host, store.RunnerIdle)
	if _, err := h.st.TransitionRunner(h.ctx, r.ID, store.RunnerRemoved, "removed"); err != nil {
		t.Fatalf("TransitionRunner: %v", err)
	}

	if err := h.c.ReportRunners(h.ctx, host.ID, liveReport(r)); err != nil {
		t.Fatalf("ReportRunners: %v", err)
	}
	if h.hasTaskOfKind(host.ID, agent.TaskRemoveRunner) {
		t.Fatal("a remove task was queued for an already-removed runner")
	}
}
