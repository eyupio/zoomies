package store

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
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

func TestOptimisationShardingApprovalBindsThePreviewAndDoesNotPublish(t *testing.T) {
	s := newTestStore(t)
	r := shardingPreviewFixture(t, s)
	ctx := context.Background()
	if err := s.PutOptimisationShardingPreview(ctx, r, optimisationAudit()); err != nil {
		t.Fatal(err)
	}
	head, source := strings.Repeat("a", 40), strings.Repeat("b", 64)
	approval, err := s.ApproveOptimisationShardingPreview(ctx, r.InstallationID, r.Repository, r.ID, head, source, r.SHA256, optimisationAudit())
	if err != nil {
		t.Fatal(err)
	}
	if approval.ApprovedBy != "usr_actor" || approval.HeadSHA != head || approval.SourceSHA256 != source || approval.PreviewSHA256 != r.SHA256 {
		t.Fatalf("approval %+v", approval)
	}
	// A repeat by the same binding must not write a second audit row.
	again, err := s.ApproveOptimisationShardingPreview(ctx, r.InstallationID, r.Repository, r.ID, head, source, r.SHA256, optimisationAudit())
	if err != nil || !again.CreatedAt.Equal(approval.CreatedAt) {
		t.Fatalf("repeat changed the approval: %+v %v", again, err)
	}
	events, total, err := s.ListAudit(ctx, AuditFilter{TargetID: r.ID}, Page{Limit: 10})
	actions := map[string]bool{}
	for _, e := range events {
		actions[e.Action] = true
		if e.Action == "optimisation.sharding_preview_approved" && e.TargetKind != "optimisation_sharding_preview" {
			t.Fatalf("target type %q", e.TargetKind)
		}
	}
	if err != nil || total != 2 || !actions["optimisation.sharding_preview_approved"] || !actions["optimisation.sharding_preview_retained"] {
		t.Fatalf("audit %+v total=%d err=%v", events, total, err)
	}
	got, err := s.GetOptimisationShardingPreview(ctx, r.InstallationID, r.Repository, r.ID)
	if err != nil || got.Approval == nil || got.Approval.ApprovedBy != "usr_actor" {
		t.Fatalf("read %+v %v", got, err)
	}
	if _, err := s.ApproveOptimisationShardingPreview(ctx, r.InstallationID, r.Repository, r.ID, head, strings.Repeat("c", 64), r.SHA256, optimisationAudit()); !errors.Is(err, ErrConflict) {
		t.Fatalf("different source accepted: %v", err)
	}
	if _, err := s.ApproveOptimisationShardingPreview(ctx, r.InstallationID, r.Repository, r.ID, strings.Repeat("d", 40), source, r.SHA256, optimisationAudit()); !errors.Is(err, ErrConflict) {
		t.Fatalf("different head accepted: %v", err)
	}
	if _, err := s.ApproveOptimisationShardingPreview(ctx, r.InstallationID, r.Repository, r.ID, head, source, strings.Repeat("e", 64), optimisationAudit()); !errors.Is(err, ErrConflict) {
		t.Fatalf("different preview digest accepted: %v", err)
	}
}

func TestOptimisationShardingApprovalExpiresWithThePreviewAndRefusesForeignScope(t *testing.T) {
	now := time.Date(2026, 10, 24, 0, 0, 0, 0, time.UTC)
	s := newTestStoreAt(t, func() time.Time { return now })
	r := shardingPreviewFixture(t, s)
	ctx := context.Background()
	if err := s.PutOptimisationShardingPreview(ctx, r, optimisationAudit()); err != nil {
		t.Fatal(err)
	}
	head, source := strings.Repeat("a", 40), strings.Repeat("b", 64)
	for _, scope := range [][2]string{{"other", r.Repository}, {r.InstallationID, "other/repo"}} {
		if _, err := s.ApproveOptimisationShardingPreview(ctx, scope[0], scope[1], r.ID, head, source, r.SHA256, optimisationAudit()); !errors.Is(err, ErrNotFound) {
			t.Fatalf("foreign scope %v: %v", scope, err)
		}
	}
	now = now.Add(OptimisationShardingPreviewRetention)
	if _, err := s.ApproveOptimisationShardingPreview(ctx, r.InstallationID, r.Repository, r.ID, head, source, r.SHA256, optimisationAudit()); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expired preview approved: %v", err)
	}
}

func TestOptimisationShardingApprovalWithAMismatchedRowIsCorruptEvidence(t *testing.T) {
	s := newTestStore(t)
	r := shardingPreviewFixture(t, s)
	ctx := context.Background()
	if err := s.PutOptimisationShardingPreview(ctx, r, optimisationAudit()); err != nil {
		t.Fatal(err)
	}
	if _, err := s.write.ExecContext(ctx, `INSERT INTO optimisation_sharding_preview_approvals(preview_id,installation_id,repository,approved_by,head_sha,source_sha256,preview_sha256,created_at) VALUES(?,?,?,?,?,?,?,?)`,
		r.ID, r.InstallationID, r.Repository, "usr_actor", strings.Repeat("a", 40), strings.Repeat("b", 64), strings.Repeat("0", 64), 1); err != nil {
		t.Fatal(err)
	}
	if _, err := s.GetOptimisationShardingPreview(ctx, r.InstallationID, r.Repository, r.ID); !errors.Is(err, ErrInvalidOptimisationShardingPreview) {
		t.Fatalf("mismatched approval read as valid: %v", err)
	}
}
