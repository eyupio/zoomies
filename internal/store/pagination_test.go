package store

import (
	"context"
	"fmt"
	"testing"
	"time"
)

// Offset pagination is how every long list in the product is read -- the
// Runners grid, the Jobs grid, the audit log -- and none of it was tested at
// this layer. The failures it is prone to are quiet ones: a page that repeats a
// row the previous page ended with, a page that skips one, a total that ignores
// the filter and so promises pages that are not there. All three look like a
// working grid until somebody counts.
//
// This is also the net under the measurement: the indexes and queries behind
// these lists are about to be changed on the evidence of a load test, and a
// change that made them fast and wrong would be worse than the slowness.

// walk reads a list a page at a time and returns the ids in the order they were
// served, plus the totals each page reported.
func walk[T any](t *testing.T, size int, list func(p Page) ([]T, int, error), id func(T) string) ([]string, []int) {
	t.Helper()
	var ids []string
	var totals []int
	for offset := 0; ; offset += size {
		items, total, err := list(Page{Limit: size, Offset: offset})
		if err != nil {
			t.Fatalf("listing at offset %d: %v", offset, err)
		}
		totals = append(totals, total)
		for _, item := range items {
			ids = append(ids, id(item))
		}
		if len(items) < size {
			return ids, totals
		}
		if offset > 10*total+size {
			t.Fatal("the pages never ran out; the walk is looping")
		}
	}
}

// distinct fails with the duplicate named, because "23 rows, 22 unique" is not
// a message anybody can act on.
func distinct(t *testing.T, what string, ids []string) {
	t.Helper()
	seen := map[string]bool{}
	for i, id := range ids {
		if seen[id] {
			t.Fatalf("%s: %s was served twice, at position %d: a page repeated a row the one before it had", what, id, i)
		}
		seen[id] = true
	}
}

func TestPagingThroughRunnersServesEveryRowExactlyOnce(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	_, pool, host := seedPool(t, s)

	const n = 23
	for i := range n {
		r := &Runner{PoolID: pool.ID, HostID: host.ID, Name: fmt.Sprintf("runner-%02d", i), State: RunnerIdle}
		if err := s.CreateRunner(ctx, r); err != nil {
			t.Fatalf("CreateRunner: %v", err)
		}
	}

	ids, totals := walk(t, 5, func(p Page) ([]*Runner, int, error) {
		return s.ListRunners(ctx, RunnerFilter{}, p)
	}, func(r *Runner) string { return r.ID })

	if len(ids) != n {
		t.Errorf("walked %d runners, want %d: the pages do not tile the list", len(ids), n)
	}
	distinct(t, "runners", ids)
	for i, total := range totals {
		if total != n {
			t.Errorf("page %d reported a total of %d, want %d: the count must be of the whole list, not the page", i, total, n)
		}
	}

	// Past the end is an empty page and the same total, not an error and not a
	// wrapped-around first page.
	items, total, err := s.ListRunners(ctx, RunnerFilter{}, Page{Limit: 5, Offset: n + 10})
	if err != nil {
		t.Fatalf("listing past the end: %v", err)
	}
	if len(items) != 0 || total != n {
		t.Errorf("past the end = %d items with total %d, want 0 and %d", len(items), total, n)
	}
}

func TestAFilteredPageCountsOnlyWhatItFiltersTo(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	_, pool, host := seedPool(t, s)

	for i := range 12 {
		state := RunnerIdle
		if i%3 == 0 {
			state = RunnerBusy
		}
		r := &Runner{PoolID: pool.ID, HostID: host.ID, Name: fmt.Sprintf("runner-%02d", i), State: state}
		if err := s.CreateRunner(ctx, r); err != nil {
			t.Fatalf("CreateRunner: %v", err)
		}
	}

	// Four of the twelve are busy. A total that counted all twelve would offer
	// a second page that does not exist, and the grid would show an empty one.
	items, total, err := s.ListRunners(ctx, RunnerFilter{States: []RunnerState{RunnerBusy}}, Page{Limit: 50})
	if err != nil {
		t.Fatalf("ListRunners: %v", err)
	}
	if len(items) != 4 || total != 4 {
		t.Fatalf("busy runners = %d items with total %d, want 4 and 4", len(items), total)
	}
}

func TestPagingThroughJobsAndTheAuditLogServesEveryRowExactlyOnce(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	now := time.Now()

	const n = 17
	for i := range n {
		if _, err := s.UpsertJob(ctx, &Job{
			GitHubJobID: int64(1000 + i),
			Repo:        "acme/widgets",
			Workflow:    "CI",
			JobName:     fmt.Sprintf("build-%02d", i),
			State:       JobQueued,
			QueuedAt:    now.Add(-time.Duration(i) * time.Minute),
		}); err != nil {
			t.Fatalf("UpsertJob: %v", err)
		}
		if err := s.AppendAudit(ctx, &AuditEvent{
			ActorKind: "system", ActorName: "test",
			Action: "pool.update", TargetKind: "pool", TargetID: fmt.Sprintf("pool_%02d", i),
			CreatedAt: now.Add(-time.Duration(i) * time.Minute),
		}); err != nil {
			t.Fatalf("AppendAudit: %v", err)
		}
	}

	jobs, jobTotals := walk(t, 4, func(p Page) ([]*Job, int, error) {
		return s.ListJobs(ctx, JobFilter{}, p)
	}, func(j *Job) string { return j.ID })
	if len(jobs) != n {
		t.Errorf("walked %d jobs, want %d", len(jobs), n)
	}
	distinct(t, "jobs", jobs)
	if jobTotals[0] != n {
		t.Errorf("jobs total = %d, want %d", jobTotals[0], n)
	}

	audit, auditTotals := walk(t, 4, func(p Page) ([]*AuditEvent, int, error) {
		return s.ListAudit(ctx, AuditFilter{}, p)
	}, func(e *AuditEvent) string { return e.ID })
	if len(audit) != n {
		t.Errorf("walked %d audit events, want %d", len(audit), n)
	}
	distinct(t, "audit events", audit)
	if auditTotals[0] != n {
		t.Errorf("audit total = %d, want %d", auditTotals[0], n)
	}
}

// The clamp is what stops one caller asking for the whole table. It is
// documented as 500 on the three long lists, and a limit above it is reduced
// rather than refused: a bookmarked URL with limit=100000 should show a page.
func TestALimitAboveTheCeilingIsReducedToIt(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	_, pool, host := seedPool(t, s)
	for i := range 3 {
		r := &Runner{PoolID: pool.ID, HostID: host.ID, Name: fmt.Sprintf("r-%d", i), State: RunnerIdle}
		if err := s.CreateRunner(ctx, r); err != nil {
			t.Fatalf("CreateRunner: %v", err)
		}
	}
	if got := (Page{Limit: 100_000}).limit(50, 500); got != 500 {
		t.Errorf("a limit of 100000 became %d, want the 500 ceiling", got)
	}
	if got := (Page{}).limit(50, 500); got != 50 {
		t.Errorf("an unset limit became %d, want the default 50", got)
	}
	if got := (Page{Limit: -1}).limit(50, 500); got != 50 {
		t.Errorf("a negative limit became %d, want the default 50", got)
	}
	// And a negative offset is the first page rather than a SQL error.
	if _, _, err := s.ListRunners(ctx, RunnerFilter{}, Page{Limit: 2, Offset: -5}); err != nil {
		t.Errorf("a negative offset errored: %v", err)
	}
}
