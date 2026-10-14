package api

import (
	"context"
	"crypto/sha1"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/eyupio/zoomies/internal/config"
	"github.com/eyupio/zoomies/internal/controller"
	"github.com/eyupio/zoomies/internal/github"
	"github.com/eyupio/zoomies/internal/optimisation"
	"github.com/eyupio/zoomies/internal/prrepair"
	"github.com/eyupio/zoomies/internal/store"
)

type optimisationAPIFactory struct {
	base   *fakeFactory
	reader *optimisationAPIReader
}

func (f *optimisationAPIFactory) For(ctx context.Context, i *store.Installation, pem []byte) (github.Client, error) {
	c, e := f.base.For(ctx, i, pem)
	f.reader.Client = c
	return f.reader, e
}

type optimisationAPIReader struct {
	github.Client
	calls, quota atomic.Int32
	afterRead    func()
	err          error
}

func (f *optimisationAPIReader) RateLimit(context.Context) (*github.RateLimit, error) {
	f.quota.Add(1)
	return &github.RateLimit{Limit: 5000, Remaining: 5000}, nil
}
func (f *optimisationAPIReader) ReadCheckoutEvidence(_ context.Context, repo string, sel github.CheckoutSelection) (*github.CheckoutEvidence, error) {
	f.calls.Add(1)
	if f.afterRead != nil {
		f.afterRead()
	}
	if f.err != nil {
		return nil, f.err
	}
	content := "on: push\njobs:\n  build:\n    name: Build\n    runs-on: ubuntu-latest\n    steps:\n      - name: Checkout\n        uses: actions/checkout@" + strings.Repeat("b", 40) + "\n        with:\n          fetch-depth: 0\n"
	hash := func(s string) string { v := sha256.Sum256([]byte(s)); return hex.EncodeToString(v[:]) }
	blob := sha1.Sum([]byte("blob " + strconv.Itoa(len(content)) + "\x00" + content))
	ms := int64(4200)
	head := strings.Repeat("a", 40)
	return &github.CheckoutEvidence{Version: "checkout-evidence-v1", Basis: "push_source_unique_literal_names", Repository: repo, Selection: sel, StepNumber: 42, BlobSHA: hex.EncodeToString(blob[:]), JobNameSHA256: hash("Build"), StepNameSHA256: hash("Checkout"), Source: optimisation.Source{HeadSHA: head, File: prrepair.File{Path: ".github/workflows/private.yml", Mode: "100755", Content: content}}, Timing: optimisation.Timing{EvidenceID: fmt.Sprintf("checkout:%d:%d:%d:42", sel.RunID, sel.Attempt, sel.GitHubJobID), HeadSHA: head, SourceSHA256: hash(content), RunID: strconv.FormatInt(sel.RunID, 10), Attempt: sel.Attempt, JobIndex: sel.JobIndex, StepIndex: sel.StepIndex, Outcome: "success", CheckoutMS: &ms}}, nil
}
func optimisationAPIHarness(t *testing.T) (*harness, string, *optimisationAPIReader, map[string]any) {
	t.Helper()
	h := newHarness(t)
	inst := h.installation()
	f := &optimisationAPIReader{}
	c, e := controller.New(controller.Options{Store: h.st, Config: h.cfg, Key: h.key, Auth: h.ctrl.Auth(), GitHub: &optimisationAPIFactory{base: &fakeFactory{gh: h.gh}, reader: f}})
	if e != nil {
		t.Fatal(e)
	}
	h.ctrl = c
	h.api.ctrl = c
	return h, inst.ID, f, map[string]any{"installation_id": inst.ID, "repository": "acme/repo", "run_id": "9223372036854775807", "github_job_id": "9223372036854775807", "attempt": 2, "job_index": 0, "step_index": 0}
}
func enableOptimisation(h *harness, source, preview, history bool) {
	h.ctrl.UpdateConfig(func(c *config.Config) {
		c.Assistant.OptimisationSourceEnabled = source
		c.Assistant.OptimisationPreviewEnabled = preview
		c.Assistant.OptimisationHistoryEnabled = history
	})
}
func optimisationReview(t *testing.T, h *harness, token string, body map[string]any) OptimisationView {
	t.Helper()
	r := h.do(request{method: "POST", path: "/api/v1/optimisations/checkout/inspect", token: token, body: body})
	r.mustStatus(t, 200, "inspect")
	var v OptimisationView
	if e := json.Unmarshal(r.body, &v); e != nil {
		t.Fatal(e)
	}
	body["reviewed_head_sha"] = v.HeadSHA
	body["reviewed_source_sha256"] = v.SourceSHA256
	body["history_not_required"] = true
	return v
}
func TestOptimisationWebInspectionCaptureAndReplayHaveDistinctSourcePermissions(t *testing.T) {
	h, inst, f, body := optimisationAPIHarness(t)
	token := h.token("operator", store.RoleOperator, "optimisations:inspect", "optimisations:write", "optimisations:read")
	h.do(request{method: "POST", path: "/api/v1/optimisations/checkout/inspect", token: token, body: body}).mustStatus(t, 409, "disabled source")
	if f.calls.Load() != 0 || f.quota.Load() != 0 {
		t.Fatal("disabled feature read source")
	}
	enableOptimisation(h, true, false, false)
	v := optimisationReview(t, h, token, body)
	if v.RunID != "9223372036854775807" || *v.GitHubJobID != "9223372036854775807" || v.Mode != "100755" || v.AfterSHA256 != nil || v.Before != nil {
		t.Fatal("inspection shape changed")
	}
	h.do(request{method: "POST", path: "/api/v1/optimisations/checkout", token: token, body: body}).mustStatus(t, 409, "preview disabled independently")
	if f.calls.Load() != 1 {
		t.Fatal("disabled save spent reads")
	}
	enableOptimisation(h, true, true, false)
	r := h.do(request{method: "POST", path: "/api/v1/optimisations/checkout", token: token, body: body})
	r.mustStatus(t, 201, "save")
	var saved OptimisationView
	if e := json.Unmarshal(r.body, &saved); e != nil {
		t.Fatal(e)
	}
	if saved.ReviewedBy == nil || saved.Verification != "inconclusive" || saved.Currentness != "not_checked" || saved.AfterSHA256 == nil || saved.Before == nil || !strings.Contains(saved.Content, "fetch-depth: 1") || r.header.Get("Cache-Control") != "no-store" {
		t.Fatal("unsafe public preview shape")
	}
	path := "/api/v1/optimisations/" + saved.ID + "?installation_id=" + inst + "&repo=acme%2Frepo"
	h.do(request{method: "GET", path: path, token: token}).mustStatus(t, 409, "history disabled independently")
	enableOptimisation(h, false, false, true)
	replay := h.do(request{method: "GET", path: path, token: token})
	replay.mustStatus(t, 200, "offline history")
	if f.calls.Load() != 2 || f.quota.Load() != 2 || string(replay.body) != string(r.body) {
		t.Fatal("replay enriched history")
	}
	h.do(request{method: "GET", path: strings.Replace(path, "acme%2Frepo", "other%2Frepo", 1), token: token}).mustStatus(t, 404, "scoped miss")
}
func TestOptimisationWebRefusesViewersWrongScopesAndPolicyWithdrawalBeforeSaving(t *testing.T) {
	h, _, f, body := optimisationAPIHarness(t)
	enableOptimisation(h, true, true, true)
	for _, token := range []string{h.token("viewer", store.RoleViewer, "*"), h.token("jobs", store.RoleOperator, "jobs:read")} {
		h.do(request{method: "POST", path: "/api/v1/optimisations/checkout/inspect", token: token, body: body}).mustStatus(t, 403, "source boundary")
	}
	token := h.token("operator", store.RoleOperator, "optimisations:inspect", "optimisations:write", "optimisations:read")
	optimisationReview(t, h, token, body)
	f.afterRead = func() { enableOptimisation(h, true, false, true) }
	h.do(request{method: "POST", path: "/api/v1/optimisations/checkout", token: token, body: body}).mustStatus(t, 409, "policy withdrawn during read")
	_, total, e := h.st.ListAudit(h.ctx, store.AuditFilter{Actions: []string{"optimisation.preview_retained"}}, store.Page{Limit: 10})
	if e != nil || total != 0 {
		t.Fatal("withdrawn policy committed preview", e)
	}
}
func TestOptimisationWebRejectsUndocumentedAssertionsAndInexactIdentities(t *testing.T) {
	h, _, f, body := optimisationAPIHarness(t)
	enableOptimisation(h, true, true, true)
	token := h.token("operator", store.RoleOperator, "optimisations:inspect", "optimisations:write")
	for _, mode := range []string{"actor", "policy", "timing", "number", "leading_zero", "missing_index"} {
		copy := map[string]any{}
		for k, v := range body {
			copy[k] = v
		}
		switch mode {
		case "actor":
			copy["reviewed_by"] = "other"
		case "policy":
			copy["source_allowed"] = true
		case "timing":
			copy["checkout_ms"] = 1
		case "number":
			copy["run_id"] = 1
		case "leading_zero":
			copy["github_job_id"] = "01"
		case "missing_index":
			delete(copy, "job_index")
		}
		h.do(request{method: "POST", path: "/api/v1/optimisations/checkout/inspect", token: token, body: copy}).mustStatus(t, 400, mode)
	}
	if f.calls.Load() != 0 || f.quota.Load() != 0 {
		t.Fatal("invalid request spent reads")
	}
}
