package store

import (
	"context"
	"fmt"
	"testing"
	"time"
)

// The percentile method is nearest rank, and the report's figures are only as
// defensible as the claim that it is: these are the sets docs/metrics.md works
// through by hand, one odd and one even, plus the edges.
func TestNearestRankAnswersWithASampleThatWasObserved(t *testing.T) {
	s := func(secs ...int) []time.Duration {
		out := make([]time.Duration, len(secs))
		for i, x := range secs {
			out[i] = time.Duration(x) * time.Second
		}
		return out
	}
	for _, tc := range []struct {
		name     string
		sorted   []time.Duration
		p        float64
		wantSecs int
	}{
		{"one sample is every percentile", s(7), 50, 7},
		{"one sample is every percentile, p95", s(7), 95, 7},
		{"odd set p50 is the middle sample", s(2, 4, 10), 50, 4},
		{"odd set p95 is the largest", s(2, 4, 10), 95, 10},
		{"even set p50 is the lower middle, never an average", s(20, 30, 40, 50), 50, 30},
		{"even set p95 is the largest", s(20, 30, 40, 50), 95, 50},
		{"twenty samples p95 is the nineteenth", s(1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16, 17, 18, 19, 20), 95, 19},
		{"twenty-one samples p95 is the twentieth", s(1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16, 17, 18, 19, 20, 21), 95, 20},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := NearestRank(tc.sorted, tc.p); got != time.Duration(tc.wantSecs)*time.Second {
				t.Fatalf("p%v of %v = %v, want %ds", tc.p, tc.sorted, got, tc.wantSecs)
			}
		})
	}
}

// reportFleet is a small fleet whose every figure can be worked out by hand.
// The numbers the test asserts are that working, written next to the rows
// they come from.
type reportFleet struct {
	s      *Store
	now    time.Time
	inst   *Installation
	other  *Installation
	day    time.Time
	runner map[string]*Runner
}

func (f *reportFleet) set(t *testing.T, query string, args ...any) {
	t.Helper()
	if _, err := f.s.exec(context.Background(), query, args...); err != nil {
		t.Fatalf("%s: %v", query, err)
	}
}

func newReportFleet(t *testing.T) *reportFleet {
	t.Helper()
	ctx := context.Background()
	f := &reportFleet{day: time.Date(2026, 3, 10, 0, 0, 0, 0, time.UTC), runner: map[string]*Runner{}}
	f.now = f.day
	f.s = newTestStoreAt(t, func() time.Time { return f.now })
	inst, pool, host := seedPool(t, f.s)
	f.inst = inst
	f.other = &Installation{AppID: 1, InstallationID: 3, Target: "globex", TargetType: TargetOrg}
	if err := f.s.CreateInstallation(ctx, f.other); err != nil {
		t.Fatal(err)
	}
	otherPool := &Pool{Name: "globex", InstallationID: f.other.ID, Labels: StringSlice{"globex"}, Backend: BackendDocker,
		MaxRunners: 4, IdleTimeout: Duration(5 * time.Minute), Ephemeral: true, DockerMode: DockerNone, Enabled: true}
	if err := f.s.CreatePool(ctx, otherPool); err != nil {
		t.Fatal(err)
	}

	at := func(h, m, s int) time.Time {
		return f.day.Add(time.Duration(h)*time.Hour + time.Duration(m)*time.Minute + time.Duration(s)*time.Second)
	}
	ms := func(x time.Time) int64 { return x.UnixMilli() }

	// finish gives a runner exactly the create task, registration and finish
	// instants passed, and confirms its cleanup at cleaned unless that is zero.
	runner := func(name string, p *Pool, created time.Time) *Runner {
		f.now = created
		r := &Runner{PoolID: p.ID, HostID: host.ID, Name: name}
		if err := f.s.CreateRunner(ctx, r); err != nil {
			t.Fatal(err)
		}
		f.runner[name] = r
		return r
	}
	finish := func(r *Runner, issued, registered, finished, cleaned time.Time) {
		f.now = finished
		for _, st := range []RunnerState{RunnerRegistering, RunnerIdle, RunnerRemoved} {
			if _, err := f.s.TransitionRunner(ctx, r.ID, st, ""); err != nil {
				t.Fatalf("%s -> %s: %v", r.Name, st, err)
			}
		}
		f.set(t, `UPDATE runners SET create_task_issued_at=?, registered_at=?, finished_at=? WHERE id=?`,
			ms(issued), ms(registered), ms(finished), r.ID)
		if cleaned.IsZero() {
			return
		}
		f.now = cleaned
		for _, side := range []bool{true, false} {
			if _, err := f.s.ConfirmRunnerCleanup(ctx, r.ID, side); err != nil {
				t.Fatal(err)
			}
		}
	}
	job := func(id int64, inst *Installation, repo string, matched bool, state JobState, eligible time.Time, runner *Runner, elsewhere string) *Job {
		f.now = eligible
		j := &Job{GitHubJobID: id, Repo: repo, Workflow: "build", JobName: "test", State: state, Matched: matched,
			InstallationID: inst.ID, QueuedAt: eligible.Add(-time.Second)}
		if runner != nil {
			j.RunnerID, j.RunnerName, j.PoolID = runner.ID, runner.Name, runner.PoolID
		}
		if elsewhere != "" {
			j.RunnerName = elsewhere
		}
		if state != JobQueued && state != JobWaiting {
			started := eligible.Add(time.Minute)
			j.StartedAt = &started
		}
		if state == JobCompleted {
			completed := eligible.Add(20 * time.Minute)
			j.CompletedAt = &completed
		}
		out, err := f.s.UpsertJob(ctx, j)
		if err != nil {
			t.Fatal(err)
		}
		return out
	}

	// r4 is prewarmed: its create task went out at 09:00, an hour before the
	// job it ran became eligible, so j4 ran here but was not created for.
	r1 := runner("r1", pool, at(10, 0, 0))
	r2 := runner("r2", pool, at(11, 0, 0))
	r3 := runner("r3", pool, at(12, 0, 0))
	r4 := runner("r4", pool, at(9, 0, 0))
	g1 := runner("g1", otherPool, at(10, 0, 0))

	job(1, inst, "acme/widgets", true, JobCompleted, at(10, 0, 0), r1, "")
	job(2, inst, "acme/widgets", true, JobCompleted, at(11, 0, 0), r2, "")
	job(3, inst, "acme/secret-plans", true, JobCompleted, at(12, 0, 0), r3, "")
	f.set(t, `UPDATE jobs SET fault_kind='out_of_memory', runner_fault='killed' WHERE github_job_id=3`)
	job(4, inst, "acme/widgets", true, JobCompleted, at(13, 0, 0), r4, "")
	job(5, inst, "acme/widgets", true, JobCompleted, at(14, 0, 0), nil, "GitHub Actions 7")
	job(6, inst, "acme/widgets", false, JobCompleted, at(15, 0, 0), nil, "GitHub Actions 8")
	job(7, inst, "acme/widgets", true, JobWaiting, at(16, 0, 0), nil, "")
	job(8, f.other, "globex/rockets", true, JobCompleted, at(10, 0, 0), g1, "")

	// Scheduling: eligible -> create task is 4 s, 10 s, 2 s for r1..r3.
	// Registration: create task -> registered is 30 s, 50 s, 20 s, 40 s.
	// Cleanup: finish -> confirmed is 60 s, 90 s, never, 30 s.
	finish(r1, at(10, 0, 4), at(10, 0, 34), at(10, 30, 0), at(10, 31, 0))
	finish(r2, at(11, 0, 10), at(11, 1, 0), at(11, 30, 0), at(11, 31, 30))
	finish(r3, at(12, 0, 2), at(12, 0, 22), at(12, 30, 0), time.Time{})
	finish(r4, at(9, 0, 0), at(9, 0, 40), at(13, 30, 0), at(13, 30, 30))
	finish(g1, at(10, 0, 1), at(10, 0, 2), at(10, 30, 0), at(10, 30, 1))

	f.now = f.day.Add(20 * time.Hour)
	return f
}

// The hand computation, for installation acme over 10 March:
//
//	observed      7  jobs 1-7
//	eligible      5  jobs 1-5; 6 was unmatched and 7 is held for review
//	created for   3  jobs 1-3; job 4's runner was issued before it was eligible
//	ran here      4  jobs 1-4
//	ran elsewhere 2  jobs 5 and 6, on GitHub's runners
//	fleet fault   1  job 3
//	cleanup       1 pending (r3), 3 converged (r1, r2, r4)
//
// Globex's job and runner are in none of them.
var handCounts = InstallationCounts{Observed: 7, Eligible: 5, CreatedFor: 3, RanHere: 4, RanElsewhere: 2,
	FleetFault: 1, CleanupPending: 1, CleanupConverged: 3}

func secs(v float64) *float64 { return &v }

// Scheduling {2, 4, 10}: nearest rank p50 is rank 2 (4 s), p95 rank 3 (10 s).
// Registration {20, 30, 40, 50}: p50 rank 2 (30 s), p95 rank 4 (50 s).
// Cleanup {30, 60, 90}: p50 rank 2 (60 s), p95 rank 3 (90 s).
var handTimings = InstallationTimings{
	Scheduling:   Percentiles{Samples: 3, P50: secs(4), P95: secs(10)},
	Registration: Percentiles{Samples: 4, P50: secs(30), P95: secs(50)},
	Cleanup:      Percentiles{Samples: 3, P50: secs(60), P95: secs(90)},
}

func samePercentiles(a, b Percentiles) bool {
	eq := func(x, y *float64) bool { return (x == nil) == (y == nil) && (x == nil || *x == *y) }
	return a.Samples == b.Samples && eq(a.P50, b.P50) && eq(a.P95, b.P95)
}

func assertTimings(t *testing.T, got, want InstallationTimings) {
	t.Helper()
	for _, c := range []struct {
		name      string
		got, want Percentiles
	}{
		{"scheduling", got.Scheduling, want.Scheduling},
		{"registration", got.Registration, want.Registration},
		{"cleanup", got.Cleanup, want.Cleanup},
	} {
		if !samePercentiles(c.got, c.want) {
			t.Errorf("%s = %s, want %s", c.name, fmtP(c.got), fmtP(c.want))
		}
	}
}

func fmtP(p Percentiles) string {
	f := func(v *float64) string {
		if v == nil {
			return "nil"
		}
		return time.Duration(*v * float64(time.Second)).String()
	}
	return fmt.Sprintf("{n=%d p50=%s p95=%s}", p.Samples, f(p.P50), f(p.P95))
}

func (f *reportFleet) report(t *testing.T, id string, from, to time.Time) *InstallationReport {
	t.Helper()
	rep, err := f.s.InstallationReport(context.Background(), id, from, to)
	if err != nil {
		t.Fatalf("InstallationReport: %v", err)
	}
	return rep
}

// The report read straight from the rows matches the hand computation, count
// for count and percentile for percentile.
func TestTheInstallationReportMatchesAHandComputationFromTheRows(t *testing.T) {
	f := newReportFleet(t)
	rep := f.report(t, f.inst.ID, f.day.Add(3*time.Hour), f.day.Add(24*time.Hour))
	if !rep.From.Equal(f.day) {
		t.Errorf("from = %v, want the window moved back to its UTC midnight, %v", rep.From, f.day)
	}
	if rep.Counts != handCounts {
		t.Errorf("counts = %+v\nwant     %+v", rep.Counts, handCounts)
	}
	assertTimings(t, rep.Timings, handTimings)
	if !rep.CountsFrom.Equal(f.day.Add(9*time.Hour)) || !rep.TimingsFrom.Equal(f.day.Add(9*time.Hour)) {
		t.Errorf("counts from %v, timings from %v; want both at the oldest row, 09:00", rep.CountsFrom, rep.TimingsFrom)
	}
	if rep.RolledUntil != nil {
		t.Errorf("rolled until %v before anything was rolled up", rep.RolledUntil)
	}

	other := f.report(t, f.other.ID, f.day, f.day.Add(24*time.Hour))
	want := InstallationCounts{Observed: 1, Eligible: 1, CreatedFor: 1, RanHere: 1, CleanupConverged: 1}
	if other.Counts != want {
		t.Errorf("globex counts = %+v, want only its own job and runner, %+v", other.Counts, want)
	}
}

// Past the row retention the counts still answer, from the roll-up, and the
// timings -- which are read from the sessions and never rolled up -- say
// where they stop rather than turning into zeros or guesses.
func TestTheInstallationReportAnswersFromTheRollUpPastRetention(t *testing.T) {
	f := newReportFleet(t)
	ctx := context.Background()
	f.now = f.day.Add(40 * 24 * time.Hour)
	cutoff := f.now.Add(-30 * 24 * time.Hour)
	// In the order the controller's prune passes reach it: the runners prune
	// writes the session of r3, whose cleanup was never confirmed, and that is
	// what lets its day close; then the roll-ups; then the jobs, which are
	// held behind the roll-up. The sessions stay.
	if _, err := f.s.PruneRunners(ctx, cutoff); err != nil {
		t.Fatal(err)
	}
	if _, err := f.s.RollUpUsage(ctx, f.now); err != nil {
		t.Fatal(err)
	}
	n, err := f.s.RollUpInstallations(ctx, f.now, cutoff)
	if err != nil || n == 0 {
		t.Fatalf("rolled up %d days, %v; want the fleet's day and every one since", n, err)
	}
	// Every job goes, globex's too, and job 7 with them: it is still held
	// for review, but older than the cutoff, which is why the roll-up above
	// counted it as it stood rather than waiting for it.
	if n, err := f.s.PruneJobs(ctx, cutoff); err != nil || n != 8 {
		t.Fatalf("pruned %d jobs, %v; want all eight", n, err)
	}
	window := func() *InstallationReport { return f.report(t, f.inst.ID, f.day, f.day.Add(24*time.Hour)) }

	rep := window()
	if rep.Counts != handCounts {
		t.Errorf("with the rows pruned, counts = %+v\nwant %+v", rep.Counts, handCounts)
	}
	if rep.RolledUntil == nil || !rep.RolledUntil.Equal(f.day.Add(24*time.Hour)) {
		t.Errorf("rolled until %v, want the whole window from the roll-up", rep.RolledUntil)
	}
	// The sessions carried the create task and the job's eligibility past
	// both rows, so the timings are unchanged.
	assertTimings(t, rep.Timings, handTimings)

	// And past the sessions' retention too: the counts stand, the timings
	// are unavailable for the window, and the report says from when.
	if _, err := f.s.PruneRunnerSessions(ctx, cutoff); err != nil {
		t.Fatal(err)
	}
	rep = window()
	if rep.Counts != handCounts {
		t.Errorf("with the sessions pruned, counts = %+v\nwant %+v", rep.Counts, handCounts)
	}
	none := InstallationTimings{}
	assertTimings(t, rep.Timings, none)
	if !rep.TimingsFrom.Equal(rep.To) {
		t.Errorf("timings from %v, want the end of the window, %v: none of it is answerable", rep.TimingsFrom, rep.To)
	}
	// The runner rows went before the first roll-up, so the oldest row it
	// found was job 1, queued a second before 10:00.
	if want := f.day.Add(10*time.Hour - time.Second); !rep.CountsFrom.Equal(want) {
		t.Errorf("counts from %v, want where the first roll-up found its oldest row, %v", rep.CountsFrom, want)
	}
}

// A job that never completes must not hold the roll-up open for ever; one
// observed before the jobs prune cutoff is counted as it stands. A newer one
// does hold its day, because its outcome can still change.
func TestAnUnfinishedJobHoldsTheRollUpOnlyUntilItWouldBePruned(t *testing.T) {
	day := time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC)
	now := day.Add(time.Hour)
	s := newTestStoreAt(t, func() time.Time { return now })
	ctx := context.Background()
	inst, _, _ := seedPool(t, s)
	if _, err := s.UpsertJob(ctx, &Job{GitHubJobID: 1, Repo: "acme/app", State: JobQueued, Matched: true,
		InstallationID: inst.ID, QueuedAt: now}); err != nil {
		t.Fatal(err)
	}
	now = day.Add(5 * 24 * time.Hour)
	if n, err := s.RollUpInstallations(ctx, now, time.Time{}); err != nil || n != 0 {
		t.Fatalf("rolled up %d days, %v, past a queued job with no prune cutoff; want none", n, err)
	}
	if n, err := s.RollUpInstallations(ctx, now, day.Add(2*24*time.Hour)); err != nil || n != 5 {
		t.Fatalf("rolled up %d days, %v, once the job was older than the cutoff; want five", n, err)
	}
	counts, _, _, err := s.InstallationCountsBetween(ctx, day, now)
	if err != nil {
		t.Fatal(err)
	}
	if got := counts[inst.ID]; got.Observed != 1 || got.Eligible != 1 || got.RanHere != 0 {
		t.Fatalf("the unfinished job counted as %+v, want observed and eligible and nothing else", got)
	}
}

// A runner from before the create task was stamped has no end to its
// scheduling interval. Its job still ran here; it was not provably created
// for, and it gives no sample -- missing is not zero, and it must not stop
// the report answering at all.
func TestAJobWhoseRunnerHasNoCreateTaskRanHereButGivesNoSample(t *testing.T) {
	day := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)
	now := day.Add(time.Hour)
	s := newTestStoreAt(t, func() time.Time { return now })
	ctx := context.Background()
	inst, pool, host := seedPool(t, s)
	r := &Runner{PoolID: pool.ID, HostID: host.ID, Name: "old"}
	if err := s.CreateRunner(ctx, r); err != nil {
		t.Fatal(err)
	}
	if _, err := s.UpsertJob(ctx, &Job{GitHubJobID: 1, Repo: "acme/app", State: JobInProgress, Matched: true,
		InstallationID: inst.ID, QueuedAt: now, StartedAt: &now, RunnerID: r.ID, RunnerName: r.Name}); err != nil {
		t.Fatal(err)
	}
	rep, err := s.InstallationReport(ctx, inst.ID, day, day.Add(24*time.Hour))
	if err != nil {
		t.Fatalf("InstallationReport: %v", err)
	}
	if rep.Counts.RanHere != 1 || rep.Counts.CreatedFor != 0 || rep.Timings.Scheduling.Samples != 0 {
		t.Fatalf("counts %+v, scheduling %+v; want ran here, not created for, and no sample", rep.Counts, rep.Timings.Scheduling)
	}
}
