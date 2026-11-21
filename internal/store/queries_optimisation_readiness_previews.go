package store

import (
	"context"
	"encoding/json"
	"errors"
	"time"
)

const OptimisationReadinessPreviewVersion = "readiness-retained-v1"
const MaxOptimisationReadinessPreviewBytes = 256 << 10
const MaxReadinessPreviewsPerInstallation = 20
const MaxRetainedReadinessPreviews = 100
const OptimisationReadinessPreviewRetention = 14 * 24 * time.Hour

var ErrInvalidOptimisationReadinessPreview = errors.New("invalid retained optimisation readiness preview")

type OptimisationReadinessPreview struct {
	ID             string                            `json:"id"`
	InstallationID string                            `json:"installation_id"`
	Repository     string                            `json:"repository"`
	InitiatingUser string                            `json:"initiating_user"`
	Version        string                            `json:"version"`
	SHA256         string                            `json:"sha256"`
	Payload        json.RawMessage                   `json:"payload"`
	CreatedAt      time.Time                         `json:"created_at"`
	Approval       *OptimisationReadinessApproval    `json:"approval,omitempty"`
	Publication    *OptimisationReadinessPublication `json:"publication,omitempty"`
}

// OptimisationReadinessPublication is the one draft pull request opened for an
// approved readiness preview. It names the branch, its base and both commits, and
// the exact approval and preview digests it acted on.
type OptimisationReadinessPublication retainedPublication

// OptimisationReadinessApproval is one attributed decision about the exact
// readiness preview the operator was shown. It is not a publication.
type OptimisationReadinessApproval retainedApproval

// optimisationReadinessPreviewKind is how this family differs from the others; see retainedPreviewKind.
var optimisationReadinessPreviewKind = &retainedPreviewKind{
	invalid: ErrInvalidOptimisationReadinessPreview, version: OptimisationReadinessPreviewVersion, idPrefix: PrefixOptimisationReadinessPreview,
	maxBytes: MaxOptimisationReadinessPreviewBytes, perInstallation: MaxReadinessPreviewsPerInstallation, maxRetained: MaxRetainedReadinessPreviews, retention: OptimisationReadinessPreviewRetention,
	previewTable: "optimisation_readiness_previews", approvalTable: "optimisation_readiness_preview_approvals", publicationTable: "optimisation_readiness_publications",
	auditTarget: "optimisation_readiness_preview", auditRetained: "optimisation.readiness_preview_retained", auditApproved: "optimisation.readiness_preview_approved", auditPublished: "optimisation.readiness_preview_published",
	baseIsApprovedHead: true,
}

// PutOptimisationReadinessPreview accepts an already authorised actor. The row
// and audit are one transaction; an existing ID cannot overwrite evidence or
// renew its lifetime.
func (s *Store) PutOptimisationReadinessPreview(ctx context.Context, r *OptimisationReadinessPreview, actor *AuditEvent) error {
	if r == nil {
		return ErrInvalidOptimisationReadinessPreview
	}
	shared := retainedPreview{ID: r.ID, InstallationID: r.InstallationID, Repository: r.Repository,
		InitiatingUser: r.InitiatingUser, Version: r.Version, Payload: r.Payload}
	if err := s.putRetainedPreview(ctx, optimisationReadinessPreviewKind, &shared, actor); err != nil {
		return err
	}
	r.SHA256, r.CreatedAt = shared.SHA256, shared.CreatedAt
	return nil
}

// A scoped miss includes expired or evicted evidence. It must not be rendered as
// an empty readiness preview.
func (s *Store) GetOptimisationReadinessPreview(ctx context.Context, installation, repo, id string) (*OptimisationReadinessPreview, error) {
	p, err := s.getRetainedPreview(ctx, optimisationReadinessPreviewKind, installation, repo, id)
	if err != nil {
		return nil, err
	}
	out := &OptimisationReadinessPreview{ID: p.ID, InstallationID: p.InstallationID, Repository: p.Repository,
		InitiatingUser: p.InitiatingUser, Version: p.Version, SHA256: p.SHA256, Payload: p.Payload, CreatedAt: p.CreatedAt}
	if p.Approval != nil {
		approval := OptimisationReadinessApproval(*p.Approval)
		out.Approval = &approval
	}
	if p.Publication != nil {
		publication := OptimisationReadinessPublication(*p.Publication)
		out.Publication = &publication
	}
	return out, nil
}

// ApproveOptimisationReadinessPreview records one exact approval of a retained
// readiness preview. The preview is not rewritten, and a repeat by the same
// binding is a no-op with no second audit row. It never publishes.
func (s *Store) ApproveOptimisationReadinessPreview(ctx context.Context, installation, repo, id, headSHA, sourceSHA256, previewSHA256 string, actor *AuditEvent) (*OptimisationReadinessApproval, error) {
	approval, err := s.approveRetainedPreview(ctx, optimisationReadinessPreviewKind, installation, repo, id, headSHA, sourceSHA256, previewSHA256, actor)
	if err != nil {
		return nil, err
	}
	return (*OptimisationReadinessApproval)(approval), nil
}

// PutOptimisationReadinessPublication records the one draft pull request opened for
// an approved readiness preview. The GitHub write already happened, so this is the
// durable record of it: it refuses a preview that is expired, unapproved or
// bound differently, and a repeat of the same record is a no-op.
func (s *Store) PutOptimisationReadinessPublication(ctx context.Context, r *OptimisationReadinessPublication, actor *AuditEvent) error {
	return s.putRetainedPublication(ctx, optimisationReadinessPreviewKind, (*retainedPublication)(r), actor)
}
