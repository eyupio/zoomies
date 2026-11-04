package controller

import (
	"testing"

	"github.com/eyupio/zoomies/internal/machine"
)

func reading(cpu, steal float64) func() (int, int64, machine.Usage, bool) {
	return func() (int, int64, machine.Usage, bool) {
		load, mem := 2.5, int64(5000)
		return 4, 8192, machine.Usage{CPUPercent: &cpu, StealPercent: &steal, LoadAverage1: &load, MemoryAvailableMB: &mem}, true
	}
}

// A controller whose agents are all elsewhere has no host of its own, so the
// machine it runs on is read by the controller itself, on the tick that already
// runs every thirty seconds, and served from what that tick left.
func TestTheControllersOwnMachineIsSampledOnTheHousekeepingTick(t *testing.T) {
	h := newHarness(t)
	if v := h.c.ControllerMachine(); v.Usage != nil || v.UsageFresh {
		t.Fatalf("a controller that has not sampled reported %+v", v)
	}
	h.c.machine.read = reading(68, 36)
	h.c.housekeep(h.ctx, &housekeeping{})

	v := h.c.ControllerMachine()
	if v.CPUs != 4 || v.MemoryMB != 8192 {
		t.Errorf("size = %d CPUs, %d MB, want the 4 and 8192 the machine reported", v.CPUs, v.MemoryMB)
	}
	if v.Usage == nil || v.Usage.CPUPercent == nil || *v.Usage.CPUPercent != 68 || v.Usage.StealPercent == nil || *v.Usage.StealPercent != 36 ||
		v.Usage.LoadAverage1 == nil || *v.Usage.LoadAverage1 != 2.5 || v.Usage.MemoryAvailableMB == nil || *v.Usage.MemoryAvailableMB != 5000 {
		t.Fatalf("usage = %+v, want every figure the sample held", v.Usage)
	}
	if !v.UsageFresh {
		t.Error("a reading taken this tick is not fresh")
	}
}

// A request reads what the tick sampled and never samples: the figures are shares
// of an interval between two samples, and a request would set it. So a reading
// that the tick has stopped refreshing goes stale on the clock and says so.
func TestAnOldReadingOfTheControllersMachineIsNotFresh(t *testing.T) {
	h := newHarness(t)
	h.c.machine.read = reading(30, 0)
	h.c.sampleMachine()
	if !h.c.ControllerMachine().UsageFresh {
		t.Fatal("a reading just taken is not fresh")
	}
	h.advance(2 * 60 * 1e9) // two minutes, past the 90 seconds a reading lasts
	v := h.c.ControllerMachine()
	if v.UsageFresh || v.Usage == nil {
		t.Fatalf("after two minutes: fresh=%v usage=%v, want the old figures still said but not called fresh", v.UsageFresh, v.Usage)
	}
}

// Every figure goes through the checks an agent's does, so the two routes cannot
// disagree about what is possible: steal is part of busy and cannot exceed it.
func TestAnImpossibleStealOnTheControllersMachineIsDropped(t *testing.T) {
	h := newHarness(t)
	h.c.machine.read = reading(40, 80)
	h.c.sampleMachine()
	v := h.c.ControllerMachine()
	if v.Usage == nil || v.Usage.StealPercent != nil {
		t.Fatalf("usage = %+v, want the CPU figure kept and the steal that exceeds it dropped", v.Usage)
	}
	if v.Usage.CPUPercent == nil || *v.Usage.CPUPercent != 40 {
		t.Errorf("the CPU figure beside the dropped steal was lost: %+v", v.Usage)
	}
}

// A platform with no procfs has no machine to describe, and the controller says
// nothing rather than a machine of no CPUs.
func TestAControllerWithNoProcfsReportsNoMachine(t *testing.T) {
	h := newHarness(t)
	h.c.machine.read = reading(30, 0)
	h.c.sampleMachine()
	h.c.machine.read = func() (int, int64, machine.Usage, bool) { return 0, 0, machine.Usage{}, false }
	h.c.sampleMachine()
	if v := h.c.ControllerMachine(); v.CPUs != 0 || v.MemoryMB != 0 || v.Usage != nil || v.UsageFresh {
		t.Fatalf("a machine that could not be read reported %+v, want nothing", v)
	}
}

// The machine's busy figure counts the container runtime, the tunnel and the
// hypervisor's share as well as Zoomies', so the controller reports its own share
// of the same interval beside it, and the two can be subtracted.
func TestTheControllersOwnShareIsReportedBesideTheMachinesBusyTime(t *testing.T) {
	h := newHarness(t)
	h.c.machine.read = reading(68, 36)
	h.c.machine.readProcess = func(cpus int) (float64, bool) {
		if cpus != 4 {
			t.Errorf("share asked for %d CPUs, want the 4 the machine has", cpus)
		}
		return 12.5, true
	}
	h.c.sampleMachine()
	u := h.c.ControllerMachine().Usage
	if u == nil || u.ProcessCPUPercent == nil || *u.ProcessCPUPercent != 12.5 {
		t.Fatalf("usage = %+v, want the controller's 12.5%% beside the machine's 68%%", u)
	}
}

// A process is part of the machine's busy time, so one that reads as more than the
// machine did is a measurement of something else, and is dropped as an agent's
// impossible figure is.
func TestAControllerShareAboveTheMachinesBusyTimeIsDroppedAndTheMachineKept(t *testing.T) {
	h := newHarness(t)
	h.c.machine.read = reading(40, 0)
	h.c.machine.readProcess = func(int) (float64, bool) { return 60, true }
	h.c.sampleMachine()
	u := h.c.ControllerMachine().Usage
	if u == nil || u.ProcessCPUPercent != nil {
		t.Fatalf("usage = %+v, want the impossible share dropped", u)
	}
	if u.CPUPercent == nil || *u.CPUPercent != 40 {
		t.Errorf("the machine's figure beside the dropped share was lost: %+v", u)
	}
}

// The two are read a moment apart and counted in hundredths of a second, so a
// share a hair over the machine's is rounding and not a contradiction.
func TestAControllerShareJustOverTheMachinesBusyTimeIsKeptAsRounding(t *testing.T) {
	h := newHarness(t)
	h.c.machine.read = reading(40, 0)
	h.c.machine.readProcess = func(int) (float64, bool) { return 41.5, true }
	h.c.sampleMachine()
	if u := h.c.ControllerMachine().Usage; u == nil || u.ProcessCPUPercent == nil {
		t.Fatalf("usage = %+v, want a share within the slack kept", u)
	}
}

func TestNoControllerShareIsReportedWhereTheSamplerHasNone(t *testing.T) {
	h := newHarness(t)
	h.c.machine.read = reading(40, 0)
	h.c.machine.readProcess = func(int) (float64, bool) { return 0, false }
	h.c.sampleMachine()
	if u := h.c.ControllerMachine().Usage; u == nil || u.ProcessCPUPercent != nil {
		t.Fatalf("usage = %+v, want the machine reported and no share invented", u)
	}
}

// The share is of the interval since the last call, so a tick that skipped the
// call because the machine figure was missing would make the next interval twice
// as long and the next share half as big.
func TestTheControllersShareIsTakenOnEveryTickEvenWhenTheMachineHasNoCPUFigureYet(t *testing.T) {
	h := newHarness(t)
	calls := 0
	h.c.machine.readProcess = func(int) (float64, bool) { calls++; return 5, true }
	h.c.machine.read = func() (int, int64, machine.Usage, bool) {
		mem := int64(5000)
		return 4, 8192, machine.Usage{MemoryAvailableMB: &mem}, true
	}
	h.c.sampleMachine()
	if calls != 1 {
		t.Fatalf("the share was taken %d times on a tick with no CPU figure, want once", calls)
	}
	if u := h.c.ControllerMachine().Usage; u == nil || u.ProcessCPUPercent != nil {
		t.Fatalf("usage = %+v, want no share beside a machine with no CPU figure to compare it with", u)
	}
}
