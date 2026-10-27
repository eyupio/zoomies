package store

import (
	"context"
	"testing"
	"time"
)

// workflow_job deliveries never say what started a run, so it arrives from a
// separate read of the run, and every job of that run has to carry it: the
// failure a person sees may be any one of them.
func TestTheRunEventIsKeptOnEveryJobOfTheRunAndOnNoOther(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	first := seedJob(t, s, 1, JobCompleted, "")
	second := seedJob(t, s, 2, JobCompleted, "")
	for _, j := range []*Job{first, second} {
		j.GitHubRunID, j.Repo = 70, "acme/site"
		if _, err := s.UpsertJob(ctx, j); err != nil {
			t.Fatal(err)
		}
	}
	other := seedJob(t, s, 3, JobCompleted, "")
	other.GitHubRunID, other.Repo = 71, "acme/site"
	if _, err := s.UpsertJob(ctx, other); err != nil {
		t.Fatal(err)
	}

	if err := s.SetRunEventForRun(ctx, "acme/site", 70, "push"); err != nil {
		t.Fatal(err)
	}
	for _, j := range []*Job{first, second} {
		got, err := s.GetJobByGitHubID(ctx, j.GitHubJobID)
		if err != nil || got.RunEvent != "push" {
			t.Errorf("job %d reads event %q (%v), want push", j.GitHubJobID, got.RunEvent, err)
		}
	}
	if got, _ := s.GetJobByGitHubID(ctx, 3); got.RunEvent != "" {
		t.Errorf("a job of another run reads event %q, want none", got.RunEvent)
	}

	// An empty answer must never erase what GitHub already said.
	if err := s.SetRunEventForRun(ctx, "acme/site", 70, ""); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.GetJobByGitHubID(ctx, 1); got.RunEvent != "push" {
		t.Errorf("an empty event erased the stored one: %q", got.RunEvent)
	}
}

// Repair discovery only offers PR runs, while the attention inbox must also
// retain failed push and scheduled runs. Unknown triggers stay discoverable
// until GitHub says what started them.
func TestTheEliRepairableFilterKeepsPullRequestRunsAndUnreadOnes(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	now := s.Now()
	for id, run := range map[int64]int64{1: 10, 2: 11, 3: 12, 4: 13} {
		j := &Job{GitHubJobID: id, GitHubRunID: run, Repo: "acme/site", JobName: "build", State: JobCompleted, Conclusion: "failure", QueuedAt: now.Add(-time.Hour), CompletedAt: &now, RunAttempt: 1}
		if _, err := s.UpsertJob(ctx, j); err != nil {
			t.Fatalf("seeding job %d: %v", id, err)
		}
	}
	for run, event := range map[int64]string{10: "push", 11: "pull_request", 12: "schedule"} {
		if err := s.SetRunEventForRun(ctx, "acme/site", run, event); err != nil {
			t.Fatalf("SetRunEventForRun: %v", err)
		}
	}
	_, attentionTotal, attentionErr := s.ListJobs(ctx, JobFilter{AttentionOnly: true}, Page{})
	if attentionErr != nil || attentionTotal != 4 {
		t.Fatalf("attention total %d (%v), want all four failures regardless of trigger", attentionTotal, attentionErr)
	}
	got, total, err := s.ListJobs(ctx, JobFilter{AttentionOnly: true, EliRepairableOnly: true}, Page{})
	if err != nil {
		t.Fatalf("ListJobs: %v", err)
	}
	runs := map[int64]bool{}
	for _, j := range got {
		runs[j.GitHubRunID] = true
	}
	if total != 2 || !runs[11] || !runs[13] {
		t.Fatalf("listed runs %v (total %d), want the pull request run 11 and the unread run 13", runs, total)
	}
}
