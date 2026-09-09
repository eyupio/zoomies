package scheduler

import (
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
func Reserve(p *store.Pool, h *store.Host) Reservation {
	alloc := h.Allocatable()
	res := Reservation{
		CPUs:     p.Resources.CPUs,
		MemoryMB: p.Resources.MemoryMB,
		DiskMB:   p.Resources.DiskGB * 1024,
	}
	// A docker-in-docker pool runs the build inside a sidecar that the backend
	// gives the same limits as the runner, so the pool's footprint on the host
	// is twice what it asked for. Only what it asked for doubles: the fallback
	// is a share of the host derived from the slot count, and a share of a
	// machine does not become two shares because of what runs inside it.
	if p.DockerMode == store.DockerDinD {
		res.CPUs *= 2
		res.MemoryMB *= 2
		res.DiskMB *= 2
	}
	if res.CPUs <= 0 {
		res.CPUs = share(alloc.CPUs, h.Capacity)
	}
	if res.MemoryMB <= 0 {
		res.MemoryMB = int64(share(float64(alloc.MemoryMB), h.Capacity))
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
