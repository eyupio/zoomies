package store

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"time"
	"unicode/utf8"
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
type OptimisationShardingPublication struct {
	PreviewID      string    `json:"preview_id"`
	InstallationID string    `json:"installation_id"`
	Repository     string    `json:"repository"`
	PublishedBy    string    `json:"published_by"`
	Branch         string    `json:"branch"`
	BaseBranch     string    `json:"base_branch"`
	BaseSHA        string    `json:"base_sha"`
	HeadSHA        string    `json:"head_sha"`
	SourceSHA256   string    `json:"source_sha256"`
	PreviewSHA256  string    `json:"preview_sha256"`
	PullNumber     int       `json:"pull_number"`
	PullURL        string    `json:"pull_url"`
	CreatedAt      time.Time `json:"created_at"`
}

// OptimisationShardingApproval is one attributed decision about the exact
// sharding preview the operator was shown. It is not a publication.
type OptimisationShardingApproval struct {
	PreviewID      string    `json:"preview_id"`
	InstallationID string    `json:"installation_id"`
	Repository     string    `json:"repository"`
	ApprovedBy     string    `json:"approved_by"`
	HeadSHA        string    `json:"head_sha"`
	SourceSHA256   string    `json:"source_sha256"`
	PreviewSHA256  string    `json:"preview_sha256"`
	CreatedAt      time.Time `json:"created_at"`
}

// PutOptimisationShardingPreview accepts an already authorised actor. The row
// and audit are one transaction; an existing ID cannot overwrite evidence or
// renew its lifetime.
func (s *Store) PutOptimisationShardingPreview(ctx context.Context, r *OptimisationShardingPreview, actor *AuditEvent) error {
	if r == nil || actor == nil || !strings.HasPrefix(r.ID, PrefixOptimisationShardingPreview+"_") || len(r.ID) > 128 || strings.ContainsAny(r.ID, " /\r\n\x00") || r.InstallationID == "" || len(r.InstallationID) > 128 || r.InitiatingUser == "" || len(r.InitiatingUser) > 128 || actor.ActorID != r.InitiatingUser || strings.TrimSpace(actor.ActorKind) == "" || len(actor.ActorKind) > 64 || len(actor.ActorName) > 200 || len(actor.IP) > 128 || r.Version != OptimisationShardingPreviewVersion || len(r.Payload) == 0 || len(r.Payload) > MaxOptimisationShardingPreviewBytes || !utf8.Valid(r.Payload) {
		return ErrInvalidOptimisationShardingPreview
	}
	if !validOptimisationRepository(r.Repository) {
		return ErrInvalidOptimisationShardingPreview
	}
	owner, _, _ := strings.Cut(r.Repository, "/")
	var envelope map[string]json.RawMessage
	if json.Unmarshal(r.Payload, &envelope) != nil || envelope == nil {
		return ErrInvalidOptimisationShardingPreview
	}
	var version string
	if json.Unmarshal(envelope["version"], &version) != nil || version != r.Version {
		return ErrInvalidOptimisationShardingPreview
	}
	sum := sha256.Sum256(r.Payload)
	digest := hex.EncodeToString(sum[:])
	now := s.Now().UTC().Truncate(time.Millisecond)
	created := now
	err := s.tx(ctx, func(tx *sql.Tx) error {
		var target, kind string
		if err := tx.QueryRowContext(ctx, `SELECT target,target_type FROM installations WHERE id = ?`, r.InstallationID).Scan(&target, &kind); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return ErrInvalidOptimisationShardingPreview
			}
			return err
		}
		if !(kind == string(TargetRepo) && target == r.Repository || kind == string(TargetOrg) && target == owner) {
			return ErrInvalidOptimisationShardingPreview
		}
		var installation, repo, user, oldVersion, oldHash string
		var at int64
		var oldPayload []byte
		err := tx.QueryRowContext(ctx, `SELECT installation_id,repository,initiating_user,version,sha256,created_at,payload FROM optimisation_sharding_previews WHERE id = ?`, r.ID).Scan(&installation, &repo, &user, &oldVersion, &oldHash, &at, &oldPayload)
		if err == nil {
			if installation != r.InstallationID || repo != r.Repository || user != r.InitiatingUser || oldVersion != r.Version || oldHash != digest {
				return ErrConflict
			}
			if sha256.Sum256(oldPayload) != sum {
				return ErrInvalidOptimisationShardingPreview
			}
			if at <= ms(now.Add(-OptimisationShardingPreviewRetention)) {
				return ErrNotFound
			}
			created = time.UnixMilli(at).UTC()
			return nil
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO optimisation_sharding_previews(id,installation_id,repository,initiating_user,version,sha256,payload,created_at) VALUES(?,?,?,?,?,?,?,?)`, r.ID, r.InstallationID, r.Repository, r.InitiatingUser, r.Version, digest, []byte(r.Payload), ms(now)); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO audit_events (`+auditCols+`) VALUES (?,?,?,?,?,?,?,?,?,?,?)`, NewID(PrefixAudit), actor.ActorID, actor.ActorName, actor.ActorKind, "optimisation.sharding_preview_retained", "optimisation_sharding_preview", r.ID, "", "", actor.IP, ms(now)); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM optimisation_sharding_previews WHERE created_at <= ?`, ms(now.Add(-OptimisationShardingPreviewRetention))); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM optimisation_sharding_previews WHERE installation_id = ? AND id NOT IN (SELECT id FROM optimisation_sharding_previews WHERE installation_id = ? ORDER BY sequence DESC LIMIT ?)`, r.InstallationID, r.InstallationID, MaxShardingPreviewsPerInstallation); err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, `DELETE FROM optimisation_sharding_previews WHERE id NOT IN (SELECT id FROM optimisation_sharding_previews ORDER BY sequence DESC LIMIT ?)`, MaxRetainedShardingPreviews)
		return err
	})
	if err == nil {
		r.SHA256 = digest
		r.CreatedAt = created
	}
	return err
}

// A scoped miss includes expired or evicted evidence. It must not be rendered as
// an empty sharding preview.
func (s *Store) GetOptimisationShardingPreview(ctx context.Context, installation, repo, id string) (*OptimisationShardingPreview, error) {
	r := &OptimisationShardingPreview{}
	var payload []byte
	var created int64
	var approvedInstallation, approvedRepo, approvedBy, approvedHead, approvedSource, approvedPreview sql.NullString
	var approvedAt sql.NullInt64
	var publishedInstallation, publishedRepo, publishedBy, publishedBranch, publishedBase, publishedBaseSHA, publishedHead, publishedSource, publishedPreview, publishedURL sql.NullString
	var publishedNumber, publishedAt sql.NullInt64
	err := s.read.QueryRowContext(ctx, `SELECT p.id,p.installation_id,p.repository,p.initiating_user,p.version,p.sha256,p.payload,p.created_at,
			a.installation_id,a.repository,a.approved_by,a.head_sha,a.source_sha256,a.preview_sha256,a.created_at,
			u.installation_id,u.repository,u.published_by,u.branch,u.base_branch,u.base_sha,u.head_sha,u.source_sha256,u.preview_sha256,u.pull_number,u.pull_url,u.created_at
		FROM optimisation_sharding_previews p
		LEFT JOIN optimisation_sharding_preview_approvals a ON a.preview_id = p.id
		LEFT JOIN optimisation_sharding_publications u ON u.preview_id = p.id
		WHERE p.installation_id = ? AND p.repository = ? AND p.id = ? AND p.created_at > ?`,
		installation, repo, id, ms(s.Now().Add(-OptimisationShardingPreviewRetention))).Scan(&r.ID, &r.InstallationID, &r.Repository, &r.InitiatingUser, &r.Version, &r.SHA256, &payload, &created,
		&approvedInstallation, &approvedRepo, &approvedBy, &approvedHead, &approvedSource, &approvedPreview, &approvedAt,
		&publishedInstallation, &publishedRepo, &publishedBy, &publishedBranch, &publishedBase, &publishedBaseSHA, &publishedHead, &publishedSource, &publishedPreview, &publishedNumber, &publishedURL, &publishedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	sum := sha256.Sum256(payload)
	if len(payload) > MaxOptimisationShardingPreviewBytes || hex.EncodeToString(sum[:]) != r.SHA256 {
		return nil, ErrInvalidOptimisationShardingPreview
	}
	r.Payload = payload
	r.CreatedAt = time.UnixMilli(created).UTC()
	if approvedInstallation.Valid || approvedRepo.Valid || approvedBy.Valid || approvedHead.Valid || approvedSource.Valid || approvedPreview.Valid || approvedAt.Valid {
		if !approvedInstallation.Valid || approvedInstallation.String != r.InstallationID || !approvedRepo.Valid || approvedRepo.String != r.Repository || !approvedBy.Valid || approvedBy.String == "" || !approvedAt.Valid || !validOptimisationHash(approvedHead.String, 40) || !validOptimisationHash(approvedSource.String, 64) || !validOptimisationHash(approvedPreview.String, 64) || approvedPreview.String != r.SHA256 {
			return nil, ErrInvalidOptimisationShardingPreview
		}
		r.Approval = &OptimisationShardingApproval{
			PreviewID: r.ID, InstallationID: r.InstallationID, Repository: r.Repository,
			ApprovedBy: approvedBy.String, HeadSHA: approvedHead.String, SourceSHA256: approvedSource.String,
			PreviewSHA256: approvedPreview.String, CreatedAt: time.UnixMilli(approvedAt.Int64).UTC(),
		}
	}
	if publishedInstallation.Valid || publishedRepo.Valid || publishedBy.Valid || publishedBranch.Valid || publishedBase.Valid || publishedBaseSHA.Valid || publishedHead.Valid || publishedSource.Valid || publishedPreview.Valid || publishedNumber.Valid || publishedURL.Valid || publishedAt.Valid {
		if r.Approval == nil || !publishedInstallation.Valid || publishedInstallation.String != r.InstallationID || !publishedRepo.Valid || publishedRepo.String != r.Repository || !publishedBy.Valid || publishedBy.String == "" || !validOptimisationBranch(publishedBranch.String) || !validOptimisationBranch(publishedBase.String) || !validOptimisationHash(publishedBaseSHA.String, 40) || !validOptimisationHash(publishedHead.String, 40) || !validOptimisationHash(publishedSource.String, 64) || !validOptimisationHash(publishedPreview.String, 64) || publishedPreview.String != r.SHA256 || publishedBaseSHA.String != r.Approval.HeadSHA || publishedHead.String != r.Approval.HeadSHA || publishedSource.String != r.Approval.SourceSHA256 || !publishedNumber.Valid || publishedNumber.Int64 <= 0 || publishedNumber.Int64 > 1<<31-1 || !publishedURL.Valid || publishedURL.String == "" || len(publishedURL.String) > 1024 || !utf8.ValidString(publishedURL.String) || strings.ContainsAny(publishedURL.String, " \t\r\n\x00") || !strings.HasPrefix(publishedURL.String, "https://") && !strings.HasPrefix(publishedURL.String, "http://") || !publishedAt.Valid {
			return nil, ErrInvalidOptimisationShardingPreview
		}
		r.Publication = &OptimisationShardingPublication{
			PreviewID: r.ID, InstallationID: r.InstallationID, Repository: r.Repository,
			PublishedBy: publishedBy.String, Branch: publishedBranch.String, BaseBranch: publishedBase.String,
			BaseSHA: publishedBaseSHA.String, HeadSHA: publishedHead.String, SourceSHA256: publishedSource.String,
			PreviewSHA256: publishedPreview.String, PullNumber: int(publishedNumber.Int64), PullURL: publishedURL.String,
			CreatedAt: time.UnixMilli(publishedAt.Int64).UTC(),
		}
	}
	return r, nil
}

// ApproveOptimisationShardingPreview records one exact approval of a retained
// sharding preview. The preview is not rewritten, and a repeat by the same
// binding is a no-op with no second audit row. It never publishes.
func (s *Store) ApproveOptimisationShardingPreview(ctx context.Context, installation, repo, id, headSHA, sourceSHA256, previewSHA256 string, actor *AuditEvent) (*OptimisationShardingApproval, error) {
	if actor == nil || strings.TrimSpace(actor.ActorID) == "" || len(actor.ActorID) > 128 || strings.TrimSpace(actor.ActorKind) == "" || len(actor.ActorKind) > 64 || len(actor.ActorName) > 200 || len(actor.IP) > 128 ||
		installation == "" || len(installation) > 128 || !strings.HasPrefix(id, PrefixOptimisationShardingPreview+"_") || len(id) > 128 || strings.ContainsAny(id, " /\r\n\x00") || !validOptimisationRepository(repo) ||
		!validOptimisationHash(headSHA, 40) || !validOptimisationHash(sourceSHA256, 64) || !validOptimisationHash(previewSHA256, 64) {
		return nil, ErrInvalidOptimisationShardingPreview
	}
	now := s.Now().UTC().Truncate(time.Millisecond)
	var out *OptimisationShardingApproval
	err := s.tx(ctx, func(tx *sql.Tx) error {
		var version, storedSHA string
		var created int64
		var payload []byte
		err := tx.QueryRowContext(ctx, `SELECT version,sha256,payload,created_at FROM optimisation_sharding_previews WHERE installation_id = ? AND repository = ? AND id = ? AND created_at > ?`,
			installation, repo, id, ms(now.Add(-OptimisationShardingPreviewRetention))).Scan(&version, &storedSHA, &payload, &created)
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
		sum := sha256.Sum256(payload)
		if len(payload) > MaxOptimisationShardingPreviewBytes || hex.EncodeToString(sum[:]) != storedSHA {
			return ErrInvalidOptimisationShardingPreview
		}
		if version != OptimisationShardingPreviewVersion || storedSHA != previewSHA256 {
			return ErrConflict
		}
		var existing OptimisationShardingApproval
		var at int64
		err = tx.QueryRowContext(ctx, `SELECT preview_id,installation_id,repository,approved_by,head_sha,source_sha256,preview_sha256,created_at FROM optimisation_sharding_preview_approvals WHERE preview_id = ?`, id).
			Scan(&existing.PreviewID, &existing.InstallationID, &existing.Repository, &existing.ApprovedBy, &existing.HeadSHA, &existing.SourceSHA256, &existing.PreviewSHA256, &at)
		switch {
		case err == nil:
			if existing.InstallationID != installation || existing.Repository != repo || existing.HeadSHA != headSHA || existing.SourceSHA256 != sourceSHA256 || existing.PreviewSHA256 != previewSHA256 {
				return ErrConflict
			}
			existing.CreatedAt = time.UnixMilli(at).UTC()
			out = &existing
			return nil
		case !errors.Is(err, sql.ErrNoRows):
			return err
		}
		out = &OptimisationShardingApproval{
			PreviewID: id, InstallationID: installation, Repository: repo, ApprovedBy: actor.ActorID,
			HeadSHA: headSHA, SourceSHA256: sourceSHA256, PreviewSHA256: previewSHA256, CreatedAt: now,
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO optimisation_sharding_preview_approvals(preview_id,installation_id,repository,approved_by,head_sha,source_sha256,preview_sha256,created_at) VALUES(?,?,?,?,?,?,?,?)`,
			out.PreviewID, out.InstallationID, out.Repository, out.ApprovedBy, out.HeadSHA, out.SourceSHA256, out.PreviewSHA256, ms(now)); err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO audit_events (`+auditCols+`) VALUES (?,?,?,?,?,?,?,?,?,?,?)`,
			NewID(PrefixAudit), actor.ActorID, actor.ActorName, actor.ActorKind, "optimisation.sharding_preview_approved", "optimisation_sharding_preview", id, "", "", actor.IP, ms(now))
		return err
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// PutOptimisationShardingPublication records the one draft pull request opened for
// an approved sharding preview. The GitHub write already happened, so this is the
// durable record of it: it refuses a preview that is expired, unapproved or
// bound differently, and a repeat of the same record is a no-op.
func (s *Store) PutOptimisationShardingPublication(ctx context.Context, r *OptimisationShardingPublication, actor *AuditEvent) error {
	if r == nil || actor == nil || strings.TrimSpace(actor.ActorID) == "" || len(actor.ActorID) > 128 || strings.TrimSpace(actor.ActorKind) == "" || len(actor.ActorKind) > 64 || len(actor.ActorName) > 200 || len(actor.IP) > 128 ||
		r.PublishedBy != actor.ActorID || !strings.HasPrefix(r.PreviewID, PrefixOptimisationShardingPreview+"_") || len(r.PreviewID) > 128 || strings.ContainsAny(r.PreviewID, " /\r\n\x00") ||
		r.InstallationID == "" || len(r.InstallationID) > 128 || !validOptimisationRepository(r.Repository) || !validOptimisationBranch(r.Branch) || !validOptimisationBranch(r.BaseBranch) ||
		!validOptimisationHash(r.BaseSHA, 40) || !validOptimisationHash(r.HeadSHA, 40) || !validOptimisationHash(r.SourceSHA256, 64) || !validOptimisationHash(r.PreviewSHA256, 64) ||
		r.PullNumber <= 0 || r.PullNumber > 1<<31-1 || r.PullURL == "" || len(r.PullURL) > 1024 || !utf8.ValidString(r.PullURL) || strings.ContainsAny(r.PullURL, " \t\r\n\x00") || !strings.HasPrefix(r.PullURL, "https://") && !strings.HasPrefix(r.PullURL, "http://") {
		return ErrInvalidOptimisationShardingPreview
	}
	now := s.Now().UTC().Truncate(time.Millisecond)
	err := s.tx(ctx, func(tx *sql.Tx) error {
		var version, storedSHA string
		var created int64
		var payload []byte
		err := tx.QueryRowContext(ctx, `SELECT version,sha256,payload,created_at FROM optimisation_sharding_previews WHERE installation_id = ? AND repository = ? AND id = ? AND created_at > ?`,
			r.InstallationID, r.Repository, r.PreviewID, ms(now.Add(-OptimisationShardingPreviewRetention))).Scan(&version, &storedSHA, &payload, &created)
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
		sum := sha256.Sum256(payload)
		if len(payload) > MaxOptimisationShardingPreviewBytes || hex.EncodeToString(sum[:]) != storedSHA {
			return ErrInvalidOptimisationShardingPreview
		}
		if version != OptimisationShardingPreviewVersion || storedSHA != r.PreviewSHA256 {
			return ErrConflict
		}
		var approvedBy, approvedHead, approvedSource, approvedPreview string
		var approvedAt int64
		err = tx.QueryRowContext(ctx, `SELECT approved_by,head_sha,source_sha256,preview_sha256,created_at FROM optimisation_sharding_preview_approvals WHERE preview_id = ?`, r.PreviewID).
			Scan(&approvedBy, &approvedHead, &approvedSource, &approvedPreview, &approvedAt)
		if errors.Is(err, sql.ErrNoRows) {
			return ErrConflict
		}
		if err != nil {
			return err
		}
		if approvedHead != r.HeadSHA || approvedSource != r.SourceSHA256 || approvedPreview != r.PreviewSHA256 || r.BaseSHA != r.HeadSHA {
			return ErrConflict
		}
		var existing OptimisationShardingPublication
		var at int64
		err = tx.QueryRowContext(ctx, `SELECT preview_id,installation_id,repository,published_by,branch,base_branch,base_sha,head_sha,source_sha256,preview_sha256,pull_number,pull_url,created_at FROM optimisation_sharding_publications WHERE preview_id = ?`, r.PreviewID).
			Scan(&existing.PreviewID, &existing.InstallationID, &existing.Repository, &existing.PublishedBy, &existing.Branch, &existing.BaseBranch, &existing.BaseSHA, &existing.HeadSHA, &existing.SourceSHA256, &existing.PreviewSHA256, &existing.PullNumber, &existing.PullURL, &at)
		switch {
		case err == nil:
			if existing.InstallationID != r.InstallationID || existing.Repository != r.Repository || existing.PublishedBy != r.PublishedBy || existing.Branch != r.Branch || existing.BaseBranch != r.BaseBranch || existing.BaseSHA != r.BaseSHA || existing.HeadSHA != r.HeadSHA || existing.SourceSHA256 != r.SourceSHA256 || existing.PreviewSHA256 != r.PreviewSHA256 || existing.PullNumber != r.PullNumber || existing.PullURL != r.PullURL {
				return ErrConflict
			}
			r.CreatedAt = time.UnixMilli(at).UTC()
			return nil
		case !errors.Is(err, sql.ErrNoRows):
			return err
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO optimisation_sharding_publications(preview_id,installation_id,repository,published_by,branch,base_branch,base_sha,head_sha,source_sha256,preview_sha256,pull_number,pull_url,created_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?)`,
			r.PreviewID, r.InstallationID, r.Repository, r.PublishedBy, r.Branch, r.BaseBranch, r.BaseSHA, r.HeadSHA, r.SourceSHA256, r.PreviewSHA256, r.PullNumber, r.PullURL, ms(now)); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO audit_events (`+auditCols+`) VALUES (?,?,?,?,?,?,?,?,?,?,?)`,
			NewID(PrefixAudit), actor.ActorID, actor.ActorName, actor.ActorKind, "optimisation.sharding_preview_published", "optimisation_sharding_preview", r.PreviewID, "", "", actor.IP, ms(now)); err != nil {
			return err
		}
		r.CreatedAt = now
		return nil
	})
	return err
}
