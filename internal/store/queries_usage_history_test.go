package store

import (
	"context"
	"testing"
	"time"
)

func TestUsageHistoryOutcomesAndHostAttribution(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	_, pool, host := seedPool(t, s)
	r := &Runner{PoolID: pool.ID, HostID: host.ID, Name: "history-runner", State: RunnerIdle}
	if err := s.CreateRunner(ctx, r); err != nil {
		t.Fatal(err)
	}
	if _, err := s.exec(ctx, `UPDATE runners SET created_at=?,finished_at=? WHERE id=?`, ms(usageAt(0)), ms(usageAt(120)), r.ID); err != nil {
		t.Fatal(err)
	}
	for i, c := range []string{"success", "failure", "cancelled", "", "success"} {
		start, done := usageAt(30), usageAt(60)
		j := &Job{GitHubJobID: int64(990 + i), Repo: "acme/history", PoolID: pool.ID, RunnerID: r.ID, State: JobCompleted, QueuedAt: usageAt(0), StartedAt: &start, CompletedAt: &done, Conclusion: c}
		if i == 4 {
			j.RunnerFault = "runner disappeared"
		}
		if _, err := s.UpsertJob(ctx, j); err != nil {
			t.Fatal(err)
		}
	}
	row := usageRow(t, s, usageAt(0), usageAt(120), UsageByHost, host.ID)
	if row.Succeeded != 1 || row.Failed != 2 || row.Cancelled != 1 || row.Unknown != 1 {
		t.Fatalf("outcomes: %+v", row)
	}
	if row.AllocatedRunnerSeconds == nil || *row.AllocatedRunnerSeconds != 7200 {
		t.Fatalf("host allocation: %+v", row)
	}
	if len(row.History) != 2 || row.History[0].Queued != 5 || row.History[0].Succeeded != 0 || row.History[1].Succeeded != 1 || row.History[1].Failed != 2 {
		t.Fatalf("half-open buckets: %+v", row.History)
	}
	if row.History[0].ExecutionSeconds != 9000 || row.History[1].ExecutionSeconds != 0 {
		t.Fatalf("execution clipping: %+v", row.History)
	}
}

func TestUsageHistoryIncludesOngoingWorkWithoutInventingFutureTime(t *testing.T) {
	s := newTestStore(t)
	_, pool, _ := seedPool(t, s)
	s.now = func() time.Time { return usageAt(90) }
	seedUsageJob(t, s, pool.ID, "acme/ongoing", 0, mins(30), nil)
	row := usageRow(t, s, usageAt(0), usageAt(180), UsageByPool, pool.ID)
	if row.JobExecutionSeconds != 3600 || row.PeakConcurrency != 1 || row.JobsCompleted != 0 {
		t.Fatalf("ongoing work: %+v", row)
	}
	if row.History[0].ExecutionSeconds != 1800 || row.History[1].ExecutionSeconds != 1800 || row.History[2].ExecutionSeconds != 0 {
		t.Fatalf("time beyond observation: %+v", row.History)
	}
}

func TestUsageCapacityCoalescesAndPrunesWithoutInventingCoverage(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	_, pool, _ := seedPool(t, s)
	for _, blocked := range []bool{false, true, false, true} {
		if err := s.RecordUsageCapacity(ctx, pool.ID, usageAt(1), blocked); err != nil {
			t.Fatal(err)
		}
	}
	row := usageRow(t, s, usageAt(0), usageAt(120), UsageByPool, pool.ID)
	if row.History[0].CapacitySamples != 1 || row.History[0].CapacityReached != 1 || row.History[1].CapacitySamples != 0 {
		t.Fatalf("coverage: %+v", row.History)
	}
	n, err := s.PruneUsageCapacity(ctx, usageAt(2))
	if err != nil || n != 1 {
		t.Fatalf("prune: %d %v", n, err)
	}
	rows, err := s.Usage(ctx, usageAt(0), usageAt(120), UsageByPool)
	if err != nil || len(rows) != 0 {
		t.Fatalf("pruned: %+v %v", rows, err)
	}
}

// A week asked for in hours is a week of hours, whatever the window's length
// would have chosen for itself: the activity matrix draws a week as a punch
// card, one square an hour, and the rule that cuts a week into days is the
// rule for a caller that did not say.
func TestUsageHistoryTakesTheIntervalItIsAskedFor(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	_, pool, _ := seedPool(t, s)
	seedUsageJob(t, s, pool.ID, "acme/hours", 0, mins(30), mins(90))

	week := usageAt(7 * 24 * 60)
	rows, err := s.UsageWithInterval(ctx, usageAt(0), week, UsageByPool, UsageHourly)
	if err != nil {
		t.Fatalf("hourly: %v", err)
	}
	if len(rows) != 1 || len(rows[0].History) != 7*24 {
		t.Fatalf("a week in hours: %d rows, %d buckets", len(rows), len(rows[0].History))
	}
	if h := rows[0].History; h[0].Queued != 1 || h[0].Started != 1 || h[1].Unknown != 1 {
		t.Fatalf("the job is in the hours it happened in: %+v %+v", h[0], h[1])
	}

	// The same week, left to the rule, is seven days.
	rows, err = s.UsageWithInterval(ctx, usageAt(0), week, UsageByPool, UsageAutoInterval)
	if err != nil {
		t.Fatalf("auto: %v", err)
	}
	if len(rows[0].History) != 7 {
		t.Fatalf("a week left to the rule: %d buckets", len(rows[0].History))
	}

	// And a day asked for in days is one bucket, where the rule would give 24.
	rows, err = s.UsageWithInterval(ctx, usageAt(0), usageAt(24*60), UsageByPool, UsageDaily)
	if err != nil {
		t.Fatalf("daily: %v", err)
	}
	if len(rows[0].History) != 1 || rows[0].History[0].Unknown != 1 {
		t.Fatalf("a day in days: %+v", rows[0].History)
	}
}
