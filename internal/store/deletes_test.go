package store

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"
)

// A pool, a host or an installation takes its runner rows with it, and the
// schema does that silently: the Runners page kept showing runners that no
// longer existed until it was reloaded. The deletes now say which rows went, so
// the controller can announce each one.
func TestDeletesReportTheRunnerRowsThatWentWithThem(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	inst, pool, host := seedPool(t, s)

	ids := func(names ...string) []string {
		out := make([]string, 0, len(names))
		for _, n := range names {
			r := &Runner{PoolID: pool.ID, HostID: host.ID, Name: n, State: RunnerIdle}
			if err := s.CreateRunner(ctx, r); err != nil {
				t.Fatalf("CreateRunner %s: %v", n, err)
			}
			out = append(out, r.ID)
		}
		slices.Sort(out)
		return out
	}

	want := ids("a", "b")
	got, _, err := s.DeletePool(ctx, pool.ID)
	if err != nil {
		t.Fatalf("DeletePool: %v", err)
	}
	slices.Sort(got)
	if !slices.Equal(got, want) {
		t.Fatalf("DeletePool reported %v, want %v", got, want)
	}
	if _, err := s.GetRunner(ctx, want[0]); !errors.Is(err, ErrNotFound) {
		t.Fatalf("the runner row survived its pool: %v", err)
	}
	if _, _, err := s.DeletePool(ctx, pool.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("deleting a pool twice = %v, want ErrNotFound", err)
	}

	// The same for a host, and for an installation whose pools are the route
	// to the rows.
	pool = &Pool{Name: "again", InstallationID: inst.ID, Labels: StringSlice{"again"}, Backend: BackendDocker, MaxRunners: 2, Ephemeral: true, DockerMode: DockerNone, Enabled: true}
	if err := s.CreatePool(ctx, pool); err != nil {
		t.Fatalf("CreatePool: %v", err)
	}
	want = ids("c")
	if got, err = s.DeleteHost(ctx, host.ID); err != nil || !slices.Equal(got, want) {
		t.Fatalf("DeleteHost reported %v (%v), want %v", got, err, want)
	}
	host = &Host{Name: "vm-2", Capacity: 2, Backends: StringSlice{"docker"}}
	if err := s.CreateHost(ctx, host); err != nil {
		t.Fatalf("CreateHost: %v", err)
	}
	want = ids("d", "e")
	if got, err = s.DeleteInstallation(ctx, inst.ID); err != nil {
		t.Fatalf("DeleteInstallation: %v", err)
	}
	slices.Sort(got)
	if !slices.Equal(got, want) {
		t.Fatalf("DeleteInstallation reported %v, want %v", got, want)
	}
}

// Deleting a pool takes its queued demand off the queue too, not only its
// runners: a job left matched to a pool that no longer exists is a dangling
// reference, and it would keep counting as queued work nobody can ever place.
func TestDeletePoolRemovesItsQueuedJobsTooAndLeavesTheRestAlone(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	_, pool, _ := seedPool(t, s)
	now := time.Now()

	queued, err := s.UpsertJob(ctx, &Job{GitHubJobID: 1, Repo: "acme/widgets", State: JobQueued,
		Matched: true, PoolID: pool.ID, QueuedAt: now})
	if err != nil {
		t.Fatalf("UpsertJob queued: %v", err)
	}
	// An operator who already removed this from the queue must not have it
	// reported again: it was already gone before the pool was.
	alreadyRemoved, err := s.UpsertJob(ctx, &Job{GitHubJobID: 2, Repo: "acme/widgets", State: JobQueued,
		Matched: true, PoolID: pool.ID, Provisioning: ProvisioningDeleted, QueuedAt: now})
	if err != nil {
		t.Fatalf("UpsertJob already-removed: %v", err)
	}
	// A job the pool is already running keeps its history: PoolID says who ran
	// it, and the pool's deletion does not rewrite the past.
	running, err := s.UpsertJob(ctx, &Job{GitHubJobID: 3, Repo: "acme/widgets", State: JobInProgress,
		Matched: true, PoolID: pool.ID, QueuedAt: now})
	if err != nil {
		t.Fatalf("UpsertJob running: %v", err)
	}

	_, jobs, err := s.DeletePool(ctx, pool.ID)
	if err != nil {
		t.Fatalf("DeletePool: %v", err)
	}
	if !slices.Equal(jobs, []string{queued.ID}) {
		t.Fatalf("DeletePool reported jobs %v, want [%s]", jobs, queued.ID)
	}

	got, err := s.GetJob(ctx, queued.ID)
	if err != nil {
		t.Fatalf("GetJob queued: %v", err)
	}
	if got.Provisioning != ProvisioningDeleted || got.PoolID != "" || got.Matched {
		t.Fatalf("the queued job was not cleanly taken off the deleted pool: %+v", got)
	}

	still, err := s.GetJob(ctx, alreadyRemoved.ID)
	if err != nil {
		t.Fatalf("GetJob alreadyRemoved: %v", err)
	}
	if still.PoolID != pool.ID {
		t.Fatalf("a job already removed from the queue should not be touched again: %+v", still)
	}

	unaffected, err := s.GetJob(ctx, running.ID)
	if err != nil {
		t.Fatalf("GetJob running: %v", err)
	}
	if unaffected.PoolID != pool.ID || !unaffected.Matched {
		t.Fatalf("an in-progress job's pool_id is history, not something the pool's deletion should erase: %+v", unaffected)
	}
}

// Pruning is the other way a runner row goes without a frame.
func TestPruneRunnersReportsWhatItRemoved(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	_, pool, host := seedPool(t, s)
	gone := &Runner{PoolID: pool.ID, HostID: host.ID, Name: "gone", State: RunnerRemoved}
	live := &Runner{PoolID: pool.ID, HostID: host.ID, Name: "live", State: RunnerIdle}
	for _, r := range []*Runner{gone, live} {
		if err := s.CreateRunner(ctx, r); err != nil {
			t.Fatalf("CreateRunner: %v", err)
		}
	}
	ids, err := s.PruneRunners(ctx, time.Now().Add(time.Hour))
	if err != nil {
		t.Fatalf("PruneRunners: %v", err)
	}
	if !slices.Equal(ids, []string{gone.ID}) {
		t.Fatalf("pruned %v, want just the removed runner %s", ids, gone.ID)
	}
	if _, err := s.GetRunner(ctx, live.ID); err != nil {
		t.Fatalf("the live runner was pruned: %v", err)
	}
}
