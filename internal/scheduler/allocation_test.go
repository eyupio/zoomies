package scheduler

import (
	"testing"

	"github.com/eyupio/zoomies/internal/store"
)

// A pool that sets no limit used to get a runner with no limit, charged one
// slot's share of the host it never had to honour. Eight such runners on an
// eight-slot host each took every core, and that is the overload this exists
// to end: the runner is given exactly what it was charged.
func TestAnUnlimitedPoolIsGivenOneSlotsShareOfItsHost(t *testing.T) {
	// 16 CPUs less the floor's 0.8 is 15.2, and 32 GB less the memory floor's
	// twentieth (1638 MB) is 31130 MB; four slots share both.
	h := sized("host_a", 4, 16, 32*1024, 500*1024)
	p := limited("builders", 0, 0)

	got, source := Allocation(p, h, true)
	if got.CPUs != 3.8 || got.MemoryMB != 7782 {
		t.Fatalf("allocation = %+v, want 3.8 CPUs and 7782 MB, one slot's share of the host", got)
	}
	if source != store.AllocationFromHost {
		t.Errorf("source = %q, want %q", source, store.AllocationFromHost)
	}
	// The charge and the allocation are the same figure, which is what makes
	// the books honest: what a host promises away is what its runners can
	// actually take.
	if charge := Reserve(p, h); charge.CPUs < got.CPUs || charge.MemoryMB != got.MemoryMB {
		t.Errorf("Reserve = %+v differs from the allocation %+v", charge, got)
	}
}

// Per field, as the charge is. A pool that set memory has said something about
// memory and nothing about CPU, so the pool answers for one and the host for
// the other -- and the source says the host had a hand in it, because that is
// what decides which setting an operator is sent to when a job runs out.
func TestAPartlyLimitedPoolKeepsItsOwnLimitsAndFillsTheRest(t *testing.T) {
	h := sized("host_a", 4, 16, 32*1024, 500*1024)
	p := limited("builders", 0, 2048)
	p.Resources.PidsLimit = 512
	p.Resources.DiskGB = 10

	got, source := Allocation(p, h, true)
	if got.MemoryMB != 2048 || got.CPUs != 3.8 || got.PidsLimit != 512 || got.DiskGB != 10 {
		t.Fatalf("allocation = %+v, want the pool's memory, pids and disk with the host's CPU share", got)
	}
	if source != store.AllocationFromHost {
		t.Errorf("source = %q, want %q because the CPU came from the host", source, store.AllocationFromHost)
	}

	p = limited("builders", 2, 2048)
	if got, source := Allocation(p, h, true); got != p.Resources || source != store.AllocationFromPool {
		t.Errorf("a fully limited pool was changed: %+v from %q", got, source)
	}
}

// Defaults off is the fleet as it was: the pool's limits are the whole answer,
// and a pool with none gets none. An operator who turns them off is warned at
// startup, and the allocation has to honour the choice rather than the warning.
func TestDefaultsOffLeavesAnUnlimitedPoolUnlimited(t *testing.T) {
	h := sized("host_a", 4, 16, 32*1024, 500*1024)
	p := limited("builders", 0, 0)
	if got, source := Allocation(p, h, false); got != (store.Resources{}) || source != "" {
		t.Fatalf("allocation = %+v from %q with defaults off, want nothing", got, source)
	}
}

// A host that never measured itself has no share to give, so its runners are
// created as they always were. It is the same rule that places such a host by
// slots alone: a figure nobody reported constrains nothing, and inventing one
// would be worse than the overload it was meant to prevent.
func TestAnUnmeasuredHostGivesNoDefault(t *testing.T) {
	h := testHost("host_a", 4, 0)
	h.BackendInfo = store.HostBackends{{Kind: store.BackendDocker, Available: true,
		Limits: store.LimitSupport{Known: true, CPU: true, Memory: true}}}
	p := limited("builders", 0, 0)
	if got, source := Allocation(p, h, true); got != (store.Resources{}) || source != "" {
		t.Fatalf("allocation = %+v from %q on an unmeasured host, want nothing", got, source)
	}
	// Half measured: the CPU share is given, the memory is not.
	h.CPUs = 8
	got, source := Allocation(p, h, true)
	if got.CPUs != 1.87 || got.MemoryMB != 0 || source != store.AllocationFromHost {
		t.Fatalf("allocation = %+v from %q on a host that measured only its CPUs", got, source)
	}
}

// A daemon that cannot apply a quota refuses the container rather than
// ignoring the request: rootless Docker on a host that delegates only memory
// and pids to the user says so in its own probe, and a default CPU limit sent
// there would fail every create on the host. So each field is defaulted only
// where the daemon said it can bind it, and a probe from an agent too old to
// say defaults nothing -- which is what every host did before.
func TestADefaultIsOnlyGivenWhereTheDaemonCanEnforceIt(t *testing.T) {
	h := sized("host_a", 4, 16, 32*1024, 500*1024)
	p := limited("builders", 0, 0)

	h.BackendInfo[0].Limits = store.LimitSupport{Known: true, CPU: false, Memory: true}
	got, source := Allocation(p, h, true)
	if got.CPUs != 0 || got.MemoryMB != 7782 || source != store.AllocationFromHost {
		t.Fatalf("allocation = %+v from %q on a daemon without CFS quotas, want memory alone", got, source)
	}
	h.BackendInfo[0].Limits = store.LimitSupport{}
	if got, source := Allocation(p, h, true); got != (store.Resources{}) || source != "" {
		t.Fatalf("allocation = %+v from %q from an agent that never said what its daemon can do", got, source)
	}
	h.BackendInfo = nil
	if got, source := Allocation(p, h, true); got != (store.Resources{}) || source != "" {
		t.Fatalf("allocation = %+v from %q on a host with no probe at all", got, source)
	}
}

// The process backend applies no limit, so a defaulted figure on one of its
// runners would be a number on the Runners page saying the opposite of the
// truth. Such a runner is recorded as what it is: unlimited.
func TestAProcessPoolGetsNoDefault(t *testing.T) {
	h := sized("host_a", 4, 16, 32*1024, 500*1024)
	h.Backends = store.StringSlice{"process"}
	h.BackendInfo = store.HostBackends{{Kind: store.BackendProcess, Available: true,
		Limits: store.LimitSupport{Known: true, CPU: true, Memory: true}}}
	p := limited("builders", 0, 0)
	p.Backend = store.BackendProcess
	if got, source := Allocation(p, h, true); got != (store.Resources{}) || source != "" {
		t.Fatalf("allocation = %+v from %q on a process pool, want nothing", got, source)
	}
	// The pool's own limits still travel, because the warning about them is
	// the pool's to earn.
	p = limited("builders", 2, 2048)
	p.Backend = store.BackendProcess
	if got, source := Allocation(p, h, true); got != p.Resources || source != store.AllocationFromPool {
		t.Fatalf("allocation = %+v from %q, want the pool's own limits", got, source)
	}
}

// The charge does not move with the throttle. Reserve and Allocation both
// divide by the operator's capacity, never the effective one: if they did not,
// stepping a host from four slots to two would double the charge on every
// runner already there, and the host card's committed figure would jump for
// runners that had not changed.
func TestTheShareIsAShareOfTheConfiguredCapacityWhateverTheThrottle(t *testing.T) {
	h := sized("host_a", 4, 16, 32*1024, 500*1024)
	p := limited("builders", 0, 0)
	before, _ := Allocation(p, h, true)
	charge := Reserve(p, h)
	h.Throttle = store.HostThrottle{Level: 2}
	after, _ := Allocation(p, h, true)
	if after != before || Reserve(p, h) != charge {
		t.Fatalf("the throttle moved the share: %+v -> %+v, charge %+v -> %+v", before, after, charge, Reserve(p, h))
	}
}

// The share is floored to the hundredth, never rounded up: three runners
// rounded up from 0.667 to 0.67 would together be promised a hundredth of a
// core the host does not have, and the daemon refuses a quota above the
// machine.
func TestTheCPUShareIsFlooredToTheHundredth(t *testing.T) {
	if got := shareCPUs(2, 3); got != 0.66 {
		t.Errorf("shareCPUs(2, 3) = %v, want 0.66", got)
	}
	if got := shareCPUs(7.5, 3); got != 2.5 {
		t.Errorf("shareCPUs(7.5, 3) = %v, want 2.5 exactly", got)
	}
	if got := shareCPUs(4, 0); got != 0 {
		t.Errorf("shareCPUs(4, 0) = %v, want 0 for a host with no slots", got)
	}
}

// A share is a request to the daemon, and a daemon refuses a quota above its
// own core count however many cores the agent's machine has -- a Docker
// Desktop VM on a ten-core laptop has four. The host is sized by its daemon,
// so this only matters for a row whose size and probe disagree, and then the
// share is capped rather than the create refused.
func TestTheDefaultShareNeverExceedsTheDaemonsOwnCores(t *testing.T) {
	h := sized("host_a", 1, 10, 32*1024, 500*1024)
	h.BackendInfo[0].CPUs = 4
	p := limited("builders", 0, 0)
	got, _ := Allocation(p, h, true)
	if got.CPUs != 4 {
		t.Fatalf("allocation = %v CPUs, want the daemon's 4 rather than the agent's share of 10", got.CPUs)
	}
}

// A problem that names "each runner's default share" has to ask first whether
// a share is ever given: a host offering only the process backend, or a
// daemon that cannot apply the limit, gives none.
func TestDefaultsBindSaysWhichFieldsAHostCanBeGivenADefaultOn(t *testing.T) {
	h := sized("host_a", 4, 16, 32*1024, 500*1024)
	if cpu, memory := DefaultsBind(h); !cpu || !memory {
		t.Fatalf("a daemon that enforces both reported %v/%v", cpu, memory)
	}
	h.BackendInfo[0].Limits = store.LimitSupport{Known: true, CPU: false, Memory: true}
	if cpu, memory := DefaultsBind(h); cpu || !memory {
		t.Fatalf("a daemon without CFS quotas reported %v/%v", cpu, memory)
	}
	h.BackendInfo = store.HostBackends{{Kind: store.BackendProcess, Available: true, Limits: store.LimitSupport{Known: true, CPU: true, Memory: true}}}
	if cpu, memory := DefaultsBind(h); cpu || memory {
		t.Fatalf("a process-only host reported %v/%v", cpu, memory)
	}
	h.BackendInfo = store.HostBackends{{Kind: store.BackendDocker, Available: true}}
	if cpu, memory := DefaultsBind(h); cpu || memory {
		t.Fatalf("an agent that has not said reported %v/%v", cpu, memory)
	}
}
