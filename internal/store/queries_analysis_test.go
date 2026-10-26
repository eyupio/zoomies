package store

import (
	"context"
	"database/sql"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"
)

// analysisStart is the start of the window these tests read; analysisEnd its
// end. Everything is placed on minutes inside or just outside it.
var (
	analysisStart = time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	analysisEnd   = analysisStart.Add(6 * time.Hour)
)

// analysisJob records job n queued at the given time on a pool and host, with
// whatever else edit sets.
func analysisJob(t *testing.T, s *Store, n int, pool, host string, queued time.Time, edit func(*Job)) *Job {
	t.Helper()
	j := &Job{
		GitHubJobID: int64(9000 + n), Repo: "acme/app", Workflow: "CI", JobName: "build",
		Labels: StringSlice{"self-hosted"}, State: JobQueued, QueuedAt: queued, Matched: true,
		PoolID: pool, HostID: host,
	}
	if edit != nil {
		edit(j)
	}
	out, _, err := s.ApplyJob(context.Background(), j)
	if err != nil {
		t.Fatalf("recording job %d: %v", n, err)
	}
	return out
}

// completed marks a job as having run from started to done.
func completed(started, done time.Time, conclusion string) func(*Job) {
	return func(j *Job) {
		j.State, j.StartedAt, j.CompletedAt, j.Conclusion = JobCompleted, &started, &done, conclusion
	}
}

func tp(t time.Time) *time.Time { return &t }

func sameTime(a, b *time.Time) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return a.Equal(*b)
}

func waitIDs(jobs []JobWait) []string {
	out := make([]string, len(jobs))
	for i, j := range jobs {
		out[i] = j.ID
	}
	return out
}

func readWaits(t *testing.T, s *Store, f JobWaitFilter, limit int) ([]JobWait, bool) {
	t.Helper()
	jobs, more, err := s.JobWaits(context.Background(), f, limit)
	if err != nil {
		t.Fatalf("JobWaits: %v", err)
	}
	return jobs, more
}

// A wait is split into phases by the first time each thing happened to the
// job: a deployment review held it, the review let it go, a runner for it
// failed to start. A webhook can be delivered twice, so the first is the one
// that counts, and a later repeat must not move the phase's start.
func TestJobWaitsCarriesTheHeldApprovedAndStartFailedTimesFromTheTimeline(t *testing.T) {
	s := newTestStoreAt(t, func() time.Time { return analysisStart })
	ctx := context.Background()
	q := analysisStart.Add(10 * time.Minute)
	held := analysisJob(t, s, 1, "pool_a", "host_x", q, func(j *Job) {
		completed(q.Add(12*time.Minute), q.Add(20*time.Minute), "failure")(j)
		j.EligibleAt, j.ControllerVersion, j.FaultKind = tp(q.Add(5*time.Minute)), "v1.3.3", FaultImage
		j.RunnerFault = "the runner's container exited"
		j.Repo, j.Workflow, j.JobName = "acme/web", "Deploy", "ship"
	})
	plain := analysisJob(t, s, 2, "pool_a", "host_x", q.Add(time.Minute), nil)
	// The kill is only ever raised by a sample, never written with the job.
	if _, err := s.exec(ctx, `UPDATE jobs SET oom_killed = 1 WHERE id = ?`, held.ID); err != nil {
		t.Fatal(err)
	}
	for _, e := range []struct {
		kind  JobEventKind
		after time.Duration
	}{
		{JobEventQueued, 0},
		{JobEventWaiting, time.Minute},
		{JobEventWaiting, 2 * time.Minute},
		{JobEventApproved, 5 * time.Minute},
		{JobEventRunnerStartFailed, 7 * time.Minute},
		{JobEventRunnerStartFailed, 9 * time.Minute},
		{JobEventStarted, 12 * time.Minute},
	} {
		if err := s.AppendJobEvent(ctx, &JobEvent{JobID: held.ID, Kind: e.kind, At: q.Add(e.after)}); err != nil {
			t.Fatalf("AppendJobEvent: %v", err)
		}
	}

	jobs, _ := readWaits(t, s, JobWaitFilter{Since: analysisStart, Until: analysisEnd}, 10)
	if got := waitIDs(jobs); !slices.Equal(got, []string{plain.ID, held.ID}) {
		t.Fatalf("jobs = %v, want the plain job then the held one", got)
	}
	h := jobs[1]
	if !sameTime(h.HeldAt, tp(q.Add(time.Minute))) || !sameTime(h.ApprovedAt, tp(q.Add(5*time.Minute))) ||
		!sameTime(h.FirstStartFailureAt, tp(q.Add(7*time.Minute))) {
		t.Errorf("held %v, approved %v, first start failure %v; want the first of each", h.HeldAt, h.ApprovedAt, h.FirstStartFailureAt)
	}
	if !h.QueuedAt.Equal(q) || !sameTime(h.EligibleAt, tp(q.Add(5*time.Minute))) ||
		!sameTime(h.StartedAt, tp(q.Add(12*time.Minute))) || !sameTime(h.CompletedAt, tp(q.Add(20*time.Minute))) {
		t.Errorf("job stamps = %+v", h)
	}
	if h.Repo != "acme/web" || h.Workflow != "Deploy" || h.JobName != "ship" || h.PoolID != "pool_a" || h.HostID != "host_x" ||
		h.Conclusion != "failure" || h.ControllerVersion != "v1.3.3" || h.FaultKind != FaultImage ||
		h.RunnerFault != "the runner's container exited" || !h.OOMKilled {
		t.Errorf("job facts = %+v", h)
	}
	p := jobs[0]
	if p.HeldAt != nil || p.ApprovedAt != nil || p.FirstStartFailureAt != nil || p.StartedAt != nil ||
		p.OOMKilled || p.RunnerFault != "" || p.FaultKind != "" {
		t.Errorf("a job with nothing on its timeline carries %+v", p)
	}
}

// A runner row is pruned after retention.runners, a week by default, and the
// session it leaves is kept for a year. Without the session, every job older
// than a week would have no startup phase, and the queue-wait breakdown could
// not tell a slow runner from GitHub's own delay.
func TestJobWaitsTakesTheRunnerTimesFromItsSessionWhenTheRowIsGone(t *testing.T) {
	now := analysisStart
	s := newTestStoreAt(t, func() time.Time { return now })
	ctx := context.Background()
	_, pool, host := seedPool(t, s)
	q := analysisStart.Add(10 * time.Minute)

	now = q.Add(time.Minute)
	issued := &Runner{PoolID: pool.ID, HostID: host.ID, Name: "issued", State: RunnerRemoved,
		CreateTaskIssuedAt: tp(q.Add(2 * time.Minute)), ContainerStartedAt: tp(q.Add(3 * time.Minute)),
		RegisteredAt: tp(q.Add(4 * time.Minute)), FinishedAt: tp(q.Add(30 * time.Minute))}
	if err := s.CreateRunner(ctx, issued); err != nil {
		t.Fatal(err)
	}
	// A runner from before the first delivery was stamped is requested when
	// its row was created.
	now = q.Add(5 * time.Minute)
	older := &Runner{PoolID: pool.ID, HostID: host.ID, Name: "older", State: RunnerRemoved,
		RegisteredAt: tp(q.Add(8 * time.Minute)), FinishedAt: tp(q.Add(40 * time.Minute))}
	if err := s.CreateRunner(ctx, older); err != nil {
		t.Fatal(err)
	}
	a := analysisJob(t, s, 1, pool.ID, host.ID, q, func(j *Job) { j.RunnerID = issued.ID })
	b := analysisJob(t, s, 2, pool.ID, host.ID, q.Add(-time.Minute), func(j *Job) { j.RunnerID = older.ID })

	check := func(when string, wantContainer *time.Time) {
		t.Helper()
		jobs, _ := readWaits(t, s, JobWaitFilter{Since: analysisStart, Until: analysisEnd}, 10)
		if got := waitIDs(jobs); !slices.Equal(got, []string{a.ID, b.ID}) {
			t.Fatalf("%s: jobs = %v", when, got)
		}
		ja, jb := jobs[0], jobs[1]
		if ja.RunnerID != issued.ID || !sameTime(ja.RunnerRequestedAt, tp(q.Add(2*time.Minute))) ||
			!sameTime(ja.RunnerRegisteredAt, tp(q.Add(4*time.Minute))) || !sameTime(ja.RunnerContainerStartedAt, wantContainer) {
			t.Errorf("%s: requested %v, container %v, registered %v; want %v, %v, %v", when,
				ja.RunnerRequestedAt, ja.RunnerContainerStartedAt, ja.RunnerRegisteredAt,
				q.Add(2*time.Minute), wantContainer, q.Add(4*time.Minute))
		}
		if !sameTime(jb.RunnerRequestedAt, tp(q.Add(5*time.Minute))) || !sameTime(jb.RunnerRegisteredAt, tp(q.Add(8*time.Minute))) {
			t.Errorf("%s: a runner with no delivery stamp: requested %v, registered %v; want its creation and registration",
				when, jb.RunnerRequestedAt, jb.RunnerRegisteredAt)
		}
	}
	check("with the rows", tp(q.Add(3*time.Minute)))

	now = q.Add(10 * 24 * time.Hour)
	if ids, err := s.PruneRunners(ctx, now.Add(-7*24*time.Hour)); err != nil || len(ids) != 2 {
		t.Fatalf("PruneRunners = %v, %v; want both runners gone", ids, err)
	}
	// The session keeps no container start (F10), so that phase is unknown
	// once the row is gone; the request and the registration survive.
	check("from the sessions", nil)
}

// With neither a row nor a session there is nothing to split the wait after
// eligibility with, and a guessed zero would read as "GitHub was slow".
func TestJobWaitsLeavesRunnerTimesNilWhenNeitherRowNorSessionRemains(t *testing.T) {
	now := analysisStart
	s := newTestStoreAt(t, func() time.Time { return now })
	ctx := context.Background()
	_, pool, host := seedPool(t, s)
	q := analysisStart.Add(10 * time.Minute)
	r := &Runner{PoolID: pool.ID, HostID: host.ID, Name: "gone", State: RunnerRemoved,
		CreateTaskIssuedAt: tp(q), RegisteredAt: tp(q.Add(time.Minute)), FinishedAt: tp(q.Add(20 * time.Minute))}
	if err := s.CreateRunner(ctx, r); err != nil {
		t.Fatal(err)
	}
	job := analysisJob(t, s, 1, pool.ID, host.ID, q, func(j *Job) { j.RunnerID = r.ID })
	now = q.Add(10 * 24 * time.Hour)
	if _, err := s.PruneRunners(ctx, now.Add(-7*24*time.Hour)); err != nil {
		t.Fatal(err)
	}
	if _, err := s.exec(ctx, `DELETE FROM runner_sessions WHERE runner_id = ?`, r.ID); err != nil {
		t.Fatal(err)
	}

	jobs, _ := readWaits(t, s, JobWaitFilter{Since: analysisStart, Until: analysisEnd}, 10)
	if len(jobs) != 1 || jobs[0].ID != job.ID {
		t.Fatalf("jobs = %v, want the one job", waitIDs(jobs))
	}
	j := jobs[0]
	if j.RunnerID != r.ID {
		t.Errorf("runner id = %q, want %q kept from the job", j.RunnerID, r.ID)
	}
	if j.RunnerRequestedAt != nil || j.RunnerContainerStartedAt != nil || j.RunnerRegisteredAt != nil {
		t.Errorf("runner times = %v, %v, %v; want all nil", j.RunnerRequestedAt, j.RunnerContainerStartedAt, j.RunnerRegisteredAt)
	}
}

// A busy period holds more jobs than one read takes. The newest are the ones
// kept, and the caller is told the rest exist, so a finding can say it read a
// sample rather than rank it as the whole period.
func TestJobWaitsReturnsTheNewestUpToTheLimitAndSaysWhenMoreRemained(t *testing.T) {
	s := newTestStoreAt(t, func() time.Time { return analysisStart })
	analysisJob(t, s, 100, "pool_a", "", analysisStart.Add(-time.Minute), nil)
	analysisJob(t, s, 101, "pool_a", "", analysisEnd, nil)
	var ids []string
	for i := range 7 {
		ids = append(ids, analysisJob(t, s, i, "pool_a", "", analysisStart.Add(time.Duration(i)*time.Minute), nil).ID)
	}
	slices.Reverse(ids)
	f := JobWaitFilter{Since: analysisStart, Until: analysisEnd}

	jobs, more := readWaits(t, s, f, 5)
	if got := waitIDs(jobs); !slices.Equal(got, ids[:5]) || !more {
		t.Errorf("limit 5: %v, more %v; want the five newest %v and more", got, more, ids[:5])
	}
	jobs, more = readWaits(t, s, f, 7)
	if got := waitIDs(jobs); !slices.Equal(got, ids) || more {
		t.Errorf("limit 7: %v, more %v; want all seven in the window and no more", got, more)
	}
	if _, _, err := s.JobWaits(context.Background(), f, 0); err == nil {
		t.Error("a limit of zero was accepted; an unbounded read must be refused")
	}
}

func TestJobWaitsNarrowsToAPoolOrAHost(t *testing.T) {
	s := newTestStoreAt(t, func() time.Time { return analysisStart })
	q := analysisStart.Add(time.Minute)
	ax := analysisJob(t, s, 1, "pool_a", "host_x", q, nil)
	ay := analysisJob(t, s, 2, "pool_a", "host_y", q.Add(time.Minute), nil)
	bx := analysisJob(t, s, 3, "pool_b", "host_x", q.Add(2*time.Minute), nil)
	for _, c := range []struct {
		pool, host string
		want       []string
	}{
		{"", "", []string{bx.ID, ay.ID, ax.ID}},
		{"pool_a", "", []string{ay.ID, ax.ID}},
		{"", "host_x", []string{bx.ID, ax.ID}},
		{"pool_a", "host_x", []string{ax.ID}},
	} {
		jobs, _ := readWaits(t, s, JobWaitFilter{Since: analysisStart, Until: analysisEnd, PoolID: c.pool, HostID: c.host}, 10)
		if got := waitIDs(jobs); !slices.Equal(got, c.want) {
			t.Errorf("pool %q host %q: %v, want %v", c.pool, c.host, got, c.want)
		}
	}
}

// GitHub reports every job in an installed repository, and a job whose labels
// all name another vendor's runners is that vendor's to run, never this fleet's
// (hosted.go). Read for the queue wait, such a job still queued at the end was
// "no pool claimed it" and one that started on the vendor's machine was time
// nothing here explains, so the read can be asked to keep to what JobFilter's
// ManagedOnly keeps: a job a pool claimed, one a runner here ran, and one
// nobody claims that nobody else hosts either.
func TestJobWaitsCanLeaveOutTheJobsAnotherVendorRuns(t *testing.T) {
	s := newTestStoreAt(t, func() time.Time { return analysisStart })
	q := analysisStart.Add(time.Minute)
	vendor := StringSlice{"blacksmith-4vcpu-ubuntu-2404"}
	unclaimed := func(labels StringSlice) func(*Job) {
		return func(j *Job) { j.Labels, j.Matched, j.PoolID = labels, false, "" }
	}
	claimed := analysisJob(t, s, 1, "pool_a", "", q, nil)
	// A pool may claim a label GitHub also offers: then it is this fleet's.
	claimedHosted := analysisJob(t, s, 2, "pool_a", "", q.Add(time.Minute), func(j *Job) { j.Labels = StringSlice{"ubuntu-latest"} })
	// Unclaimed by any pool, but a runner of this fleet ran it.
	ranHere := analysisJob(t, s, 3, "", "host_x", q.Add(2*time.Minute), func(j *Job) {
		unclaimed(vendor)(j)
		j.RunnerID = "run_here"
	})
	ours := analysisJob(t, s, 4, "", "", q.Add(3*time.Minute), unclaimed(StringSlice{"self-hosted", "linux", "gpu"}))
	analysisJob(t, s, 5, "", "", q.Add(4*time.Minute), unclaimed(vendor))
	analysisJob(t, s, 6, "", "", q.Add(5*time.Minute), func(j *Job) {
		unclaimed(StringSlice{"ubuntu-latest"})(j)
		completed(q.Add(5*time.Minute+20*time.Second), q.Add(10*time.Minute), "success")(j)
	})

	f := JobWaitFilter{Since: analysisStart, Until: analysisEnd}
	if all, _ := readWaits(t, s, f, 10); len(all) != 6 {
		t.Fatalf("an unnarrowed read holds %d jobs, want all 6", len(all))
	}
	f.ManagedOnly = true
	jobs, _ := readWaits(t, s, f, 10)
	want := []string{ours.ID, ranHere.ID, claimedHosted.ID, claimed.ID}
	if got := waitIDs(jobs); !slices.Equal(got, want) {
		t.Errorf("a managed read holds %v, want %v: the claimed, the one run here and the one nobody hosts", got, want)
	}
}

// A host's failures are placed when they happened. A job queued the evening
// before that failed this morning is this morning's failure, and a read by
// queue time would leave it out of the period it belongs to (P11).
func TestJobWaitsByActivityReadsJobsThatStartedOrCompletedInTheWindow(t *testing.T) {
	s := newTestStoreAt(t, func() time.Time { return analysisStart })
	before := analysisStart.Add(-10 * time.Hour)
	in := analysisStart.Add(time.Hour)
	after := analysisEnd.Add(time.Hour)
	failedInside := analysisJob(t, s, 1, "pool_a", "host_x", before, completed(before.Add(time.Minute), in, "failure"))
	startedInside := analysisJob(t, s, 2, "pool_a", "host_x", before.Add(time.Minute), completed(in, after, "success"))
	running := analysisJob(t, s, 3, "pool_a", "host_x", before.Add(2*time.Minute), func(j *Job) {
		j.State, j.StartedAt = JobInProgress, tp(in.Add(time.Minute))
	})
	whole := analysisJob(t, s, 4, "pool_a", "host_x", in, completed(in.Add(time.Minute), in.Add(2*time.Minute), "success"))
	analysisJob(t, s, 5, "pool_a", "host_x", in.Add(time.Minute), nil)                                                     // queued, nothing yet
	analysisJob(t, s, 6, "pool_a", "host_x", before, completed(before.Add(time.Minute), before.Add(time.Hour), "failure")) // all before
	analysisJob(t, s, 7, "pool_a", "host_x", analysisEnd, completed(after, after.Add(time.Minute), "success"))             // all after
	analysisJob(t, s, 8, "pool_a", "host_y", before.Add(3*time.Minute), completed(in, in.Add(time.Minute), "failure"))     // another host

	jobs, more := readWaits(t, s, JobWaitFilter{Since: analysisStart, Until: analysisEnd, HostID: "host_x", ByActivity: true}, 10)
	want := []string{whole.ID, running.ID, startedInside.ID, failedInside.ID}
	if got := waitIDs(jobs); !slices.Equal(got, want) || more {
		t.Errorf("by activity: %v, more %v; want %v", got, more, want)
	}
	jobs, more = readWaits(t, s, JobWaitFilter{Since: analysisStart, Until: analysisEnd, HostID: "host_x", ByActivity: true}, 3)
	if got := waitIDs(jobs); !slices.Equal(got, want[:3]) || !more {
		t.Errorf("by activity, limit 3: %v, more %v; want %v and more", got, more, want[:3])
	}
}

func spanIDs(spans []RunnerSpan) []string {
	out := make([]string, len(spans))
	for i, x := range spans {
		out[i] = x.RunnerID
	}
	return out
}

func readSpans(t *testing.T, s *Store, f RunnerSpanFilter, limit int) ([]RunnerSpan, bool) {
	t.Helper()
	spans, more, err := s.RunnerSpans(context.Background(), f, limit)
	if err != nil {
		t.Fatalf("RunnerSpans: %v", err)
	}
	return spans, more
}

// writeSession records a runner's session while its row stays, as a confirmed
// cleanup does.
func writeSession(t *testing.T, s *Store, id string) {
	t.Helper()
	if err := s.tx(context.Background(), func(tx *sql.Tx) error {
		return recordRunnerSession(context.Background(), tx, id, s.Now().UnixMilli())
	}); err != nil {
		t.Fatalf("recording session: %v", err)
	}
}

// A runner whose cleanup was confirmed has both a row and a session. Counted
// from both, the pool would look twice as full as it was and the ceiling
// would take the blame for waits it did not cause.
func TestRunnerSpansJoinsLiveRowsAndSessionsWithoutCountingARunnerTwice(t *testing.T) {
	now := analysisStart
	s := newTestStoreAt(t, func() time.Time { return now })
	ctx := context.Background()
	_, pool, host := seedPool(t, s)
	mk := func(name string, created time.Time, r Runner) *Runner {
		t.Helper()
		now = created
		r.PoolID, r.HostID, r.Name = pool.ID, host.ID, name
		if err := s.CreateRunner(ctx, &r); err != nil {
			t.Fatal(err)
		}
		return &r
	}
	// Ended before the window, and pruned to a session that also ended
	// before it: neither is read.
	old := mk("old", analysisStart.Add(-3*time.Hour), Runner{State: RunnerRemoved, FinishedAt: tp(analysisStart.Add(-2 * time.Hour))})
	// Alive across the start of the window and pruned: read from its session.
	pruned := mk("pruned", analysisStart.Add(-time.Hour), Runner{State: RunnerRemoved,
		CreateTaskIssuedAt: tp(analysisStart.Add(-59 * time.Minute)), RegisteredAt: tp(analysisStart.Add(-58 * time.Minute)),
		FinishedAt: tp(analysisStart.Add(30 * time.Minute))})
	// Cleaned up inside the window: a row and a session, read once, as the row.
	both := mk("both", analysisStart.Add(time.Hour), Runner{State: RunnerRemoved,
		ContainerStartedAt: tp(analysisStart.Add(61 * time.Minute)), RegisteredAt: tp(analysisStart.Add(62 * time.Minute)),
		FinishedAt: tp(analysisStart.Add(90 * time.Minute))})
	// Still idle at the end of the window.
	live := mk("live", analysisStart.Add(2*time.Hour), Runner{State: RunnerIdle, RegisteredAt: tp(analysisStart.Add(2*time.Hour + time.Minute))})
	// Created after the window.
	mk("later", analysisEnd, Runner{State: RunnerIdle})

	now = analysisStart.Add(91 * time.Minute)
	writeSession(t, s, both.ID)
	if _, err := s.PruneRunners(ctx, analysisStart.Add(31*time.Minute)); err != nil {
		t.Fatal(err)
	}
	var rows int
	if err := s.read.QueryRow(`SELECT COUNT(*) FROM runners WHERE id IN (?, ?)`, old.ID, pruned.ID).Scan(&rows); err != nil || rows != 0 {
		t.Fatalf("pruned rows left = %d, %v", rows, err)
	}

	spans, more := readSpans(t, s, RunnerSpanFilter{Since: analysisStart, Until: analysisEnd}, 10)
	if got, want := spanIDs(spans), []string{live.ID, both.ID, pruned.ID}; !slices.Equal(got, want) || more {
		t.Fatalf("spans = %v, more %v; want %v newest first", got, more, want)
	}
	if x := spans[1]; x.State != RunnerRemoved || !sameTime(x.ContainerStartedAt, tp(analysisStart.Add(61*time.Minute))) ||
		!sameTime(x.EndedAt, tp(analysisStart.Add(90*time.Minute))) || !x.RequestedAt.Equal(analysisStart.Add(time.Hour)) {
		t.Errorf("the cleaned-up runner was not read from its row: %+v", x)
	}
	if x := spans[2]; x.State != "" || x.PoolID != pool.ID || x.HostID != host.ID ||
		!x.RequestedAt.Equal(analysisStart.Add(-59*time.Minute)) || !sameTime(x.RegisteredAt, tp(analysisStart.Add(-58*time.Minute))) ||
		x.ContainerStartedAt != nil || !sameTime(x.EndedAt, tp(analysisStart.Add(30*time.Minute))) {
		t.Errorf("the pruned runner's session = %+v", x)
	}
	if x := spans[0]; x.State != RunnerIdle || x.EndedAt != nil || !sameTime(x.RegisteredAt, tp(analysisStart.Add(2*time.Hour+time.Minute))) {
		t.Errorf("the live runner = %+v, want idle with no end", x)
	}
}

// The startup analysis explains a failure by the fault the runner recorded
// when it failed, and quotes what the host said. Both live on the row, and a
// failure's end is when it is placed in time.
func TestRunnerSpansKeepsAFailedRunnersFaultAndMessage(t *testing.T) {
	now := analysisStart.Add(time.Hour)
	s := newTestStoreAt(t, func() time.Time { return now })
	ctx := context.Background()
	_, pool, host := seedPool(t, s)
	r := &Runner{PoolID: pool.ID, HostID: host.ID, Name: "doomed"}
	if err := s.CreateRunner(ctx, r); err != nil {
		t.Fatal(err)
	}
	now = analysisStart.Add(time.Hour + 2*time.Minute)
	if _, err := s.FailRunner(ctx, r.ID, "pull access denied for acme/runner", FaultImage); err != nil {
		t.Fatal(err)
	}
	spans, _ := readSpans(t, s, RunnerSpanFilter{Since: analysisStart, Until: analysisEnd}, 10)
	if len(spans) != 1 {
		t.Fatalf("spans = %v", spanIDs(spans))
	}
	x := spans[0]
	if x.State != RunnerFailed || x.FaultKind != FaultImage || x.Message != "pull access denied for acme/runner" {
		t.Errorf("failed runner = state %q, fault %q, message %q", x.State, x.FaultKind, x.Message)
	}
	if !sameTime(x.EndedAt, tp(now)) || x.RegisteredAt != nil || !x.RequestedAt.Equal(analysisStart.Add(time.Hour)) {
		t.Errorf("failed runner times: requested %v, registered %v, ended %v", x.RequestedAt, x.RegisteredAt, x.EndedAt)
	}
}

// A runner that is requested and fails in the same instant at the start of
// the window was still asked for in it; reading only lives that end after the
// start would lose exactly the fastest failures.
func TestRunnerSpansKeepsARunnerThatFailedTheMomentItWasAskedFor(t *testing.T) {
	now := analysisStart
	s := newTestStoreAt(t, func() time.Time { return now })
	ctx := context.Background()
	_, pool, host := seedPool(t, s)
	r := &Runner{PoolID: pool.ID, HostID: host.ID, Name: "instant", State: RunnerFailed, FinishedAt: tp(analysisStart)}
	if err := s.CreateRunner(ctx, r); err != nil {
		t.Fatal(err)
	}
	spans, _ := readSpans(t, s, RunnerSpanFilter{Since: analysisStart, Until: analysisEnd}, 10)
	if got := spanIDs(spans); !slices.Equal(got, []string{r.ID}) {
		t.Errorf("spans = %v, want the instant failure", got)
	}
}

// A runner that ended exactly as the window opened is read the same way from
// its row and from its session. Were the two halves to draw the line on
// different sides, the same history would give one answer this week and
// another once retention.runners had pruned the row.
func TestRunnerSpansReadsARunnerEndingAtTheWindowsStartTheSameFromItsRowAndItsSession(t *testing.T) {
	now := analysisStart.Add(-time.Hour)
	s := newTestStoreAt(t, func() time.Time { return now })
	ctx := context.Background()
	_, pool, host := seedPool(t, s)
	r := &Runner{PoolID: pool.ID, HostID: host.ID, Name: "edge", State: RunnerRemoved, FinishedAt: tp(analysisStart)}
	if err := s.CreateRunner(ctx, r); err != nil {
		t.Fatal(err)
	}
	f := RunnerSpanFilter{Since: analysisStart, Until: analysisEnd}
	spans, _ := readSpans(t, s, f, 10)
	if got := spanIDs(spans); !slices.Equal(got, []string{r.ID}) || spans[0].State != RunnerRemoved {
		t.Errorf("from the row: %v, want the runner that ended at the start", got)
	}
	now = analysisEnd
	if ids, err := s.PruneRunners(ctx, analysisStart.Add(time.Minute)); err != nil || len(ids) != 1 {
		t.Fatalf("PruneRunners = %v, %v", ids, err)
	}
	spans, _ = readSpans(t, s, f, 10)
	if got := spanIDs(spans); !slices.Equal(got, []string{r.ID}) || spans[0].State != "" {
		t.Errorf("from the session: %v, want the same runner", got)
	}
}

func TestRunnerSpansReturnsUpToTheLimitAndSaysWhenMoreRemained(t *testing.T) {
	now := analysisStart
	s := newTestStoreAt(t, func() time.Time { return now })
	ctx := context.Background()
	_, pool, host := seedPool(t, s)
	var ids []string
	for i := range 7 {
		now = analysisStart.Add(time.Duration(i) * time.Minute)
		r := &Runner{PoolID: pool.ID, HostID: host.ID, Name: "r" + string(rune('a'+i)), State: RunnerIdle}
		if err := s.CreateRunner(ctx, r); err != nil {
			t.Fatal(err)
		}
		ids = append(ids, r.ID)
		// Every other one is pruned, so the cut runs across rows and
		// sessions alike.
		if i%2 == 0 {
			if _, err := s.exec(ctx, `UPDATE runners SET state = 'removed', finished_at = ? WHERE id = ?`, ms(analysisEnd), r.ID); err != nil {
				t.Fatal(err)
			}
		}
	}
	now = analysisEnd.Add(time.Hour)
	if ids, err := s.PruneRunners(ctx, analysisEnd.Add(time.Minute)); err != nil || len(ids) != 4 {
		t.Fatalf("PruneRunners = %v, %v", ids, err)
	}
	slices.Reverse(ids)
	f := RunnerSpanFilter{Since: analysisStart, Until: analysisEnd}
	spans, more := readSpans(t, s, f, 5)
	if got := spanIDs(spans); !slices.Equal(got, ids[:5]) || !more {
		t.Errorf("limit 5: %v, more %v; want the five newest %v and more", got, more, ids[:5])
	}
	spans, more = readSpans(t, s, f, 7)
	if got := spanIDs(spans); !slices.Equal(got, ids) || more {
		t.Errorf("limit 7: %v, more %v; want all seven and no more", got, more)
	}
	if _, _, err := s.RunnerSpans(ctx, f, 0); err == nil {
		t.Error("a limit of zero was accepted")
	}
}

// A narrowed analysis must read its own pool's runners up to the limit. Read
// fleet-wide and filtered afterwards, a busy pool elsewhere could fill the
// read and leave the pool asked about truncated away (P15).
func TestRunnerSpansNarrowsToAPoolOrAHost(t *testing.T) {
	now := analysisStart
	s := newTestStoreAt(t, func() time.Time { return now })
	ctx := context.Background()
	inst, poolA, hostX := seedPool(t, s)
	poolB := &Pool{Name: "linux-arm64", InstallationID: inst.ID, Labels: StringSlice{"linux-arm64"},
		Backend: BackendDocker, MaxRunners: 4, Ephemeral: true, DockerMode: DockerNone, Enabled: true}
	if err := s.CreatePool(ctx, poolB); err != nil {
		t.Fatal(err)
	}
	hostY := &Host{Name: "vm-2", Capacity: 4, Backends: StringSlice{"docker"}}
	if err := s.CreateHost(ctx, hostY); err != nil {
		t.Fatal(err)
	}
	mk := func(pool, host, name string, prune bool) string {
		t.Helper()
		now = now.Add(time.Minute)
		r := &Runner{PoolID: pool, HostID: host, Name: name, State: RunnerIdle}
		if prune {
			r.State, r.FinishedAt = RunnerRemoved, tp(now.Add(time.Minute))
		}
		if err := s.CreateRunner(ctx, r); err != nil {
			t.Fatal(err)
		}
		return r.ID
	}
	ax := mk(poolA.ID, hostX.ID, "ax", false)
	ay := mk(poolA.ID, hostY.ID, "ay", true)
	bx := mk(poolB.ID, hostX.ID, "bx", true)
	by := mk(poolB.ID, hostY.ID, "by", false)
	if _, err := s.PruneRunners(ctx, analysisEnd); err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		pool, host string
		want       []string
	}{
		{"", "", []string{by, bx, ay, ax}},
		{poolA.ID, "", []string{ay, ax}},
		{"", hostX.ID, []string{bx, ax}},
		{poolB.ID, hostY.ID, []string{by}},
	} {
		spans, _ := readSpans(t, s, RunnerSpanFilter{Since: analysisStart, Until: analysisEnd, PoolID: c.pool, HostID: c.host}, 10)
		if got := spanIDs(spans); !slices.Equal(got, c.want) {
			t.Errorf("pool %q host %q: %v, want %v", c.pool, c.host, got, c.want)
		}
	}
}

func TestCapacityBlockedMinutesListsOnlyBlockedMinutesInTheWindow(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	m := func(i int) time.Time { return analysisStart.Add(time.Duration(i) * time.Minute) }
	for _, x := range []struct {
		pool    string
		minute  int
		blocked bool
	}{
		{"pool_a", -1, true}, // before the window
		{"pool_a", 0, true},
		{"pool_a", 1, false},
		{"pool_a", 2, true},
		{"pool_a", 360, true}, // the window's end, which is out
		{"pool_b", 3, true},
		{"pool_c", 4, false},
	} {
		if err := s.RecordUsageCapacity(ctx, x.pool, m(x.minute), x.blocked); err != nil {
			t.Fatal(err)
		}
	}
	got, more, err := s.CapacityBlockedMinutes(ctx, analysisStart, analysisEnd, "", 10)
	if err != nil || more {
		t.Fatalf("CapacityBlockedMinutes: %v, more %v", err, more)
	}
	if len(got) != 2 || !slices.EqualFunc(got["pool_a"], []time.Time{m(0), m(2)}, time.Time.Equal) ||
		!slices.EqualFunc(got["pool_b"], []time.Time{m(3)}, time.Time.Equal) {
		t.Errorf("blocked minutes = %v, want pool_a at 0 and 2, pool_b at 3, oldest first", got)
	}
	one, _, err := s.CapacityBlockedMinutes(ctx, analysisStart, analysisEnd, "pool_b", 10)
	if err != nil || len(one) != 1 || len(one["pool_b"]) != 1 {
		t.Errorf("narrowed to pool_b: %v, %v", one, err)
	}
}

func TestCapacityBlockedMinutesReturnsTheNewestUpToTheLimitAndSaysWhenMoreRemained(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	m := func(i int) time.Time { return analysisStart.Add(time.Duration(i) * time.Minute) }
	for i := range 7 {
		if err := s.RecordUsageCapacity(ctx, "pool_a", m(i), true); err != nil {
			t.Fatal(err)
		}
	}
	got, more, err := s.CapacityBlockedMinutes(ctx, analysisStart, analysisEnd, "pool_a", 5)
	if err != nil || !more || !slices.EqualFunc(got["pool_a"], []time.Time{m(2), m(3), m(4), m(5), m(6)}, time.Time.Equal) {
		t.Errorf("limit 5: %v, more %v, %v; want minutes 2 to 6 and more", got, more, err)
	}
	if _, more, _ := s.CapacityBlockedMinutes(ctx, analysisStart, analysisEnd, "pool_a", 7); more {
		t.Error("limit 7 over seven minutes says more remained")
	}
	if _, _, err := s.CapacityBlockedMinutes(ctx, analysisStart, analysisEnd, "", 0); err == nil {
		t.Error("a limit of zero was accepted")
	}
}

// A comparison picks its two releases from what ran in the window. A job
// recorded before releases were stamped belongs to none of them, and is named
// unknown so it can be left out by name rather than silently.
func TestJobVersionSpansGroupsUnstampedJobsAsUnknown(t *testing.T) {
	s := newTestStoreAt(t, func() time.Time { return analysisStart })
	ctx := context.Background()
	q := func(i int) time.Time { return analysisStart.Add(time.Duration(i) * time.Minute) }
	done := func(v string, conclusion string) func(*Job) {
		return func(j *Job) {
			completed(j.QueuedAt.Add(time.Minute), j.QueuedAt.Add(2*time.Minute), conclusion)(j)
			j.ControllerVersion = v
		}
	}
	analysisJob(t, s, 1, "pool_a", "", q(0), done("", "success"))
	analysisJob(t, s, 2, "pool_a", "", q(1), done("", "failure"))
	analysisJob(t, s, 3, "pool_a", "", q(2), done("v1.3.2", "success"))
	analysisJob(t, s, 4, "pool_a", "", q(3), done("v1.3.2", "cancelled"))
	analysisJob(t, s, 5, "pool_a", "", q(4), done("v1.3.3", "success"))
	analysisJob(t, s, 6, "pool_a", "", q(5), done("v1.3.3", "skipped"))
	analysisJob(t, s, 7, "pool_a", "", q(6), func(j *Job) { j.ControllerVersion = "v1.3.3" }) // still queued
	analysisJob(t, s, 8, "pool_a", "", analysisEnd, done("v1.3.4", "success"))                // outside

	spans, more, err := s.JobVersionSpans(ctx, analysisStart, analysisEnd, nil, 10)
	if err != nil || more {
		t.Fatalf("JobVersionSpans: %v, more %v", err, more)
	}
	want := []VersionSpan{
		{Version: "v1.3.3", First: q(4), Last: q(5), Jobs: 2},
		{Version: "v1.3.2", First: q(2), Last: q(3), Jobs: 2},
		{Version: UnknownVersion, First: q(0), Last: q(1), Jobs: 2},
	}
	if !slices.EqualFunc(spans, want, sameSpan) {
		t.Errorf("spans = %+v, want %+v (latest first)", spans, want)
	}

	spans, _, err = s.JobVersionSpans(ctx, analysisStart, analysisEnd, []string{"cancelled", "skipped"}, 10)
	if err != nil {
		t.Fatal(err)
	}
	want = []VersionSpan{
		{Version: "v1.3.3", First: q(4), Last: q(4), Jobs: 1},
		{Version: "v1.3.2", First: q(2), Last: q(2), Jobs: 1},
		{Version: UnknownVersion, First: q(0), Last: q(1), Jobs: 2},
	}
	if !slices.EqualFunc(spans, want, sameSpan) {
		t.Errorf("leaving out cancelled and skipped: %+v, want %+v", spans, want)
	}

	spans, more, err = s.JobVersionSpans(ctx, analysisStart, analysisEnd, nil, 2)
	if err != nil || !more || len(spans) != 2 || spans[0].Version != "v1.3.3" || spans[1].Version != "v1.3.2" {
		t.Errorf("limit 2: %+v, more %v, %v; want the two latest and more", spans, more, err)
	}
	if _, _, err := s.JobVersionSpans(ctx, analysisStart, analysisEnd, nil, 0); err == nil {
		t.Error("a limit of zero was accepted")
	}
}

func sameSpan(a, b VersionSpan) bool {
	return a.Version == b.Version && a.First.Equal(b.First) && a.Last.Equal(b.Last) && a.Jobs == b.Jobs
}

// The comparison sets its sides against the fleet's size. The average is
// worked out in SQL: read into Go it would be every host's every minute of a
// month for one number (F7).
func TestMeanFleetCapacityAveragesTheSlotsOnOfferOverTheSampledMinutes(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	m := func(i int) time.Time { return analysisStart.Add(time.Duration(i) * time.Minute) }
	if got, err := s.MeanFleetCapacity(ctx, analysisStart, analysisEnd); err != nil || got != nil {
		t.Fatalf("with no samples: %v, %v; want nil, which is not zero slots", got, err)
	}
	if err := s.RecordHostSamples(ctx, []HostSample{
		{HostID: "host_x", At: m(-1), Capacity: 100}, // before the window
		{HostID: "host_x", At: m(0), Capacity: 4},
		{HostID: "host_y", At: m(0), Capacity: 2},
		{HostID: "host_x", At: m(1), Capacity: 4},
		{HostID: "host_x", At: m(360), Capacity: 100}, // the window's end
	}); err != nil {
		t.Fatal(err)
	}
	got, err := s.MeanFleetCapacity(ctx, analysisStart, analysisEnd)
	if err != nil || got == nil || *got != 5 {
		t.Errorf("mean = %v, %v; want 5, the mean of 6 and 4 slots", got, err)
	}
}

// Host pressure reads one host's minutes over a window that can be a month;
// the read stops at the window's end and at the limit, and keeps the newest.
func TestHostSamplesBetweenReadsOneHostsWindowUpToTheLimit(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	m := func(i int) time.Time { return analysisStart.Add(time.Duration(i) * time.Minute) }
	load := 3.5
	var batch []HostSample
	for _, i := range []int{-1, 0, 1, 2, 3, 360} {
		batch = append(batch, HostSample{HostID: "host_x", At: m(i), Capacity: 4, CPUs: 8, LoadAverage1: &load, MemoryMB: 16384})
	}
	batch = append(batch, HostSample{HostID: "host_y", At: m(1), Capacity: 2})
	if err := s.RecordHostSamples(ctx, batch); err != nil {
		t.Fatal(err)
	}
	at := func(xs []HostSample) []time.Time {
		out := make([]time.Time, len(xs))
		for i, x := range xs {
			out[i] = x.At
		}
		return out
	}
	got, more, err := s.HostSamplesBetween(ctx, "host_x", analysisStart, analysisEnd, 10)
	if err != nil || more || !slices.EqualFunc(at(got), []time.Time{m(0), m(1), m(2), m(3)}, time.Time.Equal) {
		t.Fatalf("host_x: %v, more %v, %v; want minutes 0 to 3 oldest first", at(got), more, err)
	}
	if x := got[0]; x.HostID != "host_x" || x.CPUs != 8 || x.MemoryMB != 16384 || x.LoadAverage1 == nil || *x.LoadAverage1 != 3.5 ||
		x.MemoryAvailableMB != nil {
		t.Errorf("sample = %+v", x)
	}
	got, more, err = s.HostSamplesBetween(ctx, "host_x", analysisStart, analysisEnd, 3)
	if err != nil || !more || !slices.EqualFunc(at(got), []time.Time{m(1), m(2), m(3)}, time.Time.Equal) {
		t.Errorf("limit 3: %v, more %v, %v; want the three newest and more", at(got), more, err)
	}
	if _, _, err := s.HostSamplesBetween(ctx, "host_x", analysisStart, analysisEnd, 0); err == nil {
		t.Error("a limit of zero was accepted")
	}
}

// A throttle that went on before the window and came off after it leaves no
// row inside the window, and by the time anyone asks about a past window the
// host's own Throttle.Since has long been cleared. The last change before the
// window is the only record that the host spent the whole window on a rung.
func TestHostThrottleChangesCarryEachHostsLastChangeBeforeTheWindow(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	m := func(i int) time.Time { return analysisStart.Add(time.Duration(i) * time.Minute) }
	for _, e := range []AuditEvent{
		{Action: "host.throttle", TargetKind: "host", TargetID: "host_x", After: `{"level":1}`, CreatedAt: m(-90)},
		{Action: "host.throttle", TargetKind: "host", TargetID: "host_x", After: `{"level":2}`, CreatedAt: m(-60)},
		{Action: "host.throttle_clear", TargetKind: "host", TargetID: "host_x", After: `{"level":2}`, CreatedAt: m(400)},
		{Action: "host.throttle", TargetKind: "host", TargetID: "host_y", After: `{"level":1}`, CreatedAt: m(-30)},
		{Action: "host.throttle_lift", TargetKind: "host", TargetID: "host_y", After: `{"level":0}`, CreatedAt: m(-20)},
		{Action: "host.update", TargetKind: "host", TargetID: "host_z", After: `{"level":3}`, CreatedAt: m(-10)},
		{Action: "host.throttle", TargetKind: "host", TargetID: "host_y", After: `{"level":1}`, CreatedAt: m(30)},
		{Action: "host.throttle", TargetKind: "host", TargetID: "host_y", After: `{"level":2}`, CreatedAt: m(40)},
	} {
		if err := s.AppendAudit(ctx, &e); err != nil {
			t.Fatal(err)
		}
	}
	same := func(a, b ThrottleChange) bool {
		return a.HostID == b.HostID && a.Action == b.Action && a.Level == b.Level && a.At.Equal(b.At)
	}
	got, more, err := s.HostThrottleChanges(ctx, analysisStart, analysisEnd, 10)
	want := []ThrottleChange{
		{HostID: "host_x", Action: "host.throttle", Level: 2, At: m(-60)},
		{HostID: "host_y", Action: "host.throttle_lift", Level: 0, At: m(-20)},
		{HostID: "host_y", Action: "host.throttle", Level: 1, At: m(30)},
		{HostID: "host_y", Action: "host.throttle", Level: 2, At: m(40)},
	}
	if err != nil || more || !slices.EqualFunc(got, want, same) {
		t.Errorf("changes = %+v, more %v, %v; want each host's last change before the window, then the window's", got, more, err)
	}
	// The limit cuts the window's oldest rows, never the starting rungs: they
	// are at most one a host, and without them a cut read would lose the
	// throttles that were on all along.
	got, more, err = s.HostThrottleChanges(ctx, analysisStart, analysisEnd, 1)
	if err != nil || !more || !slices.EqualFunc(got, []ThrottleChange{want[0], want[1], want[3]}, same) {
		t.Errorf("limit 1: %+v, more %v, %v; want both starting rungs and the newest change", got, more, err)
	}
}

// Throttle episodes are read from the audit log, which also names who cleared
// one. The analyses expose when a host was throttled and nothing about who
// (F14), so the read returns the host, the time and the rung, and no actor or
// detail.
func TestHostThrottleChangesReadOnlyTheThrottleRowsAndTheRungEachLeft(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	m := func(i int) time.Time { return analysisStart.Add(time.Duration(i) * time.Minute) }
	for _, e := range []AuditEvent{
		{Action: "host.throttle", TargetKind: "host", TargetID: "host_x", After: `{"level":1}`, CreatedAt: m(-1)}, // its starting rung
		{Action: "host.throttle", TargetKind: "host", TargetID: "host_x", After: `{"level":2,"reason":"load"}`, CreatedAt: m(0)},
		{Action: "host.throttle_lift", TargetKind: "host", TargetID: "host_x", After: `{"level":1}`, CreatedAt: m(5)},
		// A clear's detail names the rung that was cleared; the host is on none after it.
		{Action: "host.throttle_clear", TargetKind: "host", TargetID: "host_x", ActorName: "Ada", After: `{"level":1,"name":"vm-1"}`, CreatedAt: m(9)},
		{Action: "host.throttle", TargetKind: "host", TargetID: "host_y", After: `{"level":1}`, CreatedAt: m(12)},
		{Action: "host.update", TargetKind: "host", TargetID: "host_x", After: `{"level":3}`, CreatedAt: m(13)},
		{Action: "host.throttle", TargetKind: "host", TargetID: "host_x", After: ``, CreatedAt: m(14)},
		{Action: "host.throttle", TargetKind: "host", TargetID: "host_x", After: `{"level":1}`, CreatedAt: m(360)},
	} {
		if err := s.AppendAudit(ctx, &e); err != nil {
			t.Fatal(err)
		}
	}
	got, more, err := s.HostThrottleChanges(ctx, analysisStart, analysisEnd, 10)
	if err != nil || more {
		t.Fatalf("HostThrottleChanges: %v, more %v", err, more)
	}
	want := []ThrottleChange{
		{HostID: "host_x", Action: "host.throttle", Level: 1, At: m(-1)},
		{HostID: "host_x", Action: "host.throttle", Level: 2, At: m(0)},
		{HostID: "host_x", Action: "host.throttle_lift", Level: 1, At: m(5)},
		{HostID: "host_x", Action: "host.throttle_clear", Level: 0, At: m(9)},
		{HostID: "host_y", Action: "host.throttle", Level: 1, At: m(12)},
		{HostID: "host_x", Action: "host.throttle", Level: 0, At: m(14)},
	}
	same := func(a, b ThrottleChange) bool {
		return a.HostID == b.HostID && a.Action == b.Action && a.Level == b.Level && a.At.Equal(b.At)
	}
	if !slices.EqualFunc(got, want, same) {
		t.Errorf("changes = %+v, want %+v oldest first", got, want)
	}
	got, more, err = s.HostThrottleChanges(ctx, analysisStart, analysisEnd, 2)
	if err != nil || !more || !slices.EqualFunc(got, append([]ThrottleChange{want[0]}, want[4:]...), same) {
		t.Errorf("limit 2: %+v, more %v, %v; want the starting rung, the two newest and more", got, more, err)
	}
	if _, _, err := s.HostThrottleChanges(ctx, analysisStart, analysisEnd, 0); err == nil {
		t.Error("a limit of zero was accepted")
	}
}

// A comparison leaves cancelled and skipped jobs out (spec 5.2): they stop
// when somebody says so, and a release whose users cancelled more would look
// faster and more reliable for it. The exclusion has to reach every figure,
// the counts and the queue wait as well as the duration (F1).
func TestJobStatsLeavesOutTheConclusionsItIsAskedTo(t *testing.T) {
	s := newTestStoreAt(t, func() time.Time { return analysisStart })
	ctx := context.Background()
	q := analysisStart.Add(time.Minute)
	analysisJob(t, s, 1, "pool_a", "", q, completed(q.Add(time.Minute), q.Add(2*time.Minute), "success"))
	analysisJob(t, s, 2, "pool_a", "", q, completed(q.Add(time.Minute), q.Add(2*time.Minute), "failure"))
	analysisJob(t, s, 3, "pool_a", "", q, completed(q.Add(5*time.Hour), q.Add(5*time.Hour), "cancelled"))
	analysisJob(t, s, 4, "pool_a", "", q, completed(q.Add(5*time.Hour), q.Add(5*time.Hour), "skipped"))
	f := JobFilter{Since: &analysisStart, Until: &analysisEnd, ExcludeConclusions: []string{"cancelled", "skipped"}}

	res, err := s.JobStats(ctx, f, nil)
	if err != nil || len(res.Groups) != 1 {
		t.Fatalf("JobStats: %+v, %v", res, err)
	}
	g := res.Groups[0]
	if g.Count != 2 || g.Succeeded != 1 || g.Failed != 1 || g.Cancelled != 0 {
		t.Errorf("counts = %d (%d ok, %d failed, %d cancelled), want 2, 1, 1, 0", g.Count, g.Succeeded, g.Failed, g.Cancelled)
	}
	if g.QueueWait.Samples != 2 || g.QueueWait.P95MS == nil || *g.QueueWait.P95MS != time.Minute.Milliseconds() {
		t.Errorf("queue wait = %+v, want two one-minute waits", g.QueueWait)
	}
	jobs, total, err := s.ListJobs(ctx, f, Page{})
	if err != nil || total != 2 || len(jobs) != 2 {
		t.Errorf("ListJobs = %d of %d, %v; want the two that were not cancelled or skipped", len(jobs), total, err)
	}
}

// assertSearches fails when a plan walks a table the analyses read rather
// than seeking an index on it. These tables grow with the fleet's history,
// and a read that scans one costs the size of everything kept, not of the
// window asked about.
func assertSearches(t *testing.T, name, plan string) {
	t.Helper()
	for _, step := range strings.Split(plan, "; ") {
		if !strings.HasPrefix(step, "SCAN ") {
			continue
		}
		table := strings.Fields(step)[1]
		switch table {
		case "j", "r", "rs", "x", "e", "jobs", "runners", "runner_sessions", "job_events",
			"usage_capacity_samples", "host_samples", "audit_events":
			t.Errorf("%s walks %s: %s", name, table, plan)
		}
	}
}

// Each read names the index that bounds it by the window. "Some index" is not
// enough: the audit log's target index is every host's audit trail since the
// fleet began, and the sessions' start index is a year of them, and a read on
// either would pass a test that only looked for USING.
func TestTheAnalysisReadsSeekTheirIndexes(t *testing.T) {
	s := newTestStore(t)
	type read struct {
		name, query string
		args        []any
		want        []string
	}
	var reads []read
	for _, c := range []struct {
		f    JobWaitFilter
		want []string
	}{
		{JobWaitFilter{}, []string{"idx_jobs_queued_id"}},
		{JobWaitFilter{PoolID: "pool_a"}, []string{"idx_jobs_pool"}},
		{JobWaitFilter{ManagedOnly: true}, []string{"idx_jobs_queued_id"}},
		{JobWaitFilter{PoolID: "pool_a", ManagedOnly: true}, []string{"idx_jobs_pool"}},
		{JobWaitFilter{HostID: "host_x"}, []string{"idx_jobs_queued_id"}},
		{JobWaitFilter{HostID: "host_x", ByActivity: true}, []string{"MULTI-INDEX OR", "idx_jobs_completed (state=? AND completed_at>? AND completed_at<?)"}},
	} {
		c.f.Since, c.f.Until = analysisStart, analysisEnd
		q, args := jobWaitsQuery(c.f, 10)
		reads = append(reads, read{fmt.Sprintf("JobWaits %+v", c.f), q, args, append(c.want, "idx_job_events_job")})
	}
	for _, c := range []struct {
		f    RunnerSpanFilter
		want string
	}{
		{RunnerSpanFilter{}, "idx_runners_created"},
		{RunnerSpanFilter{PoolID: "pool_a"}, "(pool_id=? AND created_at<?)"},
		{RunnerSpanFilter{HostID: "host_x"}, "idx_runners_host_state"},
	} {
		c.f.Since, c.f.Until = analysisStart, analysisEnd
		q, args := runnerSpansQuery(c.f, 10)
		reads = append(reads, read{fmt.Sprintf("RunnerSpans %+v", c.f), q, args, []string{c.want, "idx_runner_sessions_finished (finished_at>?)"}})
	}
	q, args := capacityBlockedQuery(analysisStart, analysisEnd, "", 10)
	reads = append(reads, read{"CapacityBlockedMinutes", q, args, []string{"idx_usage_capacity_at"}})
	q, args = capacityBlockedQuery(analysisStart, analysisEnd, "pool_a", 10)
	reads = append(reads, read{"CapacityBlockedMinutes for a pool", q, args, []string{"(pool_id=? AND at>? AND at<?)"}})
	q, args = jobVersionSpansQuery(analysisStart, analysisEnd, []string{"cancelled", "skipped"}, 10)
	reads = append(reads, read{"JobVersionSpans", q, args, []string{"(state=? AND queued_at>? AND queued_at<?)"}})
	reads = append(reads,
		read{"MeanFleetCapacity", meanFleetCapacitySQL, []any{ms(analysisStart), ms(analysisEnd)}, []string{"host_samples_at"}},
		read{"HostSamplesBetween", hostSamplesBetweenSQL, []any{"host_x", ms(analysisStart), ms(analysisEnd), 10}, []string{"(host_id=? AND at>? AND at<?)"}},
		read{"HostThrottleChanges", hostThrottleChangesSQL, []any{ms(analysisStart), ms(analysisEnd), 10, ms(analysisStart)},
			[]string{"idx_audit_action (action=? AND created_at>? AND created_at<?)", "idx_audit_action (action=? AND created_at<?)"}},
	)
	for _, r := range reads {
		plan := explain(t, s, r.query, r.args...)
		for _, w := range r.want {
			if !strings.Contains(plan, w) {
				t.Errorf("%s does not use %s: %s", r.name, w, plan)
			}
		}
		assertSearches(t, r.name, plan)
	}
}

// A cancelled job stops when somebody says so, and a job still running has no
// outcome: only a pass or a failure that completed in the window is read, with
// the commit and the steps the analysis groups and names a stage by.
func TestJobOutcomesReadsOnlyPassesAndFailuresCompletedInTheWindow(t *testing.T) {
	s := newTestStoreAt(t, func() time.Time { return analysisStart })
	at := func(m int) time.Time { return analysisStart.Add(time.Duration(m) * time.Minute) }
	failed := analysisJob(t, s, 1, "pool_a", "", at(1), func(j *Job) {
		completed(at(2), at(10), "failure")(j)
		j.HeadSHA, j.HeadBranch, j.RunAttempt = "abc", "main", 1
		j.Steps = JobSteps{{Number: 1, Name: "Run tests", Status: "completed", Conclusion: "failure"}}
	})
	analysisJob(t, s, 2, "pool_a", "", at(1), completed(at(2), at(11), "success"))
	analysisJob(t, s, 3, "pool_a", "", at(1), completed(at(2), at(12), "cancelled"))
	analysisJob(t, s, 4, "pool_a", "", at(1), completed(at(2), analysisStart.Add(-time.Minute), "failure"))
	analysisJob(t, s, 5, "pool_a", "", at(1), completed(at(2), analysisEnd, "failure"))

	got, more, err := s.JobOutcomes(context.Background(), analysisStart, analysisEnd, 10)
	if err != nil || more || len(got) != 2 {
		t.Fatalf("outcomes = %v, more %v, err %v; want the success and the failure", got, more, err)
	}
	if got[0].Conclusion != "success" || got[1].ID != failed.ID || got[1].HeadSHA != "abc" ||
		got[1].RunAttempt != 1 || len(got[1].Steps) != 1 || got[1].Steps[0].Name != "Run tests" || !got[1].CompletedAt.Equal(at(10)) {
		t.Errorf("outcomes = %+v", got)
	}
	if one, more, _ := s.JobOutcomes(context.Background(), analysisStart, analysisEnd, 1); len(one) != 1 || !more {
		t.Errorf("limit 1: %d, more %v; want one and more", len(one), more)
	}
	if _, _, err := s.JobOutcomes(context.Background(), analysisStart, analysisEnd, 0); err == nil {
		t.Error("a limit of zero was accepted")
	}
}
