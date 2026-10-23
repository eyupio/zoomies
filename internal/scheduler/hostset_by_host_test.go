package scheduler

import (
	"fmt"
	"math/rand"
	"reflect"
	"testing"
	"time"

	"github.com/eyupio/zoomies/internal/store"
)

// newHostSet seeds every host with what its live runners have promised away. It
// used to look through every pool and every runner for the ones on each host,
// three times over, which is hosts times runners and was 73% of Decide on a quiet
// fleet of a hundred pools; it now adds the runners up by host in one pass. The
// functions below are the ones it replaced, kept as they were, and the tests hold
// the new ones to the same answer over a great many random fleets.

// newHostSet seeds each host with its allocatable resources less what the
// runners already on it have been promised.
//
// Only live runners count, exactly as the slot count does: the snapshot keeps
// failed rows so the Runners page can show them, and charging a host for a
// runner that is gone would shrink the fleet every time one failed.
//
// Disk is the exception, and deliberately: free disk is a measurement of the
// filesystem as it is now, so what the runners already there have written is
// in the number already. Subtracting their reservations too would charge the
// same bytes twice. It is charged only for the runners this pass adds, which
// have written nothing yet.
func referenceNewHostSet(hosts []*store.Host, pools []*store.Pool, runners map[string][]*store.Runner, now time.Time) *hostSet {
	hs := &hostSet{
		hosts:          sortedHosts(hosts),
		free:           make(map[string]int, len(hosts)),
		observedCPU:    make(map[string]float64, len(hosts)),
		warming:        make(map[string]int, len(hosts)),
		warmingDinD:    make(map[string]int, len(hosts)),
		left:           make(map[string]Reservation, len(hosts)),
		alloc:          make(map[string]store.HostAllocation, len(hosts)),
		promisedMemory: make(map[string]int64, len(hosts)),
		ours:           make(map[string]bool, len(hosts)),
		now:            now,
	}
	for _, h := range hs.hosts {
		hs.free[h.ID] = h.Free()
		a := h.Allocatable()
		hs.alloc[h.ID] = a
		hs.left[h.ID] = Reservation{CPUs: a.CPUs, MemoryMB: a.MemoryMB, DiskMB: a.DiskMB}
	}
	for _, h := range hs.hosts {
		// One sum, shared with the host view: what the page shows a host has
		// promised away and what a pass starts from must be the same number,
		// or an operator reads one figure and the scheduler acts on another.
		res := referenceReserved(h, pools, runners)
		l := hs.left[h.ID]
		l.CPUs -= res.CPUs
		l.MemoryMB -= res.MemoryMB
		if hs.alloc[h.ID].DiskKnown {
			l.DiskMB -= res.DiskMB
		}
		hs.promisedMemory[h.ID] = l.MemoryMB
		hs.ours[h.ID] = referenceHostRunsOurs(h, runners)
		pending := hs.referencePending(h, pools, runners)
		if h.Usage.Fresh(now) {
			if v := h.Usage.MemoryAvailableMB; v != nil {
				// The host sample already includes running work, and what the
				// operating system itself is using is the reserve's whole
				// job, so the reserve is not taken off here. It is already
				// inside Allocatable, which is what a slot's size is measured
				// against; charging it a second time here makes measured
				// headroom permanently one reserve tighter than the figure a
				// pool asks for, and a host cut into a single slot then wants
				// its whole allocatable memory and can never have that much
				// left. That host reported "short of memory" at every pass,
				// at any size of machine, and its slot was never filled.
				// Subtract only starts whose demand the sample cannot yet
				// contain, then take the tighter of measured headroom and
				// reservation headroom.
				available := *v - pending.MemoryMB
				l.MemoryMB = min(l.MemoryMB, max(available, 0))
			}
			// CPU runners are using because it was lent them is not
			// taken: the next elastic plan gives it back to a start.
			if v := h.Usage.UnlentCPUPercent(); v != nil {
				hs.observedCPU[h.ID] = max(float64(h.CPUs)*(1-*v/100)-h.CPUReserve()-pending.CPUs, 0)
			}
		}
		hs.left[h.ID] = l
	}
	return hs
}

// Reserved is what the live runners already on h have promised away.
func referenceReserved(h *store.Host, pools []*store.Pool, runners map[string][]*store.Runner) Reservation {
	var out Reservation
	if h == nil {
		return out
	}
	for _, p := range pools {
		if p == nil {
			continue
		}
		for _, r := range runners[p.ID] {
			if r == nil || r.HostID != h.ID || !r.State.Live() {
				continue
			}
			res := RunnerCharge(p, h, r)
			out.CPUs += res.CPUs
			out.MemoryMB += res.MemoryMB
			out.DiskMB += res.DiskMB
		}
	}
	return out
}

// hostRunsOurs reports whether any live runner of ours is on h, in any pool.
func referenceHostRunsOurs(h *store.Host, runners map[string][]*store.Runner) bool {
	for _, rs := range runners {
		for _, r := range rs {
			if r != nil && r.HostID == h.ID && r.State.Live() {
				return true
			}
		}
	}
	return false
}

func (hs *hostSet) referencePending(h *store.Host, pools []*store.Pool, runners map[string][]*store.Runner) Reservation {
	var out Reservation
	for _, p := range pools {
		if p == nil {
			continue
		}
		for _, r := range runners[p.ID] {
			if r == nil || r.HostID != h.ID || !r.State.Live() {
				continue
			}
			starting := r.State == store.RunnerProvisioning || r.State == store.RunnerRegistering
			if starting {
				hs.warming[h.ID]++
				if p.DockerMode == store.DockerDinD {
					hs.warmingDinD[h.ID]++
				}
			}
			if starting || r.CreatedAt.After(h.Usage.SampledAt) ||
				(r.ContainerStartedAt != nil && r.ContainerStartedAt.After(h.Usage.SampledAt)) {
				res := RunnerCharge(p, h, r)
				out.CPUs += res.CPUs
				out.MemoryMB += res.MemoryMB
			}
		}
	}
	return out
}

// randomHostFleet is a fleet with everything the sums look at: hosts that have
// and have not been sampled, pools with typed and shared sizes, docker-in-docker,
// runners in every state on hosts that exist, hosts that do not and no host at
// all, some reduced, some lent memory, some started after the host's last sample,
// pools the snapshot does not list and rows that are nil.
func randomHostFleet(rng *rand.Rand) ([]*store.Host, []*store.Pool, map[string][]*store.Runner) {
	var hosts []*store.Host
	for i := range 1 + rng.Intn(8) {
		h := sized(fmt.Sprintf("host_%02d", i), 1+rng.Intn(8), 2+rng.Intn(30), int64(2048*(1+rng.Intn(30))), 100_000)
		switch rng.Intn(3) {
		case 1: // a sample taken a little while ago
			h.Usage = store.HostUsage{SampledAt: now.Add(-time.Duration(rng.Intn(60)) * time.Second),
				MemoryAvailableMB: ptr(int64(rng.Intn(60_000))), CPUPercent: ptr(float64(rng.Intn(100)))}
		case 2: // a sample too old to count
			h.Usage = store.HostUsage{SampledAt: now.Add(-10 * time.Minute), MemoryAvailableMB: ptr(int64(1000))}
		}
		hosts = append(hosts, h)
	}
	if rng.Intn(6) == 0 {
		// A fleet does not list a host twice, and if one did, each listing is
		// asked about as though the other were not there.
		hosts = append(hosts, hosts[rng.Intn(len(hosts))])
	}

	var pools []*store.Pool
	runners := map[string][]*store.Runner{}
	states := []store.RunnerState{store.RunnerProvisioning, store.RunnerRegistering, store.RunnerIdle, store.RunnerBusy,
		store.RunnerDraining, store.RunnerRemoved, store.RunnerFailed}
	hostIDs := []string{"", "host_unknown"}
	for _, h := range hosts {
		hostIDs = append(hostIDs, h.ID, h.ID, h.ID)
	}
	for i := range 1 + rng.Intn(10) {
		p := testPool(fmt.Sprintf("pool%02d", i), "self-hosted", fmt.Sprintf("pool%02d", i))
		p.ID = "pool_" + p.Name
		switch rng.Intn(4) {
		case 1:
			p.Resources = store.Resources{CPUs: float64(1 + rng.Intn(4)), MemoryMB: int64(512 * (1 + rng.Intn(8)))}
		case 2:
			p.DockerMode = store.DockerDinD
		case 3:
			p.Resources = store.Resources{CPUs: 2, MemoryMB: 4096, MinCPUs: 1, MinMemoryMB: 2048}
		}
		pools = append(pools, p)
	}
	phantom := &store.Pool{ID: "pool_not_listed", Name: "not-listed"}
	for _, p := range append(append([]*store.Pool{}, pools...), phantom) {
		for k := range rng.Intn(7) {
			if rng.Intn(15) == 0 {
				runners[p.ID] = append(runners[p.ID], nil)
				continue
			}
			r := testRunner(fmt.Sprintf("run_%s_%d", p.Name, k), p, states[rng.Intn(len(states))], time.Duration(rng.Intn(600))*time.Second)
			r.HostID = hostIDs[rng.Intn(len(hostIDs))]
			if rng.Intn(3) == 0 {
				started := now.Add(-time.Duration(rng.Intn(120)) * time.Second)
				r.ContainerStartedAt = &started
			}
			if rng.Intn(3) == 0 {
				r.AllocatedCPUs, r.AllocatedMemoryMB = float64(1+rng.Intn(3)), int64(256*(1+rng.Intn(16)))
				r.AllocationSource = []string{"", store.AllocationReduced, store.AllocationHistory}[rng.Intn(3)]
			}
			if rng.Intn(5) == 0 {
				r.LentMemoryMB = int64(128 * (1 + rng.Intn(8)))
			}
			runners[p.ID] = append(runners[p.ID], r)
		}
	}
	if rng.Intn(4) == 0 {
		pools = append(pools, nil)
	}
	return hosts, pools, runners
}

func TestAddingTheRunnersUpByHostSeedsTheHostsAsLookingThroughAllOfThemForEachDid(t *testing.T) {
	var charged, warming, ours, pendingCharged int
	for seed := range int64(600) {
		rng := rand.New(rand.NewSource(seed))
		hosts, pools, runners := randomHostFleet(rng)

		got := newHostSet(hosts, pools, runners, now)
		want := referenceNewHostSet(hosts, pools, runners, now)
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("seed %d: the host set differs\n got: %+v\nwant: %+v", seed, got, want)
		}

		// The sum the Hosts page shows is the one the pass starts from, for every host at once and for each alone.
		byHost := ReservedByHost(hosts, pools, runners)
		for _, h := range hosts {
			want := referenceReserved(h, pools, runners)
			if byHost[h.ID] != want {
				t.Fatalf("seed %d: ReservedByHost gives host %s %+v; looking through every runner for it gives %+v", seed, h.ID, byHost[h.ID], want)
			}
			if alone := Reserved(h, pools, runners); alone != want {
				t.Fatalf("seed %d: Reserved gives host %s %+v; looking through every runner for it gives %+v", seed, h.ID, alone, want)
			}
			if byHost[h.ID].MemoryMB > 0 {
				charged++
			}
			if got.warming[h.ID] > 0 {
				warming++
			}
			if got.ours[h.ID] {
				ours++
			}
			if got.promisedMemory[h.ID] != got.left[h.ID].MemoryMB {
				pendingCharged++
			}
		}
	}
	// The comparison says something only if the fleets gave it something to compare.
	if charged == 0 || warming == 0 || ours == 0 || pendingCharged == 0 {
		t.Fatalf("the random fleets exercised too little: %d hosts charged, %d warming, %d running ours, %d with pending charges", charged, warming, ours, pendingCharged)
	}
}

func TestReservedByHostLeavesOutAHostThatIsNotThere(t *testing.T) {
	h := sized("host_a", 2, 8, 16*1024, 100_000)
	p := testPool("a", "self-hosted")
	r := testRunner("run_1", p, store.RunnerIdle, time.Minute)
	r.HostID = h.ID
	runners := map[string][]*store.Runner{p.ID: {r}}
	got := ReservedByHost([]*store.Host{nil, h}, []*store.Pool{p}, runners)
	if want := referenceReserved(h, []*store.Pool{p}, runners); len(got) != 1 || got[h.ID] != want || want.MemoryMB == 0 {
		t.Errorf("ReservedByHost = %+v; want only host_a, with the %+v its runner has promised", got, want)
	}
}
