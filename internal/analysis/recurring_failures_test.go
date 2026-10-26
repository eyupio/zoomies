package analysis

import (
	"strings"
	"testing"
	"time"
)

var recurringT0 = time.Date(2026, 10, 20, 9, 0, 0, 0, time.UTC)

func recurringJob(id, conclusion string, minutes int, mod func(*JobOutcome)) JobOutcome {
	j := JobOutcome{JobID: id, Repository: "acme/app", Workflow: "ci.yml", Job: "test", HeadSHA: "sha-" + id,
		HeadBranch: "main", RunID: 1, RunAttempt: 1, Conclusion: conclusion, CompletedAt: recurringT0.Add(time.Duration(minutes) * time.Minute)}
	if mod != nil {
		mod(&j)
	}
	return j
}

func recurringSnap(jobs ...JobOutcome) RecurringSnapshot {
	return RecurringSnapshot{Since: recurringT0.Add(-time.Hour), Until: recurringT0.Add(24 * time.Hour), Jobs: jobs}
}

func TestARecurringJobThatHasNotPassedSinceIsPersistent(t *testing.T) {
	f, err := RecurringFailures(recurringSnap(
		recurringJob("1", JobSucceeded, 0, nil),
		recurringJob("2", JobFailed, 10, func(j *JobOutcome) { j.FailedStep = "Run tests" }),
		recurringJob("3", JobFailed, 20, func(j *JobOutcome) { j.FailedStep = "Run tests" }),
		recurringJob("4", JobFailed, 30, func(j *JobOutcome) { j.FailedStep = "Lint" }),
	))
	if err != nil {
		t.Fatal(err)
	}
	g := f.Evidence.Groups[0]
	if g.Class != ClassPersistent || !g.StillFailing || g.FailedStep != "Run tests" || g.StepFailures != 2 || g.Failures != 3 || g.Runs != 4 {
		t.Fatalf("group = %+v", g)
	}
	if f.Dominant != "4" || f.Confidence != High || !strings.Contains(f.Summary, "has not passed since") {
		t.Fatalf("finding = %+v", f)
	}
	assertNoCommand(t, f.NextSteps)
	for _, name := range []string{"acme/app", "ci.yml", "Run tests"} {
		if strings.Contains(f.Summary, name) {
			t.Errorf("the summary quotes %q, a name a workflow wrote", name)
		}
	}
}

// A rerun of the same revision that passes is the only outcome-level evidence
// that the code was not at fault, so it is what makes a job flaky.
func TestFailuresThatPassedOnARerunOfTheSameRevisionAreFlaky(t *testing.T) {
	var jobs []JobOutcome
	for i := range 3 {
		sha := string(rune('a' + i))
		jobs = append(jobs,
			recurringJob(sha+"1", JobFailed, i*20, func(j *JobOutcome) { j.HeadSHA, j.RunID = sha, int64(i+10) }),
			recurringJob(sha+"2", JobSucceeded, i*20+5, func(j *JobOutcome) { j.HeadSHA, j.RunID, j.RunAttempt = sha, int64(i+10), 2 }))
	}
	f, err := RecurringFailures(recurringSnap(jobs...))
	if err != nil {
		t.Fatal(err)
	}
	if g := f.Evidence.Groups[0]; g.Class != ClassFlaky || g.RetryPasses != 3 || g.StillFailing {
		t.Fatalf("group = %+v", g)
	}
}

func TestMostlyRunnerFaultsPointAtTheFleetNotTheWorkflow(t *testing.T) {
	f, _ := RecurringFailures(recurringSnap(
		recurringJob("1", JobFailed, 1, func(j *JobOutcome) { j.RunnerFault = true }),
		recurringJob("2", JobFailed, 2, func(j *JobOutcome) { j.RunnerFault = true }),
		recurringJob("3", JobFailed, 3, nil),
	))
	if g := f.Evidence.Groups[0]; g.Class != ClassInfrastructure || g.RunnerFaults != 2 {
		t.Fatalf("group = %+v", g)
	}
}

func TestAJobThatPassesBetweenFailuresWithoutARerunPassIsIntermittentAndMedium(t *testing.T) {
	f, _ := RecurringFailures(recurringSnap(
		recurringJob("1", JobFailed, 1, nil), recurringJob("2", JobFailed, 2, nil),
		recurringJob("3", JobFailed, 3, nil), recurringJob("4", JobSucceeded, 4, nil),
	))
	if g := f.Evidence.Groups[0]; g.Class != ClassIntermittent {
		t.Fatalf("group = %+v", g)
	}
	if f.Confidence != Medium || f.ConfidenceReason == "" {
		t.Fatalf("confidence = %s %q", f.Confidence, f.ConfidenceReason)
	}
}

func TestFewerThanThreeFailuresIsNotRecurring(t *testing.T) {
	f, _ := RecurringFailures(recurringSnap(recurringJob("1", JobFailed, 1, nil), recurringJob("2", JobFailed, 2, nil), recurringJob("3", JobSucceeded, 3, nil)))
	if len(f.Evidence.Groups) != 0 || f.Dominant != "" || f.Evidence.Failures != 2 || f.Evidence.Jobs != 3 {
		t.Fatalf("finding = %+v", f)
	}
}

func TestCancelledAndOutOfPeriodJobsAreNotCounted(t *testing.T) {
	f, _ := RecurringFailures(recurringSnap(
		recurringJob("1", "cancelled", 1, nil),
		recurringJob("2", JobFailed, -600, nil),
		recurringJob("3", JobFailed, 3000, nil),
	))
	if f.Evidence.Jobs != 0 || f.Confidence != Low || f.ConfidenceReason == "" {
		t.Fatalf("finding = %+v", f)
	}
}

func TestATruncatedReadSaysSoAndNeverClaimsHighConfidence(t *testing.T) {
	s := recurringSnap(recurringJob("1", JobFailed, 1, nil), recurringJob("2", JobFailed, 2, nil), recurringJob("3", JobFailed, 3, nil))
	s.Truncated = true
	f, _ := RecurringFailures(s)
	if f.Confidence != Medium || !strings.Contains(f.ConfidenceReason, "stopped") {
		t.Fatalf("confidence = %s %q", f.Confidence, f.ConfidenceReason)
	}
}

func TestGroupsAreRankedMostFailuresFirstAndCut(t *testing.T) {
	var jobs []JobOutcome
	for g := range MaxRecurringGroups + 2 {
		for i := range RecurringRepeats + g%3 {
			id := string(rune('A'+g)) + string(rune('a'+i))
			jobs = append(jobs, recurringJob(id, JobFailed, g*10+i, func(j *JobOutcome) { j.Job = "job-" + string(rune('A'+g)) }))
		}
	}
	f, _ := RecurringFailures(recurringSnap(jobs...))
	if len(f.Evidence.Groups) != MaxRecurringGroups || f.Evidence.GroupsCut != 2 {
		t.Fatalf("groups = %d cut = %d", len(f.Evidence.Groups), f.Evidence.GroupsCut)
	}
	for i := 1; i < len(f.Evidence.Groups); i++ {
		if f.Evidence.Groups[i].Failures > f.Evidence.Groups[i-1].Failures {
			t.Fatalf("not ranked at %d", i)
		}
	}
}

func TestARecurringFailuresPeriodNeedsAStartBeforeItsEnd(t *testing.T) {
	if _, err := RecurringFailures(RecurringSnapshot{}); err == nil {
		t.Fatal("want an error")
	}
}

// Attempt numbers count within one workflow run. A pass in another run of the
// same commit (another branch, another trigger) says nothing about whether this
// run's failure was the code's, and must not make a broken job look flaky.
func TestAPassInAnotherRunOfTheSameCommitIsNotARetry(t *testing.T) {
	same := func(j *JobOutcome) { j.HeadSHA = "shared" }
	f, _ := RecurringFailures(recurringSnap(
		recurringJob("1", JobFailed, 1, func(j *JobOutcome) { same(j); j.RunID = 1 }),
		recurringJob("2", JobFailed, 2, func(j *JobOutcome) { same(j); j.RunID = 2 }),
		recurringJob("3", JobFailed, 3, func(j *JobOutcome) { same(j); j.RunID = 3 }),
		recurringJob("4", JobSucceeded, 4, func(j *JobOutcome) { same(j); j.RunID = 4; j.RunAttempt = 2 }),
	))
	if g := f.Evidence.Groups[0]; g.RetryPasses != 0 || g.Class == ClassFlaky {
		t.Fatalf("group = %+v, want no retry pass across runs", g)
	}
}

func TestAJobWithNoRunIdentityCannotBeCountedAsARetry(t *testing.T) {
	none := func(j *JobOutcome) { j.RunID = 0; j.HeadSHA = "s" }
	f, _ := RecurringFailures(recurringSnap(
		recurringJob("1", JobFailed, 1, none), recurringJob("2", JobFailed, 2, none), recurringJob("3", JobFailed, 3, none),
		recurringJob("4", JobSucceeded, 4, func(j *JobOutcome) { none(j); j.RunAttempt = 2 }),
	))
	if g := f.Evidence.Groups[0]; g.RetryPasses != 0 {
		t.Fatalf("group = %+v", g)
	}
}
