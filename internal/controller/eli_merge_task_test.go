package controller

import (
	"testing"

	"github.com/eyupio/zoomies/internal/agent"
)

// A merge task names the attempt and the image and nothing else, which is what
// keeps a GitHub credential on the controller and megabytes out of the queue.
func TestAMergeTaskCarriesTheAttemptAndTheImage(t *testing.T) {
	h := newHarness(t)
	host := h.host("m1")

	if !h.c.QueueMergeConflict(host.ID, "attempt-1", "ghcr.io/eyupio/zoomies-git:v1") {
		t.Fatal("the merge task was not queued")
	}
	task := h.taskOfKind(host.ID, agent.TaskMergeConflict)
	if task.MergeID != "attempt-1" || task.MergeImage != "ghcr.io/eyupio/zoomies-git:v1" {
		t.Fatalf("task %+v", task)
	}
	if task.RunnerID != "" {
		t.Errorf("a merge task names runner %q, but a merge is not a runner's work", task.RunnerID)
	}
}

// One task per attempt: a repair that gave up leaves its task in flight, and the
// next conflict on the same host must not wait behind it.
func TestAMergeTaskIsQueuedOncePerAttempt(t *testing.T) {
	h := newHarness(t)
	host := h.host("m2")

	if !h.c.QueueMergeConflict(host.ID, "attempt-1", "image") {
		t.Fatal("the first task was refused")
	}
	if h.c.QueueMergeConflict(host.ID, "attempt-1", "image") {
		t.Fatal("the same attempt was queued twice")
	}
	if !h.c.QueueMergeConflict(host.ID, "attempt-2", "image") {
		t.Fatal("a second attempt was refused while the first was in flight")
	}
}

// A merge task has a lease, so a host that dies with one in flight is asked again
// rather than leaving the repair waiting for ever.
func TestAMergeTaskIsOfferedAgainWhenItsLeaseLapses(t *testing.T) {
	if got := requeueAfter(agent.TaskMergeConflict); got != mergeLease {
		t.Fatalf("a merge task's lease is %s, want %s", got, mergeLease)
	}
	if mergeLease <= 0 {
		t.Fatal("a merge task has no lease, so an agent that died holding one would hold it for ever")
	}
	if got := maxAttempts(agent.TaskMergeConflict); got != maxTaskAttempts {
		t.Fatalf("a merge task allows %d attempts, want the shared %d", got, maxTaskAttempts)
	}
	if lifecycleTask(agent.TaskMergeConflict) {
		t.Fatal("a merge task is counted as lifecycle, so an unknown kind would mark a runner failed")
	}
	if cleansUp(agent.TaskMergeConflict) {
		t.Fatal("a merge task is counted as cleaning a runner up")
	}
}
