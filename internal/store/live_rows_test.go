package store

import (
	"context"
	"testing"
)

// "Occupies a slot on its host" is defined twice: RunnerState.Live for the
// scheduler's pool arithmetic, and the state filter in the SQL that fills
// Host.ActiveRunners for host capacity. Nothing held the two definitions
// equal, so a state added to one and not the other would have a pool and its
// hosts disagreeing about how full the fleet is. This test is the equation.
func TestAHostsActiveRunnersAreExactlyItsLiveRows(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	_, pool, host := seedPool(t, s)

	states := []RunnerState{
		RunnerProvisioning, RunnerRegistering, RunnerIdle, RunnerBusy,
		RunnerDraining, RunnerRemoved, RunnerFailed,
	}
	live := 0
	for _, state := range states {
		if err := s.CreateRunner(ctx, &Runner{PoolID: pool.ID, HostID: host.ID, Name: "runner-" + string(state), State: state}); err != nil {
			t.Fatalf("CreateRunner in %s: %v", state, err)
		}
		if state.Live() {
			live++
		}
	}

	got, err := s.GetHost(ctx, host.ID)
	if err != nil {
		t.Fatalf("GetHost: %v", err)
	}
	if got.ActiveRunners != live {
		t.Fatalf("ActiveRunners = %d with one runner in each of the %d states, want the %d whose state is Live", got.ActiveRunners, len(states), live)
	}
	if live == 0 || live == len(states) {
		t.Fatalf("Live is true for %d of %d states; the test needs both kinds to mean anything", live, len(states))
	}
}
