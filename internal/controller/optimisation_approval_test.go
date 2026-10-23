package controller

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/eyupio/zoomies/internal/optimisation"
	"github.com/eyupio/zoomies/internal/prrepair"
	"github.com/eyupio/zoomies/internal/store"
)

func optimisationApprovalFixture(t *testing.T) (*harness, string, string, optimisation.Source, optimisation.Timing, optimisation.HistoryReview, *store.AuditEvent, *RetainedOptimisationPreview) {
	t.Helper()
	h := newHarness(t)
	inst := h.installationOn("acme/repo", store.TargetRepo)
	file := prrepair.File{Path: ".github/workflows/private.yml", Mode: "100644", Content: "on: push\njobs:\n  private:\n    runs-on: ubuntu-latest\n    steps:\n      - uses: actions/checkout@" + strings.Repeat("b", 40) + "\n        with:\n          fetch-depth: 0\n"}
	source := optimisation.Source{HeadSHA: strings.Repeat("a", 40), File: file}
	sum := sha256.Sum256([]byte(file.Content))
	digest := hex.EncodeToString(sum[:])
	ms, needs := int64(4200), false
	timing := optimisation.Timing{EvidenceID: "fixture", HeadSHA: source.HeadSHA, SourceSHA256: digest, RunID: "10", Attempt: 2, JobIndex: 0, StepIndex: 0, Outcome: "success", CheckoutMS: &ms}
	history := optimisation.HistoryReview{HeadSHA: source.HeadSHA, SourceSHA256: digest, Scope: "repository_and_downstream_consumers", NeedsHistory: &needs}
	actor := &store.AuditEvent{ActorID: "usr_reviewer", ActorKind: "user", ActorName: "reviewer"}
	saved, err := h.c.RetainOptimisationPreview(h.ctx, actor, inst.ID, "acme/repo", source, timing, history, optimisation.Policy{SourceAllowed: true, WorkflowEditsAllowed: true})
	if err != nil {
		t.Fatal(err)
	}
	return h, inst.ID, "acme/repo", source, timing, history, actor, saved
}

func TestOptimisationApprovalRecordsTheReviewerAndApproverAndDoesNotPublish(t *testing.T) {
	h, installation, repo, source, _, _, _, saved := optimisationApprovalFixture(t)
	approver := &store.AuditEvent{ActorID: "usr_approver", ActorKind: "user", ActorName: "approver"}
	approved, err := h.c.ApproveOptimisationPreview(h.ctx, approver, installation, repo, saved.ID, source.HeadSHA, saved.Preview.SourceSHA256)
	if err != nil {
		t.Fatal(err)
	}
	if approved.Approval == nil || approved.Approval.ApprovedBy != "usr_approver" || approved.ReviewedBy != "usr_reviewer" || approved.Approval.HeadSHA != source.HeadSHA || approved.Approval.SourceSHA256 != saved.Preview.SourceSHA256 || approved.Approval.PreviewSHA256 != saved.SHA256 {
		t.Fatalf("approval does not record both people and the exact binding: %+v", approved)
	}
	if approved.Currentness != "not_checked" || approved.Preview.Verification != "inconclusive" || approved.Preview.State != "preview_ready" {
		t.Fatal("approval promoted currentness, verification or the preview state")
	}
	replayed, err := h.c.ReplayOptimisationPreview(h.ctx, installation, repo, saved.ID)
	if err != nil || replayed.Approval == nil || replayed.Approval.ApprovedBy != "usr_approver" || !replayed.Approval.ApprovedAt.Equal(approved.Approval.ApprovedAt) {
		t.Fatalf("replay lost the approval: %+v, %v", replayed, err)
	}
	for action, want := range map[string]int{"optimisation.preview_retained": 1, "optimisation.preview_approved": 1} {
		_, total, err := h.st.ListAudit(h.ctx, store.AuditFilter{Actions: []string{action}}, store.Page{Limit: 10})
		if err != nil || total != want {
			t.Fatalf("%s audit total=%d err=%v", action, total, err)
		}
	}
}

func TestOptimisationApprovalRefusesAMovedBindingAndKeepsOneAct(t *testing.T) {
	h, installation, repo, source, _, _, _, saved := optimisationApprovalFixture(t)
	actor := &store.AuditEvent{ActorID: "usr_approver", ActorKind: "user"}
	for _, tc := range []struct {
		name string
		head string
		src  string
	}{
		{"head", strings.Repeat("c", 40), saved.Preview.SourceSHA256},
		{"source", source.HeadSHA, strings.Repeat("d", 64)},
		{"malformed", "not-a-commit", saved.Preview.SourceSHA256},
	} {
		if _, err := h.c.ApproveOptimisationPreview(h.ctx, actor, installation, repo, saved.ID, tc.head, tc.src); !errors.Is(err, ErrOptimisationApproval) {
			t.Fatalf("%s: %v", tc.name, err)
		}
	}
	if _, err := h.c.ApproveOptimisationPreview(h.ctx, nil, installation, repo, saved.ID, source.HeadSHA, saved.Preview.SourceSHA256); !errors.Is(err, ErrOptimisationApproval) {
		t.Fatalf("anonymous approval: %v", err)
	}
	_, total, err := h.st.ListAudit(h.ctx, store.AuditFilter{Actions: []string{"optimisation.preview_approved"}}, store.Page{Limit: 10})
	if err != nil || total != 0 {
		t.Fatalf("a refused approval wrote an audit row: %d, %v", total, err)
	}
	first, err := h.c.ApproveOptimisationPreview(h.ctx, actor, installation, repo, saved.ID, source.HeadSHA, saved.Preview.SourceSHA256)
	if err != nil {
		t.Fatal(err)
	}
	again, err := h.c.ApproveOptimisationPreview(h.ctx, actor, installation, repo, saved.ID, source.HeadSHA, saved.Preview.SourceSHA256)
	if err != nil || again.Approval == nil || !again.Approval.ApprovedAt.Equal(first.Approval.ApprovedAt) {
		t.Fatalf("repeated approval did not return the first act: %+v, %v", again, err)
	}
	_, total, err = h.st.ListAudit(h.ctx, store.AuditFilter{Actions: []string{"optimisation.preview_approved"}}, store.Page{Limit: 10})
	if err != nil || total != 1 {
		t.Fatalf("a repeated approval wrote a second audit row: %d, %v", total, err)
	}
}

func TestOptimisationApprovalExpiresWithThePreviewAndStaysScoped(t *testing.T) {
	now := time.Date(2026, 10, 23, 9, 0, 0, 0, time.UTC)
	h := newHarnessAt(t, func() time.Time { return now })
	inst := h.installationOn("acme/repo", store.TargetRepo)
	file := prrepair.File{Path: ".github/workflows/private.yml", Mode: "100644", Content: "on: push\njobs:\n  private:\n    runs-on: ubuntu-latest\n    steps:\n      - uses: actions/checkout@" + strings.Repeat("b", 40) + "\n        with:\n          fetch-depth: 0\n"}
	source := optimisation.Source{HeadSHA: strings.Repeat("a", 40), File: file}
	sum := sha256.Sum256([]byte(file.Content))
	digest := hex.EncodeToString(sum[:])
	ms, needs := int64(4200), false
	timing := optimisation.Timing{EvidenceID: "fixture", HeadSHA: source.HeadSHA, SourceSHA256: digest, RunID: "10", Attempt: 2, JobIndex: 0, StepIndex: 0, Outcome: "success", CheckoutMS: &ms}
	history := optimisation.HistoryReview{HeadSHA: source.HeadSHA, SourceSHA256: digest, Scope: "repository_and_downstream_consumers", NeedsHistory: &needs}
	actor := &store.AuditEvent{ActorID: "usr_reviewer", ActorKind: "user"}
	saved, err := h.c.RetainOptimisationPreview(h.ctx, actor, inst.ID, "acme/repo", source, timing, history, optimisation.Policy{SourceAllowed: true, WorkflowEditsAllowed: true})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := h.c.ApproveOptimisationPreview(h.ctx, actor, "other", "acme/repo", saved.ID, source.HeadSHA, saved.Preview.SourceSHA256); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("cross-installation approval: %v", err)
	}
	if _, err := h.c.ApproveOptimisationPreview(h.ctx, actor, inst.ID, "acme/other", saved.ID, source.HeadSHA, saved.Preview.SourceSHA256); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("cross-repository approval: %v", err)
	}
	now = now.Add(store.OptimisationPreviewRetention)
	if _, err := h.c.ApproveOptimisationPreview(h.ctx, actor, inst.ID, "acme/repo", saved.ID, source.HeadSHA, saved.Preview.SourceSHA256); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("expired approval: %v", err)
	}
}
