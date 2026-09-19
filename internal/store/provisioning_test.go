package store

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

func TestProvisioningSurvivesReplayAndRestart(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "queue.db")
	s, err := Open(ctx, Options{Path: path})
	if err != nil {
		t.Fatal(err)
	}
	j, err := s.UpsertJob(ctx, &Job{GitHubJobID: 1, State: JobQueued, Labels: StringSlice{"self-hosted"}, Repo: "acme/app"})
	if err != nil {
		t.Fatal(err)
	}
	for _, action := range []string{"pause", "delete", "run_now", "resume"} {
		if _, err := s.ControlProvisioning(ctx, []string{j.ID}, action); err != nil {
			t.Fatal(err)
		}
		replay, err := s.UpsertJob(ctx, &Job{GitHubJobID: 1, State: JobQueued})
		if err != nil {
			t.Fatal(err)
		}
		want := ""
		if action == "pause" {
			want = "paused"
		}
		if action == "delete" {
			want = "deleted"
		}
		if replay.Provisioning != want || replay.ProvisionNow != (action == "run_now") {
			t.Fatalf("%s lost on replay: %+v", action, replay)
		}
	}
	if _, err := s.ControlProvisioning(ctx, []string{j.ID}, "pause"); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = Open(ctx, Options{Path: path})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	got, err := s.GetJob(ctx, j.ID)
	if err != nil || got.Provisioning != "paused" {
		t.Fatalf("restart: %+v %v", got, err)
	}
}

func TestProvisioningSelectionAndStaleItems(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	makeJob := func(id int64, repo, branch string) *Job {
		j, err := s.UpsertJob(ctx, &Job{GitHubJobID: id, State: JobQueued, Repo: repo, HeadBranch: branch, Labels: StringSlice{"self-hosted", "linux"}})
		if err != nil {
			t.Fatal(err)
		}
		return j
	}
	first := makeJob(1, "acme/one", "main")
	second := makeJob(2, "acme/two", "main")
	makeJob(3, "acme/one", "dev")
	ids, err := s.ProvisioningSelection(ctx, JobFilter{Repos: []string{"acme/one", "acme/two"}, Branches: []string{"main"}, Labels: []string{"linux"}})
	if err != nil || len(ids) != 2 {
		t.Fatalf("selection: %v %v", ids, err)
	}
	late := makeJob(4, "acme/one", "main")
	if _, err := s.UpsertJob(ctx, &Job{GitHubJobID: second.GitHubJobID, State: JobInProgress}); err != nil {
		t.Fatal(err)
	}
	results, err := s.ControlProvisioning(ctx, append(ids, first.ID, "missing"), "delete")
	if err != nil || len(results) != 3 {
		t.Fatalf("deduplicated results: %+v %v", results, err)
	}
	ok := 0
	for _, r := range results {
		if r.OK {
			ok++
		}
	}
	if ok != 1 {
		t.Fatalf("expected only still-queued original item to change: %+v", results)
	}
	got, _ := s.GetJob(ctx, late.ID)
	if got.Provisioning != "" {
		t.Fatal("later arrival included")
	}
	selected, err := s.ProvisioningSelection(ctx, JobFilter{Provisioning: []string{"deleted"}})
	if err != nil || len(selected) != 1 || selected[0] != first.ID {
		t.Fatalf("deleted filter: %v %v", selected, err)
	}
	counts, err := s.ProvisioningCounts(ctx, JobFilter{Branches: []string{"main"}, Provisioning: []string{"deleted"}})
	if err != nil || counts["deleted"] != 1 || counts["ready"] != 1 {
		t.Fatalf("faceted counts: %v %v", counts, err)
	}
	if _, err := s.ControlProvisioning(ctx, []string{late.ID}, "invalid"); err == nil {
		t.Fatal("invalid action accepted")
	}
}

// Removing a job from the queue has to move the numbers an operator reads, or
// the removal reads as having done nothing. The Overview's queue depth, the
// pool bars beside it and the queue gauges all come from StatsSince and
// ListQueuedJobs, and an operator who empties the queue and then finds the
// tile still holding its old number has been told the button is broken.
//
// Pausing is a different word and stays counted: that job is on hold, still
// waiting, and the Queue page lists it by default.
func TestRemovingAJobFromTheQueueStopsItCountingAsWaiting(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	queue := func(id int64) *Job {
		j, err := s.UpsertJob(ctx, &Job{
			GitHubJobID: id, State: JobQueued, Repo: "acme/app", PoolID: "pool_1",
			Matched: true, Labels: StringSlice{"self-hosted", "linux"},
		})
		if err != nil {
			t.Fatal(err)
		}
		return j
	}
	ready, paused, removed := queue(1), queue(2), queue(3)
	if _, err := s.ControlProvisioning(ctx, []string{paused.ID}, "pause"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ControlProvisioning(ctx, []string{removed.ID}, "delete"); err != nil {
		t.Fatal(err)
	}

	// Both scopes: the Overview counts each of them, and the tile follows the
	// "other runners" switch between the two.
	for _, managedOnly := range []bool{false, true} {
		st, err := s.StatsSince(ctx, time.Time{}, managedOnly)
		if err != nil {
			t.Fatal(err)
		}
		if st.Queued != 2 {
			t.Errorf("managedOnly=%v: queued = %d, want the ready and the paused job only", managedOnly, st.Queued)
		}
	}

	jobs, err := s.ListQueuedJobs(ctx)
	if err != nil {
		t.Fatal(err)
	}
	ids := make([]string, len(jobs))
	for i, j := range jobs {
		ids[i] = j.ID
	}
	if len(ids) != 2 || ids[0] != ready.ID || ids[1] != paused.ID {
		t.Errorf("queued demand = %v, want %v and %v", ids, ready.ID, paused.ID)
	}

	// Restoring it puts the number back: the row was never anywhere else.
	if _, err := s.ControlProvisioning(ctx, []string{removed.ID}, "resume"); err != nil {
		t.Fatal(err)
	}
	st, err := s.StatsSince(ctx, time.Time{}, true)
	if err != nil {
		t.Fatal(err)
	}
	if st.Queued != 3 {
		t.Errorf("queued after restoring = %d, want 3", st.Queued)
	}
}

// The sweep that retires jobs GitHub stopped talking about is the one caller
// that wants the removed rows too. Removing a job suppresses this fleet's
// demand for it; it says nothing about whether GitHub still has the work, and
// a row left queued for ever would sit in the Queue's removed view for ever.
func TestStaleQueuedJobsIncludeTheOnesRemovedFromTheQueue(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	old := time.Now().Add(-48 * time.Hour)
	j, err := s.UpsertJob(ctx, &Job{GitHubJobID: 1, State: JobQueued, Repo: "acme/app", QueuedAt: old})
	if err != nil {
		t.Fatal(err)
	}
	fresh, err := s.UpsertJob(ctx, &Job{GitHubJobID: 2, State: JobQueued, Repo: "acme/app", QueuedAt: time.Now()})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.ControlProvisioning(ctx, []string{j.ID}, "delete"); err != nil {
		t.Fatal(err)
	}
	stale, err := s.ListStaleQueuedJobs(ctx, time.Now().Add(-24*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if len(stale) != 1 || stale[0].ID != j.ID {
		t.Fatalf("stale queued jobs = %+v, want only the removed one queued %v ago", stale, time.Since(old))
	}
	if _, err := s.GetJob(ctx, fresh.ID); err != nil {
		t.Fatal(err)
	}
}
