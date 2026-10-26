package store

import (
	"context"
	"database/sql"
	"fmt"
	"slices"
	"strings"
	"time"
)

// The reads in this file are what the controller's analyses (internal/analysis)
// are assembled from. Each is one statement on the reader, and each takes a
// limit and says when it stopped at it: an analysis that ranks causes over a
// sample nobody told it was cut would read a busy period's newest hour as the
// whole period. What comes back to Go is the answer, never the window: an
// average is taken in SQL, and a narrowed analysis narrows the read rather
// than filtering a fleet-wide one, so the pool asked about cannot be the part
// a limit cut away.
//
// Each seeks an index rather than walking a table, which
// TestTheAnalysisReadsSeekTheirIndexes holds them to, and most of those ranges
// are the window itself or the window and a fixed tail. Two cannot be bounded
// below, and their cost is set by retention instead. A runner of a
// non-ephemeral pool can live for as long as the fleet runs, so a runner alive
// in the window may have been created at any time before it: RunnerSpans reads
// rows created before the window's end, which is what retention.runners keeps
// of finished runners plus every live one, and sessions that ended since the
// window opened, which reaches to now. The starting rungs HostThrottleChanges
// reads are the same: a throttle can stand for any length of time, so each
// host's last change before the window is looked for in every throttle row
// before it, which is few because a rung moves at most a few times an hour.

// maxJobLife is the longest a GitHub Actions job can last from being queued to
// completing: GitHub cancels a job left queued for 24 hours, and a job on a
// self-hosted runner may run for at most 5 days. A job that started in a
// window therefore completed before the window's end plus this, which bounds
// above the index range a ByActivity read seeks; without it a read of a day a
// month ago walks every job completed in the month since.
const maxJobLife = 24*time.Hour + 5*24*time.Hour

// needLimit refuses an unbounded analysis read. A zero limit is a caller that
// forgot one, and reading the whole window in its place is the failure these
// reads exist to design out.
func needLimit(limit int) error {
	if limit < 1 {
		return fmt.Errorf("store: an analysis read needs a limit of at least one, got %d", limit)
	}
	return nil
}

// JobWaitFilter selects the jobs JobWaits reads.
type JobWaitFilter struct {
	// Since is inclusive and Until exclusive, as JobFilter's are.
	Since, Until time.Time
	// PoolID and HostID narrow the read in SQL; empty is every pool or host.
	PoolID, HostID string
	// ByActivity selects the jobs that started or completed in the window
	// instead of those queued in it. A host's failures are placed when they
	// happened, and a job queued the evening before that failed this morning
	// is this morning's failure: read by queue time it would belong to no
	// period at all.
	ByActivity bool
	// ManagedOnly keeps the jobs this controller has a hand in, as JobFilter's
	// ManagedOnly does and by the same predicate. GitHub reports every job in
	// an installed repository, and one whose labels all name another vendor's
	// runners is that vendor's to run: read for the queue wait it was "no pool
	// claimed it" while still queued, or seconds nothing here explains once it
	// had started, and on a busy installation it took up the read's limit
	// before this fleet's own jobs did.
	ManagedOnly bool
}

// JobWait is one job as the queue-wait breakdown and host pressure need it:
// when each thing happened to it, and when its runner was asked for and came
// up.
type JobWait struct {
	ID, PoolID, HostID, Repo, Workflow, JobName, Conclusion string
	// ControllerVersion is empty for a job recorded before releases were
	// stamped.
	ControllerVersion string
	FaultKind         FaultKind
	// RunnerFault is set when a runner of this fleet stopped under the job; a
	// fault recorded before categories existed has it with no FaultKind.
	RunnerFault                        string
	OOMKilled                          bool
	QueuedAt                           time.Time
	EligibleAt, StartedAt, CompletedAt *time.Time
	// HeldAt, ApprovedAt and FirstStartFailureAt are the first waiting,
	// approved and runner_start_failed entries on the job's timeline. A
	// webhook is delivered at least once, so a repeat must not move a phase's
	// start; the first is the one that counts.
	HeldAt, ApprovedAt, FirstStartFailureAt *time.Time
	// RunnerID is the runner the job last ran on, kept from the job even when
	// neither the runner's row nor its session remains.
	RunnerID string
	// RunnerRequestedAt and RunnerRegisteredAt come from the runner's row
	// while it is kept (retention.runners) and from its session after: the
	// session keeps both for a year. RunnerContainerStartedAt is the row's
	// alone, because the session table has no container start; it is nil once
	// the row is pruned, which says the phase is unknown rather than zero.
	RunnerRequestedAt, RunnerContainerStartedAt, RunnerRegisteredAt *time.Time
}

// jobWaitsQuery builds JobWaits' one statement. It asks for limit+1 rows, so
// the extra row says whether more matched without a second COUNT over the
// window.
func jobWaitsQuery(f JobWaitFilter, limit int) (string, []any) {
	since, until := ms(f.Since), ms(f.Until)
	var cond []string
	var args []any
	if f.ByActivity {
		// Index ranges rather than one test on started_at, which has no
		// index: a job completed in the window, or after it within
		// maxJobLife and started in it, from one range on the completed
		// index; and a job still running that started in it, from the few
		// rows in progress, which have no completed_at to bound and need none.
		cond = append(cond, `((j.state = 'completed' AND j.completed_at >= ? AND j.completed_at < ?
				AND (j.completed_at < ? OR (j.started_at >= ? AND j.started_at < ?)))
			OR (j.state = 'in_progress' AND j.started_at >= ? AND j.started_at < ?))`)
		args = append(args, since, ms(f.Until.Add(maxJobLife)), until, since, until, since, until)
	} else {
		cond = append(cond, `j.queued_at >= ? AND j.queued_at < ?`)
		args = append(args, since, until)
	}
	if f.PoolID != "" {
		cond = append(cond, `j.pool_id = ?`)
		args = append(args, f.PoolID)
	}
	if f.HostID != "" {
		cond = append(cond, `j.host_id = ?`)
		args = append(args, f.HostID)
	}
	if f.ManagedOnly {
		cond = append(cond, managedJobSQL("j"))
	}
	first := func(kind JobEventKind) string {
		return `(SELECT MIN(e.at) FROM job_events e WHERE e.job_id = j.id AND e.kind = '` + string(kind) + `')`
	}
	// The row wins over the session whenever it exists, column by column
	// included: a row with no registration is a runner that never
	// registered, and the session written from it says the same.
	q := `SELECT j.id, j.pool_id, COALESCE(j.host_id, ''), j.repo, j.workflow, j.job_name, j.conclusion,
		COALESCE(j.controller_version, ''), j.fault_kind, j.runner_fault, j.oom_killed,
		j.queued_at, j.eligible_at, j.started_at, j.completed_at,
		` + first(JobEventWaiting) + `, ` + first(JobEventApproved) + `, ` + first(JobEventRunnerStartFailed) + `,
		j.runner_id,
		CASE WHEN r.id IS NOT NULL THEN COALESCE(r.create_task_issued_at, r.created_at)
			ELSE COALESCE(rs.create_task_issued_at, rs.started_at) END,
		r.container_started_at,
		CASE WHEN r.id IS NOT NULL THEN r.registered_at ELSE rs.registered_at END
		FROM jobs j
		LEFT JOIN runners r ON r.id = j.runner_id
		LEFT JOIN runner_sessions rs ON rs.runner_id = j.runner_id
		WHERE ` + strings.Join(cond, " AND ") + `
		ORDER BY j.queued_at DESC, j.id ASC LIMIT ?`
	return q, append(args, limit+1)
}

// JobWaits returns the jobs f selects, newest queued first, at most limit of
// them; more is true when more matched. Queued in [Since, Until) by default,
// or started or completed in it with ByActivity.
func (s *Store) JobWaits(ctx context.Context, f JobWaitFilter, limit int) (jobs []JobWait, more bool, err error) {
	if err := needLimit(limit); err != nil {
		return nil, false, err
	}
	q, args := jobWaitsQuery(f, limit)
	rows, err := s.read.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, false, err
	}
	defer rows.Close()
	for rows.Next() {
		var j JobWait
		var queued int64
		var oom int
		var eligible, started, done, held, approved, failed, requested, container, registered sql.NullInt64
		if err := rows.Scan(&j.ID, &j.PoolID, &j.HostID, &j.Repo, &j.Workflow, &j.JobName, &j.Conclusion,
			&j.ControllerVersion, &j.FaultKind, &j.RunnerFault, &oom,
			&queued, &eligible, &started, &done, &held, &approved, &failed,
			&j.RunnerID, &requested, &container, &registered); err != nil {
			return nil, false, err
		}
		j.OOMKilled, j.QueuedAt = oom == 1, at(queued)
		j.EligibleAt, j.StartedAt, j.CompletedAt = atp(eligible), atp(started), atp(done)
		j.HeldAt, j.ApprovedAt, j.FirstStartFailureAt = atp(held), atp(approved), atp(failed)
		j.RunnerRequestedAt, j.RunnerContainerStartedAt, j.RunnerRegisteredAt = atp(requested), atp(container), atp(registered)
		jobs = append(jobs, j)
	}
	if err := rows.Err(); err != nil {
		return nil, false, err
	}
	if len(jobs) > limit {
		return jobs[:limit], true, nil
	}
	return jobs, false, nil
}

// RunnerSpanFilter selects the runners RunnerSpans reads.
type RunnerSpanFilter struct {
	// Since is inclusive and Until exclusive.
	Since, Until time.Time
	// PoolID and HostID narrow the read in SQL; empty is every pool or host.
	PoolID, HostID string
}

// RunnerSpan is one runner's life: when it was asked for, came up and ended,
// and how it ended.
type RunnerSpan struct {
	RunnerID, PoolID, HostID string
	// RequestedAt is the first delivery of the runner's create task to its
	// host, or its creation for a runner from before that was stamped.
	RequestedAt time.Time
	// ContainerStartedAt is nil for a span read from a session, which keeps
	// no container start. EndedAt is nil for a runner still alive.
	ContainerStartedAt, RegisteredAt, EndedAt *time.Time
	// State, FaultKind and Message are the row's. A span read from the
	// session of a pruned runner has none of them: the session table keeps no
	// state, fault or message, so State is "" and the outcome of a start that
	// never registered is not known.
	State     RunnerState
	FaultKind FaultKind
	// Message is written by a host, so whoever shows it to a model treats it
	// as untrusted.
	Message string
}

// runnerSpansQuery builds RunnerSpans' one statement: the rows, then the
// sessions whose row is gone, newest request first across both.
//
// A runner counts as alive in the window when it was asked for before its end
// and either was asked for inside it or ended at or after its start. The first
// half keeps a runner that was asked for inside the window and whose end
// stamps were lost; the second draws the line at the start on the same side
// for a row as for a session, so a runner reads the same before and after its
// row is pruned. Neither half can be bounded below (see the file's comment).
func runnerSpansQuery(f RunnerSpanFilter, limit int) (string, []any) {
	since, until := ms(f.Since), ms(f.Until)
	rowCond := []string{`r.created_at < ?`,
		`(r.created_at >= ? OR r.state NOT IN ('removed','failed')
			OR COALESCE(r.finished_at, r.host_removed_at, r.cleaned_up_at, r.created_at) >= ?)`}
	rowArgs := []any{until, since, since}
	// A session always ends at or after it starts, so "started inside or
	// ended after the start" is "ended at or after the start", which is a
	// range on the finished index: the sessions that ended since the window
	// opened. The start's own index would be a range over a year of them;
	// the unary plus keeps the planner off it.
	sessCond := []string{`+rs.started_at < ?`, `rs.finished_at >= ?`,
		`NOT EXISTS (SELECT 1 FROM runners x WHERE x.id = rs.runner_id)`}
	sessArgs := []any{until, since}
	if f.PoolID != "" {
		rowCond, sessCond = append(rowCond, `r.pool_id = ?`), append(sessCond, `rs.pool_id = ?`)
		rowArgs, sessArgs = append(rowArgs, f.PoolID), append(sessArgs, f.PoolID)
	}
	if f.HostID != "" {
		rowCond, sessCond = append(rowCond, `r.host_id = ?`), append(sessCond, `rs.host_id = ?`)
		rowArgs, sessArgs = append(rowArgs, f.HostID), append(sessArgs, f.HostID)
	}
	q := `SELECT r.id, r.pool_id, COALESCE(r.host_id, ''), COALESCE(r.create_task_issued_at, r.created_at) AS requested,
			r.container_started_at, r.registered_at,
			CASE WHEN r.state IN ('removed','failed') THEN COALESCE(r.finished_at, r.host_removed_at, r.cleaned_up_at) END,
			r.state, r.fault_kind, r.message
		FROM runners r WHERE ` + strings.Join(rowCond, " AND ") + `
		UNION ALL
		SELECT rs.runner_id, rs.pool_id, rs.host_id, COALESCE(rs.create_task_issued_at, rs.started_at),
			NULL, rs.registered_at, rs.finished_at, '', '', ''
		FROM runner_sessions rs WHERE ` + strings.Join(sessCond, " AND ") + `
		ORDER BY requested DESC, 1 ASC LIMIT ?`
	args := append(rowArgs, sessArgs...)
	return q, append(args, limit+1)
}

// RunnerSpans returns the runners f selects that were alive at any point of
// [Since, Until), newest request first, at most limit of them; more is true
// when more matched. A runner whose row is kept is read from the row; one
// whose row was pruned is read from its session; none is read twice.
func (s *Store) RunnerSpans(ctx context.Context, f RunnerSpanFilter, limit int) (spans []RunnerSpan, more bool, err error) {
	if err := needLimit(limit); err != nil {
		return nil, false, err
	}
	q, args := runnerSpansQuery(f, limit)
	rows, err := s.read.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, false, err
	}
	defer rows.Close()
	for rows.Next() {
		var x RunnerSpan
		var requested int64
		var container, registered, ended sql.NullInt64
		if err := rows.Scan(&x.RunnerID, &x.PoolID, &x.HostID, &requested, &container, &registered, &ended,
			&x.State, &x.FaultKind, &x.Message); err != nil {
			return nil, false, err
		}
		x.RequestedAt = at(requested)
		x.ContainerStartedAt, x.RegisteredAt, x.EndedAt = atp(container), atp(registered), atp(ended)
		spans = append(spans, x)
	}
	if err := rows.Err(); err != nil {
		return nil, false, err
	}
	if len(spans) > limit {
		return spans[:limit], true, nil
	}
	return spans, false, nil
}

// capacityBlockedQuery builds CapacityBlockedMinutes' statement: newest
// first, so a cut keeps the minutes nearest the window's end, as the other
// reads keep their newest rows.
func capacityBlockedQuery(from, to time.Time, poolID string, limit int) (string, []any) {
	q := `SELECT pool_id, at FROM usage_capacity_samples WHERE at >= ? AND at < ? AND blocked = 1`
	args := []any{ms(from), ms(to)}
	if poolID != "" {
		q += ` AND pool_id = ?`
		args = append(args, poolID)
	}
	return q + ` ORDER BY at DESC, pool_id LIMIT ?`, append(args, limit+1)
}

// CapacityBlockedMinutes returns the minutes in [from, to) that
// usage_capacity_samples records as blocked at capacity, by pool and oldest
// first within each, at most limit minutes in all; more is true when more
// matched. An empty poolID is every pool.
func (s *Store) CapacityBlockedMinutes(ctx context.Context, from, to time.Time, poolID string, limit int) (map[string][]time.Time, bool, error) {
	if err := needLimit(limit); err != nil {
		return nil, false, err
	}
	q, args := capacityBlockedQuery(from, to, poolID, limit)
	rows, err := s.read.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, false, err
	}
	defer rows.Close()
	out := map[string][]time.Time{}
	n, more := 0, false
	for rows.Next() {
		var pool string
		var minute int64
		if err := rows.Scan(&pool, &minute); err != nil {
			return nil, false, err
		}
		if n == limit {
			more = true
			break
		}
		n++
		out[pool] = append(out[pool], at(minute))
	}
	if err := rows.Err(); err != nil {
		return nil, false, err
	}
	for _, minutes := range out {
		slices.Reverse(minutes)
	}
	return out, more, nil
}

// VersionSpan is the completed jobs of one release in a window: when the
// first and last of them were queued, and how many there were.
type VersionSpan struct {
	// Version is UnknownVersion for jobs recorded before releases were
	// stamped.
	Version     string
	First, Last time.Time
	Jobs        int
}

// jobVersionSpansQuery builds JobVersionSpans' statement, latest release
// first so a cut drops the oldest.
func jobVersionSpansQuery(from, to time.Time, exclude []string, limit int) (string, []any) {
	q := `SELECT COALESCE(controller_version, '` + UnknownVersion + `') AS v, MIN(queued_at), MAX(queued_at), COUNT(*)
		FROM jobs WHERE queued_at >= ? AND queued_at < ? AND state = 'completed'`
	args := []any{ms(from), ms(to)}
	if len(exclude) > 0 {
		q += ` AND conclusion NOT IN (` + strings.TrimSuffix(strings.Repeat("?,", len(exclude)), ",") + `)`
		for _, c := range exclude {
			args = append(args, c)
		}
	}
	return q + ` GROUP BY v ORDER BY MAX(queued_at) DESC, v LIMIT ?`, append(args, limit+1)
}

// JobVersionSpans returns the completed jobs queued in [from, to) grouped by
// the release that ran them, latest last job first, at most limit releases;
// more is true when more matched. Jobs with a conclusion in exclude are left
// out of every figure, so a release whose jobs were all cancelled is not
// offered as a side with nothing to compare.
func (s *Store) JobVersionSpans(ctx context.Context, from, to time.Time, exclude []string, limit int) ([]VersionSpan, bool, error) {
	if err := needLimit(limit); err != nil {
		return nil, false, err
	}
	q, args := jobVersionSpansQuery(from, to, exclude, limit)
	rows, err := s.read.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, false, err
	}
	defer rows.Close()
	var out []VersionSpan
	for rows.Next() {
		var v VersionSpan
		var first, last int64
		if err := rows.Scan(&v.Version, &first, &last, &v.Jobs); err != nil {
			return nil, false, err
		}
		v.First, v.Last = at(first), at(last)
		out = append(out, v)
	}
	if err := rows.Err(); err != nil {
		return nil, false, err
	}
	if len(out) > limit {
		return out[:limit], true, nil
	}
	return out, false, nil
}

// meanFleetCapacitySQL averages, over the sampled minutes of a window, the
// slots every host together was taking at that minute.
const meanFleetCapacitySQL = `SELECT AVG(slots) FROM (SELECT SUM(capacity) AS slots
	FROM host_samples WHERE at >= ? AND at < ? GROUP BY at)`

// MeanFleetCapacity is the fleet's slots on offer averaged over the minutes
// in [from, to) that a host sample covers, or nil when none does: no samples
// is not zero slots. It is one figure, so it is worked out in SQL; read into
// Go it would be every host's every minute of the window.
func (s *Store) MeanFleetCapacity(ctx context.Context, from, to time.Time) (*float64, error) {
	var mean sql.NullFloat64
	if err := s.read.QueryRowContext(ctx, meanFleetCapacitySQL, ms(from), ms(to)).Scan(&mean); err != nil {
		return nil, err
	}
	if !mean.Valid {
		return nil, nil
	}
	return &mean.Float64, nil
}

// hostSamplesBetweenSQL reads one host's minutes newest first, so a cut keeps
// the ones nearest the window's end.
const hostSamplesBetweenSQL = `SELECT ` + hostSampleCols + ` FROM host_samples
	WHERE host_id = ? AND at >= ? AND at < ? ORDER BY at DESC LIMIT ?`

// HostSamplesBetween returns one host's samples in [from, to), oldest first,
// at most limit of them; more is true when more matched, and the ones kept
// are the newest. ListHostSamples reads to now and has no limit, which suits
// the Hosts page's day and not an analysis over a month.
func (s *Store) HostSamplesBetween(ctx context.Context, hostID string, from, to time.Time, limit int) ([]HostSample, bool, error) {
	if err := needLimit(limit); err != nil {
		return nil, false, err
	}
	rows, err := s.read.QueryContext(ctx, hostSamplesBetweenSQL, hostID, ms(from), ms(to), limit+1)
	if err != nil {
		return nil, false, err
	}
	defer rows.Close()
	var out []HostSample
	for rows.Next() {
		h, err := scanHostSample(rows)
		if err != nil {
			return nil, false, err
		}
		out = append(out, h)
	}
	if err := rows.Err(); err != nil {
		return nil, false, err
	}
	more := len(out) > limit
	if more {
		out = out[:limit]
	}
	slices.Reverse(out)
	return out, more, nil
}

// ThrottleChange is one step of a host's throttle as the audit log recorded
// it: the host, when, and the rung it stood on after.
//
// It carries no actor and no detail on purpose. An operator's clear is
// written under their name, and the analyses built from these rows are shown
// to anyone who can read jobs or runners, who could not read the audit log
// itself; when a host was throttled is theirs to know, who lifted it is not.
type ThrottleChange struct {
	HostID string
	// Action is host.throttle, host.throttle_lift or host.throttle_clear.
	Action string
	// Level is the rung after the change: zero after a clear, whose detail
	// names the rung that was cleared rather than the one left, and zero for
	// a row whose detail cannot be read.
	Level int
	At    time.Time
}

// throttleLevelSQL is a throttle row's rung after the change. json_valid
// guards json_extract, which fails the whole statement on one malformed
// document.
const throttleLevelSQL = `CASE WHEN action = 'host.throttle_clear' THEN 0
	WHEN json_valid(after) THEN COALESCE(json_extract(after, '$.level'), 0) ELSE 0 END`

// throttleActionsSQL names the three throttle actions. They are written for
// hosts alone, so the action index is the whole test: a condition on
// target_kind would invite the target index, which is every host's audit
// trail since the fleet began.
const throttleActionsSQL = `action IN ('host.throttle', 'host.throttle_lift', 'host.throttle_clear')`

// hostThrottleChangesSQL is two reads in one statement. The first is the
// window's rows newest first, so a cut keeps the ones nearest its end. The
// second is each host's last row before the window, the rung it started the
// window on (SQLite takes a bare column from the row MAX chose); the last
// column tells the two apart, so the limit counts only the window's rows.
const hostThrottleChangesSQL = `SELECT * FROM (SELECT target_id, action, ` + throttleLevelSQL + `, created_at, 0
		FROM audit_events WHERE ` + throttleActionsSQL + ` AND created_at >= ? AND created_at < ?
		ORDER BY created_at DESC, id LIMIT ?)
	UNION ALL
	SELECT target_id, action, ` + throttleLevelSQL + `, MAX(created_at), 1
		FROM audit_events WHERE ` + throttleActionsSQL + ` AND created_at < ?
		GROUP BY target_id`

// HostThrottleChanges returns every host's throttle steps in [from, to),
// oldest first, at most limit of them; more is true when more matched, and
// the ones kept are the newest. Ahead of them, and outside the limit, it
// returns each host's last step before from: a throttle that went on before
// the window and came off after it leaves no row inside, and is otherwise
// invisible once the host's own Throttle.Since has been cleared. There is at
// most one such row a host, so they cannot crowd the window's out.
func (s *Store) HostThrottleChanges(ctx context.Context, from, to time.Time, limit int) ([]ThrottleChange, bool, error) {
	if err := needLimit(limit); err != nil {
		return nil, false, err
	}
	rows, err := s.read.QueryContext(ctx, hostThrottleChangesSQL, ms(from), ms(to), limit+1, ms(from))
	if err != nil {
		return nil, false, err
	}
	defer rows.Close()
	var starting, window []ThrottleChange
	for rows.Next() {
		var c ThrottleChange
		var created int64
		var before bool
		if err := rows.Scan(&c.HostID, &c.Action, &c.Level, &created, &before); err != nil {
			return nil, false, err
		}
		c.At = at(created)
		if before {
			starting = append(starting, c)
		} else {
			window = append(window, c)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, false, err
	}
	more := len(window) > limit
	if more {
		window = window[:limit]
	}
	slices.Reverse(window)
	slices.SortFunc(starting, func(a, b ThrottleChange) int {
		if c := a.At.Compare(b.At); c != 0 {
			return c
		}
		return strings.Compare(a.HostID, b.HostID)
	})
	return append(starting, window...), more, nil
}

// JobOutcome is a finished job as the recurring failures analysis reads it:
// what ran, for which commit, how it ended and which step it stopped at.
type JobOutcome struct {
	ID, Repo, Workflow, JobName, Conclusion string
	HeadSHA, HeadBranch                     string
	// GitHubRunID is the workflow run the job belongs to: attempt numbers count
	// within one run, so a rerun is only another attempt of the same one.
	GitHubRunID int64
	RunAttempt  int
	Steps       JobSteps
	RunnerFault string
	CompletedAt time.Time
}

// JobOutcomes returns the jobs this fleet had a hand in that completed in
// [since, until) with a pass or a failure, newest first, at most limit of
// them; more is true when more matched. Cancelled and skipped jobs stop when
// somebody says so and say nothing about whether the job is sound.
func (s *Store) JobOutcomes(ctx context.Context, since, until time.Time, limit int) (out []JobOutcome, more bool, err error) {
	if err := needLimit(limit); err != nil {
		return nil, false, err
	}
	rows, err := s.read.QueryContext(ctx, `SELECT j.id, j.repo, j.workflow, j.job_name, j.conclusion,
		j.head_sha, j.head_branch, j.github_run_id, j.run_attempt, j.steps, j.runner_fault, j.completed_at
		FROM jobs j
		WHERE j.state = 'completed' AND j.conclusion IN ('success', 'failure')
			AND j.completed_at >= ? AND j.completed_at < ? AND `+managedJobSQL("j")+`
		ORDER BY j.completed_at DESC, j.id ASC LIMIT ?`, ms(since), ms(until), limit+1)
	if err != nil {
		return nil, false, err
	}
	defer rows.Close()
	for rows.Next() {
		var o JobOutcome
		var done int64
		if err := rows.Scan(&o.ID, &o.Repo, &o.Workflow, &o.JobName, &o.Conclusion,
			&o.HeadSHA, &o.HeadBranch, &o.GitHubRunID, &o.RunAttempt, &o.Steps, &o.RunnerFault, &done); err != nil {
			return nil, false, err
		}
		o.CompletedAt = at(done)
		out = append(out, o)
	}
	if err := rows.Err(); err != nil {
		return nil, false, err
	}
	if len(out) > limit {
		return out[:limit], true, nil
	}
	return out, false, nil
}
