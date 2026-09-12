package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

const MaxProvisioningSelection = 5000

var ErrProvisioningSelectionTooLarge = errors.New("more than 5000 items match; narrow the filters before selecting all")

// ProvisioningSelection freezes the identities behind an all-matching selection.
// Later arrivals are never silently included in an operator's confirmation.
func (s *Store) ProvisioningSelection(ctx context.Context, f JobFilter) ([]string, error) {
	f.States = []JobState{JobQueued}
	f.ManagedOnly = true
	where, args := jobWhere(f)
	rows, err := s.read.QueryContext(ctx, "SELECT id FROM jobs "+where+" ORDER BY queued_at, id LIMIT ?", append(args, MaxProvisioningSelection+1)...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	ids := []string{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	if len(ids) > MaxProvisioningSelection {
		return nil, ErrProvisioningSelectionTooLarge
	}
	return ids, rows.Err()
}

type ProvisioningResult struct {
	ID    string `json:"id"`
	OK    bool   `json:"ok"`
	Error string `json:"error,omitempty"`
}

// ControlProvisioning changes only operator state, in one transaction. A job
// that started since selection is skipped, and webhook replay cannot undo it.
func (s *Store) ControlProvisioning(ctx context.Context, ids []string, action string) ([]ProvisioningResult, error) {
	state, urgent := "", false
	switch action {
	case "pause":
		state = "paused"
	case "resume":
	case "delete":
		state = "deleted"
	case "run_now":
		urgent = true
	default:
		return nil, fmt.Errorf("unknown provisioning action %q", action)
	}
	if len(ids) == 0 || len(ids) > MaxProvisioningSelection {
		return nil, fmt.Errorf("select between 1 and %d items", MaxProvisioningSelection)
	}
	results := []ProvisioningResult{}
	err := s.tx(ctx, func(tx *sql.Tx) error {
		seen := map[string]bool{}
		for _, id := range ids {
			if seen[id] {
				continue
			}
			seen[id] = true
			j, err := scanJob(tx.QueryRowContext(ctx, "SELECT "+jobCols+" FROM jobs WHERE id=?", id))
			if errors.Is(err, sql.ErrNoRows) {
				results = append(results, ProvisioningResult{ID: id, Error: "This item no longer exists."})
				continue
			}
			if err != nil {
				return err
			}
			if j.State != JobQueued || (!j.Matched && j.RunnerID == "" && HostedJob(j.Labels)) {
				results = append(results, ProvisioningResult{ID: id, Error: "This item is no longer queued for this fleet."})
				continue
			}
			if _, err = tx.ExecContext(ctx, "UPDATE jobs SET provisioning=?, provision_now=? WHERE id=?", state, boolInt(urgent), id); err != nil {
				return err
			}
			results = append(results, ProvisioningResult{ID: id, OK: true})
		}
		return nil
	})
	return results, err
}

// LastProvisioned includes removed runners so quick jobs and restarts do not
// reset a pool's place in the fairness rotation.
func (s *Store) LastProvisioned(ctx context.Context) (map[string]time.Time, error) {
	rows, err := s.read.QueryContext(ctx, "SELECT pool_id, MAX(created_at) FROM runners GROUP BY pool_id")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]time.Time{}
	for rows.Next() {
		var id string
		var stamp int64
		if err := rows.Scan(&id, &stamp); err != nil {
			return nil, err
		}
		out[id] = at(stamp)
	}
	return out, rows.Err()
}

func (s *Store) ProvisioningCounts(ctx context.Context, f JobFilter) (map[string]int, error) {
	f.States = []JobState{JobQueued}
	f.ManagedOnly = true
	f.Provisioning = nil
	where, args := jobWhere(f)
	rows, err := s.read.QueryContext(ctx, "SELECT CASE WHEN provisioning != '' THEN provisioning WHEN provision_now=1 THEN 'expedited' ELSE 'ready' END,COUNT(*) FROM jobs "+where+" GROUP BY 1", args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]int{"ready": 0, "expedited": 0, "paused": 0, "deleted": 0}
	for rows.Next() {
		var key string
		var n int
		if err := rows.Scan(&key, &n); err != nil {
			return nil, err
		}
		out[key] = n
	}
	return out, rows.Err()
}
