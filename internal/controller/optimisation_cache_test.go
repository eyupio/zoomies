package controller

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
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

func cacheControllerFixture(t *testing.T) (*harness, *store.Installation, *cacheSourceReader, github.SetupCacheSelection, int64) {
	t.Helper()
	h, inst, _ := experimentJoinFixture(t)
	now := h.c.Now().UTC()
	workflow := "on: push\njobs:\n  build:\n    name: Build\n    runs-on: ubuntu-latest\n    steps:\n      - name: Checkout\n        uses: actions/checkout@" + strings.Repeat("a", 40) + "\n      - name: Node\n        uses: actions/setup-node@" + strings.Repeat("b", 40) + "\n        with:\n          node-version: 20\n"
	lockfile := `{"lockfileVersion":3}`
	source := &github.SetupCacheSource{
		Version: "setup-cache-evidence-v1", Basis: "push_head_root_lockfile_literal_names", Repository: inst.Target,
		RunID: "10", Attempt: 2, HeadSHA: strings.Repeat("a", 40), HeadBranch: "main",
		WorkflowPath: ".github/workflows/ci.yml", Workflow: prrepair.File{Path: ".github/workflows/ci.yml", Mode: "100644", Content: workflow}, WorkflowBlobSHA: cacheBlobSHA(workflow),
		LockfilePath: "package-lock.json", Lockfile: prrepair.File{Path: "package-lock.json", Mode: "100644", Content: lockfile}, LockfileBlobSHA: cacheBlobSHA(lockfile),
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
