package store

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"
)

// seedAttention writes a week of failed jobs, four to a run, and an attention
// record for every run, as a fleet that fails a lot would hold them. It also
// writes records for runs the jobs table no longer has, which are what a prune
// exists to remove, and returns how many of each there are.
func seedAttention(tb testing.TB, s *Store, now time.Time, jobs, stale int) (kept, removed int) {
	tb.Helper()
	ctx := context.Background()
	spacing := (AttentionWindow / time.Duration(jobs)).Truncate(time.Millisecond)
	for i := 0; i < jobs; i++ {
		queued := now.Add(-time.Duration(i+1) * spacing)
		done := queued.Add(time.Minute)
		j := &Job{
			GitHubJobID: int64(5000 + i), GitHubRunID: int64(900000 + i/4), Repo: fmt.Sprintf("acme/svc-%d", (i/4)%3),
			Workflow: "CI", JobName: fmt.Sprintf("build-%d", i%4), HeadBranch: fmt.Sprintf("feature/%d", i%50),
			HeadSHA: fmt.Sprintf("%040x", i), RunAttempt: 1, QueuedAt: queued, StartedAt: &queued,
			State: JobCompleted, CompletedAt: &done, Conclusion: "failure",
		}
		if _, err := s.UpsertJob(ctx, j); err != nil {
			tb.Fatalf("UpsertJob: %v", err)
		}
	}
	runs := (jobs + 3) / 4
	for i := 0; i < runs; i++ {
		if err := s.RecordJobAttentionRun(ctx, fmt.Sprintf("acme/svc-%d", i%3), int64(900000+i), "", now, true); err != nil {
			tb.Fatalf("RecordJobAttentionRun: %v", err)
		}
	}
	for i := 0; i < stale; i++ {
		if err := s.RecordJobAttentionRun(ctx, fmt.Sprintf("acme/svc-%d", i%3), int64(100+i), "", now, true); err != nil {
			tb.Fatalf("RecordJobAttentionRun: %v", err)
		}
	}
	return runs, stale
}

func countAttention(tb testing.TB, s *Store) int {
	tb.Helper()
	var n int
	if err := s.read.QueryRowContext(context.Background(), `SELECT COUNT(*) FROM job_attention_runs`).Scan(&n); err != nil {
		tb.Fatal(err)
	}
	return n
}

// BenchmarkPruneJobAttentionRuns is the statement the job-attention loop runs
// every thirty seconds, on the writer, against a week of failed jobs.
//
// What a prune should leave is counted while the history is written and checked
// before anything is timed, so a statement that removed everything, or nothing,
// is not timed under the name of one that removed what it should. What is timed is
// then the steady state: every record that is left has a job in the window.
func BenchmarkPruneJobAttentionRuns(b *testing.B) {
	for _, jobs := range []int{2000, 8000} {
		b.Run(fmt.Sprintf("jobs=%d", jobs), func(b *testing.B) {
			now := time.Now().UTC().Truncate(time.Millisecond)
			s := newTestStoreAt(b, func() time.Time { return now })
			kept, removed := seedAttention(b, s, now, jobs, 50)
			if got := countAttention(b, s); got != kept+removed {
				b.Fatalf("seeded %d records, want %d", got, kept+removed)
			}
			if err := s.PruneJobAttentionRuns(context.Background()); err != nil {
				b.Fatal(err)
			}
			if got := countAttention(b, s); got != kept {
				b.Fatalf("a prune left %d records, want the %d that have a job in the window", got, kept)
			}
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if err := s.PruneJobAttentionRuns(context.Background()); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

// A record is kept while any job of its run, in its repository, is inside the
// window, and only then. The run id alone does not name a run: two repositories
// number theirs independently.
func TestPruneJobAttentionRunsKeepsExactlyTheRunsWithAJobInTheWindow(t *testing.T) {
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Millisecond)
	s := newTestStoreAt(t, func() time.Time { return now })
	job := func(id, run int64, repo string, queued time.Time) {
		t.Helper()
		done := queued.Add(time.Minute)
		if _, err := s.UpsertJob(ctx, &Job{GitHubJobID: id, GitHubRunID: run, Repo: repo, Workflow: "CI", JobName: fmt.Sprintf("j%d", id),
			HeadBranch: "main", HeadSHA: fmt.Sprintf("%040x", id), RunAttempt: 1, QueuedAt: queued, StartedAt: &queued,
			State: JobCompleted, CompletedAt: &done, Conclusion: "failure"}); err != nil {
			t.Fatal(err)
		}
	}
	recent, old := now.Add(-time.Hour), now.Add(-AttentionWindow-time.Hour)
	job(1, 10, "acme/a", recent) // run 10 of a: a job in the window
	job(2, 20, "acme/a", old)    // run 20 of a: only a job outside it
	job(3, 30, "acme/a", old)    // run 30 of a: one job outside the window...
	job(4, 30, "acme/a", recent) // ...and one inside it
	job(5, 40, "acme/b", recent) // run 40 of b, which a also has a record for
	for _, rec := range []struct {
		repo string
		run  int64
	}{{"acme/a", 10}, {"acme/a", 20}, {"acme/a", 30}, {"acme/a", 40}, {"acme/b", 40}, {"acme/a", 99}} {
		if err := s.RecordJobAttentionRun(ctx, rec.repo, rec.run, "", now, true); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.PruneJobAttentionRuns(ctx); err != nil {
		t.Fatal(err)
	}
	rows, err := s.read.QueryContext(ctx, `SELECT repo, run_id FROM job_attention_runs ORDER BY repo, run_id`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var got []string
	for rows.Next() {
		var repo string
		var run int64
		if err := rows.Scan(&repo, &run); err != nil {
			t.Fatal(err)
		}
		got = append(got, fmt.Sprintf("%s#%d", repo, run))
	}
	want := []string{"acme/a#10", "acme/a#30", "acme/b#40"}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("records left = %v, want %v", got, want)
	}
}

// The prune runs on the one writer. Which index it reads the jobs by is the
// difference between a lookup per record and a week of one repository's jobs per
// record, so the plan is what is held, not only the answer.
func TestPruneJobAttentionRunsLooksEachRunUpByItsIndex(t *testing.T) {
	s := newTestStore(t)
	plan := explain(t, s, pruneJobAttentionRunsSQL, ms(s.Now().Add(-AttentionWindow)))
	if !strings.Contains(plan, "idx_jobs_run") || strings.Contains(plan, "idx_jobs_repo") {
		t.Fatalf("the prune reads jobs by %q, want idx_jobs_run for each record", plan)
	}
}
