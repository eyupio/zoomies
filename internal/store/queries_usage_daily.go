package store

import (
	"context"
	"database/sql"
	"errors"
	"math"
	"sort"
	"time"
)

const dayMS = int64(24 * time.Hour / time.Millisecond)

// utcDay is the UTC midnight at or before an instant in milliseconds.
func utcDay(at int64) int64 { return at - ((at%dayMS)+dayMS)%dayMS }

// UsageDay is one row of the daily roll-up: a UTC day's runner allocation for
// one pool on one host under one installation.
type UsageDay struct {
	Day              time.Time `json:"day"`
	PoolID           string    `json:"pool_id"`
	HostID           string    `json:"host_id"`
	InstallationID   string    `json:"installation_id"`
	AllocatedSeconds int64     `json:"allocated_seconds"`
	// CostMinor is in hundredths of whatever currency the pool's rate is in,
	// and nil when nothing that day had a rate.
	CostMinor *int64 `json:"cost_minor,omitempty"`
}

type usageDayKey struct {
	day                  int64
	pool, host, instance string
}

type usageDayAcc struct {
	ms     int64
	cost   float64 // rate * milliseconds, summed, rounded once at the end
	priced bool
}

// RollUpSessions is the roll-up's arithmetic, apart from the database: the
// days in [from, until) cut from the given sessions. It is exported so the
// claim that the roll-up is reproducible from the sessions alone is one a
// test, or an operator's script, can check.
//
// Allocation is summed in milliseconds and cost as rate times milliseconds,
// each rounded once per row, so a row is the nearest whole second and cent to
// the exact figure rather than the sum of per-session roundings.
func RollUpSessions(sessions []*RunnerSession, from, until time.Time) []UsageDay {
	lo, hi := utcDay(from.UnixMilli()), utcDay(until.UnixMilli())
	acc := map[usageDayKey]*usageDayAcc{}
	for _, x := range sessions {
		start, end := max64(x.StartedAt.UnixMilli(), lo), min64(x.FinishedAt.UnixMilli(), hi)
		for at := start; at < end; {
			day := utcDay(at)
			next := min64(end, day+dayMS)
			k := usageDayKey{day, x.PoolID, x.HostID, x.InstallationID}
			a := acc[k]
			if a == nil {
				a = &usageDayAcc{}
				acc[k] = a
			}
			a.ms += next - at
			if x.CostPerRunnerHour != nil {
				a.priced = true
				a.cost += float64(next-at) * *x.CostPerRunnerHour
			}
			at = next
		}
	}
	out := make([]UsageDay, 0, len(acc))
	for k, a := range acc {
		d := UsageDay{Day: time.UnixMilli(k.day).UTC(), PoolID: k.pool, HostID: k.host, InstallationID: k.instance,
			AllocatedSeconds: a.ms / 1000}
		if a.priced {
			c := int64(math.Round(a.cost / float64(time.Hour/time.Millisecond) * 100))
			d.CostMinor = &c
		}
		out = append(out, d)
	}
	sort.Slice(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if !a.Day.Equal(b.Day) {
			return a.Day.Before(b.Day)
		}
		if a.PoolID != b.PoolID {
			return a.PoolID < b.PoolID
		}
		if a.HostID != b.HostID {
			return a.HostID < b.HostID
		}
		return a.InstallationID < b.InstallationID
	})
	return out
}

// RollUpUsage absorbs every whole UTC day before until that can no longer
// change into usage_daily, and returns how many days it rolled up.
//
// A day can no longer change once every runner alive in it has a session. A
// runner row without one -- still running, or removed and waiting for its
// cleanup to be confirmed -- holds the watermark at the start of the day it
// was created, because its session will arrive later and must not find its
// days already closed. The prune writes a session for any runner it deletes,
// so the hold lasts no longer than retention.runners.
//
// The rows, the sessions and the watermark are read and written in one
// transaction, so a controller that stops half way has rolled up nothing, and
// one that runs twice rolls up a day once.
func (s *Store) RollUpUsage(ctx context.Context, until time.Time) (int, error) {
	days := 0
	err := s.tx(ctx, func(tx *sql.Tx) error {
		target := utcDay(until.UnixMilli())
		var held sql.NullInt64
		if err := tx.QueryRowContext(ctx, `SELECT MIN(created_at) FROM runners r
			WHERE NOT EXISTS (SELECT 1 FROM runner_sessions rs WHERE rs.runner_id = r.id)`).Scan(&held); err != nil {
			return err
		}
		if held.Valid {
			target = min64(target, utcDay(held.Int64))
		}
		var from, rolled int64
		err := tx.QueryRowContext(ctx, `SELECT rolled_from, rolled_until FROM usage_rollup WHERE id = 1`).Scan(&from, &rolled)
		if errors.Is(err, sql.ErrNoRows) {
			var first sql.NullInt64
			if err := tx.QueryRowContext(ctx, `SELECT MIN(started_at) FROM runner_sessions`).Scan(&first); err != nil {
				return err
			}
			if !first.Valid {
				return nil
			}
			from, rolled = utcDay(first.Int64), utcDay(first.Int64)
		} else if err != nil {
			return err
		}
		if target <= rolled {
			return nil
		}
		sessions, err := querySessions(ctx, tx, rolled, target)
		if err != nil {
			return err
		}
		for _, d := range RollUpSessions(sessions, time.UnixMilli(rolled), time.UnixMilli(target)) {
			var cost any
			if d.CostMinor != nil {
				cost = *d.CostMinor
			}
			if _, err := tx.ExecContext(ctx, `INSERT INTO usage_daily (day, pool_id, host_id, installation_id, allocated_seconds, cost_minor)
				VALUES (?,?,?,?,?,?)`, d.Day.UnixMilli(), d.PoolID, d.HostID, d.InstallationID, d.AllocatedSeconds, cost); err != nil {
				return err
			}
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO usage_rollup (id, rolled_from, rolled_until) VALUES (1, ?, ?)
			ON CONFLICT(id) DO UPDATE SET rolled_until = excluded.rolled_until`, from, target); err != nil {
			return err
		}
		days = int((target - rolled) / dayMS)
		return nil
	})
	return days, err
}

// UsageRollupRange is the span the roll-up has absorbed, [from, until). ok is
// false before anything has been rolled up.
func (s *Store) UsageRollupRange(ctx context.Context) (from, until time.Time, ok bool, err error) {
	var f, u int64
	err = s.read.QueryRowContext(ctx, `SELECT rolled_from, rolled_until FROM usage_rollup WHERE id = 1`).Scan(&f, &u)
	if errors.Is(err, sql.ErrNoRows) {
		return time.Time{}, time.Time{}, false, nil
	}
	if err != nil {
		return time.Time{}, time.Time{}, false, err
	}
	return time.UnixMilli(f).UTC(), time.UnixMilli(u).UTC(), true, nil
}

// UsageDays returns the roll-up's rows for the days in [from, to).
func (s *Store) UsageDays(ctx context.Context, from, to time.Time) ([]UsageDay, error) {
	rows, err := s.read.QueryContext(ctx, `SELECT day, pool_id, host_id, installation_id, allocated_seconds, cost_minor
		FROM usage_daily WHERE day >= ? AND day < ? ORDER BY day, pool_id, host_id, installation_id`, ms(from), ms(to))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []UsageDay
	for rows.Next() {
		var d UsageDay
		var day int64
		var cost sql.NullInt64
		if err := rows.Scan(&day, &d.PoolID, &d.HostID, &d.InstallationID, &d.AllocatedSeconds, &cost); err != nil {
			return nil, err
		}
		d.Day = time.UnixMilli(day).UTC()
		if cost.Valid {
			c := cost.Int64
			d.CostMinor = &c
		}
		out = append(out, d)
	}
	return out, rows.Err()
}
