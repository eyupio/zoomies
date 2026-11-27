package store

import (
	"context"
	"encoding/json"
	"errors"
	"time"
)

const KennelFixPreviewVersion = "kennel-fix-retained-v1"
const MaxKennelFixPreviewBytes = 256 << 10
const MaxKennelFixPreviewsPerInstallation = 20
const MaxRetainedKennelFixPreviews = 100
const KennelFixPreviewRetention = 14 * 24 * time.Hour

var ErrInvalidKennelFixPreview = errors.New("invalid retained Kennel fix preview")

type KennelFixPreview struct {
	ID             string                `json:"id"`
	InstallationID string                `json:"installation_id"`
	Repository     string                `json:"repository"`
	InitiatingUser string                `json:"initiating_user"`
	Version        string                `json:"version"`
	SHA256         string                `json:"sha256"`
	Payload        json.RawMessage       `json:"payload"`
	CreatedAt      time.Time             `json:"created_at"`
	Approval       *KennelFixApproval    `json:"approval,omitempty"`
	Publication    *KennelFixPublication `json:"publication,omitempty"`
}

// KennelFixPublication is the one draft pull request opened for an
// approved Kennel fix preview. It names the branch, its base and both commits, and
// the exact approval and preview digests it acted on. HeadSHA is the commit the
// preview was made at and BaseSHA the branch head the draft was opened from:
// the fix is bound to its file's blob and not to the head, so the two may differ
// when the branch moved and the file did not.
type KennelFixPublication retainedPublication

// KennelFixApproval is one attributed decision about the exact
// Kennel fix preview the operator was shown. It is not a publication.
type KennelFixApproval retainedApproval

// kennelFixPreviewKind is how this family differs from the others; see retainedPreviewKind.
var kennelFixPreviewKind = &retainedPreviewKind{
	invalid: ErrInvalidKennelFixPreview, version: KennelFixPreviewVersion, idPrefix: PrefixKennelFixPreview,
	maxBytes: MaxKennelFixPreviewBytes, perInstallation: MaxKennelFixPreviewsPerInstallation, maxRetained: MaxRetainedKennelFixPreviews, retention: KennelFixPreviewRetention,
	previewTable: "kennel_fix_previews", approvalTable: "kennel_fix_preview_approvals", publicationTable: "kennel_fix_publications",
	auditTarget: "kennel_fix_preview", auditRetained: "kennel.fix_preview_retained", auditApproved: "kennel.fix_preview_approved", auditPublished: "kennel.fix_preview_published",
	baseIsApprovedHead: false,
}

// PutKennelFixPreview accepts an already authorised actor. The row
// and audit are one transaction; an existing ID cannot overwrite evidence or
// renew its lifetime.
func (s *Store) PutKennelFixPreview(ctx context.Context, r *KennelFixPreview, actor *AuditEvent) error {
	if r == nil {
		return ErrInvalidKennelFixPreview
	}
	shared := retainedPreview{ID: r.ID, InstallationID: r.InstallationID, Repository: r.Repository,
		InitiatingUser: r.InitiatingUser, Version: r.Version, Payload: r.Payload}
	if err := s.putRetainedPreview(ctx, kennelFixPreviewKind, &shared, actor); err != nil {
		return err
	}
	r.SHA256, r.CreatedAt = shared.SHA256, shared.CreatedAt
	return nil
}

// A scoped miss includes expired or evicted evidence. It must not be rendered as
// an empty Kennel fix preview.
func (s *Store) GetKennelFixPreview(ctx context.Context, installation, repo, id string) (*KennelFixPreview, error) {
	p, err := s.getRetainedPreview(ctx, kennelFixPreviewKind, installation, repo, id)
	if err != nil {
		return nil, err
	}
	out := &KennelFixPreview{ID: p.ID, InstallationID: p.InstallationID, Repository: p.Repository,
		InitiatingUser: p.InitiatingUser, Version: p.Version, SHA256: p.SHA256, Payload: p.Payload, CreatedAt: p.CreatedAt}
	if p.Approval != nil {
		approval := KennelFixApproval(*p.Approval)
		out.Approval = &approval
	}
	if p.Publication != nil {
		publication := KennelFixPublication(*p.Publication)
		out.Publication = &publication
	}
	return out, nil
}

// ApproveKennelFixPreview records one exact approval of a retained
// Kennel fix preview. The preview is not rewritten, and a repeat by the same
// binding is a no-op with no second audit row. It never publishes.
func (s *Store) ApproveKennelFixPreview(ctx context.Context, installation, repo, id, headSHA, sourceSHA256, previewSHA256 string, actor *AuditEvent) (*KennelFixApproval, error) {
	approval, err := s.approveRetainedPreview(ctx, kennelFixPreviewKind, installation, repo, id, headSHA, sourceSHA256, previewSHA256, actor)
	if err != nil {
		return nil, err
	}
	return (*KennelFixApproval)(approval), nil
}

// PutKennelFixPublication records the one draft pull request opened for
// an approved Kennel fix preview. The GitHub write already happened, so this is the
// durable record of it: it refuses a preview that is expired, unapproved or
// bound differently, and a repeat of the same record is a no-op.
func (s *Store) PutKennelFixPublication(ctx context.Context, r *KennelFixPublication, actor *AuditEvent) error {
	return s.putRetainedPublication(ctx, kennelFixPreviewKind, (*retainedPublication)(r), actor)
}
