package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"
)

func optimisationCachePreviewFixture(t *testing.T, s *Store) *OptimisationCachePreview {
	t.Helper()
	inst := &Installation{AppID: 1, InstallationID: 2, Target: "acme/repo", TargetType: TargetRepo}
	if err := s.CreateInstallation(context.Background(), inst); err != nil {
		t.Fatal(err)
	}
	return &OptimisationCachePreview{ID: NewID(PrefixOptimisationCachePreview), InstallationID: inst.ID, Repository: inst.Target, InitiatingUser: "usr_actor", Version: OptimisationCachePreviewVersion, Payload: json.RawMessage(`{"version":"cache-retained-v1","evidence":"sample"}`)}
}

func TestOptimisationCachePreviewIsImmutableScopedAndAudited(t *testing.T) {
	s := newTestStore(t)
	r := optimisationCachePreviewFixture(t, s)
	ctx := context.Background()
	if err := s.PutOptimisationCachePreview(ctx, r, optimisationAudit()); err != nil {
		t.Fatal(err)
	}
	first, err := s.GetOptimisationCachePreview(ctx, r.InstallationID, r.Repository, r.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.PutOptimisationCachePreview(ctx, r, optimisationAudit()); err != nil {
		t.Fatal(err)
	}
	events, total, err := s.ListAudit(ctx, AuditFilter{TargetID: r.ID}, Page{Limit: 10})
	if err != nil || total != 1 || events[0].Action != "optimisation.cache_preview_retained" {
		t.Fatalf("audit %+v total=%d err=%v", events, total, err)
	}
	r.Payload = json.RawMessage(`{"version":"cache-retained-v1","evidence":"changed"}`)
	if err := s.PutOptimisationCachePreview(ctx, r, optimisationAudit()); !errors.Is(err, ErrConflict) {
		t.Fatal(err)
	}
	after, err := s.GetOptimisationCachePreview(ctx, r.InstallationID, r.Repository, r.ID)
	if err != nil || string(after.Payload) != string(first.Payload) || !after.CreatedAt.Equal(first.CreatedAt) {
		t.Fatal("immutable record changed", err)
	}
	if _, err := s.GetOptimisationCachePreview(ctx, "other", r.Repository, r.ID); !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
	if _, err := s.GetOptimisationCachePreview(ctx, r.InstallationID, "other/repo", r.ID); !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
}

func TestOptimisationCachePreviewBoundsAndExpiryDoNotRenewHistory(t *testing.T) {
	now := time.Date(2026, 10, 23, 0, 0, 0, 0, time.UTC)
	s := newTestStoreAt(t, func() time.Time { return now })
	r := optimisationCachePreviewFixture(t, s)
	ctx := context.Background()
	firstID := r.ID
	for i := 0; i < MaxCachePreviewsPerInstallation+1; i++ {
		r.ID = NewID(PrefixOptimisationCachePreview)
		if i == 0 {
			firstID = r.ID
		}
		if err := s.PutOptimisationCachePreview(ctx, r, optimisationAudit()); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := s.GetOptimisationCachePreview(ctx, r.InstallationID, r.Repository, firstID); !errors.Is(err, ErrNotFound) {
		t.Fatal("oldest record not pruned", err)
	}
	now = now.Add(OptimisationCachePreviewRetention)
	if _, err := s.GetOptimisationCachePreview(ctx, r.InstallationID, r.Repository, r.ID); !errors.Is(err, ErrNotFound) {
		t.Fatal("expired record visible", err)
	}
	if err := s.PutOptimisationCachePreview(ctx, r, optimisationAudit()); !errors.Is(err, ErrNotFound) {
		t.Fatal("retry renewed expiry", err)
	}
}

func TestOptimisationCachePreviewRejectsInvalidPayloadScopeAndActor(t *testing.T) {
	s := newTestStore(t)
	original := optimisationCachePreviewFixture(t, s)
	for _, mode := range []string{"oversized", "null", "version", "scope", "actor"} {
		r := *original
		actor := optimisationAudit()
		switch mode {
		case "oversized":
			r.Payload = json.RawMessage(fmt.Sprintf(`{"version":"cache-retained-v1","data":%q}`, strings.Repeat("x", MaxOptimisationCachePreviewBytes)))
		case "null":
			r.Payload = json.RawMessage(`null`)
		case "version":
			r.Version = "future"
		case "scope":
			r.Repository = "other/repo"
		case "actor":
			actor.ActorID = "other"
		}
		if err := s.PutOptimisationCachePreview(context.Background(), &r, actor); !errors.Is(err, ErrInvalidOptimisationCachePreview) {
			t.Fatalf("%s: %v", mode, err)
		}
	}
}

func TestOptimisationCachePreviewGlobalBoundAndInstallationDeletion(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	var firstID, firstInstallation string
	last := &OptimisationCachePreview{}
	for i := 0; i < MaxRetainedCachePreviews+1; i++ {
		if i%MaxCachePreviewsPerInstallation == 0 {
			inst := &Installation{AppID: 1, InstallationID: int64(i + 10), Target: fmt.Sprintf("acme/cache%d", i), TargetType: TargetRepo}
			if err := s.CreateInstallation(ctx, inst); err != nil {
				t.Fatal(err)
			}
			last.InstallationID = inst.ID
			last.Repository = inst.Target
		}
		last.ID = NewID(PrefixOptimisationCachePreview)
		last.InitiatingUser = "usr_actor"
		last.Version = OptimisationCachePreviewVersion
		last.Payload = json.RawMessage(`{"version":"cache-retained-v1"}`)
		if i == 0 {
			firstID = last.ID
			firstInstallation = last.InstallationID
		}
		if err := s.PutOptimisationCachePreview(ctx, last, optimisationAudit()); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := s.GetOptimisationCachePreview(ctx, firstInstallation, "acme/cache0", firstID); !errors.Is(err, ErrNotFound) {
		t.Fatal("global cap failed", err)
	}
	if _, err := s.DeleteInstallation(ctx, last.InstallationID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.GetOptimisationCachePreview(ctx, last.InstallationID, last.Repository, last.ID); !errors.Is(err, ErrNotFound) {
		t.Fatal("deleted installation left evidence", err)
	}
}

func TestOptimisationCachePreviewDetectsCorruptionAndRollsBackAuditFailure(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	r := optimisationCachePreviewFixture(t, s)
	if _, err := s.exec(ctx, `CREATE TRIGGER reject_cache_preview_audit BEFORE INSERT ON audit_events WHEN NEW.action = 'optimisation.cache_preview_retained' BEGIN SELECT RAISE(FAIL,'audit unavailable'); END`); err != nil {
		t.Fatal(err)
	}
	if err := s.PutOptimisationCachePreview(ctx, r, optimisationAudit()); err == nil {
		t.Fatal("failed audit accepted")
	}
	if _, err := s.GetOptimisationCachePreview(ctx, r.InstallationID, r.Repository, r.ID); !errors.Is(err, ErrNotFound) {
		t.Fatal("unaudited row committed")
	}
	if _, err := s.exec(ctx, `DROP TRIGGER reject_cache_preview_audit`); err != nil {
		t.Fatal(err)
	}
	if err := s.PutOptimisationCachePreview(ctx, r, optimisationAudit()); err != nil {
		t.Fatal(err)
	}
	if _, err := s.exec(ctx, `UPDATE optimisation_cache_previews SET payload = ? WHERE id = ?`, []byte(`{"version":"cache-retained-v1","corrupt":true}`), r.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.GetOptimisationCachePreview(ctx, r.InstallationID, r.Repository, r.ID); !errors.Is(err, ErrInvalidOptimisationCachePreview) {
		t.Fatal("corruption accepted", err)
	}
	if err := s.PutOptimisationCachePreview(ctx, r, optimisationAudit()); !errors.Is(err, ErrInvalidOptimisationCachePreview) {
		t.Fatal("corrupt retry accepted", err)
	}
}

func TestOptimisationCachePreviewSurvivesAReadOnlyBackupWithItsAudit(t *testing.T) {
	ctx := context.Background()
	s, _ := onDiskStore(t)
	r := optimisationCachePreviewFixture(t, s)
	if err := s.PutOptimisationCachePreview(ctx, r, optimisationAudit()); err != nil {
		t.Fatal(err)
	}
	dest := t.TempDir() + "/cache-preview-backup.db"
	if err := s.Backup(ctx, dest); err != nil {
		t.Fatal(err)
	}
	copy, err := Open(ctx, Options{Path: dest, ReadOnly: true})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { copy.Close() })
	got, err := copy.GetOptimisationCachePreview(ctx, r.InstallationID, r.Repository, r.ID)
	if err != nil || got.SHA256 != r.SHA256 || string(got.Payload) != string(r.Payload) {
		t.Fatal("backup changed private cache preview", err)
	}
	_, total, err := copy.ListAudit(ctx, AuditFilter{TargetID: r.ID}, Page{Limit: 10})
	if err != nil || total != 1 {
		t.Fatal("backup lost cache preview audit", err)
	}
}

func TestOptimisationCacheApprovalBindsThePreviewAndDoesNotPublish(t *testing.T) {
	s := newTestStore(t)
	r := optimisationCachePreviewFixture(t, s)
	ctx := context.Background()
	if err := s.PutOptimisationCachePreview(ctx, r, optimisationAudit()); err != nil {
		t.Fatal(err)
	}
	head, source := strings.Repeat("a", 40), strings.Repeat("b", 64)
	approval, err := s.ApproveOptimisationCachePreview(ctx, r.InstallationID, r.Repository, r.ID, head, source, r.SHA256, optimisationAudit())
	if err != nil {
		t.Fatal(err)
	}
	if approval.ApprovedBy != "usr_actor" || approval.HeadSHA != head || approval.SourceSHA256 != source || approval.PreviewSHA256 != r.SHA256 {
		t.Fatalf("approval %+v", approval)
	}
	again, err := s.ApproveOptimisationCachePreview(ctx, r.InstallationID, r.Repository, r.ID, head, source, r.SHA256, optimisationAudit())
	if err != nil || !again.CreatedAt.Equal(approval.CreatedAt) {
		t.Fatalf("repeat changed the approval: %+v %v", again, err)
	}
	events, total, err := s.ListAudit(ctx, AuditFilter{TargetID: r.ID}, Page{Limit: 10})
	actions := map[string]bool{}
	for _, e := range events {
		actions[e.Action] = true
	}
	if err != nil || total != 2 || !actions["optimisation.cache_preview_approved"] || !actions["optimisation.cache_preview_retained"] {
		t.Fatalf("audit %+v total=%d err=%v", events, total, err)
	}
	got, err := s.GetOptimisationCachePreview(ctx, r.InstallationID, r.Repository, r.ID)
	if err != nil || got.Approval == nil || got.Approval.ApprovedBy != "usr_actor" {
		t.Fatalf("read %+v %v", got, err)
	}
	if _, err := s.ApproveOptimisationCachePreview(ctx, r.InstallationID, r.Repository, r.ID, head, strings.Repeat("c", 64), r.SHA256, optimisationAudit()); !errors.Is(err, ErrConflict) {
		t.Fatalf("different source accepted: %v", err)
	}
}

func TestOptimisationCacheApprovalExpiresWithThePreviewAndRefusesForeignScope(t *testing.T) {
	now := time.Date(2026, 10, 23, 0, 0, 0, 0, time.UTC)
	s := newTestStoreAt(t, func() time.Time { return now })
	r := optimisationCachePreviewFixture(t, s)
	ctx := context.Background()
	if err := s.PutOptimisationCachePreview(ctx, r, optimisationAudit()); err != nil {
		t.Fatal(err)
	}
	for _, scope := range [][2]string{{"other", r.Repository}, {r.InstallationID, "other/repo"}} {
		if _, err := s.ApproveOptimisationCachePreview(ctx, scope[0], scope[1], r.ID, strings.Repeat("a", 40), strings.Repeat("b", 64), r.SHA256, optimisationAudit()); !errors.Is(err, ErrNotFound) {
			t.Fatalf("foreign scope %v: %v", scope, err)
		}
	}
	now = now.Add(OptimisationCachePreviewRetention)
	if _, err := s.ApproveOptimisationCachePreview(ctx, r.InstallationID, r.Repository, r.ID, strings.Repeat("a", 40), strings.Repeat("b", 64), r.SHA256, optimisationAudit()); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expired preview approved: %v", err)
	}
}

func TestOptimisationCachePublicationRecordsOneApprovedDraft(t *testing.T) {
	s := newTestStore(t)
	r := optimisationCachePreviewFixture(t, s)
	ctx := context.Background()
	if err := s.PutOptimisationCachePreview(ctx, r, optimisationAudit()); err != nil {
		t.Fatal(err)
	}
	head, source := strings.Repeat("a", 40), strings.Repeat("b", 64)
	if _, err := s.ApproveOptimisationCachePreview(ctx, r.InstallationID, r.Repository, r.ID, head, source, r.SHA256, optimisationAudit()); err != nil {
		t.Fatal(err)
	}
	pub := &OptimisationCachePublication{
		PreviewID: r.ID, InstallationID: r.InstallationID, Repository: r.Repository, PublishedBy: "usr_actor",
		Branch: "zoomies/cache-" + r.ID, BaseBranch: "main", BaseSHA: head, HeadSHA: head, SourceSHA256: source, PreviewSHA256: r.SHA256,
		PullNumber: 12, PullURL: "https://github.com/acme/repo/pull/12",
	}
	if err := s.PutOptimisationCachePublication(ctx, pub, optimisationAudit()); err != nil {
		t.Fatal(err)
	}
	if pub.CreatedAt.IsZero() {
		t.Fatal("publication was not timestamped")
	}
	if err := s.PutOptimisationCachePublication(ctx, pub, optimisationAudit()); err != nil {
		t.Fatalf("repeat: %v", err)
	}
	got, err := s.GetOptimisationCachePreview(ctx, r.InstallationID, r.Repository, r.ID)
	if err != nil || got.Publication == nil || got.Publication.PullNumber != 12 || got.Publication.PublishedBy != "usr_actor" {
		t.Fatalf("read %+v %v", got, err)
	}
	events, total, err := s.ListAudit(ctx, AuditFilter{TargetID: r.ID}, Page{Limit: 10})
	actions := map[string]bool{}
	for _, e := range events {
		actions[e.Action] = true
	}
	if err != nil || total != 3 || !actions["optimisation.cache_preview_published"] || !actions["optimisation.cache_preview_approved"] || !actions["optimisation.cache_preview_retained"] {
		t.Fatalf("audit %+v total=%d err=%v", events, total, err)
	}
	other := *pub
	other.PullURL = "https://github.com/acme/repo/pull/13"
	if err := s.PutOptimisationCachePublication(ctx, &other, optimisationAudit()); !errors.Is(err, ErrConflict) {
		t.Fatalf("different draft accepted: %v", err)
	}
}

func TestOptimisationCachePublicationRequiresAnApprovalAndLivePreview(t *testing.T) {
	now := time.Date(2026, 10, 23, 0, 0, 0, 0, time.UTC)
	s := newTestStoreAt(t, func() time.Time { return now })
	r := optimisationCachePreviewFixture(t, s)
	ctx := context.Background()
	if err := s.PutOptimisationCachePreview(ctx, r, optimisationAudit()); err != nil {
		t.Fatal(err)
	}
	head, source := strings.Repeat("a", 40), strings.Repeat("b", 64)
	pub := &OptimisationCachePublication{PreviewID: r.ID, InstallationID: r.InstallationID, Repository: r.Repository, PublishedBy: "usr_actor", Branch: "zoomies/cache-" + r.ID, BaseBranch: "main", BaseSHA: head, HeadSHA: head, SourceSHA256: source, PreviewSHA256: r.SHA256, PullNumber: 12, PullURL: "https://github.com/acme/repo/pull/12"}
	if err := s.PutOptimisationCachePublication(ctx, pub, optimisationAudit()); !errors.Is(err, ErrConflict) {
		t.Fatalf("unapproved preview published: %v", err)
	}
	if _, err := s.ApproveOptimisationCachePreview(ctx, r.InstallationID, r.Repository, r.ID, head, source, r.SHA256, optimisationAudit()); err != nil {
		t.Fatal(err)
	}
	now = now.Add(OptimisationCachePreviewRetention)
	if err := s.PutOptimisationCachePublication(ctx, pub, optimisationAudit()); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expired preview published: %v", err)
	}
}
