package scheduler

import (
	"math"
	"time"

	"github.com/eyupio/zoomies/internal/store"
)

// HostAdmissionReason is empty when measured pressure permits new runners.
// Connectivity, configured capacity and manual cordons remain separate facts.
func HostAdmissionReason(h *store.Host, now time.Time) string {
	if !h.Usage.Fresh(now) {
		return ""
	}
	if h.Usage.CPUHeld {
		return "new starts held after sustained CPU pressure; they resume when CPU usage falls below 85%"
	}
	if v := h.Usage.MemoryAvailableMB; v != nil && *v <= h.MemoryReserve() {
		return "new starts held because available memory is at or below the host reserve"
	}
	return ""
}

func hostUnderCPUPressure(h *store.Host, now time.Time) bool {
	// Lent CPU in use is load the elastic plan chose, and the next plan
	// takes it back for a start, so it is not pressure.
	v := h.Usage.UnlentCPUPercent()
	return h.Usage.Fresh(now) && v != nil && *v >= 85
}

func (hs *hostSet) score(h *store.Host, p *store.Pool) float64 {
	if h.Slots() <= 0 {
		return -1
	}
	want, left, alloc := Reserve(p, h), hs.left[h.ID], hs.alloc[h.ID]
	score, dimensions := 0.0, 0.0
	if alloc.CPUsKnown && alloc.CPUs > 0 {
		cpu := left.CPUs
		if measured, ok := hs.observedCPU[h.ID]; ok {
			cpu = min(cpu, measured)
		}
		score += max(cpu-want.CPUs, 0) / alloc.CPUs
		dimensions++
	}
	if alloc.MemoryKnown && alloc.MemoryMB > 0 {
		score += float64(max(left.MemoryMB-want.MemoryMB, 0)) / float64(alloc.MemoryMB)
		dimensions++
	}
	if dimensions > 0 {
		return score / dimensions
	}
	// Preserve the old greatest-free-slots choice when there are no resource
	// specifications. Missing telemetry must not make an existing fleet stop.
	return float64(hs.free[h.ID]-1) / float64(h.Slots())
}

func (hs *hostSet) prefer(h, best *store.Host, p *store.Pool) bool {
	if hs.preferReadiness {
		a, b := hs.readyDelay(h, p), hs.readyDelay(best, p)
		// A small timing difference is noise; headroom breaks near ties.
		if a+5*time.Second < b {
			return true
		}
		if b+5*time.Second < a {
			return false
		}
	}
	// Spec priority decides first: it is what ranks a bigger, faster machine
	// ahead of a smaller, slower one, whichever placement order is in force.
	// Hosts that score the same fall through to the order, and hosts that
	// measure nothing all score 0, which is what keeps an old fleet placed
	// exactly as before the score existed.
	if a, b := HostPriority(h), HostPriority(best); a.Score != b.Score {
		return a.Score > b.Score
	}
	a, b := hs.alloc[h.ID], hs.alloc[best.ID]
	measured := (a.CPUsKnown || a.MemoryKnown) && (b.CPUsKnown || b.MemoryKnown)
	switch hs.order {
	case HostOrderLargestStandard:
		if c := compareStandard(Reserve(p, h), Reserve(p, best)); c != 0 {
			return c > 0
		}
	case HostOrderBestFit:
		return hs.tighter(h, best, p, measured)
	}
	if !a.CPUsKnown && !a.MemoryKnown && !b.CPUsKnown && !b.MemoryKnown {
		return hs.free[h.ID] > hs.free[best.ID]
	}
	score, bestScore := hs.score(h, p), hs.score(best, p)
	return score > bestScore+cpuEpsilon ||
		(math.Abs(score-bestScore) <= cpuEpsilon && hs.free[h.ID] > hs.free[best.ID])
}

// A serial admission queue costs one estimated service interval per start.
// Unknown hosts use a bounded prior and remain eligible; metrics never relax fit.
func (hs *hostSet) readyDelay(h *store.Host, p *store.Pool) time.Duration {
	d := hs.readiness[p.ID][h.ID]
	if d <= 0 {
		d = time.Minute
	}
	d = min(max(d, 5*time.Second), 10*time.Minute)
	warming := hs.warming[h.ID]
	// The agent starts DinD creates and everything else on separate lanes, so
	// a pool that starts no daemon of its own is not behind a warming one.
	// Counting the whole host made a host look slow to every pool whenever its
	// DinD lane was busy, and could place a runner elsewhere for no reason.
	if p.DockerMode == store.DockerDinD {
		warming = hs.warmingDinD[h.ID]
	} else {
		warming -= hs.warmingDinD[h.ID]
	}
	return time.Duration(warming+1) * d
}
