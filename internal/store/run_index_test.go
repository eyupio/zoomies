package store

import (
	"strings"
	"testing"
)

// explainPlan is the planner's account of how SQLite will run query, one
// detail line per step.
func explainPlan(t *testing.T, s *Store, query string, args ...any) []string {
	t.Helper()
	rows, err := s.read.QueryContext(t.Context(), "EXPLAIN QUERY PLAN "+query, args...)
	if err != nil {
		t.Fatalf("EXPLAIN QUERY PLAN: %v", err)
	}
	defer rows.Close()
	var plan []string
	for rows.Next() {
		var id, parent, notused int
		var detail string
		if err := rows.Scan(&id, &parent, &notused, &detail); err != nil {
			t.Fatalf("scanning the plan: %v", err)
		}
		plan = append(plan, detail)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("reading the plan: %v", err)
	}
	return plan
}

// 0053's backfill looks up a sibling job of the same run for every job still
// without a run number. Without an index on the run each lookup scanned the
// repository's jobs, so the migration grew with the square of the jobs table
// and kept a busy controller from serving for many minutes after an upgrade.
// It has to seek, and so do the lookups every job event makes at runtime.
func TestARunsJobsAreFoundThroughAnIndex(t *testing.T) {
	s := newTestStore(t)
	migs, err := loadMigrations()
	if err != nil {
		t.Fatal(err)
	}
	backfill := ""
	for _, m := range migs {
		if m.name == "0053_job_run_number_backfill.sql" {
			backfill = m.sql[strings.Index(m.sql, "UPDATE jobs"):]
		}
	}
	if backfill == "" {
		t.Fatal("0053's backfill is not in the embedded migrations")
	}
	for _, tc := range []struct {
		name  string
		query string
		args  []any
		// seeks is how many steps must go through idx_jobs_run: the
		// backfill's two sibling lookups, one for each runtime query. The
		// backfill's own pass over the jobs table is a scan and should be --
		// it is linear; the lookups inside it are what must not be.
		seeks int
	}{
		{"the migration's backfill", backfill, nil, 2},
		{"RunNumberForRun", `SELECT run_number FROM jobs WHERE github_run_id = ? AND run_number != 0 LIMIT 1`, []any{int64(7)}, 1},
		{"SetRunNumberForRun", `UPDATE jobs SET run_number=? WHERE repo=? AND github_run_id=? AND run_number=0`, []any{int64(1), "o/r", int64(7)}, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			plan := explainPlan(t, s, tc.query, tc.args...)
			seeks := 0
			for _, step := range plan {
				if strings.Contains(step, "idx_jobs_run") {
					seeks++
				}
			}
			if seeks != tc.seeks {
				t.Fatalf("plan = %q, want %d step(s) through idx_jobs_run", plan, tc.seeks)
			}
		})
	}
}
