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
	"github.com/eyupio/zoomies/internal/prrepair"
	"github.com/eyupio/zoomies/internal/store"
)

type cacheSourceReader struct {
	github.Client
	source     *github.SetupCacheSource
	quota      *github.RateLimit
	calls      int
	quotaCalls int
	err        error
}

func (f *cacheSourceReader) RateLimit(context.Context) (*github.RateLimit, error) {
	f.quotaCalls++
	return f.quota, nil
}
func (f *cacheSourceReader) ReadSetupCacheSource(context.Context, string, github.SetupCacheSelection) (*github.SetupCacheSource, error) {
	f.calls++
	return f.source, f.err
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
	workflow := "on: push\njobs:\n  build:\n    name: Build\n    runs-on: ubuntu-latest\n    steps:\n      - name: Checkout\n        uses: actions/checkout@" + strings.Repeat("a", 40) + "\n      - name: Node\n        uses: actions/setup-node@" + strings.Repeat("b", 40) + "\n        with:\n          node-version: 20\n"
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
		return store.JobSteps{{Number: 1, Name: "Set up job", Status: "completed", Conclusion: "success", StartedAt: &start, CompletedAt: &start}, {Number: 2, Name: "Node", Status: "completed", Conclusion: "success", StartedAt: &start, CompletedAt: &end}}
	}
	if _, err := h.st.UpsertJob(h.ctx, &store.Job{GitHubJobID: 100, GitHubRunID: 10, Repo: inst.Target, Workflow: "CI", JobName: "Build", State: store.JobCompleted, Conclusion: "success", InstallationID: inst.ID, HeadBranch: "main", HeadSHA: source.HeadSHA, RunAttempt: 2, QueuedAt: now.Add(-time.Minute), CompletedAt: &now, Steps: steps(4)}); err != nil {
		t.Fatal(err)
	}
	if _, err := h.st.UpsertJob(h.ctx, &store.Job{GitHubJobID: 101, GitHubRunID: 11, Repo: inst.Target, Workflow: "CI", JobName: "Build", State: store.JobCompleted, Conclusion: "success", InstallationID: inst.ID, HeadBranch: "main", HeadSHA: source.HeadSHA, RunAttempt: 1, QueuedAt: now.Add(-2 * time.Minute), CompletedAt: &now, Steps: steps(6)}); err != nil {
		t.Fatal(err)
	}
	reader := &cacheSourceReader{Client: h.gh.Client(inst.Target, inst.TargetType), source: source, quota: &github.RateLimit{Limit: 5000, Remaining: 5000, ResetAt: now.Add(time.Hour)}}
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
