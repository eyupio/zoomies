package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/eyupio/zoomies/internal/config"
	"github.com/eyupio/zoomies/internal/controller"
	"github.com/eyupio/zoomies/internal/github"
	"github.com/eyupio/zoomies/internal/store"
)

func seedCheckoutComparison(t *testing.T, h *harness, installation, previewID, revision string) *controller.RetainedExperimentComparison {
	t.Helper()
	at := time.Date(2026, 10, 13, 1, 0, 0, 0, time.UTC)
	end := at.Add(time.Minute)
	remote := func(run int64) *github.ExperimentAttemptEvidence {
		return &github.ExperimentAttemptEvidence{Repository: "acme/repo", RunID: run, Attempt: 1, HeadSHA: strings.Repeat("a", 40), WorkflowID: 20, Status: "completed", Conclusion: "failure", UpdatedAt: &end, RunStartedAt: &at, JobsCoverage: "complete", Jobs: []github.ExperimentAttemptJob{{ID: run + 100, Status: "completed", Conclusion: "failure", StartedAt: &at, CompletedAt: &end}}}
	}
	a := &controller.ExperimentJoinedEvidence{Repository: "acme/repo", InstallationID: installation, SelectionComplete: true, Attempts: []controller.ExperimentJoinedAttempt{{Remote: remote(10)}}}
	b := &controller.ExperimentJoinedEvidence{Repository: "acme/repo", InstallationID: installation, SelectionComplete: true, Attempts: []controller.ExperimentJoinedAttempt{{Remote: remote(11)}}}
	record := controller.ExperimentComparisonRecord{Kind: "workflow", Repository: "acme/repo", Hypothesis: "read one commit", IntendedDelta: "reduce checkout history", RevisionDeltaID: revision, Since: at.Add(-time.Hour), Until: at.Add(time.Hour)}
	saved, err := h.ctrl.RetainExperimentComparison(h.ctx, &store.AuditEvent{ActorID: "usr_seed", ActorKind: "user"}, record, a, b)
	if err != nil {
		t.Fatal(err)
	}
	return saved
}

func TestCheckoutVerificationLinksARetainedWorkflowComparison(t *testing.T) {
	h, instID, f, body := checkoutAPIHarness(t, true, func(c *config.Config) { c.Optimisation.Publish = true })
	token := h.token("operator", store.RoleOperator, "jobs:read", "optimisations:write", "optimisations:read", "optimisations:publish", "comparisons:write")
	inspect := h.do(request{method: http.MethodPost, path: "/api/v1/optimisations/checkout/inspections", token: token, body: body})
	inspect.mustStatus(t, 200, "inspect")
	var inspection CheckoutInspectionView
	if err := json.Unmarshal(inspect.body, &inspection); err != nil {
		t.Fatal(err)
	}
	created := h.do(request{method: http.MethodPost, path: "/api/v1/optimisations/checkout/previews", token: token, body: previewBody(inspection, body)})
	created.mustStatus(t, 201, "capture")
	var saved CheckoutPreviewView
	if err := json.Unmarshal(created.body, &saved); err != nil {
		t.Fatal(err)
	}
	approve := h.do(request{method: http.MethodPost, path: fmt.Sprintf("/api/v1/optimisations/checkout/previews/%s/approval", saved.ID), token: token,
		body: map[string]any{"installation_id": instID, "repository": "acme/repo", "head_sha": saved.Preview.HeadSHA, "source_sha256": saved.Preview.SourceSHA256}})
	approve.mustStatus(t, 200, "approve")
	f.publishPR = &github.PullRequest{Number: 21, HTMLURL: "https://github.com/acme/repo/pull/21", Branch: "zoomies/checkout-" + saved.ID}
	h.do(request{method: http.MethodPost, path: fmt.Sprintf("/api/v1/optimisations/checkout/previews/%s/publication", saved.ID), token: token,
		body: map[string]any{"installation_id": instID, "repository": "acme/repo", "head_sha": saved.Preview.HeadSHA, "source_sha256": saved.Preview.SourceSHA256}}).mustStatus(t, 200, "publish")
	comparison := seedCheckoutComparison(t, h, instID, saved.ID, saved.ID)
	path := fmt.Sprintf("/api/v1/optimisations/checkout/previews/%s/verification", saved.ID)
	linkBody := map[string]any{"installation_id": instID, "repository": "acme/repo", "comparison_id": comparison.ID}
	resp := h.do(request{method: http.MethodPost, path: path, token: token, body: linkBody})
	resp.mustStatus(t, 200, "link")
	var linked CheckoutPreviewView
	if err := json.Unmarshal(resp.body, &linked); err != nil {
		t.Fatal(err)
	}
	if linked.Verification == nil || linked.Verification.ComparisonID != comparison.ID || linked.Verification.LinkedBy == "" || linked.Publication == nil {
		t.Fatalf("verification %s", resp.body)
	}
	assertShape(t, loadSpec(t), "CheckoutPreview", resp.body)
	if auditCount(t, h, "optimisation.comparison_linked") != 1 {
		t.Fatal("linking was not audited exactly once")
	}
	again := h.do(request{method: http.MethodPost, path: path, token: token, body: linkBody})
	again.mustStatus(t, 200, "repeat")
	if auditCount(t, h, "optimisation.comparison_linked") != 1 {
		t.Fatal("a repeat wrote a second audit row")
	}
	// A comparison that does not name the preview is refused.
	unrelated := seedCheckoutComparison(t, h, instID, saved.ID, "other-preview")
	bad := h.do(request{method: http.MethodPost, path: path, token: token, body: map[string]any{"installation_id": instID, "repository": "acme/repo", "comparison_id": unrelated.ID}})
	bad.mustStatus(t, 409, "unrelated comparison")
	if !strings.Contains(string(bad.body), "does not name this preview") {
		t.Fatalf("unrelated refusal: %s", bad.body)
	}
	h.do(request{method: http.MethodPost, path: path, token: token, body: map[string]any{"installation_id": instID, "repository": "acme/repo", "comparison_id": "not-a-comparison"}}).mustStatus(t, 400, "bad comparison id")
	h.do(request{method: http.MethodPost, path: path, token: h.token("preview-only", store.RoleOperator, "jobs:read", "optimisations:read"), body: linkBody}).mustStatus(t, 403, "no write scope")
}
