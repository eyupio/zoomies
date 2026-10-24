package api

import (
	"crypto/sha1"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/eyupio/zoomies/internal/config"
	"github.com/eyupio/zoomies/internal/github"
	"github.com/eyupio/zoomies/internal/prrepair"
	"github.com/eyupio/zoomies/internal/store"
)

func cacheAPISHA1(s string) string {
	sum := sha1.Sum([]byte(fmt.Sprintf("blob %d\x00%s", len(s), s)))
	return hex.EncodeToString(sum[:])
}

func cacheAPISHA256(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}

func cacheAPIHarness(t *testing.T) (*harness, string, *checkoutAPIReader, map[string]any) {
	t.Helper()
	h, instID, f, _ := checkoutAPIHarness(t, true)
	now := time.Now().UTC()
	workflow := "on: push\njobs:\n  build:\n    name: Build\n    runs-on: ubuntu-latest\n    steps:\n      - name: Checkout\n        uses: actions/checkout@" + strings.Repeat("a", 40) + "\n      - name: Node\n        uses: actions/setup-node@" + strings.Repeat("b", 40) + "\n        with:\n          node-version: 20\n      - name: Test\n        run: npm test\n"
	lockfile := `{"lockfileVersion":3}`
	f.cacheSource = &github.SetupCacheSource{
		Version: "setup-cache-evidence-v1", Basis: "push_head_root_lockfile_literal_names", Repository: "acme/repo",
		RunID: "10", Attempt: 2, HeadSHA: strings.Repeat("a", 40), HeadBranch: "main",
		WorkflowPath: ".github/workflows/ci.yml", Workflow: prrepair.File{Path: ".github/workflows/ci.yml", Mode: "100644", Content: workflow}, WorkflowBlobSHA: cacheAPISHA1(workflow), WorkflowSHA256: cacheAPISHA256(workflow),
		LockfilePath: "package-lock.json", Lockfile: prrepair.File{Path: "package-lock.json", Mode: "100644", Content: lockfile}, LockfileBlobSHA: cacheAPISHA1(lockfile), LockfileSHA256: cacheAPISHA256(lockfile),
		JobIndex: 0, StepIndex: 1, JobName: "Build", StepName: "Node",
	}
	steps := func(seconds int64) store.JobSteps {
		start, end := now.Add(-time.Duration(seconds+1)*time.Second), now.Add(-time.Second)
		return store.JobSteps{{Number: 1, Name: "Set up job", Status: "completed", Conclusion: "success", StartedAt: &start, CompletedAt: &start}, {Number: 2, Name: "Node", Status: "completed", Conclusion: "success", StartedAt: &start, CompletedAt: &end}}
	}
	for _, job := range []struct {
		id, run int64
		attempt int
		secs    int64
	}{{100, 10, 2, 4}, {101, 11, 1, 6}} {
		if _, err := h.st.UpsertJob(h.ctx, &store.Job{GitHubJobID: job.id, GitHubRunID: job.run, Repo: "acme/repo", Workflow: "CI", JobName: "Build", State: store.JobCompleted, Conclusion: "success", InstallationID: instID, HeadBranch: "main", HeadSHA: strings.Repeat("a", 40), RunAttempt: job.attempt, QueuedAt: now.Add(-time.Minute), CompletedAt: &now, Steps: steps(job.secs)}); err != nil {
			t.Fatal(err)
		}
	}
	return h, instID, f, map[string]any{"run_id": "10", "attempt": 2, "github_job_id": "100"}
}

func TestCacheOptimisationInspectorCaptureAndReplayKeepTheSourcePrivate(t *testing.T) {
	h, instID, f, selection := cacheAPIHarness(t)
	token := h.token("operator", store.RoleOperator, "jobs:read", "optimisations:write", "optimisations:read")
	body := map[string]any{"installation_id": instID, "repository": "acme/repo", "selection": selection}
	inspect := h.do(request{method: http.MethodPost, path: "/api/v1/optimisations/cache/inspections", token: token, body: body})
	inspect.mustStatus(t, 200, "inspect")
	var inspection CacheInspectionView
	if err := json.Unmarshal(inspect.body, &inspection); err != nil {
		t.Fatal(err)
	}
	if inspection.PackageManager != "npm" || inspection.JobName != "Build" || inspection.StepName != "Node" || inspection.Runs != 2 || inspection.SetupMS != 5000 || inspection.SourceSHA256 == "" || inspection.LockfileSHA256 == "" || inspection.HeadSHA == "" || !strings.Contains(inspection.After, "cache: npm") || strings.Contains(string(inspect.body), "on: push") || strings.Contains(string(inspect.body), "node-version: 20\n      - name: Test") {
		t.Fatalf("inspection %s", inspect.body)
	}
	assertShape(t, loadSpec(t), "CacheInspection", inspect.body)
	if f.cacheReads.Load() != 1 {
		t.Fatal("inspection did not read exactly once")
	}
	captureBody := map[string]any{"installation_id": instID, "repository": "acme/repo", "selection": selection, "head_sha": inspection.HeadSHA, "source_sha256": inspection.SourceSHA256, "lockfile_sha256": inspection.LockfileSHA256}
	created := h.do(request{method: http.MethodPost, path: "/api/v1/optimisations/cache/previews", token: token, body: captureBody})
	created.mustStatus(t, 201, "capture")
	var saved CachePreviewView
	if err := json.Unmarshal(created.body, &saved); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(saved.ID, "opc_") || saved.PackageManager != "npm" || saved.Runs != 2 || saved.SetupMS != 5000 || saved.Currentness != "not_checked" || saved.Verification != "inconclusive" || !strings.Contains(saved.After, "cache: npm") || strings.Contains(string(created.body), "on: push") {
		t.Fatalf("capture %s", created.body)
	}
	assertShape(t, loadSpec(t), "CachePreview", created.body)
	if auditCount(t, h, "optimisation.cache_preview_retained") != 1 || f.cacheReads.Load() != 2 {
		t.Fatal("capture audit or read count")
	}
	path := fmt.Sprintf("/api/v1/optimisations/cache/previews/%s?installation_id=%s&repo=acme/repo", saved.ID, instID)
	read := h.do(request{method: http.MethodGet, path: path, token: token})
	read.mustStatus(t, 200, "replay")
	var replayed CachePreviewView
	if err := json.Unmarshal(read.body, &replayed); err != nil {
		t.Fatal(err)
	}
	if replayed != saved {
		t.Fatalf("replay changed the preview: %s", read.body)
	}
	if f.cacheReads.Load() != 2 || strings.Contains(string(read.body), "on: push") {
		t.Fatal("replay read GitHub or exposed the workflow")
	}
	h.do(request{method: http.MethodGet, path: strings.Replace(path, "repo=acme/repo", "repo=other/repo", 1), token: token}).mustStatus(t, 404, "wrong scope")
	approveBody := map[string]any{"installation_id": instID, "repository": "acme/repo", "head_sha": saved.HeadSHA, "source_sha256": saved.SourceSHA256}
	approved := h.do(request{method: http.MethodPost, path: path[:strings.Index(path, "?")] + "/approval", token: token, body: approveBody})
	approved.mustStatus(t, 200, "approval")
	var decided CachePreviewView
	if err := json.Unmarshal(approved.body, &decided); err != nil {
		t.Fatal(err)
	}
	if decided.Approval == nil || decided.Approval.ApprovedBy != saved.ReviewedBy || decided.Approval.HeadSHA != saved.HeadSHA || decided.Approval.SourceSHA256 != saved.SourceSHA256 || decided.Approval.PreviewSHA256 != saved.SHA256 {
		t.Fatalf("approval %s", approved.body)
	}
	assertShape(t, loadSpec(t), "CachePreview", approved.body)
	if auditCount(t, h, "optimisation.cache_preview_approved") != 1 || f.cacheReads.Load() != 2 {
		t.Fatal("approval audited wrongly or read GitHub")
	}
	after := h.do(request{method: http.MethodGet, path: path, token: token})
	after.mustStatus(t, 200, "replay after approval")
	var replayedAfter CachePreviewView
	if err := json.Unmarshal(after.body, &replayedAfter); err != nil || replayedAfter.Approval == nil || replayedAfter.Approval.ApprovedBy != saved.ReviewedBy {
		t.Fatalf("replay lost the approval: %v %s", err, after.body)
	}
	approveBody["head_sha"] = strings.Repeat("f", 40)
	h.do(request{method: http.MethodPost, path: path[:strings.Index(path, "?")] + "/approval", token: token, body: approveBody}).mustStatus(t, 409, "wrong head")
	h.ctrl.UpdateConfig(func(c *config.Config) { c.Optimisation.Publish = true })
	f.publishPR = &github.PullRequest{Number: 21, HTMLURL: "https://github.com/acme/repo/pull/21", Branch: "zoomies/cache-" + saved.ID}
	publishToken := h.token("publisher", store.RoleOperator, "jobs:read", "optimisations:write", "optimisations:read", "optimisations:publish")
	publishBody := map[string]any{"installation_id": instID, "repository": "acme/repo", "head_sha": saved.HeadSHA, "source_sha256": saved.SourceSHA256}
	published := h.do(request{method: http.MethodPost, path: path[:strings.Index(path, "?")] + "/publication", token: publishToken, body: publishBody})
	published.mustStatus(t, 200, "publication")
	var drafted CachePreviewView
	if err := json.Unmarshal(published.body, &drafted); err != nil {
		t.Fatal(err)
	}
	if drafted.Publication == nil || drafted.Publication.PullNumber != 21 || drafted.Publication.PullURL != "https://github.com/acme/repo/pull/21" || drafted.Publication.Branch != "zoomies/cache-"+saved.ID || drafted.Publication.PublishedBy == "" || f.publishes != 1 {
		t.Fatalf("publication %s", published.body)
	}
	if f.publishReq.Path != saved.WorkflowPath || f.publishReq.Content == "" || f.publishReq.SourceSHA256 != saved.SourceSHA256 || f.publishReq.Base != "main" || f.publishReq.Head != "zoomies/cache-"+saved.ID {
		t.Fatalf("publish request %+v", f.publishReq)
	}
	assertShape(t, loadSpec(t), "CachePreview", published.body)
	if auditCount(t, h, "optimisation.cache_preview_published") != 1 || f.cacheReads.Load() != 3 {
		t.Fatal("publication audit or read count")
	}
	again := h.do(request{method: http.MethodPost, path: path[:strings.Index(path, "?")] + "/publication", token: publishToken, body: publishBody})
	again.mustStatus(t, 200, "repeat publication")
	if f.publishes != 1 {
		t.Fatal("repeat publication reached GitHub")
	}
}

func TestCacheOptimisationRefusesUntilEnabledAndAMovedSource(t *testing.T) {
	h, instID, f, selection := cacheAPIHarness(t)
	token := h.token("operator", store.RoleOperator, "jobs:read", "optimisations:write", "optimisations:read")
	body := map[string]any{"installation_id": instID, "repository": "acme/repo", "selection": selection}
	h.ctrl.UpdateConfig(func(c *config.Config) { c.Optimisation.SourceReads = false })
	off := h.do(request{method: http.MethodPost, path: "/api/v1/optimisations/cache/inspections", token: token, body: body})
	off.mustStatus(t, 403, "off")
	if !strings.Contains(string(off.body), "optimisation.source_reads") || f.cacheReads.Load() != 0 {
		t.Fatalf("off refusal read: %s", off.body)
	}
	h.ctrl.UpdateConfig(func(c *config.Config) { c.Optimisation.SourceReads = true })
	inspect := h.do(request{method: http.MethodPost, path: "/api/v1/optimisations/cache/inspections", token: token, body: body})
	inspect.mustStatus(t, 200, "inspect")
	var got CacheInspectionView
	if err := json.Unmarshal(inspect.body, &got); err != nil {
		t.Fatal(err)
	}
	capture := map[string]any{"installation_id": instID, "repository": "acme/repo", "selection": selection, "head_sha": got.HeadSHA, "source_sha256": strings.Repeat("f", 64), "lockfile_sha256": got.LockfileSHA256}
	moved := h.do(request{method: http.MethodPost, path: "/api/v1/optimisations/cache/previews", token: token, body: capture})
	moved.mustStatus(t, 409, "moved source")
	if auditCount(t, h, "optimisation.cache_preview_retained") != 0 {
		t.Fatal("a moved source was retained")
	}
	h.do(request{method: http.MethodPost, path: "/api/v1/optimisations/cache/inspections", token: token, body: map[string]any{"installation_id": instID, "repository": "acme/repo", "selection": map[string]any{"run_id": 10, "attempt": 2, "github_job_id": "100"}}}).mustStatus(t, 400, "numeric run ID")
}
