package store

// The queries behind the failure taxonomy: who has been failing, how often, and
// in which category. They are here rather than beside the jobs or the runners
// because they are one subject read from both tables, and an operator asking
// "is it us or is it them" is asking one question.

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

// JobFaultCountsSince counts the fleet's own failures in a window, by category.
//
// It answers the question the taxonomy was added for and the one a single
// job's page cannot: not "why did this job fail" but "what is this fleet doing
// wrong, and how often". Rows written before the category existed count as
// FaultRunnerExited, which is what the migration gave them.
//
// Kinds with no jobs are absent rather than zero. A caller that wants the full
// set iterates FaultKinds; one that wants "what is actually happening" reads
// what is here, and a map of nine zeroes reads as nine problems at a glance.
func (s *Store) JobFaultCountsSince(ctx context.Context, since time.Time, managedOnly bool) (map[FaultKind]int, error) {
	scope := ""
	if managedOnly {
		scope = " AND " + managedJobSQL("jobs")
	}
	rows, err := s.read.QueryContext(ctx, `SELECT fault_kind, COUNT(*) FROM jobs
		WHERE `+fleetFailedJobSQL()+` AND COALESCE(completed_at, queued_at) >= ?`+scope+`
		GROUP BY fault_kind`, ms(since))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[FaultKind]int{}
	for rows.Next() {
		var kind FaultKind
		var n int
		if err := rows.Scan(&kind, &n); err != nil {
			return nil, err
		}
		// A job whose runner died before GitHub closed it has no completion
		// stamp, and it is the case an operator most wants to see, so the
		// window falls back to when it was queued rather than dropping it.
		out[kind.Normalise()] += n
	}
	return out, rows.Err()
}

// RunnerFaultCountsSince counts the runners that failed in a window, by
// category.
//
// This is the half no job ever sees. A runner that fails before it registers
// never reaches a job at all: the job stays queued, waits for the next runner,
// and waits again, so a pool whose containers will not start reads from the
// Jobs page as a pool that is merely slow. The count is per attempt, not per
// runner name -- twenty attempts in an hour is the shape of the problem.
func (s *Store) RunnerFaultCountsSince(ctx context.Context, since time.Time) (map[FaultKind]int, error) {
	rows, err := s.read.QueryContext(ctx, `SELECT fault_kind, COUNT(*) FROM runners
		WHERE state = ? AND COALESCE(finished_at, created_at) >= ?
		GROUP BY fault_kind`, string(RunnerFailed), ms(since))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[FaultKind]int{}
	for rows.Next() {
		var kind FaultKind
		var n int
		if err := rows.Scan(&kind, &n); err != nil {
			return nil, err
		}
		out[kind.Normalise()] += n
	}
	return out, rows.Err()
}

// FailedRunnersForPoolSince lists a pool's recent failed runners, newest
// first, capped at limit.
//
// It exists for the question "why is my job still queued?" on a fleet that is
// trying and failing to answer it: the pool has demand, the scheduler keeps
// placing runners, and every one of them dies before it registers. Nothing the
// job itself carries can say that.
//
// The id breaks a tie on the stamp. Two runners failing inside one millisecond
// is ordinary on a pool that cannot start a container at all, and without it
// the "most recent failure" whose message the problems drawer and the job's
// explanation both quote would be whichever row SQLite happened to reach
// first -- a sentence that changes between two identical reads.
func (s *Store) FailedRunnersForPoolSince(ctx context.Context, poolID string, since time.Time, limit int) ([]*Runner, error) {
	if limit <= 0 {
		limit = 5
	}
	rows, err := s.read.QueryContext(ctx, `SELECT `+runnerCols+` FROM runners
		WHERE pool_id = ? AND state = ? AND COALESCE(finished_at, created_at) >= ?
		ORDER BY COALESCE(finished_at, created_at) DESC, id DESC LIMIT ?`,
		poolID, string(RunnerFailed), ms(since), limit)
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

// LastJobEventKind returns the kind of a job's most recent timeline entry, or
// empty when it has none.
//
// It is the guard on a repeating entry. A pool whose runners will not start
// tries again every pass, and appending one entry per attempt to every job
// waiting on that pool turns a timeline into a log file -- the thing an
// operator opens the timeline to avoid. One entry per job per run of failures
// says the same thing and stays readable.
func (s *Store) LastJobEventKind(ctx context.Context, jobID string) (JobEventKind, error) {
	var kind JobEventKind
	err := s.read.QueryRowContext(ctx,
		`SELECT kind FROM job_events WHERE job_id = ? ORDER BY at DESC, id DESC LIMIT 1`, jobID).Scan(&kind)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return kind, err
}
