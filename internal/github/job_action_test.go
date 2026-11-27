package github

import (
	"slices"
	"testing"
	"time"

	gh "github.com/google/go-github/v88/github"
)

// GitHub's record of a finished job can keep saying in_progress. Its conclusion
// and its completion time are each enough to call the job over, and the fake
// writes both together, so this holds each one on its own: a later edit that
// drops either would otherwise pass every other test.
func TestAJobIsOverOnEitherWitnessOfItsEnd(t *testing.T) {
	ended := &gh.Timestamp{Time: time.Date(2026, 11, 24, 11, 2, 32, 0, time.UTC)}
	for _, tc := range []struct {
		name string
		job  gh.WorkflowJob
		want string
	}{
		{"queued", gh.WorkflowJob{Status: gh.Ptr("queued")}, "queued"},
		{"running", gh.WorkflowJob{Status: gh.Ptr("in_progress")}, "in_progress"},
		{"in progress with only a conclusion", gh.WorkflowJob{Status: gh.Ptr("in_progress"), Conclusion: gh.Ptr("success")}, "completed"},
		{"in progress with only a completion time", gh.WorkflowJob{Status: gh.Ptr("in_progress"), CompletedAt: ended}, "completed"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := jobAction(&tc.job); got != tc.want {
				t.Fatalf("jobAction = %q, want %q", got, tc.want)
			}
		})
	}
}

// The poller's listing is a second reader of GitHub's job record. A job already
// concluded but still called in_progress, in a run that has another job
// running, must not be listed as running: ingestStartedJob would take its
// runner busy until the next job-recovery sweep completed it.
func TestThePollerDoesNotListAJobGitHubHasAlreadyConcluded(t *testing.T) {
	run := &gh.WorkflowRun{ID: gh.Ptr(int64(7)), Name: gh.Ptr("CI")}
	jobs := []*gh.WorkflowJob{
		{ID: gh.Ptr(int64(1)), Status: gh.Ptr("queued")},
		{ID: gh.Ptr(int64(2)), Status: gh.Ptr("in_progress")},
		{ID: gh.Ptr(int64(3)), Status: gh.Ptr("in_progress"), Conclusion: gh.Ptr("success"),
			CompletedAt: &gh.Timestamp{Time: time.Date(2026, 11, 24, 11, 2, 32, 0, time.UTC)}},
	}
	var got []int64
	for _, q := range queuedJobsOnPage("acme/widgets", run, jobs) {
		got = append(got, q.ID)
	}
	if want := []int64{1, 2}; !slices.Equal(got, want) {
		t.Fatalf("listed jobs %v, want %v: the concluded one is over, whatever its status says", got, want)
	}
}
