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
