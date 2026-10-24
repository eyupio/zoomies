package store

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"
)

func readinessPreviewFixture(t *testing.T, s *Store) *OptimisationReadinessPreview {
	t.Helper()
	inst := &Installation{AppID: 1, InstallationID: 2, Target: "acme/repo", TargetType: TargetRepo}
	if err := s.CreateInstallation(context.Background(), inst); err != nil {
		t.Fatal(err)
	}
	return &OptimisationReadinessPreview{ID: NewID(PrefixOptimisationReadinessPreview), InstallationID: inst.ID, Repository: inst.Target, InitiatingUser: "usr_actor", Version: OptimisationReadinessPreviewVersion, Payload: json.RawMessage(`{"version":"readiness-retained-v1","evidence":"sample"}`)}
}

func TestOptimisationReadinessPreviewIsImmutableScopedAndAudited(t *testing.T) {
	s := newTestStore(t)
	r := readinessPreviewFixture(t, s)
	ctx := context.Background()
	if err := s.PutOptimisationReadinessPreview(ctx, r, optimisationAudit()); err != nil {
		t.Fatal(err)
	}
	first, err := s.GetOptimisationReadinessPreview(ctx, r.InstallationID, r.Repository, r.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.PutOptimisationReadinessPreview(ctx, r, optimisationAudit()); err != nil {
		t.Fatal(err)
	}
	events, total, err := s.ListAudit(ctx, AuditFilter{TargetID: r.ID}, Page{Limit: 10})
	if err != nil || total != 1 || events[0].Action != "optimisation.readiness_preview_retained" {
		t.Fatalf("audit %+v total=%d err=%v", events, total, err)
	}
	r.Payload = json.RawMessage(`{"version":"readiness-retained-v1","evidence":"changed"}`)
	if err := s.PutOptimisationReadinessPreview(ctx, r, optimisationAudit()); !errors.Is(err, ErrConflict) {
		t.Fatal(err)
	}
	after, err := s.GetOptimisationReadinessPreview(ctx, r.InstallationID, r.Repository, r.ID)
	if err != nil || string(after.Payload) != string(first.Payload) {
		t.Fatal("immutable record changed", err)
	}
	if _, err := s.GetOptimisationReadinessPreview(ctx, "other", r.Repository, r.ID); !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
}

func TestOptimisationReadinessPreviewExpiryDoesNotRenew(t *testing.T) {
	now := time.Date(2026, 10, 24, 0, 0, 0, 0, time.UTC)
	s := newTestStoreAt(t, func() time.Time { return now })
	r := readinessPreviewFixture(t, s)
	ctx := context.Background()
	if err := s.PutOptimisationReadinessPreview(ctx, r, optimisationAudit()); err != nil {
		t.Fatal(err)
	}
	now = now.Add(OptimisationReadinessPreviewRetention)
	if _, err := s.GetOptimisationReadinessPreview(ctx, r.InstallationID, r.Repository, r.ID); !errors.Is(err, ErrNotFound) {
		t.Fatal("expired preview visible")
	}
	if err := s.PutOptimisationReadinessPreview(ctx, r, optimisationAudit()); !errors.Is(err, ErrNotFound) {
		t.Fatal("retry renewed expiry")
	}
}
