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
	return h.Usage.Fresh(now) && h.Usage.CPUPercent != nil && *h.Usage.CPUPercent >= 85
}

func (hs *hostSet) pending(h *store.Host, pools []*store.Pool, runners map[string][]*store.Runner) Reservation {
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
			}
			if starting || r.CreatedAt.After(h.Usage.SampledAt) ||
				(r.ContainerStartedAt != nil && r.ContainerStartedAt.After(h.Usage.SampledAt)) {
				res := Reserve(p, h)
				out.CPUs += res.CPUs
				out.MemoryMB += res.MemoryMB
			}
		}
	}
	return out
}

func (hs *hostSet) score(h *store.Host, p *store.Pool) float64 {
	if h.Capacity <= 0 {
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
	return float64(hs.free[h.ID]-1) / float64(h.Capacity)
}

func (hs *hostSet) prefer(h, best *store.Host, p *store.Pool) bool {
	a, b := hs.alloc[h.ID], hs.alloc[best.ID]
	if !a.CPUsKnown && !a.MemoryKnown && !b.CPUsKnown && !b.MemoryKnown {
		return hs.free[h.ID] > hs.free[best.ID]
	}
	score, bestScore := hs.score(h, p), hs.score(best, p)
	return score > bestScore+cpuEpsilon ||
		(math.Abs(score-bestScore) <= cpuEpsilon && hs.free[h.ID] > hs.free[best.ID])
}
