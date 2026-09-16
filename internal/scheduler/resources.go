package scheduler

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/eyupio/zoomies/internal/store"
)

// Reservation is what one runner of a pool is charged against the host it is
// placed on. It is the admission half of the resource model: the reporting
// half taught agents to measure their machines, and this is what decides that
// the ninth 4 GB runner does not go on a 32 GB box.
//
// It is a charge, not an enforcement. What actually binds a runner is the
// cgroup limit the container backend applies from the same pool `resources`,
// and the process backend applies none at all -- so a reservation says what
// the fleet has promised away, and only the container backends make the
// promise true.
type Reservation struct {
	CPUs     float64
	MemoryMB int64
	DiskMB   int64
}

// cpuEpsilon absorbs the rounding of a share that does not divide evenly. Eight
// slots on a 30-CPU host are 3.75 each, which is exact, but seven are not, and
// a host must not refuse the last runner it has room for because the eighth
// subtraction left 2.7755575615628914e-16 of a CPU owing.
const cpuEpsilon = 1e-6

// Reserve is what one runner of p costs on h.
//
// It is pure, and it is per field. A pool that sets a memory limit and no CPU
// limit has said something about memory and nothing about CPU, so the limit
// answers for memory and the fallback answers for CPU; charging zero for the
// half the operator left alone would let a fleet promise away a machine it had
// measured.
//
// The fallback is the host's own allocatable share per slot, and it is chosen
// so that nothing changes on upgrade: a host carrying only pools with no
// limits charges each runner exactly one slot's worth, so its capacity is what
// admits work, exactly as it did before any of this existed.
//
// Disk has no fallback. Free disk is a measurement rather than a budget, and
// dividing what is left into shares would turn every fill of the cache into a
// refusal to place work that had always placed. What guards it instead is the
// reserve floor: a pool that asks for disk is charged it, and a host at or
// below its reserve takes nothing.
//
// A docker-in-docker runner is charged for both of its containers, whichever
// figure the charge came from. The fallback used to be the exception -- a
// share of a machine was held not to become two shares because of what ran
// inside it -- and the exception was wrong in the one way that matters: the
// backend gives the sidecar the same limits as the runner, fallback share
// included, so a defaulted pair was given two shares of the machine and
// charged one. A host then read as half committed while its runners' quotas
// added up to every core it had, the daemon lost the reserve that arithmetic
// said was being kept for it, and creates started timing out on a host the
// fleet believed was half idle. The pair is what the machine carries, so the
// pair is what it is charged.
func Reserve(p *store.Pool, h *store.Host) Reservation {
	alloc := h.Allocatable()
	res := Reservation{
		CPUs:     p.Resources.CPUs,
		MemoryMB: p.Resources.MemoryMB,
		DiskMB:   p.Resources.DiskGB * 1024,
	}
	cpuFromHost, memoryFromHost := res.CPUs <= 0, res.MemoryMB <= 0
	if cpuFromHost {
		res.CPUs = share(alloc.CPUs, h.Capacity)
	}
	if memoryFromHost {
		res.MemoryMB = int64(share(float64(alloc.MemoryMB), h.Capacity))
	}
	if p.DockerMode == store.DockerDinD {
		res.CPUs *= 2
		res.MemoryMB *= 2
		res.DiskMB *= 2
		// A doubled share is capped at the machine, and only a doubled share:
		// a pool's own figures are charged in full however large they are,
		// because a host too small for what an operator typed has to be able
		// to say so. Two shares are more than the machine on a host with one
		// slot alone, where capping is the difference between placing the one
		// pair it has room for and refusing the pool outright -- and a fleet
		// of single-slot hosts must not empty itself on upgrade. There the
		// pair is still given two shares between them, because halving a lone
		// slot's memory is how a job that used to pass gets OOM-killed; what
		// answers for it is the same pressure hold and throttle that answer
		// for every other machine running more than it was sized for.
		if cpuFromHost && alloc.CPUsKnown {
			res.CPUs = min(res.CPUs, alloc.CPUs)
		}
		if memoryFromHost && alloc.MemoryKnown {
			res.MemoryMB = min(res.MemoryMB, alloc.MemoryMB)
		}
	}
	return res
}

// share is one slot's worth of a host-wide figure.
func share(total float64, capacity int) float64 {
	if capacity < 1 || total <= 0 {
		return 0
	}
	return total / float64(capacity)
}

// Reserved is what the live runners already on h have promised away.
//
// It is the same sum newHostSet seeds a pass with, and it is exported because
// the Hosts page has to show it: an operator can see what a machine is and
// what it has left, and without this cannot see what the fleet has committed
// on it -- which is the number that explains why a host with free slots is
// taking nothing.
//
// Only live rows count, exactly as the slot count does: the snapshot keeps
// failed runners so the Runners page can show them, and charging a host for a
// runner that is gone would shrink the fleet every time one failed.
//
// Disk is deliberately absent. Free disk is a measurement of the filesystem as
// it is now, so what the runners already there have written is in the figure
// already; adding their reservations to it would charge the same bytes twice.
func Reserved(h *store.Host, pools []*store.Pool, runners map[string][]*store.Runner) Reservation {
	var out Reservation
	if h == nil {
		return out
	}
	for _, p := range pools {
		if p == nil {
			continue
		}
		res := Reserve(p, h)
		for _, r := range runners[p.ID] {
			if r == nil || r.HostID != h.ID || !r.State.Live() {
				continue
			}
			out.CPUs += res.CPUs
			out.MemoryMB += res.MemoryMB
		}
	}
	return out
}

// HostFits reports whether one runner of the pool would fit on an empty host:
// whether this is the size of machine the pool could ever run on, before
// anything running on it is counted.
//
// Room left on the host is separate, and is the scheduler's own accounting
// during a pass -- the same split HostCanRun already makes for slots.
func HostFits(h *store.Host, p *store.Pool) bool {
	alloc := h.Allocatable()
	whole := Reservation{CPUs: alloc.CPUs, MemoryMB: alloc.MemoryMB, DiskMB: alloc.DiskMB}
	return fits(whole, Reserve(p, h), alloc)
}

// fits is the one comparison, so that the empty-host question and the
// room-left-during-a-pass question can never drift apart. A figure the agent
// never measured constrains nothing: an agent that predates the fields places
// by slots alone, which is what keeps an upgrade from emptying a fleet.
func fits(left, want Reservation, known store.HostAllocation) bool {
	if known.DiskKnown {
		// A host at or below its disk reserve takes no runner at all, however
		// little the pool asks for. The reserve is the room the machine needs
		// to do the work -- an image layer, a checkout, a log -- and not a
		// budget to be spent down to nothing.
		if known.DiskMB <= 0 || left.DiskMB < want.DiskMB {
			return false
		}
	}
	if known.CPUsKnown && left.CPUs+cpuEpsilon < want.CPUs {
		return false
	}
	if known.MemoryKnown && left.MemoryMB < want.MemoryMB {
		return false
	}
	return true
}

// HostShortfall says why one runner of p could not fit on an empty h, in terms
// an operator can act on: what the machine has to place on, and what this pool
// is charged for a runner. It is empty when the runner would fit, so it is
// also the sentence for "why is this host not in the count".
//
// The charge is the number worth spelling out, because it is the one nothing
// on the pool shows. A pool that asks for 8 CPU and gives its jobs Docker in
// Docker is charged 16 on every host -- the sidecar gets the same limits -- so
// a 12-CPU machine refuses it, and an operator reading the pool's own "8 CPU"
// against a "12 vCPU" host card has no way to see why.
func HostShortfall(h *store.Host, p *store.Pool) string {
	alloc := h.Allocatable()
	left := Reservation{CPUs: alloc.CPUs, MemoryMB: alloc.MemoryMB, DiskMB: alloc.DiskMB}
	want := Reserve(p, h)
	if fits(left, want, alloc) {
		return ""
	}
	dind := p.DockerMode == store.DockerDinD
	switch {
	case alloc.DiskKnown && alloc.DiskMB <= 0:
		// The reserve floor, not a limit anyone typed. Sending an operator to
		// lower the pool's disk request would be sending them to the wrong
		// screen: this host refuses every pool until space is freed on it.
		return "its work directory is at or below its disk reserve, so it takes no runner of any pool"
	case alloc.DiskKnown && left.DiskMB < want.DiskMB:
		return fmt.Sprintf("it has %s of disk free to place on, and one runner of this pool is charged %s%s",
			formatMB(left.DiskMB), formatMB(want.DiskMB), charged(p.Resources.DiskGB > 0, dind))
	case alloc.CPUsKnown && left.CPUs+cpuEpsilon < want.CPUs:
		return fmt.Sprintf("it has %s CPU to place on, and one runner of this pool is charged %s%s",
			formatCPUs(left.CPUs), formatCPUs(want.CPUs), charged(p.Resources.CPUs > 0, dind))
	case alloc.MemoryKnown && left.MemoryMB < want.MemoryMB:
		return fmt.Sprintf("it has %s of memory to place on, and one runner of this pool is charged %s%s",
			formatMB(left.MemoryMB), formatMB(want.MemoryMB), charged(p.Resources.MemoryMB > 0, dind))
	}
	return "it is too small for this pool's limits"
}

// charged explains the one case where the number an operator sees on the pool
// is not the number the host is charged. A defaulted field needs no such
// sentence even though it doubles too: its doubled share is capped at the
// machine it is being compared against, so it can never be what a host is too
// small for.
func charged(set, dind bool) string {
	if set && dind {
		return ", twice what it asks for, because a docker-in-docker runner is charged for its sidecar too"
	}
	return ""
}

// FormatCPUs is formatCPUs for the problems that quote a host's share, so the
// figure an operator reads there is rendered by the one function that renders
// it everywhere else.
func FormatCPUs(v float64) string { return formatCPUs(v) }

// formatCPUs writes a CPU count the way the pool form takes one: whole where
// it is whole, and a share where the host's capacity did not divide evenly.
// Two decimals is where a share stops being worth reading -- the rounding this
// absorbs is the same one cpuEpsilon exists for.
func formatCPUs(v float64) string {
	s := strings.TrimRight(strings.TrimRight(strconv.FormatFloat(v, 'f', 2, 64), "0"), ".")
	if s == "" || s == "-" {
		return "0"
	}
	return s
}

// formatMB writes a size in the unit an operator would say it in.
func formatMB(mb int64) string {
	if mb >= 1024 && mb%1024 == 0 {
		return strconv.FormatInt(mb/1024, 10) + " GB"
	}
	if mb >= 1024 {
		return strconv.FormatFloat(float64(mb)/1024, 'f', 1, 64) + " GB"
	}
	return strconv.FormatInt(mb, 10) + " MB"
}

// HostRoom is how many runners of p an empty h could hold, and which of the
// machine's figures ran out first.
//
// It is the arithmetic behind "this host has room for four of these", and it
// is the scheduler's rather than the browser's for the same reason HostFits
// is: the charge a runner carries is not the figure on the pool -- a
// docker-in-docker slot is charged for its sidecar too, and an unmeasured
// field constrains nothing -- and a count worked out from the pool's own
// numbers would disagree with where the fleet actually places runners.
//
// The slot count is part of the answer, not a separate one: a host set to six
// slots holds six runners however much machine is left over, and a host whose
// slots outrun its machine is the overload this function exists to show. Both
// are returned, so a caller can say which is binding.
type HostRoom struct {
	// Slots is what the operator set, less any active throttle: the ceiling
	// the scheduler would honour whatever the machine could take.
	Slots int
	// Fits is how many runners of this size the machine itself has room for,
	// ignoring the slot count. It is zero on a host too small for one.
	Fits int
	// Room is the smaller of the two, which is what the pool can actually
	// expect from this host.
	Room int
	// LimitedBy names what ran out: "slots", "cpu", "memory", "disk", or ""
	// where the host has measured nothing and only its slots bind.
	LimitedBy string
}

// HostRoomFor counts the room on an empty host. Live runners are deliberately
// not subtracted: this answers "how big is this machine, in runners of this
// pool", which is the question a pool's size and a host's capacity are chosen
// against, and it does not change every time a job starts.
func HostRoomFor(h *store.Host, p *store.Pool) HostRoom {
	out := HostRoom{Slots: h.EffectiveCapacity()}
	alloc := h.Allocatable()
	want := Reserve(p, h)

	fits := -1
	limit := ""
	consider := func(n int, by string) {
		if fits == -1 || n < fits {
			fits, limit = n, by
		}
	}
	if alloc.CPUsKnown && want.CPUs > 0 {
		consider(int((alloc.CPUs+cpuEpsilon)/want.CPUs), "cpu")
	}
	if alloc.MemoryKnown && want.MemoryMB > 0 {
		consider(int(alloc.MemoryMB/want.MemoryMB), "memory")
	}
	// Disk only where the pool asks for it. Free disk is a measurement rather
	// than a budget -- see Reserve -- so dividing what is left into runners
	// would put a machine's cache on the pool's account.
	if alloc.DiskKnown && want.DiskMB > 0 {
		consider(int(alloc.DiskMB/want.DiskMB), "disk")
	}
	if fits == -1 {
		// Nothing measured: the host places by slots alone, exactly as it did
		// before any of this existed.
		out.Fits, out.Room, out.LimitedBy = out.Slots, out.Slots, ""
		return out
	}
	out.Fits = max(fits, 0)
	out.Room = min(out.Fits, out.Slots)
	out.LimitedBy = limit
	if out.Slots < out.Fits {
		out.LimitedBy = "slots"
	}
	return out
}
