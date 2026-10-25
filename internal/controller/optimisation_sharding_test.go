package controller

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/eyupio/zoomies/internal/config"
	"github.com/eyupio/zoomies/internal/github"
	"github.com/eyupio/zoomies/internal/kennel/workflow"
	"github.com/eyupio/zoomies/internal/optimisation"
	"github.com/eyupio/zoomies/internal/prrepair"
	"github.com/eyupio/zoomies/internal/store"
)

const shardingControllerWorkflow = "on: push\njobs:\n  e2e:\n    name: End to end\n    runs-on: ubuntu-latest\n    steps:\n      - uses: actions/checkout@" + "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa" + "\n      - name: Browser tests\n        run: npx playwright test\n"

// shardControllerFixture is a fleet that ran the same job runs times: two
// seconds to set up the job, forty to install, six minutes of browser tests.
func shardControllerFixture(t *testing.T, runs int) (*harness, *store.Installation, *cacheSourceReader, github.SetupCacheSelection, int64) {
	t.Helper()
	h, inst, _ := experimentJoinFixture(t)
	now := h.c.Now().UTC()
	file := prrepair.File{Path: ".github/workflows/ci.yml", Mode: "100644", Content: shardingControllerWorkflow}
	source := &github.ShardSource{
		Version: "shard-evidence-v1", Basis: "push_head_root_workflow_literal_names", Repository: inst.Target,
		RunID: "10", Attempt: 2, HeadSHA: strings.Repeat("a", 40), HeadBranch: "main",
		WorkflowPath: file.Path, Workflow: file, WorkflowBlobSHA: cacheBlobSHA(file.Content), WorkflowSHA256: cacheSHA256(file.Content),
		JobIndex: 0, StepIndex: 1, JobName: "End to end", StepName: "Browser tests",
	}
	steps := func() store.JobSteps {
		begin := now.Add(-402 * time.Second)
		return store.JobSteps{
			{Number: 1, Name: "Set up job", Status: "completed", Conclusion: "success", StartedAt: &begin, CompletedAt: ptr(begin.Add(2 * time.Second))},
			{Number: 2, Name: "Install", Status: "completed", Conclusion: "success", StartedAt: ptr(begin.Add(2 * time.Second)), CompletedAt: ptr(begin.Add(42 * time.Second))},
			{Number: 3, Name: "Browser tests", Status: "completed", Conclusion: "success", StartedAt: ptr(begin.Add(42 * time.Second)), CompletedAt: ptr(begin.Add(402 * time.Second))},
		}
	}
	for i := 0; i < runs; i++ {
		id := int64(100 + i)
		run := int64(10)
		attempt := 2
		if i > 0 {
			run, attempt = int64(10+i), 1
		}
		if _, err := h.st.UpsertJob(h.ctx, &store.Job{GitHubJobID: id, GitHubRunID: run, Repo: inst.Target, Workflow: "CI", JobName: "End to end", Labels: store.StringSlice{"ubuntu-latest"},
			State: store.JobCompleted, Conclusion: "success", InstallationID: inst.ID, HeadBranch: "main", HeadSHA: source.HeadSHA, RunAttempt: attempt,
			QueuedAt: now.Add(-10 * time.Minute), CompletedAt: &now, Steps: steps()}); err != nil {
			t.Fatal(err)
		}
	}
	reader := &cacheSourceReader{Client: h.gh.Client(inst.Target, inst.TargetType), shardSource: source, quota: &github.RateLimit{Limit: 5000, Remaining: 5000, ResetAt: now.Add(time.Hour)}}
	h.c.clients.entries[inst.ID] = &clientEntry{client: reader, updatedAt: inst.UpdatedAt.Truncate(time.Millisecond)}
	h.c.UpdateConfig(func(c *config.Config) { c.Optimisation.SourceReads, c.Optimisation.WorkflowEdits = true, true })
	h.pool(inst, "browsers", "self-hosted", "linux", "ubuntu-latest")
	h.host("browser-host")
	return h, inst, reader, github.SetupCacheSelection{RunID: 10, Attempt: 2}, 100
}

func TestInspectShardingPreviewSizesTheShardsFromTheFleetsOwnHistoryAndHeadroom(t *testing.T) {
	h, inst, reader, selection, jobID := shardControllerFixture(t, 12)
	inspection, err := h.c.inspectShardingPreview(h.ctx, inst.ID, inst.Target, selection, jobID)
	if err != nil {
		t.Fatal(err)
	}
	p, e := inspection.Preview, inspection.Evidence
	if e.Runs != 12 || e.TestMS != 360_000 || e.SetupMS != 42_000 || e.Headroom != 4 || len(inspection.Samples) != 12 {
		t.Fatalf("evidence %+v", e)
	}
	// Four shards at 90s each is the most the floor allows, and the pool has room for four.
	if p.Shards != 4 || p.JobName != "End to end" || p.StepName != "Browser tests" || p.Verification != "inconclusive" || !strings.Contains(p.Edit.After, "--shard=${{ matrix.shard }}/4") {
		t.Fatalf("preview %+v", p)
	}
	if reader.calls != 1 || reader.quotaCalls != 1 {
		t.Fatalf("reads=%d quota=%d", reader.calls, reader.quotaCalls)
	}
	again, err := h.c.inspectShardingPreview(h.ctx, inst.ID, inst.Target, selection, jobID)
	if err != nil || again.Evidence.EvidenceID != e.EvidenceID {
		t.Fatalf("the same history gave a different evidence ID: %v", err)
	}
}

func TestInspectShardingPreviewNeverAsksForMoreRunnersThanThePoolHas(t *testing.T) {
	h, inst, _, selection, jobID := shardControllerFixture(t, 12)
	pool := shardPool(t, h)
	pool.MaxRunners = 2
	if err := h.st.UpdatePool(h.ctx, pool); err != nil {
		t.Fatal(err)
	}
	inspection, err := h.c.inspectShardingPreview(h.ctx, inst.ID, inst.Target, selection, jobID)
	if err != nil || inspection.Preview.Shards != 2 || inspection.Evidence.Headroom != 2 {
		t.Fatalf("shards=%d headroom=%d err=%v", inspection.Preview.Shards, inspection.Evidence.Headroom, err)
	}
	pool.MaxRunners = 1
	if err := h.st.UpdatePool(h.ctx, pool); err != nil {
		t.Fatal(err)
	}
	_, err = h.c.inspectShardingPreview(h.ctx, inst.ID, inst.Target, selection, jobID)
	var refusal *optimisation.ShardingRefusal
	if !errors.As(err, &refusal) || !strings.Contains(refusal.Reason, "queue behind each other") {
		t.Fatalf("a pool with room for one runner: %v", err)
	}
}

func TestInspectShardingPreviewRefusesWithoutEnoughHistoryOrAPool(t *testing.T) {
	h, inst, _, selection, jobID := shardControllerFixture(t, 9)
	if _, err := h.c.inspectShardingPreview(h.ctx, inst.ID, inst.Target, selection, jobID); !errors.Is(err, optimisation.ErrShardingUnsupported) {
		t.Fatalf("nine runs: %v", err)
	}
	h2, inst2, _, selection2, jobID2 := shardControllerFixture(t, 12)
	pool := shardPool(t, h2)
	pool.Labels = store.StringSlice{"something-else"}
	if err := h2.st.UpdatePool(h2.ctx, pool); err != nil {
		t.Fatal(err)
	}
	if _, err := h2.c.inspectShardingPreview(h2.ctx, inst2.ID, inst2.Target, selection2, jobID2); !errors.Is(err, optimisation.ErrShardingUnsupported) {
		t.Fatalf("no pool for the job's labels: %v", err)
	}
}

func TestInspectShardingPreviewRefusesAMovedJobOrPolicyOff(t *testing.T) {
	h, inst, reader, selection, jobID := shardControllerFixture(t, 12)
	if _, err := h.c.inspectShardingPreview(h.ctx, inst.ID, inst.Target, selection, jobID+1000); err == nil {
		t.Fatal("an unknown job was accepted")
	}
	reader.shardSource.JobName = "Another job"
	if _, err := h.c.inspectShardingPreview(h.ctx, inst.ID, inst.Target, selection, jobID); err == nil {
		t.Fatal("a source naming a different job was accepted")
	}
	reader.shardSource.JobName = "End to end"
	h.c.UpdateConfig(func(c *config.Config) { c.Optimisation.WorkflowEdits = false })
	if _, err := h.c.inspectShardingPreview(h.ctx, inst.ID, inst.Target, selection, jobID); !errors.Is(err, ErrOptimisationPolicy) {
		t.Fatalf("policy off: %v", err)
	}
}

func shardPool(t *testing.T, h *harness) *store.Pool {
	t.Helper()
	pools, err := h.st.ListPools(h.ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range pools {
		if strings.HasSuffix(p.Name, "browsers") {
			return p
		}
	}
	t.Fatal("no browsers pool")
	return nil
}

func TestInspectShardingPreviewTellsTheOperatorWhatToChangeInTheWorkflow(t *testing.T) {
	h, inst, reader, selection, jobID := shardControllerFixture(t, 12)
	reader.readinessErr = fmt.Errorf("github: sharding preview needs one shardable Playwright step: %w", &workflow.ShardRefusal{Reason: "the command already shards, filters or narrows the run (--grep)"})
	_, err := h.c.inspectShardingPreview(h.ctx, inst.ID, inst.Target, selection, jobID)
	var refusal *optimisation.ShardingRefusal
	if !errors.As(err, &refusal) || !strings.Contains(refusal.Reason, "--grep") {
		t.Fatalf("the workflow's reason was lost: %v", err)
	}
}

func TestCaptureShardingPreviewKeepsExactlyWhatWasInspectedAndReplaysIt(t *testing.T) {
	h, inst, reader, selection, jobID := shardControllerFixture(t, 12)
	preview, err := h.c.InspectShardingPreview(h.ctx, inst.ID, inst.Target, selection, jobID)
	if err != nil {
		t.Fatal(err)
	}
	actor := &store.AuditEvent{ActorID: "usr_sharding", ActorKind: "user"}
	retained, err := h.c.CaptureShardingPreview(h.ctx, actor, inst.ID, inst.Target, selection, jobID, preview.HeadSHA, preview.SourceSHA256, preview.Shards)
	if err != nil {
		t.Fatal(err)
	}
	if retained.ID == "" || !strings.HasPrefix(retained.ID, "ops_") || retained.ReviewedBy != "usr_sharding" || retained.Currentness != "not_checked" || retained.Provenance.Selection != selection || retained.Provenance.GitHubJobID != jobID || retained.Provenance.HeadBranch != "main" {
		t.Fatalf("retained %+v", retained)
	}
	if reader.calls != 2 {
		t.Fatalf("capture reads=%d", reader.calls)
	}
	replayed, err := h.c.ReplayShardingPreview(h.ctx, inst.ID, inst.Target, retained.ID)
	if err != nil || replayed.Preview.Edit != preview.Edit || replayed.Preview.Shards != preview.Shards || replayed.Preview.Headroom != preview.Headroom {
		t.Fatalf("replay %+v %v", replayed, err)
	}
	if reader.calls != 2 {
		t.Fatalf("replay read GitHub: %d", reader.calls)
	}
	if _, total, err := h.st.ListAudit(h.ctx, store.AuditFilter{Actions: []string{"optimisation.sharding_preview_retained"}}, store.Page{Limit: 10}); err != nil || total != 1 {
		t.Fatalf("audit total=%d err=%v", total, err)
	}
}

func TestCaptureShardingPreviewRefusesWhatIsNotWhatWasReviewed(t *testing.T) {
	h, inst, _, selection, jobID := shardControllerFixture(t, 12)
	preview, err := h.c.InspectShardingPreview(h.ctx, inst.ID, inst.Target, selection, jobID)
	if err != nil {
		t.Fatal(err)
	}
	actor := &store.AuditEvent{ActorID: "usr_sharding", ActorKind: "user"}
	for name, c := range map[string]struct {
		head, source string
		shards       int
	}{
		"moved source":       {preview.HeadSHA, strings.Repeat("f", 64), preview.Shards},
		"moved head":         {strings.Repeat("f", 40), preview.SourceSHA256, preview.Shards},
		"different count":    {preview.HeadSHA, preview.SourceSHA256, preview.Shards - 1},
		"count out of range": {preview.HeadSHA, preview.SourceSHA256, 9},
	} {
		if _, err := h.c.CaptureShardingPreview(h.ctx, actor, inst.ID, inst.Target, selection, jobID, c.head, c.source, c.shards); !errors.Is(err, ErrOptimisationSharding) {
			t.Errorf("%s accepted: %v", name, err)
		}
	}
	if _, total, _ := h.st.ListAudit(h.ctx, store.AuditFilter{Actions: []string{"optimisation.sharding_preview_retained"}}, store.Page{Limit: 10}); total != 0 {
		t.Fatalf("a refused capture was retained: %d", total)
	}
}

func TestReplayShardingPreviewRefusesAnAlteredEnvelope(t *testing.T) {
	h, inst, _, selection, jobID := shardControllerFixture(t, 12)
	preview, err := h.c.InspectShardingPreview(h.ctx, inst.ID, inst.Target, selection, jobID)
	if err != nil {
		t.Fatal(err)
	}
	actor := &store.AuditEvent{ActorID: "usr_sharding", ActorKind: "user"}
	retained, err := h.c.CaptureShardingPreview(h.ctx, actor, inst.ID, inst.Target, selection, jobID, preview.HeadSHA, preview.SourceSHA256, preview.Shards)
	if err != nil {
		t.Fatal(err)
	}
	row, err := h.st.GetOptimisationShardingPreview(h.ctx, inst.ID, inst.Target, retained.ID)
	if err != nil {
		t.Fatal(err)
	}
	for name, alter := range map[string]func(*optimisationShardingRetainedEnvelope){
		"after digest":   func(e *optimisationShardingRetainedEnvelope) { e.Preview.AfterSHA256 = strings.Repeat("f", 64) },
		"shard count":    func(e *optimisationShardingRetainedEnvelope) { e.Preview.Shards = 2 },
		"headroom":       func(e *optimisationShardingRetainedEnvelope) { e.Evidence.Headroom = 8 },
		"a sample":       func(e *optimisationShardingRetainedEnvelope) { e.Samples[0].TestMS = 1 },
		"no provenance":  func(e *optimisationShardingRetainedEnvelope) { e.Provenance.GitHubJobID = 0 },
		"another branch": func(e *optimisationShardingRetainedEnvelope) { e.Provenance.HeadBranch = "bad branch name" },
	} {
		var e optimisationShardingRetainedEnvelope
		if err := json.Unmarshal(row.Payload, &e); err != nil {
			t.Fatal(err)
		}
		alter(&e)
		copyRow := *row
		copyRow.ID = store.NewID(store.PrefixOptimisationShardingPreview)
		e.ID = copyRow.ID
		copyRow.Payload, _ = json.Marshal(e)
		if err := h.st.PutOptimisationShardingPreview(h.ctx, &copyRow, actor); err != nil {
			t.Fatal(err)
		}
		if _, err := h.c.ReplayShardingPreview(h.ctx, inst.ID, inst.Target, copyRow.ID); !errors.Is(err, ErrOptimisationSharding) {
			t.Errorf("%s: altered preview accepted: %v", name, err)
		}
	}
}

func TestApproveShardingPreviewRecordsTheExactPreviewWithoutReadingGitHub(t *testing.T) {
	h, inst, reader, selection, jobID := shardControllerFixture(t, 12)
	preview, err := h.c.InspectShardingPreview(h.ctx, inst.ID, inst.Target, selection, jobID)
	if err != nil {
		t.Fatal(err)
	}
	actor := &store.AuditEvent{ActorID: "usr_sharding", ActorKind: "user"}
	retained, err := h.c.CaptureShardingPreview(h.ctx, actor, inst.ID, inst.Target, selection, jobID, preview.HeadSHA, preview.SourceSHA256, preview.Shards)
	if err != nil {
		t.Fatal(err)
	}
	approver := &store.AuditEvent{ActorID: "usr_approver", ActorKind: "user"}
	approved, err := h.c.ApproveShardingPreview(h.ctx, approver, inst.ID, inst.Target, retained.ID, preview.HeadSHA, preview.SourceSHA256)
	if err != nil {
		t.Fatal(err)
	}
	if approved.Approval == nil || approved.Approval.ApprovedBy != "usr_approver" || approved.Approval.HeadSHA != preview.HeadSHA || approved.Approval.SourceSHA256 != preview.SourceSHA256 || approved.Approval.PreviewSHA256 != retained.SHA256 {
		t.Fatalf("approval %+v", approved)
	}
	if reader.calls != 2 {
		t.Fatalf("approval read GitHub: %d", reader.calls)
	}
	replayed, err := h.c.ReplayShardingPreview(h.ctx, inst.ID, inst.Target, retained.ID)
	if err != nil || replayed.Approval == nil || replayed.Approval.ApprovedBy != "usr_approver" {
		t.Fatalf("replay lost the approval: %+v %v", replayed, err)
	}
	if _, total, err := h.st.ListAudit(h.ctx, store.AuditFilter{Actions: []string{"optimisation.sharding_preview_approved"}}, store.Page{Limit: 10}); err != nil || total != 1 {
		t.Fatalf("audit total=%d err=%v", total, err)
	}
	if _, err := h.c.ApproveShardingPreview(h.ctx, approver, inst.ID, inst.Target, retained.ID, strings.Repeat("f", 40), preview.SourceSHA256); !errors.Is(err, ErrOptimisationShardingApproval) {
		t.Fatalf("wrong head accepted: %v", err)
	}
	if _, err := h.c.ApproveShardingPreview(h.ctx, approver, inst.ID, inst.Target, retained.ID, preview.HeadSHA, strings.Repeat("f", 64)); !errors.Is(err, ErrOptimisationShardingApproval) {
		t.Fatalf("wrong source accepted: %v", err)
	}
}

func shardingPublishFixture(t *testing.T) (*harness, *store.Installation, *cacheSourceReader, *RetainedShardingPreview, *store.AuditEvent, OptimisationPolicyCheck) {
	t.Helper()
	h, inst, reader, selection, jobID := shardControllerFixture(t, 12)
	preview, err := h.c.InspectShardingPreview(h.ctx, inst.ID, inst.Target, selection, jobID)
	if err != nil {
		t.Fatal(err)
	}
	actor := &store.AuditEvent{ActorID: "usr_sharding", ActorKind: "user"}
	retained, err := h.c.CaptureShardingPreview(h.ctx, actor, inst.ID, inst.Target, selection, jobID, preview.HeadSHA, preview.SourceSHA256, preview.Shards)
	if err != nil {
		t.Fatal(err)
	}
	reader.publishPR = &github.PullRequest{Number: 12, HTMLURL: "https://github.com/" + inst.Target + "/pull/12", Branch: "zoomies/sharding-" + retained.ID}
	check := func(context.Context) (optimisation.Policy, error) {
		return optimisation.Policy{SourceAllowed: true, WorkflowEditsAllowed: true, PublishAllowed: true}, nil
	}
	return h, inst, reader, retained, actor, check
}

func approveShardingForTest(t *testing.T, h *harness, inst *store.Installation, retained *RetainedShardingPreview, actor *store.AuditEvent) {
	t.Helper()
	if _, err := h.c.ApproveShardingPreview(h.ctx, actor, inst.ID, inst.Target, retained.ID, retained.Preview.HeadSHA, retained.Preview.SourceSHA256); err != nil {
		t.Fatal(err)
	}
}

func TestPublishShardingPreviewOpensOneDraftOfTheApprovedChange(t *testing.T) {
	h, inst, reader, retained, actor, check := shardingPublishFixture(t)
	approveShardingForTest(t, h, inst, retained, actor)
	p := retained.Preview
	published, err := h.c.PublishShardingPreview(h.ctx, actor, inst.ID, inst.Target, retained.ID, p.HeadSHA, p.SourceSHA256, check)
	if err != nil {
		t.Fatal(err)
	}
	if published.Publication == nil || published.Publication.PullNumber != 12 || published.Publication.PullURL != reader.publishPR.HTMLURL || reader.publishes != 1 {
		t.Fatalf("publication %+v publishes=%d", published.Publication, reader.publishes)
	}
	req := reader.publishRequest
	if req.Path != p.Edit.Path || req.Mode != p.File.Mode || req.Content != p.File.Content || req.SourceSHA256 != p.SourceSHA256 || req.Base != "main" || req.BaseCommit != p.HeadSHA || req.Head != "zoomies/sharding-"+retained.ID {
		t.Fatalf("publish request %+v", req)
	}
	for _, want := range []string{"Zoomies will not merge", "costs more runner time, not less", "estimate from medians", "A job with the original name stays behind", "needs 4 runners at once"} {
		if !strings.Contains(req.Body, want) {
			t.Errorf("the pull request body does not say %q:\n%s", want, req.Body)
		}
	}
	if !strings.Contains(req.Content, "--shard=${{ matrix.shard }}/4") {
		t.Fatal("the published file is not the approved change")
	}
	if reader.calls != 3 {
		t.Fatalf("publish read GitHub %d times", reader.calls)
	}
	if _, total, err := h.st.ListAudit(h.ctx, store.AuditFilter{Actions: []string{"optimisation.sharding_preview_published"}}, store.Page{Limit: 10}); err != nil || total != 1 {
		t.Fatalf("audit total=%d err=%v", total, err)
	}
	calls := reader.calls
	again, err := h.c.PublishShardingPreview(h.ctx, actor, inst.ID, inst.Target, retained.ID, p.HeadSHA, p.SourceSHA256, check)
	if err != nil || again.Publication == nil || reader.publishes != 1 || reader.calls != calls {
		t.Fatalf("repeat wrote or read again: %v %+v publishes=%d", err, again, reader.publishes)
	}
}

func TestPublishShardingPreviewRefusesWithoutApprovalOrWithAForeignBinding(t *testing.T) {
	h, inst, reader, retained, actor, check := shardingPublishFixture(t)
	p := retained.Preview
	if _, err := h.c.PublishShardingPreview(h.ctx, actor, inst.ID, inst.Target, retained.ID, p.HeadSHA, p.SourceSHA256, check); !errors.Is(err, ErrOptimisationNotApproved) {
		t.Fatalf("unapproved publish: %v", err)
	}
	approveShardingForTest(t, h, inst, retained, actor)
	if _, err := h.c.PublishShardingPreview(h.ctx, actor, inst.ID, inst.Target, retained.ID, strings.Repeat("f", 40), p.SourceSHA256, check); !errors.Is(err, ErrOptimisationShardingApproval) {
		t.Fatalf("foreign head publish: %v", err)
	}
	if reader.publishes != 0 {
		t.Fatal("a refused publish reached GitHub")
	}
}

func TestPublishShardingPreviewRefusesWhatMovedSinceTheReview(t *testing.T) {
	for name, move := range map[string]func(*harness, *cacheSourceReader){
		"head":   func(_ *harness, r *cacheSourceReader) { r.shardSource.HeadSHA = strings.Repeat("c", 40) },
		"branch": func(_ *harness, r *cacheSourceReader) { r.shardSource.HeadBranch = "release" },
		"workflow": func(_ *harness, r *cacheSourceReader) {
			w := r.shardSource.Workflow
			w.Content += "# edited\n"
			r.shardSource.Workflow = w
			r.shardSource.WorkflowSHA256 = cacheSHA256(w.Content)
		},
		"pool no longer supports the count": func(h *harness, _ *cacheSourceReader) {
			pool := shardPool(t, h)
			pool.MaxRunners = 2
			if err := h.st.UpdatePool(h.ctx, pool); err != nil {
				t.Fatal(err)
			}
		},
	} {
		t.Run(name, func(t *testing.T) {
			h, inst, reader, retained, actor, check := shardingPublishFixture(t)
			approveShardingForTest(t, h, inst, retained, actor)
			move(h, reader)
			p := retained.Preview
			_, err := h.c.PublishShardingPreview(h.ctx, actor, inst.ID, inst.Target, retained.ID, p.HeadSHA, p.SourceSHA256, check)
			if err == nil || reader.publishes != 0 {
				t.Fatalf("published after %s moved: err=%v publishes=%d", name, err, reader.publishes)
			}
			if got, replayErr := h.c.ReplayShardingPreview(h.ctx, inst.ID, inst.Target, retained.ID); replayErr != nil || got.Publication != nil {
				t.Fatalf("a refused publish left a record: %+v %v", got, replayErr)
			}
		})
	}
}

func TestPublishShardingPreviewRefusesAWithdrawnSettingAndMissingPermission(t *testing.T) {
	h, inst, reader, retained, actor, check := shardingPublishFixture(t)
	approveShardingForTest(t, h, inst, retained, actor)
	p := retained.Preview
	off := func(context.Context) (optimisation.Policy, error) {
		return optimisation.Policy{SourceAllowed: true, WorkflowEditsAllowed: true}, nil
	}
	if _, err := h.c.PublishShardingPreview(h.ctx, actor, inst.ID, inst.Target, retained.ID, p.HeadSHA, p.SourceSHA256, off); !errors.Is(err, ErrOptimisationPublishPolicy) {
		t.Fatalf("publish setting off: %v", err)
	}
	calls := 0
	flipping := func(context.Context) (optimisation.Policy, error) {
		calls++
		return optimisation.Policy{SourceAllowed: true, WorkflowEditsAllowed: true, PublishAllowed: calls < 2}, nil
	}
	if _, err := h.c.PublishShardingPreview(h.ctx, actor, inst.ID, inst.Target, retained.ID, p.HeadSHA, p.SourceSHA256, flipping); !errors.Is(err, ErrOptimisationPublishPolicy) {
		t.Fatalf("setting withdrawn mid-flight: %v", err)
	}
	reader.probeInfo = &github.AppInfo{Permissions: map[string]string{"contents": "read"}}
	if _, err := h.c.PublishShardingPreview(h.ctx, actor, inst.ID, inst.Target, retained.ID, p.HeadSHA, p.SourceSHA256, check); !errors.Is(err, ErrOptimisationPublishPermission) {
		t.Fatalf("missing permission: %v", err)
	}
	if reader.publishes != 0 {
		t.Fatal("a refused publish reached GitHub")
	}
}

func TestPublishShardingPreviewKeepsADifferentExistingDraftOutOfTheRecord(t *testing.T) {
	h, inst, reader, retained, actor, check := shardingPublishFixture(t)
	approveShardingForTest(t, h, inst, retained, actor)
	p := retained.Preview
	reader.publishErr = github.ErrOptimisationPublishConflict
	if _, err := h.c.PublishShardingPreview(h.ctx, actor, inst.ID, inst.Target, retained.ID, p.HeadSHA, p.SourceSHA256, check); !errors.Is(err, ErrOptimisationPublish) {
		t.Fatalf("conflicting draft: %v", err)
	}
	if got, err := h.c.ReplayShardingPreview(h.ctx, inst.ID, inst.Target, retained.ID); err != nil || got.Publication != nil {
		t.Fatalf("a conflicting draft was recorded: %+v %v", got, err)
	}
}
