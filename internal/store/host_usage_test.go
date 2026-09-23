package store

import (
	"math"
	"testing"
	"time"
)

func TestCPUAdmissionRequiresSustainedPressureAndHasRecoveryHysteresis(t *testing.T) {
	now := time.Date(2026, 9, 13, 12, 0, 0, 0, time.UTC)
	observe := func(old HostUsage, cpu float64, seconds int) HostUsage {
		return ObserveHostUsage(old, HostUsage{CPUPercent: &cpu}, 0, 8192, now.Add(time.Duration(seconds)*time.Second))
	}
	u := observe(HostUsage{}, 98, 0)
	if u.CPUHeld {
		t.Fatal("one high sample stopped new work")
	}
	u = observe(u, 98, 15)
	if u.CPUHeld {
		t.Fatal("less than 30 seconds of pressure stopped new work")
	}
	u = observe(u, 98, 30)
	if !u.CPUHeld {
		t.Fatal("sustained saturation did not hold admission")
	}
	u = observe(u, 90, 60)
	if !u.CPUHeld {
		t.Fatal("a small CPU dip released the hold")
	}
	u = observe(u, 84, 90)
	if u.CPUHeld {
		t.Fatal("recovered CPU did not release the hold")
	}
	u = observe(u, 98, 120)
	if u.CPUHeld {
		t.Fatal("old pressure was reused after recovery")
	}
	u = observe(u, 98, 240)
	if u.CPUHeld {
		t.Fatal("a stale observation counted as sustained pressure")
	}
}

// A load average is the one figure with no upper bound, so the validation is
// about the lower one and about numbers that are not numbers: a negative,
// a NaN or an infinity is an agent reading the wrong file, and none of them
// may become a reading the throttle then acts on.
func TestLoadAverageIsKeptWhenSaneAndDroppedWhenNot(t *testing.T) {
	now := time.Now()
	for _, v := range []float64{0, 0.5, 17.25, 400} {
		load := v
		u := ObserveHostUsage(HostUsage{}, HostUsage{LoadAverage1: &load}, 0, 8192, now)
		if u.LoadAverage1 == nil || *u.LoadAverage1 != v || !u.Fresh(now) {
			t.Fatalf("a load average of %v was not kept: %+v", v, u)
		}
	}
	for _, v := range []float64{-1, math.NaN(), math.Inf(1), math.Inf(-1)} {
		load := v
		u := ObserveHostUsage(HostUsage{}, HostUsage{LoadAverage1: &load}, 0, 8192, now)
		if u.LoadAverage1 != nil || u.Fresh(now) {
			t.Fatalf("a load average of %v became a measurement: %+v", v, u)
		}
	}
}

func TestUsageIgnoresAgentTimestampsAndRejectsImpossibleValues(t *testing.T) {
	now := time.Now()
	zero, full := 0.0, int64(0)
	future := now.Add(time.Hour)
	u := ObserveHostUsage(HostUsage{}, HostUsage{CPUPercent: &zero, MemoryAvailableMB: &full,
		SampledAt: future, CPUHighSince: &future, CPUHeld: true}, 0, 8192, now)
	if u.CPUHeld || u.CPUHighSince != nil || !u.SampledAt.Equal(now) || u.CPUPercent == nil || u.MemoryAvailableMB == nil {
		t.Fatalf("agent control fields or measured zero were mishandled: %+v", u)
	}
	if u.Fresh(now.Add(HostUsageMaxAge)) || u.Fresh(now.Add(-time.Second)) {
		t.Fatal("stale/future sample is fresh")
	}
	for _, cpu := range []float64{-1, 101, math.NaN(), math.Inf(1)} {
		if got := ObserveHostUsage(u, HostUsage{CPUPercent: &cpu}, 0, 8192, now); !got.SampledAt.IsZero() {
			t.Fatalf("invalid CPU accepted: %v", cpu)
		}
	}
	for _, mem := range []int64{-1, 8193} {
		if got := ObserveHostUsage(u, HostUsage{MemoryAvailableMB: &mem}, 0, 8192, now); got.MemoryAvailableMB != nil {
			t.Fatalf("invalid memory accepted: %d", mem)
		}
	}
}

func TestUsageRoundTripCannotUndoCordonCapacityOrReserves(t *testing.T) {
	s := newTestStore(t)
	_, _, host := seedPool(t, s)
	ctx := t.Context()
	if err := s.SetHostCordoned(ctx, host.ID, true); err != nil {
		t.Fatal(err)
	}
	capacity, reserve := 0, 1
	if err := s.PatchHost(ctx, host.ID, HostChanges{Capacity: &capacity, ReserveCPUs: &reserve}); err != nil {
		t.Fatal(err)
	}
	cpu, mem := 97.0, int64(2048)
	u := HostUsage{CPUPercent: &cpu, MemoryAvailableMB: &mem, SampledAt: s.Now(), CPUHeld: true}
	if err := s.SetHostUsage(ctx, host.ID, u); err != nil {
		t.Fatal(err)
	}
	h, err := s.GetHost(ctx, host.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !h.Cordoned || h.Capacity != 0 || h.ReserveCPUs != 1 || !h.Usage.CPUHeld || h.Usage.CPUPercent == nil || *h.Usage.CPUPercent != cpu {
		t.Fatalf("usage or operator state lost: %+v", h)
	}
}

// A host at full CPU because its runners are using the CPU they were lent is
// doing what the plan chose. Holding new starts for it would punish a boost
// for working; CPU nobody lent still holds them.
func TestLentCPUInUseDoesNotTripTheSustainedCPUHold(t *testing.T) {
	now := time.Now()
	cpu := 99.0
	u := HostUsage{}
	for s := 0; s <= 40; s += 10 {
		u = ObserveHostUsage(u, HostUsage{CPUPercent: &cpu}, 30, 8192, now.Add(time.Duration(s)*time.Second))
	}
	if u.CPUHeld || u.CPUHighSince != nil {
		t.Fatalf("a host at 99%% with 30%% of it lent CPU in use was held: %+v", u)
	}
	if *u.CPUPercent != 99 || u.LentCPUPercent != 30 || *u.UnlentCPUPercent() != 69 {
		t.Fatalf("usage = %+v, want the raw 99%% recorded beside 30%% lent", u)
	}

	u = HostUsage{}
	for s := 0; s <= 40; s += 10 {
		u = ObserveHostUsage(u, HostUsage{CPUPercent: &cpu}, 2, 8192, now.Add(time.Duration(s)*time.Second))
	}
	if !u.CPUHeld {
		t.Fatalf("a host at 99%% with only 2%% of it lent was not held: %+v", u)
	}
}
