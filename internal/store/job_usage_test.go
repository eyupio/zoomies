package store

import (
	"context"
	"strings"
	"testing"
	"time"
)

// usageJob records a job on a runner in the given state.
func usageJob(t *testing.T, s *Store, n int, name, pool, runner string, state JobState, queued time.Time) *Job {
	t.Helper()
	j := &Job{
		GitHubJobID: int64(5000 + n), Repo: "eyupio/zoomies", Workflow: "CI", JobName: name,
		Labels: StringSlice{"self-hosted"}, State: state, QueuedAt: queued, Matched: true,
		PoolID: pool, RunnerID: runner,
	}
	if state == JobCompleted {
		done := queued.Add(time.Hour)
		j.StartedAt, j.CompletedAt, j.Conclusion = &queued, &done, "success"
	}
	out, _, err := s.ApplyJob(context.Background(), j)
	if err != nil {
		t.Fatalf("recording job %d: %v", n, err)
	}
	return out
}

// Samples arrive on every heartbeat, out of order after a reconnect, and
// before and after the job. Only the job GitHub says is running is charged,
// and a later, smaller sample must not lower what was already seen.
func TestRecordJobUsageRaisesOnlyTheRunningJobsPeaks(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	running := usageJob(t, s, 1, "Go (controller)", "pool_a", "run_1", JobInProgress, versionsEpoch)
	done := usageJob(t, s, 2, "Go (controller)", "pool_a", "run_1", JobCompleted, versionsEpoch.Add(-time.Hour))

	for _, sample := range []struct {
		cpus float64
		mb   int64
	}{{1.5, 900}, {3.9, 2800}, {0.2, 100}, {0, 0}} {
		if err := s.RecordJobUsage(ctx, "run_1", sample.cpus, sample.mb); err != nil {
			t.Fatalf("RecordJobUsage: %v", err)
		}
	}
	got, err := s.GetJob(ctx, running.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.PeakCPUs != 3.9 || got.PeakMemoryMB != 2800 {
		t.Fatalf("running job's peaks = %v CPU, %d MB, want 3.9 and 2800", got.PeakCPUs, got.PeakMemoryMB)
	}
	other, err := s.GetJob(ctx, done.ID)
	if err != nil {
		t.Fatal(err)
	}
	if other.PeakCPUs != 0 || other.PeakMemoryMB != 0 {
		t.Fatalf("a finished job was charged a later sample: %v CPU, %d MB", other.PeakCPUs, other.PeakMemoryMB)
	}
}

// A step killed with exit 137 leaves a runner that finishes the job, so the
// kill is reported after GitHub has closed it. It is recorded all the same,
// once, and a fault the runner reported first keeps its own words.
func TestMarkJobOOMKilledMarksTheRunnersLastJobOnce(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	usageJob(t, s, 1, "older", "pool_a", "run_1", JobCompleted, versionsEpoch.Add(-2*time.Hour))
	last := usageJob(t, s, 2, "Go (controller)", "pool_a", "run_1", JobCompleted, versionsEpoch)

	j, marked, err := s.MarkJobOOMKilled(ctx, "run_1", "runner r1 was killed for memory")
	if err != nil || !marked {
		t.Fatalf("MarkJobOOMKilled = %v, %v; want the job marked", marked, err)
	}
	if j.ID != last.ID || !j.OOMKilled || j.FaultKind != FaultOutOfMemory || j.RunnerFault != "runner r1 was killed for memory" {
		t.Fatalf("marked %+v, want the runner's last job, out of memory", j)
	}
	if _, again, err := s.MarkJobOOMKilled(ctx, "run_1", "a second report"); err != nil || again {
		t.Fatalf("a second report marked it again (%v, %v)", again, err)
	}

	// A fault recorded first keeps its sentence and category.
	lost := usageJob(t, s, 3, "lost", "pool_a", "run_2", JobInProgress, versionsEpoch)
	if _, _, err := s.SetJobRunnerFault(ctx, lost.ID, "runner exited with code 137", FaultOutOfMemory); err != nil {
		t.Fatal(err)
	}
	j, marked, err = s.MarkJobOOMKilled(ctx, "run_2", "ignored")
	if err != nil || !marked || j.RunnerFault != "runner exited with code 137" {
		t.Fatalf("MarkJobOOMKilled = %+v, %v, %v", j, marked, err)
	}

	if j, marked, err := s.MarkJobOOMKilled(ctx, "run_none", "x"); j != nil || marked || err != nil {
		t.Fatalf("a runner with no job marked %+v, %v, %v", j, marked, err)
	}
}

// The history a profile is taken from is the job's own measured runs, per
// pool, newest first, at most JobUsageHistoryLimit of them -- and a run that
// was never measured is not in it at all, because a zero is "unknown", not
// "needed nothing".
func TestJobUsageHistoryIsEachPoolsRecentMeasuredRuns(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	n := 0
	record := func(pool string, mb int64, oom bool, at time.Time) {
		n++
		runner := "run_" + strings.Repeat("x", n)
		usageJob(t, s, n, "Go (controller)", pool, runner, JobInProgress, at)
		if mb > 0 {
			if err := s.RecordJobUsage(ctx, runner, 2, mb); err != nil {
				t.Fatal(err)
			}
		}
		done := at.Add(time.Hour)
		if _, _, err := s.ApplyJob(ctx, &Job{GitHubJobID: int64(5000 + n), State: JobCompleted, Conclusion: "failure",
			Repo: "eyupio/zoomies", Workflow: "CI", JobName: "Go (controller)", StartedAt: &at, CompletedAt: &done}); err != nil {
			t.Fatal(err)
		}
		if oom {
			if _, _, err := s.MarkJobOOMKilled(ctx, runner, "killed"); err != nil {
				t.Fatal(err)
			}
		}
	}
	for i := range JobUsageHistoryLimit + 5 {
		record("pool_a", int64(1000+i), false, versionsEpoch.Add(time.Duration(i)*time.Hour))
	}
	record("pool_b", 3000, true, versionsEpoch)
	record("pool_b", 0, false, versionsEpoch.Add(time.Hour)) // never measured

	got, err := s.JobUsageHistory(ctx, []JobUsageKey{{Repo: "eyupio/zoomies", Workflow: "CI", JobName: "Go (controller)"}})
	if err != nil {
		t.Fatalf("JobUsageHistory: %v", err)
	}
	a := got[JobUsageKey{Repo: "eyupio/zoomies", Workflow: "CI", JobName: "Go (controller)", PoolID: "pool_a"}]
	if len(a) != JobUsageHistoryLimit || a[0].MemoryMB != int64(1000+JobUsageHistoryLimit+4) {
		t.Fatalf("pool_a history = %d runs, newest %+v; want %d, newest first", len(a), a[0], JobUsageHistoryLimit)
	}
	b := got[JobUsageKey{Repo: "eyupio/zoomies", Workflow: "CI", JobName: "Go (controller)", PoolID: "pool_b"}]
	if len(b) != 1 || !b[0].OOMKilled || b[0].MemoryMB != 3000 {
		t.Fatalf("pool_b history = %+v, want the one measured, killed run", b)
	}
}

// A run's CPU throttling is kept beside its peak, because "never held back by
// its quota" is what says a job does not want more cores, and a peak alone
// cannot: a job pinned at its ceiling and a job that merely used that much look
// the same without it.
func TestJobUsageHistoryCarriesEachRunsCPUThrottling(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	usageJob(t, s, 1, "Go (controller)", "pool_a", "run_1", JobInProgress, versionsEpoch)
	if err := s.RecordJobUsage(ctx, "run_1", 2.5, 900); err != nil {
		t.Fatal(err)
	}
	if err := s.RecordJobThrottle(ctx, "run_1", 1000, 120); err != nil {
		t.Fatal(err)
	}
	done := versionsEpoch.Add(time.Hour)
	if _, _, err := s.ApplyJob(ctx, &Job{GitHubJobID: 5001, State: JobCompleted, Conclusion: "success",
		Repo: "eyupio/zoomies", Workflow: "CI", JobName: "Go (controller)", StartedAt: &versionsEpoch, CompletedAt: &done}); err != nil {
		t.Fatal(err)
	}
	got, err := s.JobUsageHistory(ctx, []JobUsageKey{{Repo: "eyupio/zoomies", Workflow: "CI", JobName: "Go (controller)"}})
	if err != nil {
		t.Fatalf("JobUsageHistory: %v", err)
	}
	runs := got[JobUsageKey{Repo: "eyupio/zoomies", Workflow: "CI", JobName: "Go (controller)", PoolID: "pool_a"}]
	if len(runs) != 1 || runs[0].Periods != 1000 || runs[0].ThrottledPeriods != 120 {
		t.Fatalf("history = %+v, want one run with 120 of 1000 periods throttled", runs)
	}
}

// The history is read for every distinct queued job on every pass, so it must
// seek its index rather than walk a jobs table that grows without bound.
func TestJobUsageHistorySeeksItsIndex(t *testing.T) {
	s := newTestStore(t)
	rows, err := s.read.QueryContext(t.Context(), "EXPLAIN QUERY PLAN "+jobUsageHistorySQL, "r", "w", "j", 10)
	if err != nil {
		t.Fatalf("EXPLAIN QUERY PLAN: %v", err)
	}
	defer rows.Close()
	var plan []string
	for rows.Next() {
		var id, parent, notused int
		var detail string
		if err := rows.Scan(&id, &parent, &notused, &detail); err != nil {
			t.Fatal(err)
		}
		plan = append(plan, detail)
	}
	if joined := strings.Join(plan, "\n"); !strings.Contains(joined, "idx_jobs_usage_profile") {
		t.Fatalf("the history lookup does not use its index:\n%s", joined)
	}
}

// job_stats carries the heaviest job in each group and how many were killed
// for memory, so "which of our jobs need big hosts" is one call.
func TestJobStatsCarryPeakUsageAndMemoryKills(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	usageJob(t, s, 1, "light", "pool_a", "run_1", JobInProgress, versionsEpoch)
	usageJob(t, s, 2, "heavy", "pool_a", "run_2", JobInProgress, versionsEpoch)
	for _, u := range []struct {
		runner string
		cpus   float64
		mb     int64
	}{{"run_1", 0.5, 400}, {"run_2", 3.5, 5200}} {
		if err := s.RecordJobUsage(ctx, u.runner, u.cpus, u.mb); err != nil {
			t.Fatal(err)
		}
	}
	for n := 1; n <= 2; n++ {
		done := versionsEpoch.Add(time.Hour)
		if _, _, err := s.ApplyJob(ctx, &Job{GitHubJobID: int64(5000 + n), State: JobCompleted, Conclusion: "failure",
			Repo: "eyupio/zoomies", StartedAt: &versionsEpoch, CompletedAt: &done}); err != nil {
			t.Fatal(err)
		}
	}
	if _, _, err := s.MarkJobOOMKilled(ctx, "run_2", "killed"); err != nil {
		t.Fatal(err)
	}
	res, err := s.JobStats(ctx, JobFilter{}, []string{GroupByJobName})
	if err != nil {
		t.Fatalf("JobStats: %v", err)
	}
	by := map[string]JobStatsGroup{}
	for _, g := range res.Groups {
		by[g.Keys[GroupByJobName]] = g
	}
	heavy := by["heavy"]
	if heavy.PeakCPUs == nil || *heavy.PeakCPUs != 3.5 || heavy.PeakMemoryMB == nil || *heavy.PeakMemoryMB != 5200 || heavy.OOMKilled != 1 {
		t.Fatalf("heavy = %+v", heavy)
	}
	if light := by["light"]; light.OOMKilled != 0 || light.PeakMemoryMB == nil || *light.PeakMemoryMB != 400 {
		t.Fatalf("light = %+v", light)
	}
	// Both were measured, so the coverage is the whole group.
	if heavy.MeasuredMemory != 1 || by["light"].MeasuredMemory != 1 {
		t.Fatalf("measured = %d and %d, want 1 each", heavy.MeasuredMemory, by["light"].MeasuredMemory)
	}
}

// A job too short to be sampled has no peak, so a group's measured count is
// smaller than its job count: the figure advice rests on is how many were measured.
func TestJobStatsSayHowManyJobsWereMeasured(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	usageJob(t, s, 1, "build", "pool_a", "run_1", JobInProgress, versionsEpoch)
	usageJob(t, s, 2, "build", "pool_a", "run_2", JobInProgress, versionsEpoch)
	if err := s.RecordJobUsage(ctx, "run_1", 1, 900); err != nil {
		t.Fatal(err)
	}
	for n := 1; n <= 2; n++ {
		done := versionsEpoch.Add(time.Hour)
		if _, _, err := s.ApplyJob(ctx, &Job{GitHubJobID: int64(5000 + n), State: JobCompleted, Conclusion: "success",
			Repo: "eyupio/zoomies", StartedAt: &versionsEpoch, CompletedAt: &done}); err != nil {
			t.Fatal(err)
		}
	}
	res, err := s.JobStats(ctx, JobFilter{}, []string{GroupByPool})
	if err != nil || len(res.Groups) != 1 {
		t.Fatalf("JobStats = %+v, %v", res, err)
	}
	if g := res.Groups[0]; g.Count != 2 || g.MeasuredMemory != 1 {
		t.Fatalf("count %d, measured %d; want 2 completed and 1 measured", g.Count, g.MeasuredMemory)
	}
}

// A pair's halves are kept per job, as the most each used, and the statistics say how many
// jobs have them: a job that ran before they were kept has none and is not counted, so the
// advice that sizes a floor by them can wait for enough that do.
func TestRecordJobHalfPeaksKeepsTheMostEachHalfUsedAndCountsTheJobsThatHaveThem(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	running := usageJob(t, s, 1, "build", "pool_a", "run_1", JobInProgress, versionsEpoch)
	old := usageJob(t, s, 2, "build", "pool_a", "run_old", JobCompleted, versionsEpoch.Add(-time.Hour))
	_ = old
	for _, h := range [][2]int64{{800, 200}, {400, 2600}, {100, 50}, {0, 0}} {
		if err := s.RecordJobHalfPeaks(ctx, "run_1", h[0], h[1]); err != nil {
			t.Fatalf("RecordJobHalfPeaks: %v", err)
		}
	}
	// The job completes; it is the week's statistics that read it.
	done := *running
	done.State = JobCompleted
	when := versionsEpoch.Add(time.Minute)
	done.StartedAt, done.CompletedAt, done.Conclusion = &versionsEpoch, &when, "success"
	if _, _, err := s.ApplyJob(ctx, &done); err != nil {
		t.Fatal(err)
	}
	if err := s.RecordJobUsage(ctx, "run_old", 1, 500); err != nil {
		t.Fatal(err)
	}
	since := versionsEpoch.Add(-24 * time.Hour)
	res, err := s.JobStats(ctx, JobFilter{Since: &since}, []string{GroupByPool})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Groups) != 1 {
		t.Fatalf("groups = %d", len(res.Groups))
	}
	g := res.Groups[0]
	if g.PeakRunnerMemoryMB != 800 || g.PeakDaemonMemoryMB != 2600 || g.MeasuredHalves != 1 {
		t.Errorf("halves = runner %d, daemon %d from %d jobs; want 800, 2600 from 1", g.PeakRunnerMemoryMB, g.PeakDaemonMemoryMB, g.MeasuredHalves)
	}
}

// Every host read counts its live runners, and removed and failed ones are kept for the
// history, so the count has to be answered from the live rows alone: a covering read of the
// partial index, not a walk of every runner the host ever had.
func TestLiveRunnerCountReadsOnlyThePartialIndex(t *testing.T) {
	s := newTestStore(t)
	rows, err := s.read.QueryContext(t.Context(), "EXPLAIN QUERY PLAN "+countRunnersByHostSQL)
	if err != nil {
		t.Fatalf("EXPLAIN QUERY PLAN: %v", err)
	}
	defer rows.Close()
	var plan []string
	for rows.Next() {
		var id, parent, notused int
		var detail string
		if err := rows.Scan(&id, &parent, &notused, &detail); err != nil {
			t.Fatal(err)
		}
		plan = append(plan, detail)
	}
	if joined := strings.Join(plan, "\n"); !strings.Contains(joined, "COVERING INDEX idx_runners_live_host") {
		t.Fatalf("the live runner count does not read its partial index:\n%s", joined)
	}
}

// How full each of a job's in-memory folders got is kept on the job the way
// its memory peak is: raised by every sample and never lowered, only while
// GitHub says the job is in progress on that runner. A folder that filled is
// what the explanation reads from after the runner, and the folder, are gone.
func TestAJobKeepsTheFullestEachInMemoryFolderGot(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	_, pool, host := seedPool(t, s)
	r := &Runner{PoolID: pool.ID, HostID: host.ID, Name: "full", State: RunnerBusy}
	if err := s.CreateRunner(ctx, r); err != nil {
		t.Fatal(err)
	}
	started := s.Now()
	job, err := s.UpsertJob(ctx, &Job{GitHubJobID: 77, Repo: "acme/widgets", Workflow: "CI", JobName: "build",
		State: JobInProgress, PoolID: pool.ID, RunnerID: r.ID, QueuedAt: started, StartedAt: &started, Matched: true})
	if err != nil {
		t.Fatal(err)
	}
	for _, sample := range [][]ScratchPeak{
		{{Path: "/tmp", PeakMB: 400, SizeMB: 1024}, {Path: "/home/runner/_work", PeakMB: 300, SizeMB: 4096}},
		{{Path: "/tmp", PeakMB: 962, SizeMB: 1024}},
		{{Path: "/tmp", PeakMB: 10, SizeMB: 1024}, {Path: "/home/runner/_work", PeakMB: 250, SizeMB: 4096}},
	} {
		if err := s.RecordJobScratchPeaks(ctx, r.ID, sample); err != nil {
			t.Fatal(err)
		}
	}
	got, err := s.GetJob(ctx, job.ID)
	if err != nil {
		t.Fatal(err)
	}
	want := ScratchPeaks{{Path: "/home/runner/_work", PeakMB: 300, SizeMB: 4096}, {Path: "/tmp", PeakMB: 962, SizeMB: 1024}}
	if len(got.ScratchPeaks) != 2 || got.ScratchPeaks[0] != want[0] || got.ScratchPeaks[1] != want[1] {
		t.Fatalf("scratch peaks = %+v, want %+v: the fullest each folder got, by path", got.ScratchPeaks, want)
	}
	fullest, fraction, ok := got.ScratchPeaks.Fullest()
	if !ok || fullest.Path != "/tmp" || fraction < 0.93 || fraction > 0.95 {
		t.Fatalf("fullest = %+v at %.2f, %v; want /tmp at about 0.94", fullest, fraction, ok)
	}

	// A sample after the job is over charges nothing to it.
	done := s.Now()
	if _, err := s.UpsertJob(ctx, &Job{GitHubJobID: 77, Repo: job.Repo, State: JobCompleted, Conclusion: "failure", StartedAt: &started, CompletedAt: &done}); err != nil {
		t.Fatal(err)
	}
	if err := s.RecordJobScratchPeaks(ctx, r.ID, []ScratchPeak{{Path: "/tmp", PeakMB: 1024, SizeMB: 1024}}); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.GetJob(ctx, job.ID); got.ScratchPeaks[1].PeakMB != 962 {
		t.Fatalf("scratch peaks = %+v after the job ended, want them untouched", got.ScratchPeaks)
	}
	if _, _, ok := (ScratchPeaks{}).Fullest(); ok {
		t.Fatal("no folders is no fullest folder")
	}
}

// A pool's performance recommendation reads what its recent jobs used, from the
// pool's own rows or, for a pool not yet made, from every pool's. A job that
// was never measured says nothing about size and is left out, and so is one
// from before the window.
func TestPoolJobPeaksAreOnePoolsMeasuredJobsSinceAMoment(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	n := 0
	record := func(pool string, mb int64, at time.Time) {
		n++
		runner := "run_" + strings.Repeat("y", n)
		usageJob(t, s, n, "Go (api)", pool, runner, JobInProgress, at)
		if mb > 0 {
			if err := s.RecordJobUsage(ctx, runner, 1.5, mb); err != nil {
				t.Fatal(err)
			}
		}
		done := at.Add(time.Hour)
		if _, _, err := s.ApplyJob(ctx, &Job{GitHubJobID: int64(5000 + n), State: JobCompleted, Conclusion: "success",
			Repo: "eyupio/zoomies", Workflow: "CI", JobName: "Go (api)", StartedAt: &at, CompletedAt: &done}); err != nil {
			t.Fatal(err)
		}
	}
	record("pool_a", 900, versionsEpoch)
	record("pool_a", 1100, versionsEpoch.Add(time.Hour))
	record("pool_a", 0, versionsEpoch.Add(2*time.Hour))      // never measured
	record("pool_a", 5000, versionsEpoch.Add(-48*time.Hour)) // before the window
	record("pool_b", 2000, versionsEpoch.Add(30*time.Minute))
	since := versionsEpoch.Add(-time.Hour)

	a, err := s.PoolJobPeaks(ctx, "pool_a", since)
	if err != nil {
		t.Fatalf("PoolJobPeaks: %v", err)
	}
	if len(a) != 2 || a[0].MemoryMB != 1100 || a[1].MemoryMB != 900 {
		t.Fatalf("pool_a = %+v, want its two measured jobs in the window, newest first", a)
	}
	all, err := s.PoolJobPeaks(ctx, "", since)
	if err != nil {
		t.Fatalf("PoolJobPeaks of every pool: %v", err)
	}
	if len(all) != 3 {
		t.Fatalf("every pool = %d jobs, want 3", len(all))
	}
}

// Both statements are read on demand over a table that grows without bound, so
// each must be served by an index and not walk it.
func TestPoolJobPeaksSeekAnIndex(t *testing.T) {
	s := newTestStore(t)
	for name, tc := range map[string]struct {
		sql  string
		args []any
		want string
	}{
		"one pool":   {poolJobPeaksSQL, []any{"pool_a", 0, 10}, "idx_jobs_pool"},
		"every pool": {allPoolJobPeaksSQL, []any{0, 10}, "idx_jobs_completed"},
	} {
		t.Run(name, func(t *testing.T) {
			rows, err := s.read.QueryContext(t.Context(), "EXPLAIN QUERY PLAN "+tc.sql, tc.args...)
			if err != nil {
				t.Fatalf("EXPLAIN QUERY PLAN: %v", err)
			}
			defer rows.Close()
			var plan []string
			for rows.Next() {
				var id, parent, notused int
				var detail string
				if err := rows.Scan(&id, &parent, &notused, &detail); err != nil {
					t.Fatal(err)
				}
				plan = append(plan, detail)
			}
			if joined := strings.Join(plan, "\n"); !strings.Contains(joined, tc.want) {
				t.Fatalf("the lookup does not use %s:\n%s", tc.want, joined)
			}
		})
	}
}
