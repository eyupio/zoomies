package store

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

// EliRepairWatch is a person's explicit decision to keep following one pull
// request. It is deliberately separate from EliRepair: a repair is one attempt
// to change the branch, while a watch survives that attempt and a controller
// restart. Checks reports whether failed checks may lead to a repair, and
// Conflicts whether merge conflicts may do so; the second capability is stored
// now so an old row still says what was asked for when conflict repair arrives.
type EliRepairWatch struct {
	ID               string    `json:"id"`
	InstallationID   string    `json:"installation_id"`
	Repo             string    `json:"repo"`
	PullNumber       int       `json:"pull_number"`
	OwnerID          string    `json:"owner_id"`
	PolicyProviderID string    `json:"policy_provider_id"`
	PolicyUpdatedAt  time.Time `json:"policy_updated_at"`
	Checks           bool      `json:"checks"`
	Conflicts        bool      `json:"conflicts"`
	Enabled          bool      `json:"enabled"`
	PausedReason     string    `json:"paused_reason,omitempty"`
	HeadSHA          string    `json:"head_sha,omitempty"`
	BaseRef          string    `json:"base_ref,omitempty"`
	BaseSHA          string    `json:"base_sha,omitempty"`
	LastObservedAt   time.Time `json:"last_observed_at,omitempty"`
	NextCheckAt      time.Time `json:"next_check_at,omitempty"`
	LeaseToken       string    `json:"-"`
	LeaseUntil       time.Time `json:"-"`
	StopReason       string    `json:"stop_reason,omitempty"`
	CheckState       string    `json:"check_state,omitempty"`
	CreatedAt        time.Time `json:"created_at"`
	UpdatedAt        time.Time `json:"updated_at"`
}

const eliRepairWatchCols = `id,installation_id,repo,pull_number,owner_id,policy_provider_id,policy_updated_at,checks,conflicts,enabled,paused_reason,head_sha,base_ref,base_sha,last_observed_at,next_check_at,lease_token,lease_until,stop_reason,check_state,created_at,updated_at`

func scanEliRepairWatch(row interface{ Scan(...any) error }) (*EliRepairWatch, error) {
	var w EliRepairWatch
	var policyUpdated, lastObserved, nextCheck, leaseUntil, created, updated int64
	err := row.Scan(&w.ID, &w.InstallationID, &w.Repo, &w.PullNumber, &w.OwnerID, &w.PolicyProviderID,
		&policyUpdated, &w.Checks, &w.Conflicts, &w.Enabled, &w.PausedReason, &w.HeadSHA,
		&w.BaseRef, &w.BaseSHA, &lastObserved, &nextCheck, &w.LeaseToken, &leaseUntil,
		&w.StopReason, &w.CheckState, &created, &updated)
	if policyUpdated > 0 {
		w.PolicyUpdatedAt = at(policyUpdated)
	}
	if lastObserved > 0 {
		w.LastObservedAt = at(lastObserved)
	}
	if nextCheck > 0 {
		w.NextCheckAt = at(nextCheck)
	}
	if leaseUntil > 0 {
		w.LeaseUntil = at(leaseUntil)
	}
	w.CreatedAt, w.UpdatedAt = at(created), at(updated)
	return &w, err
}

// AdmitEliRepairWatch creates a watch or explicitly resumes one that a person
// paused. Only this call enables a watch: changing a policy, seeing a webhook
// or restarting the controller never revives one behind the person's back.
func (s *Store) AdmitEliRepairWatch(ctx context.Context, w EliRepairWatch) (*EliRepairWatch, error) {
	now := s.Now()
	var out *EliRepairWatch
	err := s.tx(ctx, func(tx *sql.Tx) error {
		existing, err := scanEliRepairWatch(tx.QueryRowContext(ctx,
			`SELECT `+eliRepairWatchCols+` FROM eli_repair_watches WHERE repo COLLATE NOCASE=? AND pull_number=?`,
			w.Repo, w.PullNumber))
		switch {
		case errors.Is(err, sql.ErrNoRows):
			w.ID = NewID(PrefixEliRepairWatch)
			w.Enabled = true
			w.PausedReason, w.StopReason = "", ""
			w.LeaseToken, w.LeaseUntil = "", time.Time{}
			w.CreatedAt, w.UpdatedAt = now, now
			w.NextCheckAt = now
			if w.CheckState == "" {
				w.CheckState = "unknown"
			}
			_, err = tx.ExecContext(ctx, `INSERT INTO eli_repair_watches(`+eliRepairWatchCols+`) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
				w.ID, w.InstallationID, w.Repo, w.PullNumber, w.OwnerID, w.PolicyProviderID, ms(w.PolicyUpdatedAt),
				boolInt(w.Checks), boolInt(w.Conflicts), boolInt(w.Enabled), w.PausedReason, w.HeadSHA,
				w.BaseRef, w.BaseSHA, ms(w.LastObservedAt), ms(w.NextCheckAt), w.LeaseToken, ms(w.LeaseUntil),
				w.StopReason, w.CheckState, ms(w.CreatedAt), ms(w.UpdatedAt))
			if err != nil {
				return err
			}
			out = &w
			return nil
		case err != nil:
			return err
		}
		// A repeated admission must not break an observation already in
		// flight: its lease, identity and next check are the observer's to
		// finish. The explicit decision can still update who authorised the
		// watch and which capabilities it has.
		observing := existing.LeaseToken != "" && existing.LeaseUntil.After(now)
		existing.InstallationID = w.InstallationID
		existing.OwnerID = w.OwnerID
		existing.PolicyProviderID = w.PolicyProviderID
		existing.PolicyUpdatedAt = w.PolicyUpdatedAt
		existing.Checks, existing.Conflicts = w.Checks, w.Conflicts
		existing.Enabled = true
		existing.PausedReason, existing.StopReason = "", ""
		existing.UpdatedAt = now
		if observing {
			_, err = tx.ExecContext(ctx, `UPDATE eli_repair_watches SET installation_id=?,owner_id=?,policy_provider_id=?,policy_updated_at=?,checks=?,conflicts=?,enabled=1,paused_reason='',stop_reason='',updated_at=? WHERE id=?`,
				existing.InstallationID, existing.OwnerID, existing.PolicyProviderID, ms(existing.PolicyUpdatedAt),
				boolInt(existing.Checks), boolInt(existing.Conflicts), ms(existing.UpdatedAt), existing.ID)
		} else {
			existing.HeadSHA, existing.BaseRef, existing.BaseSHA = w.HeadSHA, w.BaseRef, w.BaseSHA
			existing.LeaseToken, existing.LeaseUntil = "", time.Time{}
			existing.NextCheckAt = now
			_, err = tx.ExecContext(ctx, `UPDATE eli_repair_watches SET installation_id=?,owner_id=?,policy_provider_id=?,policy_updated_at=?,checks=?,conflicts=?,enabled=1,paused_reason='',head_sha=?,base_ref=?,base_sha=?,check_state='unknown',next_check_at=?,lease_token='',lease_until=0,stop_reason='',updated_at=? WHERE id=?`,
				existing.InstallationID, existing.OwnerID, existing.PolicyProviderID, ms(existing.PolicyUpdatedAt),
				boolInt(existing.Checks), boolInt(existing.Conflicts), existing.HeadSHA, existing.BaseRef, existing.BaseSHA,
				ms(existing.NextCheckAt), ms(existing.UpdatedAt), existing.ID)
		}
		if err != nil {
			return err
		}
		out = existing
		return nil
	})
	if err != nil {
		return nil, wrapWrite(err)
	}
	return out, nil
}

func (s *Store) EliRepairWatch(ctx context.Context, id string) (*EliRepairWatch, error) {
	w, err := scanEliRepairWatch(s.read.QueryRowContext(ctx, `SELECT `+eliRepairWatchCols+` FROM eli_repair_watches WHERE id=?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return w, err
}

func (s *Store) EliRepairWatchForPull(ctx context.Context, repo string, pull int) (*EliRepairWatch, error) {
	w, err := scanEliRepairWatch(s.read.QueryRowContext(ctx, `SELECT `+eliRepairWatchCols+` FROM eli_repair_watches WHERE repo COLLATE NOCASE=? AND pull_number=?`, repo, pull))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return w, err
}

// EliRepairWatches lists every watch, including paused and stopped ones, so a
// caller can explain why a pull request is or is not being followed.
func (s *Store) EliRepairWatches(ctx context.Context) ([]*EliRepairWatch, error) {
	rows, err := s.read.QueryContext(ctx, `SELECT `+eliRepairWatchCols+` FROM eli_repair_watches ORDER BY repo COLLATE NOCASE,pull_number`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*EliRepairWatch{}
	for rows.Next() {
		w, err := scanEliRepairWatch(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, w)
	}
	return out, rows.Err()
}

// ClaimDueEliRepairWatch leases one due watch for the caller. The lease is what
// stops two passes, or a restarted controller, from observing the same pull
// request at once. A watch already leased is not returned until its lease
// expires or RecoverEliRepairWatches releases it.
func (s *Store) ClaimDueEliRepairWatch(ctx context.Context, now time.Time, ttl time.Duration) (*EliRepairWatch, error) {
	var out *EliRepairWatch
	err := s.tx(ctx, func(tx *sql.Tx) error {
		w, err := scanEliRepairWatch(tx.QueryRowContext(ctx,
			`SELECT `+eliRepairWatchCols+` FROM eli_repair_watches WHERE enabled=1 AND next_check_at<=? AND lease_until<=? ORDER BY next_check_at,id LIMIT 1`,
			ms(now), ms(now)))
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
		token := NewSecret(16)
		result, err := tx.ExecContext(ctx, `UPDATE eli_repair_watches SET lease_token=?,lease_until=?,updated_at=? WHERE id=? AND enabled=1 AND lease_until<=?`,
			token, ms(now.Add(ttl)), ms(now), w.ID, ms(now))
		if err != nil {
			return err
		}
		n, err := result.RowsAffected()
		if err != nil {
			return err
		}
		if n == 0 {
			return ErrNotFound
		}
		w.LeaseToken, w.LeaseUntil, w.UpdatedAt = token, now.Add(ttl), now
		out = w
		return nil
	})
	if err != nil {
		return nil, wrapWrite(err)
	}
	return out, nil
}

// ReleaseEliRepairWatch records one observation and returns the watch to the
// queue. A stale holder is refused, so work that continued after its lease was
// reclaimed cannot overwrite a newer observation.
func (s *Store) ReleaseEliRepairWatch(ctx context.Context, id, token, headSHA, baseRef, baseSHA, checkState string, next time.Time) error {
	now := s.Now()
	result, err := s.exec(ctx, `UPDATE eli_repair_watches SET lease_token='',lease_until=0,last_observed_at=?,head_sha=?,base_ref=?,base_sha=?,check_state=?,next_check_at=?,updated_at=? WHERE id=? AND lease_token=?`,
		ms(now), headSHA, baseRef, baseSHA, checkState, ms(next), ms(now), id, token)
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if err == nil && n == 0 {
		return ErrConflict
	}
	return err
}

func (s *Store) PauseEliRepairWatch(ctx context.Context, id, reason string) error {
	return s.setEliRepairWatchEnabled(ctx, id, `paused_reason=?,stop_reason=''`, reason)
}

func (s *Store) StopEliRepairWatch(ctx context.Context, id, reason string) error {
	return s.setEliRepairWatchEnabled(ctx, id, `paused_reason='',stop_reason=?`, reason)
}

func (s *Store) setEliRepairWatchEnabled(ctx context.Context, id, fields, reason string) error {
	result, err := s.exec(ctx, `UPDATE eli_repair_watches SET enabled=0,`+fields+`,lease_token='',lease_until=0,updated_at=? WHERE id=?`, reason, ms(s.Now()), id)
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if err == nil && n == 0 {
		return ErrNotFound
	}
	return err
}

// RecoverEliRepairWatches releases leases a previous controller left behind and
// makes an interrupted observation due at once. Observation is read-only, so
// replaying it is safe; a repair attempt is recovered separately by
// InterruptEliRepairs, which never repeats an uncertain publication.
func (s *Store) RecoverEliRepairWatches(ctx context.Context, now time.Time) error {
	_, err := s.exec(ctx, `UPDATE eli_repair_watches SET lease_token='',lease_until=0,next_check_at=CASE WHEN enabled=1 THEN ? ELSE next_check_at END,updated_at=? WHERE lease_token<>''`,
		ms(now), ms(now))
	return err
}
