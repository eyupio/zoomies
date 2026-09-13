package store

import (
	"math"
	"testing"
	"time"
)

func TestCPUAdmissionRequiresSustainedPressureAndHasRecoveryHysteresis(t *testing.T) {
	now := time.Date(2026, 9, 13, 12, 0, 0, 0, time.UTC)
	observe := func(old HostUsage, cpu float64, seconds int) HostUsage {
		return ObserveHostUsage(old, HostUsage{CPUPercent: &cpu}, 8192, now.Add(time.Duration(seconds)*time.Second))
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

func TestUsageIgnoresAgentTimestampsAndRejectsImpossibleValues(t *testing.T) {
	now := time.Now()
	zero, full := 0.0, int64(0)
	future := now.Add(time.Hour)
	u := ObserveHostUsage(HostUsage{}, HostUsage{CPUPercent: &zero, MemoryAvailableMB: &full,
		SampledAt: future, CPUHighSince: &future, CPUHeld: true}, 8192, now)
	if u.CPUHeld || u.CPUHighSince != nil || !u.SampledAt.Equal(now) || u.CPUPercent == nil || u.MemoryAvailableMB == nil {
		t.Fatalf("agent control fields or measured zero were mishandled: %+v", u)
	}
	if u.Fresh(now.Add(HostUsageMaxAge)) || u.Fresh(now.Add(-time.Second)) {
		t.Fatal("stale/future sample is fresh")
	}
	for _, cpu := range []float64{-1, 101, math.NaN(), math.Inf(1)} {
		if got := ObserveHostUsage(u, HostUsage{CPUPercent: &cpu}, 8192, now); !got.SampledAt.IsZero() {
			t.Fatalf("invalid CPU accepted: %v", cpu)
		}
	}
	for _, mem := range []int64{-1, 8193} {
		if got := ObserveHostUsage(u, HostUsage{MemoryAvailableMB: &mem}, 8192, now); got.MemoryAvailableMB != nil {
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
