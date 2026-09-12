package store

import (
	"context"
	"path/filepath"
	"testing"
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
