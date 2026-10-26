package api

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/eyupio/zoomies/internal/config"
	"github.com/eyupio/zoomies/internal/github"
	"github.com/eyupio/zoomies/internal/kennel"
	"github.com/eyupio/zoomies/internal/prrepair"
	"github.com/eyupio/zoomies/internal/store"
)

const kennelFixAPIWorkflow = "on: push\njobs:\n  build:\n    name: Build\n    runs-on: ubuntu-latest\n    steps:\n      - run: make\n"

// kennelFixAPIHarness is a tracked repository Kennel Club has evaluated with one
// open ci.no_timeout finding, and a GitHub fake that serves the file it names.
func kennelFixAPIHarness(t *testing.T) (*harness, string, *checkoutAPIReader, map[string]any) {
	t.Helper()
	h, instID, f, _ := checkoutAPIHarness(t, true)
	h.ctrl.UpdateConfig(func(c *config.Config) { c.Kennel.Enabled = true })
	blob := cacheAPISHA1(kennelFixAPIWorkflow)
	path := ".github/workflows/ci.yml"
	row, err := h.st.TouchKennelRepository(h.ctx, store.KennelRepositoryRef{GitHubHost: "github.com", RepositoryID: 77, InstallationID: instID, FullName: "acme/repo", Visibility: "private"})
	if err != nil {
		t.Fatal(err)
	}
	zero := 0
	ev := kennel.Evaluation{Version: kennel.Version, Findings: []kennel.Finding{{
		Code: kennel.CodeNoTimeout, Severity: kennel.SeverityWarning, Subject: blob[:12], Title: "Jobs have no explicit timeout",
		Evidence: []kennel.Evidence{{Kind: kennel.EvidenceFile, Ref: blob, JobIndex: &zero, Line: 3}},
	}}}
	evJSON, _ := json.Marshal(ev)
	watermark, _ := json.Marshal(map[string]any{"workflow_files": []map[string]string{{"sha": blob, "path": path}}})
	now := time.Now().UTC()
	if err := h.st.SaveKennelEvaluation(h.ctx, row.ID, store.KennelEvaluationRecord{
		State: string(kennel.StateAttention), EvaluatorVersion: kennel.Version, EvaluatedAt: now, NextDueAt: now.Add(time.Hour),
		InputsDigest: "x", Coverage: json.RawMessage(`{}`), Evaluation: evJSON, Watermark: watermark, OpenWarnings: 1,
	}); err != nil {
		t.Fatal(err)
	}
	f.kennelFixSource = &github.KennelFixSource{
		Version: "kennel-fix-source-v1", Repository: "acme/repo", Branch: "main", HeadSHA: strings.Repeat("a", 40), Path: path, BlobSHA: blob,
		Workflow: prrepair.File{Path: path, Mode: "100644", Content: kennelFixAPIWorkflow}, WorkflowSHA256: cacheAPISHA256(kennelFixAPIWorkflow),
	}
	return h, instID, f, map[string]any{"kennel_repository_id": row.ID, "blob_sha": blob, "job_index": 0, "minutes": 30}
}

func TestKennelTimeoutFixIsInspectedKeptApprovedAndPublishedWithoutExposingTheFile(t *testing.T) {
	h, instID, f, body := kennelFixAPIHarness(t)
	token := h.token("operator", store.RoleOperator, "kennel:read", "optimisations:write", "optimisations:read")
	resp := h.do(request{method: http.MethodPost, path: "/api/v1/kennel/fixes/timeout/inspections", token: token, body: body})
	resp.mustStatus(t, 200, "inspect")
	var got KennelFixInspectionView
	if err := json.Unmarshal(resp.body, &got); err != nil {
		t.Fatal(err)
	}
	if got.CheckCode != "ci.no_timeout" || got.JobName != "Build" || got.Minutes != 30 || got.Verification != "recheck" || got.Branch != "main" || !strings.Contains(got.After, "timeout-minutes: 30") || strings.Contains(string(resp.body), "on: push") || resp.header.Get("Cache-Control") != "no-store" {
		t.Fatalf("inspection %s", resp.body)
	}
	assertShape(t, loadSpec(t), "KennelFixInspection", resp.body)
	if f.kennelFixReads.Load() != 1 || auditCount(t, h, "kennel.fix_preview_retained") != 0 {
		t.Fatal("inspection read more than once or retained something")
	}

	capture := map[string]any{"head_sha": got.HeadSHA, "source_sha256": got.SourceSHA256}
	for k, v := range body {
		capture[k] = v
	}
	created := h.do(request{method: http.MethodPost, path: "/api/v1/kennel/fixes/timeout/previews", token: token, body: capture})
	created.mustStatus(t, 201, "capture")
	var saved KennelFixPreviewView
	if err := json.Unmarshal(created.body, &saved); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(saved.ID, "kfp_") || saved.Currentness != "not_checked" || saved.Minutes != 30 || strings.Contains(string(created.body), "on: push") || saved.KennelRepositoryID != body["kennel_repository_id"] {
		t.Fatalf("capture %s", created.body)
	}
	assertShape(t, loadSpec(t), "KennelFixPreview", created.body)
	if auditCount(t, h, "kennel.fix_preview_retained") != 1 || f.kennelFixReads.Load() != 2 {
		t.Fatal("capture audit or read count")
	}
	readPath := "/api/v1/kennel/fixes/timeout/previews/" + saved.ID + "?installation_id=" + instID + "&repo=acme/repo"
	read := h.do(request{method: http.MethodGet, path: readPath, token: token})
	read.mustStatus(t, 200, "replay")
	var replayed KennelFixPreviewView
	if err := json.Unmarshal(read.body, &replayed); err != nil {
		t.Fatal(err)
	}
	if replayed != saved || f.kennelFixReads.Load() != 2 || strings.Contains(string(read.body), "on: push") {
		t.Fatalf("replay changed or exposed the file: %s", read.body)
	}

	binding := map[string]any{"installation_id": instID, "repository": "acme/repo", "head_sha": saved.HeadSHA, "source_sha256": saved.SourceSHA256}
	approvePath := "/api/v1/kennel/fixes/timeout/previews/" + saved.ID + "/approval"
	h.do(request{method: http.MethodPost, path: approvePath, token: token, body: binding}).mustStatus(t, 200, "approve")
	if auditCount(t, h, "kennel.fix_preview_approved") != 1 {
		t.Fatal("approval not audited")
	}
	h.do(request{method: http.MethodPost, path: approvePath, token: token, body: binding}).mustStatus(t, 200, "repeat approval")
	if auditCount(t, h, "kennel.fix_preview_approved") != 1 {
		t.Fatal("a repeat approval wrote a second audit row")
	}
	wrong := map[string]any{"installation_id": instID, "repository": "acme/repo", "head_sha": strings.Repeat("f", 40), "source_sha256": saved.SourceSHA256}
	h.do(request{method: http.MethodPost, path: approvePath, token: token, body: wrong}).mustStatus(t, 409, "wrong head")

	publishPath := "/api/v1/kennel/fixes/timeout/previews/" + saved.ID + "/publication"
	h.do(request{method: http.MethodPost, path: publishPath, token: token, body: binding}).mustStatus(t, 403, "operator without the publish scope")
	h.ctrl.UpdateConfig(func(c *config.Config) { c.Optimisation.Publish = true })
	f.publishPR = &github.PullRequest{Number: 31, HTMLURL: "https://github.com/acme/repo/pull/31", Branch: "zoomies/kennel-fix-" + saved.ID}
	publisher := h.token("publisher", store.RoleOperator, "kennel:read", "optimisations:write", "optimisations:read", "optimisations:publish")
	published := h.do(request{method: http.MethodPost, path: publishPath, token: publisher, body: binding})
	published.mustStatus(t, 200, "publish")
	var drafted KennelFixPreviewView
	if err := json.Unmarshal(published.body, &drafted); err != nil {
		t.Fatal(err)
	}
	if drafted.Publication == nil || drafted.Publication.PullNumber != 31 || drafted.Publication.Branch != "zoomies/kennel-fix-"+saved.ID {
		t.Fatalf("publication %s", published.body)
	}
	assertShape(t, loadSpec(t), "KennelFixPreview", published.body)
	if f.publishReq.Path != saved.WorkflowPath || f.publishReq.SourceSHA256 != saved.SourceSHA256 || f.publishReq.Base != "main" || !strings.Contains(f.publishReq.Content, "timeout-minutes: 30") || auditCount(t, h, "kennel.fix_preview_published") != 1 {
		t.Fatalf("publish request %+v", f.publishReq)
	}
	h.do(request{method: http.MethodPost, path: publishPath, token: publisher, body: binding}).mustStatus(t, 200, "repeat publication")
	if f.publishes != 1 {
		t.Fatal("repeat publication reached GitHub")
	}
}

func TestKennelTimeoutFixRefusesBadRequestsAnOffSwitchAndAFindingThatIsNotOpen(t *testing.T) {
	h, _, f, body := kennelFixAPIHarness(t)
	token := h.token("operator", store.RoleOperator, "kennel:read", "optimisations:write", "optimisations:read")
	inspect := func(mutate func(map[string]any)) *response {
		b := map[string]any{}
		for k, v := range body {
			b[k] = v
		}
		mutate(b)
		return h.do(request{method: http.MethodPost, path: "/api/v1/kennel/fixes/timeout/inspections", token: token, body: b})
	}
	inspect(func(b map[string]any) { b["minutes"] = 0 }).mustStatus(t, 400, "no minutes")
	inspect(func(b map[string]any) { b["minutes"] = 9999 }).mustStatus(t, 400, "too many minutes")
	inspect(func(b map[string]any) { b["blob_sha"] = "ABC" }).mustStatus(t, 400, "a blob that is not a name")
	inspect(func(b map[string]any) { b["kennel_repository_id"] = "pool_1" }).mustStatus(t, 400, "not a Kennel repository")
	inspect(func(b map[string]any) { b["blob_sha"] = strings.Repeat("e", 40) }).mustStatus(t, 409, "a file no finding names")
	h.ctrl.UpdateConfig(func(c *config.Config) { c.Optimisation.WorkflowEdits = false })
	off := inspect(func(map[string]any) {})
	off.mustStatus(t, 403, "edits off")
	if !strings.Contains(string(off.body), "optimisation.workflow_edits") || f.kennelFixReads.Load() != 0 {
		t.Fatalf("an off switch read GitHub or did not say which: %s", off.body)
	}
	viewer := h.token("viewer", store.RoleViewer, "kennel:read", "optimisations:read")
	h.do(request{method: http.MethodPost, path: "/api/v1/kennel/fixes/timeout/inspections", token: viewer, body: body}).mustStatus(t, 403, "a viewer")
}
