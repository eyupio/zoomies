package store

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"
)

func shardingPreviewFixture(t *testing.T, s *Store) *OptimisationShardingPreview {
	t.Helper()
	inst := &Installation{AppID: 1, InstallationID: 2, Target: "acme/repo", TargetType: TargetRepo}
	if err := s.CreateInstallation(context.Background(), inst); err != nil {
		t.Fatal(err)
	}
	return &OptimisationShardingPreview{ID: NewID(PrefixOptimisationShardingPreview), InstallationID: inst.ID, Repository: inst.Target, InitiatingUser: "usr_actor", Version: OptimisationShardingPreviewVersion, Payload: json.RawMessage(`{"version":"sharding-retained-v1","evidence":"sample"}`)}
}

func TestOptimisationShardingPreviewIsImmutableScopedAndAudited(t *testing.T) {
	s := newTestStore(t)
	r := shardingPreviewFixture(t, s)
	ctx := context.Background()
	if err := s.PutOptimisationShardingPreview(ctx, r, optimisationAudit()); err != nil {
		t.Fatal(err)
	}
	first, err := s.GetOptimisationShardingPreview(ctx, r.InstallationID, r.Repository, r.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.PutOptimisationShardingPreview(ctx, r, optimisationAudit()); err != nil {
		t.Fatal(err)
	}
	events, total, err := s.ListAudit(ctx, AuditFilter{TargetID: r.ID}, Page{Limit: 10})
	if err != nil || total != 1 || events[0].Action != "optimisation.sharding_preview_retained" {
		t.Fatalf("audit %+v total=%d err=%v", events, total, err)
	}
	r.Payload = json.RawMessage(`{"version":"sharding-retained-v1","evidence":"changed"}`)
	if err := s.PutOptimisationShardingPreview(ctx, r, optimisationAudit()); !errors.Is(err, ErrConflict) {
		t.Fatal(err)
	}
	after, err := s.GetOptimisationShardingPreview(ctx, r.InstallationID, r.Repository, r.ID)
	if err != nil || string(after.Payload) != string(first.Payload) {
		t.Fatal("immutable record changed", err)
	}
	if _, err := s.GetOptimisationShardingPreview(ctx, "other", r.Repository, r.ID); !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
}

func TestOptimisationShardingPreviewExpiryDoesNotRenew(t *testing.T) {
	now := time.Date(2026, 10, 24, 0, 0, 0, 0, time.UTC)
	s := newTestStoreAt(t, func() time.Time { return now })
	r := shardingPreviewFixture(t, s)
	ctx := context.Background()
	if err := s.PutOptimisationShardingPreview(ctx, r, optimisationAudit()); err != nil {
		t.Fatal(err)
	}
	now = now.Add(OptimisationShardingPreviewRetention)
	if _, err := s.GetOptimisationShardingPreview(ctx, r.InstallationID, r.Repository, r.ID); !errors.Is(err, ErrNotFound) {
		t.Fatal("expired preview visible")
	}
	if err := s.PutOptimisationShardingPreview(ctx, r, optimisationAudit()); !errors.Is(err, ErrNotFound) {
		t.Fatal("retry renewed expiry")
	}
}
