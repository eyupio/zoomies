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
	// Weight is the workload's share of the spare CPU relative to the other
	// demanding workloads, so a job known to scale is given more of what is not
	// enough for everyone. Zero, the value of a workload nothing is known about,
	// weighs one, which shares equally exactly as a plan without weights does.
	Weight float64
	// Idle is a live runner waiting for a job. Idle runners are charged
	// together only the largest single guarantee among them, not the sum: at
	// most one of them can be handed the next job before the following plan,
	// and CPU they are not using is the whole point of lending.
	Idle bool
}

// ElasticCPUPlan lends a host's genuinely unpromised CPU to demanding
// workloads with weighted max-min fairness: equal shares unless a workload
// carries a Weight.
//
// total is the host's allocatable CPU after its operator reserve. startReserve
// protects an imminent runner when compatible work is queued. A workload with
// MaxCPUs at or below BaseCPUs stays at its guarantee. Returned targets always
// include every workload, so an agent can restore a runner whose demand ended.
//
// Idle workloads are charged collectively as a start reserve -- the largest
// idle guarantee -- rather than each in full, or a host holding warm runners
// would never have anything to lend. The oversubscription that allows is
// bounded: when a second idle runner turns busy it is charged in full on the
// very next plan, and the plan is complete, so the lent CPU comes back then.
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

	committed, idleReserve := 0.0, 0.0
	var active []ElasticCPUWorkload
	for _, w := range ordered {
		base := max(w.BaseCPUs, 0)
		targets[w.ID] = base
		if w.Idle {
			idleReserve = max(idleReserve, base)
			continue
		}
		committed += base
		if w.Demanding && w.MaxCPUs > base+cpuEpsilon {
			active = append(active, w)
		}
	}
	spare := max(total-max(startReserve, 0)-idleReserve-committed, 0)

	// Water-fill rather than divide once: a runner with a low ceiling gives
	// what it cannot use back to the remaining runners instead of stranding it.
	// Each round divides what is left in proportion to the weights, so a runner
	// whose share is more than its room is filled and the rest is divided again.
	for spare > cpuEpsilon && len(active) > 0 {
		weights := 0.0
		for _, w := range active {
			weights += weightOf(w)
		}
		next := active[:0]
		used := 0.0
		for _, w := range active {
			room := w.MaxCPUs - targets[w.ID]
			give := min(spare*weightOf(w)/weights, room)
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

// weightOf is a workload's weight, one when it has none.
func weightOf(w ElasticCPUWorkload) float64 {
	if w.Weight > 0 {
		return w.Weight
	}
	return 1
}
