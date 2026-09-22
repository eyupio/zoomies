package scheduler

import (
	"strings"
	"testing"
	"time"

	"github.com/eyupio/zoomies/internal/store"
)

func withUsage(h *store.Host, cpu float64, memoryMB int64) *store.Host {
	h.Usage = store.HostUsage{CPUPercent: &cpu, MemoryAvailableMB: &memoryMB, SampledAt: now}
	return h
}

func TestPlacementUsesHostSizeAndMeasuredHeadroomInsteadOfFreeSlots(t *testing.T) {
	p := limited("build", 2, 2048)
	for _, tc := range []struct {
		name string
		a, b *store.Host
		want string
	}{
		{"larger machine", sized("a", 4, 4, 8192, 100000), sized("b", 4, 16, 32768, 100000), "b"},
		{"busy machine has more slots", withUsage(sized("a", 10, 16, 32768, 100000), 90, 28000), withUsage(sized("b", 4, 16, 32768, 100000), 20, 28000), "b"},
		{"outside work consumes memory", withUsage(sized("a", 10, 16, 32768, 100000), 10, 1024), withUsage(sized("b", 4, 8, 16384, 100000), 40, 12000), "b"},
		{"equal measurements break ties by ID", withUsage(sized("a", 4, 8, 16384, 100000), 20, 12000), withUsage(sized("b", 4, 8, 16384, 100000), 20, 12000), "a"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			hs := newHostSet([]*store.Host{tc.b, tc.a}, []*store.Pool{p}, nil, now)
			if got := hs.place(p, 1); len(got) != 1 || got[0] != tc.want {
				t.Fatalf("placed on %v, want %s", got, tc.want)
			}
		})
	}
}

// placeAvoiding is a preference, not a refusal: a host named in avoid is
// skipped in favour of any other eligible host, but is still used once it is
// the only one left, because a host that failed a create a few minutes ago is
// still better than no host at all.
func TestPlaceAvoidingPrefersAnyOtherEligibleHost(t *testing.T) {
	a := sized("a", 4, 16, 32768, 100000)
	b := sized("b", 4, 16, 32768, 100000)
	p := limited("build", 2, 2048)

	hs := newHostSet([]*store.Host{a, b}, []*store.Pool{p}, nil, now)
	if got := hs.placeAvoiding(p, 1, map[string]bool{"a": true}); len(got) != 1 || got[0] != "b" {
		t.Fatalf("placed on %v, want the host not in avoid", got)
	}

	hs = newHostSet([]*store.Host{a}, []*store.Pool{p}, nil, now)
	if got := hs.placeAvoiding(p, 1, map[string]bool{"a": true}); len(got) != 1 || got[0] != "a" {
		t.Fatalf("placed on %v, want the only host even though it is avoided", got)
	}
}

func TestPlacementRebalancesEachNewReservationAcrossPools(t *testing.T) {
	a := withUsage(sized("a", 8, 16, 32768, 100000), 10, 30000)
	b := withUsage(sized("b", 8, 16, 32768, 100000), 10, 30000)
	p, q := limited("one", 4, 4096), limited("two", 4, 4096)
	hs := newHostSet([]*store.Host{a, b}, []*store.Pool{p, q}, nil, now)
	if got := hs.place(p, 1); len(got) != 1 || got[0] != "a" {
		t.Fatal(got)
	}
	if got := hs.place(q, 1); len(got) != 1 || got[0] != "b" {
		t.Fatalf("second pool ignored first reservation: %v", got)
	}
}

func TestPendingStartsAreChargedBetweenPassesWithoutChargingBusyMemoryTwice(t *testing.T) {
	p := limited("build", 1, 4096)
	for _, state := range []store.RunnerState{store.RunnerProvisioning, store.RunnerRegistering, store.RunnerBusy} {
		t.Run(string(state), func(t *testing.T) {
			h := withUsage(sized("host_a", 4, 8, 16384, 100000), 20, 6656)
			h.ActiveRunners = 1
			r := testRunner("existing", p, state, time.Minute)
			hs := newHostSet([]*store.Host{h}, []*store.Pool{p}, map[string][]*store.Runner{p.ID: {r}}, now)
			got := hs.place(p, 1)
			if state == store.RunnerBusy && len(got) != 1 {
				t.Fatal("busy memory was subtracted twice")
			}
			if state != store.RunnerBusy && len(got) != 0 {
				t.Fatal("another pass reused memory promised to a pending start")
			}
		})
	}
}

func TestPressureHoldsNewWorkButLeavesBusyRunnersAndManualLimitsAlone(t *testing.T) {
	p := limited("build", 1, 1024)
	h := withUsage(sized("host_a", 4, 8, 16384, 100000), 99, 12000)
	h.Usage.CPUHeld = true
	h.ActiveRunners = 1
	r := testRunner("busy", p, store.RunnerBusy, time.Minute)
	hs := newHostSet([]*store.Host{h}, []*store.Pool{p}, map[string][]*store.Runner{p.ID: {r}}, now)
	if got := hs.place(p, 2); len(got) != 0 {
		t.Fatalf("held host got %v", got)
	}
	if !strings.Contains(hs.why(p).what, "pressure") {
		t.Fatal("hold had no useful reason")
	}
	if r.State != store.RunnerBusy || h.Capacity != 4 || h.Cordoned {
		t.Fatal("pressure changed existing work or operator settings")
	}
	h.Usage.CPUHeld = false
	*h.Usage.CPUPercent = 30
	if got := hs.place(p, 1); len(got) != 1 {
		t.Fatal("recovery did not allow new work")
	}
	h.Cordoned = true
	if got := hs.place(p, 1); len(got) != 0 {
		t.Fatal("recovery undid manual cordon")
	}
}

func TestBusyCPUAllowsOnlyOneStartUntilItRegisters(t *testing.T) {
	p := limited("build", 1, 1024)
	h := withUsage(sized("host_a", 8, 16, 32768, 100000), 90, 28000)
	hs := newHostSet([]*store.Host{h}, []*store.Pool{p}, nil, now)
	if got := hs.place(p, 4); len(got) != 1 {
		t.Fatalf("got %d simultaneous starts under pressure", len(got))
	}
	r := testRunner("pending", p, store.RunnerProvisioning, time.Second)
	hs = newHostSet([]*store.Host{h}, []*store.Pool{p}, map[string][]*store.Runner{p.ID: {r}}, now)
	if got := hs.place(p, 1); len(got) != 0 {
		t.Fatal("next pass ignored pending startup")
	}
}

func TestStaleAndMissingUsageRetainReservationGuards(t *testing.T) {
	p := limited("build", 2, 4096)
	h := withUsage(sized("host_a", 8, 8, 16384, 100000), 99, 0)
	h.Usage.CPUHeld = true
	h.Usage.SampledAt = now.Add(-store.HostUsageMaxAge)
	hs := newHostSet([]*store.Host{h}, []*store.Pool{p}, nil, now)
	if got := hs.place(p, 8); len(got) != 3 {
		t.Fatalf("stale reading blocked work or erased reservations: %v", got)
	}
	h.Usage.SampledAt = now
	hs = newHostSet([]*store.Host{h}, []*store.Pool{p}, nil, now)
	if got := hs.place(p, 1); len(got) != 0 {
		t.Fatal("fresh zero memory was treated as missing")
	}
	old := newHostSet([]*store.Host{testHost("a", 2, 0), testHost("b", 5, 1)}, []*store.Pool{p}, nil, now)
	if got := old.place(p, 1); len(got) != 1 || got[0] != "b" {
		t.Fatalf("old agent no longer placed by slots: %v", got)
	}
}

// The reserve is held back inside Allocatable, and a pool that leaves its size
// to the host asks for a share of that same figure. Taking the reserve off the
// measured sample as well made measured headroom permanently one reserve
// tighter than the share, so a host cut into a single slot could never fit the
// runner it was sized for: it reported "short of memory" at every pass, on an
// idle machine with tens of gigabytes free, and its slot was never used.
func TestASingleSlotHostFitsTheRunnerItsOwnSizeAsksFor(t *testing.T) {
	p := testPool("build", "build")
	h := sized("host_a", 1, 8, 31*1024, 100000)
	withUsage(h, 10, h.Allocatable().MemoryMB)

	hs := newHostSet([]*store.Host{h}, []*store.Pool{p}, nil, now)
	if got := hs.place(p, 1); len(got) != 1 {
		t.Fatalf("placed on %v, want the idle host's only slot", got)
	}
}

// An idle host that is using more for its own operating system than the reserve
// guessed -- Docker, containerd and a VPN on a 31 GB machine is enough -- has
// less measured free memory than its allocatable figure. A pool that leaves its
// size to the host asks for a share of allocatable, and on a host of one slot
// that share is the whole of it, so comparing the share with what was measured
// free refused the slot at every pass, on a machine running nothing. The Hosts
// page, which counts from allocatable, said the slot was free all along.
//
// This is the case the test above does not cover: there, measured free memory
// was set exactly equal to allocatable, the one value at which it worked.
func TestAnIdleHostTakesAHostSizedRunnerWhenItsOperatingSystemUsesMoreThanTheReserve(t *testing.T) {
	p := testPool("build", "build")
	h := sized("host_a", 1, 4, 31*1024, 100000)
	// Three gigabytes in use by the machine itself, against a reserve of
	// about one and a half.
	withUsage(h, 5, h.MemoryMB-3*1024)
	if avail, alloc := *h.Usage.MemoryAvailableMB, h.Allocatable().MemoryMB; avail >= alloc {
		t.Fatalf("the fixture does not reproduce the fault: %d MB available is not below %d MB allocatable", avail, alloc)
	}

	hs := newHostSet([]*store.Host{h}, []*store.Pool{p}, nil, now)
	if got := hs.place(p, 1); len(got) != 1 {
		t.Fatalf("placed on %v, want the idle host's only slot", got)
	}
}

// What the measured check is for still holds once the host runs work of ours:
// a second host-sized runner is refused when what is measured free cannot
// carry it, because the runners already there are using that memory.
func TestAHostAlreadyRunningOurWorkStillMeasuresBeforeTakingMore(t *testing.T) {
	p := testPool("build", "build")
	h := sized("host_a", 2, 8, 32*1024, 100000)
	h.ActiveRunners = 1
	r := testRunner("existing", p, store.RunnerBusy, time.Minute)
	r.HostID = h.ID
	// The busy runner is using most of the machine.
	withUsage(h, 40, 6*1024)

	hs := newHostSet([]*store.Host{h}, []*store.Pool{p}, map[string][]*store.Runner{p.ID: {r}}, now)
	if got := hs.place(p, 1); len(got) != 0 {
		t.Fatalf("placed on %v; a host whose runner is using its memory must not be handed another share it has not got", got)
	}
}

// A pool that states its size is asking for that much, so an idle host must
// have it free: the leniency is for a share the host chose, not for a figure
// an operator wrote down.
func TestAnIdleHostStillRefusesAStatedSizeItHasNotGotFree(t *testing.T) {
	p := limited("build", 2, 8192)
	h := sized("host_a", 1, 4, 16*1024, 100000)
	withUsage(h, 5, 6*1024)

	hs := newHostSet([]*store.Host{h}, []*store.Pool{p}, nil, now)
	if got := hs.place(p, 1); len(got) != 0 {
		t.Fatalf("placed on %v; an 8 GB runner does not fit in 6 GB free, whatever the host is otherwise doing", got)
	}
}
