package api

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/eyupio/zoomies/internal/config"
	"github.com/eyupio/zoomies/internal/github"
	"github.com/eyupio/zoomies/internal/kennel/offline"
	"github.com/eyupio/zoomies/internal/store"
)

const (
	reviewAPIBase = "on: push\njobs:\n  build:\n    runs-on: ubuntu-latest\n    timeout-minutes: 5\n    steps:\n      - run: make\n"
	reviewAPIHead = "on: push\njobs:\n  build:\n    runs-on: ubuntu-latest\n    steps:\n      - run: make\n"
)

func kennelReviewAPIHarness(t *testing.T) (*harness, string, *checkoutAPIReader) {
	t.Helper()
	h, _, f, body := kennelFixAPIHarness(t)
	h.ctrl.UpdateConfig(func(c *config.Config) { c.Kennel.WorkflowChecks = true })
	path := ".github/workflows/ci.yml"
	f.kennelReviewSource = &github.KennelReviewSource{
		Version: "kennel-review-source-v1", Repository: "acme/repo", Number: 9, Open: true, BaseBranch: "main",
		BaseSHA: strings.Repeat("a", 40), HeadSHA: strings.Repeat("b", 40),
		Changed: []string{"AGENTS.md", path},
		Base:    []offline.ReviewFile{{Path: path, Data: []byte(reviewAPIBase)}},
		Head:    []offline.ReviewFile{{Path: path, Data: []byte(reviewAPIHead)}},
	}
	return h, body["kennel_repository_id"].(string), f
}

func TestAPullRequestReviewReportsOnlyWhatTheChangeIntroducedAndNamesWhatItDidNotCover(t *testing.T) {
	h, id, f := kennelReviewAPIHarness(t)
	token := h.token("operator", store.RoleOperator, "kennel:read", "optimisations:read")
	resp := h.do(request{method: http.MethodGet, path: "/api/v1/kennel/repositories/" + id + "/pull-reviews/9", token: token})
	resp.mustStatus(t, 200, "review")
	var got KennelReviewView
	if err := json.Unmarshal(resp.body, &got); err != nil {
		t.Fatal(err)
	}
	if got.PullNumber != 9 || got.HeadSHA != strings.Repeat("b", 40) || got.BaseSHA != strings.Repeat("a", 40) || !got.Complete ||
		len(got.Introduced) != 1 || got.Introduced[0].Code != "ci.no_timeout" || got.Introduced[0].Path != ".github/workflows/ci.yml" {
		t.Fatalf("review %s", resp.body)
	}
	if len(got.RuleChanges) != 1 || got.RuleChanges[0] != "AGENTS.md" {
		t.Errorf("rule changes = %v, want the instruction file named and not applied", got.RuleChanges)
	}
	if resp.header.Get("Cache-Control") != "no-store" || strings.Contains(string(resp.body), "runs-on") {
		t.Errorf("cache %q or the file's text reached the response: %s", resp.header.Get("Cache-Control"), resp.body)
	}
	assertShape(t, loadSpec(t), "KennelReview", resp.body)
	if f.kennelReviewReads.Load() != 1 {
		t.Errorf("read GitHub %d times", f.kennelReviewReads.Load())
	}
}

func TestAPullRequestReviewIsRefusedWithTheSettingToChangeAndForAnUnknownRepository(t *testing.T) {
	h, id, f := kennelReviewAPIHarness(t)
	token := h.token("operator", store.RoleOperator, "kennel:read", "optimisations:read")
	h.ctrl.UpdateConfig(func(c *config.Config) { c.Optimisation.SourceReads = false })
	resp := h.do(request{method: http.MethodGet, path: "/api/v1/kennel/repositories/" + id + "/pull-reviews/9", token: token})
	resp.mustStatus(t, http.StatusForbidden, "source reads off")
	if !strings.Contains(string(resp.body), "optimisation.source_reads") {
		t.Errorf("the refusal does not name the setting: %s", resp.body)
	}
	h.ctrl.UpdateConfig(func(c *config.Config) { c.Optimisation.SourceReads = true })
	h.do(request{method: http.MethodGet, path: "/api/v1/kennel/repositories/" + id + "/pull-reviews/0", token: token}).mustStatus(t, http.StatusBadRequest, "pull 0")
	h.do(request{method: http.MethodGet, path: "/api/v1/kennel/repositories/" + id + "/pull-reviews/abc", token: token}).mustStatus(t, http.StatusBadRequest, "pull abc")
	h.do(request{method: http.MethodGet, path: "/api/v1/kennel/repositories/kcr_missing/pull-reviews/9", token: token}).mustStatus(t, http.StatusNotFound, "unknown repository")
	if f.kennelReviewReads.Load() != 0 {
		t.Errorf("GitHub was read %d times for refused requests", f.kennelReviewReads.Load())
	}
}
