package api

import (
	"context"
	"crypto/sha1"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/eyupio/zoomies/internal/config"
	"github.com/eyupio/zoomies/internal/controller"
	"github.com/eyupio/zoomies/internal/github"
	"github.com/eyupio/zoomies/internal/optimisation"
	"github.com/eyupio/zoomies/internal/prrepair"
	"github.com/eyupio/zoomies/internal/store"
)

const checkoutAPISecret = "private_workflow_name"

type checkoutAPIFactory struct {
	base   *fakeFactory
	reader *checkoutAPIReader
}

func (f *checkoutAPIFactory) For(ctx context.Context, inst *store.Installation, pem []byte) (github.Client, error) {
	raw, err := f.base.For(ctx, inst, pem)
	f.reader.Client = raw
	return f.reader, err
}

type checkoutAPIReader struct {
	github.Client
	evidence        *github.CheckoutEvidence
	reads           atomic.Int32
	err             error
	publishes       int
	publishPR       *github.PullRequest
	publishErr      error
	publishReq      github.OptimisationPublishRequest
	cacheSource     *github.SetupCacheSource
	cacheReads      atomic.Int32
	readinessSource *github.ReadinessSource
	shardSource     *github.ShardSource
	shardReads      atomic.Int32
	readinessReads  atomic.Int32
}

func (f *checkoutAPIReader) RateLimit(context.Context) (*github.RateLimit, error) {
	return &github.RateLimit{Limit: 5000, Remaining: 5000, ResetAt: time.Now().Add(time.Hour)}, nil
}
func (f *checkoutAPIReader) ReadCheckoutEvidence(context.Context, string, github.CheckoutSelection) (*github.CheckoutEvidence, error) {
	f.reads.Add(1)
	return f.evidence, f.err
}
func (f *checkoutAPIReader) PublishOptimisation(_ context.Context, req github.OptimisationPublishRequest) (*github.PullRequest, error) {
	f.publishes++
	f.publishReq = req
	return f.publishPR, f.publishErr
}
func (f *checkoutAPIReader) ReadSetupCacheSource(context.Context, string, github.SetupCacheSelection) (*github.SetupCacheSource, error) {
	f.cacheReads.Add(1)
	return f.cacheSource, f.err
}
func (f *checkoutAPIReader) ReadShardSource(context.Context, string, github.SetupCacheSelection) (*github.ShardSource, error) {
	f.shardReads.Add(1)
	return f.shardSource, f.err
}
func (f *checkoutAPIReader) ReadReadinessSource(context.Context, string, github.SetupCacheSelection) (*github.ReadinessSource, error) {
	f.readinessReads.Add(1)
	return f.readinessSource, f.err
}

func checkoutAPIEvidence(repo string, sel github.CheckoutSelection) *github.CheckoutEvidence {
	hash := func(s string) string { v := sha256.Sum256([]byte(s)); return hex.EncodeToString(v[:]) }
	source := optimisation.Source{HeadSHA: strings.Repeat("a", 40), File: prrepair.File{Path: ".github/workflows/ci.yml", Mode: "100644", Content: "on: push\njobs:\n  build:\n    name: Build\n    runs-on: ubuntu-latest\n    steps:\n      - name: Checkout\n        uses: actions/checkout@" + strings.Repeat("b", 40) + "\n        with:\n          fetch-depth: 0\n      - name: Say\n        run: echo " + checkoutAPISecret + "\n"}}
	blob := sha1.Sum([]byte("blob " + strconv.Itoa(len(source.File.Content)) + "\x00" + source.File.Content))
	ms := int64(4200)
	return &github.CheckoutEvidence{Version: "checkout-evidence-v1", Basis: "push_source_unique_literal_names", Repository: repo, Selection: sel, StepNumber: 42, BlobSHA: hex.EncodeToString(blob[:]), JobNameSHA256: hash("Build"), StepNameSHA256: hash("Checkout"), HeadBranch: "main", Source: source, Timing: optimisation.Timing{EvidenceID: fmt.Sprintf("checkout:%d:%d:%d:42", sel.RunID, sel.Attempt, sel.GitHubJobID), HeadSHA: source.HeadSHA, SourceSHA256: hash(source.File.Content), RunID: strconv.FormatInt(sel.RunID, 10), Attempt: sel.Attempt, JobIndex: sel.JobIndex, StepIndex: sel.StepIndex, Outcome: "success", CheckoutMS: &ms}}
}

func checkoutAPIHarness(t *testing.T, enabled bool, opts ...func(*config.Config)) (*harness, string, *checkoutAPIReader, map[string]any) {
	t.Helper()
	opts = append(opts, func(c *config.Config) {
		c.Optimisation.SourceReads = enabled
		c.Optimisation.WorkflowEdits = enabled
	})
	h := newHarness(t, opts...)
	inst := h.installation()
	sel := github.CheckoutSelection{RunID: 10, Attempt: 2, GitHubJobID: 100}
	f := &checkoutAPIReader{evidence: checkoutAPIEvidence("acme/repo", sel)}
	ctrl, err := controller.New(controller.Options{Store: h.st, Config: h.cfg, Key: h.key, Auth: h.ctrl.Auth(), GitHub: &checkoutAPIFactory{base: &fakeFactory{gh: h.gh}, reader: f}})
	if err != nil {
		t.Fatal(err)
	}
	h.ctrl = ctrl
	h.api.ctrl = ctrl
	body := map[string]any{"installation_id": inst.ID, "repository": "acme/repo", "selection": map[string]any{"run_id": "10", "attempt": 2, "github_job_id": "100", "job_index": 0, "step_index": 0}}
	return h, inst.ID, f, body
}

func previewBody(inspection CheckoutInspectionView, body map[string]any) map[string]any {
	out := map[string]any{"history_reviewed": true, "head_sha": inspection.HeadSHA, "source_sha256": inspection.SourceSHA256}
	for k, v := range body {
		out[k] = v
	}
	return out
}

func TestCheckoutOptimisationInspectCaptureAndReplayKeepTheSourceToTheInspection(t *testing.T) {
	h, instID, f, body := checkoutAPIHarness(t, true)
	token := h.token("operator", store.RoleOperator, "jobs:read", "optimisations:write", "optimisations:read")
	resp := h.do(request{method: http.MethodPost, path: "/api/v1/optimisations/checkout/inspections", token: token, body: body})
	resp.mustStatus(t, 200, "inspect")
	var inspection CheckoutInspectionView
	if err := json.Unmarshal(resp.body, &inspection); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(inspection.Content, checkoutAPISecret) || inspection.CheckoutMS != 4200 || resp.header.Get("Cache-Control") != "no-store" || f.reads.Load() != 1 {
		t.Fatalf("inspection %s", resp.body)
	}
	assertShape(t, loadSpec(t), "CheckoutInspection", resp.body)

	created := h.do(request{method: http.MethodPost, path: "/api/v1/optimisations/checkout/previews", token: token, body: previewBody(inspection, body)})
	created.mustStatus(t, 201, "capture")
	var saved CheckoutPreviewView
	if err := json.Unmarshal(created.body, &saved); err != nil {
		t.Fatal(err)
	}
	if saved.Preview.Before != "          fetch-depth: 0\n" || saved.Preview.After != "          fetch-depth: 1\n" || saved.Preview.Verification != "inconclusive" || saved.Currentness != "not_checked" || saved.ReviewedBy == "" || strings.Contains(string(created.body), checkoutAPISecret) || f.reads.Load() != 2 {
		t.Fatalf("capture %s", created.body)
	}
	assertShape(t, loadSpec(t), "CheckoutPreview", created.body)

	path := fmt.Sprintf("/api/v1/optimisations/checkout/previews/%s?installation_id=%s&repo=acme/repo", saved.ID, instID)
	approvePath := fmt.Sprintf("/api/v1/optimisations/checkout/previews/%s/approval", saved.ID)
	approvalBody := map[string]any{"installation_id": instID, "repository": "acme/repo", "head_sha": saved.Preview.HeadSHA, "source_sha256": saved.Preview.SourceSHA256}
	approve := h.do(request{method: http.MethodPost, path: approvePath, token: token, body: approvalBody})
	approve.mustStatus(t, 200, "approve")
	var approved CheckoutPreviewView
	if err := json.Unmarshal(approve.body, &approved); err != nil {
		t.Fatal(err)
	}
	if approved.Approval == nil || approved.Approval.ApprovedBy == "" || approved.Approval.HeadSHA != saved.Preview.HeadSHA || approved.Approval.SourceSHA256 != saved.Preview.SourceSHA256 || approved.Approval.PreviewSHA256 != saved.SHA256 || approved.ReviewedBy != saved.ReviewedBy {
		t.Fatalf("approval %s", approve.body)
	}
	assertShape(t, loadSpec(t), "CheckoutPreview", approve.body)
	read := h.do(request{method: http.MethodGet, path: path, token: token})
	read.mustStatus(t, 200, "replay")
	var replayed CheckoutPreviewView
	if err := json.Unmarshal(read.body, &replayed); err != nil {
		t.Fatal(err)
	}
	if replayed.Preview != saved.Preview || replayed.ID != saved.ID || replayed.SHA256 != saved.SHA256 || replayed.Approval == nil || replayed.Approval.ApprovedBy != approved.Approval.ApprovedBy || !replayed.Approval.ApprovedAt.Equal(approved.Approval.ApprovedAt) {
		t.Fatalf("replay changed the preview or lost its approval: %s", read.body)
	}
	if f.reads.Load() != 2 || strings.Contains(string(read.body), checkoutAPISecret) {
		t.Fatal("approval or replay read GitHub again or exposed the workflow file")
	}
	if auditCount(t, h, "optimisation.preview_retained") != 1 || auditCount(t, h, "optimisation.preview_approved") != 1 {
		t.Fatal("capture and approval were not audited exactly once")
	}
	h.do(request{method: http.MethodGet, path: strings.Replace(path, "repo=acme/repo", "repo=other/repo", 1), token: token}).mustStatus(t, 404, "wrong scope")
}

func TestCheckoutApprovalBindsTheRetainedPreviewAndIsIdempotent(t *testing.T) {
	h, instID, f, body := checkoutAPIHarness(t, true)
	token := h.token("operator", store.RoleOperator, "jobs:read", "optimisations:write", "optimisations:read")
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
	path := fmt.Sprintf("/api/v1/optimisations/checkout/previews/%s/approval", saved.ID)
	valid := map[string]any{"installation_id": instID, "repository": "acme/repo", "head_sha": saved.Preview.HeadSHA, "source_sha256": saved.Preview.SourceSHA256}

	// A digest that is not the retained one is a refusal, not an approval of
	// whatever happens to be in the database now.
	moved := map[string]any{}
	for k, v := range valid {
		moved[k] = v
	}
	moved["source_sha256"] = strings.Repeat("d", 64)
	h.do(request{method: http.MethodPost, path: path, token: token, body: moved}).mustStatus(t, 409, "moved source")
	if auditCount(t, h, "optimisation.preview_approved") != 0 {
		t.Fatal("a refused approval wrote an audit row")
	}
	// Malformed requests are refused before the store is asked.
	for _, missing := range []string{"installation_id", "repository", "head_sha", "source_sha256"} {
		bad := map[string]any{}
		for k, v := range valid {
			if k != missing {
				bad[k] = v
			}
		}
		h.do(request{method: http.MethodPost, path: path, token: token, body: bad}).mustStatus(t, 400, "missing "+missing)
	}
	h.do(request{method: http.MethodPost, path: "/api/v1/optimisations/checkout/previews/not-a-preview/approval", token: token, body: valid}).mustStatus(t, 400, "bad preview id")
	h.do(request{method: http.MethodPost, path: path, token: token, body: map[string]any{}}).mustStatus(t, 400, "empty body")

	first := h.do(request{method: http.MethodPost, path: path, token: token, body: valid})
	first.mustStatus(t, 200, "approve")
	var approved CheckoutPreviewView
	if err := json.Unmarshal(first.body, &approved); err != nil {
		t.Fatal(err)
	}
	if approved.Approval == nil || approved.Approval.PreviewSHA256 != saved.SHA256 || approved.Approval.HeadSHA != saved.Preview.HeadSHA || approved.Approval.SourceSHA256 != saved.Preview.SourceSHA256 {
		t.Fatalf("approval %s", first.body)
	}
	// Another operator pressing on the same preview sees the act that was
	// already recorded rather than a second approval.
	second := h.token("other-operator", store.RoleOperator, "jobs:read", "optimisations:write", "optimisations:read")
	again := h.do(request{method: http.MethodPost, path: path, token: second, body: valid})
	again.mustStatus(t, 200, "repeat")
	var repeated CheckoutPreviewView
	if err := json.Unmarshal(again.body, &repeated); err != nil {
		t.Fatal(err)
	}
	if repeated.Approval == nil || repeated.Approval.ApprovedBy != approved.Approval.ApprovedBy || !repeated.Approval.ApprovedAt.Equal(approved.Approval.ApprovedAt) {
		t.Fatalf("a repeat replaced the first approval: %s", again.body)
	}
	if auditCount(t, h, "optimisation.preview_approved") != 1 {
		t.Fatal("the repeat wrote a second approval audit row")
	}
	if f.reads.Load() != 2 {
		t.Fatal("approval read GitHub source again")
	}
	// The preview remains scoped; a different installation sees no preview and
	// therefore no approval.
	other := map[string]any{}
	for k, v := range valid {
		other[k] = v
	}
	other["installation_id"] = "ins_missing"
	h.do(request{method: http.MethodPost, path: path, token: token, body: other}).mustStatus(t, 404, "wrong installation")
}

func TestCheckoutApprovalDoesNotNeedTheSourceSettings(t *testing.T) {
	h, instID, f, body := checkoutAPIHarness(t, true)
	token := h.token("operator", store.RoleOperator, "jobs:read", "optimisations:write", "optimisations:read")
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
	h.ctrl.UpdateConfig(func(c *config.Config) { c.Optimisation.SourceReads, c.Optimisation.WorkflowEdits = false, false })
	// Approval is a decision about evidence already kept, so turning the source
	// settings off stops new reads without erasing the ability to act on what
	// was kept while they were on.
	approved := h.do(request{method: http.MethodPost, path: fmt.Sprintf("/api/v1/optimisations/checkout/previews/%s/approval", saved.ID), token: token,
		body: map[string]any{"installation_id": instID, "repository": "acme/repo", "head_sha": saved.Preview.HeadSHA, "source_sha256": saved.Preview.SourceSHA256}})
	approved.mustStatus(t, 200, "approval with the settings off")
	if f.reads.Load() != 2 {
		t.Fatal("approval read source")
	}
}

func TestCheckoutOptimisationIsRefusedUntilTheSettingsAreOnWithoutReadingSource(t *testing.T) {
	h, _, f, body := checkoutAPIHarness(t, false)
	token := h.token("operator", store.RoleOperator, "jobs:read", "optimisations:write")
	resp := h.do(request{method: http.MethodPost, path: "/api/v1/optimisations/checkout/inspections", token: token, body: body})
	resp.mustStatus(t, 403, "inspect while off")
	if !strings.Contains(string(resp.body), "optimisation.source_reads") || f.reads.Load() != 0 {
		t.Fatalf("refusal must name the setting and read nothing: %s", resp.body)
	}
	pb := map[string]any{"history_reviewed": true, "head_sha": strings.Repeat("a", 40), "source_sha256": strings.Repeat("c", 64)}
	for k, v := range body {
		pb[k] = v
	}
	h.do(request{method: http.MethodPost, path: "/api/v1/optimisations/checkout/previews", token: token, body: pb}).mustStatus(t, 403, "capture while off")
	if f.reads.Load() != 0 {
		t.Fatal("a refused capture read source")
	}
}

func TestCheckoutOptimisationNeedsOperatorScopesAnIdentityAndNoVisitors(t *testing.T) {
	h, _, f, body := checkoutAPIHarness(t, true)
	path := "/api/v1/optimisations/checkout/inspections"
	h.do(request{method: http.MethodPost, path: path, body: body}).mustStatus(t, 401, "anonymous")
	h.do(request{method: http.MethodPost, path: path, token: h.token("viewer", store.RoleViewer, "jobs:read", "optimisations:write"), body: body}).mustStatus(t, 403, "viewer")
	h.do(request{method: http.MethodPost, path: path, token: h.token("no-scope", store.RoleOperator, "jobs:read"), body: body}).mustStatus(t, 403, "read-only token")
	h.do(request{method: http.MethodPost, path: path, token: h.token("no-jobs", store.RoleOperator, "optimisations:write"), body: body}).mustStatus(t, 403, "no jobs:read")
	if f.reads.Load() != 0 {
		t.Fatal("an unauthorised caller read source")
	}
	public, _, pf, pbody := checkoutAPIHarness(t, true, func(c *config.Config) { c.Security.PublicView = true })
	public.do(request{method: http.MethodPost, path: path, body: pbody}).mustStatus(t, 403, "visitor inspect")
	public.do(request{method: http.MethodGet, path: "/api/v1/optimisations/checkout/previews/opv_x?installation_id=a&repo=b"}).mustStatus(t, 403, "visitor replay")
	if pf.reads.Load() != 0 {
		t.Fatal("a visitor read source")
	}
}

func TestCheckoutOptimisationRefusesAnUnreviewedMovedOrMalformedRequest(t *testing.T) {
	h, _, f, body := checkoutAPIHarness(t, true)
	token := h.token("operator", store.RoleOperator, "jobs:read", "optimisations:write")
	inspect := "/api/v1/optimisations/checkout/inspections"
	capture := "/api/v1/optimisations/checkout/previews"
	with := func(sel map[string]any) map[string]any {
		out := map[string]any{"installation_id": body["installation_id"], "repository": body["repository"], "selection": sel}
		return out
	}
	badSelections := []map[string]any{
		{"run_id": 10, "attempt": 2, "github_job_id": "100", "job_index": 0, "step_index": 0},
		{"run_id": "010", "attempt": 2, "github_job_id": "100", "job_index": 0, "step_index": 0},
		{"run_id": "10", "attempt": 0, "github_job_id": "100", "job_index": 0, "step_index": 0},
		{"run_id": "10", "attempt": 2, "github_job_id": "9223372036854775808", "job_index": 0, "step_index": 0},
		{"run_id": "10", "attempt": 2, "github_job_id": "100", "job_index": -1, "step_index": 0},
	}
	for i, sel := range badSelections {
		h.do(request{method: http.MethodPost, path: inspect, token: token, body: with(sel)}).mustStatus(t, 400, fmt.Sprintf("selection %d", i))
	}
	forged := with(body["selection"].(map[string]any))
	forged["policy"] = map[string]any{"source_allowed": true}
	h.do(request{method: http.MethodPost, path: inspect, token: token, body: forged}).mustStatus(t, 400, "forged policy field")
	if f.reads.Load() != 0 {
		t.Fatal("invalid input spent reads")
	}
	pb := func(reviewed bool, sha string) map[string]any {
		out := map[string]any{"history_reviewed": reviewed, "head_sha": strings.Repeat("a", 40), "source_sha256": sha}
		for k, v := range body {
			out[k] = v
		}
		return out
	}
	h.do(request{method: http.MethodPost, path: capture, token: token, body: pb(false, strings.Repeat("c", 64))}).mustStatus(t, 400, "history not confirmed")
	if f.reads.Load() != 0 {
		t.Fatal("an unconfirmed review spent reads")
	}
	// A digest that is not the one the server reads is the source having moved.
	h.do(request{method: http.MethodPost, path: capture, token: token, body: pb(true, strings.Repeat("c", 64))}).mustStatus(t, 409, "stale digest")
	if auditCount(t, h, "optimisation.preview_retained") != 0 {
		t.Fatal("a refused capture was retained")
	}
}

func TestCheckoutOptimisationQuotaAndReadFailuresSayWhatToDo(t *testing.T) {
	h, _, f, body := checkoutAPIHarness(t, true)
	token := h.token("operator", store.RoleOperator, "jobs:read", "optimisations:write")
	f.err = github.ErrForbidden
	resp := h.do(request{method: http.MethodPost, path: "/api/v1/optimisations/checkout/inspections", token: token, body: body})
	resp.mustStatus(t, 403, "App lacks contents")
	if !strings.Contains(string(resp.body), "Contents") {
		t.Fatalf("a permission failure must name the permission: %s", resp.body)
	}
}

func TestOptimisationEnablementIsOnePressForAnAdministratorAndAudited(t *testing.T) {
	h, _, f, body := checkoutAPIHarness(t, false)
	admin := h.token("admin", store.RoleAdmin, "*")
	state := func(token string) OptimisationEnablement {
		resp := h.do(request{method: http.MethodGet, path: "/api/v1/optimisations/enablement", token: token})
		resp.mustStatus(t, 200, "state")
		var out OptimisationEnablement
		if err := json.Unmarshal(resp.body, &out); err != nil {
			t.Fatal(err)
		}
		assertShape(t, loadSpec(t), "OptimisationEnablement", resp.body)
		return out
	}
	if s := state(admin); s.Enabled || s.Publish || s.PublishEnabled || !s.CanChange || !s.CanPublish {
		t.Fatalf("a fresh instance is off and an administrator can change it: %+v", s)
	}
	op := h.token("operator", store.RoleOperator, "jobs:read", "optimisations:read", "optimisations:write")
	if s := state(op); s.CanChange || s.CanPublish || !strings.Contains(s.Reason, "administrator") || !strings.Contains(s.PublishReason, "administrator") {
		t.Fatalf("an operator is told whom to ask: %+v", s)
	}
	// The operator is refused until the administrator presses the button, and
	// then the very next request goes through with no restart.
	h.do(request{method: http.MethodPost, path: "/api/v1/optimisations/checkout/inspections", token: op, body: body}).mustStatus(t, 403, "off")
	h.do(request{method: http.MethodPut, path: "/api/v1/optimisations/enablement", token: op, body: map[string]any{"enabled": true}}).mustStatus(t, 403, "operator cannot enable")
	put := h.do(request{method: http.MethodPut, path: "/api/v1/optimisations/enablement", token: admin, body: map[string]any{"enabled": true}})
	put.mustStatus(t, 200, "enable")
	var after OptimisationEnablement
	if err := json.Unmarshal(put.body, &after); err != nil || !after.Enabled || !after.SourceReads || !after.WorkflowEdits || after.Publish || after.PublishEnabled {
		t.Fatalf("both preview switches must come on together without publishing: %s", put.body)
	}
	h.do(request{method: http.MethodPost, path: "/api/v1/optimisations/checkout/inspections", token: op, body: body}).mustStatus(t, 200, "on")
	if f.reads.Load() != 1 {
		t.Fatal("the enabled instance did not read source")
	}
	if auditCount(t, h, "settings.update") != 1 {
		t.Fatal("turning the switches on must leave one settings audit row")
	}
	// Publishing is its own press and turns the preview grants on with it.
	put = h.do(request{method: http.MethodPut, path: "/api/v1/optimisations/enablement", token: admin, body: map[string]any{"publish": true}})
	put.mustStatus(t, 200, "publish enable")
	if err := json.Unmarshal(put.body, &after); err != nil || !after.Enabled || !after.Publish || !after.PublishEnabled || !after.SourceReads || !after.WorkflowEdits {
		t.Fatalf("publishing must turn all three on together: %s", put.body)
	}
	// Turning publishing off does not take the preview grants away.
	put = h.do(request{method: http.MethodPut, path: "/api/v1/optimisations/enablement", token: admin, body: map[string]any{"publish": false}})
	put.mustStatus(t, 200, "publish disable")
	if err := json.Unmarshal(put.body, &after); err != nil || !after.Enabled || after.Publish || after.PublishEnabled || !after.SourceReads || !after.WorkflowEdits {
		t.Fatalf("disabling publishing must leave previews on: %s", put.body)
	}
	h.do(request{method: http.MethodPut, path: "/api/v1/optimisations/enablement", token: admin, body: map[string]any{"enabled": false}}).mustStatus(t, 200, "disable")
	h.do(request{method: http.MethodPost, path: "/api/v1/optimisations/checkout/inspections", token: op, body: body}).mustStatus(t, 403, "off again")
	h.do(request{method: http.MethodPut, path: "/api/v1/optimisations/enablement", token: admin, body: map[string]any{}}).mustStatus(t, 400, "missing enabled or publish")
	h.do(request{method: http.MethodPut, path: "/api/v1/optimisations/enablement", token: admin, body: map[string]any{"enabled": true, "extra": 1}}).mustStatus(t, 400, "unknown field")
	h.do(request{method: http.MethodPut, path: "/api/v1/optimisations/enablement", token: op, body: map[string]any{"publish": true}}).mustStatus(t, 403, "operator cannot publish")
}

func TestOptimisationEnablementRefusesVisitorsAndAnonymous(t *testing.T) {
	h, _, _, _ := checkoutAPIHarness(t, false, func(c *config.Config) { c.Security.PublicView = true })
	h.do(request{method: http.MethodGet, path: "/api/v1/optimisations/enablement"}).mustStatus(t, 403, "visitor read")
	h.do(request{method: http.MethodPut, path: "/api/v1/optimisations/enablement", body: map[string]any{"enabled": true}}).mustStatus(t, 403, "visitor write")
}

func TestCheckoutOptimisationFindsTheStepWhenNoPositionsAreGiven(t *testing.T) {
	h, _, _, body := checkoutAPIHarness(t, true)
	token := h.token("operator", store.RoleOperator, "jobs:read", "optimisations:write")
	located := map[string]any{"installation_id": body["installation_id"], "repository": body["repository"], "selection": map[string]any{"run_id": "10", "attempt": 2, "github_job_id": "100"}}
	resp := h.do(request{method: http.MethodPost, path: "/api/v1/optimisations/checkout/inspections", token: token, body: located})
	resp.mustStatus(t, 200, "located")
	var got CheckoutInspectionView
	if err := json.Unmarshal(resp.body, &got); err != nil {
		t.Fatal(err)
	}
	if got.JobName != "Build" || got.StepName != "Checkout" || got.Selection.JobIndex == nil || got.Selection.StepIndex == nil {
		t.Fatalf("a person should see names and the positions used: %s", resp.body)
	}
	assertShape(t, loadSpec(t), "CheckoutInspection", resp.body)
	half := map[string]any{"installation_id": body["installation_id"], "repository": body["repository"], "selection": map[string]any{"run_id": "10", "attempt": 2, "github_job_id": "100", "job_index": 0}}
	h.do(request{method: http.MethodPost, path: "/api/v1/optimisations/checkout/inspections", token: token, body: half}).mustStatus(t, 400, "one position without the other")
}

func TestCheckoutPublicationOpensADraftForAnApprovedPreviewAndReconciles(t *testing.T) {
	h, instID, f, body := checkoutAPIHarness(t, true, func(c *config.Config) { c.Optimisation.Publish = true })
	token := h.token("operator", store.RoleOperator, "jobs:read", "optimisations:write", "optimisations:read", "optimisations:publish")
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
	publicationBody := map[string]any{"installation_id": instID, "repository": "acme/repo", "head_sha": saved.Preview.HeadSHA, "source_sha256": saved.Preview.SourceSHA256}
	path := fmt.Sprintf("/api/v1/optimisations/checkout/previews/%s/publication", saved.ID)
	// Publishing has its own live consent and is refused before any source read.
	reads := f.reads.Load()
	h.ctrl.UpdateConfig(func(c *config.Config) { c.Optimisation.Publish = false })
	off := h.do(request{method: http.MethodPost, path: path, token: token, body: publicationBody})
	off.mustStatus(t, 403, "publishing off")
	if !strings.Contains(string(off.body), "optimisation.publish") || f.reads.Load() != reads || f.publishes != 0 {
		t.Fatalf("publishing-off refusal read source or opened a draft: %s", off.body)
	}
	h.ctrl.UpdateConfig(func(c *config.Config) { c.Optimisation.Publish = true })
	publishedResp := h.do(request{method: http.MethodPost, path: path, token: token, body: publicationBody})
	publishedResp.mustStatus(t, 200, "publish")
	var published CheckoutPreviewView
	if err := json.Unmarshal(publishedResp.body, &published); err != nil {
		t.Fatal(err)
	}
	if published.Publication == nil || published.Publication.PullNumber != 21 || published.Publication.PullURL != "https://github.com/acme/repo/pull/21" || published.Publication.Branch != "zoomies/checkout-"+saved.ID || published.Publication.BaseBranch != "main" || published.Publication.HeadSHA != saved.Preview.HeadSHA || published.Publication.PublishedBy == "" {
		t.Fatalf("publication %s", publishedResp.body)
	}
	if f.publishes != 1 || f.publishReq.Base != "main" || f.publishReq.BaseCommit != saved.Preview.HeadSHA || f.publishReq.Path != saved.Preview.Path {
		t.Fatalf("publish request %+v", f.publishReq)
	}
	assertShape(t, loadSpec(t), "CheckoutPreview", publishedResp.body)
	if auditCount(t, h, "optimisation.preview_published") != 1 {
		t.Fatal("publication was not audited exactly once")
	}
	// A repeat is the same act; a token without the publish scope is refused.
	again := h.do(request{method: http.MethodPost, path: path, token: token, body: publicationBody})
	again.mustStatus(t, 200, "repeat")
	if f.publishes != 1 {
		t.Fatal("a repeat opened a second draft")
	}
	readonly := h.token("preview-only", store.RoleOperator, "jobs:read", "optimisations:write", "optimisations:read")
	h.do(request{method: http.MethodPost, path: path, token: readonly, body: publicationBody}).mustStatus(t, 403, "no publish scope")
}

func TestCheckoutPublicationRefusesAnUnapprovedOrMovedPreview(t *testing.T) {
	h, instID, f, body := checkoutAPIHarness(t, true, func(c *config.Config) { c.Optimisation.Publish = true })
	token := h.token("operator", store.RoleOperator, "jobs:read", "optimisations:write", "optimisations:read", "optimisations:publish")
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
	path := fmt.Sprintf("/api/v1/optimisations/checkout/previews/%s/publication", saved.ID)
	unapproved := map[string]any{"installation_id": instID, "repository": "acme/repo", "head_sha": saved.Preview.HeadSHA, "source_sha256": saved.Preview.SourceSHA256}
	resp := h.do(request{method: http.MethodPost, path: path, token: token, body: unapproved})
	resp.mustStatus(t, 409, "unapproved")
	if !strings.Contains(string(resp.body), "Approve this preview") || f.publishes != 0 {
		t.Fatalf("unapproved publication: %s", resp.body)
	}
	approve := h.do(request{method: http.MethodPost, path: fmt.Sprintf("/api/v1/optimisations/checkout/previews/%s/approval", saved.ID), token: token,
		body: map[string]any{"installation_id": instID, "repository": "acme/repo", "head_sha": saved.Preview.HeadSHA, "source_sha256": saved.Preview.SourceSHA256}})
	approve.mustStatus(t, 200, "approve")
	moved := map[string]any{"installation_id": instID, "repository": "acme/repo", "head_sha": saved.Preview.HeadSHA, "source_sha256": strings.Repeat("d", 64)}
	resp = h.do(request{method: http.MethodPost, path: path, token: token, body: moved})
	resp.mustStatus(t, 409, "moved source")
	if f.publishes != 0 {
		t.Fatal("a moved approval opened a draft")
	}
	// A missing App permission is a 403 that names the permission, not a
	// generic publication conflict.
	h.gh.SetPermissions(map[string]string{"contents": "read", "pull_requests": "write", "workflows": "write"})
	resp = h.do(request{method: http.MethodPost, path: path, token: token, body: unapproved})
	resp.mustStatus(t, 403, "missing Contents write")
	if !strings.Contains(string(resp.body), "Contents") || f.publishes != 0 {
		t.Fatalf("permission refusal: %s", resp.body)
	}
}
