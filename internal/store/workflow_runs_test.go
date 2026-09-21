package store

import (
	"context"
	"testing"
	"time"
)

// seedRuns writes three runs' worth of jobs: one that failed and was re-run
// to success, one still going, and one in another repository that somebody
// else's runners ran.
func seedRuns(t *testing.T, s *Store, now time.Time) {
	t.Helper()
	ctx := context.Background()
	at := func(d time.Duration) *time.Time {
		v := now.Add(d)
		return &v
	}
	seed := []*Job{
		// Run 1: build passed; test failed, was re-run, and passed.
		{GitHubJobID: 11, GitHubRunID: 1, Repo: "acme/widgets", Workflow: "CI", JobName: "build", RunNumber: 300, RunAttempt: 1, HeadBranch: "main",
			HTMLURL: "https://github.com/acme/widgets/actions/runs/1/job/11", Labels: StringSlice{"self-hosted"}, Matched: true, PoolID: "pool_a",
			State: JobCompleted, Conclusion: "success", QueuedAt: now, StartedAt: at(10 * time.Second), CompletedAt: at(time.Minute)},
		{GitHubJobID: 12, GitHubRunID: 1, Repo: "acme/widgets", Workflow: "CI", JobName: "test", RunNumber: 300, RunAttempt: 1, HeadBranch: "main",
			HTMLURL: "https://github.com/acme/widgets/actions/runs/1/job/12", Labels: StringSlice{"self-hosted"}, Matched: true, PoolID: "pool_a",
			State: JobCompleted, Conclusion: "failure", QueuedAt: now, StartedAt: at(20 * time.Second), CompletedAt: at(2 * time.Minute)},
		{GitHubJobID: 13, GitHubRunID: 1, Repo: "acme/widgets", Workflow: "CI", JobName: "test", RunNumber: 300, RunAttempt: 2, HeadBranch: "main",
			HTMLURL: "https://github.com/acme/widgets/actions/runs/1/job/13", Labels: StringSlice{"self-hosted"}, Matched: true, PoolID: "pool_a",
			State: JobCompleted, Conclusion: "success", QueuedAt: now.Add(5 * time.Minute), StartedAt: at(5*time.Minute + 10*time.Second), CompletedAt: at(7 * time.Minute)},
		// Run 2: one job running, the other still queued, on a second pool.
		{GitHubJobID: 21, GitHubRunID: 2, Repo: "acme/widgets", Workflow: "CI", JobName: "build", RunAttempt: 1, HeadBranch: "feature/x",
			Labels: StringSlice{"self-hosted"}, Matched: true, PoolID: "pool_a",
			State: JobInProgress, QueuedAt: now.Add(time.Minute), StartedAt: at(time.Minute + 5*time.Second)},
		{GitHubJobID: 22, GitHubRunID: 2, Repo: "acme/widgets", Workflow: "CI", JobName: "test", RunAttempt: 1, HeadBranch: "feature/x",
			Labels: StringSlice{"self-hosted", "arm64"}, Matched: true, PoolID: "pool_b",
			State: JobQueued, QueuedAt: now.Add(time.Minute)},
		// Run 3: another repository, run on GitHub's own runners.
		{GitHubJobID: 31, GitHubRunID: 3, Repo: "acme/site", Workflow: "Pages", JobName: "deploy", RunAttempt: 1,
			Labels: StringSlice{"ubuntu-latest"}, HTMLURL: "https://github.com/acme/site/actions/runs/3/job/31",
			State: JobCompleted, Conclusion: "success", QueuedAt: now.Add(2 * time.Minute), StartedAt: at(2*time.Minute + 5*time.Second), CompletedAt: at(3 * time.Minute)},
	}
	for _, j := range seed {
		if _, err := s.UpsertJob(ctx, j); err != nil {
			t.Fatalf("seeding job %d: %v", j.GitHubJobID, err)
		}
	}
}

// A run is what GitHub's Actions tab lists, so the Workflows page has to sum
// its jobs up the way GitHub's run page does: over the latest attempt of each
// job, and running while anything in it still is. Counting the superseded
// attempt would report a run that passed on its re-run as a failure.
func TestWorkflowRunsSumUpTheLatestAttemptOfEachJob(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Millisecond)
	seedRuns(t, s, now)

	runs, total, err := s.ListWorkflowRuns(ctx, JobFilter{}, Page{Sort: "queued_at"})
	if err != nil {
		t.Fatalf("ListWorkflowRuns: %v", err)
	}
	if total != 3 || len(runs) != 3 {
		t.Fatalf("got %d runs (total %d), want 3", len(runs), total)
	}

	rerun := runs[0]
	if rerun.GitHubRunID != 1 || rerun.RunNumber != 300 || rerun.RunAttempt != 2 || rerun.Workflow != "CI" || rerun.HeadBranch != "main" {
		t.Fatalf("first run = %+v, want run 1 (#300) at attempt 2", rerun)
	}
	if rerun.State != JobCompleted || rerun.Conclusion != "success" {
		t.Fatalf("run 1 is %s/%q, want completed/success: the failed attempt was superseded", rerun.State, rerun.Conclusion)
	}
	if rerun.Jobs != (RunJobCounts{Total: 2, Completed: 2, Succeeded: 2}) {
		t.Fatalf("run 1 job counts = %+v, want two jobs both succeeded", rerun.Jobs)
	}
	if rerun.HTMLURL != "https://github.com/acme/widgets/actions/runs/1" {
		t.Fatalf("run 1 URL = %q, want the run's page rather than a job's", rerun.HTMLURL)
	}
	if !rerun.QueuedAt.Equal(now) || rerun.StartedAt == nil || !rerun.StartedAt.Equal(now.Add(10*time.Second)) {
		t.Fatalf("run 1 queued %v, started %v; want the first job's", rerun.QueuedAt, rerun.StartedAt)
	}
	if rerun.CompletedAt == nil || !rerun.CompletedAt.Equal(now.Add(7*time.Minute)) {
		t.Fatalf("run 1 completed %v, want the last job's", rerun.CompletedAt)
	}
	if !rerun.Managed || rerun.Hosted {
		t.Fatalf("run 1 managed=%v hosted=%v, want this fleet's own", rerun.Managed, rerun.Hosted)
	}

	going := runs[1]
	if going.GitHubRunID != 2 || going.State != JobInProgress || going.Conclusion != "" {
		t.Fatalf("run 2 = %s/%q, want in_progress with no conclusion", going.State, going.Conclusion)
	}
	if going.Jobs != (RunJobCounts{Total: 2, Queued: 1, InProgress: 1}) {
		t.Fatalf("run 2 job counts = %+v, want one running and one queued", going.Jobs)
	}
	if going.CompletedAt != nil {
		t.Fatalf("run 2 completed %v, want nothing while a job is still queued", going.CompletedAt)
	}
	if going.QueueWait() != 5*time.Second || going.Duration() != 0 {
		t.Fatalf("run 2 waited %v and took %v, want 5s and nothing yet", going.QueueWait(), going.Duration())
	}

	theirs := runs[2]
	if theirs.Repo != "acme/site" || theirs.Managed || !theirs.Hosted {
		t.Fatalf("run 3 = %+v, want acme/site on somebody else's runners", theirs)
	}
}

// The status half of the filter is the run's own: a run with a queued job
// beside a running one is running, not queued, and a run whose only failure
// was re-run to success has not failed. Everything else keeps a run when any
// job of it matches, so the run stays whole on the page.
func TestWorkflowRunsAreFilteredByTheirOwnStatusAndByAnyJob(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Millisecond)
	seedRuns(t, s, now)
	// Run 4: the runner died under its only job, which GitHub still believes
	// is running.
	started := now.Add(3 * time.Minute)
	lost, err := s.UpsertJob(ctx, &Job{GitHubJobID: 41, GitHubRunID: 4, Repo: "acme/widgets", Workflow: "Nightly", JobName: "soak",
		RunAttempt: 1, Labels: StringSlice{"self-hosted"}, Matched: true, PoolID: "pool_a", RunnerID: "run_x",
		State: JobInProgress, QueuedAt: now.Add(3 * time.Minute), StartedAt: &started})
	if err != nil {
		t.Fatalf("seeding run 4: %v", err)
	}
	if _, _, err := s.SetJobRunnerFault(ctx, lost.ID, "runner exited with code 137", FaultRunnerExited); err != nil {
		t.Fatalf("SetJobRunnerFault: %v", err)
	}

	ids := func(f JobFilter) []int64 {
		t.Helper()
		runs, _, err := s.ListWorkflowRuns(ctx, f, Page{Sort: "queued_at"})
		if err != nil {
			t.Fatalf("ListWorkflowRuns(%+v): %v", f, err)
		}
		out := make([]int64, 0, len(runs))
		for _, r := range runs {
			out = append(out, r.GitHubRunID)
		}
		return out
	}
	same := func(got, want []int64) bool {
		if len(got) != len(want) {
			return false
		}
		for i := range got {
			if got[i] != want[i] {
				return false
			}
		}
		return true
	}
	no := false
	for _, tc := range []struct {
		name   string
		filter JobFilter
		want   []int64
	}{
		{"running runs", JobFilter{States: []JobState{JobInProgress}}, []int64{2, 4}},
		{"queued runs: none, since run 2 is running", JobFilter{States: []JobState{JobQueued}}, nil},
		{"successful runs, the re-run included", JobFilter{Conclusions: []string{"success"}}, []int64{1, 3}},
		{"failed runs, on either side", JobFilter{FailedOnly: true}, []int64{4}},
		{"the fleet's own failures", JobFilter{FaultedOnly: true}, []int64{4}},
		{"the workflows' own failures", JobFilter{WorkflowFailedOnly: true}, nil},
		{"runs this fleet has a hand in", JobFilter{ManagedOnly: true}, []int64{1, 2, 4}},
		{"a repository", JobFilter{Repos: []string{"acme/site"}}, []int64{3}},
		{"a workflow", JobFilter{Workflows: []string{"Nightly"}}, []int64{4}},
		{"a job on a pool keeps its whole run", JobFilter{PoolIDs: []string{"pool_b"}}, []int64{2}},
		{"a search over job names", JobFilter{Search: "deploy"}, []int64{3}},
		{"work still in hand", JobFilter{States: []JobState{JobInProgress}, Cancelling: &no}, []int64{2, 4}},
	} {
		if got := ids(tc.filter); !same(got, tc.want) {
			t.Errorf("%s: runs %v, want %v", tc.name, got, tc.want)
		}
	}

	// The run a pool filter keeps arrives whole, not reduced to the job that
	// matched: an operator asked which runs touch the pool, not which jobs.
	runs, _, err := s.ListWorkflowRuns(ctx, JobFilter{PoolIDs: []string{"pool_b"}}, Page{})
	if err != nil {
		t.Fatalf("ListWorkflowRuns by pool: %v", err)
	}
	if len(runs) != 1 || runs[0].Jobs.Total != 2 {
		t.Fatalf("run by pool = %+v, want run 2 with both its jobs counted", runs)
	}

	// Paging counts runs, not jobs.
	page, total, err := s.ListWorkflowRuns(ctx, JobFilter{}, Page{Sort: "queued_at", Limit: 1, Offset: 1})
	if err != nil {
		t.Fatalf("ListWorkflowRuns page: %v", err)
	}
	if total != 4 || len(page) != 1 || page[0].GitHubRunID != 2 {
		t.Fatalf("page = %v runs of %d, want run 2 alone of 4", len(page), total)
	}
}

// Opening a run is a job listing narrowed to it, so the run's row and the
// jobs under it come from the same rows and cannot disagree.
func TestJobsCanBeListedByTheirRun(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	seedRuns(t, s, time.Now().UTC().Truncate(time.Millisecond))

	jobs, total, err := s.ListJobs(ctx, JobFilter{Repos: []string{"acme/widgets"}, RunIDs: []int64{1}}, Page{Sort: "queued_at", Desc: false})
	if err != nil {
		t.Fatalf("ListJobs by run: %v", err)
	}
	if total != 3 || len(jobs) != 3 {
		t.Fatalf("got %d jobs (total %d), want the three attempts of run 1", len(jobs), total)
	}
	for _, j := range jobs {
		if j.GitHubRunID != 1 {
			t.Fatalf("job %d belongs to run %d, want run 1 only", j.GitHubJobID, j.GitHubRunID)
		}
	}
}

// Every job of a run is counted once. GitHub does not require job names to be
// unique, so two jobs of one attempt may share a name and both still count;
// and a job whose runner stopped under it is the fleet's failure whatever
// conclusion GitHub recorded, so it is never a success as well.
func TestWorkflowRunsCountEachJobOnce(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Millisecond)
	started, done := now.Add(time.Second), now.Add(time.Minute)
	seed := []*Job{
		{GitHubJobID: 51, JobName: "build", Conclusion: "success"},
		{GitHubJobID: 52, JobName: "build", Conclusion: "failure"},
		{GitHubJobID: 53, JobName: "deploy", Conclusion: "success"},
	}
	for _, j := range seed {
		j.GitHubRunID, j.RunAttempt, j.Repo, j.Workflow = 5, 1, "acme/widgets", "CI"
		j.State, j.QueuedAt, j.StartedAt, j.CompletedAt = JobCompleted, now, &started, &done
		j.Labels, j.Matched, j.PoolID = StringSlice{"self-hosted"}, true, "pool_a"
		if _, err := s.UpsertJob(ctx, j); err != nil {
			t.Fatalf("seeding job %d: %v", j.GitHubJobID, err)
		}
	}
	deploy, err := s.GetJobByGitHubID(ctx, 53)
	if err != nil {
		t.Fatalf("GetJobByGitHubID: %v", err)
	}
	if _, _, err := s.SetJobRunnerFault(ctx, deploy.ID, "runner exited with code 137", FaultRunnerExited); err != nil {
		t.Fatalf("SetJobRunnerFault: %v", err)
	}

	runs, total, err := s.ListWorkflowRuns(ctx, JobFilter{}, Page{})
	if err != nil {
		t.Fatalf("ListWorkflowRuns: %v", err)
	}
	if total != 1 || len(runs) != 1 {
		t.Fatalf("got %d runs (total %d), want the one", len(runs), total)
	}
	run := runs[0]
	if run.Jobs != (RunJobCounts{Total: 3, Completed: 3, Succeeded: 1, Failed: 2, Faulted: 1}) {
		t.Fatalf("job counts = %+v, want three jobs: one succeeded, two failed of which one is the fleet's", run.Jobs)
	}
	if run.State != JobCompleted || run.Conclusion != "failure" {
		t.Fatalf("run = %s/%q, want completed as a failure", run.State, run.Conclusion)
	}
}

// A run row can offer Pause and Run now only if it knows what its queued
// jobs already carry: Pause pressed on a run whose every queued job is paused
// has to be refused with a reason, and a Run now on the run must not sweep
// back in a job an operator removed from the queue on purpose.
func TestWorkflowRunsCountWhatAnOperatorDidToTheirQueuedJobs(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Millisecond)
	seed := []*Job{
		{GitHubJobID: 61, JobName: "build"},
		{GitHubJobID: 62, JobName: "test"},
		{GitHubJobID: 63, JobName: "lint"},
		{GitHubJobID: 64, JobName: "package"},
	}
	for _, j := range seed {
		j.GitHubRunID, j.RunAttempt, j.Repo, j.Workflow = 6, 1, "acme/widgets", "CI"
		j.State, j.QueuedAt = JobQueued, now
		j.Labels, j.Matched, j.PoolID = StringSlice{"self-hosted"}, true, "pool_a"
		if _, err := s.UpsertJob(ctx, j); err != nil {
			t.Fatalf("seeding job %d: %v", j.GitHubJobID, err)
		}
	}
	id := func(githubID int64) string {
		t.Helper()
		j, err := s.GetJobByGitHubID(ctx, githubID)
		if err != nil {
			t.Fatalf("GetJobByGitHubID(%d): %v", githubID, err)
		}
		return j.ID
	}
	for _, step := range []struct {
		action string
		job    int64
	}{{"run_now", 61}, {"pause", 62}, {"delete", 63}} {
		if _, err := s.ControlProvisioning(ctx, []string{id(step.job)}, step.action); err != nil {
			t.Fatalf("%s job %d: %v", step.action, step.job, err)
		}
	}

	runs, _, err := s.ListWorkflowRuns(ctx, JobFilter{}, Page{})
	if err != nil {
		t.Fatalf("ListWorkflowRuns: %v", err)
	}
	if len(runs) != 1 {
		t.Fatalf("got %d runs, want the one", len(runs))
	}
	got := runs[0].Jobs
	want := RunJobCounts{Total: 4, Queued: 4, Expedited: 1, Paused: 1, Removed: 1}
	if got != want {
		t.Fatalf("job counts = %+v, want %+v: the four queued jobs, one each expedited, paused and removed", got, want)
	}
	// The three the run's own controls reach: the removed one is restored
	// from the Queue page, not swept up with its siblings.
	if got.Controllable() != 3 {
		t.Fatalf("controllable = %d, want 3", got.Controllable())
	}

	// The run's provisioning selection is what a run-level action acts on,
	// and it leaves the removed job alone.
	ids, err := s.ProvisioningSelection(ctx, JobFilter{
		Repos: []string{"acme/widgets"}, RunIDs: []int64{6},
		Provisioning: []string{"ready", "expedited", ProvisioningPaused},
	})
	if err != nil {
		t.Fatalf("ProvisioningSelection: %v", err)
	}
	if len(ids) != 3 {
		t.Fatalf("selection = %v, want the three queued jobs not removed", ids)
	}
	for _, sel := range ids {
		if sel == id(63) {
			t.Fatalf("selection %v includes the removed job", ids)
		}
	}
}
