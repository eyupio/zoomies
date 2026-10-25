package store

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"
)

func kennelFixPreviewFixture(t *testing.T, s *Store) *KennelFixPreview {
	t.Helper()
	inst := &Installation{AppID: 1, InstallationID: 2, Target: "acme/repo", TargetType: TargetRepo}
	if err := s.CreateInstallation(context.Background(), inst); err != nil {
		t.Fatal(err)
	}
	return &KennelFixPreview{ID: NewID(PrefixKennelFixPreview), InstallationID: inst.ID, Repository: inst.Target, InitiatingUser: "usr_actor", Version: KennelFixPreviewVersion, Payload: json.RawMessage(`{"version":"kennel-fix-retained-v1","evidence":"sample"}`)}
}

func TestKennelFixPreviewIsImmutableScopedAndAudited(t *testing.T) {
	s := newTestStore(t)
	r := kennelFixPreviewFixture(t, s)
	ctx := context.Background()
	if err := s.PutKennelFixPreview(ctx, r, optimisationAudit()); err != nil {
		t.Fatal(err)
	}
	first, err := s.GetKennelFixPreview(ctx, r.InstallationID, r.Repository, r.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.PutKennelFixPreview(ctx, r, optimisationAudit()); err != nil {
		t.Fatal(err)
	}
	events, total, err := s.ListAudit(ctx, AuditFilter{TargetID: r.ID}, Page{Limit: 10})
	if err != nil || total != 1 || events[0].Action != "kennel.fix_preview_retained" {
		t.Fatalf("audit %+v total=%d err=%v", events, total, err)
	}
	r.Payload = json.RawMessage(`{"version":"kennel-fix-retained-v1","evidence":"changed"}`)
	if err := s.PutKennelFixPreview(ctx, r, optimisationAudit()); !errors.Is(err, ErrConflict) {
		t.Fatal(err)
	}
	after, err := s.GetKennelFixPreview(ctx, r.InstallationID, r.Repository, r.ID)
	if err != nil || string(after.Payload) != string(first.Payload) {
		t.Fatal("immutable record changed", err)
	}
	if _, err := s.GetKennelFixPreview(ctx, "other", r.Repository, r.ID); !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
}

func TestKennelFixPreviewExpiryDoesNotRenew(t *testing.T) {
	now := time.Date(2026, 10, 24, 0, 0, 0, 0, time.UTC)
	s := newTestStoreAt(t, func() time.Time { return now })
	r := kennelFixPreviewFixture(t, s)
	ctx := context.Background()
	if err := s.PutKennelFixPreview(ctx, r, optimisationAudit()); err != nil {
		t.Fatal(err)
	}
	now = now.Add(KennelFixPreviewRetention)
	if _, err := s.GetKennelFixPreview(ctx, r.InstallationID, r.Repository, r.ID); !errors.Is(err, ErrNotFound) {
		t.Fatal("expired preview visible")
	}
	if err := s.PutKennelFixPreview(ctx, r, optimisationAudit()); !errors.Is(err, ErrNotFound) {
		t.Fatal("retry renewed expiry")
	}
}

func TestKennelFixApprovalBindsThePreviewAndDoesNotPublish(t *testing.T) {
	s := newTestStore(t)
	r := kennelFixPreviewFixture(t, s)
	ctx := context.Background()
	if err := s.PutKennelFixPreview(ctx, r, optimisationAudit()); err != nil {
		t.Fatal(err)
	}
	head, source := strings.Repeat("a", 40), strings.Repeat("b", 64)
	approval, err := s.ApproveKennelFixPreview(ctx, r.InstallationID, r.Repository, r.ID, head, source, r.SHA256, optimisationAudit())
	if err != nil {
		t.Fatal(err)
	}
	if approval.ApprovedBy != "usr_actor" || approval.HeadSHA != head || approval.SourceSHA256 != source || approval.PreviewSHA256 != r.SHA256 {
		t.Fatalf("approval %+v", approval)
	}
	// A repeat by the same binding must not write a second audit row.
	again, err := s.ApproveKennelFixPreview(ctx, r.InstallationID, r.Repository, r.ID, head, source, r.SHA256, optimisationAudit())
	if err != nil || !again.CreatedAt.Equal(approval.CreatedAt) {
		t.Fatalf("repeat changed the approval: %+v %v", again, err)
	}
	events, total, err := s.ListAudit(ctx, AuditFilter{TargetID: r.ID}, Page{Limit: 10})
	actions := map[string]bool{}
	for _, e := range events {
		actions[e.Action] = true
		if e.Action == "kennel.fix_preview_approved" && e.TargetKind != "kennel_fix_preview" {
			t.Fatalf("target type %q", e.TargetKind)
		}
	}
	if err != nil || total != 2 || !actions["kennel.fix_preview_approved"] || !actions["kennel.fix_preview_retained"] {
		t.Fatalf("audit %+v total=%d err=%v", events, total, err)
	}
	got, err := s.GetKennelFixPreview(ctx, r.InstallationID, r.Repository, r.ID)
	if err != nil || got.Approval == nil || got.Approval.ApprovedBy != "usr_actor" {
		t.Fatalf("read %+v %v", got, err)
	}
	if _, err := s.ApproveKennelFixPreview(ctx, r.InstallationID, r.Repository, r.ID, head, strings.Repeat("c", 64), r.SHA256, optimisationAudit()); !errors.Is(err, ErrConflict) {
		t.Fatalf("different source accepted: %v", err)
	}
	if _, err := s.ApproveKennelFixPreview(ctx, r.InstallationID, r.Repository, r.ID, strings.Repeat("d", 40), source, r.SHA256, optimisationAudit()); !errors.Is(err, ErrConflict) {
		t.Fatalf("different head accepted: %v", err)
	}
	if _, err := s.ApproveKennelFixPreview(ctx, r.InstallationID, r.Repository, r.ID, head, source, strings.Repeat("e", 64), optimisationAudit()); !errors.Is(err, ErrConflict) {
		t.Fatalf("different preview digest accepted: %v", err)
	}
}

func TestKennelFixApprovalExpiresWithThePreviewAndRefusesForeignScope(t *testing.T) {
	now := time.Date(2026, 10, 24, 0, 0, 0, 0, time.UTC)
	s := newTestStoreAt(t, func() time.Time { return now })
	r := kennelFixPreviewFixture(t, s)
	ctx := context.Background()
	if err := s.PutKennelFixPreview(ctx, r, optimisationAudit()); err != nil {
		t.Fatal(err)
	}
	head, source := strings.Repeat("a", 40), strings.Repeat("b", 64)
	for _, scope := range [][2]string{{"other", r.Repository}, {r.InstallationID, "other/repo"}} {
		if _, err := s.ApproveKennelFixPreview(ctx, scope[0], scope[1], r.ID, head, source, r.SHA256, optimisationAudit()); !errors.Is(err, ErrNotFound) {
			t.Fatalf("foreign scope %v: %v", scope, err)
		}
	}
	now = now.Add(KennelFixPreviewRetention)
	if _, err := s.ApproveKennelFixPreview(ctx, r.InstallationID, r.Repository, r.ID, head, source, r.SHA256, optimisationAudit()); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expired preview approved: %v", err)
	}
}

func TestKennelFixApprovalWithAMismatchedRowIsCorruptEvidence(t *testing.T) {
	s := newTestStore(t)
	r := kennelFixPreviewFixture(t, s)
	ctx := context.Background()
	if err := s.PutKennelFixPreview(ctx, r, optimisationAudit()); err != nil {
		t.Fatal(err)
	}
	if _, err := s.write.ExecContext(ctx, `INSERT INTO kennel_fix_preview_approvals(preview_id,installation_id,repository,approved_by,head_sha,source_sha256,preview_sha256,created_at) VALUES(?,?,?,?,?,?,?,?)`,
		r.ID, r.InstallationID, r.Repository, "usr_actor", strings.Repeat("a", 40), strings.Repeat("b", 64), strings.Repeat("0", 64), 1); err != nil {
		t.Fatal(err)
	}
	if _, err := s.GetKennelFixPreview(ctx, r.InstallationID, r.Repository, r.ID); !errors.Is(err, ErrInvalidKennelFixPreview) {
		t.Fatalf("mismatched approval read as valid: %v", err)
	}
}

func TestKennelFixPublicationRecordsOneApprovedDraft(t *testing.T) {
	s := newTestStore(t)
	r := kennelFixPreviewFixture(t, s)
	ctx := context.Background()
	if err := s.PutKennelFixPreview(ctx, r, optimisationAudit()); err != nil {
		t.Fatal(err)
	}
	head, source := strings.Repeat("a", 40), strings.Repeat("b", 64)
	if _, err := s.ApproveKennelFixPreview(ctx, r.InstallationID, r.Repository, r.ID, head, source, r.SHA256, optimisationAudit()); err != nil {
		t.Fatal(err)
	}
	pub := &KennelFixPublication{
		PreviewID: r.ID, InstallationID: r.InstallationID, Repository: r.Repository, PublishedBy: "usr_actor",
		Branch: "zoomies/kennel-fix-" + r.ID, BaseBranch: "main", BaseSHA: head, HeadSHA: head, SourceSHA256: source, PreviewSHA256: r.SHA256,
		PullNumber: 12, PullURL: "https://github.com/acme/repo/pull/12",
	}
	if err := s.PutKennelFixPublication(ctx, pub, optimisationAudit()); err != nil {
		t.Fatal(err)
	}
	if pub.CreatedAt.IsZero() {
		t.Fatal("publication was not timestamped")
	}
	if err := s.PutKennelFixPublication(ctx, pub, optimisationAudit()); err != nil {
		t.Fatalf("repeat: %v", err)
	}
	got, err := s.GetKennelFixPreview(ctx, r.InstallationID, r.Repository, r.ID)
	if err != nil || got.Publication == nil || got.Publication.PullNumber != 12 || got.Publication.PublishedBy != "usr_actor" {
		t.Fatalf("read %+v %v", got, err)
	}
	events, total, err := s.ListAudit(ctx, AuditFilter{TargetID: r.ID}, Page{Limit: 10})
	published := false
	for _, e := range events {
		published = published || e.Action == "kennel.fix_preview_published" && e.TargetKind == "kennel_fix_preview"
	}
	if err != nil || total != 3 || !published {
		t.Fatalf("audit %+v total=%d err=%v", events, total, err)
	}
	other := *pub
	other.PullURL = "https://github.com/acme/repo/pull/13"
	if err := s.PutKennelFixPublication(ctx, &other, optimisationAudit()); !errors.Is(err, ErrConflict) {
		t.Fatalf("different draft accepted: %v", err)
	}
}

func TestKennelFixPublicationRequiresAnApprovalAndLivePreview(t *testing.T) {
	now := time.Date(2026, 10, 24, 0, 0, 0, 0, time.UTC)
	s := newTestStoreAt(t, func() time.Time { return now })
	r := kennelFixPreviewFixture(t, s)
	ctx := context.Background()
	if err := s.PutKennelFixPreview(ctx, r, optimisationAudit()); err != nil {
		t.Fatal(err)
	}
	head, source := strings.Repeat("a", 40), strings.Repeat("b", 64)
	pub := &KennelFixPublication{PreviewID: r.ID, InstallationID: r.InstallationID, Repository: r.Repository, PublishedBy: "usr_actor", Branch: "zoomies/kennel-fix-" + r.ID, BaseBranch: "main", BaseSHA: head, HeadSHA: head, SourceSHA256: source, PreviewSHA256: r.SHA256, PullNumber: 12, PullURL: "https://github.com/acme/repo/pull/12"}
	if err := s.PutKennelFixPublication(ctx, pub, optimisationAudit()); !errors.Is(err, ErrConflict) {
		t.Fatalf("unapproved preview published: %v", err)
	}
	if _, err := s.ApproveKennelFixPreview(ctx, r.InstallationID, r.Repository, r.ID, head, source, r.SHA256, optimisationAudit()); err != nil {
		t.Fatal(err)
	}
	now = now.Add(KennelFixPreviewRetention)
	if err := s.PutKennelFixPublication(ctx, pub, optimisationAudit()); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expired preview published: %v", err)
	}
}

// A fix is bound to its file and not to the branch head, so a draft may be based
// on a newer head than the one the preview was made at.
func TestKennelFixPublicationMayBeBasedOnANewerHeadThanThePreview(t *testing.T) {
	s := newTestStore(t)
	r := kennelFixPreviewFixture(t, s)
	ctx := context.Background()
	if err := s.PutKennelFixPreview(ctx, r, optimisationAudit()); err != nil {
		t.Fatal(err)
	}
	head, newer, source := strings.Repeat("a", 40), strings.Repeat("c", 40), strings.Repeat("b", 64)
	if _, err := s.ApproveKennelFixPreview(ctx, r.InstallationID, r.Repository, r.ID, head, source, r.SHA256, optimisationAudit()); err != nil {
		t.Fatal(err)
	}
	pub := &KennelFixPublication{
		PreviewID: r.ID, InstallationID: r.InstallationID, Repository: r.Repository, PublishedBy: "usr_actor",
		Branch: "zoomies/kennel-fix-" + r.ID, BaseBranch: "main", BaseSHA: newer, HeadSHA: head, SourceSHA256: source, PreviewSHA256: r.SHA256,
		PullNumber: 12, PullURL: "https://github.com/acme/repo/pull/12",
	}
	if err := s.PutKennelFixPublication(ctx, pub, optimisationAudit()); err != nil {
		t.Fatal(err)
	}
	got, err := s.GetKennelFixPreview(ctx, r.InstallationID, r.Repository, r.ID)
	if err != nil || got.Publication == nil || got.Publication.BaseSHA != newer || got.Publication.HeadSHA != head {
		t.Fatalf("publication %+v err=%v", got.Publication, err)
	}
	// What was approved is still what is published: a different head is refused.
	other := *pub
	other.HeadSHA = newer
	if err := s.PutKennelFixPublication(ctx, &other, optimisationAudit()); !errors.Is(err, ErrConflict) {
		t.Fatalf("a publication of a head nobody approved: %v", err)
	}
}
