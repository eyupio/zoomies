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
	ChainAttempts    int       `json:"chain_attempts"`
	LastFailure      string    `json:"last_failure_signature,omitempty"`
	ChainStopped     string    `json:"chain_stopped_reason,omitempty"`
	CheckEvidence    string    `json:"check_evidence,omitempty"`
	ConflictEvidence string    `json:"conflict_evidence,omitempty"`
	CreatedAt        time.Time `json:"created_at"`
	UpdatedAt        time.Time `json:"updated_at"`
}

const eliRepairWatchCols = `id,installation_id,repo,pull_number,owner_id,policy_provider_id,policy_updated_at,checks,conflicts,enabled,paused_reason,head_sha,base_ref,base_sha,last_observed_at,next_check_at,lease_token,lease_until,stop_reason,check_state,chain_attempts,last_failure_signature,chain_stopped_reason,check_evidence,conflict_evidence,created_at,updated_at`

func scanEliRepairWatch(row interface{ Scan(...any) error }) (*EliRepairWatch, error) {
	var w EliRepairWatch
	var policyUpdated, lastObserved, nextCheck, leaseUntil, created, updated int64
	err := row.Scan(&w.ID, &w.InstallationID, &w.Repo, &w.PullNumber, &w.OwnerID, &w.PolicyProviderID,
		&policyUpdated, &w.Checks, &w.Conflicts, &w.Enabled, &w.PausedReason, &w.HeadSHA,
		&w.BaseRef, &w.BaseSHA, &lastObserved, &nextCheck, &w.LeaseToken, &leaseUntil,
		&w.StopReason, &w.CheckState, &w.ChainAttempts, &w.LastFailure, &w.ChainStopped, &w.CheckEvidence, &w.ConflictEvidence, &created, &updated)
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
			if w.CheckEvidence == "" {
				w.CheckEvidence = "[]"
			}
			if w.ConflictEvidence == "" {
				w.ConflictEvidence = "{}"
			}
			_, err = tx.ExecContext(ctx, `INSERT INTO eli_repair_watches(`+eliRepairWatchCols+`) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
				w.ID, w.InstallationID, w.Repo, w.PullNumber, w.OwnerID, w.PolicyProviderID, ms(w.PolicyUpdatedAt),
				boolInt(w.Checks), boolInt(w.Conflicts), boolInt(w.Enabled), w.PausedReason, w.HeadSHA,
				w.BaseRef, w.BaseSHA, ms(w.LastObservedAt), ms(w.NextCheckAt), w.LeaseToken, ms(w.LeaseUntil),
				w.StopReason, w.CheckState, w.ChainAttempts, w.LastFailure, w.ChainStopped, w.CheckEvidence, w.ConflictEvidence, ms(w.CreatedAt), ms(w.UpdatedAt))
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
			existing.CheckState = "unknown"
			existing.NextCheckAt = now
			_, err = tx.ExecContext(ctx, `UPDATE eli_repair_watches SET installation_id=?,owner_id=?,policy_provider_id=?,policy_updated_at=?,checks=?,conflicts=?,enabled=1,paused_reason='',head_sha=?,base_ref=?,base_sha=?,check_state='unknown',check_evidence='[]',next_check_at=?,lease_token='',lease_until=0,stop_reason='',updated_at=? WHERE id=?`,
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

// CreateEliRepairWatchIfAbsent admits a watch only when none exists. Backfill
// uses it so a race with another admission cannot turn an existing paused watch
// back on; an explicit admission is still the only thing that resumes one.
func (s *Store) CreateEliRepairWatchIfAbsent(ctx context.Context, w EliRepairWatch) (*EliRepairWatch, bool, error) {
	now := s.Now()
	created := false
	var out *EliRepairWatch
	err := s.tx(ctx, func(tx *sql.Tx) error {
		existing, err := scanEliRepairWatch(tx.QueryRowContext(ctx,
			`SELECT `+eliRepairWatchCols+` FROM eli_repair_watches WHERE repo COLLATE NOCASE=? AND pull_number=?`,
			w.Repo, w.PullNumber))
		switch {
		case err == nil:
			out = existing
			return nil
		case !errors.Is(err, sql.ErrNoRows):
			return err
		}
		w.ID = NewID(PrefixEliRepairWatch)
		w.Enabled = true
		w.PausedReason, w.StopReason = "", ""
		w.LeaseToken, w.LeaseUntil = "", time.Time{}
		w.CreatedAt, w.UpdatedAt = now, now
		w.NextCheckAt = now
		if w.CheckState == "" {
			w.CheckState = "unknown"
		}
		if w.CheckEvidence == "" {
			w.CheckEvidence = "[]"
		}
		if w.ConflictEvidence == "" {
			w.ConflictEvidence = "{}"
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO eli_repair_watches(`+eliRepairWatchCols+`) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
			w.ID, w.InstallationID, w.Repo, w.PullNumber, w.OwnerID, w.PolicyProviderID, ms(w.PolicyUpdatedAt),
			boolInt(w.Checks), boolInt(w.Conflicts), boolInt(w.Enabled), w.PausedReason, w.HeadSHA,
			w.BaseRef, w.BaseSHA, ms(w.LastObservedAt), ms(w.NextCheckAt), w.LeaseToken, ms(w.LeaseUntil),
			w.StopReason, w.CheckState, w.ChainAttempts, w.LastFailure, w.ChainStopped, w.CheckEvidence, w.ConflictEvidence, ms(w.CreatedAt), ms(w.UpdatedAt))
		if err != nil {
			return err
		}
		created = true
		out = &w
		return nil
	})
	if err != nil {
		return nil, false, wrapWrite(err)
	}
	return out, created, nil
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
//
// The loop asks every three seconds whether or not anybody has set a watch up, so
// the answer is nearly always that none is due, and it is looked for on the
// readers before the writer is taken for it. The claim under the writer reads
// again, so the look is only a hint: a watch that falls due a moment after it is
// claimed by the next ask, and a look that fails is no reason to skip the claim.
func (s *Store) ClaimDueEliRepairWatch(ctx context.Context, now time.Time, ttl time.Duration) (*EliRepairWatch, error) {
	if s.readOnly {
		return nil, ErrReadOnly
	}
	var due bool
	if s.read.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM eli_repair_watches WHERE enabled=1 AND next_check_at<=? AND lease_until<=?)`,
		ms(now), ms(now)).Scan(&due) == nil && !due {
		return nil, ErrNotFound
	}
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
func (s *Store) ReleaseEliRepairWatch(ctx context.Context, id, token, headSHA, baseRef, baseSHA, checkState, checkEvidence string, next time.Time) error {
	now := s.Now()
	result, err := s.exec(ctx, `UPDATE eli_repair_watches SET lease_token='',lease_until=0,last_observed_at=?,head_sha=?,base_ref=?,base_sha=?,check_state=?,check_evidence=?,next_check_at=?,updated_at=? WHERE id=? AND lease_token=?`,
		ms(now), headSHA, baseRef, baseSHA, checkState, checkEvidence, ms(next), ms(now), id, token)
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
	return s.setEliRepairWatchEnabled(ctx, id, "", `paused_reason=?,stop_reason=''`, reason)
}

func (s *Store) StopEliRepairWatch(ctx context.Context, id, reason string) error {
	return s.setEliRepairWatchEnabled(ctx, id, "", `paused_reason='',stop_reason=?`, reason)
}

// PauseEliRepairWatchLease and StopEliRepairWatchLease only change the row if
// the caller still holds the lease it claimed. An observation that outlived a
// pause and explicit re-admission must not stop the new watch.
func (s *Store) PauseEliRepairWatchLease(ctx context.Context, id, token, reason string) error {
	return s.setEliRepairWatchEnabled(ctx, id, token, `paused_reason=?,stop_reason=''`, reason)
}

func (s *Store) StopEliRepairWatchLease(ctx context.Context, id, token, reason string) error {
	return s.setEliRepairWatchEnabled(ctx, id, token, `paused_reason='',stop_reason=?`, reason)
}

func (s *Store) setEliRepairWatchEnabled(ctx context.Context, id, token, fields, reason string) error {
	query := `UPDATE eli_repair_watches SET enabled=0,` + fields + `,lease_token='',lease_until=0,updated_at=? WHERE id=?`
	args := []any{reason, ms(s.Now()), id}
	if token != "" {
		query += ` AND lease_token=?`
		args = append(args, token)
	}
	result, err := s.exec(ctx, query, args...)
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if err == nil && n == 0 {
		if token != "" {
			return ErrConflict
		}
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

// EnqueueEliWatchRepair starts one repair for a watched head and increments the
// watch chain in the same transaction. The dedup key still makes a redelivered
// check or a repeated observation harmless. When the chain reaches its limit the
// watch records the reason and no further repair is inserted.
func (s *Store) EnqueueEliWatchRepair(ctx context.Context, watchID string, r *EliRepair, signature string, limit int, exhaustedReason string) (bool, int, error) {
	added := false
	attempts := 0
	err := s.tx(ctx, func(tx *sql.Tx) error {
		var enabled bool
		var stopped string
		err := tx.QueryRowContext(ctx, `SELECT enabled,chain_attempts,chain_stopped_reason FROM eli_repair_watches WHERE id=?`, watchID).Scan(&enabled, &attempts, &stopped)
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
		if !enabled || stopped != "" {
			return nil
		}
		if limit <= 0 {
			limit = 3
		}
		stopChain := func() error {
			_, err := tx.ExecContext(ctx, `UPDATE eli_repair_watches SET chain_stopped_reason=?,updated_at=? WHERE id=?`, exhaustedReason, ms(s.Now()), watchID)
			return err
		}
		// The head's own repair is looked for before the limit is, because the
		// last allowed attempt is still working on the head the checks keep
		// reporting as failed. Stopping the chain for that head would write "a
		// person needs to look" over a repair that has not finished.
		var existing, existingState string
		err = tx.QueryRowContext(ctx, `SELECT id,state FROM eli_repairs WHERE dedup_key=?`, r.DedupKey).Scan(&existing, &existingState)
		switch {
		case err == nil:
			// An attempt that ended without moving the head leaves the checks
			// failing on the same commit, so no new head will ever arrive to
			// show the limit was reached. Say so now rather than never.
			if attempts >= limit && !eliRepairInFlight(existingState) {
				return stopChain()
			}
			return nil
		case !errors.Is(err, sql.ErrNoRows):
			return err
		}
		if attempts >= limit {
			return stopChain()
		}
		r.ID = NewID(PrefixEliRepair)
		r.WatchID = watchID
		r.State = "queued"
		r.CreatedAt, r.UpdatedAt = s.Now(), s.Now()
		if _, err = tx.ExecContext(ctx, `INSERT INTO eli_repairs (`+eliRepairCols+`) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
			r.ID, r.DedupKey, r.InstallationID, r.Repo, r.PullNumber, r.JobID, r.RunID, r.HeadSHA, r.CommitSHA, r.UserID, r.GitHubUserID, r.GitHubLogin, r.ProviderID, r.Trigger, boolInt(r.RequesterAdmin), r.Instruction, r.State, r.Message, r.CommentID, msp(r.PublishedAt), ms(r.CreatedAt), ms(r.UpdatedAt), repairDetailsJSON(r.Details), r.WatchID); err != nil {
			return err
		}
		attempts++
		_, err = tx.ExecContext(ctx, `UPDATE eli_repair_watches SET chain_attempts=?,last_failure_signature=?,chain_stopped_reason='',updated_at=? WHERE id=?`, attempts, signature, ms(s.Now()), watchID)
		added = err == nil
		return err
	})
	return added, attempts, wrapWrite(err)
}

// eliRepairInFlight reports whether an attempt is still queued or working, the
// same three states eliRepairActive names for SQL.
func eliRepairInFlight(state string) bool {
	switch state {
	case "queued", "working", "checking":
		return true
	}
	return false
}

// ResetEliWatchRepairChain starts a new incident after a healthy observation.
func (s *Store) ResetEliWatchRepairChain(ctx context.Context, id string) error {
	_, err := s.exec(ctx, `UPDATE eli_repair_watches SET chain_attempts=0,last_failure_signature='',chain_stopped_reason='',updated_at=? WHERE id=?`, ms(s.Now()), id)
	return err
}

// SetEliRepairWatchConflict records a conflict read only if the caller still
// holds the observation lease, so a stale read cannot replace a newer head.
func (s *Store) SetEliRepairWatchConflict(ctx context.Context, id, token, evidence string) error {
	result, err := s.exec(ctx, `UPDATE eli_repair_watches SET conflict_evidence=?,updated_at=? WHERE id=? AND lease_token=?`, evidence, ms(s.Now()), id, token)
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if err == nil && n == 0 {
		return ErrConflict
	}
	return err
}
