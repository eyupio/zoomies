package scheduler

import (
	"fmt"
	"math"

	"github.com/eyupio/zoomies/internal/store"
)

// What to suggest for a host's capacity and reserve, from what the machine is
// and what a runner in this fleet asks for. This is the Adjust dialog's
// "Set to recommendations" worked out on the controller, so the controller can
// keep a host there by itself. web/src/lib/hosts/recommend.ts says the same
// thing for the first paint, and the two are held to the same cases by a test
// on each side; a change to one is a change to both.
//
// It is pure: it reads the host as it stands and the pools it is handed.

// RunnerDiskMB is the working room a runner is assumed to need on the disk,
// beside the reserve. Checkouts, caches and the images a job pulls land on the
// work filesystem, and a host that offers more runners than the disk holds
// fills it and fails jobs part-way through, which is the failure this exists to
// stop. It is deliberately generous and is not a limit on any single runner.
// Placement charges it too (Reserve) where a pool types no resources.disk_gb,
// so the capacity the recommender suggests is the capacity a pass will fill.
const RunnerDiskMB int64 = 10 * 1024

// RunnerAsk is what one runner asks of a machine: the largest ask across the
// enabled pools, or the fleet's default where none has said.
type RunnerAsk struct {
	CPUs     float64 `json:"cpus"`
	MemoryMB int64   `json:"memory_mb"`
	// Pool names the pool whose figures won, empty for the default.
	Pool string `json:"pool,omitempty"`
	// Pair is whether the ask was doubled because a slot is a pair.
	Pair bool `json:"pair,omitempty"`
}

// DefaultRunnerAsk is the ask where neither a pool nor the fleet has said.
var DefaultRunnerAsk = RunnerAsk{CPUs: 2, MemoryMB: 4096}

// AskOf is the largest enabled pool's ask. fleetCPUs and fleetMemoryMB are the
// fleet's own defaults (runners.default_cpus, runners.default_memory_mb), used
// for a figure no pool sets.
func AskOf(pools []*store.Pool, fleetCPUs float64, fleetMemoryMB int64) RunnerAsk {
	fallbackCPUs, fallbackMemory := DefaultRunnerAsk.CPUs, DefaultRunnerAsk.MemoryMB
	if fleetCPUs > 0 {
		fallbackCPUs = fleetCPUs
	}
	if fleetMemoryMB > 0 {
		fallbackMemory = fleetMemoryMB
	}
	var cpus float64
	var memory int64
	var winner string
	var cpuPair, memoryPair, anyPair bool
	for _, p := range pools {
		if p == nil || !p.Enabled {
			continue
		}
		// A docker-in-docker slot is two containers where the pool typed its
		// own limits; a pool that leaves its size to the host puts the pair in
		// one slot between them and asks for no more than anybody else.
		typed := p.Resources.CPUs > 0 || p.Resources.MemoryMB > 0
		containers := 1
		if p.DockerMode == store.DockerDinD && typed {
			containers = 2
		}
		anyPair = anyPair || containers > 1
		wantCPUs := p.Resources.CPUs * float64(containers)
		wantMemory := p.Resources.MemoryMB * int64(containers)
		if wantCPUs > cpus {
			cpus, cpuPair, winner = wantCPUs, containers > 1, p.Name
		}
		if wantMemory > memory {
			memory, memoryPair = wantMemory, containers > 1
			if winner == "" {
				winner = p.Name
			}
		}
	}
	if cpus <= 0 && memory <= 0 {
		if !anyPair {
			return RunnerAsk{CPUs: fallbackCPUs, MemoryMB: fallbackMemory}
		}
		return RunnerAsk{CPUs: fallbackCPUs * 2, MemoryMB: fallbackMemory * 2, Pair: true}
	}
	ask := RunnerAsk{CPUs: fallbackCPUs, MemoryMB: fallbackMemory, Pool: winner, Pair: cpuPair || memoryPair}
	if cpus > 0 {
		ask.CPUs = cpus
	}
	if memory > 0 {
		ask.MemoryMB = memory
	}
	return ask
}

// ReserveCPUsFor is the cores to hold back for the machine itself: one on
// anything up to sixteen, then one more for every sixteen. Never the whole
// machine.
func ReserveCPUsFor(cpus int) int {
	if cpus <= 1 {
		return 0
	}
	return min(cpus-1, max(1, (cpus+15)/16))
}

// ReserveMemoryMBFor is a tenth of the machine, between one and eight
// gigabytes, on the nearest notch of the slider, so the recommendation is
// somewhere the slider can stop.
func ReserveMemoryMBFor(memoryMB int64) int64 {
	if memoryMB <= 1024 {
		return 0
	}
	tenth := math.Min(float64(memoryMB-512), math.Max(1024, math.Min(8192, float64(memoryMB)*0.1)))
	return nearest(memoryNotches(memoryMB), tenth)
}

// ReserveDiskMBFor is a tenth of the filesystem, between the scheduler's own
// two-gigabyte floor and sixty-four gigabytes, on the nearest notch. Runner
// checkouts and caches land here, so the reserve is what stops a full disk
// turning into a job that fails part-way through.
func ReserveDiskMBFor(diskTotalMB int64) int64 {
	if diskTotalMB <= 4096 {
		return 0
	}
	tenth := math.Min(float64(diskTotalMB-2048), math.Max(2048, math.Min(65536, float64(diskTotalMB)*0.1)))
	return nearest(diskNotches(diskTotalMB), tenth)
}

// nearest is the notch closest to want, the later one on a tie, as the slider
// picks it.
func nearest(notches []int64, want float64) int64 {
	var best int64
	for _, n := range notches {
		if math.Abs(float64(n)-want) <= math.Abs(float64(best)-want) {
			best = n
		}
	}
	return best
}

func memoryNotches(memoryMB int64) []int64 {
	if memoryMB <= 512 {
		return []int64{0}
	}
	top := memoryMB - 512
	out := []int64{0}
	for _, s := range []int64{512, 1024, 2048, 3072, 4096, 6144, 8192, 12288, 16384, 24576, 32768} {
		if s <= top {
			out = append(out, s)
		}
	}
	for next := int64(49152); next <= top; next += 16384 {
		out = append(out, next)
	}
	return out
}

func diskNotches(diskTotalMB int64) []int64 {
	if diskTotalMB <= 4096 {
		return []int64{0}
	}
	top := diskTotalMB - 2048
	out := []int64{0}
	for step := int64(2048); step <= top; step *= 2 {
		out = append(out, step)
	}
	return out
}

// HostRecommendation is the capacity and reserve to keep a host at, and what
// bound the capacity, in the words the Hosts page and the audit row use.
type HostRecommendation struct {
	Capacity        int
	ReserveCPUs     int
	ReserveMemoryMB int64
	ReserveDiskMB   int64
	// LimitedBy is "cpu", "memory" or "disk": the figure that set Capacity.
	LimitedBy string
	Ask       RunnerAsk
}

// Reason is the recommendation as one sentence.
func (r HostRecommendation) Reason() string {
	from := "the fleet's default runner size"
	if r.Ask.Pool != "" {
		from = fmt.Sprintf("the runner size of pool %s", r.Ask.Pool)
	}
	return fmt.Sprintf("%d %s fit after the reserve (%s, %s memory, %s of disk): a runner asks for %s CPU and %s memory (%s), and %s is the tightest",
		r.Capacity, plural(r.Capacity, "runner"), plural(r.ReserveCPUs, "CPU"), formatMB(r.ReserveMemoryMB), formatMB(r.ReserveDiskMB),
		formatCPUs(r.Ask.CPUs), formatMB(r.Ask.MemoryMB), from, r.LimitedBy)
}

// RecommendHost works out the capacity and reserve for a host. It reports false
// for a host that has not said how big it is: a reserve held back from a figure
// nobody has measured is a number the scheduler ignores, and a recommendation
// made up from nothing is worse than none.
func RecommendHost(h *store.Host, ask RunnerAsk) (HostRecommendation, bool) {
	if h.CPUs <= 0 && h.MemoryMB <= 0 {
		return HostRecommendation{}, false
	}
	rec := HostRecommendation{
		ReserveCPUs:     ReserveCPUsFor(h.CPUs),
		ReserveMemoryMB: ReserveMemoryMBFor(h.MemoryMB),
		ReserveDiskMB:   ReserveDiskMBFor(h.DiskTotalMB),
		Ask:             ask,
	}
	if h.CPUs <= 0 {
		rec.ReserveCPUs = 0
	}
	fit := math.MaxInt32
	if h.CPUs > 0 && ask.CPUs > 0 {
		if v := int(math.Floor((float64(h.CPUs-rec.ReserveCPUs))/ask.CPUs + 1e-9)); v < fit {
			fit, rec.LimitedBy = v, "cpu"
		}
	}
	if h.MemoryMB > 0 && ask.MemoryMB > 0 {
		if v := int((h.MemoryMB - rec.ReserveMemoryMB) / ask.MemoryMB); v < fit {
			fit, rec.LimitedBy = v, "memory"
		}
	}
	// The disk is a budget as well as a reserve. The Adjust dialog's own
	// figure follows cores and memory alone, and a host built that way on a
	// small disk is the one that fills it.
	if h.DiskTotalMB > 0 {
		if v := int((h.DiskTotalMB - rec.ReserveDiskMB) / RunnerDiskMB); v < fit {
			fit, rec.LimitedBy = v, "disk"
		}
	}
	if fit == math.MaxInt32 {
		return HostRecommendation{}, false
	}
	rec.Capacity = max(1, fit)
	return rec, true
}
