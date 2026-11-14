package store

import (
	"context"
	"regexp"
	"strings"
	"testing"
)

// explain returns the query plan for a statement, joined into one string.
func explain(t *testing.T, s *Store, query string, args ...any) string {
	t.Helper()
	rows, err := s.read.QueryContext(context.Background(), "EXPLAIN QUERY PLAN "+query, args...)
	if err != nil {
		t.Fatalf("EXPLAIN QUERY PLAN: %v", err)
	}
	defer rows.Close()
	var plan []string
	for rows.Next() {
		var id, parent, unused int
		var detail string
		if err := rows.Scan(&id, &parent, &unused, &detail); err != nil {
			t.Fatal(err)
		}
		plan = append(plan, detail)
	}
	return strings.Join(plan, "; ")
}

// The problems list asks for the fleet's own failures on every pass, twice. A
// partial index only serves a query whose WHERE it can prove implies its own,
// so rewording fleetFailedJobSQL -- adding a column to the fault test, say --
// would quietly send both reads back to scanning every job. This is the test
// that notices, rather than an operator with a slow Overview.
func TestFaultedJobsAreReadFromTheirOwnIndex(t *testing.T) {
	s := newTestStore(t)
	where, args := jobWhere(JobFilter{FaultedOnly: true}, s.Now())
	for name, query := range map[string]string{
		"the count": `SELECT COUNT(*) FROM jobs ` + where,
		"the page":  `SELECT id FROM jobs ` + where + ` ORDER BY queued_at DESC, id ASC LIMIT 100`,
	} {
		plan := explain(t, s, query, args...)
		if !strings.Contains(plan, "idx_jobs_faulted") {
			t.Errorf("%s is not read from idx_jobs_faulted: %s", name, plan)
		}
		if strings.Contains(plan, "TEMP B-TREE") {
			t.Errorf("%s sorts in a temporary b-tree: %s", name, plan)
		}
	}
}

// The attention view asks, for each recent failure, whether a newer job has
// taken its place, and there are four ways one can: a later attempt of the same
// run, which idx_jobs_run finds, a later run of the same workflow on the same
// branch, which idx_jobs_attention_branch finds, and a run of another commit
// on the branch or a later attempt of the same Dependabot update, which both
// read the runs that came after it on the branch from idx_jobs_branch_run.
// Joined by an OR inside one NOT EXISTS, SQLite could use none of them and
// walked every retained job with the same name in every repository
// (idx_jobs_name_queued): seconds on a month of history for the page, the same
// for the loop that picks runs to check, and every other read queued behind
// them. This is the test that notices, rather
// than an Overview that stops loading.
func TestTheAttentionFilterIsReadFromTheRunAndBranchIndexes(t *testing.T) {
	s := newTestStore(t)
	where, args := jobWhere(JobFilter{AttentionOnly: true}, s.Now())
	for name, q := range map[string]struct {
		query string
		args  []any
	}{
		"the page":                {`SELECT id FROM jobs ` + where + ` ORDER BY queued_at DESC, id ASC LIMIT 50`, args},
		"the count":               {`SELECT COUNT(*) FROM jobs ` + where, args},
		"the runs to check again": {jobAttentionRunsToCheckSQL(), []any{0, 0, 0, 5}},
	} {
		plan := explain(t, s, q.query, q.args...)
		// The replacement is the subquery's alias, so the outer scan of the
		// runs to check (which is also over idx_jobs_run) cannot satisfy it.
		for _, index := range []string{"idx_jobs_run", "idx_jobs_attention_branch", "idx_jobs_branch_run"} {
			if !regexp.MustCompile(`SEARCH later USING (COVERING )?INDEX ` + index + ` \(`).MatchString(plan) {
				t.Errorf("%s does not look for a replacement through %s: %s", name, index, plan)
			}
		}
		if strings.Contains(plan, "idx_jobs_name_queued") {
			t.Errorf("%s scans every job with the same name to look for a replacement: %s", name, plan)
		}
	}
}

// The default listing order has a tie-break on id. An index that stops at
// queued_at leaves SQLite sorting the ties itself, which is what made a deep
// page cost half a second; the keyset reader needs the same index to seek.
func TestTheJobListingIsReadInIndexOrder(t *testing.T) {
	s := newTestStore(t)
	plan := explain(t, s, `SELECT id FROM jobs ORDER BY queued_at DESC, id ASC LIMIT 50 OFFSET 1000`)
	if !strings.Contains(plan, "idx_jobs_queued_id") || strings.Contains(plan, "TEMP B-TREE") {
		t.Errorf("the default job order is not served by idx_jobs_queued_id: %s", plan)
	}
}
