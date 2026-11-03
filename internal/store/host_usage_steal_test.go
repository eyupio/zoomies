package store

import (
	"math"
	"testing"
	"time"
)

func stealUsage(cpu, steal float64) HostUsage {
	return HostUsage{CPUPercent: &cpu, StealPercent: &steal}
}

// What an agent sends is checked, as I/O wait is: steal is part of what CPU
// calls busy and cannot be more than it, so a figure that is, or that lies
// outside 0..100, is an agent that read something other than /proc/stat.
func TestAnImpossibleStealIsNotRecorded(t *testing.T) {
	now := time.Date(2026, 11, 3, 12, 0, 0, 0, time.UTC)
	for name, u := range map[string]HostUsage{
		"negative":               stealUsage(70, -1),
		"over the whole":         stealUsage(100, 101),
		"not a number":           stealUsage(70, math.NaN()),
		"infinite":               stealUsage(70, math.Inf(1)),
		"more than busy":         stealUsage(40, 60),
		"a little over rounding": stealUsage(40, 40.2),
	} {
		if got := ObserveHostUsage(HostUsage{}, u, 0, 16384, now); got.StealPercent != nil {
			t.Errorf("%s: steal %v was recorded: %+v", name, *got.StealPercent, got)
		}
	}
}

func TestStealUpToTheBusyShareIsRecordedWithItsCPUFigure(t *testing.T) {
	now := time.Date(2026, 11, 3, 12, 0, 0, 0, time.UTC)
	for name, u := range map[string]HostUsage{
		"half of the busy time":   stealUsage(70, 35),
		"all of it":               stealUsage(70, 70),
		"a measured zero":         stealUsage(70, 0),
		"a tenth of a point over": stealUsage(40, 40.05),
	} {
		got := ObserveHostUsage(HostUsage{}, u, 0, 16384, now)
		if got.StealPercent == nil || *got.StealPercent != *u.StealPercent {
			t.Errorf("%s: steal = %v, want %v", name, got.StealPercent, *u.StealPercent)
		}
		if got.CPUPercent == nil || *got.CPUPercent != *u.CPUPercent {
			t.Errorf("%s: the CPU figure beside it was lost: %+v", name, got)
		}
	}
}

// Steal and busy are shares of the same interval. Without a CPU figure there is
// nothing for steal to be a part of, and nothing to hold it against.
func TestStealIsNotRecordedWithoutACPUFigure(t *testing.T) {
	now := time.Date(2026, 11, 3, 12, 0, 0, 0, time.UTC)
	steal := 30.0
	mem := int64(4096)
	got := ObserveHostUsage(HostUsage{}, HostUsage{StealPercent: &steal, MemoryAvailableMB: &mem}, 0, 16384, now)
	if got.StealPercent != nil {
		t.Fatalf("steal %v was recorded with no CPU figure: %+v", *got.StealPercent, got)
	}
}
