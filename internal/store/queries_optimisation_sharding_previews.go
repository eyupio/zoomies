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
	ID             string                        `json:"id"`
	InstallationID string                        `json:"installation_id"`
	Repository     string                        `json:"repository"`
	InitiatingUser string                        `json:"initiating_user"`
	Version        string                        `json:"version"`
	SHA256         string                        `json:"sha256"`
	Payload        json.RawMessage               `json:"payload"`
	CreatedAt      time.Time                     `json:"created_at"`
	Approval       *OptimisationShardingApproval `json:"approval,omitempty"`
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
	err := s.read.QueryRowContext(ctx, `SELECT p.id,p.installation_id,p.repository,p.initiating_user,p.version,p.sha256,p.payload,p.created_at,
			a.installation_id,a.repository,a.approved_by,a.head_sha,a.source_sha256,a.preview_sha256,a.created_at
		FROM optimisation_sharding_previews p
		LEFT JOIN optimisation_sharding_preview_approvals a ON a.preview_id = p.id
		WHERE p.installation_id = ? AND p.repository = ? AND p.id = ? AND p.created_at > ?`,
		installation, repo, id, ms(s.Now().Add(-OptimisationShardingPreviewRetention))).Scan(&r.ID, &r.InstallationID, &r.Repository, &r.InitiatingUser, &r.Version, &r.SHA256, &payload, &created,
		&approvedInstallation, &approvedRepo, &approvedBy, &approvedHead, &approvedSource, &approvedPreview, &approvedAt)
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
