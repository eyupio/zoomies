package store

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"
)

func optimisationPublicationFixture(t *testing.T, s *Store) (*OptimisationPreview, *OptimisationPublication) {
	t.Helper()
	r, approval := optimisationApprovalFixture(t, s)
	publication := &OptimisationPublication{
		PreviewID: r.ID, InstallationID: r.InstallationID, Repository: r.Repository, PublishedBy: "usr_actor",
		Branch: "zoomies/checkout-" + r.ID, BaseBranch: "main", BaseSHA: approval.HeadSHA, HeadSHA: approval.HeadSHA,
		SourceSHA256: approval.SourceSHA256, PreviewSHA256: r.SHA256, PullNumber: 7, PullURL: "https://github.com/acme/repo/pull/7",
	}
	if err := s.PutOptimisationPublication(context.Background(), publication, optimisationAudit()); err != nil {
		t.Fatal(err)
	}
	return r, publication
}

func TestOptimisationPublicationNamesThePullRequestAndIsIdempotentAndAudited(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	r, publication := optimisationPublicationFixture(t, s)
	got, err := s.GetOptimisationPreview(ctx, r.InstallationID, r.Repository, r.ID)
	if err != nil || got.Publication == nil || got.Publication.PullNumber != 7 || got.Publication.PullURL != publication.PullURL || got.Publication.Branch != publication.Branch || got.Publication.BaseBranch != "main" || got.Publication.PublishedBy != "usr_actor" || !got.Publication.CreatedAt.Equal(publication.CreatedAt) {
		t.Fatalf("publication %+v, %v", got.Publication, err)
	}
	if got.Approval == nil || got.Approval.HeadSHA != got.Publication.HeadSHA || got.Approval.SourceSHA256 != got.Publication.SourceSHA256 {
		t.Fatal("the publication is not bound to the approval")
	}
	// A retry after an uncertain successful response records nothing new.
	if err := s.PutOptimisationPublication(ctx, publication, optimisationAudit()); err != nil {
		t.Fatal(err)
	}
	events, total, err := s.ListAudit(ctx, AuditFilter{Actions: []string{"optimisation.preview_published"}, TargetID: r.ID}, Page{Limit: 10})
	if err != nil || total != 1 || events[0].ActorID != "usr_actor" {
		t.Fatalf("publication audit %+v total=%d err=%v", events, total, err)
	}
	// A different pull request for the same preview is a conflict, not a second
	// publication.
	changed := *publication
	changed.PullNumber, changed.PullURL = 8, "https://github.com/acme/repo/pull/8"
	if err := s.PutOptimisationPublication(ctx, &changed, optimisationAudit()); !errors.Is(err, ErrConflict) {
		t.Fatalf("a second publication was accepted: %v", err)
	}
	// A publication without an approval is refused even if every other field is
	// correct.
	inst := &Installation{AppID: 1, InstallationID: 3, Target: "acme/other", TargetType: TargetRepo}
	if err := s.CreateInstallation(ctx, inst); err != nil {
		t.Fatal(err)
	}
	other := &OptimisationPreview{ID: NewID(PrefixOptimisationPreview), InstallationID: inst.ID, Repository: inst.Target, InitiatingUser: "usr_actor", Version: OptimisationPreviewVersion, Payload: json.RawMessage(`{"version":"checkout-retained-v1"}`)}
	if err := s.PutOptimisationPreview(ctx, other, optimisationAudit()); err != nil {
		t.Fatal(err)
	}
	unapproved := *publication
	unapproved.PreviewID, unapproved.InstallationID, unapproved.Repository = other.ID, other.InstallationID, other.Repository
	unapproved.PreviewSHA256 = other.SHA256
	if err := s.PutOptimisationPublication(ctx, &unapproved, optimisationAudit()); !errors.Is(err, ErrConflict) {
		t.Fatalf("an unapproved preview was published: %v", err)
	}
}

func TestOptimisationPublicationRejectsInvalidInput(t *testing.T) {
	s := newTestStore(t)
	_, publication := optimisationPublicationFixture(t, s)
	ctx := context.Background()
	for _, mode := range []string{"actor", "preview", "branch", "base", "url", "number", "hash", "approval"} {
		changed := *publication
		actor := optimisationAudit()
		switch mode {
		case "actor":
			actor.ActorID = "somebody-else"
		case "preview":
			changed.PreviewSHA256 = strings.Repeat("c", 64)
		case "branch":
			changed.Branch = "not a branch"
		case "base":
			changed.BaseSHA = strings.Repeat("d", 40)
		case "url":
			changed.PullURL = "not a url"
		case "number":
			changed.PullNumber = 0
		case "hash":
			changed.SourceSHA256 = "short"
		case "approval":
			changed.SourceSHA256 = strings.Repeat("e", 64)
		}
		err := s.PutOptimisationPublication(ctx, &changed, actor)
		switch mode {
		case "actor", "branch", "url", "number", "hash":
			if !errors.Is(err, ErrInvalidOptimisationPublication) {
				t.Fatalf("%s: %v", mode, err)
			}
		case "preview":
			if !errors.Is(err, ErrConflict) {
				t.Fatalf("%s: %v", mode, err)
			}
		case "base", "approval":
			if !errors.Is(err, ErrConflict) {
				t.Fatalf("%s: %v", mode, err)
			}
		}
	}
	if err := s.PutOptimisationPublication(ctx, nil, optimisationAudit()); !errors.Is(err, ErrInvalidOptimisationPublication) {
		t.Fatalf("nil publication: %v", err)
	}
}

func TestOptimisationPublicationExpiresWithThePreviewAndCascades(t *testing.T) {
	now := time.Date(2026, 10, 23, 10, 0, 0, 0, time.UTC)
	s := newTestStoreAt(t, func() time.Time { return now })
	r, publication := optimisationPublicationFixture(t, s)
	ctx := context.Background()
	now = now.Add(OptimisationPreviewRetention)
	if _, err := s.GetOptimisationPreview(ctx, r.InstallationID, r.Repository, r.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("an expired publication remained readable: %v", err)
	}
	if err := s.PutOptimisationPublication(ctx, publication, optimisationAudit()); !errors.Is(err, ErrNotFound) {
		t.Fatalf("an expired preview accepted publication: %v", err)
	}

	// Eviction and installation deletion cascade like the approval.
	s2 := newTestStore(t)
	r2, _ := optimisationPublicationFixture(t, s2)
	if _, err := s2.DeleteInstallation(ctx, r2.InstallationID); err != nil {
		t.Fatal(err)
	}
	var publications int
	if err := s2.read.QueryRowContext(ctx, `SELECT COUNT(*) FROM optimisation_publications WHERE preview_id = ?`, r2.ID).Scan(&publications); err != nil || publications != 0 {
		t.Fatalf("installation deletion left a publication: %d, %v", publications, err)
	}
}

func TestOptimisationPublicationRollsBackWhenItsAuditCannotBeWritten(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	r := optimisationPreviewFixture(t, s)
	if err := s.PutOptimisationPreview(ctx, r, optimisationAudit()); err != nil {
		t.Fatal(err)
	}
	approval, err := s.ApproveOptimisationPreview(ctx, r.InstallationID, r.Repository, r.ID, strings.Repeat("a", 40), strings.Repeat("b", 64), r.SHA256, optimisationAudit())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.exec(ctx, `CREATE TRIGGER reject_publication_audit BEFORE INSERT ON audit_events WHEN NEW.action = 'optimisation.preview_published' BEGIN SELECT RAISE(FAIL,'audit unavailable'); END`); err != nil {
		t.Fatal(err)
	}
	publication := &OptimisationPublication{
		PreviewID: r.ID, InstallationID: r.InstallationID, Repository: r.Repository, PublishedBy: "usr_actor",
		Branch: "zoomies/checkout-" + r.ID, BaseBranch: "main", BaseSHA: approval.HeadSHA, HeadSHA: approval.HeadSHA,
		SourceSHA256: approval.SourceSHA256, PreviewSHA256: r.SHA256, PullNumber: 7, PullURL: "https://github.com/acme/repo/pull/7",
	}
	if err := s.PutOptimisationPublication(ctx, publication, optimisationAudit()); err == nil {
		t.Fatal("a publication without its audit row was accepted")
	}
	var publications int
	if err := s.read.QueryRowContext(ctx, `SELECT COUNT(*) FROM optimisation_publications WHERE preview_id = ?`, r.ID).Scan(&publications); err != nil || publications != 0 {
		t.Fatalf("an unaudited publication was committed: %d, %v", publications, err)
	}
	if _, err := s.exec(ctx, `DROP TRIGGER reject_publication_audit`); err != nil {
		t.Fatal(err)
	}
	if err := s.PutOptimisationPublication(ctx, publication, optimisationAudit()); err != nil {
		t.Fatal(err)
	}
}

func TestOptimisationPreviewRefusesACorruptPublication(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	r, _ := optimisationPublicationFixture(t, s)
	if _, err := s.exec(ctx, `UPDATE optimisation_publications SET pull_url = ? WHERE preview_id = ?`, "not a url", r.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.GetOptimisationPreview(ctx, r.InstallationID, r.Repository, r.ID); !errors.Is(err, ErrInvalidOptimisationPreview) {
		t.Fatalf("a corrupt publication was served: %v", err)
	}
}
