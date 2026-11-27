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

const ComparisonEvidenceVersion = "observed-comparison-v1"
const MaxComparisonEvidenceBytes = 2 << 20
const MaxComparisonsPerInstallation = 32
const MaxRetainedComparisons = 256
const ComparisonEvidenceRetention = 30 * 24 * time.Hour

var ErrInvalidComparisonEvidence = errors.New("invalid retained comparison evidence")

type ComparisonEvidence struct {
	ID             string          `json:"id"`
	InstallationID string          `json:"installation_id"`
	Repository     string          `json:"repository"`
	InitiatingUser string          `json:"initiating_user"`
	Version        string          `json:"version"`
	SHA256         string          `json:"sha256"`
	Payload        json.RawMessage `json:"payload"`
	CreatedAt      time.Time       `json:"created_at"`
}

// PutComparisonEvidence accepts an already authorised actor. The row and audit
// are one transaction; retries cannot overwrite evidence or renew its lifetime.
func (s *Store) PutComparisonEvidence(ctx context.Context, r *ComparisonEvidence, actor *AuditEvent) error {
	if r == nil || actor == nil || !strings.HasPrefix(r.ID, PrefixComparison+"_") || len(r.ID) > 128 || strings.ContainsAny(r.ID, " /\r\n\x00") || r.InstallationID == "" || len(r.InstallationID) > 128 || r.InitiatingUser == "" || len(r.InitiatingUser) > 128 || actor.ActorID != r.InitiatingUser || strings.TrimSpace(actor.ActorKind) == "" || len(actor.ActorKind) > 64 || len(actor.ActorName) > 200 || len(actor.IP) > 128 || r.Version != ComparisonEvidenceVersion || len(r.Payload) == 0 || len(r.Payload) > MaxComparisonEvidenceBytes || !utf8.Valid(r.Payload) {
		return ErrInvalidComparisonEvidence
	}
	owner, name, ok := strings.Cut(r.Repository, "/")
	if !ok || owner == "" || name == "" || strings.Contains(name, "/") || len(r.Repository) > 1024 {
		return ErrInvalidComparisonEvidence
	}
	var envelope map[string]json.RawMessage
	if json.Unmarshal(r.Payload, &envelope) != nil || envelope == nil {
		return ErrInvalidComparisonEvidence
	}
	var version string
	if json.Unmarshal(envelope["version"], &version) != nil || version != r.Version {
		return ErrInvalidComparisonEvidence
	}
	sum := sha256.Sum256(r.Payload)
	digest := hex.EncodeToString(sum[:])
	now := s.Now().UTC().Truncate(time.Millisecond)
	created := now
	err := s.tx(ctx, func(tx *sql.Tx) error {
		var target, kind string
		if err := tx.QueryRowContext(ctx, `SELECT target,target_type FROM installations WHERE id = ?`, r.InstallationID).Scan(&target, &kind); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return ErrInvalidComparisonEvidence
			}
			return err
		}
		// Folded, as the webhook path folds it: the target is stored in lower case and the repository arrives in GitHub's.
		if !(kind == string(TargetRepo) && strings.EqualFold(target, r.Repository) || kind == string(TargetOrg) && strings.EqualFold(target, owner)) {
			return ErrInvalidComparisonEvidence
		}
		var installation, repo, user, oldVersion, oldHash string
		var at int64
		var oldPayload []byte
		err := tx.QueryRowContext(ctx, `SELECT installation_id,repository,initiating_user,version,sha256,created_at,payload FROM comparison_evidence WHERE id = ?`, r.ID).Scan(&installation, &repo, &user, &oldVersion, &oldHash, &at, &oldPayload)
		if err == nil {
			if installation != r.InstallationID || repo != r.Repository || user != r.InitiatingUser || oldVersion != r.Version || oldHash != digest {
				return ErrConflict
			}
			if sha256.Sum256(oldPayload) != sum {
				return ErrInvalidComparisonEvidence
			}
			if at <= ms(now.Add(-ComparisonEvidenceRetention)) {
				return ErrNotFound
			}
			created = time.UnixMilli(at).UTC()
			return nil
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO comparison_evidence(id,installation_id,repository,initiating_user,version,sha256,payload,created_at) VALUES(?,?,?,?,?,?,?,?)`, r.ID, r.InstallationID, r.Repository, r.InitiatingUser, r.Version, digest, []byte(r.Payload), ms(now)); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO audit_events (`+auditCols+`) VALUES (?,?,?,?,?,?,?,?,?,?,?)`, NewID(PrefixAudit), actor.ActorID, actor.ActorName, actor.ActorKind, "comparison.evidence_retained", "comparison", r.ID, "", "", actor.IP, ms(now)); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM comparison_evidence WHERE created_at <= ?`, ms(now.Add(-ComparisonEvidenceRetention))); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM comparison_evidence WHERE installation_id = ? AND id NOT IN (SELECT id FROM comparison_evidence WHERE installation_id = ? ORDER BY sequence DESC LIMIT ?)`, r.InstallationID, r.InstallationID, MaxComparisonsPerInstallation); err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, `DELETE FROM comparison_evidence WHERE id NOT IN (SELECT id FROM comparison_evidence ORDER BY sequence DESC LIMIT ?)`, MaxRetainedComparisons)
		return err
	})
	if err == nil {
		r.SHA256 = digest
		r.CreatedAt = created
	}
	return err
}

// A scoped miss includes expired or evicted evidence. It must not be rendered
// as an empty cohort. The hash detects corruption; it is not an authorisation.
func (s *Store) GetComparisonEvidence(ctx context.Context, installation, repo, id string) (*ComparisonEvidence, error) {
	r := &ComparisonEvidence{}
	var payload []byte
	var created int64
	err := s.read.QueryRowContext(ctx, `SELECT id,installation_id,repository,initiating_user,version,sha256,payload,created_at FROM comparison_evidence WHERE installation_id = ? AND repository = ? AND id = ? AND created_at > ?`, installation, repo, id, ms(s.Now().Add(-ComparisonEvidenceRetention))).Scan(&r.ID, &r.InstallationID, &r.Repository, &r.InitiatingUser, &r.Version, &r.SHA256, &payload, &created)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	sum := sha256.Sum256(payload)
	if len(payload) > MaxComparisonEvidenceBytes || hex.EncodeToString(sum[:]) != r.SHA256 {
		return nil, ErrInvalidComparisonEvidence
	}
	r.Payload = payload
	r.CreatedAt = time.UnixMilli(created).UTC()
	return r, nil
}
