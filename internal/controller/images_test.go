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

// The regression this pins: a host that pulled :dev once already has a :dev, so
// a refresh that applied the pool's if-not-present literally looked at the local
// tag, found it, and never asked the registry again. The pool was moved back
// onto the tag and its first runner started from a runner image two builds old.
func TestAPrewarmPullsAMovingTagWhateverTheCreatePolicyIs(t *testing.T) {
	digest := "ghcr.io/eyupio/zoomies-runner-docker@sha256:" + "a0cb27481432f5b803a78727e8cce7cdde26820c8b3e7b0defacbdcaff824b8c"
	tests := map[string]struct {
		policy store.PullPolicy
		image  string
		want   store.PullPolicy
	}{
		"a tag under the default policy":       {store.PullIfNotPresent, "ghcr.io/eyupio/zoomies-runner-docker:dev", store.PullAlways},
		"a tag with a registry port":           {store.PullIfNotPresent, "registry.example.com:5000/ci/runner:dev", store.PullAlways},
		"no tag at all, which is latest":       {store.PullIfNotPresent, "ghcr.io/eyupio/zoomies-runner-docker", store.PullAlways},
		"a digest cannot move":                 {store.PullIfNotPresent, digest, store.PullIfNotPresent},
		"a tag pinned by digest as well":       {store.PullIfNotPresent, "ghcr.io/eyupio/zoomies-runner-docker:dev@sha256:" + "a0cb27481432f5b803a78727e8cce7cdde26820c8b3e7b0defacbdcaff824b8c", store.PullIfNotPresent},
		"always already says so":               {store.PullAlways, "ghcr.io/eyupio/zoomies-runner-docker:dev", store.PullAlways},
		"pinned-only keeps refusing a tag":     {store.PullPinnedOnly, "ghcr.io/eyupio/zoomies-runner-docker:dev", store.PullPinnedOnly},
		"pinned-only keeps accepting a digest": {store.PullPinnedOnly, digest, store.PullPinnedOnly},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			if got := prewarmPullPolicy(tt.policy, tt.image); got != tt.want {
				t.Fatalf("prewarmPullPolicy(%q, %q) = %q, want %q", tt.policy, tt.image, got, tt.want)
			}
		})
	}
}

// The hourly refresh is the caller that matters, and the pool's own policy must
// stay what it was: it still decides what a runner's create does, which is the
// one place a registry round trip would cost a job its start.
func TestRefreshPoolImagesPullsATagButLeavesThePoolsPolicyAlone(t *testing.T) {
	h := newHarness(t)
	inst := h.installation()
	pool := h.pool(inst, "linux-x64")
	pool.Image = "ghcr.io/eyupio/zoomies-runner-docker:dev"
	pool.PullPolicy = store.PullIfNotPresent
	if err := h.st.UpdatePool(h.ctx, pool); err != nil {
		t.Fatalf("UpdatePool: %v", err)
	}
	host := h.host("vm-1")

	h.c.refreshPoolImages(h.ctx)

	task := h.taskOfKind(host.ID, agent.TaskPrewarmImage)
	if task.PullPolicy != store.PullAlways {
		t.Fatalf("prewarm pull policy = %q, want %q: a host that already has the tag must still ask the registry", task.PullPolicy, store.PullAlways)
	}
	stored, err := h.st.GetPool(h.ctx, pool.ID)
	if err != nil {
		t.Fatalf("GetPool: %v", err)
	}
	if stored.PullPolicy != store.PullIfNotPresent {
		t.Fatalf("pool pull policy = %q, want %q: a refresh must not rewrite what a create does", stored.PullPolicy, store.PullIfNotPresent)
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
