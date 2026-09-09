package scheduler

import (
	"strings"
	"testing"

	"github.com/eyupio/zoomies/internal/store"
)

// sized is a host that has told the fleet how big it is: CPUs, memory and the
// free disk behind its work directory, which is what the reporting half of
// this work taught agents to measure.
func sized(id string, capacity, cpus int, memoryMB, diskFreeMB int64) *store.Host {
	h := testHost(id, capacity, 0)
	h.CPUs = cpus
	h.MemoryMB = memoryMB
	h.DiskTotalMB = diskFreeMB * 2
	h.DiskFreeMB = diskFreeMB
	return h
}

// limited is a pool that says what one of its runners may consume.
func limited(name string, cpus float64, memoryMB int64) *store.Pool {
	p := testPool(name, name)
	p.Resources = store.Resources{CPUs: cpus, MemoryMB: memoryMB}
	return p
}

// The headline case for the whole reservation model, and the one an operator
// would recognise: a host advertises 32 GB, a pool asks for 4 GB a runner, and
// the ninth runner is the one that would have been killed rather than run.
func TestAHostAdmitsWhatItsMemoryCoversAndRefusesTheNext(t *testing.T) {
	// Capacity is deliberately far above what memory allows, so that only the
	// memory rule can be what stops the ninth.
	h := sized("host_a", 32, 32, 32*1024+store.MinHostReserveMemoryMB, 500*1024)
	p := limited("big", 0, 4096)

	hs := newHostSet([]*store.Host{h}, []*store.Pool{p}, nil, now)
	got := hs.place(p, 12)
	if len(got) != 8 {
		t.Fatalf("placed %d runners on a 32 GB host at 4 GB each, want 8", len(got))
	}
	if more := hs.place(p, 1); len(more) != 0 {
		t.Errorf("the ninth runner was placed on a host with nothing left: %v", more)
	}
}

// Two pools drawing on one host is the case a per-pool check would get wrong:
// each fits on its own, and the host cannot carry both. The slot model already
// had this property and the resource model has to keep it, or the second pool
// is promised memory the first has already been given.
func TestTwoPoolsCannotOversubscribeOneHostsMemory(t *testing.T) {
	h := sized("host_a", 8, 16, 8*1024+store.MinHostReserveMemoryMB, 500*1024)
	first := limited("first", 0, 6*1024)
	second := limited("second", 0, 4*1024)

	hs := newHostSet([]*store.Host{h}, []*store.Pool{first, second}, nil, now)
	if got := hs.place(first, 1); len(got) != 1 {
		t.Fatalf("the first pool was refused a host with 8 GB free: %v", got)
	}
	// 2 GB is left and the second pool wants 4.
	if got := hs.place(second, 1); len(got) != 0 {
		t.Errorf("the second pool was promised memory the first already has: %v", got)
	}
}

// A docker-in-docker pool's build runs in a sidecar that the backend gives the
// same limits as the runner, so the host carries twice what the pool asked
// for. Charging it once would let a fleet promise away a machine twice over.
func TestADockerInDockerRunnerIsChargedForItsSidecar(t *testing.T) {
	h := sized("host_a", 8, 16, 8*1024+store.MinHostReserveMemoryMB, 500*1024)
	p := limited("dind", 0, 4096)
	p.DockerMode = store.DockerDinD

	hs := newHostSet([]*store.Host{h}, []*store.Pool{p}, nil, now)
	got := hs.place(p, 4)
	if len(got) != 1 {
		t.Fatalf("placed %d dind runners on an 8 GB host at 4 GB each, want 1", len(got))
	}

	// The same pool without the sidecar fits twice, which is what says the
	// difference above came from the sidecar and not from the arithmetic.
	plain := limited("plain", 0, 4096)
	hs = newHostSet([]*store.Host{sized("host_b", 8, 16, 8*1024+store.MinHostReserveMemoryMB, 500*1024)}, []*store.Pool{plain}, nil, now)
	if got := hs.place(plain, 4); len(got) != 2 {
		t.Fatalf("placed %d plain runners, want 2", len(got))
	}
}

// The upgrade property, and the one that decides whether this change is safe
// to ship: a host carrying only pools that set no limits admits exactly what
// its capacity admitted before any of this existed. The fallback share is what
// makes that true -- each runner is charged one slot's worth of the machine,
// so the slot count is still what runs out.
func TestAHostOfUnlimitedPoolsStillAdmitsItsCapacity(t *testing.T) {
	for _, capacity := range []int{1, 3, 4, 7, 8} {
		h := sized("host_a", capacity, 30, 30*1024, 500*1024)
		p := testPool("unlimited", "unlimited")

		hs := newHostSet([]*store.Host{h}, []*store.Pool{p}, nil, now)
		got := hs.place(p, capacity+3)
		if len(got) != capacity {
			t.Errorf("capacity %d admitted %d runners; the upgrade changed what a host takes", capacity, len(got))
		}
	}
}

// What the fallback share is actually for. The upgrade property above holds
// with or without it -- a pool charged nothing is bounded by slots, and so is
// a pool charged one slot's worth -- so the case that separates them is a host
// carrying both kinds at once. An unlimited pool that costs nothing lets a
// fleet hand the same memory to a pool that asked for it by name.
func TestAnUnlimitedPoolStillSpendsTheMemoryItTakes(t *testing.T) {
	h := sized("host_a", 2, 8, 8*1024+store.MinHostReserveMemoryMB, 500*1024)
	unlimited := testPool("unlimited", "unlimited")
	named := limited("named", 0, 6*1024)

	hs := newHostSet([]*store.Host{h}, []*store.Pool{unlimited, named}, nil, now)
	if got := hs.place(unlimited, 1); len(got) != 1 {
		t.Fatalf("the unlimited pool was refused an empty host: %v", got)
	}
	// Half the machine is one slot's worth and is now spoken for, so the 6 GB
	// pool does not fit beside it even though a slot is free.
	if got := hs.place(named, 1); len(got) != 0 {
		t.Errorf("6 GB was promised on a host with 4 GB left: %v", got)
	}
}

// An agent that predates the resource fields reports nothing, and nothing is
// not zero. Reading an unmeasured host as an empty one would refuse every
// placement on it, which on a fleet mid-upgrade is every host.
func TestAHostThatReportedNoResourcesIsStillPlacedBySlots(t *testing.T) {
	h := testHost("host_old", 3, 0) // no CPUs, no memory, no disk
	p := limited("big", 8, 64*1024)

	hs := newHostSet([]*store.Host{h}, []*store.Pool{p}, nil, now)
	if got := hs.place(p, 5); len(got) != 3 {
		t.Fatalf("placed %d runners on an unmeasured host with 3 slots, want 3: %v", len(got), got)
	}
}

// The reservation is rebuilt from the runner rows on every pass -- there is no
// second table holding it -- so which rows count is the whole of the
// arithmetic. Live rows count and terminal ones do not, exactly as the host's
// own slot count does: a failed runner is not holding any memory, and charging
// the host for it would shrink the fleet a little with every failure.
func TestOnlyLiveRunnersHoldAReservation(t *testing.T) {
	p := limited("big", 0, 4096)
	h := sized("host_a", 8, 16, 8*1024+store.MinHostReserveMemoryMB, 500*1024)
	// One live runner is holding 4 GB; the failed and removed rows are not.
	h.ActiveRunners = 1
	runners := map[string][]*store.Runner{p.ID: {
		onHost(testRunner("r_live", p, store.RunnerBusy, 0), "host_a"),
		onHost(testRunner("r_failed", p, store.RunnerFailed, 0), "host_a"),
		onHost(testRunner("r_gone", p, store.RunnerRemoved, 0), "host_a"),
	}}

	hs := newHostSet([]*store.Host{h}, []*store.Pool{p}, runners, now)
	if got := hs.place(p, 3); len(got) != 1 {
		t.Fatalf("placed %d runners beside one live 4 GB runner on an 8 GB host, want 1: %v", len(got), got)
	}
}

// Disk is a gate rather than a budget: a host with less free space than its
// reserve takes no runner at all, whatever the pool asks for, because the
// reserve is the room the machine needs to do the work rather than something
// to spend down. Nothing evicts to make room -- retention is ZF-105's.
func TestAHostBelowItsDiskReserveTakesNothing(t *testing.T) {
	full := sized("host_full", 4, 16, 16*1024, store.MinHostReserveDiskMB-1)
	roomy := sized("host_roomy", 4, 16, 16*1024, 200*1024)
	p := testPool("unlimited", "unlimited")

	hs := newHostSet([]*store.Host{full, roomy}, []*store.Pool{p}, nil, now)
	for _, id := range hs.place(p, 4) {
		if id == "host_full" {
			t.Fatalf("a runner was placed on a host below its disk reserve")
		}
	}

	// And the operator is told which resource it was, because "no capacity"
	// would send them to wait for a job that finishing will not help: a runner
	// leaves its caches behind on purpose.
	only := newHostSet([]*store.Host{full}, []*store.Pool{p}, nil, now)
	b := only.why(p)
	if !strings.Contains(b.what, "low on disk") {
		t.Errorf("the reason does not name the disk: %q", b.what)
	}
	if !strings.Contains(b.fix, "free space") {
		t.Errorf("the fix does not say what to free: %q", b.fix)
	}
}

// A machine too small for one runner is a different answer from a machine that
// is busy, and an operator told to wait for the second when they have the
// first waits for ever.
func TestAHostTooSmallForThePoolSaysSoRatherThanSayingItIsFull(t *testing.T) {
	h := sized("host_small", 4, 4, 4*1024, 200*1024)
	p := limited("huge", 0, 32*1024)

	hs := newHostSet([]*store.Host{h}, []*store.Pool{p}, nil, now)
	b := hs.why(p)
	if !strings.Contains(b.what, "too small for this pool's limits") {
		t.Errorf("the reason does not say the machine is too small: %q", b.what)
	}
	if b.atCapacity {
		t.Error("a host too small for the pool was reported as merely busy")
	}
	if !strings.Contains(b.fix, "lower this pool's") {
		t.Errorf("the fix does not name the pool's own limits: %q", b.fix)
	}
}

// The other half of that distinction: a host large enough for the pool that
// has already promised its memory away is short of memory, not too small, and
// waiting is exactly the right advice.
func TestAHostWhoseMemoryIsSpentSaysItIsShortOfMemory(t *testing.T) {
	p := limited("big", 0, 6*1024)
	h := sized("host_a", 8, 16, 8*1024+store.MinHostReserveMemoryMB, 200*1024)
	h.ActiveRunners = 1
	runners := map[string][]*store.Runner{p.ID: {onHost(testRunner("r1", p, store.RunnerBusy, 0), "host_a")}}

	hs := newHostSet([]*store.Host{h}, []*store.Pool{p}, runners, now)
	b := hs.why(p)
	if !strings.Contains(b.what, "short of memory") {
		t.Errorf("the reason does not name memory: %q", b.what)
	}
	if strings.Contains(b.what, "too small") {
		t.Errorf("a host that fits the pool was called too small: %q", b.what)
	}
}

// HostCanRun is the one placement rule, so the wizard's count, the
// capacity-demand signal and image prewarming inherit the fit without asking
// for it. A host that cannot hold one runner of the pool is not a host that
// can run it, whatever else is true of it.
func TestHostCanRunAsksWhetherARunnerFits(t *testing.T) {
	small := sized("host_small", 4, 2, 2*1024, 200*1024)
	big := sized("host_big", 4, 16, 32*1024, 200*1024)
	p := limited("huge", 0, 16*1024)

	if HostCanRun(small, p, now) {
		t.Error("a host with 2 GB was offered a pool that reserves 16")
	}
	if !HostCanRun(big, p, now) {
		t.Error("a host with 32 GB was refused a pool that reserves 16")
	}
}

// The reserve is the operator's, and an agent reports what it sees rather than
// what it may have: memory an operator held back for the machine is not
// memory the fleet may promise to a job.
func TestTheOperatorsReserveIsHeldBackFromPlacement(t *testing.T) {
	p := limited("big", 0, 4096)
	h := sized("host_a", 8, 16, 12*1024, 200*1024)
	h.ReserveMemoryMB = 8 * 1024

	hs := newHostSet([]*store.Host{h}, []*store.Pool{p}, nil, now)
	if got := hs.place(p, 3); len(got) != 1 {
		t.Fatalf("placed %d runners in the 4 GB left after an 8 GB reserve, want 1: %v", len(got), got)
	}
}

// onHost puts a test runner somewhere other than the helper's default host.
func onHost(r *store.Runner, hostID string) *store.Runner {
	r.HostID = hostID
	return r
}
