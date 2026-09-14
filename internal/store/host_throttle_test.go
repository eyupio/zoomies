package store

import (
	"context"
	"errors"
	"testing"
	"time"
)

// A throttled host takes fewer runners, and the number it takes is what every
// slot count on it has to be measured against: the scheduler's free-slot rule
// and the Hosts page's "n of m slots" both read it, and a page that promised
// slots the pass then refused would be worse than a page saying nothing.
func TestAThrottleTakesAQuarterOfTheSlotsPerRungAndNeverTheLastOne(t *testing.T) {
	cases := []struct {
		capacity, level, want int
	}{
		{4, 0, 4}, {4, 1, 3}, {4, 2, 2}, {4, 3, 1},
		// A small host reaches its floor early: a throttle is not a cordon,
		// and a host with one slot left is still a host in the fleet.
		{2, 1, 1}, {2, 3, 1}, {1, 3, 1},
		{16, 1, 12}, {16, 2, 8}, {16, 3, 4},
		// A capacity of zero is the operator's "take nothing", and the
		// throttle has nothing to add to it.
		{0, 2, 0},
		// A level past the top rung is treated as the top rung.
		{8, 9, 2},
	}
	for _, tc := range cases {
		h := Host{Capacity: tc.capacity, Throttle: HostThrottle{Level: tc.level}}
		if got := h.EffectiveCapacity(); got != tc.want {
			t.Errorf("capacity %d at level %d: effective = %d, want %d", tc.capacity, tc.level, got, tc.want)
		}
	}
	h := Host{Capacity: 4, ActiveRunners: 2, Throttle: HostThrottle{Level: 2}}
	if h.Free() != 0 || h.Available(time.Now()) {
		t.Fatalf("a host at its throttled capacity still offered slots: free=%d", h.Free())
	}
	h.Throttle = HostThrottle{}
	if h.Free() != 2 {
		t.Fatalf("lifting the throttle did not give the slots back: free=%d", h.Free())
	}
}

// The throttle survives a restart and cannot be reached by the paths a host
// writes through: it is the controller's decision about the host, and a host
// that could rewrite it on a heartbeat could talk its way off its own rung.
func TestAThrottleIsStoredByItsOwnStatementAndSurvivesTheHostsOwnWrites(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	h := &Host{Name: "pressed", Capacity: 4, Backends: StringSlice{"docker"}, Labels: StringMap{}}
	if err := s.CreateHost(ctx, h); err != nil {
		t.Fatalf("CreateHost: %v", err)
	}
	since := time.Date(2026, 9, 13, 12, 0, 0, 0, time.UTC)
	want := HostThrottle{Level: 2, Since: &since, ChangedAt: &since, Reason: "CPU has been at or above 95% for 30 s"}
	if err := s.SetHostThrottle(ctx, h.ID, want, 0); err != nil {
		t.Fatalf("SetHostThrottle: %v", err)
	}
	got, err := s.GetHost(ctx, h.ID)
	if err != nil {
		t.Fatalf("GetHost: %v", err)
	}
	if got.Throttle.Level != 2 || got.Throttle.Reason != want.Reason || got.Throttle.Since == nil || !got.Throttle.Since.Equal(since) {
		t.Fatalf("throttle = %+v after a round trip, want %+v", got.Throttle, want)
	}
	if got.EffectiveCapacity() != 2 {
		t.Fatalf("effective capacity = %d, want 2 from a level-2 throttle on 4 slots", got.EffectiveCapacity())
	}

	// The heartbeat's path and the operator's path both leave it alone.
	got.Capacity = 8
	if err := s.UpdateHost(ctx, got); err != nil {
		t.Fatalf("UpdateHost: %v", err)
	}
	if err := s.SetHostReported(ctx, got); err != nil {
		t.Fatalf("SetHostReported: %v", err)
	}
	if err := s.SetHostUsage(ctx, h.ID, HostUsage{}); err != nil {
		t.Fatalf("SetHostUsage: %v", err)
	}
	again, err := s.GetHost(ctx, h.ID)
	if err != nil {
		t.Fatalf("GetHost: %v", err)
	}
	if again.Throttle.Level != 2 {
		t.Fatalf("a host's own write moved its throttle: %+v", again.Throttle)
	}
	if err := s.SetHostThrottle(ctx, h.ID, HostThrottle{}, 2); err != nil {
		t.Fatalf("SetHostThrottle: %v", err)
	}
	if again, _ = s.GetHost(ctx, h.ID); again.Throttle.Active() || again.EffectiveCapacity() != 8 {
		t.Fatalf("clearing the throttle did not give the host its capacity back: %+v", again)
	}
	if err := s.SetHostThrottle(ctx, "host_missing", HostThrottle{Level: 1}, 0); !errors.Is(err, ErrNotFound) {
		t.Fatalf("a throttle written for a host that does not exist: %v", err)
	}
	// Two deciders, one decision. The write says which rung it was decided
	// from, and a rung that has moved since -- a heartbeat that got there
	// first, or an operator's clear -- refuses the stale one rather than
	// recording it twice or undoing the clear.
	if err := s.SetHostThrottle(ctx, h.ID, HostThrottle{Level: 1}, 0); err != nil {
		t.Fatalf("SetHostThrottle from rung 0: %v", err)
	}
	if err := s.SetHostThrottle(ctx, h.ID, HostThrottle{Level: 1}, 0); !errors.Is(err, ErrThrottleMoved) {
		t.Fatalf("a decision from a rung the host has left was written: %v", err)
	}
	if got, _ := s.GetHost(ctx, h.ID); got.Throttle.Level != 1 {
		t.Fatalf("the refused write moved the rung: %+v", got.Throttle)
	}
}

// A host's row counts its live runners on every read, and now also how many
// of them nothing binds: a sustained CPU hold means overload only on a host
// carrying a runner that can take more than its share, so the count has to
// come from the rows rather than from memory, and it has to leave finished
// runners out exactly as the slot count does.
func TestAHostCountsTheLiveRunnersNoCPULimitBinds(t *testing.T) {
	s := newTestStore(t)
	_, p, h := seedPool(t, s)
	ctx := context.Background()
	limited := &Runner{PoolID: p.ID, HostID: h.ID, Name: "limited", AllocatedCPUs: 2, AllocationSource: AllocationFromHost}
	unlimited := &Runner{PoolID: p.ID, HostID: h.ID, Name: "unlimited"}
	finished := &Runner{PoolID: p.ID, HostID: h.ID, Name: "finished"}
	for _, r := range []*Runner{limited, unlimited, finished} {
		if err := s.CreateRunner(ctx, r); err != nil {
			t.Fatalf("CreateRunner: %v", err)
		}
	}
	if _, err := s.TransitionRunner(ctx, finished.ID, RunnerFailed, "gone"); err != nil {
		t.Fatalf("TransitionRunner: %v", err)
	}
	got, err := s.GetHost(ctx, h.ID)
	if err != nil {
		t.Fatalf("GetHost: %v", err)
	}
	if got.ActiveRunners != 2 || got.UnlimitedRunners != 1 {
		t.Fatalf("active = %d, unlimited = %d; want 2 live of which 1 has no CPU limit", got.ActiveRunners, got.UnlimitedRunners)
	}
}

// The throttle column is JSON, and it has to come back from every shape the
// driver hands over: a string, bytes, and the NULL a row written before the
// column existed would have carried had the migration not defaulted it.
func TestAThrottleScansFromEveryShapeTheDriverOffers(t *testing.T) {
	since := time.Date(2026, 9, 13, 12, 0, 0, 0, time.UTC)
	want := HostThrottle{Level: 2, Since: &since, Reason: "memory at the reserve"}
	v, err := want.Value()
	if err != nil {
		t.Fatalf("Value: %v", err)
	}
	raw, ok := v.(string)
	if !ok {
		t.Fatalf("Value() = %T, want the JSON as a string", v)
	}
	for name, in := range map[string]any{"string": raw, "bytes": []byte(raw)} {
		var got HostThrottle
		if err := got.Scan(in); err != nil {
			t.Fatalf("Scan(%s): %v", name, err)
		}
		if got.Level != 2 || got.Reason != want.Reason || got.Since == nil || !got.Since.Equal(since) {
			t.Fatalf("Scan(%s) = %+v, want %+v", name, got, want)
		}
	}
	var empty HostThrottle
	if err := empty.Scan(nil); err != nil || empty.Active() {
		t.Fatalf("Scan(nil) = %+v, %v; want an inactive throttle and no error", empty, err)
	}
	if err := empty.Scan(42); err == nil {
		t.Fatal("a number scanned as a throttle")
	}
}
