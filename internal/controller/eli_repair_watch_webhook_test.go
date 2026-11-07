package controller

import (
	"encoding/json"
	"testing"

	"github.com/eyupio/zoomies/internal/github"
	"github.com/eyupio/zoomies/internal/store"
)

func pullRequestWebhook(t *testing.T, h *harness, repo string, number int, action string) {
	t.Helper()
	body, err := json.Marshal(map[string]any{
		"action": action, "number": number,
		"repository": map[string]any{"full_name": repo},
	})
	if err != nil {
		t.Fatal(err)
	}
	if rec := h.deliver("pull_request", body, testWebhookSecret); rec.Code != 202 {
		t.Fatalf("pull_request delivery: %d %s", rec.Code, rec.Body.String())
	}
}

func TestAPullRequestWebhookAdmitsANewWatchWhereTheRepositoryWasChosen(t *testing.T) {
	h := newHarness(t)
	fake, inst, repo := repositoryWatchFixture(t, h)
	fake.pull = github.RepairPull{Number: 2, Branch: "fix-2", Base: "main", BaseSHA: "base-2", HeadSHA: "head-2", Open: true}
	if _, err := h.st.SetEliRepairWatchRepository(h.ctx, store.EliRepairWatchRepository{InstallationID: inst.ID, Repo: repo, OwnerID: "usr_owner", Checks: true}); err != nil {
		t.Fatal(err)
	}
	pullRequestWebhook(t, h, repo, 2, "opened")
	got, err := h.st.EliRepairWatchForPull(h.ctx, repo, 2)
	if err != nil || !got.Enabled || got.HeadSHA != "head-2" || got.OwnerID != "usr_owner" {
		t.Fatalf("webhook watch %+v, %v", got, err)
	}
}

func TestAPullRequestWebhookWillNotInventARepositoryDecision(t *testing.T) {
	h := newHarness(t)
	_, _, repo := repositoryWatchFixture(t, h)
	pullRequestWebhook(t, h, repo, 2, "opened")
	if _, err := h.st.EliRepairWatchForPull(h.ctx, repo, 2); err != store.ErrNotFound {
		t.Fatalf("a single-PR webhook created a repository watch: %v", err)
	}
}

func TestAPullRequestWebhookLeavesAPausedWatchPaused(t *testing.T) {
	h := newHarness(t)
	fake, inst, repo := repositoryWatchFixture(t, h)
	fake.pull = github.RepairPull{Number: 2, Branch: "fix-2", Base: "main", BaseSHA: "base-2", HeadSHA: "head-2", Open: true}
	if _, err := h.st.SetEliRepairWatchRepository(h.ctx, store.EliRepairWatchRepository{InstallationID: inst.ID, Repo: repo, OwnerID: "usr_owner", Checks: true}); err != nil {
		t.Fatal(err)
	}
	watch, err := h.st.AdmitEliRepairWatch(h.ctx, store.EliRepairWatch{InstallationID: inst.ID, Repo: repo, PullNumber: 2, OwnerID: "usr_owner", Checks: true})
	if err != nil {
		t.Fatal(err)
	}
	if err := h.st.PauseEliRepairWatch(h.ctx, watch.ID, "Paused for this pull request."); err != nil {
		t.Fatal(err)
	}
	pullRequestWebhook(t, h, repo, 2, "reopened")
	got, err := h.st.EliRepairWatch(h.ctx, watch.ID)
	if err != nil || got.Enabled {
		t.Fatalf("a webhook resumed a paused watch: %+v, %v", got, err)
	}
}

// Pausing the repository decision is the operator saying "no more", and the
// periodic discovery already honours it; the webhook reads the same row.
func TestAPullRequestWebhookAdmitsNothingWhileTheRepositoryIsPaused(t *testing.T) {
	h := newHarness(t)
	fake, inst, repo := repositoryWatchFixture(t, h)
	fake.pull = github.RepairPull{Number: 2, Branch: "fix-2", Base: "main", BaseSHA: "base-2", HeadSHA: "head-2", Open: true}
	decision, err := h.st.SetEliRepairWatchRepository(h.ctx, store.EliRepairWatchRepository{InstallationID: inst.ID, Repo: repo, OwnerID: "usr_owner", Checks: true})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := h.c.PauseEliRepositoryWatches(h.ctx, decision.ID); err != nil {
		t.Fatal(err)
	}
	pullRequestWebhook(t, h, repo, 2, "opened")
	if got, err := h.st.EliRepairWatchForPull(h.ctx, repo, 2); err != store.ErrNotFound {
		t.Fatalf("a paused repository admitted a new pull request: %+v, %v", got, err)
	}
}
