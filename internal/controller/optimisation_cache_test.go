package controller

import (
	"context"
	"crypto/sha1"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/eyupio/zoomies/internal/config"
	"github.com/eyupio/zoomies/internal/github"
	"github.com/eyupio/zoomies/internal/optimisation"
	"github.com/eyupio/zoomies/internal/prrepair"
	"github.com/eyupio/zoomies/internal/store"
)

type cacheSourceReader struct {
	github.Client
	source          *github.SetupCacheSource
	quota           *github.RateLimit
	calls           int
	quotaCalls      int
	err             error
	publishes       int
	publishRequest  github.OptimisationPublishRequest
	publishPR       *github.PullRequest
	publishErr      error
	readinessSource *github.ReadinessSource
	readinessErr    error
}

func (f *cacheSourceReader) RateLimit(context.Context) (*github.RateLimit, error) {
	f.quotaCalls++
	return f.quota, nil
}
func (f *cacheSourceReader) ReadSetupCacheSource(context.Context, string, github.SetupCacheSelection) (*github.SetupCacheSource, error) {
	f.calls++
	return f.source, f.err
}
func (f *cacheSourceReader) ReadReadinessSource(context.Context, string, github.SetupCacheSelection) (*github.ReadinessSource, error) {
	f.calls++
	return f.readinessSource, f.readinessErr
}
func (f *cacheSourceReader) PublishOptimisation(_ context.Context, req github.OptimisationPublishRequest) (*github.PullRequest, error) {
	f.publishes++
	f.publishRequest = req
	return f.publishPR, f.publishErr
}

func cacheBlobSHA(s string) string {
	sum := sha1.Sum([]byte(fmt.Sprintf("blob %d\x00%s", len(s), s)))
	return hex.EncodeToString(sum[:])
}

func cacheSHA256(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}

func cacheControllerFixture(t *testing.T) (*harness, *store.Installation, *cacheSourceReader, github.SetupCacheSelection, int64) {
	t.Helper()
	h, inst, _ := experimentJoinFixture(t)
	now := h.c.Now().UTC()
	workflow := "on: push\njobs:\n  build:\n    name: Build\n    runs-on: ubuntu-latest\n    steps:\n      - name: Checkout\n        uses: actions/checkout@" + strings.Repeat("a", 40) + "\n      - name: Node\n        uses: actions/setup-node@" + strings.Repeat("b", 40) + "\n        with:\n          node-version: 20\n      - name: Wait for the API\n        run: sleep 4\n"
	lockfile := `{"lockfileVersion":3}`
	source := &github.SetupCacheSource{
		Version: "setup-cache-evidence-v1", Basis: "push_head_root_lockfile_literal_names", Repository: inst.Target,
		RunID: "10", Attempt: 2, HeadSHA: strings.Repeat("a", 40), HeadBranch: "main",
		WorkflowPath: ".github/workflows/ci.yml", Workflow: prrepair.File{Path: ".github/workflows/ci.yml", Mode: "100644", Content: workflow}, WorkflowBlobSHA: cacheBlobSHA(workflow), WorkflowSHA256: cacheSHA256(workflow),
		LockfilePath: "package-lock.json", Lockfile: prrepair.File{Path: "package-lock.json", Mode: "100644", Content: lockfile}, LockfileBlobSHA: cacheBlobSHA(lockfile), LockfileSHA256: cacheSHA256(lockfile),
		JobIndex: 0, StepIndex: 1, JobName: "Build", StepName: "Node",
	}
	steps := func(seconds int64) store.JobSteps {
		start, end := now.Add(-time.Duration(seconds+1)*time.Second), now.Add(-time.Second)
		return store.JobSteps{{Number: 1, Name: "Set up job", Status: "completed", Conclusion: "success", StartedAt: &start, CompletedAt: &start}, {Number: 2, Name: "Node", Status: "completed", Conclusion: "success", StartedAt: &start, CompletedAt: &end}, {Number: 3, Name: "Wait for the API", Status: "completed", Conclusion: "success", StartedAt: &start, CompletedAt: &end}}
	}
	if _, err := h.st.UpsertJob(h.ctx, &store.Job{GitHubJobID: 100, GitHubRunID: 10, Repo: inst.Target, Workflow: "CI", JobName: "Build", State: store.JobCompleted, Conclusion: "success", InstallationID: inst.ID, HeadBranch: "main", HeadSHA: source.HeadSHA, RunAttempt: 2, QueuedAt: now.Add(-time.Minute), CompletedAt: &now, Steps: steps(4)}); err != nil {
		t.Fatal(err)
	}
	if _, err := h.st.UpsertJob(h.ctx, &store.Job{GitHubJobID: 101, GitHubRunID: 11, Repo: inst.Target, Workflow: "CI", JobName: "Build", State: store.JobCompleted, Conclusion: "success", InstallationID: inst.ID, HeadBranch: "main", HeadSHA: source.HeadSHA, RunAttempt: 1, QueuedAt: now.Add(-2 * time.Minute), CompletedAt: &now, Steps: steps(6)}); err != nil {
		t.Fatal(err)
	}
	reader := &cacheSourceReader{Client: h.gh.Client(inst.Target, inst.TargetType), source: source, quota: &github.RateLimit{Limit: 5000, Remaining: 5000, ResetAt: now.Add(time.Hour)}}
	reader.readinessSource = &github.ReadinessSource{Version: "readiness-evidence-v1", Basis: "push_head_root_workflow_literal_names", Repository: inst.Target, RunID: "10", Attempt: 2, HeadSHA: source.HeadSHA, HeadBranch: source.HeadBranch, WorkflowPath: source.WorkflowPath, Workflow: source.Workflow, WorkflowBlobSHA: source.WorkflowBlobSHA, WorkflowSHA256: source.WorkflowSHA256, JobIndex: 0, StepIndex: 2, JobName: "Build", StepName: "Wait for the API", WaitSeconds: 4}
	h.c.clients.entries[inst.ID] = &clientEntry{client: reader, updatedAt: inst.UpdatedAt.Truncate(time.Millisecond)}
	h.c.UpdateConfig(func(c *config.Config) { c.Optimisation.SourceReads, c.Optimisation.WorkflowEdits = true, true })
	return h, inst, reader, github.SetupCacheSelection{RunID: 10, Attempt: 2}, 100
}

func TestInspectCachePreviewBindsOneJobToTheSourceAndItsHistory(t *testing.T) {
	h, inst, reader, selection, jobID := cacheControllerFixture(t)
	p, err := h.c.InspectCachePreview(h.ctx, inst.ID, inst.Target, selection, jobID)
	if err != nil {
		t.Fatal(err)
	}
	if p.PackageManager != "npm" || p.Runs != 2 || p.SetupMS != 5000 || p.JobName != "Build" || p.StepName != "Node" || p.JobIndex != 0 || p.StepIndex != 1 || p.LockfilePath != "package-lock.json" || p.Verification != "inconclusive" {
		t.Fatalf("preview %+v", p)
	}
	if reader.calls != 1 || reader.quotaCalls != 1 {
		t.Fatalf("reads=%d quota=%d", reader.calls, reader.quotaCalls)
	}
	if _, total, err := h.st.ListAudit(h.ctx, store.AuditFilter{Actions: []string{"optimisation.preview_retained"}}, store.Page{Limit: 10}); err != nil || total != 0 {
		t.Fatal("inspection retained a preview")
	}
}

func TestInspectCachePreviewRefusesPolicyJobMismatchAndThinHistory(t *testing.T) {
	for _, mode := range []string{"policy", "job name", "step name", "run", "attempt", "history", "quota", "source"} {
		t.Run(mode, func(t *testing.T) {
			h, inst, reader, selection, jobID := cacheControllerFixture(t)
			switch mode {
			case "policy":
				h.c.UpdateConfig(func(c *config.Config) { c.Optimisation.WorkflowEdits = false })
			case "job name":
				job, _ := h.st.GetJobByGitHubID(h.ctx, jobID)
				job.JobName = "Other"
				if _, err := h.st.UpsertJob(h.ctx, job); err != nil {
					t.Fatal(err)
				}
			case "step name":
				job, _ := h.st.GetJobByGitHubID(h.ctx, jobID)
				job.Steps[1].Name = "Other"
				if _, err := h.st.UpsertJob(h.ctx, job); err != nil {
					t.Fatal(err)
				}
			case "run":
				selection.RunID = 99
			case "attempt":
				selection.Attempt = 3
			case "history":
				other, _ := h.st.GetJobByGitHubID(h.ctx, 101)
				other.Conclusion = "failure"
				if _, err := h.st.UpsertJob(h.ctx, other); err != nil {
					t.Fatal(err)
				}
			case "quota":
				reader.quota = &github.RateLimit{Limit: 100, Remaining: 0}
			case "source":
				reader.source = &github.SetupCacheSource{Version: "future"}
			}
			if p, err := h.c.InspectCachePreview(h.ctx, inst.ID, inst.Target, selection, jobID); err == nil || p != nil {
				t.Fatalf("accepted %+v, %v", p, err)
			}
			if mode == "policy" && reader.calls != 0 {
				t.Fatal("policy refusal read GitHub")
			}
		})
	}
}

func TestInspectCachePreviewRefusesAWrongScopeOrReaderFailure(t *testing.T) {
	h, inst, reader, selection, jobID := cacheControllerFixture(t)
	if _, err := h.c.InspectCachePreview(h.ctx, inst.ID, "acme/other", selection, jobID); err == nil {
		t.Fatal("wrong repository accepted")
	}
	reader.err = fmt.Errorf("source unavailable")
	if _, err := h.c.InspectCachePreview(h.ctx, inst.ID, inst.Target, selection, jobID); err == nil {
		t.Fatal("reader failure accepted")
	}
}

func TestCaptureCachePreviewRetainsExactlyWhatWasInspectedAndReplaysIt(t *testing.T) {
	h, inst, _, selection, jobID := cacheControllerFixture(t)
	preview, err := h.c.InspectCachePreview(h.ctx, inst.ID, inst.Target, selection, jobID)
	if err != nil {
		t.Fatal(err)
	}
	actor := &store.AuditEvent{ActorID: "usr_cache", ActorKind: "user"}
	retained, err := h.c.CaptureCachePreview(h.ctx, actor, inst.ID, inst.Target, selection, jobID, preview.HeadSHA, preview.SourceSHA256, preview.LockfileSHA256)
	if err != nil {
		t.Fatal(err)
	}
	if retained.ID == "" || retained.ReviewedBy != "usr_cache" || retained.Preview.PackageManager != "npm" || retained.Currentness != "not_checked" {
		t.Fatalf("retained %+v", retained)
	}
	replayed, err := h.c.ReplayCachePreview(h.ctx, inst.ID, inst.Target, retained.ID)
	if err != nil {
		t.Fatal(err)
	}
	a, _ := json.Marshal(retained.Preview)
	b, _ := json.Marshal(replayed.Preview)
	if string(a) != string(b) {
		t.Fatal("replay changed the cache preview")
	}
	if _, total, err := h.st.ListAudit(h.ctx, store.AuditFilter{Actions: []string{"optimisation.cache_preview_retained"}}, store.Page{Limit: 10}); err != nil || total != 1 {
		t.Fatalf("audit total=%d err=%v", total, err)
	}
	if _, err := h.c.CaptureCachePreview(h.ctx, actor, inst.ID, inst.Target, selection, jobID, preview.HeadSHA, strings.Repeat("f", 64), preview.LockfileSHA256); !errors.Is(err, ErrOptimisationCache) {
		t.Fatalf("moved source accepted: %v", err)
	}
}

func TestApproveCachePreviewRecordsTheExactPreviewWithoutReadingGitHub(t *testing.T) {
	h, inst, reader, selection, jobID := cacheControllerFixture(t)
	preview, err := h.c.InspectCachePreview(h.ctx, inst.ID, inst.Target, selection, jobID)
	if err != nil {
		t.Fatal(err)
	}
	actor := &store.AuditEvent{ActorID: "usr_cache", ActorKind: "user"}
	retained, err := h.c.CaptureCachePreview(h.ctx, actor, inst.ID, inst.Target, selection, jobID, preview.HeadSHA, preview.SourceSHA256, preview.LockfileSHA256)
	if err != nil {
		t.Fatal(err)
	}
	approver := &store.AuditEvent{ActorID: "usr_approver", ActorKind: "user"}
	approved, err := h.c.ApproveCachePreview(h.ctx, approver, inst.ID, inst.Target, retained.ID, preview.HeadSHA, preview.SourceSHA256)
	if err != nil {
		t.Fatal(err)
	}
	if approved.Approval == nil || approved.Approval.ApprovedBy != "usr_approver" || approved.Approval.HeadSHA != preview.HeadSHA || approved.Approval.SourceSHA256 != preview.SourceSHA256 || approved.Approval.PreviewSHA256 != retained.SHA256 {
		t.Fatalf("approval %+v", approved)
	}
	if reader.calls != 2 {
		t.Fatalf("approval read GitHub: %d", reader.calls)
	}
	replayed, err := h.c.ReplayCachePreview(h.ctx, inst.ID, inst.Target, retained.ID)
	if err != nil || replayed.Approval == nil || replayed.Approval.ApprovedBy != "usr_approver" {
		t.Fatalf("replay lost the approval: %+v %v", replayed, err)
	}
	if _, total, err := h.st.ListAudit(h.ctx, store.AuditFilter{Actions: []string{"optimisation.cache_preview_approved"}}, store.Page{Limit: 10}); err != nil || total != 1 {
		t.Fatalf("audit total=%d err=%v", total, err)
	}
	if _, err := h.c.ApproveCachePreview(h.ctx, approver, inst.ID, inst.Target, retained.ID, strings.Repeat("f", 40), preview.SourceSHA256); !errors.Is(err, ErrOptimisationCacheApproval) {
		t.Fatalf("wrong head accepted: %v", err)
	}
}

func TestReplayCachePreviewRefusesAnAlteredEnvelope(t *testing.T) {
	h, inst, _, selection, jobID := cacheControllerFixture(t)
	preview, err := h.c.InspectCachePreview(h.ctx, inst.ID, inst.Target, selection, jobID)
	if err != nil {
		t.Fatal(err)
	}
	actor := &store.AuditEvent{ActorID: "usr_cache", ActorKind: "user"}
	retained, err := h.c.CaptureCachePreview(h.ctx, actor, inst.ID, inst.Target, selection, jobID, preview.HeadSHA, preview.SourceSHA256, preview.LockfileSHA256)
	if err != nil {
		t.Fatal(err)
	}
	row, err := h.st.GetOptimisationCachePreview(h.ctx, inst.ID, inst.Target, retained.ID)
	if err != nil {
		t.Fatal(err)
	}
	var e optimisationCacheRetainedEnvelope
	if err := json.Unmarshal(row.Payload, &e); err != nil {
		t.Fatal(err)
	}
	e.Preview.AfterSHA256 = strings.Repeat("f", 64)
	row.ID = store.NewID(store.PrefixOptimisationCachePreview)
	row.Payload, _ = json.Marshal(e)
	if err := h.st.PutOptimisationCachePreview(h.ctx, row, actor); err != nil {
		t.Fatal(err)
	}
	if _, err := h.c.ReplayCachePreview(h.ctx, inst.ID, inst.Target, row.ID); !errors.Is(err, ErrOptimisationCache) {
		t.Fatalf("altered preview accepted: %v", err)
	}
}

func TestPublishCachePreviewOpensOneDraftFromTheApprovedBinding(t *testing.T) {
	h, inst, reader, selection, jobID := cacheControllerFixture(t)
	preview, err := h.c.InspectCachePreview(h.ctx, inst.ID, inst.Target, selection, jobID)
	if err != nil {
		t.Fatal(err)
	}
	actor := &store.AuditEvent{ActorID: "usr_cache", ActorKind: "user"}
	retained, err := h.c.CaptureCachePreview(h.ctx, actor, inst.ID, inst.Target, selection, jobID, preview.HeadSHA, preview.SourceSHA256, preview.LockfileSHA256)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := h.c.ApproveCachePreview(h.ctx, actor, inst.ID, inst.Target, retained.ID, preview.HeadSHA, preview.SourceSHA256); err != nil {
		t.Fatal(err)
	}
	reader.publishPR = &github.PullRequest{Number: 12, HTMLURL: "https://github.com/" + inst.Target + "/pull/12", Branch: "zoomies/cache-" + retained.ID}
	check := func(context.Context) (optimisation.Policy, error) {
		return optimisation.Policy{SourceAllowed: true, WorkflowEditsAllowed: true, PublishAllowed: true}, nil
	}
	published, err := h.c.PublishCachePreview(h.ctx, actor, inst.ID, inst.Target, retained.ID, preview.HeadSHA, preview.SourceSHA256, check)
	if err != nil {
		t.Fatal(err)
	}
	if published.Publication == nil || published.Publication.PullNumber != 12 || published.Publication.PullURL != reader.publishPR.HTMLURL || reader.publishes != 1 {
		t.Fatalf("publication %+v publishes=%d", published.Publication, reader.publishes)
	}
	req := reader.publishRequest
	if req.Path != preview.Edit.Path || req.Mode != preview.File.Mode || req.Content != preview.File.Content || req.SourceSHA256 != preview.SourceSHA256 || req.Base != "main" || req.BaseCommit != preview.HeadSHA || req.Head != "zoomies/cache-"+retained.ID || !strings.Contains(req.Body, retained.ID) || !strings.Contains(req.Body, "no saving") {
		t.Fatalf("publish request %+v", req)
	}
	if reader.calls != 3 {
		t.Fatalf("publish read GitHub %d times", reader.calls)
	}
	if _, total, err := h.st.ListAudit(h.ctx, store.AuditFilter{Actions: []string{"optimisation.cache_preview_published"}}, store.Page{Limit: 10}); err != nil || total != 1 {
		t.Fatalf("audit total=%d err=%v", total, err)
	}
	again, err := h.c.PublishCachePreview(h.ctx, actor, inst.ID, inst.Target, retained.ID, preview.HeadSHA, preview.SourceSHA256, check)
	if err != nil || again.Publication == nil || reader.publishes != 1 {
		t.Fatalf("repeat published again: %v %+v", err, again)
	}
}

func TestPublishCachePreviewRefusesWithoutApprovalOrWithAForeignBinding(t *testing.T) {
	h, inst, reader, selection, jobID := cacheControllerFixture(t)
	preview, err := h.c.InspectCachePreview(h.ctx, inst.ID, inst.Target, selection, jobID)
	if err != nil {
		t.Fatal(err)
	}
	actor := &store.AuditEvent{ActorID: "usr_cache", ActorKind: "user"}
	retained, err := h.c.CaptureCachePreview(h.ctx, actor, inst.ID, inst.Target, selection, jobID, preview.HeadSHA, preview.SourceSHA256, preview.LockfileSHA256)
	if err != nil {
		t.Fatal(err)
	}
	check := func(context.Context) (optimisation.Policy, error) {
		return optimisation.Policy{SourceAllowed: true, WorkflowEditsAllowed: true, PublishAllowed: true}, nil
	}
	if _, err := h.c.PublishCachePreview(h.ctx, actor, inst.ID, inst.Target, retained.ID, preview.HeadSHA, preview.SourceSHA256, check); !errors.Is(err, ErrOptimisationNotApproved) {
		t.Fatalf("unapproved publish: %v", err)
	}
	if reader.publishes != 0 {
		t.Fatal("unapproved publish reached GitHub")
	}
	if _, err := h.c.PublishCachePreview(h.ctx, actor, inst.ID, inst.Target, retained.ID, strings.Repeat("f", 40), preview.SourceSHA256, check); !errors.Is(err, ErrOptimisationCacheApproval) {
		t.Fatalf("foreign head publish: %v", err)
	}
}

func TestInspectReadinessPreviewBindsTheJobToTheOperatorsCondition(t *testing.T) {
	h, inst, reader, selection, jobID := cacheControllerFixture(t)
	condition := "curl -fsS http://127.0.0.1:8080/health"
	if !github.ValidReadinessSource(reader.readinessSource, inst.Target, selection) {
		t.Fatalf("source invalid: %+v", reader.readinessSource)
	}
	p, err := h.c.InspectReadinessPreview(h.ctx, inst.ID, inst.Target, selection, jobID, condition)
	if err != nil {
		t.Fatal(err)
	}
	if p.Kind != "bounded_readiness_poll" || p.WaitSeconds != 4 || p.JobName != "Build" || p.StepName != "Wait for the API" || p.Condition != condition || p.Runs != 2 || p.WaitMS != 5000 || !strings.Contains(p.Edit.After, "timeout 4 sh -c 'until "+condition+"; do sleep 1; done'") || p.Verification != "inconclusive" {
		t.Fatalf("preview %+v", p)
	}
	if reader.calls != 1 {
		t.Fatalf("reads=%d", reader.calls)
	}
	if _, err := h.c.InspectReadinessPreview(h.ctx, inst.ID, inst.Target, selection, jobID, " "); !errors.Is(err, ErrOptimisationReadiness) {
		t.Fatalf("empty condition: %v", err)
	}
}

func TestCaptureReadinessPreviewKeepsExactlyWhatWasInspectedAndReplaysIt(t *testing.T) {
	h, inst, reader, selection, jobID := cacheControllerFixture(t)
	condition := "curl -fsS http://127.0.0.1:8080/health"
	preview, err := h.c.InspectReadinessPreview(h.ctx, inst.ID, inst.Target, selection, jobID, condition)
	if err != nil {
		t.Fatal(err)
	}
	actor := &store.AuditEvent{ActorID: "usr_readiness", ActorKind: "user"}
	retained, err := h.c.CaptureReadinessPreview(h.ctx, actor, inst.ID, inst.Target, selection, jobID, condition, preview.HeadSHA, preview.SourceSHA256)
	if err != nil {
		t.Fatal(err)
	}
	if retained.ID == "" || retained.ReviewedBy != "usr_readiness" || retained.Currentness != "not_checked" || retained.Provenance == nil || retained.Provenance.Selection != selection || retained.Provenance.GitHubJobID != jobID || retained.Provenance.HeadBranch != "main" {
		t.Fatalf("retained %+v", retained)
	}
	if reader.calls != 2 {
		t.Fatalf("capture reads=%d", reader.calls)
	}
	replayed, err := h.c.ReplayReadinessPreview(h.ctx, inst.ID, inst.Target, retained.ID)
	if err != nil || replayed.Preview.Edit != preview.Edit || replayed.Preview.Condition != condition {
		t.Fatalf("replay %+v %v", replayed, err)
	}
	if _, total, err := h.st.ListAudit(h.ctx, store.AuditFilter{Actions: []string{"optimisation.readiness_preview_retained"}}, store.Page{Limit: 10}); err != nil || total != 1 {
		t.Fatalf("audit total=%d err=%v", total, err)
	}
	if _, err := h.c.CaptureReadinessPreview(h.ctx, actor, inst.ID, inst.Target, selection, jobID, condition, preview.HeadSHA, strings.Repeat("f", 64)); !errors.Is(err, ErrOptimisationReadiness) {
		t.Fatalf("moved source accepted: %v", err)
	}
}
