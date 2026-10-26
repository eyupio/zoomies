package analysis

import (
	"cmp"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"
)

const RecurringFailuresVersion = "recurring-failures-v1"

// MaxRecurringGroups bounds the jobs a finding lists; the counts keep the rest.
const MaxRecurringGroups = 20

// RecurringRepeats is the fewest failures that make a job one that keeps
// failing: a failure or two is what any busy repository has.
const RecurringRepeats = 3

// What a recurring job's failures look like, tried in this order.
const (
	// ClassInfrastructure: most failures carry a runner fault, so the fleet,
	// not the workflow, is what to look at.
	ClassInfrastructure = "infrastructure"
	// ClassFlaky: most failures passed when the same revision ran again.
	ClassFlaky = "flaky"
	// ClassPersistent: it has not passed since its latest failure.
	ClassPersistent = "persistent"
	// ClassIntermittent: it fails repeatedly, passes in between, and does not
	// pass on a rerun of the same revision either.
	ClassIntermittent = "intermittent"
)

// Conclusions this analysis reads. Any other is neither a pass nor a failure
// (cancelled, skipped, unknown) and is left out rather than guessed at.
const (
	JobSucceeded = "success"
	JobFailed    = "failure"
)

// JobOutcome is one finished job. Repository, workflow, job and step names are
// written by whoever controls the workflow, so whoever shows them to a model
// treats them as untrusted.
type JobOutcome struct {
	JobID, Repository, Workflow, Job string
	HeadSHA, HeadBranch              string
	// RunID is the workflow run the job belongs to; 0 when unknown. Attempt
	// numbers count within one run, so only the same run's later attempt is a
	// retry of it.
	RunID      int64
	RunAttempt int
	Conclusion string
	// FailedStep is the step that failed; "" when none is recorded.
	FailedStep string
	// RunnerFault is true when the runner recorded a fault for this job.
	RunnerFault bool
	CompletedAt time.Time
}

type RecurringSnapshot struct {
	Since, Until time.Time
	Jobs         []JobOutcome
	// Truncated says the read stopped at MaxReadJobs jobs.
	Truncated bool
}

// RecurringGroup is one job, in one workflow of one repository, and how its
// failures went. Every time in it is when a job completed.
type RecurringGroup struct {
	Repository string `json:"repository"`
	Workflow   string `json:"workflow"`
	Job        string `json:"job"`
	Runs       int    `json:"runs"`
	Failures   int    `json:"failures"`
	// Branches counts the distinct branches the failures fell on: one means
	// a branch's own trouble, many means the workflow's.
	Branches       int       `json:"branches"`
	FirstFailureAt time.Time `json:"first_failure_at"`
	LastFailureAt  time.Time `json:"last_failure_at"`
	// FailedStep is the step most of its failures stopped at, and StepFailures
	// how many did; "" when no failure recorded one.
	FailedStep   string `json:"failed_step"`
	StepFailures int    `json:"step_failures"`
	// RunnerFaults counts failures whose runner recorded a fault.
	RunnerFaults int `json:"runner_faults"`
	// RetryPasses counts failures that passed when the same revision ran
	// again, the one signal that separates a flaky job from a broken one.
	RetryPasses int `json:"retry_passes"`
	// StillFailing is true when it has not passed since its latest failure.
	StillFailing bool   `json:"still_failing"`
	Class        string `json:"class"`
	LastJobID    string `json:"last_job_id"`
}

type RecurringEvidence struct {
	// Groups are the jobs that keep failing, most failures first, cut to
	// MaxRecurringGroups; GroupsCut says how many were left out.
	Groups    []RecurringGroup `json:"groups"`
	GroupsCut int              `json:"groups_cut"`
	// Jobs counts the jobs read that passed or failed, and Failures those that
	// failed.
	Jobs      int  `json:"jobs"`
	Failures  int  `json:"failures"`
	Truncated bool `json:"truncated"`
}

type RecurringFailuresFinding = Finding[RecurringEvidence]

var recurringFailuresCodes = []string{}

type recurringKey struct{ repo, workflow, job string }

// RecurringFailures names the jobs that keep failing, what their failures have
// in common, and whether the evidence points at the workflow or at the fleet.
// It reads outcomes only: it cannot say why a test failed, and says so.
func RecurringFailures(s RecurringSnapshot) (RecurringFailuresFinding, error) {
	f := RecurringFailuresFinding{Analysis: "recurring_failures", Version: RecurringFailuresVersion, Since: s.Since, Until: s.Until,
		NextSteps: []NextStep{}, Codes: []string{}}
	if s.Since.IsZero() || !s.Since.Before(s.Until) {
		return f, errors.New("recurring failures snapshot needs a period with a start before its end")
	}
	e := RecurringEvidence{Groups: []RecurringGroup{}, Truncated: s.Truncated}
	byJob := map[recurringKey][]JobOutcome{}
	for _, j := range s.Jobs {
		if j.CompletedAt.Before(s.Since) || !j.CompletedAt.Before(s.Until) {
			continue
		}
		if j.Conclusion != JobSucceeded && j.Conclusion != JobFailed {
			continue
		}
		e.Jobs++
		if j.Conclusion == JobFailed {
			e.Failures++
		}
		k := recurringKey{j.Repository, j.Workflow, j.Job}
		byJob[k] = append(byJob[k], j)
	}

	var groups []RecurringGroup
	for k, jobs := range byJob {
		if g := recurringGroup(k, jobs); g.Failures >= RecurringRepeats {
			groups = append(groups, g)
		}
	}
	slices.SortFunc(groups, func(a, b RecurringGroup) int {
		return cmp.Or(b.Failures-a.Failures, b.LastFailureAt.Compare(a.LastFailureAt),
			strings.Compare(a.Repository, b.Repository), strings.Compare(a.Workflow, b.Workflow), strings.Compare(a.Job, b.Job))
	})
	e.GroupsCut = max(0, len(groups)-MaxRecurringGroups)
	e.Groups = append(e.Groups, groups[:min(len(groups), MaxRecurringGroups)]...)
	f.Evidence = e

	switch {
	case e.Jobs == 0:
		f.Confidence, f.ConfidenceReason = Low, "no job finished with a pass or a failure in this period"
		f.Summary = "No job finished with a pass or a failure in this period."
		return f, nil
	case len(groups) == 0:
		f.Summary = fmt.Sprintf("No job failed %d times or more in this period (%d of %s failed).", RecurringRepeats,
			e.Failures, count(e.Jobs, "job"))
		f.Confidence = High
	default:
		top := groups[0]
		f.Dominant = top.LastJobID
		f.Summary = recurringSentence(top)
		f.NextSteps = recurringSteps(top)
		f.Confidence = High
	}
	var reasons []string
	if s.Truncated {
		reasons = append(reasons, fmt.Sprintf("the read stopped at %d jobs, so a job may have failed more often than shown", MaxReadJobs))
	}
	if len(groups) > 0 && groups[0].Class == ClassIntermittent {
		reasons = append(reasons, "the failures neither passed on a rerun nor stayed failing, so the cause is not in these outcomes")
	}
	if len(reasons) > 0 {
		f.Confidence, f.ConfidenceReason = Medium, strings.Join(reasons, "; ")
	}
	return f, nil
}

func recurringGroup(k recurringKey, jobs []JobOutcome) RecurringGroup {
	slices.SortFunc(jobs, func(a, b JobOutcome) int {
		return cmp.Or(a.CompletedAt.Compare(b.CompletedAt), strings.Compare(a.JobID, b.JobID))
	})
	g := RecurringGroup{Repository: k.repo, Workflow: k.workflow, Job: k.job, Runs: len(jobs)}
	branches := map[string]bool{}
	steps := map[string]int{}
	var lastFail, lastPass time.Time
	for i, j := range jobs {
		if j.Conclusion == JobSucceeded {
			lastPass = j.CompletedAt
			continue
		}
		g.Failures++
		branches[j.HeadBranch] = true
		if g.FirstFailureAt.IsZero() {
			g.FirstFailureAt = j.CompletedAt
		}
		g.LastFailureAt, g.LastJobID, lastFail = j.CompletedAt, j.JobID, j.CompletedAt
		if j.FailedStep != "" {
			steps[j.FailedStep]++
		}
		if j.RunnerFault {
			g.RunnerFaults++
		}
		// A later attempt of the same revision that passed is the rerun that
		// shows the failure was not the code's.
		if j.HeadSHA != "" && j.RunID != 0 {
			for _, later := range jobs[i+1:] {
				if later.Conclusion == JobSucceeded && later.RunID == j.RunID && later.HeadSHA == j.HeadSHA && later.RunAttempt > j.RunAttempt {
					g.RetryPasses++
					break
				}
			}
		}
	}
	g.Branches = len(branches)
	for step, n := range steps {
		if n > g.StepFailures || (n == g.StepFailures && step < g.FailedStep) {
			g.FailedStep, g.StepFailures = step, n
		}
	}
	g.StillFailing = !lastPass.After(lastFail)
	switch {
	case g.RunnerFaults*2 >= g.Failures:
		g.Class = ClassInfrastructure
	case g.RetryPasses*2 >= g.Failures:
		g.Class = ClassFlaky
	case g.StillFailing:
		g.Class = ClassPersistent
	default:
		g.Class = ClassIntermittent
	}
	return g
}

func recurringSentence(g RecurringGroup) string {
	// Repository, workflow, job and step names are written by whoever controls
	// the workflow, so the sentence points at the latest failed job by id and
	// leaves the names to the evidence, which readers quote as untrusted.
	b := fmt.Sprintf("The job that fails most failed %d of its %d runs, first at %s and last at %s (job %s)",
		g.Failures, g.Runs, g.FirstFailureAt.UTC().Format(time.RFC3339), g.LastFailureAt.UTC().Format(time.RFC3339), g.LastJobID)
	if g.FailedStep != "" {
		b += fmt.Sprintf(", %d of its failures at one step", g.StepFailures)
	}
	switch g.Class {
	case ClassInfrastructure:
		b += fmt.Sprintf("; %d of the failures recorded a runner fault, which points at the fleet rather than the workflow", g.RunnerFaults)
	case ClassFlaky:
		b += fmt.Sprintf("; %d of the failures passed when the same revision ran again, which points at a flaky job", g.RetryPasses)
	case ClassPersistent:
		b += "; it has not passed since, which points at a change that broke it"
	default:
		b += "; it passes in between and does not pass on a rerun, so these outcomes do not say why"
	}
	return b + "."
}

func recurringSteps(g RecurringGroup) []NextStep {
	switch g.Class {
	case ClassInfrastructure:
		return []NextStep{{Text: "Read the runner faults on the failed jobs before changing the workflow; the startup failures and host pressure analyses cover the same period."}}
	case ClassFlaky:
		return []NextStep{{Text: "Read the failed step's log from one failed run and one rerun of the same revision, and compare them; the outcomes alone cannot say what differs."}}
	case ClassPersistent:
		return []NextStep{{Text: "Compare the revision of the first failure with the last revision that passed; the change between them is where to look."}}
	}
	return []NextStep{{Text: "Read the failed step's log from two of the failed runs; these outcomes cannot say what they share."}}
}
