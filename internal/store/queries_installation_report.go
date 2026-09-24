package store

import (
	"context"
	"database/sql"
	"errors"
	"math"
	"sort"
	"time"
)

// InstallationCounts are the per-installation report's counts. Each one is
// defined in docs/metrics.md ("Per-installation report"); the definitions
// here are the SQL's, and the two must agree.
type InstallationCounts struct {
	// Observed is every job first seen for the installation in the window.
	Observed int `json:"observed"`
	// Eligible is those of them the fleet could ever act on: a pool claimed
	// the labels and GitHub was not holding the job for a review.
	Eligible int `json:"eligible"`
	// CreatedFor is eligible jobs that ran on a runner whose create task was
	// issued after the job became eligible -- a runner started because of the
	// demand rather than one already waiting.
	CreatedFor int `json:"created_for"`
	// RanHere is jobs that ran on any runner this fleet created.
	RanHere int `json:"ran_here"`
	// RanElsewhere is jobs GitHub gave to a runner this fleet did not create.
	RanElsewhere int `json:"ran_elsewhere"`
	// FleetFault is jobs carrying a fault_kind: the fleet, not the workflow,
	// is why they went wrong.
	FleetFault int `json:"fleet_fault"`
	// CleanupPending and CleanupConverged split the runners that finished in
	// the window by whether both the host and GitHub have confirmed they are
	// gone.
	CleanupPending   int `json:"cleanup_pending"`
	CleanupConverged int `json:"cleanup_converged"`
}

func (c *InstallationCounts) add(o InstallationCounts) {
	c.Observed += o.Observed
	c.Eligible += o.Eligible
	c.CreatedFor += o.CreatedFor
	c.RanHere += o.RanHere
	c.RanElsewhere += o.RanElsewhere
	c.FleetFault += o.FleetFault
	c.CleanupPending += o.CleanupPending
	c.CleanupConverged += o.CleanupConverged
}

// Percentiles summarises one interval's exact samples. P50 and P95 are nil
// when there are no samples, because a zero would read as "instant".
type Percentiles struct {
	Samples int      `json:"samples"`
	P50     *float64 `json:"p50_seconds"`
	P95     *float64 `json:"p95_seconds"`
}

// NearestRank is the percentile method the report uses: the smallest sample
// such that at least p per cent of the samples are at or below it, which is
// the sample at 1-based rank ceil(p/100 * n) of the sorted set.
//
// It is chosen because it always answers with a value that was observed. An
// interpolating method reports a p95 of 9.7 s for a fleet where no job ever
// waited 9.7 s, and an operator asked "which job was that?" has no answer.
func NearestRank(sorted []time.Duration, p float64) time.Duration {
	n := len(sorted)
	rank := int(math.Ceil(p / 100 * float64(n)))
	if rank < 1 {
		rank = 1
	}
	if rank > n {
		rank = n
	}
	return sorted[rank-1]
}

func summarise(samples []time.Duration) Percentiles {
	out := Percentiles{Samples: len(samples)}
	if len(samples) == 0 {
		return out
	}
	sort.Slice(samples, func(i, j int) bool { return samples[i] < samples[j] })
	p50, p95 := NearestRank(samples, 50).Seconds(), NearestRank(samples, 95).Seconds()
	out.P50, out.P95 = &p50, &p95
	return out
}

// InstallationTimings are the report's three intervals, each measured exactly
// from stored timestamps.
type InstallationTimings struct {
	// Scheduling is a job's eligible_at to the first create task of the
	// runner that ran it, for the jobs counted in CreatedFor.
	Scheduling Percentiles `json:"scheduling"`
	// Registration is a runner's first create task to its registration.
	Registration Percentiles `json:"registration"`
	// Cleanup is a runner's finish to its confirmed cleanup.
	Cleanup Percentiles `json:"cleanup"`
}

// InstallationReport is one installation's report over [From, To).
type InstallationReport struct {
	InstallationID string              `json:"installation_id"`
	From           time.Time           `json:"from"`
	To             time.Time           `json:"to"`
	Counts         InstallationCounts  `json:"counts"`
	Timings        InstallationTimings `json:"timings"`
	// CountsFrom and TimingsFrom are where each half's record begins inside
	// the window. Equal to From, the half covers the whole window; later, the
	// part before it is unavailable -- pruned before anything kept it -- and
	// the figures are for the rest, never an estimate of the whole.
	CountsFrom  time.Time `json:"counts_from"`
	TimingsFrom time.Time `json:"timings_from"`
	// RolledUntil is where the daily roll-up hands over to the rows, when any
	// of the window was answered from it.
	RolledUntil *time.Time `json:"rolled_until,omitempty"`
}

type rowQuerier interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

type instDayKey struct {
	day  int64
	inst string
}

// countRows computes the counts in [lo, hi) from the rows, per UTC day and
// installation. Jobs are placed on the day they were first observed and
// runners on the day they finished, so a day's figures never move once every
// job in it has completed and every runner in it has a session.
func countRows(ctx context.Context, q rowQuerier, lo, hi int64) (map[instDayKey]*InstallationCounts, error) {
	out := map[instDayKey]*InstallationCounts{}
	at := func(inst string, when int64) *InstallationCounts {
		k := instDayKey{utcDay(when), inst}
		c := out[k]
		if c == nil {
			c = &InstallationCounts{}
			out[k] = c
		}
		return c
	}
	// A job's runner is ours when runner_id is set: ingest only links a job
	// to a runner row this fleet created. A job GitHub gave to anybody else's
	// runner keeps the name GitHub reported and no id.
	rows, err := q.QueryContext(ctx, `SELECT j.installation_id, j.queued_at,
			j.eligible_at IS NOT NULL,
			COALESCE(j.eligible_at IS NOT NULL AND j.runner_id != ''
				AND COALESCE(r.create_task_issued_at, rs.create_task_issued_at) >= j.eligible_at, 0),
			j.runner_id != '',
			j.runner_id = '' AND j.runner_name != '',
			j.fault_kind != ''
		FROM jobs j
		LEFT JOIN runners r ON r.id = j.runner_id
		LEFT JOIN runner_sessions rs ON rs.runner_id = j.runner_id
		WHERE j.installation_id != '' AND j.queued_at >= ? AND j.queued_at < ?`, lo, hi)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var inst string
		var queued int64
		var eligible, created, here, elsewhere, fault bool
		if err := rows.Scan(&inst, &queued, &eligible, &created, &here, &elsewhere, &fault); err != nil {
			rows.Close()
			return nil, err
		}
		c := at(inst, queued)
		c.Observed++
		c.Eligible += b2i(eligible)
		c.CreatedFor += b2i(created)
		c.RanHere += b2i(here)
		c.RanElsewhere += b2i(elsewhere)
		c.FleetFault += b2i(fault)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}

	// A runner with a session is counted from the session, which is what
	// outlives it; one without is still in the table, finished and not yet
	// confirmed clean.
	rows, err = q.QueryContext(ctx, `SELECT rs.installation_id, rs.finished_at, rs.cleaned_up_at IS NOT NULL
		FROM runner_sessions rs WHERE rs.finished_at >= ?1 AND rs.finished_at < ?2
		UNION ALL
		SELECT COALESCE(p.installation_id, ''), COALESCE(r.finished_at, r.host_removed_at, r.created_at), r.cleaned_up_at IS NOT NULL
		FROM runners r LEFT JOIN pools p ON p.id = r.pool_id
		WHERE r.state IN ('removed', 'failed')
		AND COALESCE(r.finished_at, r.host_removed_at, r.created_at) >= ?1
		AND COALESCE(r.finished_at, r.host_removed_at, r.created_at) < ?2
		AND NOT EXISTS (SELECT 1 FROM runner_sessions rs WHERE rs.runner_id = r.id)`, lo, hi)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var inst string
		var finished int64
		var converged bool
		if err := rows.Scan(&inst, &finished, &converged); err != nil {
			return nil, err
		}
		if inst == "" {
			continue
		}
		c := at(inst, finished)
		if converged {
			c.CleanupConverged++
		} else {
			c.CleanupPending++
		}
	}
	return out, rows.Err()
}

func b2i(b bool) int {
	if b {
		return 1
	}
	return 0
}

// RollUpInstallations absorbs every whole UTC day before until that can no
// longer change into installation_daily, and returns how many days it rolled.
//
// A day is held open by a job observed on it that has not completed, and by a
// runner created on it that has no session yet. A job that will never
// complete -- GitHub stopped telling us about it -- must not hold the roll-up
// for ever, so one observed before settledBefore does not hold: it is about to
// be pruned, and is counted as what it was. The caller passes the jobs prune
// cutoff; the zero time means jobs are never pruned, and then nothing is lost
// by waiting.
func (s *Store) RollUpInstallations(ctx context.Context, until, settledBefore time.Time) (int, error) {
	days := 0
	err := s.tx(ctx, func(tx *sql.Tx) error {
		target := utcDay(until.UnixMilli())
		settled := int64(math.MinInt64)
		if !settledBefore.IsZero() {
			settled = ms(settledBefore)
		}
		var held sql.NullInt64
		if err := tx.QueryRowContext(ctx, `SELECT MIN(at) FROM (
			SELECT MIN(queued_at) AS at FROM jobs WHERE state != 'completed' AND queued_at >= ?
			UNION ALL SELECT MIN(created_at) FROM runners r
				WHERE NOT EXISTS (SELECT 1 FROM runner_sessions rs WHERE rs.runner_id = r.id))`, settled).Scan(&held); err != nil {
			return err
		}
		if held.Valid {
			target = min64(target, utcDay(held.Int64))
		}
		var from, rolled int64
		err := tx.QueryRowContext(ctx, `SELECT rolled_from, rolled_until FROM installation_rollup WHERE id = 1`).Scan(&from, &rolled)
		if errors.Is(err, sql.ErrNoRows) {
			first, err := earliestRow(ctx, tx)
			if err != nil || first == nil {
				return err
			}
			from, rolled = *first, utcDay(*first)
		} else if err != nil {
			return err
		}
		if target <= rolled {
			return nil
		}
		counts, err := countRows(ctx, tx, rolled, target)
		if err != nil {
			return err
		}
		for k, c := range counts {
			if _, err := tx.ExecContext(ctx, `INSERT INTO installation_daily (day, installation_id, observed, eligible,
				created_for, ran_here, ran_elsewhere, fleet_fault, cleanup_pending, cleanup_converged)
				VALUES (?,?,?,?,?,?,?,?,?,?)`, k.day, k.inst, c.Observed, c.Eligible, c.CreatedFor, c.RanHere,
				c.RanElsewhere, c.FleetFault, c.CleanupPending, c.CleanupConverged); err != nil {
				return err
			}
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO installation_rollup (id, rolled_from, rolled_until) VALUES (1, ?, ?)
			ON CONFLICT(id) DO UPDATE SET rolled_until = excluded.rolled_until`, from, target); err != nil {
			return err
		}
		days = int((target - rolled) / dayMS)
		return nil
	})
	return days, err
}

// earliestRow is the oldest instant any row the counts are read from still
// records, or nil on a database with none.
func earliestRow(ctx context.Context, q rowQuerier) (*int64, error) {
	var first sql.NullInt64
	if err := q.QueryRowContext(ctx, `SELECT MIN(at) FROM (
		SELECT MIN(queued_at) AS at FROM jobs WHERE installation_id != ''
		UNION ALL SELECT MIN(finished_at) FROM runner_sessions
		UNION ALL SELECT MIN(created_at) FROM runners)`).Scan(&first); err != nil || !first.Valid {
		return nil, err
	}
	return &first.Int64, nil
}

// InstallationRollupUntil is the end of what the installation roll-up has
// absorbed, and the zero time before anything has been. The prune loop holds
// the jobs prune and the sessions prune behind it, so no row goes before the
// day it belongs to has been counted.
func (s *Store) InstallationRollupUntil(ctx context.Context) (time.Time, error) {
	var until int64
	err := s.read.QueryRowContext(ctx, `SELECT rolled_until FROM installation_rollup WHERE id = 1`).Scan(&until)
	if errors.Is(err, sql.ErrNoRows) {
		return time.Time{}, nil
	}
	if err != nil {
		return time.Time{}, err
	}
	return time.UnixMilli(until).UTC(), nil
}

// InstallationCountsBetween returns every installation's counts over the UTC
// days [from, to) touch -- from is moved back to its midnight -- and the
// instant the counts are complete from.
//
// Days the roll-up has absorbed are read from it and every later moment from
// the rows. Before the roll-up's first instant (or, before any roll-up, the
// oldest row) nothing is left to count from, and the returned instant says so
// rather than the counts pretending the window was quiet.
func (s *Store) InstallationCountsBetween(ctx context.Context, from, to time.Time) (map[string]InstallationCounts, time.Time, *time.Time, error) {
	lo, hi := utcDay(ms(from)), ms(to)
	var rolledFrom, rolledUntil int64
	err := s.read.QueryRowContext(ctx, `SELECT rolled_from, rolled_until FROM installation_rollup WHERE id = 1`).Scan(&rolledFrom, &rolledUntil)
	hasRollup := err == nil
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return nil, time.Time{}, nil, err
	}
	out := map[string]InstallationCounts{}
	var until *time.Time
	rowsFrom := lo
	if hasRollup && rolledUntil > lo {
		rows, err := s.read.QueryContext(ctx, `SELECT installation_id, observed, eligible, created_for, ran_here,
			ran_elsewhere, fleet_fault, cleanup_pending, cleanup_converged
			FROM installation_daily WHERE day >= ? AND day < ?`, lo, min64(hi, rolledUntil))
		if err != nil {
			return nil, time.Time{}, nil, err
		}
		for rows.Next() {
			var inst string
			var c InstallationCounts
			if err := rows.Scan(&inst, &c.Observed, &c.Eligible, &c.CreatedFor, &c.RanHere, &c.RanElsewhere,
				&c.FleetFault, &c.CleanupPending, &c.CleanupConverged); err != nil {
				rows.Close()
				return nil, time.Time{}, nil, err
			}
			acc := out[inst]
			acc.add(c)
			out[inst] = acc
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return nil, time.Time{}, nil, err
		}
		rowsFrom = rolledUntil
		u := time.UnixMilli(min64(hi, rolledUntil)).UTC()
		until = &u
	}
	if rowsFrom < hi {
		days, err := countRows(ctx, s.read, rowsFrom, hi)
		if err != nil {
			return nil, time.Time{}, nil, err
		}
		for k, c := range days {
			acc := out[k.inst]
			acc.add(*c)
			out[k.inst] = acc
		}
	}
	complete := lo
	if hasRollup {
		complete = max64(lo, rolledFrom)
	} else if first, err := earliestRow(ctx, s.read); err != nil {
		return nil, time.Time{}, nil, err
	} else if first != nil {
		complete = max64(lo, *first)
	}
	return out, time.UnixMilli(min64(complete, hi)).UTC(), until, nil
}

// InstallationReport answers one installation's report over [from, to), with
// from moved back to its UTC midnight so every day the roll-up serves is
// whole.
func (s *Store) InstallationReport(ctx context.Context, installationID string, from, to time.Time) (*InstallationReport, error) {
	all, countsFrom, until, err := s.InstallationCountsBetween(ctx, from, to)
	if err != nil {
		return nil, err
	}
	lo, hi := utcDay(ms(from)), ms(to)
	rep := &InstallationReport{
		InstallationID: installationID,
		From:           time.UnixMilli(lo).UTC(),
		To:             time.UnixMilli(hi).UTC(),
		Counts:         all[installationID],
		CountsFrom:     countsFrom,
		RolledUntil:    until,
	}
	if rep.Timings, rep.TimingsFrom, err = s.installationTimings(ctx, installationID, lo, hi); err != nil {
		return nil, err
	}
	return rep, nil
}

// installationTimings collects each interval's exact samples. A sample is in
// the window when the interval starts in it. Each is read from the rows where
// they are still here and from the sessions where they are not, and a pair
// with either end missing, or ending before it began, is left out rather than
// counted as zero.
func (s *Store) installationTimings(ctx context.Context, inst string, lo, hi int64) (InstallationTimings, time.Time, error) {
	var t InstallationTimings
	collect := func(query string, args ...any) ([]time.Duration, error) {
		rows, err := s.read.QueryContext(ctx, query, args...)
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		var out []time.Duration
		for rows.Next() {
			var start, end int64
			if err := rows.Scan(&start, &end); err != nil {
				return nil, err
			}
			if end >= start {
				out = append(out, time.Duration(end-start)*time.Millisecond)
			}
		}
		return out, rows.Err()
	}

	sched, err := collect(`SELECT j.eligible_at, COALESCE(r.create_task_issued_at, rs.create_task_issued_at) AS issued
		FROM jobs j LEFT JOIN runners r ON r.id = j.runner_id LEFT JOIN runner_sessions rs ON rs.runner_id = j.runner_id
		WHERE j.installation_id = ?1 AND j.runner_id != '' AND j.eligible_at >= ?2 AND j.eligible_at < ?3 AND issued IS NOT NULL
		UNION ALL
		SELECT rs.job_eligible_at, rs.create_task_issued_at FROM runner_sessions rs
		WHERE rs.installation_id = ?1 AND rs.job_id != '' AND rs.job_eligible_at >= ?2 AND rs.job_eligible_at < ?3
		AND rs.create_task_issued_at IS NOT NULL
		AND NOT EXISTS (SELECT 1 FROM jobs j WHERE j.id = rs.job_id)`, inst, lo, hi)
	if err != nil {
		return t, time.Time{}, err
	}
	reg, err := collect(`SELECT rs.create_task_issued_at, rs.registered_at FROM runner_sessions rs
		WHERE rs.installation_id = ?1 AND rs.create_task_issued_at >= ?2 AND rs.create_task_issued_at < ?3
		AND rs.registered_at IS NOT NULL
		UNION ALL
		SELECT r.create_task_issued_at, r.registered_at FROM runners r JOIN pools p ON p.id = r.pool_id
		WHERE p.installation_id = ?1 AND r.create_task_issued_at >= ?2 AND r.create_task_issued_at < ?3
		AND r.registered_at IS NOT NULL
		AND NOT EXISTS (SELECT 1 FROM runner_sessions rs WHERE rs.runner_id = r.id)`, inst, lo, hi)
	if err != nil {
		return t, time.Time{}, err
	}
	// A runner still in the table without a session has not had its cleanup
	// confirmed, so every cleanup sample is in the sessions.
	clean, err := collect(`SELECT finished_at, cleaned_up_at FROM runner_sessions
		WHERE installation_id = ?1 AND finished_at >= ?2 AND finished_at < ?3 AND cleaned_up_at IS NOT NULL`, inst, lo, hi)
	if err != nil {
		return t, time.Time{}, err
	}
	t.Scheduling, t.Registration, t.Cleanup = summarise(sched), summarise(reg), summarise(clean)

	// The timings reach back as far as the oldest session or runner row; the
	// roll-up keeps no timestamps, by design. With none left at all, whatever
	// the roll-up has counted is exactly what the timings can no longer see.
	var first, counted sql.NullInt64
	if err := s.read.QueryRowContext(ctx, `SELECT
		(SELECT MIN(at) FROM (SELECT MIN(started_at) AS at FROM runner_sessions UNION ALL SELECT MIN(created_at) FROM runners)),
		(SELECT rolled_until FROM installation_rollup WHERE id = 1)`).Scan(&first, &counted); err != nil {
		return t, time.Time{}, err
	}
	from := lo
	if first.Valid {
		from = max64(lo, first.Int64)
	} else if counted.Valid {
		from = max64(lo, counted.Int64)
	}
	return t, time.UnixMilli(min64(from, hi)).UTC(), nil
}
