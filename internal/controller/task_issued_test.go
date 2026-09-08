package controller

import (
	"testing"
	"time"

	"github.com/eyupio/zoomies/internal/agent"
	"github.com/eyupio/zoomies/internal/store"
)

// The task queue is in memory by design: every task is derived from state the
// database already holds, so persisting the queue would add a second source of
// truth that could disagree with the runners table.
//
// What a restart must not lose is the time. Without a stamp on the row, a
// provisioning runner whose create was issued twenty minutes ago and one whose
// create went out a second ago are the same row, and the operator reading the
// page -- or a controller that has just come back -- has no way to tell how
// long this runner has actually been waiting on its host.
func TestIssuingARunnersTaskIsRecordedOnItsRow(t *testing.T) {
	h := newHarness(t)
	_, _, host := h.fleet()
	h.deliverJob(jobEvent{Action: "queued", JobID: 9101, Labels: []string{"self-hosted", "linux", "x64", "demo"}})
	if err := h.c.Reconcile(h.ctx); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}

	r := h.onlyRunner()
	if r.TaskIssuedAt == nil {
		t.Fatal("task_issued_at is unset after the create was queued; the row cannot say when its host was asked")
	}
	first := *r.TaskIssuedAt

	batch, err := h.c.PollTasks(h.ctx, host.ID, time.Second)
	if err != nil || len(batch.Tasks) != 1 {
		t.Fatalf("PollTasks: %v, %d tasks", err, len(batch.Tasks))
	}
	after := h.onlyRunner()
	if after.TaskIssuedAt == nil || after.TaskIssuedAt.Before(first) {
		t.Fatalf("task_issued_at = %v after the poll, want it at or after the enqueue stamp %v", after.TaskIssuedAt, first)
	}
}

// The redelivery half. A create whose lease expires is offered again, and the
// second offer is the one that matters: counting the provision timeout from
// the first would fail a runner whose host only just received the work.
func TestRedeliveringATaskMovesTheRunnersIssueStampForward(t *testing.T) {
	h := newHarness(t)
	_, pool, host := h.fleet()
	r := h.runnerRow(pool, host, store.RunnerProvisioning)

	task := agent.Task{Kind: agent.TaskCreateRunner, RunnerID: r.ID, Backend: pool.Backend}
	if !h.c.enqueueLifecycle(h.ctx, host.ID, task) {
		t.Fatal("the task was not queued")
	}
	if _, err := h.c.PollTasks(h.ctx, host.ID, time.Second); err != nil {
		t.Fatalf("PollTasks: %v", err)
	}
	issued := h.runnerByID(t, r.ID).TaskIssuedAt
	if issued == nil {
		t.Fatal("task_issued_at is unset after the first delivery")
	}

	// Wind the clock past the create lease and sweep, which is what puts the
	// task back on the queue for a second attempt.
	h.advance(createLease + time.Minute)
	if requeued, _ := h.c.queues.get(host.ID).sweep(h.c.Now()); requeued != 1 {
		t.Fatalf("sweep re-queued %d tasks, want 1", requeued)
	}
	if _, err := h.c.PollTasks(h.ctx, host.ID, time.Second); err != nil {
		t.Fatalf("PollTasks: %v", err)
	}

	again := h.runnerByID(t, r.ID).TaskIssuedAt
	if again == nil {
		t.Fatal("task_issued_at is unset after the redelivery")
	}
	if !again.After(*issued) {
		t.Fatalf("task_issued_at = %v after redelivery, want it later than the first issue %v; "+
			"the provision timeout would be counted from an attempt the host never received", again, issued)
	}
}

// A log relay says nothing about whether a runner is progressing -- the
// container is where it was, doing what it was doing -- so it must not move a
// clock that decides whether the runner is written off.
func TestALogTaskDoesNotTouchTheRunnersIssueStamp(t *testing.T) {
	h := newHarness(t)
	_, pool, host := h.fleet()
	r := h.runnerRow(pool, host, store.RunnerIdle)

	h.c.enqueueLifecycle(h.ctx, host.ID, agent.Task{
		Kind: agent.TaskStreamLogs, RunnerID: r.ID, StreamID: "stream_x",
	})
	if got := h.runnerByID(t, r.ID).TaskIssuedAt; got != nil {
		t.Fatalf("task_issued_at = %v after a log task, want it untouched", got)
	}
}
