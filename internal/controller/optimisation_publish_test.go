package controller

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/eyupio/zoomies/internal/github"
	"github.com/eyupio/zoomies/internal/optimisation"
	"github.com/eyupio/zoomies/internal/store"
)

func optimisationPublishCheck(allowed bool) OptimisationPolicyCheck {
	return func(context.Context) (optimisation.Policy, error) {
		return optimisation.Policy{SourceAllowed: allowed, WorkflowEditsAllowed: allowed, PublishAllowed: allowed}, nil
	}
}

func TestPublishOptimisationRecordsOneDraftAndReconcilesARetry(t *testing.T) {
	h, inst, f, sel, review, actor := checkoutCaptureFixture(t)
	saved, err := h.c.CaptureOptimisationPreview(h.ctx, actor, inst.ID, inst.Target, sel, review, optimisationPublishCheck(true))
	if err != nil {
		t.Fatal(err)
	}
	if saved.Collection == nil || saved.Collection.HeadBranch != "main" {
		t.Fatalf("capture did not record the source branch: %+v", saved.Collection)
	}
	if _, err := h.c.ApproveOptimisationPreview(h.ctx, actor, inst.ID, inst.Target, saved.ID, saved.Preview.HeadSHA, saved.Preview.SourceSHA256); err != nil {
		t.Fatal(err)
	}
	f.publishPR = &github.PullRequest{Number: 12, HTMLURL: "https://github.com/acme/repo/pull/12", Branch: "zoomies/checkout-" + saved.ID}
	published, err := h.c.PublishOptimisationPreview(h.ctx, actor, inst.ID, inst.Target, saved.ID, saved.Preview.HeadSHA, saved.Preview.SourceSHA256, optimisationPublishCheck(true))
	if err != nil {
		t.Fatal(err)
	}
	if published.Publication == nil || published.Publication.PullNumber != 12 || published.Publication.PullURL != "https://github.com/acme/repo/pull/12" || published.Publication.Branch != "zoomies/checkout-"+saved.ID || published.Publication.BaseBranch != "main" || published.Publication.BaseSHA != saved.Preview.HeadSHA || published.Publication.HeadSHA != saved.Preview.HeadSHA || published.Publication.PublishedBy != actor.ActorID {
		t.Fatalf("publication %+v", published.Publication)
	}
	if f.publishes != 1 || f.publishRequest.Base != "main" || f.publishRequest.BaseCommit != saved.Preview.HeadSHA || f.publishRequest.Head != "zoomies/checkout-"+saved.ID || f.publishRequest.Path != saved.Preview.Edit.Path || f.publishRequest.Mode != saved.Preview.File.Mode || f.publishRequest.Content != saved.Preview.File.Content || f.publishRequest.SourceSHA256 != saved.Preview.SourceSHA256 {
		t.Fatalf("publish request %+v", f.publishRequest)
	}
	if f.calls != 2 || f.quotaCalls != 2 {
		t.Fatal("publication did not reread the source once")
	}
	if audit, total, err := h.st.ListAudit(h.ctx, store.AuditFilter{Actions: []string{"optimisation.preview_published"}}, store.Page{Limit: 10}); err != nil || total != 1 || audit[0].ActorID != actor.ActorID {
		t.Fatalf("publication audit %+v total=%d err=%v", audit, total, err)
	}
	// The replay carries the draft, and pressing publish again is the same act.
	replayed, err := h.c.ReplayOptimisationPreview(h.ctx, inst.ID, inst.Target, saved.ID)
	if err != nil || replayed.Publication == nil || replayed.Publication.PullNumber != 12 {
		t.Fatalf("replay lost the publication: %+v, %v", replayed, err)
	}
	again, err := h.c.PublishOptimisationPreview(h.ctx, actor, inst.ID, inst.Target, saved.ID, saved.Preview.HeadSHA, saved.Preview.SourceSHA256, optimisationPublishCheck(true))
	if err != nil || again.Publication == nil || again.Publication.PullNumber != 12 || f.publishes != 1 || f.calls != 2 {
		t.Fatalf("retry did not reconcile: %+v, %v, publishes=%d reads=%d", again, err, f.publishes, f.calls)
	}
}

func TestPublishOptimisationRefusesBeforeWritingWithoutApprovalOrPolicy(t *testing.T) {
	h, inst, f, sel, review, actor := checkoutCaptureFixture(t)
	saved, err := h.c.CaptureOptimisationPreview(h.ctx, actor, inst.ID, inst.Target, sel, review, optimisationPublishCheck(true))
	if err != nil {
		t.Fatal(err)
	}
	head, source := saved.Preview.HeadSHA, saved.Preview.SourceSHA256
	// A kept but unapproved preview cannot be published.
	if _, err := h.c.PublishOptimisationPreview(h.ctx, actor, inst.ID, inst.Target, saved.ID, head, source, optimisationPublishCheck(true)); !errors.Is(err, ErrOptimisationNotApproved) {
		t.Fatalf("unapproved publication: %v", err)
	}
	if _, err := h.c.ApproveOptimisationPreview(h.ctx, actor, inst.ID, inst.Target, saved.ID, head, source); err != nil {
		t.Fatal(err)
	}
	// A wrong head or source names something other than the retained preview.
	if _, err := h.c.PublishOptimisationPreview(h.ctx, actor, inst.ID, inst.Target, saved.ID, strings.Repeat("c", 40), source, optimisationPublishCheck(true)); !errors.Is(err, ErrOptimisationApproval) {
		t.Fatalf("wrong head: %v", err)
	}
	if _, err := h.c.PublishOptimisationPreview(h.ctx, actor, inst.ID, inst.Target, saved.ID, head, strings.Repeat("d", 64), optimisationPublishCheck(true)); !errors.Is(err, ErrOptimisationApproval) {
		t.Fatalf("wrong source: %v", err)
	}
	// Publishing is its own live consent, checked before any source read.
	if _, err := h.c.PublishOptimisationPreview(h.ctx, actor, inst.ID, inst.Target, saved.ID, head, source, optimisationPublishCheck(false)); !errors.Is(err, ErrOptimisationPublishPolicy) {
		t.Fatalf("policy: %v", err)
	}
	if f.calls != 1 || f.publishes != 0 {
		t.Fatal("a refused publication read source or opened a draft")
	}
}

func TestPublishOptimisationRefusesAMovedSourceBranchOrExpiredPreview(t *testing.T) {
	h, inst, f, sel, review, actor := checkoutCaptureFixture(t)
	saved, err := h.c.CaptureOptimisationPreview(h.ctx, actor, inst.ID, inst.Target, sel, review, optimisationPublishCheck(true))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := h.c.ApproveOptimisationPreview(h.ctx, actor, inst.ID, inst.Target, saved.ID, saved.Preview.HeadSHA, saved.Preview.SourceSHA256); err != nil {
		t.Fatal(err)
	}
	f.evidence.HeadBranch = "release"
	if _, err := h.c.PublishOptimisationPreview(h.ctx, actor, inst.ID, inst.Target, saved.ID, saved.Preview.HeadSHA, saved.Preview.SourceSHA256, optimisationPublishCheck(true)); !errors.Is(err, ErrOptimisationPublish) {
		t.Fatalf("branch changed: %v", err)
	}
	f.evidence.HeadBranch = "main"
	f.evidence.Source.File.Content = strings.Replace(f.evidence.Source.File.Content, "fetch-depth: 0", "fetch-depth: 2", 1)
	if _, err := h.c.PublishOptimisationPreview(h.ctx, actor, inst.ID, inst.Target, saved.ID, saved.Preview.HeadSHA, saved.Preview.SourceSHA256, optimisationPublishCheck(true)); !errors.Is(err, ErrOptimisationPublish) && !errors.Is(err, ErrOptimisationPreview) {
		t.Fatalf("source changed: %v", err)
	}
	if f.publishes != 0 {
		t.Fatal("a moved source opened a draft")
	}
}

func TestPublishOptimisationRefusesAPreviewWithoutABranchAndAMissingPermission(t *testing.T) {
	c, installation, source, timing, history, policy, actor := optimisationRetentionFixture(t)
	saved, err := c.RetainOptimisationPreview(context.Background(), actor, installation, "acme/repo", source, timing, history, policy)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.ApproveOptimisationPreview(context.Background(), actor, installation, "acme/repo", saved.ID, source.HeadSHA, saved.Preview.SourceSHA256); err != nil {
		t.Fatal(err)
	}
	if _, err := c.PublishOptimisationPreview(context.Background(), actor, installation, "acme/repo", saved.ID, source.HeadSHA, saved.Preview.SourceSHA256, optimisationPublishCheck(true)); !errors.Is(err, ErrOptimisationPublish) || !strings.Contains(err.Error(), "predates branch recording") {
		t.Fatalf("pre-branch preview: %v", err)
	}

	h, inst, _, sel, review, publisher := checkoutCaptureFixture(t)
	fresh, err := h.c.CaptureOptimisationPreview(h.ctx, publisher, inst.ID, inst.Target, sel, review, optimisationPublishCheck(true))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := h.c.ApproveOptimisationPreview(h.ctx, publisher, inst.ID, inst.Target, fresh.ID, fresh.Preview.HeadSHA, fresh.Preview.SourceSHA256); err != nil {
		t.Fatal(err)
	}
	h.gh.SetPermissions(map[string]string{"contents": "read", "pull_requests": "write", "workflows": "write"})
	if _, err := h.c.PublishOptimisationPreview(h.ctx, publisher, inst.ID, inst.Target, fresh.ID, fresh.Preview.HeadSHA, fresh.Preview.SourceSHA256, optimisationPublishCheck(true)); !errors.Is(err, ErrOptimisationPublish) || !strings.Contains(err.Error(), "Contents") {
		t.Fatalf("missing permission: %v", err)
	}
}

func TestPublishOptimisationDoesNotRecordAFailedGitHubWrite(t *testing.T) {
	h, inst, f, sel, review, actor := checkoutCaptureFixture(t)
	saved, err := h.c.CaptureOptimisationPreview(h.ctx, actor, inst.ID, inst.Target, sel, review, optimisationPublishCheck(true))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := h.c.ApproveOptimisationPreview(h.ctx, actor, inst.ID, inst.Target, saved.ID, saved.Preview.HeadSHA, saved.Preview.SourceSHA256); err != nil {
		t.Fatal(err)
	}
	f.publishErr = github.ErrOptimisationPublishConflict
	if _, err := h.c.PublishOptimisationPreview(h.ctx, actor, inst.ID, inst.Target, saved.ID, saved.Preview.HeadSHA, saved.Preview.SourceSHA256, optimisationPublishCheck(true)); !errors.Is(err, github.ErrOptimisationPublishConflict) {
		t.Fatalf("publisher refusal: %v", err)
	}
	if _, total, err := h.st.ListAudit(h.ctx, store.AuditFilter{Actions: []string{"optimisation.preview_published"}}, store.Page{Limit: 10}); err != nil || total != 0 {
		t.Fatal("a failed write was recorded")
	}
}
