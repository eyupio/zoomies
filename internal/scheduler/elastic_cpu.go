package scheduler

import (
	"math"
	"slices"
)

// ElasticCPUWorkload is one live runner in the host CPU ledger. Every live
// workload belongs in the input, including ones that may not burst: their base
// is already promised and therefore is not spare CPU.
type ElasticCPUWorkload struct {
	ID        string
	BaseCPUs  float64
	MaxCPUs   float64
	Demanding bool
}

// ElasticCPUPlan lends a host's genuinely unpromised CPU to demanding
// workloads with max-min fairness.
//
// total is the host's allocatable CPU after its operator reserve. startReserve
// protects an imminent runner when compatible work is queued. A workload with
// MaxCPUs at or below BaseCPUs stays at its guarantee. Returned targets always
// include every workload, so an agent can restore a runner whose demand ended.
func ElasticCPUPlan(total, startReserve float64, workloads []ElasticCPUWorkload) map[string]float64 {
	targets := make(map[string]float64, len(workloads))
	ordered := slices.Clone(workloads)
	slices.SortFunc(ordered, func(a, b ElasticCPUWorkload) int {
		if a.ID < b.ID {
			return -1
		}
		if a.ID > b.ID {
			return 1
		}
		return 0
	})

	committed := 0.0
	var active []ElasticCPUWorkload
	for _, w := range ordered {
		base := max(w.BaseCPUs, 0)
		targets[w.ID] = base
		committed += base
		if w.Demanding && w.MaxCPUs > base+cpuEpsilon {
			active = append(active, w)
		}
	}
	spare := max(total-max(startReserve, 0)-committed, 0)

	// Water-fill rather than divide once: a runner with a low ceiling gives
	// what it cannot use back to the remaining runners instead of stranding it.
	for spare > cpuEpsilon && len(active) > 0 {
		share := spare / float64(len(active))
		next := active[:0]
		used := 0.0
		for _, w := range active {
			room := w.MaxCPUs - targets[w.ID]
			give := min(share, room)
			if give > 0 {
				targets[w.ID] += give
				used += give
			}
			if room-give > cpuEpsilon {
				next = append(next, w)
			}
		}
		if used <= cpuEpsilon {
			break
		}
		spare -= used
		active = next
	}

	// The daemon works in hundredths of a core. Floor each target so rounding
	// can never promise more CPU than total, even across many runners.
	for id, target := range targets {
		targets[id] = math.Floor((target+cpuEpsilon)*100) / 100
	}
	return targets
}
