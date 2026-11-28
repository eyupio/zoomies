package store

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"
)

// The families of retained previews behave alike by construction, because one
// implementation serves them all (queries_retained_previews.go), and these tests
// hold each of them to the same account of that behaviour. What differs is what
// each is called, and that is written out here as literals and not read back
// from the descriptors: the audit actions, the target kind and the version
// are what other things read, so a test that took them from the code under test
// would pin nothing.
//
// Cache previews keep their own tests (optimisation_cache_previews_test.go) for
// the parts those cover beyond this account, and a Kennel fix's publication that
// may be based on a newer head than its preview is Kennel's alone.
type previewFamily struct {
	name, version, idPrefix                                   string
	retention                                                 time.Duration
	invalid                                                   error
	auditRetained, auditApproved, auditPublished, auditTarget string
	approvalTable, branchPrefix                               string

	// The four operations, on the shapes every family shares.
	put     func(*Store, context.Context, *retainedPreview, *AuditEvent) error
	get     func(*Store, context.Context, string, string, string) (*retainedPreview, error)
	approve func(*Store, context.Context, string, string, string, string, string, string, *AuditEvent) (*retainedApproval, error)
	publish func(*Store, context.Context, *retainedPublication, *AuditEvent) error
}

var previewFamilies = []previewFamily{
	{
		name: "sharding", version: "sharding-retained-v1", idPrefix: PrefixOptimisationShardingPreview,
		retention: OptimisationShardingPreviewRetention, invalid: ErrInvalidOptimisationShardingPreview,
		auditRetained: "optimisation.sharding_preview_retained", auditApproved: "optimisation.sharding_preview_approved",
		auditPublished: "optimisation.sharding_preview_published", auditTarget: "optimisation_sharding_preview",
		approvalTable: "optimisation_sharding_preview_approvals", branchPrefix: "zoomies/sharding-",
		put: func(s *Store, ctx context.Context, r *retainedPreview, actor *AuditEvent) error {
			p := &OptimisationShardingPreview{ID: r.ID, InstallationID: r.InstallationID, Repository: r.Repository, InitiatingUser: r.InitiatingUser, Version: r.Version, Payload: r.Payload}
			err := s.PutOptimisationShardingPreview(ctx, p, actor)
			r.SHA256, r.CreatedAt = p.SHA256, p.CreatedAt
			return err
		},
		get: func(s *Store, ctx context.Context, installation, repo, id string) (*retainedPreview, error) {
			p, err := s.GetOptimisationShardingPreview(ctx, installation, repo, id)
			if err != nil {
				return nil, err
			}
			out := &retainedPreview{ID: p.ID, InstallationID: p.InstallationID, Repository: p.Repository, InitiatingUser: p.InitiatingUser, Version: p.Version, SHA256: p.SHA256, Payload: p.Payload, CreatedAt: p.CreatedAt}
			if p.Approval != nil {
				out.Approval = (*retainedApproval)(p.Approval)
			}
			if p.Publication != nil {
				out.Publication = (*retainedPublication)(p.Publication)
			}
			return out, nil
		},
		approve: func(s *Store, ctx context.Context, installation, repo, id, head, source, preview string, actor *AuditEvent) (*retainedApproval, error) {
			a, err := s.ApproveOptimisationShardingPreview(ctx, installation, repo, id, head, source, preview, actor)
			return (*retainedApproval)(a), err
		},
		publish: func(s *Store, ctx context.Context, r *retainedPublication, actor *AuditEvent) error {
			return s.PutOptimisationShardingPublication(ctx, (*OptimisationShardingPublication)(r), actor)
		},
	},
	{
		name: "readiness", version: "readiness-retained-v1", idPrefix: PrefixOptimisationReadinessPreview,
		retention: OptimisationReadinessPreviewRetention, invalid: ErrInvalidOptimisationReadinessPreview,
		auditRetained: "optimisation.readiness_preview_retained", auditApproved: "optimisation.readiness_preview_approved",
		auditPublished: "optimisation.readiness_preview_published", auditTarget: "optimisation_readiness_preview",
		approvalTable: "optimisation_readiness_preview_approvals", branchPrefix: "zoomies/readiness-",
		put: func(s *Store, ctx context.Context, r *retainedPreview, actor *AuditEvent) error {
			p := &OptimisationReadinessPreview{ID: r.ID, InstallationID: r.InstallationID, Repository: r.Repository, InitiatingUser: r.InitiatingUser, Version: r.Version, Payload: r.Payload}
			err := s.PutOptimisationReadinessPreview(ctx, p, actor)
			r.SHA256, r.CreatedAt = p.SHA256, p.CreatedAt
			return err
		},
		get: func(s *Store, ctx context.Context, installation, repo, id string) (*retainedPreview, error) {
			p, err := s.GetOptimisationReadinessPreview(ctx, installation, repo, id)
			if err != nil {
				return nil, err
			}
			out := &retainedPreview{ID: p.ID, InstallationID: p.InstallationID, Repository: p.Repository, InitiatingUser: p.InitiatingUser, Version: p.Version, SHA256: p.SHA256, Payload: p.Payload, CreatedAt: p.CreatedAt}
			if p.Approval != nil {
				out.Approval = (*retainedApproval)(p.Approval)
			}
			if p.Publication != nil {
				out.Publication = (*retainedPublication)(p.Publication)
			}
			return out, nil
		},
		approve: func(s *Store, ctx context.Context, installation, repo, id, head, source, preview string, actor *AuditEvent) (*retainedApproval, error) {
			a, err := s.ApproveOptimisationReadinessPreview(ctx, installation, repo, id, head, source, preview, actor)
			return (*retainedApproval)(a), err
		},
		publish: func(s *Store, ctx context.Context, r *retainedPublication, actor *AuditEvent) error {
			return s.PutOptimisationReadinessPublication(ctx, (*OptimisationReadinessPublication)(r), actor)
		},
	},
	{
		name: "kennel fix", version: "kennel-fix-retained-v1", idPrefix: PrefixKennelFixPreview,
		retention: KennelFixPreviewRetention, invalid: ErrInvalidKennelFixPreview,
		auditRetained: "kennel.fix_preview_retained", auditApproved: "kennel.fix_preview_approved",
		auditPublished: "kennel.fix_preview_published", auditTarget: "kennel_fix_preview",
		approvalTable: "kennel_fix_preview_approvals", branchPrefix: "zoomies/kennel-fix-",
		put: func(s *Store, ctx context.Context, r *retainedPreview, actor *AuditEvent) error {
			p := &KennelFixPreview{ID: r.ID, InstallationID: r.InstallationID, Repository: r.Repository, InitiatingUser: r.InitiatingUser, Version: r.Version, Payload: r.Payload}
			err := s.PutKennelFixPreview(ctx, p, actor)
			r.SHA256, r.CreatedAt = p.SHA256, p.CreatedAt
			return err
		},
		get: func(s *Store, ctx context.Context, installation, repo, id string) (*retainedPreview, error) {
			p, err := s.GetKennelFixPreview(ctx, installation, repo, id)
			if err != nil {
				return nil, err
			}
			out := &retainedPreview{ID: p.ID, InstallationID: p.InstallationID, Repository: p.Repository, InitiatingUser: p.InitiatingUser, Version: p.Version, SHA256: p.SHA256, Payload: p.Payload, CreatedAt: p.CreatedAt}
			if p.Approval != nil {
				out.Approval = (*retainedApproval)(p.Approval)
			}
			if p.Publication != nil {
				out.Publication = (*retainedPublication)(p.Publication)
			}
			return out, nil
		},
		approve: func(s *Store, ctx context.Context, installation, repo, id, head, source, preview string, actor *AuditEvent) (*retainedApproval, error) {
			a, err := s.ApproveKennelFixPreview(ctx, installation, repo, id, head, source, preview, actor)
			return (*retainedApproval)(a), err
		},
		publish: func(s *Store, ctx context.Context, r *retainedPublication, actor *AuditEvent) error {
			return s.PutKennelFixPublication(ctx, (*KennelFixPublication)(r), actor)
		},
	},
}

func forEachPreviewFamily(t *testing.T, test func(*testing.T, previewFamily)) {
	t.Helper()
	for _, f := range previewFamilies {
		t.Run(f.name, func(t *testing.T) { test(t, f) })
	}
}

// previewFixture is a preview of a family, ready to be kept.
func previewFixture(t *testing.T, s *Store, f previewFamily) *retainedPreview {
	t.Helper()
	inst := &Installation{AppID: 1, InstallationID: 2, Target: "acme/repo", TargetType: TargetRepo}
	if err := s.CreateInstallation(context.Background(), inst); err != nil {
		t.Fatal(err)
	}
	return &retainedPreview{ID: NewID(f.idPrefix), InstallationID: inst.ID, Repository: inst.Target, InitiatingUser: "usr_actor", Version: f.version,
		Payload: json.RawMessage(`{"version":"` + f.version + `","evidence":"sample"}`)}
}

func TestARetainedPreviewIsImmutableScopedAndAudited(t *testing.T) {
	forEachPreviewFamily(t, func(t *testing.T, f previewFamily) {
		s := newTestStore(t)
		r := previewFixture(t, s, f)
		ctx := context.Background()
		if err := f.put(s, ctx, r, optimisationAudit()); err != nil {
			t.Fatal(err)
		}
		first, err := f.get(s, ctx, r.InstallationID, r.Repository, r.ID)
		if err != nil {
			t.Fatal(err)
		}
		if err := f.put(s, ctx, r, optimisationAudit()); err != nil {
			t.Fatal(err)
		}
		events, total, err := s.ListAudit(ctx, AuditFilter{TargetID: r.ID}, Page{Limit: 10})
		if err != nil || total != 1 || events[0].Action != f.auditRetained {
			t.Fatalf("audit %+v total=%d err=%v", events, total, err)
		}
		r.Payload = json.RawMessage(`{"version":"` + f.version + `","evidence":"changed"}`)
		if err := f.put(s, ctx, r, optimisationAudit()); !errors.Is(err, ErrConflict) {
			t.Fatal(err)
		}
		after, err := f.get(s, ctx, r.InstallationID, r.Repository, r.ID)
		if err != nil || string(after.Payload) != string(first.Payload) {
			t.Fatal("immutable record changed", err)
		}
		if _, err := f.get(s, ctx, "other", r.Repository, r.ID); !errors.Is(err, ErrNotFound) {
			t.Fatal(err)
		}
	})
}

func TestARetainedPreviewsExpiryDoesNotRenew(t *testing.T) {
	forEachPreviewFamily(t, func(t *testing.T, f previewFamily) {
		now := time.Date(2026, 10, 24, 0, 0, 0, 0, time.UTC)
		s := newTestStoreAt(t, func() time.Time { return now })
		r := previewFixture(t, s, f)
		ctx := context.Background()
		if err := f.put(s, ctx, r, optimisationAudit()); err != nil {
			t.Fatal(err)
		}
		now = now.Add(f.retention)
		if _, err := f.get(s, ctx, r.InstallationID, r.Repository, r.ID); !errors.Is(err, ErrNotFound) {
			t.Fatal("expired preview visible")
		}
		if err := f.put(s, ctx, r, optimisationAudit()); !errors.Is(err, ErrNotFound) {
			t.Fatal("retry renewed expiry")
		}
	})
}

func TestARetainedApprovalBindsThePreviewAndDoesNotPublish(t *testing.T) {
	forEachPreviewFamily(t, func(t *testing.T, f previewFamily) {
		s := newTestStore(t)
		r := previewFixture(t, s, f)
		ctx := context.Background()
		if err := f.put(s, ctx, r, optimisationAudit()); err != nil {
			t.Fatal(err)
		}
		head, source := strings.Repeat("a", 40), strings.Repeat("b", 64)
		approval, err := f.approve(s, ctx, r.InstallationID, r.Repository, r.ID, head, source, r.SHA256, optimisationAudit())
		if err != nil {
			t.Fatal(err)
		}
		if approval.ApprovedBy != "usr_actor" || approval.HeadSHA != head || approval.SourceSHA256 != source || approval.PreviewSHA256 != r.SHA256 {
			t.Fatalf("approval %+v", approval)
		}
		// A repeat by the same binding must not write a second audit row.
		again, err := f.approve(s, ctx, r.InstallationID, r.Repository, r.ID, head, source, r.SHA256, optimisationAudit())
		if err != nil || !again.CreatedAt.Equal(approval.CreatedAt) {
			t.Fatalf("repeat changed the approval: %+v %v", again, err)
		}
		events, total, err := s.ListAudit(ctx, AuditFilter{TargetID: r.ID}, Page{Limit: 10})
		actions := map[string]bool{}
		for _, e := range events {
			actions[e.Action] = true
			if e.Action == f.auditApproved && e.TargetKind != f.auditTarget {
				t.Fatalf("target type %q", e.TargetKind)
			}
		}
		if err != nil || total != 2 || !actions[f.auditApproved] || !actions[f.auditRetained] {
			t.Fatalf("audit %+v total=%d err=%v", events, total, err)
		}
		got, err := f.get(s, ctx, r.InstallationID, r.Repository, r.ID)
		if err != nil || got.Approval == nil || got.Approval.ApprovedBy != "usr_actor" {
			t.Fatalf("read %+v %v", got, err)
		}
		if _, err := f.approve(s, ctx, r.InstallationID, r.Repository, r.ID, head, strings.Repeat("c", 64), r.SHA256, optimisationAudit()); !errors.Is(err, ErrConflict) {
			t.Fatalf("different source accepted: %v", err)
		}
		if _, err := f.approve(s, ctx, r.InstallationID, r.Repository, r.ID, strings.Repeat("d", 40), source, r.SHA256, optimisationAudit()); !errors.Is(err, ErrConflict) {
			t.Fatalf("different head accepted: %v", err)
		}
		if _, err := f.approve(s, ctx, r.InstallationID, r.Repository, r.ID, head, source, strings.Repeat("e", 64), optimisationAudit()); !errors.Is(err, ErrConflict) {
			t.Fatalf("different preview digest accepted: %v", err)
		}
	})
}

func TestARetainedApprovalExpiresWithThePreviewAndRefusesForeignScope(t *testing.T) {
	forEachPreviewFamily(t, func(t *testing.T, f previewFamily) {
		now := time.Date(2026, 10, 24, 0, 0, 0, 0, time.UTC)
		s := newTestStoreAt(t, func() time.Time { return now })
		r := previewFixture(t, s, f)
		ctx := context.Background()
		if err := f.put(s, ctx, r, optimisationAudit()); err != nil {
			t.Fatal(err)
		}
		head, source := strings.Repeat("a", 40), strings.Repeat("b", 64)
		for _, scope := range [][2]string{{"other", r.Repository}, {r.InstallationID, "other/repo"}} {
			if _, err := f.approve(s, ctx, scope[0], scope[1], r.ID, head, source, r.SHA256, optimisationAudit()); !errors.Is(err, ErrNotFound) {
				t.Fatalf("foreign scope %v: %v", scope, err)
			}
		}
		now = now.Add(f.retention)
		if _, err := f.approve(s, ctx, r.InstallationID, r.Repository, r.ID, head, source, r.SHA256, optimisationAudit()); !errors.Is(err, ErrNotFound) {
			t.Fatalf("expired preview approved: %v", err)
		}
	})
}

func TestARetainedApprovalWithAMismatchedRowIsCorruptEvidence(t *testing.T) {
	forEachPreviewFamily(t, func(t *testing.T, f previewFamily) {
		s := newTestStore(t)
		r := previewFixture(t, s, f)
		ctx := context.Background()
		if err := f.put(s, ctx, r, optimisationAudit()); err != nil {
			t.Fatal(err)
		}
		if _, err := s.write.ExecContext(ctx, `INSERT INTO `+f.approvalTable+`(preview_id,installation_id,repository,approved_by,head_sha,source_sha256,preview_sha256,created_at) VALUES(?,?,?,?,?,?,?,?)`,
			r.ID, r.InstallationID, r.Repository, "usr_actor", strings.Repeat("a", 40), strings.Repeat("b", 64), strings.Repeat("0", 64), 1); err != nil {
			t.Fatal(err)
		}
		if _, err := f.get(s, ctx, r.InstallationID, r.Repository, r.ID); !errors.Is(err, f.invalid) {
			t.Fatalf("mismatched approval read as valid: %v", err)
		}
	})
}

func TestARetainedPublicationRecordsOneApprovedDraft(t *testing.T) {
	forEachPreviewFamily(t, func(t *testing.T, f previewFamily) {
		s := newTestStore(t)
		r := previewFixture(t, s, f)
		ctx := context.Background()
		if err := f.put(s, ctx, r, optimisationAudit()); err != nil {
			t.Fatal(err)
		}
		head, source := strings.Repeat("a", 40), strings.Repeat("b", 64)
		if _, err := f.approve(s, ctx, r.InstallationID, r.Repository, r.ID, head, source, r.SHA256, optimisationAudit()); err != nil {
			t.Fatal(err)
		}
		pub := &retainedPublication{
			PreviewID: r.ID, InstallationID: r.InstallationID, Repository: r.Repository, PublishedBy: "usr_actor",
			Branch: f.branchPrefix + r.ID, BaseBranch: "main", BaseSHA: head, HeadSHA: head, SourceSHA256: source, PreviewSHA256: r.SHA256,
			PullNumber: 12, PullURL: "https://github.com/acme/repo/pull/12",
		}
		if err := f.publish(s, ctx, pub, optimisationAudit()); err != nil {
			t.Fatal(err)
		}
		if pub.CreatedAt.IsZero() {
			t.Fatal("publication was not timestamped")
		}
		if err := f.publish(s, ctx, pub, optimisationAudit()); err != nil {
			t.Fatalf("repeat: %v", err)
		}
		got, err := f.get(s, ctx, r.InstallationID, r.Repository, r.ID)
		if err != nil || got.Publication == nil || got.Publication.PullNumber != 12 || got.Publication.PublishedBy != "usr_actor" {
			t.Fatalf("read %+v %v", got, err)
		}
		events, total, err := s.ListAudit(ctx, AuditFilter{TargetID: r.ID}, Page{Limit: 10})
		published := false
		for _, e := range events {
			published = published || e.Action == f.auditPublished && e.TargetKind == f.auditTarget
		}
		if err != nil || total != 3 || !published {
			t.Fatalf("audit %+v total=%d err=%v", events, total, err)
		}
		other := *pub
		other.PullURL = "https://github.com/acme/repo/pull/13"
		if err := f.publish(s, ctx, &other, optimisationAudit()); !errors.Is(err, ErrConflict) {
			t.Fatalf("different draft accepted: %v", err)
		}
	})
}

func TestARetainedPublicationRequiresAnApprovalAndLivePreview(t *testing.T) {
	forEachPreviewFamily(t, func(t *testing.T, f previewFamily) {
		now := time.Date(2026, 10, 24, 0, 0, 0, 0, time.UTC)
		s := newTestStoreAt(t, func() time.Time { return now })
		r := previewFixture(t, s, f)
		ctx := context.Background()
		if err := f.put(s, ctx, r, optimisationAudit()); err != nil {
			t.Fatal(err)
		}
		head, source := strings.Repeat("a", 40), strings.Repeat("b", 64)
		pub := &retainedPublication{PreviewID: r.ID, InstallationID: r.InstallationID, Repository: r.Repository, PublishedBy: "usr_actor", Branch: f.branchPrefix + r.ID, BaseBranch: "main", BaseSHA: head, HeadSHA: head, SourceSHA256: source, PreviewSHA256: r.SHA256, PullNumber: 12, PullURL: "https://github.com/acme/repo/pull/12"}
		if err := f.publish(s, ctx, pub, optimisationAudit()); !errors.Is(err, ErrConflict) {
			t.Fatalf("unapproved preview published: %v", err)
		}
		if _, err := f.approve(s, ctx, r.InstallationID, r.Repository, r.ID, head, source, r.SHA256, optimisationAudit()); err != nil {
			t.Fatal(err)
		}
		now = now.Add(f.retention)
		if err := f.publish(s, ctx, pub, optimisationAudit()); !errors.Is(err, ErrNotFound) {
			t.Fatalf("expired preview published: %v", err)
		}
	})
}

// An installation's target is stored folded to lower case, and a repository
// arrives in the case GitHub gives it: from a job's delivery, or from Kennel
// Club's list. Compared byte for byte, every owner or repository with a capital
// in its name was refused as outside its own installation, which the webhook
// path, folding both, never did.
func TestEvidenceIsKeptForARepositoryWhoseNameHasCapitals(t *testing.T) {
	const repo = "Acme/Widgets"
	payload := func(version string) json.RawMessage { return json.RawMessage(`{"version":"` + version + `"}`) }
	checks := []struct {
		name string
		put  func(*Store, context.Context, string) error
	}{
		{"a retained preview", func(s *Store, ctx context.Context, inst string) error {
			return s.PutKennelFixPreview(ctx, &KennelFixPreview{ID: NewID(PrefixKennelFixPreview), InstallationID: inst, Repository: repo,
				InitiatingUser: "usr_actor", Version: KennelFixPreviewVersion, Payload: payload(KennelFixPreviewVersion)}, optimisationAudit())
		}},
		{"a checkout preview", func(s *Store, ctx context.Context, inst string) error {
			return s.PutOptimisationPreview(ctx, &OptimisationPreview{ID: NewID(PrefixOptimisationPreview), InstallationID: inst, Repository: repo,
				InitiatingUser: "usr_actor", Version: OptimisationPreviewVersion, Payload: payload(OptimisationPreviewVersion)}, optimisationAudit())
		}},
		{"comparison evidence", func(s *Store, ctx context.Context, inst string) error {
			return s.PutComparisonEvidence(ctx, &ComparisonEvidence{ID: NewID(PrefixComparison), InstallationID: inst, Repository: repo,
				InitiatingUser: "usr_actor", Version: ComparisonEvidenceVersion, Payload: payload(ComparisonEvidenceVersion)}, optimisationAudit())
		}},
	}
	targets := []struct {
		target string
		kind   TargetType
	}{{"Acme", TargetOrg}, {repo, TargetRepo}}
	for _, check := range checks {
		for _, target := range targets {
			t.Run(check.name+" under "+string(target.kind), func(t *testing.T) {
				s := newTestStore(t)
				ctx := context.Background()
				inst := &Installation{AppID: 1, InstallationID: 2, Target: target.target, TargetType: target.kind}
				if err := s.CreateInstallation(ctx, inst); err != nil {
					t.Fatal(err)
				}
				if err := check.put(s, ctx, inst.ID); err != nil {
					t.Fatalf("%s for %s under the installation for %q (stored as %q) was refused: %v", check.name, repo, target.target, inst.Target, err)
				}
			})
		}
	}
}
