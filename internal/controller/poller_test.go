package controller

import (
	"testing"
	"time"

	"github.com/eyupio/zoomies/internal/github"
	"github.com/eyupio/zoomies/internal/store"
)

// The poller is what stops a misconfigured webhook from silently ending the
// fleet's scaling. It feeds the same rows the webhook path writes.
func TestPollerDiscoversQueuedJobs(t *testing.T) {
	h := newHarness(t)
	_, pool, _ := h.fleet()
	queued := h.gh.AddQueuedJob("acme/widgets", "CI", "build", []string{"self-hosted", "linux", "x64", "demo"})

	h.c.pollOnce(h.ctx)

	job, err := h.st.GetJobByGitHubID(h.ctx, queued.ID)
	if err != nil {
		t.Fatalf("the poller did not record the queued job: %v", err)
	}
	if job.State != store.JobQueued || job.PoolID != pool.ID || !job.Matched {
		t.Fatalf("job = %+v, want it queued and matched to %s", job, pool.ID)
	}
	if !h.c.PollingOnly() {
		t.Fatal("no webhook has ever arrived, so the controller is polling-only and should say so")
	}

	// And the pass that follows creates a runner for it, exactly as the
	// webhook path would.
	if err := h.c.Reconcile(h.ctx); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	if got := len(h.runners()); got != 1 {
		t.Fatalf("created %d runners for the polled job, want 1", got)
	}
}

// When webhooks are working the poller must cost nothing at all -- one local
// query and no GitHub calls -- which is what makes leaving it on by default
// defensible.
func TestPollerStandsDownWhenWebhooksAreRecent(t *testing.T) {
	h := newHarness(t)
	h.fleet()
	h.gh.AddQueuedJob("acme/widgets", "CI", "build", []string{"self-hosted", "linux", "x64", "demo"})

	// The repository is what credits the delivery to an installation, and a
	// real workflow_job delivery always carries one.
	if err := h.st.RecordDelivery(h.ctx, &store.WebhookDelivery{
		DeliveryID: "recent", Event: "workflow_job", Repo: "acme/widgets",
		Status: "accepted", ReceivedAt: time.Now(),
	}); err != nil {
		t.Fatalf("RecordDelivery: %v", err)
	}
	before := len(h.gh.Requests())

	h.c.pollOnce(h.ctx)

	if after := len(h.gh.Requests()); after != before {
		t.Fatalf("the poller made %d GitHub calls despite a webhook arriving seconds ago", after-before)
	}
	if h.c.PollingOnly() {
		t.Fatal("a delivery has arrived, so the controller is not polling-only")
	}
}

// A rejected delivery -- a mistyped webhook secret, say -- records a job for
// nobody. The poller must not take it as proof that webhooks work, or a fleet
// with the wrong secret never starts a runner and never says why.
func TestPollerKeepsGoingWhenDeliveriesAreRejected(t *testing.T) {
	h := newHarness(t)
	h.fleet()
	h.gh.AddQueuedJob("acme/widgets", "CI", "build", []string{"self-hosted", "linux", "x64", "demo"})

	if err := h.st.RecordDelivery(h.ctx, &store.WebhookDelivery{
		DeliveryID: "bad-secret", Event: "workflow_job", Status: "rejected", ReceivedAt: time.Now(),
	}); err != nil {
		t.Fatalf("RecordDelivery: %v", err)
	}
	before := len(h.gh.Requests())

	h.c.pollOnce(h.ctx)

	if after := len(h.gh.Requests()); after == before {
		t.Fatal("the poller stood down on a delivery whose signature did not verify")
	}
	if !h.c.PollingOnly() {
		t.Fatal("no delivery has verified, so the controller is still polling-only")
	}
}

// A rate-limited installation must make the poller stand down rather than
// spend the quota the webhook path's own calls need.
func TestPollerBacksOffWhenRateLimited(t *testing.T) {
	h := newHarness(t)
	inst, _, _ := h.fleet()
	reset := time.Now().Add(time.Hour)
	h.gh.SetRateLimit(5000, 0, reset)
	h.gh.SetError("/installation/repositories", 403, "API rate limit exceeded")

	h.c.pollOnce(h.ctx)

	if !h.c.githubHeld(inst.ID, time.Now()) {
		t.Fatal("the poller did not back off after GitHub reported a rate limit")
	}

	before := len(h.gh.Requests())
	h.c.pollOnce(h.ctx)
	if after := len(h.gh.Requests()); after != before {
		t.Fatalf("the poller made %d more calls while backed off", after-before)
	}
}

// A flat fifteen minutes is either most of a window wasted or most of a window
// spent rediscovering the same refusal. GitHub says when the quota returns, so
// the fixed wait is only what to do when it did not.
func TestAStandDownLastsAsLongAsGitHubAsked(t *testing.T) {
	now := time.Date(2025, 3, 4, 12, 0, 0, 0, time.UTC)
	reset := now.Add(2 * time.Minute)

	got := rateLimitHold(&github.RateLimitedError{ResetAt: reset}, now)

	// A little past the reset: resuming on the exact second races GitHub's own
	// accounting and buys another refusal.
	if !got.After(reset) {
		t.Fatalf("hold = %v; it must clear the reset at %v", got, reset)
	}
	if got.Sub(reset) > time.Minute {
		t.Fatalf("hold = %v, which is far past the reset at %v", got, reset)
	}
	if got.Sub(now) >= rateLimitBackoff {
		t.Fatalf("hold of %s is no better than the flat %s it replaces", got.Sub(now), rateLimitBackoff)
	}
}

// GitHub does not always say -- an older enterprise server, a proxy that drops
// the headers -- and the fixed wait is what that case still gets.
func TestAStandDownFallsBackToTheFixedWaitWhenGitHubSaysNothing(t *testing.T) {
	now := time.Date(2025, 3, 4, 12, 0, 0, 0, time.UTC)

	got := rateLimitHold(&github.RateLimitedError{}, now)

	if !got.Equal(now.Add(rateLimitBackoff)) {
		t.Fatalf("hold = %v, want the fixed %s", got, rateLimitBackoff)
	}
}

// A reset days away is a clock out of step or a proxy inventing a header.
// Believing it would take an installation out of service until somebody
// noticed, which is a worse failure than one more refused call.
func TestAnAbsurdResetDoesNotParkAnInstallationForEver(t *testing.T) {
	now := time.Date(2025, 3, 4, 12, 0, 0, 0, time.UTC)

	got := rateLimitHold(&github.RateLimitedError{ResetAt: now.Add(72 * time.Hour)}, now)

	if got.Sub(now) > maxRateLimitBackoff {
		t.Fatalf("hold of %s exceeds the %s cap", got.Sub(now), maxRateLimitBackoff)
	}
}
