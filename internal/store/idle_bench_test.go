package store

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// newFileStore is a store on a file in WAL mode, as the controller runs it. The
// in-memory store the other benchmarks use has no file to read pages from, so it
// cannot show what a scan costs in the reads the file takes, and its writer is the
// same single connection but never waits on a disk.
func newFileStore(tb testing.TB) *Store {
	tb.Helper()
	s, err := Open(context.Background(), Options{Path: filepath.Join(tb.TempDir(), "zoomies.db")})
	if err != nil {
		tb.Fatalf("Open: %v", err)
	}
	tb.Cleanup(func() { s.Close() })
	return s
}

// countWriterTakes counts how many times the single writer is taken from now on.
// Every write queues for it, behind every heartbeat, webhook and scheduling pass,
// so a loop that takes it to find nothing has made the whole controller wait to
// learn that.
func countWriterTakes(s *Store) *atomic.Int64 {
	n := new(atomic.Int64)
	s.ObserveWrites(func(_, _ time.Duration) { n.Add(1) })
	return n
}

// The enrichment loop asks for work once a second, and almost always there is
// none. This is that question with nothing due: either an empty table or rows
// that were claimed a moment ago and are waiting out their minute. The metric is
// how often the question took the writer, which is the cost the controller's
// webhooks and heartbeats pay for it.
func BenchmarkClaimEnrichmentIdle(b *testing.B) {
	ctx := context.Background()
	for _, waiting := range []int{0, 200} {
		b.Run(fmt.Sprintf("waiting=%d", waiting), func(b *testing.B) {
			s := newFileStore(b)
			for i := range waiting {
				if err := s.QueueEnrichment(ctx, "job", fmt.Sprintf("job_%04d", i)); err != nil {
					b.Fatalf("QueueEnrichment: %v", err)
				}
			}
			// Claimed once, so each is held back for a minute and none is due.
			if waiting > 0 {
				if claimed, err := s.ClaimEnrichment(ctx, waiting); err != nil || len(claimed) != waiting {
					b.Fatalf("claiming the setup rows = %d rows, err = %v; want %d", len(claimed), err, waiting)
				}
			}
			var rows int
			if err := s.read.QueryRowContext(ctx, `SELECT COUNT(*) FROM control_enrichment`).Scan(&rows); err != nil || rows != waiting {
				b.Fatalf("the table holds %d rows, err = %v; want %d", rows, err, waiting)
			}
			if due, err := s.ClaimEnrichment(ctx, 4); err != nil || len(due) != 0 {
				b.Fatalf("a claim on an idle table returned %d items, err = %v; want none", len(due), err)
			}

			takes := countWriterTakes(s)
			b.ReportAllocs()
			for b.Loop() {
				if _, err := s.ClaimEnrichment(ctx, 4); err != nil {
					b.Fatal(err)
				}
			}
			b.ReportMetric(float64(takes.Load())/float64(b.N), "writer-takes/op")
		})
	}
}

// The Eli watch loop asks for a due watch every three seconds whether or not
// anybody has set one up. Here none is due: either there are no watches, or each
// is waiting for its next check.
func BenchmarkClaimDueEliRepairWatchIdle(b *testing.B) {
	ctx := context.Background()
	for _, waiting := range []int{0, 50} {
		b.Run(fmt.Sprintf("waiting=%d", waiting), func(b *testing.B) {
			s := newFileStore(b)
			if waiting > 0 {
				inst := &Installation{AppID: 1, InstallationID: 2, Target: "acme", TargetType: TargetOrg}
				if err := s.CreateInstallation(ctx, inst); err != nil {
					b.Fatalf("CreateInstallation: %v", err)
				}
				for i := range waiting {
					if _, err := s.AdmitEliRepairWatch(ctx, EliRepairWatch{InstallationID: inst.ID, Repo: "acme/repo", PullNumber: i + 1, OwnerID: "usr_one", Checks: true}); err != nil {
						b.Fatalf("AdmitEliRepairWatch: %v", err)
					}
				}
				// Every watch has just been read, and is next due an hour from now.
				if _, err := s.exec(ctx, `UPDATE eli_repair_watches SET next_check_at=?`, ms(s.Now().Add(time.Hour))); err != nil {
					b.Fatalf("pushing the checks back: %v", err)
				}
			}
			var rows int
			if err := s.read.QueryRowContext(ctx, `SELECT COUNT(*) FROM eli_repair_watches`).Scan(&rows); err != nil || rows != waiting {
				b.Fatalf("the table holds %d watches, err = %v; want %d", rows, err, waiting)
			}
			now := s.Now()
			if w, err := s.ClaimDueEliRepairWatch(ctx, now, time.Minute); err != ErrNotFound {
				b.Fatalf("a claim with nothing due returned %+v, err = %v; want ErrNotFound", w, err)
			}

			takes := countWriterTakes(s)
			b.ReportAllocs()
			for b.Loop() {
				if _, err := s.ClaimDueEliRepairWatch(ctx, now, time.Minute); err != ErrNotFound {
					b.Fatalf("err = %v; want ErrNotFound", err)
				}
			}
			b.ReportMetric(float64(takes.Load())/float64(b.N), "writer-takes/op")
		})
	}
}

// wideJobs puts n completed jobs in the file, spread over the last twenty-nine
// days so that every one is inside the thirty-day window the reads below ask for
// (the oldest of thirty days exactly would fall outside it by the time the seed
// finished), each about as wide as a real row: a dozen steps and a tail of the
// runner's output. The width is the point. A scan reads the pages the rows sit
// on, so a table of narrow rows is a table that fits in a few pages and says
// nothing about what reading a month of a busy fleet's history costs.
func wideJobs(tb testing.TB, s *Store, n int) {
	tb.Helper()
	var steps strings.Builder
	steps.WriteString("[")
	for i := 1; i <= 12; i++ {
		if i > 1 {
			steps.WriteString(",")
		}
		fmt.Fprintf(&steps, `{"number":%d,"name":"Run step number %d of the workflow","status":"completed","conclusion":"success","started_at":"2026-10-20T10:00:00Z","completed_at":"2026-10-20T10:00:09Z"}`, i, i)
	}
	steps.WriteString("]")
	tail := strings.Repeat("compiling package github.com/example/pkg ... ok\n", 10)

	now := time.Now().UnixMilli()
	step := int64(29*24*time.Hour/time.Millisecond) / int64(n)
	_, err := s.write.ExecContext(context.Background(), `
WITH RECURSIVE seq(i) AS (SELECT 0 UNION ALL SELECT i+1 FROM seq WHERE i < ?1 - 1)
INSERT INTO jobs (id, github_job_id, github_run_id, repo, workflow, job_name, labels, state, conclusion,
	pool_id, runner_id, runner_name, html_url, queued_at, started_at, completed_at, matched,
	head_branch, head_sha, run_attempt, steps, installation_id, output_tail)
SELECT 'job_' || i, 1000000 + i, 40000000000 + (i / 6),
	'acme/repo' || ((i / 6) % 6), 'wf' || ((i / 6) % 8), 'job-' || (i % 6),
	'["linux"]', 'completed', 'success',
	'pool_' || (i % 4), 'run_' || i, 'runner-' || i, 'https://github.com/acme/repo/actions/runs/' || (i / 6),
	?2 - ((?1 - i) * ?3), ?2 - ((?1 - i) * ?3) + 20000, ?2 - ((?1 - i) * ?3) + 80000, 1,
	'branch-' || ((i / 6) % 40), 'sha' || (i / 6), 1, ?4, 'ins_bench', ?5
FROM seq`, n, now, step, steps.String(), tail)
	if err != nil {
		tb.Fatalf("seeding %d jobs: %v", n, err)
	}
}

// BenchmarkJobTableScans is two of the reads the controller repeats against a
// month of history on a file: the repositories this fleet has served in the
// retention window, which Kennel Club asks every minute, and a week of what the
// jobs used, which the problems list asks when a pool is under pressure. Both
// read most of the table, so what they cost is the cost of getting its pages.
func BenchmarkJobTableScans(b *testing.B) {
	ctx := context.Background()
	const jobs = 20000
	s := newFileStore(b)
	wideJobs(b, s, jobs)

	now := time.Now()
	month, week := now.Add(-30*24*time.Hour), now.Add(-7*24*time.Hour)

	served, err := s.KennelServedRepos(ctx, month)
	if err != nil {
		b.Fatalf("KennelServedRepos: %v", err)
	}
	total := 0
	for _, r := range served {
		total += r.Jobs
	}
	if total != jobs {
		b.Fatalf("the served repositories add up to %d jobs; the table holds %d", total, jobs)
	}
	var inWeek int
	if err := s.read.QueryRowContext(ctx, `SELECT COUNT(*) FROM jobs WHERE queued_at >= ?`, ms(week)).Scan(&inWeek); err != nil {
		b.Fatalf("counting the week: %v", err)
	}
	// The peaks figure leaves the count out, so the scenario is read with the counts
	// and timed with the peaks, which is the one the problems list asks for.
	weekly, err := s.JobStatsOf(ctx, JobFilter{Since: &week}, []string{GroupByPool}, MeasureCounts)
	if err != nil {
		b.Fatalf("JobStatsOf: %v", err)
	}
	counted := 0
	for _, g := range weekly.Groups {
		counted += g.Count
	}
	if inWeek < jobs/5 || counted != inWeek {
		b.Fatalf("the week's statistics cover %d jobs of the %d in the week (%d in the table); want all of a fifth or more", counted, inWeek, jobs)
	}

	b.Run("served_repositories_30d", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			if _, err := s.KennelServedRepos(ctx, month); err != nil {
				b.Fatal(err)
			}
		}
	})
	b.Run("job_peaks_7d", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			if _, err := s.JobStatsOf(ctx, JobFilter{Since: &week}, []string{GroupByPool}, MeasurePeaks); err != nil {
				b.Fatal(err)
			}
		}
	})
}
