package store

import (
	"context"
	"encoding/json"
	"errors"
	"time"
)

const OptimisationShardingPreviewVersion = "sharding-retained-v1"
const MaxOptimisationShardingPreviewBytes = 256 << 10
const MaxShardingPreviewsPerInstallation = 20
const MaxRetainedShardingPreviews = 100
const OptimisationShardingPreviewRetention = 14 * 24 * time.Hour

var ErrInvalidOptimisationShardingPreview = errors.New("invalid retained optimisation sharding preview")

type OptimisationShardingPreview struct {
	ID             string                           `json:"id"`
	InstallationID string                           `json:"installation_id"`
	Repository     string                           `json:"repository"`
	InitiatingUser string                           `json:"initiating_user"`
	Version        string                           `json:"version"`
	SHA256         string                           `json:"sha256"`
	Payload        json.RawMessage                  `json:"payload"`
	CreatedAt      time.Time                        `json:"created_at"`
	Approval       *OptimisationShardingApproval    `json:"approval,omitempty"`
	Publication    *OptimisationShardingPublication `json:"publication,omitempty"`
}

// OptimisationShardingPublication is the one draft pull request opened for an
// approved sharding preview. It names the branch, its base and both commits, and
// the exact approval and preview digests it acted on.
type OptimisationShardingPublication retainedPublication

// OptimisationShardingApproval is one attributed decision about the exact
// sharding preview the operator was shown. It is not a publication.
type OptimisationShardingApproval retainedApproval

// optimisationShardingPreviewKind is how this family differs from the others; see retainedPreviewKind.
var optimisationShardingPreviewKind = &retainedPreviewKind{
	invalid: ErrInvalidOptimisationShardingPreview, version: OptimisationShardingPreviewVersion, idPrefix: PrefixOptimisationShardingPreview,
	maxBytes: MaxOptimisationShardingPreviewBytes, perInstallation: MaxShardingPreviewsPerInstallation, maxRetained: MaxRetainedShardingPreviews, retention: OptimisationShardingPreviewRetention,
	previewTable: "optimisation_sharding_previews", approvalTable: "optimisation_sharding_preview_approvals", publicationTable: "optimisation_sharding_publications",
	auditTarget: "optimisation_sharding_preview", auditRetained: "optimisation.sharding_preview_retained", auditApproved: "optimisation.sharding_preview_approved", auditPublished: "optimisation.sharding_preview_published",
	baseIsApprovedHead: true,
}

// PutOptimisationShardingPreview accepts an already authorised actor. The row
// and audit are one transaction; an existing ID cannot overwrite evidence or
// renew its lifetime.
func (s *Store) PutOptimisationShardingPreview(ctx context.Context, r *OptimisationShardingPreview, actor *AuditEvent) error {
	if r == nil {
		return ErrInvalidOptimisationShardingPreview
	}
	shared := retainedPreview{ID: r.ID, InstallationID: r.InstallationID, Repository: r.Repository,
		InitiatingUser: r.InitiatingUser, Version: r.Version, Payload: r.Payload}
	if err := s.putRetainedPreview(ctx, optimisationShardingPreviewKind, &shared, actor); err != nil {
		return err
	}
	r.SHA256, r.CreatedAt = shared.SHA256, shared.CreatedAt
	return nil
}

// A scoped miss includes expired or evicted evidence. It must not be rendered as
// an empty sharding preview.
func (s *Store) GetOptimisationShardingPreview(ctx context.Context, installation, repo, id string) (*OptimisationShardingPreview, error) {
	p, err := s.getRetainedPreview(ctx, optimisationShardingPreviewKind, installation, repo, id)
	if err != nil {
		return nil, err
	}
	out := &OptimisationShardingPreview{ID: p.ID, InstallationID: p.InstallationID, Repository: p.Repository,
		InitiatingUser: p.InitiatingUser, Version: p.Version, SHA256: p.SHA256, Payload: p.Payload, CreatedAt: p.CreatedAt}
	if p.Approval != nil {
		approval := OptimisationShardingApproval(*p.Approval)
		out.Approval = &approval
	}
	if p.Publication != nil {
		publication := OptimisationShardingPublication(*p.Publication)
		out.Publication = &publication
	}
	return out, nil
}

// ApproveOptimisationShardingPreview records one exact approval of a retained
// sharding preview. The preview is not rewritten, and a repeat by the same
// binding is a no-op with no second audit row. It never publishes.
func (s *Store) ApproveOptimisationShardingPreview(ctx context.Context, installation, repo, id, headSHA, sourceSHA256, previewSHA256 string, actor *AuditEvent) (*OptimisationShardingApproval, error) {
	approval, err := s.approveRetainedPreview(ctx, optimisationShardingPreviewKind, installation, repo, id, headSHA, sourceSHA256, previewSHA256, actor)
	if err != nil {
		return nil, err
	}
	return (*OptimisationShardingApproval)(approval), nil
}

// PutOptimisationShardingPublication records the one draft pull request opened for
// an approved sharding preview. The GitHub write already happened, so this is the
// durable record of it: it refuses a preview that is expired, unapproved or
// bound differently, and a repeat of the same record is a no-op.
func (s *Store) PutOptimisationShardingPublication(ctx context.Context, r *OptimisationShardingPublication, actor *AuditEvent) error {
	return s.putRetainedPublication(ctx, optimisationShardingPreviewKind, (*retainedPublication)(r), actor)
}
