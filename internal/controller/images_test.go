package controller

import (
	"testing"

	"github.com/eyupio/zoomies/internal/agent"
	"github.com/eyupio/zoomies/internal/store"
)

// The whole point of the refresh: a pool is created once and then nothing
// touches it again, so without this pass the image its hosts pulled on the
// first job is the image they keep, however many times the tag has moved.
func TestRefreshPoolImagesPrewarmsEveryPoolAgain(t *testing.T) {
	h := newHarness(t)
	_, pool, host := h.fleet()

	h.c.refreshPoolImages(h.ctx)

	task := h.taskOfKind(host.ID, agent.TaskPrewarmImage)
	if task.PoolID != pool.ID {
		t.Fatalf("prewarm task pool = %q, want %q", task.PoolID, pool.ID)
	}
	if task.Image != pool.Image {
		t.Fatalf("prewarm image = %q, want %q", task.Image, pool.Image)
	}
}

// A process-backend pool has no image to pull -- its runners fetch the
// actions/runner archive themselves -- so PrewarmPool refuses it. The refusal
// is expected and must not be logged as a failure or stop the pools after it
// from being refreshed.
func TestRefreshPoolImagesSkipsProcessPoolsAndKeepsGoing(t *testing.T) {
	h := newHarness(t)
	inst := h.installation()

	proc := h.pool(inst, "bare-metal")
	proc.Backend = store.BackendProcess
	if err := h.st.UpdatePool(h.ctx, proc); err != nil {
		t.Fatalf("UpdatePool: %v", err)
	}
	container := h.pool(inst, "linux-x64")
	host := h.host("vm-1")

	h.c.refreshPoolImages(h.ctx)

	var seen []string
	for _, task := range h.tasksFor(host.ID) {
		if task.Kind == agent.TaskPrewarmImage {
			seen = append(seen, task.PoolID)
		}
	}
	if len(seen) != 1 || seen[0] != container.ID {
		t.Fatalf("prewarmed pools = %v, want only the container pool %q", seen, container.ID)
	}
}

// Refreshing is not a scheduling decision and must never look like one: it
// queues its own idempotent task kind and creates no runners.
func TestRefreshPoolImagesCreatesNoRunners(t *testing.T) {
	h := newHarness(t)
	h.fleet()

	h.c.refreshPoolImages(h.ctx)

	if got := h.runners(); len(got) != 0 {
		t.Fatalf("runners = %d, want 0: the refresh must not scale the fleet", len(got))
	}
}
