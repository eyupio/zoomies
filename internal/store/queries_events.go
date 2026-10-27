package store

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"encoding/base64"
	"errors"
	"fmt"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"time"
)

// ---------------------------------------------------------------------------
// Jobs
// ---------------------------------------------------------------------------

// jobInsertCols are the columns a new job is written with. The usage columns
// are left out: a job has used nothing when it is first seen, and only
// RecordJobUsage and MarkJobOOMKilled ever raise them.
const jobInsertCols = `id, github_job_id, github_run_id, repo, workflow, job_name, labels, state,
	conclusion, installation_id, pool_id, runner_id, runner_name, html_url, queued_at,
	started_at, completed_at, matched, eligible_at, head_branch, head_sha, run_attempt, run_number, steps, runner_fault, fault_kind, provisioning, provision_now, cancel_requested_at,
	controller_version, controller_channel, agent_version, host_id`

const jobCols = jobInsertCols + `, peak_cpus, peak_memory_mb, oom_killed,
	granted_cpus, granted_memory_mb, granted_source,
	size_class, size_reason, size_basis, size_floor_mb, routed_class, routed_note, ran_class,
	cpu_periods, cpu_throttled_periods, output_tail, scratch_peaks, acknowledged_at, acknowledged_by, run_event`

func scanJob(sc interface{ Scan(...any) error }) (*Job, error) {
	var j Job
	var queued int64
	var started, completed, eligible, cancelRequested, acknowledged sql.NullInt64
	var matched, oomKilled int
	var controllerVersion, controllerChannel, agentVersion, hostID sql.NullString
	err := sc.Scan(&j.ID, &j.GitHubJobID, &j.GitHubRunID, &j.Repo, &j.Workflow, &j.JobName,
		&j.Labels, &j.State, &j.Conclusion, &j.InstallationID, &j.PoolID, &j.RunnerID,
		&j.RunnerName, &j.HTMLURL, &queued, &started, &completed, &matched, &eligible,
		&j.HeadBranch, &j.HeadSHA, &j.RunAttempt, &j.RunNumber, &j.Steps, &j.RunnerFault, &j.FaultKind, &j.Provisioning, &j.ProvisionNow,
		&cancelRequested, &controllerVersion, &controllerChannel, &agentVersion, &hostID,
		&j.PeakCPUs, &j.PeakMemoryMB, &oomKilled,
		&j.GrantedCPUs, &j.GrantedMemoryMB, &j.GrantedSource,
		&j.SizeClass, &j.SizeReason, &j.SizeBasis, &j.SizeFloorMB, &j.RoutedClass, &j.RoutedNote, &j.RanClass,
		&j.CPUPeriods, &j.CPUThrottledPeriods, &j.OutputTail, &j.ScratchPeaks,
		&acknowledged, &j.AcknowledgedBy, &j.RunEvent)
	if err != nil {
		return nil, err
	}
	j.ControllerVersion, j.ControllerChannel = controllerVersion.String, controllerChannel.String
	j.AgentVersion, j.HostID = agentVersion.String, hostID.String
	j.QueuedAt = at(queued)
	j.StartedAt, j.CompletedAt, j.EligibleAt = atp(started), atp(completed), atp(eligible)
	j.CancelRequestedAt = atp(cancelRequested)
	j.AcknowledgedAt = atp(acknowledged)
	j.Matched = matched == 1
	j.OOMKilled = oomKilled == 1
	return &j, nil
}

// UpsertJob inserts or updates a job keyed on its GitHub job ID.
//
// Webhook deliveries are at-least-once and can arrive out of order, so this
// merges rather than overwrites: a late "queued" delivery must not resurrect a
// job that has already completed.
func (s *Store) UpsertJob(ctx context.Context, j *Job) (*Job, error) {
	out, _, err := s.ApplyJob(ctx, j)
	return out, err
}

// The statements a job's arrival runs, on every webhook delivery and for every
// job a poller sweep meets: see hot.go.
var (
	jobByGitHubIDQuery = hot(`SELECT ` + jobCols + ` FROM jobs WHERE github_job_id = ?`)
	jobByIDQuery       = hot(`SELECT ` + jobCols + ` FROM jobs WHERE id = ?`)
	insertJobQuery     = hot(`INSERT INTO jobs (` + jobInsertCols + `)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`)
	updateJobQuery = hot(`UPDATE jobs SET github_run_id=?, repo=?, workflow=?,
		job_name=?, labels=?, state=?, conclusion=?, installation_id=?, pool_id=?,
		runner_id=?, runner_name=?, html_url=?, started_at=?, completed_at=?, matched=?,
		eligible_at=?, head_branch=?, head_sha=?, run_attempt=?, run_number=?, steps=?,
		runner_fault=?, fault_kind=?, queued_at=? WHERE id=?`)
	stampJobVersionsQuery = hot(`UPDATE jobs SET
		controller_version = COALESCE(controller_version, NULLIF(?1, '')),
		controller_channel = COALESCE(controller_channel, NULLIF(?2, '')),
		agent_version      = COALESCE(agent_version,      NULLIF(?3, '')),
		host_id            = COALESCE(host_id,            NULLIF(?4, ''))
		WHERE id = ?5`)
	insertJobEventQuery  = hot(`INSERT INTO job_events (` + jobEventCols + `) VALUES (?,?,?,?,?,?,?,?)`)
	runNumberForRunQuery = hot(`SELECT run_number FROM jobs WHERE github_run_id = ? AND run_number != 0 LIMIT 1`)
	insertDeliveryQuery  = hot(`INSERT INTO webhook_deliveries
		(id, delivery_id, event, action, repo, status, error, installation_id, received_at)
		VALUES (?,?,?,?,?,?,?,?,?)`)
)

// jobUpdateArgs are the arguments of updateJobQuery, in its order. They are made
// in one place so that what ApplyJob compares to find it has nothing to write is
// exactly what it would write, and a column added to the statement is compared
// as soon as it is added here.
func jobUpdateArgs(j *Job) []any {
	return []any{
		j.GitHubRunID, j.Repo, j.Workflow, j.JobName, j.Labels, string(j.State),
		j.Conclusion, j.InstallationID, j.PoolID, j.RunnerID, j.RunnerName, j.HTMLURL,
		msp(j.StartedAt), msp(j.CompletedAt), boolInt(j.Matched), msp(j.EligibleAt),
		j.HeadBranch, j.HeadSHA, j.RunAttempt, j.RunNumber, j.Steps,
		j.RunnerFault, j.FaultKind, ms(j.QueuedAt), j.ID,
	}
}

// sameArguments reports whether two lists of arguments bind the same values. A
// JSON document is compared as the text it is written as: a nil list and an
// empty one are the same, and a step's time written in another zone is not,
// because it would be stored differently.
func sameArguments(a, b []any) bool {
	if len(a) != len(b) {
		return false
	}
	// The documents last. Each is encoded to be compared, and a delivery that
	// changes something has usually changed something that costs nothing to see.
	for _, documents := range [...]bool{false, true} {
		for i := range a {
			if _, isDocument := a[i].(driver.Valuer); isDocument == documents && !sameArgument(a[i], b[i]) {
				return false
			}
		}
	}
	return true
}

func sameArgument(a, b any) bool {
	if va, ok := a.(driver.Valuer); ok {
		vb, ok := b.(driver.Valuer)
		if !ok {
			return false
		}
		x, errA := va.Value()
		y, errB := vb.Value()
		return errA == nil && errB == nil && sameArgument(x, y)
	}
	if t := reflect.TypeOf(a); t != nil && !t.Comparable() {
		return reflect.DeepEqual(a, b)
	}
	return a == b
}

// JobCondition keeps an age-based retirement tied to the row it selected.
// A newly learned start or a completed delivery makes that selection obsolete.
type JobCondition struct {
	State     JobState
	StartedAt *time.Time
}

// ApplyJob is UpsertJob that also says what changed.
//
// The change is worked out inside the write transaction, against the row as it
// was, so two deliveries for the same job arriving together cannot both be
// told they moved it: exactly one of them did. That is what lets the caller
// write a job's timeline from deliveries GitHub may send twice.
//
// A delivery that leaves the row as it is writes nothing. Most of what a job is
// sent after its first delivery is that: GitHub sends at least once, and the
// poller finds jobs the webhook already announced.
// Optional conditions are checked against the current row before writing;
// a mismatch leaves it untouched and reports no state change.
func (s *Store) ApplyJob(ctx context.Context, j *Job, conditions ...JobCondition) (*Job, JobChange, error) {
	var out *Job
	var change JobChange
	err := s.tx(ctx, func(tx *sql.Tx) error {
		row := s.hotTx(tx).QueryRow(ctx, jobByGitHubIDQuery, j.GitHubJobID)
		existing, err := scanJob(row)
		switch {
		case errors.Is(err, sql.ErrNoRows):
			if len(conditions) != 0 {
				return ErrConflict
			}
			if j.ID == "" {
				j.ID = NewID(PrefixJob)
			}
			if j.QueuedAt.IsZero() {
				j.QueuedAt = s.Now()
			}
			j.Labels = NormalizeLabels(j.Labels)
			// A job that arrives already claimed is eligible from the moment
			// it is first seen: there was no earlier moment to record.
			if j.Matched && j.State != JobWaiting && j.EligibleAt == nil {
				now := s.Now()
				j.EligibleAt = &now
			}
			_, err := s.hotTx(tx).Exec(ctx, insertJobQuery,
				j.ID, j.GitHubJobID, j.GitHubRunID, j.Repo, j.Workflow, j.JobName, j.Labels,
				string(j.State), j.Conclusion, j.InstallationID, j.PoolID, j.RunnerID,
				j.RunnerName, j.HTMLURL, ms(j.QueuedAt), msp(j.StartedAt), msp(j.CompletedAt),
				boolInt(j.Matched), msp(j.EligibleAt), j.HeadBranch, j.HeadSHA, j.RunAttempt, j.RunNumber,
				j.Steps, j.RunnerFault, j.FaultKind, j.Provisioning, j.ProvisionNow,
				msp(j.CancelRequestedAt), nullText(j.ControllerVersion), nullText(j.ControllerChannel),
				nullText(j.AgentVersion), nullText(j.HostID))
			if err != nil {
				return err
			}
			out = j
			change = JobChange{Created: true, StateChanged: true, Claimed: j.Matched, RunnerLinked: j.RunnerID != ""}
			return nil
		case err != nil:
			return err
		}

		// Merge: never move a job backwards through its lifecycle.
		//
		// A delivery that is current -- at or past the row's state -- may
		// change anything it carries. A stale one -- a "queued" arriving after
		// "completed" -- may only fill in what the row does not have yet: it
		// is old news about the same job, so a label set or a runner name it
		// carries is at best what the row already says and at worst what was
		// true before the job moved on. The steps are the sharpest case: an
		// "in_progress" that arrives after "completed" would put every step
		// back to "queued".
		for _, condition := range conditions {
			sameStart := existing.StartedAt == nil && condition.StartedAt == nil
			if existing.StartedAt != nil && condition.StartedAt != nil {
				sameStart = existing.StartedAt.Equal(*condition.StartedAt)
			}
			if existing.State != condition.State || !sameStart {
				out = existing
				change.PreviousState = existing.State
				return nil
			}
		}
		merged := *existing
		current := jobRank(j.State) >= jobRank(existing.State)
		if current {
			merged.State = j.State
			if len(j.Steps) > 0 {
				merged.Steps = j.Steps
			}
		}
		if existing.State == JobWaiting && merged.State == JobQueued {
			// The deployment review ended and the job has actually entered
			// the queue. Replayed deliveries cannot move this boundary again.
			merged.QueuedAt = s.Now()
		}
		text := func(dst *string, v string) {
			if v != "" && (current || *dst == "") {
				*dst = v
			}
		}
		text(&merged.HeadBranch, j.HeadBranch)
		text(&merged.HeadSHA, j.HeadSHA)
		text(&merged.RunnerFault, j.RunnerFault)
		// Neither of these ever arrives from GitHub -- they are the fleet's own
		// half of a failure, written by SetJobRunnerFault. Merging them the same
		// way as the rest costs nothing and stops a future caller that does set
		// them finding them silently dropped.
		if j.FaultKind != "" && (current || merged.FaultKind == "") {
			merged.FaultKind = j.FaultKind.Normalise()
		}
		text(&merged.Conclusion, j.Conclusion)
		text(&merged.Repo, j.Repo)
		text(&merged.Workflow, j.Workflow)
		text(&merged.JobName, j.JobName)
		text(&merged.InstallationID, j.InstallationID)
		text(&merged.PoolID, j.PoolID)
		text(&merged.RunnerID, j.RunnerID)
		text(&merged.RunnerName, j.RunnerName)
		text(&merged.HTMLURL, j.HTMLURL)
		if j.RunAttempt != 0 && (current || merged.RunAttempt == 0) {
			merged.RunAttempt = j.RunAttempt
		}
		// Never arrives from the webhook itself -- see attachRunNumber -- but
		// merges the same way as everything else GitHub's own record can settle
		// after the fact, so a backfilled value survives the next delivery.
		if j.RunNumber != 0 && (current || merged.RunNumber == 0) {
			merged.RunNumber = j.RunNumber
		}
		if j.GitHubRunID != 0 && (current || merged.GitHubRunID == 0) {
			merged.GitHubRunID = j.GitHubRunID
		}
		if len(j.Labels) > 0 && (current || len(merged.Labels) == 0) {
			merged.Labels = NormalizeLabels(j.Labels)
		}
		if j.StartedAt != nil && merged.StartedAt == nil {
			merged.StartedAt = j.StartedAt
		}
		if j.CompletedAt != nil && (current || merged.CompletedAt == nil) {
			merged.CompletedAt = j.CompletedAt
		}
		merged.Matched = merged.Matched || j.Matched
		// Stamped on the transition, not on every delivery that finds the job
		// matched: this is the moment the fleet could first have acted, and a
		// later delivery saying the same thing is not a new one.
		if merged.Matched && merged.State != JobWaiting && merged.EligibleAt == nil {
			now := s.Now()
			merged.EligibleAt = &now
		}

		// Putting back what the row already holds is a write transaction's worth
		// of work for nothing: a third of a poller sweep over known jobs, and on
		// disk most of what a redelivery cost. The row was read a moment ago under
		// the writer, so nothing can have moved it since. A job that moved on has
		// something to write, which is the one answer that costs nothing to find.
		args := jobUpdateArgs(&merged)
		if merged.State != existing.State || !sameArguments(args, jobUpdateArgs(existing)) {
			if _, err = s.hotTx(tx).Exec(ctx, updateJobQuery, args...); err != nil {
				return err
			}
		}
		out = &merged
		change = JobChange{
			PreviousState: existing.State,
			StateChanged:  merged.State != existing.State,
			Claimed:       !existing.Matched && merged.Matched,
			RunnerLinked:  existing.RunnerID == "" && merged.RunnerID != "",
		}
		return nil
	})
	return out, change, err
}

// JobVersions is what StampJobVersions records: the build that claimed a job
// and the host and agent that ran it. An empty field is "not known yet", not
// a value to store.
type JobVersions struct {
	ControllerVersion string
	ControllerChannel string
	AgentVersion      string
	HostID            string
}

// StampJobVersions records which release handled a job, and returns the job
// as it now is.
//
// Every column is set once and never again: each is written only where it is
// still NULL, inside the statement rather than after a read, so two stamps
// racing -- a webhook and the poller, both seeing the same claim -- leave the
// first one's values. That is also why the controller's pair and the host's
// pair can arrive separately: a job is claimed before it has a host, and the
// second stamp fills in what the first could not without touching it.
func (s *Store) StampJobVersions(ctx context.Context, jobID string, v JobVersions) (*Job, error) {
	var out *Job
	err := s.tx(ctx, func(tx *sql.Tx) error {
		if _, err := s.hotTx(tx).Exec(ctx, stampJobVersionsQuery,
			v.ControllerVersion, v.ControllerChannel, v.AgentVersion, v.HostID, jobID); err != nil {
			return err
		}
		j, err := scanJob(s.hotTx(tx).QueryRow(ctx, jobByIDQuery, jobID))
		if errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("job %s: %w", jobID, ErrNotFound)
		}
		out = j
		return err
	})
	return out, err
}

// StampJobGranted records the size of the runner that took a job, and returns
// the job as it now is.
//
// It is written once. The statement only fills a job whose granted_source is
// still empty, so two observers of the same assignment -- a webhook and the
// poller -- leave the first one's figures, and a runner later resized, or the
// same job re-run, cannot rewrite what this run was given. A source of "" is
// nothing to record and writes nothing: the job is returned as it is.
func (s *Store) StampJobGranted(ctx context.Context, jobID string, cpus float64, memoryMB int64, source string) (*Job, error) {
	var out *Job
	err := s.tx(ctx, func(tx *sql.Tx) error {
		if source != "" {
			if _, err := tx.ExecContext(ctx, `UPDATE jobs SET granted_cpus = ?1, granted_memory_mb = ?2,
				granted_source = ?3 WHERE id = ?4 AND granted_source = ''`, cpus, memoryMB, source, jobID); err != nil {
				return err
			}
		}
		j, err := scanJob(s.hotTx(tx).QueryRow(ctx, jobByIDQuery, jobID))
		if errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("job %s: %w", jobID, ErrNotFound)
		}
		out = j
		return err
	})
	return out, err
}

// SetJobRunnerFault records what the runner executing a job said when it
// stopped before GitHub reported the job over, and returns the job as it now
// is, along with whether this call is the one that recorded the fault.
//
// The message and its kind are written together, in one statement, because
// they are one fact: a row carrying prose with no category would be counted as
// unclassified for ever, and a category with no prose would name a fix for a
// failure nobody can read.
//
// Only the first fault is kept. The agent may report the same exit more than
// once -- a runner report and then a task result -- and the first message is
// the one closest to the event. The flag is what lets the caller write the
// timeline entry and count the metric exactly once, decided inside the write
// rather than by a read that two reports could both make first.
func (s *Store) SetJobRunnerFault(ctx context.Context, jobID, fault string, kind FaultKind) (*Job, bool, error) {
	var out *Job
	var recorded bool
	if kind = kind.Normalise(); kind == "" {
		kind = FaultRunnerExited
	}
	err := s.tx(ctx, func(tx *sql.Tx) error {
		res, err := tx.ExecContext(ctx,
			`UPDATE jobs SET runner_fault=?, fault_kind=? WHERE id=? AND runner_fault='' AND fault_kind=''`,
			fault, kind, jobID)
		if err != nil {
			return err
		}
		if n, err := res.RowsAffected(); err == nil && n > 0 {
			recorded = true
		}
		j, err := scanJob(s.hotTx(tx).QueryRow(ctx, jobByIDQuery, jobID))
		if errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("job %s: %w", jobID, ErrNotFound)
		}
		out = j
		return err
	})
	return out, recorded, err
}

// ---------------------------------------------------------------------------
// Job timeline
// ---------------------------------------------------------------------------

const jobEventCols = `id, job_id, at, kind, source, message, runner_id, runner_name`

// AppendJobEvent adds one entry to a job's timeline.
func (s *Store) AppendJobEvent(ctx context.Context, e *JobEvent) error {
	if e.ID == "" {
		e.ID = NewID(PrefixJobEvent)
	}
	if e.At.IsZero() {
		e.At = s.Now()
	}
	_, err := s.execHot(ctx, insertJobEventQuery,
		e.ID, e.JobID, ms(e.At), string(e.Kind), e.Source, e.Message, e.RunnerID, e.RunnerName)
	return err
}

// ListJobEvents returns a job's timeline, oldest first.
func (s *Store) ListJobEvents(ctx context.Context, jobID string) ([]*JobEvent, error) {
	// Two entries written in the same millisecond -- "queued" and then
	// "claimed", every time -- keep the order they were written in, which
	// the rowid records and a random ID would not.
	rows, err := s.read.QueryContext(ctx, `SELECT `+jobEventCols+` FROM job_events
		WHERE job_id = ? ORDER BY at, rowid`, jobID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*JobEvent
	for rows.Next() {
		var e JobEvent
		var at64 int64
		if err := rows.Scan(&e.ID, &e.JobID, &at64, &e.Kind, &e.Source, &e.Message, &e.RunnerID, &e.RunnerName); err != nil {
			return nil, err
		}
		e.At = at(at64)
		out = append(out, &e)
	}
	return out, rows.Err()
}

// jobRank orders job states so a stale webhook cannot rewind one.
func jobRank(s JobState) int {
	switch s {
	case JobWaiting:
		return 1
	case JobQueued:
		return 2
	case JobInProgress:
		return 3
	case JobCompleted:
		return 4
	}
	return 0
}

// GetJob returns one job by internal ID.
func (s *Store) GetJob(ctx context.Context, id string) (*Job, error) {
	row := s.read.QueryRowContext(ctx, `SELECT `+jobCols+` FROM jobs WHERE id = ?`, id)
	j, err := scanJob(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("job %s: %w", id, ErrNotFound)
	}
	return j, err
}

// GetJobByGitHubID returns one job by the ID GitHub assigned it.
func (s *Store) GetJobByGitHubID(ctx context.Context, ghID int64) (*Job, error) {
	row := s.read.QueryRowContext(ctx, `SELECT `+jobCols+` FROM jobs WHERE github_job_id = ?`, ghID)
	j, err := scanJob(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("job %d: %w", ghID, ErrNotFound)
	}
	return j, err
}

// RunNumberForRun returns a run number some other job of the same workflow
// run has already recorded, so a matrix's second job does not have to ask
// GitHub again for what its first job already learned. ErrNotFound means no
// job of this run has one yet.
func (s *Store) RunNumberForRun(ctx context.Context, runID int64) (int64, error) {
	if runID == 0 {
		return 0, ErrNotFound
	}
	var n int64
	err := s.hotRead().QueryRow(ctx, runNumberForRunQuery, runID).Scan(&n)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, ErrNotFound
	}
	return n, err
}

// SetRunNumberForRun records a workflow run's number on every job of it that
// does not have one yet. A run's jobs arrive as separate deliveries, often
// before the one lookup of the run's number has come back, so the number is
// written to the whole run rather than only to the job that asked.
func (s *Store) SetRunNumberForRun(ctx context.Context, repo string, runID, number int64) error {
	if runID == 0 || number == 0 {
		return nil
	}
	return s.tx(ctx, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx,
			`UPDATE jobs SET run_number=? WHERE repo=? AND github_run_id=? AND run_number=0`, number, repo, runID)
		return err
	})
}

// SetRunEventForRun records the event that started a workflow run on every job
// of it. Unlike the run number it is overwritten, because the answer is
// GitHub's and a later read is as good as the first.
func (s *Store) SetRunEventForRun(ctx context.Context, repo string, runID int64, event string) error {
	if runID == 0 || event == "" {
		return nil
	}
	return s.tx(ctx, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx,
			`UPDATE jobs SET run_event=? WHERE repo=? AND github_run_id=? AND run_event!=?`, event, repo, runID, event)
		return err
	})
}

// ListJobsForRun returns every locally known job in one repository workflow
// run. Repository is part of the key because run IDs are not globally unique.
func (s *Store) ListJobsForRun(ctx context.Context, repo string, runID int64) ([]*Job, error) {
	rows, err := s.read.QueryContext(ctx, `SELECT `+jobCols+` FROM jobs
		WHERE repo = ? AND github_run_id = ? ORDER BY queued_at, id`, repo, runID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*Job
	for rows.Next() {
		j, err := scanJob(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, j)
	}
	return out, rows.Err()
}

// JobFilter narrows a job history listing. Every field is optional; the UI's
// filter bar maps one-to-one onto it.
type JobFilter struct {
	Provisioning []string
	Branches     []string

	Repos     []string
	Workflows []string
	// RunIDs narrows to the jobs of particular workflow runs, by GitHub's run
	// ID. It is how the Workflows page opens a run to the jobs inside it; sent
	// with the repository as well, because a run ID keys a run only within one.
	RunIDs []int64
	// HeadSHA keeps the jobs that ran one commit. A repair started from a pull
	// request alone has no run to name, and the commit its checks failed on is
	// what ties the jobs to it; sent with the repository, because two
	// repositories can share a commit.
	HeadSHA     string
	PoolIDs     []string
	RunnerIDs   []string
	States      []JobState
	Conclusions []string
	// ExcludeConclusions leaves out the jobs with one of these conclusions,
	// from every count and figure as well as from a listing. A comparison
	// leaves cancelled and skipped jobs out this way: they stop when somebody
	// says so, and a release whose users cancelled more would otherwise look
	// faster and more reliable for it.
	ExcludeConclusions []string
	Labels             []string
	Search             string
	// Since is inclusive and Until exclusive: a job queued exactly at Since is
	// in, one queued exactly at Until is out. Half-open so that two windows
	// cut at the same instant -- the last 30 days and the 30 before -- share
	// no job and leave none out.
	Since *time.Time
	Until *time.Time
	// ControllerVersions keeps the jobs stamped with one of these controller
	// versions. UnknownVersion matches the jobs that were never stamped.
	ControllerVersions []string
	// HostIDs keeps the jobs that ran on one of these hosts, as stamped.
	HostIDs []string
	// JobNames is an exact match on the job's name, unlike Search, which
	// finds a substring of it, the repository, the workflow or the runner.
	JobNames []string
	// Sizes keeps the jobs whose runner was this size, written as the size
	// group names it -- "3 CPU / 8 GB" -- with UnknownVersion matching the jobs
	// no size was recorded for.
	Sizes []string
	// Hosted narrows by whether every label names somebody else's runners:
	// true keeps only those, false keeps only jobs with a label that does not.
	// Nil is both.
	Hosted *bool
	// UnmatchedOnly surfaces queued jobs that no pool claims. It is deliberately
	// narrower than "matched = 0" in two ways, and both are the same mistake:
	// reporting a fleet-wide fault every time somebody keeps one repository on
	// runners this controller does not own. A job that has already started or
	// finished was run by something -- another fleet, a hosted-runner vendor,
	// GitHub itself -- and a job whose labels all name somebody else's runners
	// is about to be, however long it sits queued. What is left is the job that
	// asked this fleet for something no pool here offers, which is the whole
	// question the filter exists to answer.
	UnmatchedOnly bool
	// ManagedOnly narrows the list to jobs this controller has a hand in: one an
	// enabled pool claimed, or one that ran on a runner this fleet started.
	// GitHub tells us about every job in an installed repository, most of which
	// are somebody else's hosted runners, and a Jobs page that mixes the two
	// answers "why is my fleet slow?" with somebody else's numbers. Queued jobs
	// no pool claims stay in: nothing ran them, so they are this fleet's
	// problem to see, which is also why UnmatchedOnly wins over this flag.
	ManagedOnly bool
	// FailedOnly keeps the jobs that went wrong, on either side: a conclusion
	// GitHub counts as a failure, or a runner that stopped under the job. A
	// job whose runner died while GitHub still thinks it is running is
	// included -- that is the case an operator most wants to find.
	FailedOnly bool
	// AttentionOnly keeps the failures that still want a person: recent,
	// not superseded by a later attempt of the same job, and not yet
	// acknowledged. It is the whole question behind the Jobs page's "Needs
	// attention" view and the Overview's failures panel, answered in one
	// place so the two cannot disagree about what is still owed.
	AttentionOnly bool
	// EliRepairableOnly keeps the jobs of a run a pull request started, or whose
	// trigger has not been read yet. Eli pushes one commit to an existing PR's
	// branch and never raises one, so a failed push, merge or scheduled run is
	// not something it can be asked to fix. This is a repair discovery filter,
	// not a filter for failures that still need a person.
	EliRepairableOnly bool
	// FaultedOnly keeps only the jobs whose runner stopped under them. This is
	// the fleet's own failure rate, as distinct from the workflows'.
	FaultedOnly bool
	// WorkflowFailedOnly is the other half: jobs GitHub failed with nothing
	// wrong on this side. It is the list to send somebody who has been told
	// "CI is broken" and needs to know whether that is true of their code.
	WorkflowFailedOnly bool
	// FaultKinds narrows a fleet failure to particular categories, which is
	// how "show me every job we lost to memory this week" is asked. An unknown
	// kind matches nothing rather than everything: a filter that silently
	// widened would be read as a fleet in better shape than it is.
	FaultKinds []FaultKind
	// Cancelling narrows by whether a cancellation has been asked of GitHub
	// and not yet confirmed -- see Job.Cancelling. Nil is every job, which is
	// what a history wants; false is what a list of work still in hand wants,
	// because a cancelled job is neither waiting nor running.
	Cancelling *bool
}

var jobSortCols = map[string]string{
	"queued_at":    "queued_at",
	"started_at":   "started_at",
	"completed_at": "completed_at",
	"repo":         "repo",
	"workflow":     "workflow",
	"state":        "state",
	"duration":     "(COALESCE(completed_at,0) - COALESCE(started_at,0))",
	"queue_wait":   "(COALESCE(started_at,0) - queued_at)",
}

// ListJobs returns a filtered page of jobs and the matching total.
func (s *Store) ListJobs(ctx context.Context, f JobFilter, p Page) ([]*Job, int, error) {
	where, args := jobWhere(f, s.Now())
	var total int
	if err := s.read.QueryRowContext(ctx, `SELECT COUNT(*) FROM jobs `+where, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	q := `SELECT ` + jobCols + ` FROM jobs ` + where +
		` ORDER BY ` + p.orderBy(jobSortCols, "queued_at DESC") + `, id ASC LIMIT ? OFFSET ?`
	args = append(args, p.limit(50, 500), max(p.Offset, 0))
	rows, err := s.read.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	var out []*Job
	for rows.Next() {
		j, err := scanJob(rows)
		if err != nil {
			return nil, 0, err
		}
		out = append(out, j)
	}
	return out, total, rows.Err()
}

// UnknownVersion is what a job that was never stamped with a release is
// called wherever a release is named: as a filter value, and as the group the
// job statistics put those jobs in.
const UnknownVersion = "unknown"

// JobCursor is a place in the job listing: the last job a page returned. The
// next page starts after it, so a job queued while somebody is paging cannot
// push a row from one page onto the next, which an offset would.
type JobCursor struct {
	QueuedAt time.Time
	ID       string
}

// ErrInvalidCursor is a cursor this build did not issue.
var ErrInvalidCursor = errors.New("store: not a job cursor")

// Encode renders the cursor as an opaque token. It is opaque so that callers
// hand it back rather than build one, which leaves room to change what is in it.
func (c JobCursor) Encode() string {
	return base64.RawURLEncoding.EncodeToString([]byte(strconv.FormatInt(ms(c.QueuedAt), 10) + "." + c.ID))
}

// ParseJobCursor reads a token Encode wrote.
func ParseJobCursor(token string) (JobCursor, error) {
	raw, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil {
		return JobCursor{}, ErrInvalidCursor
	}
	at, id, ok := strings.Cut(string(raw), ".")
	n, perr := strconv.ParseInt(at, 10, 64)
	if !ok || perr != nil || id == "" {
		return JobCursor{}, ErrInvalidCursor
	}
	return JobCursor{QueuedAt: time.UnixMilli(n), ID: id}, nil
}

// ListJobsAfter reads the listing's default order -- newest queued first, ties
// by ID -- a page at a time by keyset rather than by offset. after is the last
// job of the previous page, or nil for the first. It returns the matching
// total, which ignores the cursor, and the cursor for the next page, nil once
// the listing has run out.
//
// The page is read one row long. A next cursor that promised a page it could
// not fill would send a caller round once more for nothing.
func (s *Store) ListJobsAfter(ctx context.Context, f JobFilter, after *JobCursor, limit int) ([]*Job, int, *JobCursor, error) {
	where, args := jobWhere(f, s.Now())
	var total int
	if err := s.read.QueryRowContext(ctx, `SELECT COUNT(*) FROM jobs `+where, args...).Scan(&total); err != nil {
		return nil, 0, nil, err
	}
	if after != nil {
		keyset := `(queued_at < ? OR (queued_at = ? AND id > ?))`
		if where == "" {
			where = "WHERE " + keyset
		} else {
			where += " AND " + keyset
		}
		args = append(args, ms(after.QueuedAt), ms(after.QueuedAt), after.ID)
	}
	limit = min(max(limit, 1), 500)
	rows, err := s.read.QueryContext(ctx, `SELECT `+jobCols+` FROM jobs `+where+
		` ORDER BY queued_at DESC, id ASC LIMIT ?`, append(args, limit+1)...)
	if err != nil {
		return nil, 0, nil, err
	}
	defer rows.Close()
	var out []*Job
	for rows.Next() {
		j, err := scanJob(rows)
		if err != nil {
			return nil, 0, nil, err
		}
		out = append(out, j)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, nil, err
	}
	var next *JobCursor
	if len(out) > limit {
		out = out[:limit]
		last := out[len(out)-1]
		next = &JobCursor{QueuedAt: last.QueuedAt, ID: last.ID}
	}
	return out, total, next, nil
}

// jobStateTerm is the condition that keeps the jobs in one of states, and the
// states it is asked of.
//
// A list that includes completed, the state nearly every job in a history is in,
// and another state besides is written with a unary plus, which tells SQLite not
// to read the term through an index. Left to itself it does, because a state is
// something an index can seek; but a list like that narrows almost nothing, and
// having sought each state SQLite has most of the table in hand and still has to
// put it in order, which it does by reading every row whole and sorting. The
// listing is ordered by queued_at, and the index that serves that order,
// idx_jobs_queued_id, stops after the page's fifty rows when the states are only
// a test each row passes. That was 61 ms against 5.7 ms for the Jobs page's All
// view over 10,000 jobs, and the first grows with the table where the second
// grows with its count.
//
// Every other list is left to the index. Without completed the states are few,
// an index finds them at once, and walking the whole history for the rare job
// that is in one would cost far more than the plus saves. Completed alone is the
// state index too: it reads that in queued order with no sort of the rows, and
// with a window of dates it reads only the window.
func jobStateTerm(states []JobState) (string, []any) {
	var distinct []JobState
	for _, st := range states {
		if !slices.Contains(distinct, st) {
			distinct = append(distinct, st)
		}
	}
	ph := make([]string, len(distinct))
	args := make([]any, len(distinct))
	for i, st := range distinct {
		ph[i], args[i] = "?", string(st)
	}
	term := `state IN (` + strings.Join(ph, ",") + `)`
	if len(distinct) > 1 && slices.Contains(distinct, JobCompleted) {
		term = "+" + term
	}
	return term, args
}

func jobWhere(f JobFilter, now time.Time) (string, []any) {
	var cond []string
	var args []any
	inClause := func(col string, vals []string) {
		if len(vals) == 0 {
			return
		}
		ph := make([]string, len(vals))
		for i, v := range vals {
			ph[i] = "?"
			args = append(args, v)
		}
		cond = append(cond, col+` IN (`+strings.Join(ph, ",")+`)`)
	}
	inClause("head_branch", f.Branches)
	if len(f.Provisioning) > 0 {
		var states []string
		for _, v := range f.Provisioning {
			switch v {
			case "ready":
				states = append(states, "(provisioning = '' AND provision_now = 0)")
			case "expedited":
				states = append(states, "(provisioning = '' AND provision_now = 1)")
			case ProvisioningPaused, ProvisioningDeleted:
				states = append(states, "provisioning = ?")
				args = append(args, v)
			}
		}
		if len(states) > 0 {
			cond = append(cond, "("+strings.Join(states, " OR ")+")")
		}
	}
	inClause("repo", f.Repos)
	inClause("workflow", f.Workflows)
	if len(f.RunIDs) > 0 {
		ph := make([]string, len(f.RunIDs))
		for i, id := range f.RunIDs {
			ph[i] = "?"
			args = append(args, id)
		}
		cond = append(cond, `github_run_id IN (`+strings.Join(ph, ",")+`)`)
	}
	if f.HeadSHA != "" {
		cond = append(cond, `head_sha = ?`)
		args = append(args, f.HeadSHA)
	}
	inClause("pool_id", f.PoolIDs)
	inClause("runner_id", f.RunnerIDs)
	inClause("conclusion", f.Conclusions)
	if len(f.ExcludeConclusions) > 0 {
		cond = append(cond, `conclusion NOT IN (`+strings.TrimSuffix(strings.Repeat("?,", len(f.ExcludeConclusions)), ",")+`)`)
		for _, c := range f.ExcludeConclusions {
			args = append(args, c)
		}
	}
	inClause("job_name", f.JobNames)
	inClause("host_id", f.HostIDs)
	inClause("("+sizeSQL+")", f.Sizes)
	if len(f.ControllerVersions) > 0 {
		var known []string
		unknown := false
		for _, v := range f.ControllerVersions {
			if v == UnknownVersion {
				unknown = true
			} else {
				known = append(known, v)
			}
		}
		var alt []string
		if unknown {
			alt = append(alt, "controller_version IS NULL")
		}
		if len(known) > 0 {
			ph := make([]string, len(known))
			for i, v := range known {
				ph[i] = "?"
				args = append(args, v)
			}
			alt = append(alt, "controller_version IN ("+strings.Join(ph, ",")+")")
		}
		cond = append(cond, "("+strings.Join(alt, " OR ")+")")
	}
	if f.Hosted != nil {
		if *f.Hosted {
			cond = append(cond, hostedJobSQL("jobs"))
		} else {
			cond = append(cond, "NOT "+hostedJobSQL("jobs"))
		}
	}
	if len(f.States) > 0 {
		term, stateArgs := jobStateTerm(f.States)
		cond = append(cond, term)
		args = append(args, stateArgs...)
	}
	// Labels are stored as a JSON array; a LIKE on the quoted label is exact
	// because NormalizeLabels guarantees no embedded quotes and likeEscape
	// keeps an underscore in the label meaning an underscore.
	for _, l := range NormalizeLabels(f.Labels) {
		cond = append(cond, `labels LIKE ? ESCAPE '\'`)
		args = append(args, `%"`+likeEscape(l)+`"%`)
	}
	if f.Since != nil {
		cond = append(cond, `queued_at >= ?`)
		args = append(args, ms(*f.Since))
	}
	if f.Until != nil {
		cond = append(cond, `queued_at < ?`)
		args = append(args, ms(*f.Until))
	}
	if f.UnmatchedOnly {
		cond = append(cond, `matched = 0 AND state = ? AND NOT `+hostedJobSQL("jobs"))
		args = append(args, string(JobQueued))
	} else if f.ManagedOnly {
		cond = append(cond, managedJobSQL("jobs"))
	}
	switch {
	case f.FaultedOnly:
		cond = append(cond, fleetFailedJobSQL())
	case f.WorkflowFailedOnly:
		cond = append(cond, workflowFailedJobSQL())
	case f.FailedOnly:
		cond = append(cond, failedJobSQL())
	}
	if f.AttentionOnly {
		// The three halves of "still wants a person", together rather than
		// as separate filters a caller could compose wrongly: the failure
		// itself, nobody having said they have seen it, and nothing newer
		// having replaced it. The window is read from the store's clock, so
		// a test's injected one decides what "recent" means.
		cond = append(cond, failedJobSQL(), `acknowledged_at IS NULL`, attentionJobSQL(), `queued_at >= ?`)
		args = append(args, ms(now.Add(-AttentionWindow)))
	}
	if f.EliRepairableOnly {
		cond = append(cond, `run_event IN ('', 'pull_request', 'pull_request_target')`)
	}
	if f.Cancelling != nil {
		if *f.Cancelling {
			cond = append(cond, `cancel_requested_at IS NOT NULL AND state != ?`)
		} else {
			cond = append(cond, `(cancel_requested_at IS NULL OR state = ?)`)
		}
		args = append(args, string(JobCompleted))
	}
	if len(f.FaultKinds) > 0 {
		placeholders := make([]string, 0, len(f.FaultKinds))
		for _, k := range f.FaultKinds {
			placeholders = append(placeholders, "?")
			args = append(args, string(k))
		}
		cond = append(cond, `fault_kind IN (`+strings.Join(placeholders, ",")+`)`)
	}
	if q := strings.TrimSpace(f.Search); q != "" {
		cond = append(cond, `(repo LIKE ? ESCAPE '\' OR workflow LIKE ? ESCAPE '\' OR job_name LIKE ? ESCAPE '\' OR runner_name LIKE ? ESCAPE '\')`)
		like := likePattern(q)
		args = append(args, like, like, like, like)
	}
	if len(cond) == 0 {
		return "", args
	}
	return "WHERE " + strings.Join(cond, " AND "), args
}

var listQueuedJobsQuery = hot(`SELECT ` + jobCols + ` FROM jobs
	WHERE ` + queuedJobSQL("jobs") + ` ORDER BY queued_at`)

// ListQueuedJobs returns jobs still waiting for a runner here, oldest first.
// This is the scheduler's demand signal, and the source of every figure that
// answers "how much work is waiting?": the Overview's tiles and pool bars, the
// queue depth and age gauges, the elastic reserve, and the problems that only
// matter while somebody is queued behind them.
//
// A job an operator removed from the queue is left out, because none of those
// questions is still about it -- see Job.RemovedFromQueue. The sweep that
// retires jobs GitHub stopped talking about wants the rows regardless of what
// an operator did with them, and asks for them with ListStaleQueuedJobs.
func (s *Store) ListQueuedJobs(ctx context.Context) ([]*Job, error) {
	rows, err := s.hotRead().Query(ctx, listQueuedJobsQuery)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*Job
	for rows.Next() {
		j, err := scanJob(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, j)
	}
	return out, rows.Err()
}

// ListHeldJobs returns the jobs GitHub is holding for a deployment review,
// oldest first. They are not demand -- nothing here can start one until it is
// approved -- but each is classed as it arrives, and a pin has to reach it before
// the approval does, not after.
func (s *Store) ListHeldJobs(ctx context.Context) ([]*Job, error) {
	rows, err := s.read.QueryContext(ctx, `SELECT `+jobCols+` FROM jobs
		WHERE state = '`+string(JobWaiting)+`' AND provisioning != '`+ProvisioningDeleted+`' ORDER BY queued_at`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*Job
	for rows.Next() {
		j, err := scanJob(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, j)
	}
	return out, rows.Err()
}

// ListStaleQueuedJobs returns every job still marked queued that GitHub queued
// before the cutoff, oldest first, whatever an operator has done with it.
//
// Removed jobs are here and nowhere else. Removing one suppresses this fleet's
// demand for it; it says nothing about whether GitHub still has the work. Left
// out of this sweep they would never be retired at all, and the Queue's own
// view of what was removed would fill up with work GitHub gave up on a year
// ago.
func (s *Store) ListStaleQueuedJobs(ctx context.Context, before time.Time) ([]*Job, error) {
	rows, err := s.read.QueryContext(ctx, `SELECT `+jobCols+` FROM jobs
		WHERE state = ? AND queued_at < ? ORDER BY queued_at`, string(JobQueued), ms(before))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*Job
	for rows.Next() {
		j, err := scanJob(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, j)
	}
	return out, rows.Err()
}

// ListRunningJobsForRunner returns the jobs GitHub says are executing on one
// runner, which is how a caller asks whether a workload still has work in it.
//
// The runner row cannot answer that: it clears current_job_id the moment the
// runner goes terminal, so a runner failed as lost has forgotten what it was
// running by the time anybody needs to know. The job keeps its side of the
// link, so the question is asked from here.
//
// The equality on state is load-bearing, and not only as a filter. There is no
// index on runner_id and there should not be one for this: the state predicate
// is what lets the planner seek idx_jobs_state_queued straight to the
// in-progress rows -- of which a fleet has at most as many as it has busy
// runners -- and scan runner_id across that handful. Drop it and the query
// walks a jobs table that grows without bound, on every heartbeat.
// runningJobsForRunnerSQL is a const so the query-plan test pins the statement
// that actually ships rather than a copy of it that can drift away from it.
var runningJobsForRunnerSQL = `SELECT ` + jobCols + ` FROM jobs
	WHERE state = 'in_progress' AND runner_id = ? ORDER BY queued_at`

func (s *Store) ListRunningJobsForRunner(ctx context.Context, runnerID string) ([]*Job, error) {
	if runnerID == "" {
		return nil, nil
	}
	rows, err := s.read.QueryContext(ctx, runningJobsForRunnerSQL, runnerID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*Job
	for rows.Next() {
		j, err := scanJob(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, j)
	}
	return out, rows.Err()
}

// runnersWithAJobQuery runs on every scheduling pass, and the equality on state
// is what lets the planner seek an index that leads with state, as it does for
// ListRunningJobsForRunner, rather than walk the whole jobs table. The
// query-plan test reads the statement that ships from here.
var runnersWithAJobQuery = hot(`SELECT DISTINCT runner_id FROM jobs
	WHERE state = 'in_progress' AND runner_id != ''`)

// RunnersWithAJob returns, by ID, every runner GitHub says a job is executing
// on: ListRunningJobsForRunner's question asked of the whole fleet at once, for
// the scheduler, which decides from one snapshot and cannot ask runner by
// runner. Like that question it is read from the jobs, because a runner failed
// as lost has forgotten its job.
//
// A job whose run an operator cancelled still counts, unlike in runningJobSQL.
// Until GitHub says the job has ended, the container is where the cancellation
// is carried out, including any steps that run when a job is cancelled, and
// this is the answer the late-report path gets about the same container: the
// reap must never take back a workload that path decided to keep.
func (s *Store) RunnersWithAJob(ctx context.Context) (map[string]bool, error) {
	rows, err := s.hotRead().Query(ctx, runnersWithAJobQuery)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]bool{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out[id] = true
	}
	return out, rows.Err()
}

// JobDistinct returns the distinct values of a filterable column, for
// populating the job filter's dropdowns without a full table scan client-side.
func (s *Store) JobDistinct(ctx context.Context, column string, limit int) ([]string, error) {
	allowed := map[string]bool{"repo": true, "workflow": true, "conclusion": true}
	if !allowed[column] {
		return nil, fmt.Errorf("store: %q is not a filterable job column", column)
	}
	if limit <= 0 || limit > 500 {
		limit = 200
	}
	rows, err := s.read.QueryContext(ctx,
		`SELECT DISTINCT `+column+` FROM jobs WHERE `+column+` != '' ORDER BY 1 LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var v string
		if err := rows.Scan(&v); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

// JobStats summarises a time window for the Overview cards.
type JobStats struct {
	Queued        int `json:"queued"`
	Running       int `json:"running"`
	CompletedLast int `json:"completed"`
	// The four ways a completed job ended, which add up to CompletedLast.
	// Succeeded is a success GitHub reported with no fault of the fleet's;
	// Failed is FailedConclusions or a runner fault; Cancelled is a person or
	// a rule stopping it; Unknown is everything else, including a job GitHub
	// stopped reporting and one with no conclusion at all, because a rate
	// that counted those as successes would be a rate nobody should trust.
	Succeeded int `json:"succeeded"`
	Failed    int `json:"failed"`
	// FleetFailed is the part of Failed this fleet caused: a runner that never
	// started, or one that stopped under the job. It is a subset rather than a
	// fifth outcome, because GitHub's answer for such a job is still "failure"
	// and a split that did not add up would be a worse lie than no split.
	//
	// It is the number the whole taxonomy exists for. "Eleven failures this
	// hour" is a question; "eleven failures, nine of them ours" is an answer,
	// and it sends a different person to a different page.
	FleetFailed  int           `json:"fleet_failed"`
	Cancelled    int           `json:"cancelled"`
	Unknown      int           `json:"unknown"`
	MedianWait   time.Duration `json:"-"`
	P95Wait      time.Duration `json:"-"`
	MedianWaitMS int64         `json:"median_wait_ms"`
	P95WaitMS    int64         `json:"p95_wait_ms"`
}

// StatsSince computes queue and outcome statistics over a rolling window.
// failedJobSQL is the SQL spelling of Job.Failed, built from the same list, so
// the Overview's failed count and the Jobs page's failed filter cannot drift
// apart again: one used to count "failure" alone while the other added the
// timeouts and the runner faults, and the tile said 1 where the page showed 3.
// managedJobSQL is the predicate behind "jobs this fleet has a hand in": one an
// enabled pool claimed, one that ran on a runner this fleet started, or one
// still queued that no pool claims -- unclaimed rather than somebody else's,
// and the fault the Jobs page exists to show.
//
// It is a function rather than two copies because the Jobs list and the
// Overview have to mean the same thing by it. A page that says four jobs are
// queued beside a list showing five is worse than either number alone.
//
// The last clause is the interesting one, and it covers two states rather than
// one. A job nothing has claimed is kept when nothing has run it either, which
// is true while it is queued and equally true while GitHub holds it for a
// deployment review -- a held job cannot be claimed yet, because the claim
// happens on the approval. Keeping only `queued` hid every held job from the
// page by default, so the operator whose deploy was waiting on a review could
// not find it at all, and the drawer's explanation for one was unreachable.
//
// A queued job whose labels all name somebody else's runners is the exception
// to that clause: nothing here has run it, and nothing here ever will, because
// GitHub or a vendor is about to. Those jobs used to arrive on the page in the
// seconds before they started -- badged as hosted elsewhere, in a view whose
// switch for other runners was off -- so an organisation part-way through a
// migration saw a queue that was not its own and could not turn it off.
func managedJobSQL(jobs string) string {
	return `(` + jobs + `.matched = 1 OR ` + jobs + `.pool_id != '' OR ` + jobs + `.runner_id != ''
		OR (` + jobs + `.matched = 0 AND ` + jobs + `.state IN ('` + string(JobQueued) + `','` +
		string(JobWaiting) + `') AND NOT ` + hostedJobSQL(jobs) + `))`
}

// queuedJobSQL is the SQL spelling of "still waiting for a runner here", and
// the counting half of Job.RemovedFromQueue. A job an operator removed from
// the queue is queued at GitHub and not here: the row keeps its state because
// GitHub keeps offering the work, and the removal is restorable, but nothing
// that reports queue depth should go on counting it. The Overview used to,
// which meant an operator could empty the queue and watch the tile above it
// hold the number it had before.
func queuedJobSQL(jobs string) string {
	return `(` + jobs + `.state = '` + string(JobQueued) + `' AND ` +
		jobs + `.provisioning != '` + ProvisioningDeleted + `' AND ` +
		notCancellingSQL(jobs) + `)`
}

// runningJobSQL is the same question for work in progress, and the counting
// half of Job.Cancelling on that side. A job whose run was cancelled has had
// its runner taken away already; GitHub's completion delivery is all that is
// outstanding, and counting it as running until that arrives reported work
// nobody was doing.
func runningJobSQL(jobs string) string {
	return `(` + jobs + `.state = '` + string(JobInProgress) + `' AND ` + notCancellingSQL(jobs) + `)`
}

// notCancellingSQL is the SQL spelling of the second half of Job.Cancelling.
// The state test lives in the callers, which are all about one state.
func notCancellingSQL(jobs string) string { return jobs + `.cancel_requested_at IS NULL` }

func failedJobSQL() string {
	return `(` + failedConclusionSQL() + ` OR ` + fleetFailedJobSQL() + `)`
}

func failedConclusionSQL() string {
	quoted := make([]string, len(FailedConclusions))
	for i, c := range FailedConclusions {
		quoted[i] = "'" + c + "'"
	}
	return `conclusion IN (` + strings.Join(quoted, ",") + `)`
}

// fleetFailedJobSQL and workflowFailedJobSQL are the SQL spellings of
// Job.FleetFailed and Job.WorkflowFailed. They exist here, beside the count
// that uses them, for the same reason failedJobSQL does: the Overview's split
// and the Jobs page's two filters have to mean the same thing by "ours", or
// the tile and the list it links to disagree about whose bad afternoon it was.
//
// Both halves of the fleet predicate are tested, as in the Go method: a row
// written before the category existed carries only the message.
func fleetFailedJobSQL() string { return `(runner_fault != '' OR fault_kind != '')` }

func workflowFailedJobSQL() string {
	return `(NOT ` + fleetFailedJobSQL() + ` AND ` + failedConclusionSQL() + `)`
}

// AttentionWindow is how recent a failure has to be to still ask for a person.
// A week is long enough to survive a weekend or a short holiday and short
// enough that an inbox nobody has ever emptied does not open on a year of
// history. A failure older than this is not forgotten: it is still in the
// failed view, and its run's own page still tells its story.
const AttentionWindow = 7 * 24 * time.Hour

// attentionJobSQL keeps the unresolved inbox separate from failure history.
// New attempts and runs replace the same job on a known branch; a cancelled,
// skipped or neutral result is not evidence of a fix. GitHub observations can
// also settle a run, but only failures older than that observation, so a new
// failure is never hidden by a previous passing result. Missing run or branch
// metadata is not a licence to guess which failure a newer job replaces.
func attentionJobSQL() string {
	return unreplacedJobSQL() + ` AND NOT EXISTS (
		SELECT 1 FROM job_attention_runs a
		WHERE a.repo = jobs.repo AND a.run_id = jobs.github_run_id
			AND ` + settledOnGitHubSQL("a", "jobs") + `)`
}

// settledOnGitHubSQL is the observation that takes a failure out of the
// attention list: GitHub said, after the job finished, that its PR is closed,
// that newer commits replaced it or that its checks pass. The list and the loop
// that asks GitHub again both read it, so that "settled" cannot mean one thing
// to the page and another to the schedule.
func settledOnGitHubSQL(observation, jobs string) string {
	return `(` + observation + `.reason IN ('closed','superseded','fixed')
		AND ` + observation + `.checked_at >= COALESCE(` + jobs + `.completed_at, ` + jobs + `.queued_at))`
}

// unreplacedJobSQL says that no later job has taken this one's place. A later
// job does that in one of two ways, and each is its own NOT EXISTS: a later
// attempt of the same run is found through idx_jobs_run, and a later run of the
// same workflow on the same branch through idx_jobs_attention_branch. Joined
// by an OR inside a single NOT EXISTS, SQLite could use neither and walked
// every retained job with the same name in every repository, which took seconds
// on a month of history and queued every other read behind it.
// TestTheAttentionFilterIsReadFromTheRunAndBranchIndexes holds the plan, and
// TestSplittingTheReplacementTestKeepsTheSameJobs holds the meaning.
func unreplacedJobSQL() string {
	const countsAsReplacement = `(later.state != 'completed' OR later.conclusion NOT IN ('cancelled','skipped','neutral')
				OR later.runner_fault != '' OR later.fault_kind != '')`
	return `(NOT EXISTS (
		SELECT 1 FROM jobs later
		WHERE later.github_run_id = jobs.github_run_id AND later.repo = jobs.repo
			AND later.github_run_id != 0 AND later.job_name = jobs.job_name
			AND later.run_attempt > jobs.run_attempt
			AND ` + countsAsReplacement + `)
	AND NOT EXISTS (
		SELECT 1 FROM jobs later
		WHERE jobs.github_run_id != 0 AND jobs.head_branch != '' AND jobs.workflow != ''
			AND later.repo = jobs.repo AND later.head_branch = jobs.head_branch
			AND later.workflow = jobs.workflow AND later.job_name = jobs.job_name
			AND later.github_run_id != 0 AND later.github_run_id != jobs.github_run_id
			AND later.queued_at > jobs.queued_at
			AND ` + countsAsReplacement + `))`
}

// StatsSince counts the jobs behind the Overview.
//
// managedOnly narrows every count and both percentiles to the jobs this fleet
// has a hand in. GitHub reports every job in an installed repository, and on an
// organisation that also uses hosted runners most of them are somebody else's:
// a queue depth that counts those answers "why is my fleet slow?" with a number
// the fleet cannot act on, and a median wait computed from them is somebody
// else's queue.
func (s *Store) StatsSince(ctx context.Context, since time.Time, managedOnly bool) (JobStats, error) {
	scope := ""
	if managedOnly {
		scope = " AND " + managedJobSQL("jobs")
	}
	var st JobStats
	err := s.read.QueryRowContext(ctx, `SELECT
		(SELECT COUNT(*) FROM jobs WHERE `+queuedJobSQL("jobs")+scope+`),
		(SELECT COUNT(*) FROM jobs WHERE `+runningJobSQL("jobs")+scope+`),
		(SELECT COUNT(*) FROM jobs WHERE state='completed' AND completed_at >= ?`+scope+`),
		(SELECT COUNT(*) FROM jobs WHERE state='completed' AND `+failedJobSQL()+` AND completed_at >= ?`+scope+`),
		(SELECT COUNT(*) FROM jobs WHERE state='completed' AND conclusion = 'success' AND runner_fault = '' AND completed_at >= ?`+scope+`),
		(SELECT COUNT(*) FROM jobs WHERE state='completed' AND conclusion IN ('cancelled','skipped') AND runner_fault = '' AND completed_at >= ?`+scope+`),
		(SELECT COUNT(*) FROM jobs WHERE state='completed' AND `+fleetFailedJobSQL()+` AND completed_at >= ?`+scope+`)`,
		ms(since), ms(since), ms(since), ms(since), ms(since)).Scan(&st.Queued, &st.Running,
		&st.CompletedLast, &st.Failed, &st.Succeeded, &st.Cancelled, &st.FleetFailed)
	if err != nil {
		return st, err
	}
	// The residual is neither a success, a failure nor a cancellation: stale,
	// empty, neutral, action_required. It is counted rather than dropped so
	// the four always add up to the completed total.
	st.Unknown = st.CompletedLast - st.Failed - st.Succeeded - st.Cancelled
	waits, err := s.queueWaits(ctx, since, managedOnly)
	if err != nil {
		return st, err
	}
	st.MedianWait = percentile(waits, 0.50)
	st.P95Wait = percentile(waits, 0.95)
	st.MedianWaitMS = st.MedianWait.Milliseconds()
	st.P95WaitMS = st.P95Wait.Milliseconds()
	return st, nil
}

func (s *Store) queueWaits(ctx context.Context, since time.Time, managedOnly bool) ([]time.Duration, error) {
	scope := ""
	if managedOnly {
		scope = " AND " + managedJobSQL("jobs")
	}
	// Only a job a runner picked up has a wait to measure. GitHub stamps
	// started_at on a job that never reached one, with the same value as
	// queued_at: a job cancelled while queued, or one its `if:` skipped. On a
	// repository whose workflow skips its real job when a path filter finds
	// nothing, those zeros come with every push and outnumber the real waits,
	// which pins the median, and eventually the p95, at nothing. A job
	// cancelled after a runner took it keeps its name, so its wait, which was
	// real, stays.
	rows, err := s.read.QueryContext(ctx, `SELECT started_at - queued_at FROM jobs
		WHERE started_at IS NOT NULL AND (runner_name != '' OR runner_id != '')
			AND queued_at >= ?`+scope+` ORDER BY 1`, ms(since))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []time.Duration
	for rows.Next() {
		var v int64
		if err := rows.Scan(&v); err != nil {
			return nil, err
		}
		if v < 0 {
			v = 0
		}
		out = append(out, time.Duration(v)*time.Millisecond)
	}
	return out, rows.Err()
}

// percentile returns the p-th percentile of a pre-sorted slice.
func percentile(sorted []time.Duration, p float64) time.Duration {
	if len(sorted) == 0 {
		return 0
	}
	i := int(float64(len(sorted)-1) * p)
	return sorted[i]
}

// The history prunes delete in bounded batches, each in its own transaction,
// because a batch holds the single writer for as long as it runs. A pass used to
// run one batch an hour, which stopped retention bounding a table once its inflow
// beat the batch, and drained a backlog at the same crawl. pruneInBatches repeats
// the batch until one comes back short, and the writer is released between
// batches so a webhook or a heartbeat waits for one batch, never for the backlog.
const (
	pruneJobBatch = 500
	pruneRowBatch = 1000
	// pruneMaxBatches bounds what one pass may do. A backlog deeper than this
	// -- 50k jobs, 100k rows -- is finished by the next hourly pass rather than
	// by monopolising the writer for the whole of this one.
	pruneMaxBatches = 100
)

// pruneInBatches calls batch until it deletes fewer than size rows, the bound is
// reached or ctx is done, and returns the total. What it deleted before an
// error is still counted, since it is already gone.
func pruneInBatches(ctx context.Context, size int64, maxBatches int, batch func(context.Context) (int64, error)) (int64, error) {
	var total int64
	for i := 0; i < maxBatches && ctx.Err() == nil; i++ {
		n, err := batch(ctx)
		total += n
		if err != nil {
			return total, err
		}
		if n < size {
			break
		}
	}
	return total, nil
}

// PruneJobs deletes jobs older than the cutoff, and their timelines with them:
// an event whose job is gone answers nothing.
//
// A job that finished is aged from when it finished. One that never did -- a
// completed delivery that was lost, a repository deleted while its job was
// queued -- is aged from when it was queued, so that it cannot sit in the
// table for ever as demand nothing will ever meet; the controller marks such
// a job stale long before this, and this is the backstop.
func (s *Store) PruneJobs(ctx context.Context, before time.Time) (int64, error) {
	const old = `(state = 'completed' AND completed_at < ?) OR (state != 'completed' AND queued_at < ?)`
	return pruneInBatches(ctx, pruneJobBatch, pruneMaxBatches, func(ctx context.Context) (int64, error) {
		var pruned int64
		err := s.tx(ctx, func(tx *sql.Tx) error {
			if _, err := tx.ExecContext(ctx, `DELETE FROM job_events WHERE job_id IN
				(SELECT id FROM jobs WHERE `+old+` ORDER BY id LIMIT 500)`, ms(before), ms(before)); err != nil {
				return err
			}
			res, err := tx.ExecContext(ctx, `DELETE FROM jobs WHERE id IN (SELECT id FROM jobs WHERE `+old+` ORDER BY id LIMIT 500)`, ms(before), ms(before))
			if err != nil {
				return err
			}
			pruned, err = res.RowsAffected()
			return err
		})
		return pruned, err
	})
}

// ---------------------------------------------------------------------------
// Audit
// ---------------------------------------------------------------------------

const auditCols = `id, actor_id, actor_name, actor_kind, action, target_kind, target_id,
	before, after, ip, created_at`

// AppendAudit records one mutating action.
func (s *Store) AppendAudit(ctx context.Context, e *AuditEvent) error {
	if e.ID == "" {
		e.ID = NewID(PrefixAudit)
	}
	if e.CreatedAt.IsZero() {
		e.CreatedAt = s.Now()
	}
	if e.ActorKind == "" {
		e.ActorKind = "system"
	}
	_, err := s.exec(ctx, `INSERT INTO audit_events (`+auditCols+`) VALUES (?,?,?,?,?,?,?,?,?,?,?)`,
		e.ID, e.ActorID, e.ActorName, e.ActorKind, e.Action, e.TargetKind, e.TargetID,
		e.Before, e.After, e.IP, ms(e.CreatedAt))
	return err
}

// AuditFilter narrows an audit listing.
type AuditFilter struct {
	ActorIDs    []string
	Actions     []string
	TargetKinds []string
	TargetID    string
	Search      string
	Since       *time.Time
	Until       *time.Time
}

var auditSortCols = map[string]string{
	"created_at": "created_at",
	"action":     "action",
	"actor":      "actor_name",
}

// ListAudit returns a filtered page of audit events, newest first.
func (s *Store) ListAudit(ctx context.Context, f AuditFilter, p Page) ([]*AuditEvent, int, error) {
	var cond []string
	var args []any
	in := func(col string, vals []string) {
		if len(vals) == 0 {
			return
		}
		ph := make([]string, len(vals))
		for i, v := range vals {
			ph[i] = "?"
			args = append(args, v)
		}
		cond = append(cond, col+` IN (`+strings.Join(ph, ",")+`)`)
	}
	in("actor_id", f.ActorIDs)
	in("action", f.Actions)
	in("target_kind", f.TargetKinds)
	if f.TargetID != "" {
		cond = append(cond, `target_id = ?`)
		args = append(args, f.TargetID)
	}
	if f.Since != nil {
		cond = append(cond, `created_at >= ?`)
		args = append(args, ms(*f.Since))
	}
	if f.Until != nil {
		cond = append(cond, `created_at <= ?`)
		args = append(args, ms(*f.Until))
	}
	if q := strings.TrimSpace(f.Search); q != "" {
		cond = append(cond, `(actor_name LIKE ? ESCAPE '\' OR action LIKE ? ESCAPE '\' OR target_id LIKE ? ESCAPE '\')`)
		like := likePattern(q)
		args = append(args, like, like, like)
	}
	where := ""
	if len(cond) > 0 {
		where = "WHERE " + strings.Join(cond, " AND ")
	}
	var total int
	if err := s.read.QueryRowContext(ctx, `SELECT COUNT(*) FROM audit_events `+where, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	q := `SELECT ` + auditCols + ` FROM audit_events ` + where +
		` ORDER BY ` + p.orderBy(auditSortCols, "created_at DESC") + ` LIMIT ? OFFSET ?`
	args = append(args, p.limit(50, 500), max(p.Offset, 0))
	rows, err := s.read.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	var out []*AuditEvent
	for rows.Next() {
		var e AuditEvent
		var created int64
		if err := rows.Scan(&e.ID, &e.ActorID, &e.ActorName, &e.ActorKind, &e.Action,
			&e.TargetKind, &e.TargetID, &e.Before, &e.After, &e.IP, &created); err != nil {
			return nil, 0, err
		}
		e.CreatedAt = at(created)
		out = append(out, &e)
	}
	return out, total, rows.Err()
}

// AuditActions returns the distinct action names present, for the filter menu.
func (s *Store) AuditActions(ctx context.Context) ([]string, error) {
	rows, err := s.read.QueryContext(ctx, `SELECT DISTINCT action FROM audit_events ORDER BY action LIMIT 200`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var v string
		if err := rows.Scan(&v); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

// ---------------------------------------------------------------------------
// Scaling events
// ---------------------------------------------------------------------------

// AppendScalingEvent records one scheduler decision.
func (s *Store) AppendScalingEvent(ctx context.Context, e *ScalingEvent) error {
	if e.ID == "" {
		e.ID = NewID(PrefixScaling)
	}
	if e.CreatedAt.IsZero() {
		e.CreatedAt = s.Now()
	}
	_, err := s.exec(ctx, `INSERT INTO scaling_events
		(id, pool_id, pool_name, from_count, to_count, reason, created_at) VALUES (?,?,?,?,?,?,?)`,
		e.ID, e.PoolID, e.PoolName, e.From, e.To, e.Reason, ms(e.CreatedAt))
	return err
}

// ListScalingEvents returns recent scaling decisions, newest first.
func (s *Store) ListScalingEvents(ctx context.Context, poolID string, limit int) ([]*ScalingEvent, error) {
	if limit <= 0 || limit > 500 {
		limit = 50
	}
	q := `SELECT id, pool_id, pool_name, from_count, to_count, reason, created_at FROM scaling_events`
	var args []any
	if poolID != "" {
		q += ` WHERE pool_id = ?`
		args = append(args, poolID)
	}
	q += ` ORDER BY created_at DESC LIMIT ?`
	args = append(args, limit)
	rows, err := s.read.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*ScalingEvent
	for rows.Next() {
		var e ScalingEvent
		var created int64
		if err := rows.Scan(&e.ID, &e.PoolID, &e.PoolName, &e.From, &e.To, &e.Reason, &created); err != nil {
			return nil, err
		}
		e.CreatedAt = at(created)
		out = append(out, &e)
	}
	return out, rows.Err()
}

// PruneScalingEvents deletes scaling history older than the cutoff.
func (s *Store) PruneScalingEvents(ctx context.Context, before time.Time) (int64, error) {
	return pruneInBatches(ctx, pruneRowBatch, pruneMaxBatches, func(ctx context.Context) (int64, error) {
		res, err := s.exec(ctx, `DELETE FROM scaling_events WHERE rowid IN (SELECT rowid FROM scaling_events WHERE created_at < ? LIMIT 1000)`, ms(before))
		if err != nil {
			return 0, err
		}
		return res.RowsAffected()
	})
}

// ---------------------------------------------------------------------------
// Webhook deliveries
// ---------------------------------------------------------------------------

// RecordDelivery stores the outcome of one inbound webhook.
func (s *Store) RecordDelivery(ctx context.Context, d *WebhookDelivery) error {
	if d.ID == "" {
		d.ID = NewID(PrefixDelivery)
	}
	if d.ReceivedAt.IsZero() {
		d.ReceivedAt = s.Now()
	}
	_, err := s.execHot(ctx, insertDeliveryQuery,
		d.ID, d.DeliveryID, d.Event, d.Action, d.Repo, d.Status, d.Error,
		d.InstallationID, ms(d.ReceivedAt))
	return err
}

// ListDeliveries returns recent webhook deliveries, newest first. A non-empty
// status filters to just that outcome.
func (s *Store) ListDeliveries(ctx context.Context, status string, limit int) ([]*WebhookDelivery, error) {
	if limit <= 0 || limit > 500 {
		limit = 50
	}
	q := `SELECT id, delivery_id, event, action, repo, status, error, installation_id, received_at
		FROM webhook_deliveries`
	var args []any
	if status != "" {
		q += ` WHERE status = ?`
		args = append(args, status)
	}
	q += ` ORDER BY received_at DESC LIMIT ?`
	args = append(args, limit)
	rows, err := s.read.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*WebhookDelivery
	for rows.Next() {
		var d WebhookDelivery
		var recv int64
		if err := rows.Scan(&d.ID, &d.DeliveryID, &d.Event, &d.Action, &d.Repo,
			&d.Status, &d.Error, &d.InstallationID, &recv); err != nil {
			return nil, err
		}
		d.ReceivedAt = at(recv)
		out = append(out, &d)
	}
	return out, rows.Err()
}

// CountFailedDeliveries returns how many deliveries failed since a cutoff.
func (s *Store) CountFailedDeliveries(ctx context.Context, since time.Time) (int, error) {
	var n int
	err := s.read.QueryRowContext(ctx, `SELECT COUNT(*) FROM webhook_deliveries
		WHERE status != 'accepted' AND received_at >= ?`, ms(since)).Scan(&n)
	return n, err
}

// LastDeliveryAt returns when a webhook was last received, whether or not it
// verified, or the zero time. The webhook health check uses it to tell
// "webhooks are quiet" from "GitHub has never reached this address".
func (s *Store) LastDeliveryAt(ctx context.Context) (time.Time, error) {
	return s.lastDeliveryAt(ctx, "")
}

// LastAcceptedDeliveryAt returns when a webhook last verified, or the zero
// time. This is the one the poller stands down on: a delivery whose signature
// did not verify started no runner, so a stream of them -- a mistyped secret,
// say -- is exactly the case the fallback poller exists for.
func (s *Store) LastAcceptedDeliveryAt(ctx context.Context) (time.Time, error) {
	return s.lastDeliveryAt(ctx, "accepted")
}

// InstallationsFreshSince returns the installations a webhook has verified for
// since a cutoff: the ones whose deliveries are arriving.
//
// A delivery is credited to the installation that owns its repository, by the
// same precedence FindInstallationByTarget uses -- deliberately not to the one
// whose secret verified it, which verification allows to be another's. An
// installation whose secrets have drifted is exactly the one whose webhooks
// are not arriving, and crediting its neighbour's delivery to it would tell
// the poller to stand down over the fleet's most broken installation.
//
// It answers "which installations are fresh" rather than "when was each one
// last fresh", because the cutoff is what lets the index do the work. Asking
// for the last delivery per installation groups over every accepted row ever
// kept -- a week of them by default, rescanned on every sweep. With the cutoff
// the plan is a range over idx_webhook_status and touches only the deliveries
// inside the window. The caller asks nothing else, and an installation absent
// from the set is one to poll.
//
// One query rather than one per installation: the caller has the whole list in
// hand and asks on every sweep.
//
// Where an organisation installation and a repository installation both cover
// a repository, only the repository one is credited, because that is the
// precedence everything else here uses. An organisation whose only traffic is
// for a repository-scoped sibling therefore looks silent and keeps being
// polled, which is the direction to be wrong in.
func (s *Store) InstallationsFreshSince(ctx context.Context, since time.Time) (map[string]bool, error) {
	rows, err := s.read.QueryContext(ctx, `
		SELECT DISTINCT (
			SELECT i.id FROM installations i
			 WHERE (i.target_type = 'repo' AND LOWER(i.target) = LOWER(d.repo))
			    OR (i.target_type = 'org'  AND LOWER(i.target) = LOWER(CASE
			            WHEN instr(d.repo, '/') > 1
			            THEN substr(d.repo, 1, instr(d.repo, '/') - 1)
			            ELSE d.repo END))
			 ORDER BY CASE i.target_type WHEN 'repo' THEN 0 ELSE 1 END, i.created_at, i.id
			 LIMIT 1
		) AS owner
		  FROM webhook_deliveries d
		 WHERE d.status = 'accepted' AND d.received_at >= ? AND d.repo != ''`, ms(since))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]bool{}
	for rows.Next() {
		var id sql.NullString
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		// A repository no installation covers is nobody's freshness, and a
		// delivery that verified against a secret belonging to some other
		// installation does not make that one's webhooks work.
		if id.Valid && id.String != "" {
			out[id.String] = true
		}
	}
	return out, rows.Err()
}

func (s *Store) lastDeliveryAt(ctx context.Context, status string) (time.Time, error) {
	var v sql.NullInt64
	var err error
	if status == "" {
		err = s.read.QueryRowContext(ctx, `SELECT MAX(received_at) FROM webhook_deliveries`).Scan(&v)
	} else {
		err = s.read.QueryRowContext(ctx, `SELECT MAX(received_at) FROM webhook_deliveries WHERE status = ?`, status).Scan(&v)
	}
	if err != nil || !v.Valid {
		return time.Time{}, err
	}
	return at(v.Int64), nil
}

// PruneDeliveries deletes webhook history older than the cutoff.
func (s *Store) PruneDeliveries(ctx context.Context, before time.Time) (int64, error) {
	return pruneInBatches(ctx, pruneRowBatch, pruneMaxBatches, func(ctx context.Context) (int64, error) {
		res, err := s.exec(ctx, `DELETE FROM webhook_deliveries WHERE rowid IN (SELECT rowid FROM webhook_deliveries WHERE received_at < ? LIMIT 1000)`, ms(before))
		if err != nil {
			return 0, err
		}
		return res.RowsAffected()
	})
}

// ---------------------------------------------------------------------------
// Fleet samples (Overview sparklines)
// ---------------------------------------------------------------------------

// FleetSample is one point on the Overview's sparklines.
type FleetSample struct {
	At          time.Time `json:"at"`
	QueuedJobs  int       `json:"queued_jobs"`
	RunningJobs int       `json:"running_jobs"`
	// FleetQueuedJobs and FleetRunningJobs are the same minute narrowed to the
	// jobs this fleet has a hand in, so the sparkline can follow the Overview's
	// "other runners" toggle instead of always plotting somebody else's queue.
	// Samples taken before this existed carry 0: there is no honest way to work
	// out now what the fleet's own queue was then.
	FleetQueuedJobs  int `json:"fleet_queued_jobs"`
	FleetRunningJobs int `json:"fleet_running_jobs"`
	IdleRunners      int `json:"idle_runners"`
	BusyRunners      int `json:"busy_runners"`
	TotalRunners     int `json:"total_runners"`
}

// RecordSample stores one fleet sample, replacing any sample for the same
// minute so a restart mid-minute cannot double-count.
func (s *Store) RecordSample(ctx context.Context, f FleetSample) error {
	minute := f.At.UTC().Truncate(time.Minute)
	_, err := s.exec(ctx, `INSERT INTO fleet_samples
		(at, queued_jobs, running_jobs, fleet_queued_jobs, fleet_running_jobs,
		 idle_runners, busy_runners, total_runners)
		VALUES (?,?,?,?,?,?,?,?)
		ON CONFLICT(at) DO UPDATE SET queued_jobs=excluded.queued_jobs,
			running_jobs=excluded.running_jobs,
			fleet_queued_jobs=excluded.fleet_queued_jobs,
			fleet_running_jobs=excluded.fleet_running_jobs,
			idle_runners=excluded.idle_runners,
			busy_runners=excluded.busy_runners, total_runners=excluded.total_runners`,
		ms(minute), f.QueuedJobs, f.RunningJobs, f.FleetQueuedJobs, f.FleetRunningJobs,
		f.IdleRunners, f.BusyRunners, f.TotalRunners)
	return err
}

// ListSamples returns fleet samples since a cutoff, oldest first.
func (s *Store) ListSamples(ctx context.Context, since time.Time) ([]FleetSample, error) {
	rows, err := s.read.QueryContext(ctx, `SELECT at, queued_jobs, running_jobs,
		fleet_queued_jobs, fleet_running_jobs, idle_runners,
		busy_runners, total_runners FROM fleet_samples WHERE at >= ? ORDER BY at`, ms(since))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []FleetSample
	for rows.Next() {
		var f FleetSample
		var t int64
		if err := rows.Scan(&t, &f.QueuedJobs, &f.RunningJobs,
			&f.FleetQueuedJobs, &f.FleetRunningJobs, &f.IdleRunners,
			&f.BusyRunners, &f.TotalRunners); err != nil {
			return nil, err
		}
		f.At = at(t)
		out = append(out, f)
	}
	return out, rows.Err()
}

// PruneSamples deletes fleet samples older than the cutoff.
func (s *Store) PruneSamples(ctx context.Context, before time.Time) (int64, error) {
	return pruneInBatches(ctx, pruneRowBatch, pruneMaxBatches, func(ctx context.Context) (int64, error) {
		res, err := s.exec(ctx, `DELETE FROM fleet_samples WHERE rowid IN (SELECT rowid FROM fleet_samples WHERE at < ? LIMIT 1000)`, ms(before))
		if err != nil {
			return 0, err
		}
		return res.RowsAffected()
	})
}

// ListStaleRunningJobs finds jobs whose execution or workflow lifetime has elapsed.
func (s *Store) ListStaleRunningJobs(ctx context.Context, startedBefore, queuedBefore time.Time) ([]*Job, error) {
	rows, err := s.read.QueryContext(ctx, `SELECT `+jobCols+` FROM jobs
        WHERE state = ? AND ((started_at IS NOT NULL AND started_at < ?)
        OR (started_at IS NULL AND queued_at < ?)) ORDER BY queued_at`,
		string(JobInProgress), ms(startedBefore), ms(queuedBefore))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*Job
	for rows.Next() {
		j, err := scanJob(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, j)
	}
	return out, rows.Err()
}
