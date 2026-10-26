package controller

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/eyupio/zoomies/internal/config"
	"github.com/eyupio/zoomies/internal/github"
	"github.com/eyupio/zoomies/internal/kennel"
	"github.com/eyupio/zoomies/internal/optimisation"
	"github.com/eyupio/zoomies/internal/prrepair"
	"github.com/eyupio/zoomies/internal/store"
)

const kennelFixWorkflow = "on: push\njobs:\n  build:\n    name: Build\n    runs-on: ubuntu-latest\n    steps:\n      - run: make\n"

type kennelFixReader struct {
	github.Client
	source         *github.KennelFixSource
	quota          *github.RateLimit
	err            error
	calls          int
	quotaCalls     int
	publishes      int
	publishRequest github.OptimisationPublishRequest
	publishPR      *github.PullRequest
}

func (f *kennelFixReader) RateLimit(context.Context) (*github.RateLimit, error) {
	f.quotaCalls++
	return f.quota, nil
}
func (f *kennelFixReader) ReadKennelFixSource(context.Context, string, string, string) (*github.KennelFixSource, error) {
	f.calls++
	return f.source, f.err
}
func (f *kennelFixReader) Probe(ctx context.Context) (*github.AppInfo, error) {
	return f.Client.Probe(ctx)
}
func (f *kennelFixReader) PublishOptimisation(_ context.Context, req github.OptimisationPublishRequest) (*github.PullRequest, error) {
	f.publishes++
	f.publishRequest = req
	return f.publishPR, nil
}

// kennelFixFixture is a tracked repository Kennel Club has evaluated, with one
// open ci.no_timeout finding, and a GitHub reader that serves the file the
// finding names.
func kennelFixFixture(t *testing.T) (*harness, *store.Installation, *kennelFixReader, KennelFixRequest) {
	t.Helper()
	h, inst, _ := experimentJoinFixture(t)
	now := h.c.Now().UTC()
	blob := cacheBlobSHA(kennelFixWorkflow)
	path := ".github/workflows/ci.yml"
	row, err := h.st.TouchKennelRepository(h.ctx, store.KennelRepositoryRef{GitHubHost: "github.com", RepositoryID: 77, InstallationID: inst.ID, FullName: inst.Target, Visibility: "private"})
	if err != nil {
		t.Fatal(err)
	}
	zero := 0
	finding := kennel.Finding{
		Code: kennel.CodeNoTimeout, Severity: kennel.SeverityWarning, Subject: blob[:12], Title: "Jobs have no explicit timeout",
		Evidence: []kennel.Evidence{{Kind: kennel.EvidenceFile, Ref: blob, JobIndex: &zero, Line: 3}},
	}
	h.saveKennelFixEvaluation(t, row.ID, kennel.Evaluation{Version: kennel.Version, Findings: []kennel.Finding{finding}}, blob, path)
	head := strings.Repeat("a", 40)
	reader := &kennelFixReader{
		Client: h.gh.Client(inst.Target, inst.TargetType),
		source: &github.KennelFixSource{
			Version: "kennel-fix-source-v1", Repository: inst.Target, Branch: "main", HeadSHA: head, Path: path, BlobSHA: blob,
			Workflow: prrepair.File{Path: path, Mode: "100644", Content: kennelFixWorkflow}, WorkflowSHA256: cacheSHA256(kennelFixWorkflow),
		},
		quota: &github.RateLimit{Limit: 5000, Remaining: 5000, ResetAt: now.Add(time.Hour)},
	}
	h.c.clients.entries[inst.ID] = &clientEntry{client: reader, updatedAt: inst.UpdatedAt.Truncate(time.Millisecond)}
	h.c.UpdateConfig(func(c *config.Config) {
		c.Kennel.Enabled = true
		c.Optimisation.SourceReads, c.Optimisation.WorkflowEdits = true, true
	})
	return h, inst, reader, KennelFixRequest{KennelRepositoryID: row.ID, BlobSHA: blob, JobIndex: 0, Minutes: 30}
}

func (h *harness) saveKennelFixEvaluation(t *testing.T, id string, ev kennel.Evaluation, blob, path string) {
	t.Helper()
	evJSON, err := json.Marshal(ev)
	if err != nil {
		t.Fatal(err)
	}
	wm := kennelWatermark{WorkflowFiles: []kennelWorkflowRef{{SHA: blob, Path: path}}}
	now := h.c.Now().UTC()
	if err := h.st.SaveKennelEvaluation(h.ctx, id, store.KennelEvaluationRecord{
		State: string(kennel.StateAttention), EvaluatorVersion: kennel.Version, EvaluatedAt: now, NextDueAt: now.Add(time.Hour),
		InputsDigest: "x", Coverage: json.RawMessage(`{}`), Evaluation: evJSON, Watermark: wm.marshal(), OpenWarnings: len(ev.Findings),
	}); err != nil {
		t.Fatal(err)
	}
}

var kennelFixActor = &store.AuditEvent{ActorID: "usr_fixer", ActorKind: "user"}

func allowPublish(context.Context) (optimisation.Policy, error) {
	return optimisation.Policy{SourceAllowed: true, WorkflowEditsAllowed: true, PublishAllowed: true}, nil
}

func TestInspectKennelFixBindsTheOpenFindingToTheSourceAndRetainsNothing(t *testing.T) {
	h, _, reader, req := kennelFixFixture(t)
	in, err := h.c.InspectKennelFix(h.ctx, req)
	if err != nil {
		t.Fatal(err)
	}
	p := in.Preview
	if p.CheckCode != "ci.no_timeout" || p.JobName != "Build" || p.Minutes != 30 || p.Verification != "recheck" || p.BlobSHA != req.BlobSHA || in.Finding.Code != kennel.CodeNoTimeout {
		t.Fatalf("preview %+v", p)
	}
	if !strings.Contains(p.File.Content, "    runs-on: ubuntu-latest\n    timeout-minutes: 30\n") {
		t.Fatalf("file %q", p.File.Content)
	}
	if reader.calls != 1 || reader.quotaCalls != 1 {
		t.Fatalf("reads=%d quota=%d", reader.calls, reader.quotaCalls)
	}
	if _, total, err := h.st.ListAudit(h.ctx, store.AuditFilter{Actions: []string{"kennel.fix_preview_retained"}}, store.Page{Limit: 10}); err != nil || total != 0 {
		t.Fatal("inspection retained a preview")
	}
}

func TestInspectKennelFixRefusesWhatIsNotAnOpenFindingTheOperatorMayFix(t *testing.T) {
	for _, mode := range []string{"kennel off", "untracked", "other blob", "other job", "minutes", "policy", "quota", "stale", "bad id"} {
		t.Run(mode, func(t *testing.T) {
			h, _, reader, req := kennelFixFixture(t)
			switch mode {
			case "kennel off":
				h.c.UpdateConfig(func(c *config.Config) { c.Kennel.Enabled = false })
			case "untracked":
				if _, err := h.st.UntrackKennelRepository(h.ctx, req.KennelRepositoryID, store.KennelUntracked{Reason: "not ours", By: "usr_x", ByName: "X"}); err != nil {
					t.Fatal(err)
				}
			case "other blob":
				req.BlobSHA = strings.Repeat("e", 40)
			case "other job":
				req.JobIndex = 3
			case "minutes":
				req.Minutes = 0
			case "policy":
				h.c.UpdateConfig(func(c *config.Config) { c.Optimisation.WorkflowEdits = false })
			case "quota":
				reader.quota = &github.RateLimit{Limit: 5000, Remaining: 10, ResetAt: h.c.Now().Add(time.Hour)}
			case "stale":
				reader.err = github.ErrKennelFixStale
			case "bad id":
				req.KennelRepositoryID = "pool_1"
			}
			_, err := h.c.InspectKennelFix(h.ctx, req)
			if err == nil {
				t.Fatal("accepted")
			}
			if mode == "stale" && !errors.Is(err, github.ErrKennelFixStale) {
				t.Fatalf("err = %v, want the stale error", err)
			}
		})
	}
}

// A waiver is a decision somebody made and wrote down. A fix does not route
// around it: the finding offers none, and the waiver stays where it is.
func TestInspectKennelFixOffersNoFixForAWaivedFinding(t *testing.T) {
	h, _, _, req := kennelFixFixture(t)
	zero := 0
	waived := kennel.Finding{Code: kennel.CodeNoTimeout, Severity: kennel.SeverityWarning, Subject: req.BlobSHA[:12], Evidence: []kennel.Evidence{{Kind: kennel.EvidenceFile, Ref: req.BlobSHA, JobIndex: &zero, Line: 3}}}
	h.saveKennelFixEvaluation(t, req.KennelRepositoryID, kennel.Evaluation{Version: kennel.Version, Waived: []kennel.WaivedFinding{{Finding: waived}}}, req.BlobSHA, ".github/workflows/ci.yml")
	if _, err := h.c.InspectKennelFix(h.ctx, req); !errors.Is(err, ErrKennelFixWaived) {
		t.Fatalf("err = %v, want the waived refusal", err)
	}
}

func TestKennelFixIsCapturedReplayedApprovedAndPublishedOnce(t *testing.T) {
	h, inst, reader, req := kennelFixFixture(t)
	in, err := h.c.InspectKennelFix(h.ctx, req)
	if err != nil {
		t.Fatal(err)
	}
	retained, err := h.c.CaptureKennelFix(h.ctx, kennelFixActor, req, in.Source.HeadSHA, in.Source.WorkflowSHA256)
	if err != nil {
		t.Fatal(err)
	}
	replayed, err := h.c.ReplayKennelFix(h.ctx, inst.ID, inst.Target, retained.ID)
	if err != nil || replayed.Preview.AfterSHA256 != in.Preview.AfterSHA256 || replayed.Currentness != "not_checked" {
		t.Fatalf("replay %+v err=%v", replayed, err)
	}
	if _, err := h.c.PublishKennelFix(h.ctx, kennelFixActor, inst.ID, inst.Target, retained.ID, in.Source.HeadSHA, in.Source.WorkflowSHA256, allowPublish); !errors.Is(err, ErrOptimisationNotApproved) {
		t.Fatalf("unapproved publish: %v", err)
	}
	if _, err := h.c.ApproveKennelFix(h.ctx, kennelFixActor, inst.ID, inst.Target, retained.ID, in.Source.HeadSHA, in.Source.WorkflowSHA256); err != nil {
		t.Fatal(err)
	}
	// The branch moved while the file did not: the draft is based on the newer
	// head, because the fix is bound to the file and not to the head.
	newer := strings.Repeat("c", 40)
	reader.source.HeadSHA = newer
	reader.publishPR = &github.PullRequest{Number: 12, HTMLURL: "https://github.com/" + inst.Target + "/pull/12", Branch: "zoomies/kennel-fix-" + retained.ID}
	published, err := h.c.PublishKennelFix(h.ctx, kennelFixActor, inst.ID, inst.Target, retained.ID, in.Source.HeadSHA, in.Source.WorkflowSHA256, allowPublish)
	if err != nil {
		t.Fatal(err)
	}
	if published.Publication == nil || published.Publication.PullNumber != 12 || published.Publication.BaseSHA != newer || published.Publication.HeadSHA != strings.Repeat("a", 40) || reader.publishes != 1 {
		t.Fatalf("publication %+v publishes=%d", published.Publication, reader.publishes)
	}
	r := reader.publishRequest
	if r.Path != in.Preview.Edit.Path || r.Content != in.Preview.File.Content || r.Base != "main" || r.BaseCommit != newer || r.Head != "zoomies/kennel-fix-"+retained.ID || !strings.Contains(r.Body, "not when this pull request is opened") || !strings.Contains(r.Body, retained.ID) {
		t.Fatalf("publish request %+v", r)
	}
	again, err := h.c.PublishKennelFix(h.ctx, kennelFixActor, inst.ID, inst.Target, retained.ID, in.Source.HeadSHA, in.Source.WorkflowSHA256, allowPublish)
	if err != nil || again.Publication == nil || reader.publishes != 1 {
		t.Fatalf("repeat published again: %v", err)
	}
}

func TestPublishKennelFixRefusesAChangedFileAPolicyWithdrawnAndAForeignBinding(t *testing.T) {
	setup := func(t *testing.T) (*harness, *store.Installation, *kennelFixReader, *RetainedKennelFix, *KennelFixInspection) {
		h, inst, reader, req := kennelFixFixture(t)
		in, err := h.c.InspectKennelFix(h.ctx, req)
		if err != nil {
			t.Fatal(err)
		}
		retained, err := h.c.CaptureKennelFix(h.ctx, kennelFixActor, req, in.Source.HeadSHA, in.Source.WorkflowSHA256)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := h.c.ApproveKennelFix(h.ctx, kennelFixActor, inst.ID, inst.Target, retained.ID, in.Source.HeadSHA, in.Source.WorkflowSHA256); err != nil {
			t.Fatal(err)
		}
		reader.publishPR = &github.PullRequest{Number: 12, HTMLURL: "https://github.com/" + inst.Target + "/pull/12", Branch: "zoomies/kennel-fix-" + retained.ID}
		return h, inst, reader, retained, in
	}
	t.Run("the file changed", func(t *testing.T) {
		h, inst, reader, retained, in := setup(t)
		reader.err = github.ErrKennelFixStale
		_, err := h.c.PublishKennelFix(h.ctx, kennelFixActor, inst.ID, inst.Target, retained.ID, in.Source.HeadSHA, in.Source.WorkflowSHA256, allowPublish)
		if !errors.Is(err, ErrOptimisationPublish) || reader.publishes != 0 {
			t.Fatalf("err=%v publishes=%d", err, reader.publishes)
		}
	})
	t.Run("publish policy off", func(t *testing.T) {
		h, inst, reader, retained, in := setup(t)
		off := func(context.Context) (optimisation.Policy, error) {
			return optimisation.Policy{SourceAllowed: true, WorkflowEditsAllowed: true}, nil
		}
		if _, err := h.c.PublishKennelFix(h.ctx, kennelFixActor, inst.ID, inst.Target, retained.ID, in.Source.HeadSHA, in.Source.WorkflowSHA256, off); !errors.Is(err, ErrOptimisationPublishPolicy) || reader.publishes != 0 {
			t.Fatalf("err=%v", err)
		}
	})
	t.Run("a head nobody approved", func(t *testing.T) {
		h, inst, reader, retained, in := setup(t)
		if _, err := h.c.PublishKennelFix(h.ctx, kennelFixActor, inst.ID, inst.Target, retained.ID, strings.Repeat("f", 40), in.Source.WorkflowSHA256, allowPublish); !errors.Is(err, ErrKennelFixApproval) || reader.publishes != 0 {
			t.Fatalf("err=%v", err)
		}
	})
	t.Run("the finding was waived since", func(t *testing.T) {
		h, inst, reader, retained, in := setup(t)
		zero := 0
		blob := in.Preview.BlobSHA
		waived := kennel.Finding{Code: kennel.CodeNoTimeout, Subject: blob[:12], Evidence: []kennel.Evidence{{Kind: kennel.EvidenceFile, Ref: blob, JobIndex: &zero, Line: 3}}}
		h.saveKennelFixEvaluation(t, retained.KennelRepositoryID, kennel.Evaluation{Version: kennel.Version, Waived: []kennel.WaivedFinding{{Finding: waived}}}, blob, ".github/workflows/ci.yml")
		if _, err := h.c.PublishKennelFix(h.ctx, kennelFixActor, inst.ID, inst.Target, retained.ID, in.Source.HeadSHA, in.Source.WorkflowSHA256, allowPublish); !errors.Is(err, ErrKennelFixWaived) || reader.publishes != 0 {
			t.Fatalf("err=%v", err)
		}
	})
}
