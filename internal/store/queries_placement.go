package store

import (
	"context"
	"time"
)

// Service time excludes the agent's admission queue. It is recorded once so
// duplicate task results cannot overweight one successful start.
func (s *Store) SetRunnerServiceTime(ctx context.Context, id string, d time.Duration) error {
	if d <= 0 || d > 30*time.Minute {
		return nil
	}
	_, err := s.exec(ctx, `UPDATE runners SET startup_service_ms=? WHERE id=? AND startup_service_ms IS NULL`, d.Milliseconds(), id)
	return err
}

type StartupEstimate struct {
	HostID, PoolID, Image string
	Samples               int
	Milliseconds          int64
}

// Limit the evidence before aggregating, so a long-lived fleet does not make
// scheduling scan its history. Sparse or stale evidence is a fallback, not 0s.
func (s *Store) StartupEstimates(ctx context.Context, since time.Time) ([]StartupEstimate, error) {
	rows, err := s.read.QueryContext(ctx, `SELECT host_id,pool_id,image,COUNT(*),CAST(AVG(startup_service_ms) AS INTEGER) FROM
 (SELECT host_id,pool_id,image,startup_service_ms FROM runners WHERE created_at>=? AND startup_service_ms IS NOT NULL ORDER BY created_at DESC LIMIT 1000)
 GROUP BY host_id,pool_id,image HAVING COUNT(*)>=3`, ms(since))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []StartupEstimate
	for rows.Next() {
		var v StartupEstimate
		if err := rows.Scan(&v.HostID, &v.PoolID, &v.Image, &v.Samples, &v.Milliseconds); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

// One snapshot query replaces one read per pool, retaining failed rows because
// backoff and cleanup still need them. The state filter is intentionally intact.
func (s *Store) SchedulingRunners(ctx context.Context) (map[string][]*Runner, error) {
	rows, err := s.read.QueryContext(ctx, `SELECT `+runnerCols+` FROM runners WHERE state!='removed' ORDER BY created_at`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string][]*Runner{}
	for rows.Next() {
		v, err := scanRunner(rows)
		if err != nil {
			return nil, err
		}
		out[v.PoolID] = append(out[v.PoolID], v)
	}
	return out, rows.Err()
}
