package store

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

func optimisationApprovalFixture(t *testing.T, s *Store) (*OptimisationPreview, *OptimisationApproval) {
	t.Helper()
	r := optimisationPreviewFixture(t, s)
	ctx := context.Background()
	if err := s.PutOptimisationPreview(ctx, r, optimisationAudit()); err != nil {
		t.Fatal(err)
	}
	approval, err := s.ApproveOptimisationPreview(ctx, r.InstallationID, r.Repository, r.ID, strings.Repeat("a", 40), strings.Repeat("b", 64), r.SHA256, optimisationAudit())
	if err != nil {
		t.Fatal(err)
	}
	return r, approval
}

func TestOptimisationPreviewApprovalNamesItsBindingIsIdempotentAndAudited(t *testing.T) {
	fixed := time.Date(2026, 10, 23, 9, 0, 0, 0, time.UTC)
	s := newTestStoreAt(t, func() time.Time { return fixed })
	ctx := context.Background()
	r, approval := optimisationApprovalFixture(t, s)
	if approval.ApprovedBy != "usr_actor" || approval.HeadSHA != strings.Repeat("a", 40) || approval.SourceSHA256 != strings.Repeat("b", 64) || approval.PreviewSHA256 != r.SHA256 || !approval.CreatedAt.Equal(fixed) {
		t.Fatalf("approval does not name the retained binding: %+v", approval)
	}
	// Approving the same preview again returns the original act rather than
	// rewriting it or adding a second audit row.
	again, err := s.ApproveOptimisationPreview(ctx, r.InstallationID, r.Repository, r.ID, strings.Repeat("a", 40), strings.Repeat("b", 64), r.SHA256, optimisationAudit())
	if err != nil {
		t.Fatal(err)
	}
	if !again.CreatedAt.Equal(approval.CreatedAt) || again.ApprovedBy != approval.ApprovedBy {
		t.Fatal("a repeated approval renewed or replaced the first")
	}
	events, total, err := s.ListAudit(ctx, AuditFilter{Actions: []string{"optimisation.preview_approved"}, TargetID: r.ID}, Page{Limit: 10})
	if err != nil || total != 1 || events[0].ActorID != "usr_actor" {
		t.Fatalf("approval audit %+v total=%d err=%v", events, total, err)
	}
	if events[0].Before != "" || events[0].After != "" {
		t.Fatalf("the audit row carries a document instead of only the binding: before=%q after=%q", events[0].Before, events[0].After)
	}
	got, err := s.GetOptimisationPreview(ctx, r.InstallationID, r.Repository, r.ID)
	if err != nil || got.Approval == nil || got.Approval.HeadSHA != approval.HeadSHA || got.Approval.SourceSHA256 != approval.SourceSHA256 || got.Approval.PreviewSHA256 != approval.PreviewSHA256 {
		t.Fatalf("a read lost or changed the approval: %+v, %v", got, err)
	}
	// A different binding is a conflict, not a silent second approval.
	if _, err := s.ApproveOptimisationPreview(ctx, r.InstallationID, r.Repository, r.ID, strings.Repeat("a", 40), strings.Repeat("c", 64), r.SHA256, optimisationAudit()); !errors.Is(err, ErrConflict) {
		t.Fatalf("a changed binding was accepted: %v", err)
	}
	// The approval is scoped exactly like the preview.
	if _, err := s.ApproveOptimisationPreview(ctx, "other", r.Repository, r.ID, strings.Repeat("a", 40), strings.Repeat("b", 64), r.SHA256, optimisationAudit()); !errors.Is(err, ErrNotFound) {
		t.Fatalf("cross-installation approval answered %v", err)
	}
	if _, err := s.ApproveOptimisationPreview(ctx, r.InstallationID, "other/repo", r.ID, strings.Repeat("a", 40), strings.Repeat("b", 64), r.SHA256, optimisationAudit()); !errors.Is(err, ErrNotFound) {
		t.Fatalf("cross-repository approval answered %v", err)
	}
}

func TestOptimisationPreviewApprovalRejectsInvalidInput(t *testing.T) {
	s := newTestStore(t)
	r, _ := optimisationApprovalFixture(t, s)
	ctx := context.Background()
	for _, mode := range []string{"actor", "id", "head", "source", "preview", "repo", "installation"} {
		installation, repo, id := r.InstallationID, r.Repository, r.ID
		head, source, preview, actor := strings.Repeat("a", 40), strings.Repeat("b", 64), r.SHA256, optimisationAudit()
		switch mode {
		case "actor":
			actor = nil
		case "id":
			id = "preview"
		case "head":
			head = strings.Repeat("A", 40)
		case "source":
			source = "short"
		case "preview":
			// A preview digest that does not name the stored record is not
			// invalid input, it is a conflict with the retained evidence.
			preview = strings.Repeat("c", 64)
		case "repo":
			repo = "acme"
		case "installation":
			installation = ""
		}
		_, err := s.ApproveOptimisationPreview(ctx, installation, repo, id, head, source, preview, actor)
		if mode == "preview" {
			if !errors.Is(err, ErrConflict) {
				t.Fatalf("%s: %v", mode, err)
			}
			continue
		}
		if !errors.Is(err, ErrInvalidOptimisationApproval) {
			t.Fatalf("%s: %v", mode, err)
		}
	}
}

func TestOptimisationPreviewApprovalExpiresWithThePreview(t *testing.T) {
	now := time.Date(2026, 10, 23, 0, 0, 0, 0, time.UTC)
	s := newTestStoreAt(t, func() time.Time { return now })
	r, approval := optimisationApprovalFixture(t, s)
	ctx := context.Background()
	now = now.Add(OptimisationPreviewRetention)
	if _, err := s.GetOptimisationPreview(ctx, r.InstallationID, r.Repository, r.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("an expired preview remained readable: %v", err)
	}
	if _, err := s.ApproveOptimisationPreview(ctx, r.InstallationID, r.Repository, r.ID, approval.HeadSHA, approval.SourceSHA256, r.SHA256, optimisationAudit()); !errors.Is(err, ErrNotFound) {
		t.Fatalf("an expired preview accepted approval: %v", err)
	}
}

func TestOptimisationPreviewApprovalIsRemovedWithItsPreviewOrInstallation(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	r := optimisationPreviewFixture(t, s)
	if err := s.PutOptimisationPreview(ctx, r, optimisationAudit()); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ApproveOptimisationPreview(ctx, r.InstallationID, r.Repository, r.ID, strings.Repeat("a", 40), strings.Repeat("b", 64), r.SHA256, optimisationAudit()); err != nil {
		t.Fatal(err)
	}
	// Fill the per-installation history; the approved preview is the oldest and
	// is evicted first, taking its approval with it.
	for i := 0; i < MaxPreviewsPerInstallation; i++ {
		next := *r
		next.ID = NewID(PrefixOptimisationPreview)
		if err := s.PutOptimisationPreview(ctx, &next, optimisationAudit()); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := s.GetOptimisationPreview(ctx, r.InstallationID, r.Repository, r.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("the oldest preview was not evicted: %v", err)
	}
	var approvals int
	if err := s.read.QueryRowContext(ctx, `SELECT COUNT(*) FROM optimisation_preview_approvals WHERE preview_id = ?`, r.ID).Scan(&approvals); err != nil || approvals != 0 {
		t.Fatalf("eviction left an approval row: %d, %v", approvals, err)
	}
	// Deleting the installation cascades through the preview to its approval.
	latest := NewID(PrefixOptimisationPreview)
	last := *r
	last.ID = latest
	if err := s.PutOptimisationPreview(ctx, &last, optimisationAudit()); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ApproveOptimisationPreview(ctx, r.InstallationID, r.Repository, latest, strings.Repeat("a", 40), strings.Repeat("b", 64), last.SHA256, optimisationAudit()); err != nil {
		t.Fatal(err)
	}
	if _, err := s.DeleteInstallation(ctx, r.InstallationID); err != nil {
		t.Fatal(err)
	}
	if err := s.read.QueryRowContext(ctx, `SELECT COUNT(*) FROM optimisation_preview_approvals WHERE installation_id = ?`, r.InstallationID).Scan(&approvals); err != nil || approvals != 0 {
		t.Fatalf("installation deletion left approvals: %d, %v", approvals, err)
	}
}

func TestOptimisationPreviewApprovalRollsBackWhenItsAuditCannotBeWritten(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	r := optimisationPreviewFixture(t, s)
	if err := s.PutOptimisationPreview(ctx, r, optimisationAudit()); err != nil {
		t.Fatal(err)
	}
	if _, err := s.exec(ctx, `CREATE TRIGGER reject_preview_approval_audit BEFORE INSERT ON audit_events WHEN NEW.action = 'optimisation.preview_approved' BEGIN SELECT RAISE(FAIL,'audit unavailable'); END`); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ApproveOptimisationPreview(ctx, r.InstallationID, r.Repository, r.ID, strings.Repeat("a", 40), strings.Repeat("b", 64), r.SHA256, optimisationAudit()); err == nil {
		t.Fatal("an approval without its audit row was accepted")
	}
	if _, err := s.GetOptimisationPreview(ctx, r.InstallationID, r.Repository, r.ID); err != nil {
		t.Fatal(err)
	}
	var approvals int
	if err := s.read.QueryRowContext(ctx, `SELECT COUNT(*) FROM optimisation_preview_approvals WHERE preview_id = ?`, r.ID).Scan(&approvals); err != nil || approvals != 0 {
		t.Fatalf("an unaudited approval was committed: %d, %v", approvals, err)
	}
	if _, err := s.exec(ctx, `DROP TRIGGER reject_preview_approval_audit`); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ApproveOptimisationPreview(ctx, r.InstallationID, r.Repository, r.ID, strings.Repeat("a", 40), strings.Repeat("b", 64), r.SHA256, optimisationAudit()); err != nil {
		t.Fatal(err)
	}
}

func TestOptimisationPreviewApprovalIsHiddenWhenEitherRecordIsCorrupt(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	r, _ := optimisationApprovalFixture(t, s)
	if _, err := s.exec(ctx, `UPDATE optimisation_preview_approvals SET source_sha256 = ? WHERE preview_id = ?`, "not-a-digest", r.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.GetOptimisationPreview(ctx, r.InstallationID, r.Repository, r.ID); !errors.Is(err, ErrInvalidOptimisationPreview) {
		t.Fatalf("a corrupt approval was served: %v", err)
	}
	// Corrupting the preview after it was kept must also stop an approval that
	// names the old digest; the store checks the payload it is about to approve.
	if _, err := s.exec(ctx, `UPDATE optimisation_previews SET payload = ? WHERE id = ?`, []byte(`{"version":"checkout-retained-v1","corrupt":true}`), r.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ApproveOptimisationPreview(ctx, r.InstallationID, r.Repository, r.ID, strings.Repeat("a", 40), strings.Repeat("b", 64), r.SHA256, optimisationAudit()); !errors.Is(err, ErrInvalidOptimisationPreview) {
		t.Fatalf("a corrupt preview was approved: %v", err)
	}
}
