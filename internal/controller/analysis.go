package controller

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"math"
	"slices"
	"strings"
	"time"

	"github.com/eyupio/zoomies/internal/analysis"
	"github.com/eyupio/zoomies/internal/naming"
	"github.com/eyupio/zoomies/internal/scheduler"
	"github.com/eyupio/zoomies/internal/store"
)

// The four analyses are pure functions over a snapshot (internal/analysis).
// This file is the other half: it reads what the fleet keeps and assembles
// each snapshot, so the route, the MCP tool, Eli, the CLI and the UI card all
// render one computed answer. Every read is bounded, and a read that could be
// cut says so in the finding rather than ranking a part as if it were the
// whole. Each call reads the clock once, so the retention line and the
// labels of one finding are drawn at one instant.

// AnalysisWindow is the period an analysis reads: Since inclusive, Until
// exclusive, as the job routes' are.
type AnalysisWindow struct{ Since, Until time.Time }

// CompareRequest is what AnalyseCompare compares.
type CompareRequest struct {
	// By is "version" (the default when empty) or "period".
	By string
	// A and B name two releases when By is version. Either left empty is
	// taken from the releases that ran jobs in the period, the latest first.
	A, B         string
	Since, Until time.Time
}

// ErrTooFewReleases is a release comparison asked to choose its releases
// when fewer than two completed a job in the period. Pairing the one there is
// with the jobs no release stamped, or with nothing, would read as a verdict
// on it.
var ErrTooFewReleases = errors.New("fewer than two releases completed a job in this period, so there is no pair to compare; name the releases with a and b, widen the period, or compare by period")

// ErrSameRelease is a release comparison asked to compare a release with
// itself, which would read as a release that held still.
var ErrSameRelease = errors.New("a and b name the same release, so there is nothing to compare; name two different releases, or leave a out to compare with the release before b")

// ErrWholeDaysOnly is a by-period comparison one of whose sides is older than
// retention.jobs and has no midnight inside it to end on. The roll-up keeps
// whole UTC days, so such a side would read the other side's hours as its
// own, or nothing at all. The error a caller sees is a wholeDaysError, which
// says which side and what to change.
var ErrWholeDaysOnly = errors.New("a period older than retention.jobs can only be read in whole UTC days")

// NotFoundError is a pool or host an analysis was narrowed to that the fleet
// does not have. It carries what was asked for, so a caller can say so in its
// own words rather than reading this error's text, which a later wrap would
// change; it unwraps to store.ErrNotFound, so errors.Is still finds it.
type NotFoundError struct {
	// Kind is "pool" or "host".
	Kind, ID string
}

func (e NotFoundError) Error() string { return e.Kind + " " + e.ID + ": not found" }

func (e NotFoundError) Unwrap() error { return store.ErrNotFound }

type wholeDaysError struct {
	which        string
	since, until time.Time
}

func (e wholeDaysError) Error() string {
	const layout = "2006-01-02 15:04 UTC"
	return fmt.Sprintf("%s, %s to %s, is older than retention.jobs, so it can only be read in whole UTC days from the usage roll-up, "+
		"and it has no midnight inside it to end on; choose a period of at least a whole day, or, for a period ending now, one no longer than half of retention.jobs",
		e.which, e.since.UTC().Format(layout), e.until.UTC().Format(layout))
}

func (e wholeDaysError) Unwrap() error { return ErrWholeDaysOnly }

// analysisExcluded is what a comparison leaves out of every figure: jobs
// that stop when somebody says so, not when the work is done (spec 5.2).
var analysisExcluded = []string{"cancelled", "skipped"}

// faultsOfARunningJob are the faults that happen to a runner while it runs a
// job: store's own list puts them first, apart from the ones that stop a
// runner ever taking one. A registered runner that ended with one of them
// started; what went wrong was its job's, not its start's.
var faultsOfARunningJob = []store.FaultKind{store.FaultHostLost, store.FaultOutOfMemory, store.FaultOutOfDisk, store.FaultRemoved}

// ErrSinceNotPast is a period that begins at or after now. Nothing in it has
// happened, so any answer would be about an empty period read as a quiet one.
var ErrSinceNotPast = errors.New("since is not before now, and an analysis reads only what has already happened; choose a since in the past")

// readableWindow is the period an analysis reads, given the instant its call
// read the clock at, or why it cannot read it: one that ends before it
// starts, one that begins at or after now, and one longer than
// limits.job_stats_window, which bounds every analysis as it bounds the job
// statistics. The routes check all three, on the same window clamped to now,
// before calling; this is the same rule for any other caller.
//
// An until past now ends now. The hours after it have not happened, and read
// as if they had, every job still waiting would be counted as waiting
// through them, and a host's unsampled future minutes would read as gaps in
// its samples, at full confidence. Clamping here, once, holds the route, the
// tools, the CLI and the card to it, and the finding's until says what was
// read.
func (c *Controller) readableWindow(w AnalysisWindow, now time.Time) (AnalysisWindow, error) {
	if w.Since.IsZero() || !w.Since.Before(w.Until) {
		return w, errors.New("an analysis needs a period whose since is before its until")
	}
	if !w.Since.Before(now) {
		return w, ErrSinceNotPast
	}
	w.Until = earlier(w.Until, now)
	if limit := c.cfg().Limits.JobStatsSpan(); w.Until.Sub(w.Since) > limit {
		return w, errors.New(SpanRefusal(w.Until.Sub(w.Since), limit, "an analysis covers"))
	}
	return w, nil
}

// SpanRefusal is the sentence that refuses a period longer than
// limits.job_stats_window, which bounds the job statistics and every analysis
// alike. what is the reader and its verb, "job statistics cover" or "an
// analysis covers", so the routes and this package say one sentence and
// neither borrows the other's noun.
func SpanRefusal(span, limit time.Duration, what string) string {
	return fmt.Sprintf("the period from since to until is %s, and %s at most %s; narrow it, or raise limits.job_stats_window",
		daysAndHours(span, true), what, daysAndHours(limit, false))
}

// daysAndHours says a length the way limits.job_stats_window is thought of,
// "91 days and 3 hours"; a Go duration's "2187h0m0s" is right and
// unreadable. A period is rounded up to the hour and a limit down, so a
// period over its limit never reads as equal to it.
func daysAndHours(d time.Duration, up bool) string {
	unit := func(n int64, one string) string {
		if n == 1 {
			return "1 " + one
		}
		return fmt.Sprintf("%d %ss", n, one)
	}
	h := int64(d / time.Hour)
	if up && d%time.Hour != 0 {
		h++
	}
	switch days, hours := h/24, h%24; {
	case days == 0:
		return unit(hours, "hour")
	case hours == 0:
		return unit(days, "day")
	default:
		return unit(days, "day") + " and " + unit(hours, "hour")
	}
}

// minutesIn is a limit no per-minute read of one pool or one host over
// [since, until) can reach: there is at most one row a minute, so a read
// limited by it is never cut, whatever the period's length.
func minutesIn(since, until time.Time) int {
	return int(until.Sub(since)/time.Minute) + 2
}

// linkSteps fills each step's catalog page after the pure analysis returns:
// where the docs are published is not the analysis's to know (F8).
func linkSteps(steps []analysis.NextStep) {
	for i := range steps {
		if steps[i].Code != "" {
			steps[i].Link = catalogDocsFor(steps[i].Code)
		}
	}
}

func poolsByID(pools []*store.Pool) map[string]*store.Pool {
	out := make(map[string]*store.Pool, len(pools))
	for _, p := range pools {
		out[p.ID] = p
	}
	return out
}

func hostsByID(hosts []*store.Host) map[string]*store.Host {
	out := make(map[string]*store.Host, len(hosts))
	for _, h := range hosts {
		out[h.ID] = h
	}
	return out
}

// AnalyseQueueWait puts every second jobs queued in the period waited down to
// one cause, for the fleet or for one pool.
func (c *Controller) AnalyseQueueWait(ctx context.Context, w AnalysisWindow, poolID string) (analysis.QueueWaitFinding, error) {
	snap, err := c.queueWaitSnapshot(ctx, w, poolID)
	if err != nil {
		return analysis.QueueWaitFinding{}, err
	}
	f, err := analysis.QueueWait(snap)
	linkSteps(f.NextSteps)
	return f, err
}

func (c *Controller) queueWaitSnapshot(ctx context.Context, w AnalysisWindow, poolID string) (analysis.QueueWaitSnapshot, error) {
	w, err := c.readableWindow(w, c.Now())
	snap := analysis.QueueWaitSnapshot{Since: w.Since, Until: w.Until}
	if err != nil {
		return snap, err
	}
	pools, err := c.st.ListPools(ctx)
	if err != nil {
		return snap, fmt.Errorf("listing pools: %w", err)
	}
	byID := poolsByID(pools)
	// Queued in the period, not active in it: the wait is not clamped at
	// Since, so a job queued before it would bring its earlier wait along.
	//
	// Only the jobs this fleet has a hand in, the Jobs page's own default: a
	// vendor's job nothing here claims is not time a pool should have claimed
	// it in, and the finding would otherwise name it as such and tell an
	// operator to change a pool's labels to fit a vendor's.
	jobs, more, err := c.st.JobWaits(ctx, store.JobWaitFilter{Since: w.Since, Until: w.Until, PoolID: poolID, ManagedOnly: true}, analysis.MaxReadJobs)
	if err != nil {
		return snap, fmt.Errorf("reading job waits: %w", err)
	}
	// A pool deleted since is still named in the period's jobs, and a cut
	// answer tells its reader to narrow to it; refusing it then would send
	// them round in a circle. Only an id the fleet never had, or none of
	// the period's jobs name, is not found.
	if poolID != "" && byID[poolID] == nil && len(jobs) == 0 {
		return snap, NotFoundError{Kind: "pool", ID: poolID}
	}
	snap.JobsTruncated = more
	hosts, err := c.st.ListHosts(ctx)
	if err != nil {
		return snap, fmt.Errorf("listing hosts: %w", err)
	}
	episodes, err := c.throttleHistory(ctx, hosts, w)
	if err != nil {
		return snap, err
	}
	seen := map[string]bool{}
	for _, j := range jobs {
		snap.Jobs = append(snap.Jobs, waitJob(j, byID[j.PoolID]))
		if j.PoolID == "" || seen[j.PoolID] {
			continue
		}
		seen[j.PoolID] = true
		cond, cut, err := c.poolConditions(ctx, w, j.PoolID, byID[j.PoolID], hosts, episodes)
		if err != nil {
			return snap, err
		}
		snap.RunnersTruncated = snap.RunnersTruncated || cut
		snap.Pools = append(snap.Pools, cond)
	}
	return snap, nil
}

// waitJob is one job's timeline as the breakdown reads it.
//
// A pool that keeps its runners hands a job to one registered long before
// it, so that runner's request and registration are not this job's startup,
// and the wait after eligibility cannot be read as GitHub's hand-off to a
// runner that was ready either: it may have been busy with another job. Its
// stamps are left out, so the wait goes to the pool's own conditions or to
// none, and RunnerKept says why, so the finding does not call the runner's
// record missing. A warm runner of an ephemeral pool also came before its
// job, but it was idle and waiting for exactly one, so its hand-off is
// GitHub's, and the breakdown already gives a runner that was there an empty
// startup.
func waitJob(j store.JobWait, p *store.Pool) analysis.WaitJob {
	out := analysis.WaitJob{ID: j.ID, PoolID: j.PoolID, HostID: j.HostID, Repo: j.Repo, Workflow: j.Workflow, JobName: j.JobName,
		QueuedAt: j.QueuedAt, HeldAt: j.HeldAt, ApprovedAt: j.ApprovedAt, EligibleAt: j.EligibleAt,
		FirstStartFailureAt: j.FirstStartFailureAt, RunnerRequestedAt: j.RunnerRequestedAt, RunnerRegisteredAt: j.RunnerRegisteredAt,
		StartedAt: j.StartedAt}
	if p != nil && !p.Ephemeral && j.RunnerRequestedAt != nil && j.RunnerRequestedAt.Before(j.QueuedAt) {
		out.RunnerRequestedAt, out.RunnerRegisteredAt, out.RunnerKept = nil, nil, true
	}
	return out
}

// poolConditions is what held one pool back over the period, and whether
// its runner read was cut. A pool deleted since has no ceiling or hosts to
// judge it by now, so only the minutes it was recorded blocked are known.
func (c *Controller) poolConditions(ctx context.Context, w AnalysisWindow, id string, p *store.Pool,
	hosts []*store.Host, episodes map[string][]analysis.Interval) (analysis.PoolConditions, bool, error) {
	out := analysis.PoolConditions{PoolID: id, Deleted: p == nil}
	blocked, _, err := c.st.CapacityBlockedMinutes(ctx, w.Since, w.Until, id, minutesIn(w.Since, w.Until))
	if err != nil {
		return out, false, fmt.Errorf("reading pool %s's blocked minutes: %w", id, err)
	}
	out.NoRoom = blocked[id]
	if p == nil {
		return out, false, nil
	}
	out.PoolName = p.Name
	// The scheduler's own placement rule with the weather left out: a host
	// that could never run this pool (another architecture, another backend,
	// a selector it fails) gave the pool no room to lose to a throttle.
	for _, h := range hosts {
		if scheduler.HostCouldRun(h, p) {
			out.Throttled = append(out.Throttled, episodes[h.ID]...)
		}
	}
	if p.MaxRunners <= 0 {
		return out, false, nil
	}
	spans, more, err := c.st.RunnerSpans(ctx, store.RunnerSpanFilter{Since: w.Since, Until: w.Until, PoolID: id}, analysis.MaxReadJobs)
	if err != nil {
		return out, false, fmt.Errorf("reading pool %s's runners: %w", id, err)
	}
	out.AtCeiling = atCeiling(spans, p.MaxRunners, w)
	return out, more, nil
}

// atCeiling is when the pool's live runners were at or above maxRunners,
// swept from each runner's request to its end. The ceiling is today's: the
// fleet records no history of a pool's settings.
func atCeiling(spans []store.RunnerSpan, maxRunners int, w AnalysisWindow) []analysis.Interval {
	type edge struct {
		at   time.Time
		step int
	}
	var edges []edge
	for _, s := range spans {
		if from, to, ok := runnerLife(s, w); ok {
			edges = append(edges, edge{from, 1}, edge{to, -1})
		}
	}
	slices.SortFunc(edges, func(a, b edge) int { return a.at.Compare(b.at) })
	var out []analysis.Interval
	var open *time.Time
	live := 0
	for i := 0; i < len(edges); {
		now := edges[i].at
		// Every change at one instant first: a runner ending as another is
		// asked for is a hand-over, not a moment with both.
		for ; i < len(edges) && edges[i].at.Equal(now); i++ {
			live += edges[i].step
		}
		switch {
		case live >= maxRunners && open == nil:
			open = &now
		case live < maxRunners && open != nil:
			out = append(out, analysis.Interval{From: *open, To: now})
			open = nil
		}
	}
	if open != nil {
		out = append(out, analysis.Interval{From: *open, To: w.Until})
	}
	return out
}

// runnerLife is the part of the period a runner held its place in the pool.
// A runner that failed before it registered held none (P8): the scheduler
// gives a failed runner no slot, and counting the starts that kept failing
// would send the reader to raise a ceiling rather than to fix the starts. A
// runner known only from a session that never registered may have been one
// of them, and is left out with them. A finished row whose end was never
// stamped has no known life, and is left out rather than counted as alive
// for ever.
func runnerLife(s store.RunnerSpan, w AnalysisWindow) (time.Time, time.Time, bool) {
	// A session keeps no state or fault, so one that never registered may
	// be a failed start too, and is held to the same rule.
	if s.RegisteredAt == nil && (s.State == store.RunnerFailed || s.FaultKind != "" || s.State == "") {
		return time.Time{}, time.Time{}, false
	}
	to := w.Until
	switch {
	case s.EndedAt != nil:
		to = *s.EndedAt
	case s.State == "" || s.State.Terminal():
		return time.Time{}, time.Time{}, false
	}
	// A row is read on its creation, and its request can be delivered after
	// the period ends; both ends are held to the period.
	from := later(w.Since, earlier(s.RequestedAt, w.Until))
	to = later(w.Since, earlier(to, w.Until))
	return from, to, to.After(from)
}

// throttleHistory is every host's throttle episodes over the period, keyed
// by host.
//
// A rung moves at most once a ThrottleStep on a host, so the limit is a bound
// the steps the fleet writes cannot pass; an operator's clears and the hosts
// deleted since are what could, and a read cut by them claims nothing before
// the oldest step it kept.
func (c *Controller) throttleHistory(ctx context.Context, hosts []*store.Host, w AnalysisWindow) (map[string][]analysis.Interval, error) {
	limit := max(analysis.MaxReadJobs, (len(hosts)+1)*(int(w.Until.Sub(w.Since)/scheduler.ThrottleStep)+2))
	rows, more, err := c.st.HostThrottleChanges(ctx, w.Since, w.Until, limit)
	if err != nil {
		return nil, fmt.Errorf("reading throttle history: %w", err)
	}
	rows, from := keptThrottleRows(rows, more, w.Since)
	return throttleEpisodes(rows, hosts, from, w.Until), nil
}

// keptThrottleRows is what a throttle read can be trusted for, and from when.
// A whole read is trusted from the period's start. A cut one lost the
// period's oldest steps, so any host's rung before the oldest step kept is
// not known, and the starting rungs, which the lost steps may have
// overtaken, are dropped with it.
func keptThrottleRows(rows []store.ThrottleChange, more bool, since time.Time) ([]store.ThrottleChange, time.Time) {
	if !more {
		return rows, since
	}
	var kept []store.ThrottleChange
	for _, r := range rows {
		if !r.At.Before(since) {
			kept = append(kept, r)
		}
	}
	if len(kept) == 0 {
		return nil, since
	}
	return kept, kept[0].At
}

// throttleEpisodes turns the audit log's throttle steps into the stretches
// each host stood on a rung during [since, until). An episode still open at
// until has a zero To.
//
// Rows before since are each host's last step before the period, its
// starting rung; a rung above none then was standing as the period opened.
// That is believed only of a host that still exists: one deleted while
// throttled never gets the step that would close it. A host with no step
// before its first in the period, where that first step is a lift, a clear or
// a climb past the first rung, was on a rung already (P14). And a host still
// throttled now carries when its episode began, which covers the period's
// start even when the audit trail does not.
//
// Only times and hosts leave here: the rung and the action decide the
// episodes and go no further (F14), and the actor was never read.
func throttleEpisodes(rows []store.ThrottleChange, hosts []*store.Host, since, until time.Time) map[string][]analysis.Interval {
	exists := hostsByID(hosts)
	rows = slices.Clone(rows)
	slices.SortStableFunc(rows, func(a, b store.ThrottleChange) int { return a.At.Compare(b.At) })
	out := map[string][]analysis.Interval{}
	open := map[string]time.Time{}
	seen := map[string]bool{}
	for _, r := range rows {
		h := r.HostID
		if r.At.Before(since) {
			if exists[h] == nil {
				continue
			}
			seen[h] = true
			if r.Level > 0 {
				open[h] = since
			} else {
				delete(open, h)
			}
			continue
		}
		if !r.At.Before(until) {
			continue
		}
		_, on := open[h]
		if !seen[h] && !on && (r.Action != "host.throttle" || r.Level > 1) {
			open[h], on = since, true
		}
		seen[h] = true
		switch {
		case !on && r.Level > 0:
			open[h] = r.At
		case on && r.Level <= 0:
			out[h] = append(out[h], analysis.Interval{From: open[h], To: r.At})
			delete(open, h)
		}
	}
	for h, from := range open {
		out[h] = append(out[h], analysis.Interval{From: from})
	}
	for _, h := range hosts {
		t := h.Throttle
		if t.Active() && t.Since != nil && t.Since.Before(until) {
			out[h.ID] = append(out[h.ID], analysis.Interval{From: later(since, *t.Since)})
		}
	}
	for h, list := range out {
		out[h] = mergeEpisodes(list, until)
	}
	return out
}

// mergeEpisodes sorts one host's episodes and joins those that overlap, so
// the same stretch read from the audit log and from the host's own since is
// one episode; an episode reaching until is open again.
func mergeEpisodes(list []analysis.Interval, until time.Time) []analysis.Interval {
	end := func(iv analysis.Interval) time.Time {
		if iv.To.IsZero() {
			return until
		}
		return iv.To
	}
	slices.SortFunc(list, func(a, b analysis.Interval) int { return a.From.Compare(b.From) })
	var out []analysis.Interval
	for _, iv := range list {
		iv.To = end(iv)
		if n := len(out); n > 0 && !iv.From.After(out[n-1].To) {
			out[n-1].To = later(out[n-1].To, iv.To)
			continue
		}
		out = append(out, iv)
	}
	for i := range out {
		if !out[i].To.Before(until) {
			out[i].To = time.Time{}
		}
	}
	return out
}

// AnalyseCompare says what got worse and what got better between two
// releases, or between the period and the one before it.
func (c *Controller) AnalyseCompare(ctx context.Context, req CompareRequest) (analysis.CompareFinding, error) {
	snap, err := c.compareSnapshot(ctx, req)
	if err != nil {
		return analysis.CompareFinding{}, err
	}
	f, err := analysis.Compare(snap)
	linkSteps(f.NextSteps)
	return f, err
}

func (c *Controller) compareSnapshot(ctx context.Context, req CompareRequest) (analysis.CompareSnapshot, error) {
	now := c.Now()
	snap := analysis.CompareSnapshot{By: cmp.Or(req.By, "version"), Excluded: slices.Clone(analysisExcluded)}
	w, err := c.readableWindow(AnalysisWindow{Since: req.Since, Until: req.Until}, now)
	if err != nil {
		return snap, err
	}
	// Jobs completed before this are pruned; zero keeps them for ever.
	var cut *time.Time
	if keep := c.cfg().Retention.Jobs; keep > 0 {
		cut = ptrTo(now.Add(-keep))
	}
	switch snap.By {
	case "version":
		snap.A, snap.B, err = c.releaseSides(ctx, req, w, cut)
	case "period":
		snap.A, snap.B, err = c.periodSides(ctx, w, now, cut)
	default:
		return snap, fmt.Errorf("a comparison is by version or by period, not %q", snap.By)
	}
	return snap, err
}

func ptrTo[T any](v T) *T { return &v }

// earlier is the earlier of two instants, later's partner.
func earlier(a, b time.Time) time.Time {
	if b.Before(a) {
		return b
	}
	return a
}

// releaseSides reads two releases' jobs in the period. With a release left
// unnamed, the latest to run jobs is taken, never the jobs no release here
// stamped, which are hosted jobs and history from before stamping.
func (c *Controller) releaseSides(ctx context.Context, req CompareRequest, w AnalysisWindow, cut *time.Time) (analysis.CompareSide, analysis.CompareSide, error) {
	var a, b analysis.CompareSide
	spans, more, err := c.st.JobVersionSpans(ctx, w.Since, w.Until, analysisExcluded, analysis.MaxReadJobs)
	if err != nil {
		return a, b, fmt.Errorf("reading releases: %w", err)
	}
	if req.A != "" && req.A == req.B {
		return a, b, ErrSameRelease
	}
	// pick is the latest release that ran jobs, not already named and, when
	// before is set, whose last job came before it: with only B named, A is
	// the release before B, so a change reads in the direction time ran.
	named := map[string]bool{req.A: true, req.B: true}
	pick := func(before *time.Time) string {
		for _, s := range spans {
			if s.Version != store.UnknownVersion && !named[s.Version] && (before == nil || s.Last.Before(*before)) {
				named[s.Version] = true
				return s.Version
			}
		}
		return ""
	}
	va, vb := req.A, req.B
	if vb == "" {
		vb = pick(nil)
	}
	if va == "" {
		// A named B with no jobs here has no place in time to be before, so
		// its other side is simply the latest other release.
		var before *time.Time
		if i := slices.IndexFunc(spans, func(s store.VersionSpan) bool { return s.Version == vb }); i >= 0 && req.B != "" {
			before = &spans[i].Last
		}
		va = pick(before)
	}
	if va == "" || vb == "" {
		return a, b, ErrTooFewReleases
	}
	if a, err = c.releaseSide(ctx, va, w, spans, more, cut); err != nil {
		return a, b, err
	}
	b, err = c.releaseSide(ctx, vb, w, spans, more, cut)
	return a, b, err
}

// releaseSide is one release's jobs in the period, spanning the minutes its
// first and last were queued in, so the fleet's size is read over the time
// it was the release running.
func (c *Controller) releaseSide(ctx context.Context, version string, w AnalysisWindow, spans []store.VersionSpan, more bool, cut *time.Time) (analysis.CompareSide, error) {
	side := analysis.CompareSide{Label: version, Since: w.Since, Until: w.Until, AskedUntil: w.Until}
	i := slices.IndexFunc(spans, func(s store.VersionSpan) bool { return s.Version == version })
	if i >= 0 {
		side.Since = later(w.Since, spans[i].First.Truncate(time.Minute))
		side.Until = earlier(w.Until, spans[i].Last.Truncate(time.Minute).Add(time.Minute))
	}
	figures, err := c.jobFigures(ctx, store.JobFilter{Since: &w.Since, Until: &w.Until, ControllerVersions: []string{version}})
	if err != nil {
		return side, err
	}
	side.Figures = figures
	if side.MeanSlots, err = c.st.MeanFleetCapacity(ctx, side.Since, side.Until); err != nil {
		return side, fmt.Errorf("reading the fleet's slots: %w", err)
	}
	side.JobsGoneBefore = releaseJobsGone(version, spans, more, w, cut)
	return side, nil
}

// releaseJobsGone is the retention line when the release may have lost jobs
// to it, and nil when it cannot have. The cut fell inside one release's run:
// the one whose earliest kept job no other release's precedes. A release
// that began after another's kept jobs began after the cut, and lost none.
// The jobs no release stamped run throughout and say nothing about when a
// release began. A release with no kept job in a period reaching past the
// cut may have lost all of them, and a read cut short cannot tell which
// release came first, so both are said to have.
func releaseJobsGone(version string, spans []store.VersionSpan, more bool, w AnalysisWindow, cut *time.Time) *time.Time {
	if cut == nil || !w.Since.Before(*cut) {
		return nil
	}
	i := slices.IndexFunc(spans, func(s store.VersionSpan) bool { return s.Version == version })
	if i < 0 || more {
		return cut
	}
	for _, o := range spans {
		if o.Version != store.UnknownVersion && o.Version != version && o.First.Before(spans[i].First) {
			return nil
		}
	}
	return cut
}

// jobFigures reads one side's completed jobs, cancelled and skipped left
// out of every figure, through the query behind the Usage page's table.
func (c *Controller) jobFigures(ctx context.Context, f store.JobFilter) (analysis.JobFigures, error) {
	f.ExcludeConclusions = analysisExcluded
	res, err := c.st.JobStats(ctx, f, nil)
	if err != nil {
		return analysis.JobFigures{}, fmt.Errorf("computing job statistics: %w", err)
	}
	if len(res.Groups) == 0 {
		return analysis.JobFigures{}, nil
	}
	g := res.Groups[0]
	spread := func(p store.JobPercentiles) analysis.Spread {
		return analysis.Spread{Samples: p.Samples, P50MS: p.P50MS, P95MS: p.P95MS}
	}
	return analysis.JobFigures{Jobs: g.Count, Succeeded: g.Succeeded, Failed: g.Failed, FleetFailed: g.FleetFailed,
		OOMKilled: g.OOMKilled, Duration: spread(g.Duration), QueueWait: spread(g.QueueWait), Startup: spread(g.Startup)}, nil
}

// periodSides reads the period and the one of the same length ending where
// it begins.
func (c *Controller) periodSides(ctx context.Context, w AnalysisWindow, now time.Time, cut *time.Time) (analysis.CompareSide, analysis.CompareSide, error) {
	span := w.Until.Sub(w.Since)
	aw := AnalysisWindow{Since: w.Since.Add(-span), Until: w.Since}
	// The later side first: a side read from the roll-up begins where the
	// store's counts begin, which can be its first midnight, before the
	// period asked for. The earlier side ends there, so no hour is in both
	// counts.
	b, err := c.periodSide(ctx, w, w.Until, cut, "the period")
	if err != nil {
		return analysis.CompareSide{}, b, err
	}
	var a analysis.CompareSide
	if b.NoRecordBefore != nil {
		// Nothing before the later side's counts is on record, so the
		// earlier side has nothing to read: it is said to be unread, never
		// read as a period in which no jobs ran.
		a = analysis.CompareSide{Since: aw.Until, Until: aw.Until, AskedUntil: aw.Until, CountsOnly: true, NoRecordBefore: b.NoRecordBefore}
	} else if a, err = c.periodSide(ctx, aw, earlier(aw.Until, b.Since), cut, "the earlier period"); err != nil {
		return a, b, err
	}
	// Each side is named for the span its counts cover: one read from the
	// roll-up covers whole days, and "the 33 hours before" over counts of two
	// days would misname them.
	words := func(s analysis.CompareSide) string {
		if d := s.Until.Sub(s.Since); d > 0 {
			return periodWords(d)
		}
		return periodWords(span)
	}
	a.Label = "the " + words(a) + " before"
	b.Label = "the last " + words(b)
	// "The last" is a promise about now; a period that ended earlier says
	// when. A side that could not be read is named for the span asked for,
	// its end included.
	end := b.Until
	if !b.Since.Before(b.Until) {
		end = b.AskedUntil
	}
	if now.Sub(end) > time.Minute || end.Sub(now) > time.Minute {
		b.Label = "the " + words(b) + " to " + end.UTC().Format("2006-01-02 15:04 UTC")
	}
	return a, b, nil
}

// periodSide is the jobs of the period w asks for, read up to the instant to
// (w.Until, or where the later side's counts begin). One that starts before
// the retention line is read from the daily roll-up instead (decision 5):
// counts only, whole UTC days from the midnight before its start, and its
// dates are the span the store says it read, never the span asked for. Its
// Since is where the counts are complete from: that midnight, or the
// fleet's first record if it is later, which NoRecordBefore then names. It
// is never a side whose jobs are cut short, because the roll-up keeps every
// day.
func (c *Controller) periodSide(ctx context.Context, w AnalysisWindow, to time.Time, cut *time.Time, which string) (analysis.CompareSide, error) {
	side := analysis.CompareSide{Since: w.Since, Until: to, AskedUntil: w.Until}
	if cut != nil && w.Since.Before(*cut) {
		// A day the roll-up has absorbed is read whole, so a side ending
		// partway through one would count that day's later hours, which are
		// the other side's. It ends at that day's midnight instead: its Until
		// says so, its label names the span it covers, and, because its
		// AskedUntil keeps the end asked for, the comparison's doubts name
		// the hours cut off and which side, if either, counts them. A side with
		// no midnight inside it to end on cannot be read apart from the
		// other, and is refused. A day not yet absorbed is read from the
		// rows, to the instant.
		if midnight := to.Truncate(24 * time.Hour); !midnight.Equal(to) {
			rolled, err := c.st.InstallationRollupUntil(ctx)
			if err != nil {
				return side, fmt.Errorf("reading the usage roll-up: %w", err)
			}
			if rolled.After(midnight) {
				if !midnight.After(w.Since) {
					return side, wholeDaysError{which: which, since: w.Since, until: w.Until}
				}
				to = midnight
			}
		}
		if !to.After(w.Since) {
			// The other side's counts begin at or before this side's start:
			// what is left of it has no midnight of its own to end on.
			return side, wholeDaysError{which: which, since: w.Since, until: w.Until}
		}
		counts, from, _, err := c.st.InstallationCountsBetween(ctx, w.Since, to)
		if err != nil {
			return side, fmt.Errorf("reading the usage roll-up: %w", err)
		}
		for _, n := range counts {
			side.Figures.Jobs += n.RanHere
			side.Figures.FleetFailed += n.FleetFault
		}
		side.Since, side.Until, side.CountsOnly = from, to, true
		if from.After(w.Since) {
			side.NoRecordBefore = &from
			if !from.Before(to) && to.Before(w.Until) {
				// Read short of its asked end, an unread side learns only
				// that nothing is on record before the end of that read: a
				// midnight, or where the later side's counts begin. The
				// fleet's counts can begin later, inside the hours cut off,
				// which are then the later side's or neither side's. Asked
				// over the whole span, the store says where they begin, and
				// that is the instant to name.
				_, first, _, err := c.st.InstallationCountsBetween(ctx, w.Since, w.Until)
				if err != nil {
					return side, fmt.Errorf("reading the usage roll-up: %w", err)
				}
				side.NoRecordBefore = &first
			}
		}
	} else {
		figures, err := c.jobFigures(ctx, store.JobFilter{Since: &w.Since, Until: &to})
		if err != nil {
			return side, err
		}
		side.Figures = figures
	}
	var err error
	if side.MeanSlots, err = c.st.MeanFleetCapacity(ctx, side.Since, side.Until); err != nil {
		return side, fmt.Errorf("reading the fleet's slots: %w", err)
	}
	return side, nil
}

// periodWords is a period's length as a sentence names it after "the last":
// "hour", "36 hours", "2 days", and anything else as Words gives it.
func periodWords(d time.Duration) string {
	unit := func(n int64, one string) string {
		if n == 1 {
			return one
		}
		return fmt.Sprintf("%d %ss", n, one)
	}
	switch {
	case d%time.Hour == 0 && d < 48*time.Hour:
		return unit(int64(d/time.Hour), "hour")
	case d%(24*time.Hour) == 0:
		return unit(int64(d/(24*time.Hour)), "day")
	case d%time.Minute == 0 && d < time.Hour:
		return unit(int64(d/time.Minute), "minute")
	}
	return analysis.Words(d)
}

// AnalyseHostPressure says whether what went wrong on a host came in the
// minutes it was under pressure.
func (c *Controller) AnalyseHostPressure(ctx context.Context, w AnalysisWindow, hostID string) (analysis.HostPressureFinding, error) {
	snap, err := c.hostPressureSnapshot(ctx, w, hostID)
	if err != nil {
		return analysis.HostPressureFinding{}, err
	}
	f, err := analysis.HostPressure(snap)
	linkSteps(f.NextSteps)
	return f, err
}

func (c *Controller) hostPressureSnapshot(ctx context.Context, w AnalysisWindow, hostID string) (analysis.HostPressureSnapshot, error) {
	w, err := c.readableWindow(w, c.Now())
	snap := analysis.HostPressureSnapshot{Since: w.Since, Until: w.Until, HostID: hostID, Thresholds: analysis.DefaultPressure}
	if err != nil {
		return snap, err
	}
	h, err := c.st.GetHost(ctx, hostID)
	if errors.Is(err, store.ErrNotFound) {
		return snap, NotFoundError{Kind: "host", ID: hostID}
	}
	if err != nil {
		return snap, fmt.Errorf("reading host %s: %w", hostID, err)
	}
	// A name stored before joins were checked can carry what ValidateHostName
	// now refuses, and it reaches the finding's sentences.
	snap.HostName = naming.ForSentence(h.Name)
	// The throttle's own memory test is the host's reserve, which has no
	// default to fall back on (F9).
	snap.Thresholds.MemoryReserveMB = h.MemoryReserve()
	// The minutes the analysis counts: every one the period overlaps. One
	// sample a minute, so a limit of the minutes is never reached.
	first, end := w.Since.Truncate(time.Minute), w.Until.Add(time.Minute-1).Truncate(time.Minute)
	samples, _, err := c.st.HostSamplesBetween(ctx, h.ID, first, end, minutesIn(first, end))
	if err != nil {
		return snap, fmt.Errorf("reading host %s's samples: %w", h.ID, err)
	}
	for _, s := range samples {
		snap.Minutes = append(snap.Minutes, hostMinute(s))
	}
	// A failure is placed when it happened, so the jobs are those that
	// started or completed in the period, wherever they were queued (P11).
	jobs, more, err := c.st.JobWaits(ctx, store.JobWaitFilter{Since: w.Since, Until: w.Until, HostID: h.ID, ByActivity: true}, analysis.MaxReadJobs)
	if err != nil {
		return snap, fmt.Errorf("reading host %s's jobs: %w", h.ID, err)
	}
	snap.JobsTruncated = more
	for _, j := range jobs {
		snap.Jobs = append(snap.Jobs, hostJob(j))
	}
	return snap, nil
}

// hostMinute is one sample as the analysis judges it. The load is per core
// only when the host reported its cores, as the scheduler reads it: a load
// over none is no measurement, not an infinite one.
func hostMinute(s store.HostSample) analysis.HostMinute {
	m := analysis.HostMinute{At: s.At, CPUPercent: s.CPUPercent, MemoryAvailableMB: s.MemoryAvailableMB, MemoryMB: s.MemoryMB}
	if s.LoadAverage1 != nil && s.CPUs > 0 {
		m.LoadPerCPU = ptrTo(*s.LoadAverage1 / float64(s.CPUs))
	}
	return m
}

// hostJob is a job that ran on the host. Its startup is its runner's request
// to registration, the phase the queue-wait breakdown carves out of a wait,
// so the two analyses mean one thing by it (P11); a runner asked for before
// the job was queued was not started for it, and its startup is not this
// job's. A fleet fault recorded before faults had kinds is a fault all the
// same, named as the job statistics name it.
func hostJob(j store.JobWait) analysis.HostJob {
	out := analysis.HostJob{ID: j.ID, Repo: j.Repo, Workflow: j.Workflow, JobName: j.JobName,
		StartedAt: j.StartedAt, CompletedAt: j.CompletedAt, Conclusion: j.Conclusion,
		FaultKind: string(j.FaultKind), OOMKilled: j.OOMKilled}
	if out.FaultKind == "" && j.RunnerFault != "" {
		out.FaultKind = "unclassified"
	}
	if r, g := j.RunnerRequestedAt, j.RunnerRegisteredAt; r != nil && g != nil && !r.Before(j.QueuedAt) && !g.Before(*r) {
		out.StartupSeconds = ptrTo(int64(math.Round(g.Sub(*r).Seconds())))
	}
	return out
}

// AnalyseStartupFailures names the host or pool that keeps failing to start
// runners in the period, narrowed to a pool or a host when one is given.
func (c *Controller) AnalyseStartupFailures(ctx context.Context, w AnalysisWindow, poolID, hostID string) (analysis.StartupFailuresFinding, error) {
	snap, err := c.startupSnapshot(ctx, w, poolID, hostID)
	if err != nil {
		return analysis.StartupFailuresFinding{}, err
	}
	f, err := analysis.StartupFailures(snap)
	linkSteps(f.NextSteps)
	return f, err
}

func (c *Controller) startupSnapshot(ctx context.Context, w AnalysisWindow, poolID, hostID string) (analysis.StartupSnapshot, error) {
	w, err := c.readableWindow(w, c.Now())
	snap := analysis.StartupSnapshot{Since: w.Since, Until: w.Until, HeartbeatInterval: c.heartbeatInterval()}
	if err != nil {
		return snap, err
	}
	pools, err := c.st.ListPools(ctx)
	if err != nil {
		return snap, fmt.Errorf("listing pools: %w", err)
	}
	hosts, err := c.st.ListHosts(ctx)
	if err != nil {
		return snap, fmt.Errorf("listing hosts: %w", err)
	}
	poolByID, hostByID := poolsByID(pools), hostsByID(hosts)
	spans, more, err := c.st.RunnerSpans(ctx, store.RunnerSpanFilter{Since: w.Since, Until: w.Until, PoolID: poolID, HostID: hostID}, analysis.MaxReadJobs)
	if err != nil {
		return snap, fmt.Errorf("reading runners: %w", err)
	}
	// A pool or host deleted since is still named in the period's runners,
	// as the queue wait's pools are in its jobs: only one that none of them
	// name, and the fleet does not have, is not found.
	if poolID != "" && poolByID[poolID] == nil && len(spans) == 0 {
		return snap, NotFoundError{Kind: "pool", ID: poolID}
	}
	if hostID != "" && hostByID[hostID] == nil && len(spans) == 0 {
		return snap, NotFoundError{Kind: "host", ID: hostID}
	}
	snap.Truncated = startupTruncated(spans, more, w.Since)
	for _, s := range spans {
		// A start belongs to the period it was asked for in. A row is read
		// on its creation, and its request can come after the period ends.
		requested := earlier(s.RequestedAt, w.Until)
		if requested.Before(w.Since) || !requested.Before(w.Until) {
			continue
		}
		a := analysis.StartAttempt{RunnerID: s.RunnerID, PoolID: s.PoolID, HostID: s.HostID, RequestedAt: requested,
			ContainerStartedAt: s.ContainerStartedAt, RegisteredAt: s.RegisteredAt, EndedAt: s.EndedAt,
			Outcome: startOutcome(s, w.Until), FaultKind: string(s.FaultKind), Message: s.Message}
		if p := poolByID[s.PoolID]; p != nil {
			a.PoolName = p.Name
		}
		if h := hostByID[s.HostID]; h != nil {
			a.HostName = naming.ForSentence(h.Name)
		}
		snap.Attempts = append(snap.Attempts, a)
	}
	// A host keeps its incidents only while they are current; one that has
	// cleared is not known, and the finding says so.
	for _, h := range hosts {
		if inc := h.Incidents.Runtime; inc != nil {
			snap.Incidents = append(snap.Incidents, analysis.HostIncident{HostID: h.ID, Kind: analysis.IncidentRuntime, Since: inc.Since})
		}
		if inc := h.Incidents.ImagePull; inc != nil {
			snap.Incidents = append(snap.Incidents, analysis.HostIncident{HostID: h.ID, Kind: analysis.IncidentImagePull, PoolID: inc.PoolID, Since: inc.Since})
		}
	}
	episodes, err := c.throttleHistory(ctx, hosts, w)
	if err != nil {
		return snap, err
	}
	for id, list := range episodes {
		for _, iv := range list {
			snap.Throttles = append(snap.Throttles, analysis.HostInterval{HostID: id, Interval: iv})
		}
	}
	slices.SortFunc(snap.Throttles, func(a, b analysis.HostInterval) int {
		return cmp.Or(strings.Compare(a.HostID, b.HostID), a.From.Compare(b.From))
	})
	return snap, nil
}

// startupTruncated says whether a cut runner read lost a start of the
// period. The runners come newest request first, so a cut drops the oldest:
// when the oldest kept was asked for before the period, every one dropped
// was too, and none of them is a start the period holds.
func startupTruncated(spans []store.RunnerSpan, more bool, since time.Time) bool {
	return more && (len(spans) == 0 || !spans[len(spans)-1].RequestedAt.Before(since))
}

// startOutcome is how a start stood when the period ended. The store's state
// is the runner's state now, so a failure counts only if it came before
// until, and a registration likewise; a start with neither by then was still
// starting, or ended with nothing on record (P13).
//
// A failure is a row in failed, or a finished row with a fault, which is a
// failed runner since cleaned up. One that came after the runner registered
// is a failed start too, at its last stage, unless it is a fault of a running
// job: that runner started, and its job is what went wrong.
func startOutcome(s store.RunnerSpan, until time.Time) string {
	by := func(t *time.Time) bool { return t != nil && t.Before(until) }
	registered := by(s.RegisteredAt)
	failed := s.State == store.RunnerFailed || (s.State.Terminal() && s.FaultKind != "")
	// A failure with no end on record is placed at its request, which the
	// period holds.
	if failed && (s.EndedAt == nil || by(s.EndedAt)) {
		if registered && slices.Contains(faultsOfARunningJob, s.FaultKind) {
			return analysis.StartStarted
		}
		return analysis.StartFailed
	}
	if registered {
		return analysis.StartStarted
	}
	return analysis.StartUnknown
}

// AnalyseRecurringFailures names the jobs that keep failing in the period and
// what their failures have in common.
func (c *Controller) AnalyseRecurringFailures(ctx context.Context, w AnalysisWindow) (analysis.RecurringFailuresFinding, error) {
	snap, err := c.recurringSnapshot(ctx, w)
	if err != nil {
		return analysis.RecurringFailuresFinding{}, err
	}
	f, err := analysis.RecurringFailures(snap)
	linkSteps(f.NextSteps)
	return f, err
}

func (c *Controller) recurringSnapshot(ctx context.Context, w AnalysisWindow) (analysis.RecurringSnapshot, error) {
	w, err := c.readableWindow(w, c.Now())
	snap := analysis.RecurringSnapshot{Since: w.Since, Until: w.Until}
	if err != nil {
		return snap, err
	}
	jobs, more, err := c.st.JobOutcomes(ctx, w.Since, w.Until, analysis.MaxReadJobs)
	if err != nil {
		return snap, fmt.Errorf("reading job outcomes: %w", err)
	}
	snap.Truncated = more
	for _, j := range jobs {
		o := analysis.JobOutcome{JobID: j.ID, Repository: j.Repo, Workflow: j.Workflow, Job: j.JobName,
			HeadSHA: j.HeadSHA, HeadBranch: j.HeadBranch, RunID: j.GitHubRunID, RunAttempt: j.RunAttempt, Conclusion: j.Conclusion,
			RunnerFault: j.RunnerFault != "", CompletedAt: j.CompletedAt}
		for _, s := range j.Steps {
			if s.Conclusion == "failure" {
				o.FailedStep = s.Name
				break
			}
		}
		snap.Jobs = append(snap.Jobs, o)
	}
	return snap, nil
}
