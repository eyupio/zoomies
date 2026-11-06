package controller

import (
	"strings"
	"testing"

	"github.com/eyupio/zoomies/internal/agent"
)

// A merge task names the attempt, the image and the three pinned commit ids,
// which is what keeps a GitHub credential on the controller and megabytes out of
// the queue while still giving the worker the request it must run.
func TestAMergeTaskCarriesTheAttemptTheImageAndThePins(t *testing.T) {
	h := newHarness(t)
	host := h.host("m1")
	prepared := MergePrepared{
		ID: "attempt-1", Image: "ghcr.io/eyupio/zoomies-git:v1",
		Ancestor: strings.Repeat("a", 40), Base: strings.Repeat("b", 40), Head: strings.Repeat("c", 40),
	}

	if !h.c.QueueMergeConflict(host.ID, prepared) {
		t.Fatal("the merge task was not queued")
	}
	task := h.taskOfKind(host.ID, agent.TaskMergeConflict)
	if task.MergeID != prepared.ID || task.MergeImage != prepared.Image {
		t.Fatalf("task %+v", task)
	}
	if task.MergeAncestor != prepared.Ancestor || task.MergeBase != prepared.Base || task.MergeHead != prepared.Head {
		t.Fatalf("task pins = %q/%q/%q, want %q/%q/%q", task.MergeAncestor, task.MergeBase, task.MergeHead, prepared.Ancestor, prepared.Base, prepared.Head)
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

	one := MergePrepared{ID: "attempt-1", Image: "image", Ancestor: "a", Base: "b", Head: "c"}
	if !h.c.QueueMergeConflict(host.ID, one) {
		t.Fatal("the first task was refused")
	}
	if h.c.QueueMergeConflict(host.ID, one) {
		t.Fatal("the same attempt was queued twice")
	}
	two := one
	two.ID = "attempt-2"
	if !h.c.QueueMergeConflict(host.ID, two) {
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

// A merge task's result is not allowed to fail a runner: it names none, and a
// merge that went wrong is the answer endpoint's business.
func TestAMergeTaskResultChangesNoRunnerState(t *testing.T) {
	h := newHarness(t)
	host := h.host("m3")
	prepared := MergePrepared{ID: "attempt-merge", Image: "image", Ancestor: "a", Base: "b", Head: "c"}
	if !h.c.QueueMergeConflict(host.ID, prepared) {
		t.Fatal("the merge task was not queued")
	}
	task := h.taskOfKind(host.ID, agent.TaskMergeConflict)
	if err := h.c.ReportResult(h.ctx, host.ID, agent.TaskResult{
		TaskID: task.ID, Kind: agent.TaskMergeConflict, OK: false, Error: "the worker did not answer",
	}); err != nil {
		t.Fatalf("ReportResult: %v", err)
	}
}
