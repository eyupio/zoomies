package scheduler

import (
	"math"

	"github.com/eyupio/zoomies/internal/store"
)

// Allocation is what one runner of p is limited to when it is created on h,
// and where those limits came from.
//
// It is the enforcement half of the resource model, and it closes the gap the
// charge left open. Reserve charges a pool that sets no limit one slot's share
// of the host, so the books balance -- and until now the runner itself was
// given no limit at all, so the books balanced while eight runners each took
// every core on the machine. A pool's own limits win where it set them; where
// it did not, the runner is given exactly what it was charged, as a cgroup
// limit the container backends apply.
//
// It is per field, as Reserve is, and for the same reason: a pool that sets
// memory and leaves CPU alone has said something about memory and nothing
// about CPU. Disk and the pids limit pass through untouched -- disk has no
// share to give (it is a measurement, not a budget) and a pids limit is not a
// thing the machine's size says anything about.
//
// A docker-in-docker runner's sidecar receives the same limits the runner
// does, from the same spec, so a defaulted pair may burst to two shares
// between them. That is the one place the allocation is looser than the
// charge, and it is looser on purpose: the alternative is half a share each,
// which hobbles the build for the sake of a symmetry the charge does not
// keep either (a defaulted pair is charged one share, not two).
//
// A default is only given where it would bind. The process backend applies no
// limit at all, so a defaulted figure on one of its runners would be a number
// on the Runners page saying the opposite of the truth. And a daemon that
// cannot apply a CPU quota -- rootless Docker on a host that delegates only
// memory and pids to the user -- refuses the container rather than ignoring
// the request, so a default sent there would fail every create on the host.
// The host's own probe says what its daemon can do, and a probe from an agent
// too old to say is read as "nothing", which is what every host did before.
//
// With defaults off the pool's limits are the whole answer, which is what
// every fleet had before this existed.
func Allocation(p *store.Pool, h *store.Host, defaults bool) (store.Resources, string) {
	res := p.Resources
	source := ""
	if res.CPUs > 0 || res.MemoryMB > 0 {
		source = store.AllocationFromPool
	}
	if !defaults || h == nil || p.Backend == store.BackendProcess {
		return res, source
	}
	limits := hostLimits(h, p.Backend)
	alloc := h.Allocatable()
	def := HostShare(h)
	if res.CPUs <= 0 && alloc.CPUsKnown && limits.CPU && def.CPUs > 0 {
		res.CPUs = def.CPUs
		source = store.AllocationFromHost
	}
	if res.MemoryMB <= 0 && alloc.MemoryKnown && limits.Memory && def.MemoryMB > 0 {
		res.MemoryMB = def.MemoryMB
		source = store.AllocationFromHost
	}
	return res, source
}

// HostShare is one slot's share of a host: the CPU and memory a runner of a
// pool that sets neither is given by Allocation, before the host's daemon is
// asked whether it can apply them. Zero on a field the host has not measured.
//
// It is exported so that the problem which warns about an over-provisioned
// host can say what each runner's default share would be in the same figures
// Allocation hands out, rather than in a second sum that could drift from it.
func HostShare(h *store.Host) store.Resources {
	if h == nil {
		return store.Resources{}
	}
	alloc := h.Allocatable()
	var out store.Resources
	if alloc.CPUsKnown {
		out.CPUs = shareCPUs(alloc.CPUs, h.Capacity)
	}
	if alloc.MemoryKnown {
		out.MemoryMB = int64(share(float64(alloc.MemoryMB), h.Capacity))
	}
	return out
}

// hostLimits is what the host's daemon for this backend said it can enforce.
// Unknown is every field false, which defaults nothing.
func hostLimits(h *store.Host, kind store.BackendKind) store.LimitSupport {
	info, ok := h.BackendInfo.Find(kind)
	if !ok || !info.Limits.Known {
		return store.LimitSupport{}
	}
	return info.Limits
}

// shareCPUs is one slot's worth of CPU, floored to the hundredth the pool
// form takes limits in. Floored rather than rounded: three runners rounded
// up from 0.667 to 0.67 would together be promised a hundredth of a core the
// host does not have, and the daemon refuses a quota above the machine.
func shareCPUs(total float64, capacity int) float64 {
	return math.Floor(share(total, capacity)*100) / 100
}
