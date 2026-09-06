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
	got, err := s.DeletePool(ctx, pool.ID)
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
	if _, err := s.DeletePool(ctx, pool.ID); !errors.Is(err, ErrNotFound) {
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
