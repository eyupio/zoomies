package store

import (
	"context"
	"errors"
	"testing"
	"time"
)

func setupHistoryJob(t *testing.T, s *Store, id int64, conclusion string, completed time.Time, steps ...JobStep) {
	t.Helper()
	if _, err := s.UpsertJob(context.Background(), &Job{
		GitHubJobID: id, GitHubRunID: id * 10, Repo: "acme/repo", Workflow: "CI", JobName: "build",
		State: JobCompleted, Conclusion: conclusion, QueuedAt: completed.Add(-time.Minute), CompletedAt: &completed, Steps: steps,
	}); err != nil {
		t.Fatal(err)
	}
}

func setupStep(name string, number int, start, end time.Time) JobStep {
	return JobStep{Number: number, Name: name, Status: "completed", Conclusion: "success", StartedAt: &start, CompletedAt: &end}
}

func TestRecentSetupStepSamplesKeepsOnlySuccessfulUnambiguousTimedSteps(t *testing.T) {
	s := newTestStore(t)
	now := time.Date(2026, 10, 23, 12, 0, 0, 0, time.UTC)
	setupHistoryJob(t, s, 1, "success", now, setupStep("Set up Node", 2, now.Add(-5*time.Second), now.Add(-1*time.Second)))
	setupHistoryJob(t, s, 2, "success", now.Add(-10*24*time.Hour), setupStep("Set up Node", 2, now.Add(-10*24*time.Hour-6*time.Second), now.Add(-10*24*time.Hour)))
	setupHistoryJob(t, s, 3, "failure", now, setupStep("Set up Node", 2, now.Add(-4*time.Second), now))
	setupHistoryJob(t, s, 4, "success", now, setupStep("Other", 2, now.Add(-4*time.Second), now))
	setupHistoryJob(t, s, 5, "success", now, setupStep("Set up Node", 2, now.Add(-4*time.Second), now), setupStep("Set up Node", 3, now.Add(-3*time.Second), now))
	setupHistoryJob(t, s, 6, "success", now, JobStep{Number: 2, Name: "Set up Node", Status: "completed", Conclusion: "success"})
	samples, err := s.RecentSetupStepSamples(context.Background(), "acme/repo", "CI", "build", "Set up Node", now.Add(-30*24*time.Hour), 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(samples) != 2 || samples[0].GitHubJobID != 1 || samples[0].SetupMS != 4000 || samples[0].StepNumber != 2 || samples[0].RunAttempt != 0 || samples[1].GitHubJobID != 2 || samples[1].SetupMS != 6000 {
		t.Fatalf("samples %+v", samples)
	}
	one, err := s.RecentSetupStepSamples(context.Background(), "acme/repo", "CI", "build", "Set up Node", now.Add(-30*24*time.Hour), 1)
	if err != nil || len(one) != 1 || one[0].GitHubJobID != 1 {
		t.Fatalf("limit %+v, %v", one, err)
	}
}

func TestRecentSetupStepSamplesRefusesInvalidQueries(t *testing.T) {
	s := newTestStore(t)
	now := time.Now()
	for _, tc := range []struct {
		repo, workflow, job, step string
		since                     time.Time
		limit                     int
	}{
		{"", "CI", "build", "Set up Node", now, 1},
		{"acme/repo", "", "build", "Set up Node", now, 1},
		{"acme/repo", "CI", "", "Set up Node", now, 1},
		{"acme/repo", "CI", "build", "", now, 1},
		{"acme/repo", "CI", "build", "Set up\nNode", now, 1},
		{"acme/repo", "CI", "build", "Set up Node", time.Time{}, 1},
		{"acme/repo", "CI", "build", "Set up Node", now, 0},
		{"acme/repo", "CI", "build", "Set up Node", now, 101},
	} {
		if _, err := s.RecentSetupStepSamples(context.Background(), tc.repo, tc.workflow, tc.job, tc.step, tc.since, tc.limit); !errors.Is(err, ErrInvalidSetupHistory) {
			t.Fatalf("%+v: %v", tc, err)
		}
	}
}

func TestRecentSetupStepSamplesReportsHowLongTheJobRanBeforeTheStep(t *testing.T) {
	s := newTestStore(t)
	now := time.Date(2026, 10, 24, 12, 0, 0, 0, time.UTC)
	begin := now.Add(-100 * time.Second)
	setupHistoryJob(t, s, 1, "success", now,
		setupStep("Set up job", 1, begin, begin.Add(2*time.Second)),
		setupStep("Install", 2, begin.Add(2*time.Second), begin.Add(40*time.Second)),
		setupStep("Browser tests", 3, begin.Add(40*time.Second), now),
	)
	// A step that is the first the job recorded has nothing before it.
	setupHistoryJob(t, s, 2, "success", now.Add(-time.Hour), setupStep("Browser tests", 1, now.Add(-90*time.Minute), now.Add(-time.Hour)))
	samples, err := s.RecentSetupStepSamples(context.Background(), "acme/repo", "CI", "build", "Browser tests", now.Add(-30*24*time.Hour), 10)
	if err != nil || len(samples) != 2 {
		t.Fatalf("samples %+v, %v", samples, err)
	}
	if samples[0].GitHubJobID != 1 || samples[0].LeadMS != 40_000 || samples[0].SetupMS != 60_000 {
		t.Fatalf("lead %+v", samples[0])
	}
	if samples[1].LeadMS != 0 {
		t.Fatalf("a first step reported lead %d", samples[1].LeadMS)
	}
}
