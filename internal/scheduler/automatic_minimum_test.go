package scheduler

import (
	"strings"
	"testing"
	"time"

	"github.com/eyupio/zoomies/internal/store"
)

// partlyUsed is the host the automatic-pool tests share: 8 cores and 16 GB
// allocatable over two slots, so a slot's share is 3.75 cores (after the
// host's CPU reserve) and 8 GB. One slot is taken by a fixed-size runner of
// another pool holding 5 cores and 10 GB, which leaves one free slot and 2.5
// cores and 6 GB to go with it -- less than a whole share, more than a runner
// needs.
func partlyUsed(t *testing.T, auto *store.Pool) *hostSet {
	t.Helper()
	h := sized("h", 2, 8, hostFor(16*1024), 100000)
	h.ActiveRunners = 1
	big := limited("big", 5, 10*1024)
	r := &store.Runner{ID: "r1", PoolID: big.ID, HostID: h.ID, State: store.RunnerBusy}
	return newHostSet([]*store.Host{h}, []*store.Pool{big, auto}, map[string][]*store.Runner{big.ID: {r}}, now)
}

func automatic(name string, minCPUs float64, minMemoryMB int64) *store.Pool {
	p := testPool(name, name)
	p.Resources.MinCPUs, p.Resources.MinMemoryMB = minCPUs, minMemoryMB
	return p
}

// The report that found this: an automatic pool on a host with a free slot and
// room for a runner several times over, refused because it only ever asked for
// a whole slot's share. With a minimum it runs, on what the host has left.
func TestAnAutomaticPoolWithAMinimumRunsOnWhatIsLeftOfASlot(t *testing.T) {
	p := automatic("auto", 1, 2048)
	hs := partlyUsed(t, p)

	placed := hs.placeAvoiding(p, 1, nil)
	if len(placed) != 1 || placed[0].hostID != "h" {
		t.Fatalf("placed %+v, want one runner on the partly used host", placed)
	}
	size := placed[0].size
	if size == nil {
		t.Fatal("the runner was placed at a whole share on a host without one left")
	}
	// All of what is left, not the minimum: the minimum is a floor.
	if size.CPUs != 2.5 || size.MemoryMB != 6*1024 {
		t.Errorf("size = %g CPU and %d MB, want what the host has left: 2.5 and 6144", size.CPUs, size.MemoryMB)
	}
	// The charge is what the runner is created with, and the host's slot and
	// room are both gone.
	if hs.free["h"] != 0 {
		t.Errorf("free slots = %d, want 0", hs.free["h"])
	}
}

// Without a minimum nothing changes: an automatic pool is still a whole slot
// or nothing, and the refusal now says what to do about it instead of telling
// the operator to lower limits the pool does not have.
func TestAnAutomaticPoolWithoutAMinimumStillWantsAWholeShare(t *testing.T) {
	p := automatic("auto", 0, 0)
	hs := partlyUsed(t, p)

	if got := hs.placeAvoiding(p, 1, nil); len(got) != 0 {
		t.Fatalf("placed %+v on a host without a whole share left, with no minimum", got)
	}
	b := hs.why(p)
	if !strings.Contains(b.fix, "minimum") || strings.Contains(b.fix, "limits") {
		t.Errorf("fix = %q, want it to point at a minimum, not at limits the pool does not set", b.fix)
	}
}

// A minimum is a way down, never the first choice: a host with a whole share
// left gets a runner at the whole share.
func TestAnAutomaticMinimumNeverShrinksARunnerThatHasAWholeShare(t *testing.T) {
	h := sized("idle", 2, 8, hostFor(16*1024), 100000)
	p := automatic("auto", 1, 2048)
	hs := newHostSet([]*store.Host{h}, []*store.Pool{p}, nil, now)

	placed := hs.placeAvoiding(p, 1, nil)
	if len(placed) != 1 || placed[0].size != nil {
		t.Fatalf("placed %+v, want one runner at the standard share", placed)
	}
}

// Below the minimum is still no.
func TestAnAutomaticPoolDoesNotGoBelowItsMinimum(t *testing.T) {
	p := automatic("auto", 1, 8*1024)
	hs := partlyUsed(t, p)
	if got := hs.placeAvoiding(p, 1, nil); len(got) != 0 {
		t.Fatalf("placed %+v with 6 GB left under an 8 GB minimum", got)
	}
}

// A docker-in-docker slot is split between the runner and its daemon, so it is
// never cut below what that split needs, whatever the minimum says.
func TestAnAutomaticDinDSlotIsNotCutBelowWhatItsPairNeeds(t *testing.T) {
	p := automatic("auto", 0.5, 1024)
	p.DockerMode = store.DockerDinD
	floor := ShareFloor(p)

	h := sized("h", 2, 8, hostFor(16*1024), 100000)
	h.ActiveRunners = 1
	big := limited("big", 6.5, 13*1024)
	r := &store.Runner{ID: "r1", PoolID: big.ID, HostID: h.ID, State: store.RunnerBusy}
	hs := newHostSet([]*store.Host{h}, []*store.Pool{big, p}, map[string][]*store.Runner{big.ID: {r}}, now)
	left := hs.leftFor(h, p)
	if left.CPUs >= floor.CPUs {
		t.Fatalf("test setup: %g cores left is not under the pair's %g", left.CPUs, floor.CPUs)
	}
	if got := hs.placeAvoiding(p, 1, nil); len(got) != 0 {
		t.Fatalf("placed %+v on %g cores, under the %g a runner and its daemon need", got, left.CPUs, floor.CPUs)
	}
}

// A reduced automatic runner is charged what its row says it was given, once:
// its allocation is the whole of a (smaller) slot, shared with its daemon, not
// a per-container figure to double.
func TestAReducedAutomaticRunnerIsChargedWhatItWasGiven(t *testing.T) {
	p := automatic("auto", 1, 2048)
	p.DockerMode = store.DockerDinD
	h := sized("h", 2, 8, hostFor(16*1024), 100000)
	r := &store.Runner{ID: "r", PoolID: p.ID, HostID: h.ID, State: store.RunnerBusy,
		AllocationSource: store.AllocationReduced, AllocatedCPUs: 2.5, AllocatedMemoryMB: 6144}
	got := RunnerCharge(p, h, r)
	if got.CPUs != 2.5 || got.MemoryMB != 6144 {
		t.Errorf("charge = %+v, want 2.5 cores and 6144 MB", got)
	}
}

// A minimum above a small host's share is not a way up: it leaves the share as
// the least the pool asks for there.
func TestAnAutomaticMinimumAboveTheShareChangesNothing(t *testing.T) {
	h := sized("h", 4, 8, hostFor(8*1024), 100000)
	p := automatic("auto", 4, 8*1024)
	if got, want := MinimumReserve(p, h), Reserve(p, h); got != want {
		t.Errorf("MinimumReserve = %+v, want the share %+v", got, want)
	}
}

// End to end through Decide: a queued job for the automatic pool becomes a
// create on the partly used host, carrying the reduced size, with a reason
// that does not name a standard size the pool never set.
func TestAQueuedJobOnAnAutomaticPoolIsCreatedReduced(t *testing.T) {
	h := sized("h", 2, 8, hostFor(16*1024), 100000)
	h.ActiveRunners = 1
	big := limited("big", 5, 10*1024)
	auto := automatic("auto", 1, 2048)
	busy := &store.Runner{ID: "r1", PoolID: big.ID, HostID: h.ID, State: store.RunnerBusy, CreatedAt: now}
	s := snap([]*store.Pool{big, auto}, []*store.Runner{busy}, []*store.Job{queued("j1", time.Minute, "auto")}, []*store.Host{h})

	var creates []Action
	for _, a := range actionsOf(Decide(s).Actions, ActionCreate) {
		if a.PoolID == auto.ID {
			creates = append(creates, a)
		}
	}
	if len(creates) != 1 {
		t.Fatalf("creates for the automatic pool = %+v, want one", creates)
	}
	if creates[0].Size == nil || creates[0].Size.MemoryMB != 6*1024 {
		t.Errorf("size = %+v, want the 6 GB left carried on the action", creates[0].Size)
	}
	if !strings.Contains(creates[0].Reason, "reduced to 2.5 CPU and 6 GB") || strings.Contains(creates[0].Reason, "standard") {
		t.Errorf("reason = %q, want the reduced size and no standard the pool never set", creates[0].Reason)
	}
}
