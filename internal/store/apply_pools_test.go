package store

import (
	"context"
	"errors"
	"testing"
)

// An import of pools is one change or none. If the last row of a document is
// refused by the database, the rows before it must not have landed either,
// or the fleet matches neither the file nor what was there before.
func TestApplyPoolsWritesEveryPoolOrNone(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	inst, existing, _ := seedPool(t, s)

	fresh := &Pool{Name: "zoomies-fresh", InstallationID: inst.ID, Backend: BackendDocker,
		MaxRunners: 2, DockerMode: DockerNone, Enabled: true}
	// A second pool under the existing one's name breaks the unique index,
	// after the first create has already been executed inside the transaction.
	clash := &Pool{Name: existing.Name, InstallationID: inst.ID, Backend: BackendDocker,
		MaxRunners: 1, DockerMode: DockerNone, Enabled: true}
	err := s.ApplyPools(ctx, []*Pool{fresh, clash}, nil)
	if !errors.Is(err, ErrConflict) {
		t.Fatalf("ApplyPools error = %v, want ErrConflict", err)
	}
	if _, err := s.GetPoolByName(ctx, "zoomies-fresh"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("the pool before the refused one was kept: %v", err)
	}

	edited := *existing
	edited.MaxRunners = 9
	missing := &Pool{ID: "pool_gone", Name: "zoomies-gone", InstallationID: inst.ID, Backend: BackendDocker,
		MaxRunners: 1, DockerMode: DockerNone, Enabled: true}
	if err := s.ApplyPools(ctx, nil, []*Pool{&edited, missing}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("updating a pool that is not there: error = %v, want ErrNotFound", err)
	}
	got, err := s.GetPool(ctx, existing.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.MaxRunners == 9 {
		t.Fatal("the update before the refused one was kept")
	}

	fresh = &Pool{Name: "zoomies-fresh", InstallationID: inst.ID, Backend: BackendDocker,
		MaxRunners: 2, DockerMode: DockerNone, Enabled: true}
	if err := s.ApplyPools(ctx, []*Pool{fresh}, []*Pool{&edited}); err != nil {
		t.Fatalf("ApplyPools: %v", err)
	}
	if got, _ := s.GetPool(ctx, existing.ID); got.MaxRunners != 9 {
		t.Fatalf("max_runners = %d after the apply, want 9", got.MaxRunners)
	}
	if _, err := s.GetPoolByName(ctx, "zoomies-fresh"); err != nil {
		t.Fatalf("the created pool is not there: %v", err)
	}
}
