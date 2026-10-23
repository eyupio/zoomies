package controller

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/eyupio/zoomies/internal/github"
	"github.com/eyupio/zoomies/internal/store"
)

func retainedWorkflowComparison(t *testing.T, c *Controller, actor *store.AuditEvent, installation, previewID string) *RetainedExperimentComparison {
	t.Helper()
	record, a, b := experimentSnapshotFixture()
	record.Kind = "workflow"
	record.RevisionDeltaID = previewID
	a.InstallationID, b.InstallationID = installation, installation
	saved, err := c.RetainExperimentComparison(context.Background(), actor, record, a, b)
	if err != nil {
		t.Fatal(err)
	}
	return saved
}

func TestOptimisationVerificationLinksAWorkflowComparisonToAPublishedPreview(t *testing.T) {
	h, inst, f, sel, review, actor := checkoutCaptureFixture(t)
	saved, err := h.c.CaptureOptimisationPreview(h.ctx, actor, inst.ID, inst.Target, sel, review, optimisationPublishCheck(true))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := h.c.ApproveOptimisationPreview(h.ctx, actor, inst.ID, inst.Target, saved.ID, saved.Preview.HeadSHA, saved.Preview.SourceSHA256); err != nil {
		t.Fatal(err)
	}
	f.publishPR = &github.PullRequest{Number: 12, HTMLURL: "https://github.com/acme/repo/pull/12", Branch: "zoomies/checkout-" + saved.ID}
	if _, err := h.c.PublishOptimisationPreview(h.ctx, actor, inst.ID, inst.Target, saved.ID, saved.Preview.HeadSHA, saved.Preview.SourceSHA256, optimisationPublishCheck(true)); err != nil {
		t.Fatal(err)
	}
	comparison := retainedWorkflowComparison(t, h.c, actor, inst.ID, saved.ID)
	linked, err := h.c.LinkOptimisationVerification(h.ctx, actor, inst.ID, inst.Target, saved.ID, comparison.ID)
	if err != nil {
		t.Fatal(err)
	}
	if linked.Verification == nil || linked.Verification.ComparisonID != comparison.ID || linked.Verification.LinkedBy != actor.ActorID {
		t.Fatalf("verification %+v", linked.Verification)
	}
	replayed, err := h.c.ReplayOptimisationPreview(h.ctx, inst.ID, inst.Target, saved.ID)
	if err != nil || replayed.Verification == nil || replayed.Verification.ComparisonID != comparison.ID {
		t.Fatalf("replay lost the link: %+v, %v", replayed, err)
	}
	if _, total, err := h.st.ListAudit(h.ctx, store.AuditFilter{Actions: []string{"optimisation.comparison_linked"}}, store.Page{Limit: 10}); err != nil || total != 1 {
		t.Fatalf("audit total=%d err=%v", total, err)
	}
	again, err := h.c.LinkOptimisationVerification(h.ctx, actor, inst.ID, inst.Target, saved.ID, comparison.ID)
	if err != nil || again.Verification == nil || !again.Verification.LinkedAt.Equal(linked.Verification.LinkedAt) {
		t.Fatalf("repeat %+v, %v", again, err)
	}
}

func TestOptimisationVerificationRefusesAnUnpublishedPreviewOrAnUnrelatedComparison(t *testing.T) {
	h, inst, f, sel, review, actor := checkoutCaptureFixture(t)
	saved, err := h.c.CaptureOptimisationPreview(h.ctx, actor, inst.ID, inst.Target, sel, review, optimisationPublishCheck(true))
	if err != nil {
		t.Fatal(err)
	}
	comparison := retainedWorkflowComparison(t, h.c, actor, inst.ID, saved.ID)
	if _, err := h.c.LinkOptimisationVerification(h.ctx, actor, inst.ID, inst.Target, saved.ID, comparison.ID); !errors.Is(err, ErrOptimisationVerification) {
		t.Fatalf("unpublished preview: %v", err)
	}
	if _, err := h.c.ApproveOptimisationPreview(h.ctx, actor, inst.ID, inst.Target, saved.ID, saved.Preview.HeadSHA, saved.Preview.SourceSHA256); err != nil {
		t.Fatal(err)
	}
	f.publishPR = &github.PullRequest{Number: 12, HTMLURL: "https://github.com/acme/repo/pull/12", Branch: "zoomies/checkout-" + saved.ID}
	if _, err := h.c.PublishOptimisationPreview(h.ctx, actor, inst.ID, inst.Target, saved.ID, saved.Preview.HeadSHA, saved.Preview.SourceSHA256, optimisationPublishCheck(true)); err != nil {
		t.Fatal(err)
	}
	record, a, b := experimentSnapshotFixture()
	record.Kind = "workflow"
	record.RevisionDeltaID = "other-preview"
	a.InstallationID, b.InstallationID = inst.ID, inst.ID
	unrelated, err := h.c.RetainExperimentComparison(h.ctx, actor, record, a, b)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := h.c.LinkOptimisationVerification(h.ctx, actor, inst.ID, inst.Target, saved.ID, unrelated.ID); !errors.Is(err, ErrOptimisationVerification) || !strings.Contains(err.Error(), "does not name this preview") {
		t.Fatalf("unrelated comparison: %v", err)
	}
	record, a, b = experimentSnapshotFixture()
	record.Kind = "runner"
	record.RevisionDeltaID = saved.ID
	a.InstallationID, b.InstallationID = inst.ID, inst.ID
	runnerComparison, err := h.c.RetainExperimentComparison(h.ctx, actor, record, a, b)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := h.c.LinkOptimisationVerification(h.ctx, actor, inst.ID, inst.Target, saved.ID, runnerComparison.ID); !errors.Is(err, ErrOptimisationVerification) || !strings.Contains(err.Error(), "workflow comparison") {
		t.Fatalf("runner comparison: %v", err)
	}
}
