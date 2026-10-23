package store

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"
)

func optimisationComparisonFixture(t *testing.T, s *Store, r *OptimisationPreview) *ComparisonEvidence {
	t.Helper()
	c := &ComparisonEvidence{
		ID: NewID(PrefixComparison), InstallationID: r.InstallationID, Repository: r.Repository,
		InitiatingUser: "usr_actor", Version: ComparisonEvidenceVersion, Payload: json.RawMessage(`{"version":"observed-comparison-v1"}`),
	}
	if err := s.PutComparisonEvidence(context.Background(), c, optimisationAudit()); err != nil {
		t.Fatal(err)
	}
	return c
}

func TestOptimisationVerificationLinksOneComparisonAndIsIdempotentAudited(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	r, _ := optimisationPublicationFixture(t, s)
	c := optimisationComparisonFixture(t, s, r)
	link, err := s.LinkOptimisationComparison(ctx, r.InstallationID, r.Repository, r.ID, c.ID, optimisationAudit())
	if err != nil {
		t.Fatal(err)
	}
	if link.ComparisonID != c.ID || link.LinkedBy != "usr_actor" || link.CreatedAt.IsZero() {
		t.Fatalf("link %+v", link)
	}
	got, err := s.GetOptimisationPreview(ctx, r.InstallationID, r.Repository, r.ID)
	if err != nil || got.Verification == nil || got.Verification.ComparisonID != c.ID || got.Verification.LinkedBy != "usr_actor" {
		t.Fatalf("verification %+v, %v", got, err)
	}
	again, err := s.LinkOptimisationComparison(ctx, r.InstallationID, r.Repository, r.ID, c.ID, optimisationAudit())
	if err != nil || !again.CreatedAt.Equal(link.CreatedAt) {
		t.Fatalf("repeat %+v, %v", again, err)
	}
	events, total, err := s.ListAudit(ctx, AuditFilter{Actions: []string{"optimisation.comparison_linked"}, TargetID: r.ID}, Page{Limit: 10})
	if err != nil || total != 1 || events[0].ActorID != "usr_actor" {
		t.Fatalf("audit %+v total=%d err=%v", events, total, err)
	}
	// A different comparison is a conflict, not a replacement.
	other := optimisationComparisonFixture(t, s, r)
	if _, err := s.LinkOptimisationComparison(ctx, r.InstallationID, r.Repository, r.ID, other.ID, optimisationAudit()); !errors.Is(err, ErrConflict) {
		t.Fatalf("replacement: %v", err)
	}
	// A comparison outside the preview's scope is not found.
	if _, err := s.LinkOptimisationComparison(ctx, r.InstallationID, "acme/other", r.ID, c.ID, optimisationAudit()); !errors.Is(err, ErrNotFound) {
		t.Fatalf("wrong scope: %v", err)
	}
	// A preview without a publication cannot carry a verification.
	s2 := newTestStore(t)
	r2, _ := optimisationApprovalFixture(t, s2)
	c2 := optimisationComparisonFixture(t, s2, r2)
	if _, err := s2.LinkOptimisationComparison(ctx, r2.InstallationID, r2.Repository, r2.ID, c2.ID, optimisationAudit()); !errors.Is(err, ErrConflict) {
		t.Fatalf("unpublished preview: %v", err)
	}
}

func TestOptimisationVerificationRejectsInvalidInput(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	r, _ := optimisationPublicationFixture(t, s)
	c := optimisationComparisonFixture(t, s, r)
	for _, mode := range []string{"actor", "preview", "comparison", "repo", "installation"} {
		installation, repo, preview, comparison := r.InstallationID, r.Repository, r.ID, c.ID
		actor := optimisationAudit()
		switch mode {
		case "actor":
			actor = nil
		case "preview":
			preview = "preview"
		case "comparison":
			comparison = "comparison"
		case "repo":
			repo = "acme"
		case "installation":
			installation = ""
		}
		if _, err := s.LinkOptimisationComparison(ctx, installation, repo, preview, comparison, actor); !errors.Is(err, ErrInvalidOptimisationVerification) {
			t.Fatalf("%s: %v", mode, err)
		}
	}
}

func TestOptimisationVerificationExpiresWithThePreviewAndCascades(t *testing.T) {
	now := time.Date(2026, 10, 23, 12, 0, 0, 0, time.UTC)
	s := newTestStoreAt(t, func() time.Time { return now })
	ctx := context.Background()
	r, _ := optimisationPublicationFixture(t, s)
	c := optimisationComparisonFixture(t, s, r)
	if _, err := s.LinkOptimisationComparison(ctx, r.InstallationID, r.Repository, r.ID, c.ID, optimisationAudit()); err != nil {
		t.Fatal(err)
	}
	now = now.Add(OptimisationPreviewRetention)
	if _, err := s.GetOptimisationPreview(ctx, r.InstallationID, r.Repository, r.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expired preview: %v", err)
	}
	if _, err := s.LinkOptimisationComparison(ctx, r.InstallationID, r.Repository, r.ID, c.ID, optimisationAudit()); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expired link: %v", err)
	}
	// Deleting the installation cascades through the preview and publication.
	s2 := newTestStore(t)
	r2, _ := optimisationPublicationFixture(t, s2)
	c2 := optimisationComparisonFixture(t, s2, r2)
	if _, err := s2.LinkOptimisationComparison(ctx, r2.InstallationID, r2.Repository, r2.ID, c2.ID, optimisationAudit()); err != nil {
		t.Fatal(err)
	}
	if _, err := s2.DeleteInstallation(ctx, r2.InstallationID); err != nil {
		t.Fatal(err)
	}
	var links int
	if err := s2.read.QueryRowContext(ctx, `SELECT COUNT(*) FROM optimisation_publication_verifications WHERE preview_id = ?`, r2.ID).Scan(&links); err != nil || links != 0 {
		t.Fatalf("installation deletion left a link: %d, %v", links, err)
	}
}

func TestOptimisationVerificationRollsBackWhenItsAuditCannotBeWritten(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	r, _ := optimisationPublicationFixture(t, s)
	c := optimisationComparisonFixture(t, s, r)
	if _, err := s.exec(ctx, `CREATE TRIGGER reject_verification_audit BEFORE INSERT ON audit_events WHEN NEW.action = 'optimisation.comparison_linked' BEGIN SELECT RAISE(FAIL,'audit unavailable'); END`); err != nil {
		t.Fatal(err)
	}
	if _, err := s.LinkOptimisationComparison(ctx, r.InstallationID, r.Repository, r.ID, c.ID, optimisationAudit()); err == nil {
		t.Fatal("a link without its audit row was accepted")
	}
	var links int
	if err := s.read.QueryRowContext(ctx, `SELECT COUNT(*) FROM optimisation_publication_verifications WHERE preview_id = ?`, r.ID).Scan(&links); err != nil || links != 0 {
		t.Fatalf("an unaudited link was committed: %d, %v", links, err)
	}
}

func TestOptimisationPreviewRefusesACorruptVerification(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	r, _ := optimisationPublicationFixture(t, s)
	c := optimisationComparisonFixture(t, s, r)
	if _, err := s.LinkOptimisationComparison(ctx, r.InstallationID, r.Repository, r.ID, c.ID, optimisationAudit()); err != nil {
		t.Fatal(err)
	}
	if _, err := s.exec(ctx, `UPDATE optimisation_publication_verifications SET comparison_id = ? WHERE preview_id = ?`, "not a comparison", r.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.GetOptimisationPreview(ctx, r.InstallationID, r.Repository, r.ID); !errors.Is(err, ErrInvalidOptimisationPreview) {
		t.Fatalf("a corrupt link was served: %v", err)
	}
}
