package store

import (
	"testing"
	"time"
)

// The hold's figures are settings now, and the defaults have to be the hold as
// it was: the old test above runs on CPUThresholds{} and still passes. This one
// moves every figure and checks that each moves the hold.
func TestCPUHoldFollowsItsSettings(t *testing.T) {
	now := time.Date(2026, 11, 26, 12, 0, 0, 0, time.UTC)
	cpu := CPUThresholds{HoldPercent: 80, HoldWindow: 10 * time.Second, ReleasePercent: 70}
	observe := func(old HostUsage, v float64, seconds int) HostUsage {
		return ObserveHostUsage(old, HostUsage{CPUPercent: &v}, 0, 8192, now.Add(time.Duration(seconds)*time.Second), cpu)
	}
	u := observe(HostUsage{}, 82, 0)
	if u.CPUHeld {
		t.Fatal("one sample above the start line held new starts")
	}
	u = observe(u, 82, 10)
	if !u.CPUHeld {
		t.Fatal("ten seconds above an 80% start line with a 10s window did not hold new starts")
	}
	u = observe(u, 75, 20)
	if !u.CPUHeld {
		t.Fatal("CPU above the 70% release line released the hold")
	}
	u = observe(u, 69, 30)
	if u.CPUHeld {
		t.Fatal("CPU under the 70% release line did not release the hold")
	}
	if got := u.Thresholds(); got.HoldPercent != 80 || got.HoldWindow != 10*time.Second || got.ReleasePercent != 70 {
		t.Fatalf("the measurement does not carry the figures it was judged by: %+v", got)
	}
	if got := u.Thresholds(); got.PacedPercent != DefaultCPUPacedPercent || got.ElasticBusyPercent != DefaultCPUElasticBusyPercent {
		t.Fatalf("unset figures were not given their defaults: %+v", got)
	}
}

// A row written before the figures existed carries none, and is judged as the
// hold always judged it.
func TestUsageWithoutFiguresReadsTheDefaults(t *testing.T) {
	got := HostUsage{}.Thresholds()
	want := CPUThresholds{HoldPercent: 95, HoldWindow: 30 * time.Second, ReleasePercent: 85, PacedPercent: 85, ElasticBusyPercent: 85}
	if got != want {
		t.Fatalf("defaults are %+v, want %+v", got, want)
	}
}
