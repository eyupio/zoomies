package store

import (
	"context"
	"fmt"
	"testing"
	"time"
)

// BenchmarkPruneJobsPastAHistoryOfRecentOnes times the hourly prune on the
// history a fleet has most of the time: a table of recent jobs with a few old ones
// at the far end of the key. Nothing indexes "old", so each batch walks the table in
// key order until it has found enough, and what a batch costs is how many rows
// lie between the start of the table and the old ones.
//
// The old rows are put back after every pass with the clock stopped, so each pass
// meets the same table, and what is checked before anything is timed is the
// scenario the name describes: the table is as large as it says, the old jobs are
// the last ones by key, and a pass deletes exactly them and no others.
func BenchmarkPruneJobsPastAHistoryOfRecentOnes(b *testing.B) {
	const recent, old = 8000, 700
	ctx := context.Background()
	now := time.Date(2026, 11, 22, 12, 0, 0, 0, time.UTC)
	s := newTestStoreAt(b, func() time.Time { return now })

	put := func(i int, finished time.Time) {
		done := finished
		if _, err := s.UpsertJob(ctx, &Job{
			ID: fmt.Sprintf("job_%06d", i), GitHubJobID: int64(i + 1), State: JobCompleted,
			Conclusion: "success", QueuedAt: finished.Add(-time.Minute), CompletedAt: &done,
		}); err != nil {
			b.Fatalf("UpsertJob: %v", err)
		}
	}
	for i := range recent {
		put(i, now.Add(-time.Hour))
	}
	restoreOld := func() {
		for i := recent; i < recent+old; i++ {
			put(i, now.Add(-72*time.Hour))
		}
	}
	count := func() (n int) {
		if err := s.read.QueryRowContext(ctx, `SELECT COUNT(*) FROM jobs`).Scan(&n); err != nil {
			b.Fatal(err)
		}
		return n
	}

	restoreOld()
	if got := count(); got != recent+old {
		b.Fatalf("the table holds %d jobs, want %d", got, recent+old)
	}
	cutoff := now.Add(-24 * time.Hour)
	if n, err := s.PruneJobs(ctx, cutoff); err != nil || n != old {
		b.Fatalf("the scenario's first pass pruned %d, %v; want exactly the %d old jobs", n, err, old)
	}
	if got := count(); got != recent {
		b.Fatalf("after a pass the table holds %d jobs, want the %d recent ones", got, recent)
	}

	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		b.StopTimer()
		restoreOld()
		b.StartTimer()
		if n, err := s.PruneJobs(ctx, cutoff); err != nil || n != old {
			b.Fatalf("a pass pruned %d, %v; want %d", n, err, old)
		}
	}
}
