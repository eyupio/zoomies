package store

import "context"

// PendingHostCleanup recovers removal intent from completed jobs and failed
// host removals. A keyset cursor prevents offline hosts starving later rows.
// Natural exits with an explicit log-retention window stay with the agent.
func (s *Store) PendingHostCleanup(ctx context.Context, afterID string, limit int) ([]*Runner, error) {
	rows, err := s.read.QueryContext(ctx, `SELECT `+runnerCols+` FROM runners
		WHERE id > ? AND state IN ('removed', 'failed') AND host_removed_at IS NULL
		AND (host_cleanup_error != '' OR EXISTS
			(SELECT 1 FROM jobs WHERE jobs.runner_id=runners.id AND jobs.state='completed'))
		AND NOT EXISTS (SELECT 1 FROM jobs WHERE jobs.runner_id=runners.id AND jobs.state='in_progress')
		ORDER BY id LIMIT ?`, afterID, min(max(limit, 1), 200))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*Runner
	for rows.Next() {
		r, err := scanRunner(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}
