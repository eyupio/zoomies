package scheduler

import (
	"math"
	"slices"

	"github.com/eyupio/zoomies/internal/store"
)

// What a job's recent runs say about how much CPU it can use, so elastic CPU can
// stop lending to a job that has never wanted more than it has.
//
// Elastic CPU lends spare cores to a runner using at least 80% of its share. For
// a job that never scales past two cores on a runner lent eight, that is cores
// held in a loan nobody uses, and a reclaim cycle for every one of them. History
// can say so, because the runner's cgroup counts the periods in which its quota
// held it back: a run that was held back in a tenth of its periods wanted more
// than it had, and one that never was did not, whatever its peak reads. Peak CPU
// alone cannot say that. It is capped by the quota the run had, so a job held to
// two cores and a job that needed exactly two both peak at two.
//
// It is pure: the controller hands in the runs of one job.

const (
	// CPUBoundShare is the share of its CPU periods a run was held back in, at
	// or above which it counts as CPU-bound: it wanted more than it had.
	CPUBoundShare = 0.10
	// CPUProfileMinRuns is how many measured runs the profile needs before it
	// says anything.
	CPUProfileMinRuns = 5
	// CPUProfileMinPeriods is how many enforcement periods a run needs to count as
	// measured, so a run that was sampled for a few seconds does not vote. A period
	// is a tenth of a second, so this is half a minute.
	CPUProfileMinPeriods = 300
	// CPUBoundRunShare is the share of measured runs that were CPU-bound at or
	// above which the job is taken to scale: one in four. A job that was held back
	// that often has something to gain from a loan, and a ceiling would cost it.
	CPUBoundRunShare = 0.25
	// CPUCeilingMargin is what the heaviest ordinary run's peak is multiplied by to
	// give the ceiling, so a job that peaked at two cores may be lent to two and a
	// half. It is not a limit on the job's real need: a job held to its ceiling that
	// wanted more is held back in its periods, so its next profile says it scales
	// and the ceiling lifts.
	CPUCeilingMargin = 1.25
	cpuCeilingStep   = 0.25
)

// Verdicts of a CPUDemand.
const (
	// CPUDemandUnknown says there are too few measured runs to say. Nothing about
	// lending changes for the job.
	CPUDemandUnknown = "unknown"
	// CPUDemandScales says the job was held back by its quota often enough that it
	// has something to gain from a loan. Nothing about lending changes for it.
	CPUDemandScales = "scales"
	// CPUDemandCapped says the job was not CPU-bound in enough of its runs to be
	// taken to scale: it has a Ceiling, and a loan above it is cores wasted.
	CPUDemandCapped = "capped"
)

// CPUDemand is what a job's runs say about its CPU.
type CPUDemand struct {
	Verdict string `json:"verdict"`
	// Ceiling is the most CPU a runner of this job should be lent up to, in cores,
	// and zero when the verdict is not capped.
	Ceiling float64 `json:"ceiling,omitempty"`
	// Runs is how many measured runs the profile was taken from and Bound how many
	// of those were CPU-bound.
	Runs  int `json:"runs"`
	Bound int `json:"bound"`
}

// ProfileCPU works out a job's CPU demand from its recent runs.
func ProfileCPU(peaks []store.JobPeak) CPUDemand {
	var out CPUDemand
	var ordinary []float64
	for _, p := range peaks {
		// An unsampled run says nothing, and neither does one that was killed for
		// memory: it was cut short, so its CPU is not what the job uses.
		if p.Periods < CPUProfileMinPeriods || p.OOMKilled {
			continue
		}
		out.Runs++
		if float64(p.ThrottledPeriods)/float64(p.Periods) >= CPUBoundShare {
			out.Bound++
			continue
		}
		if p.CPUs > 0 {
			ordinary = append(ordinary, p.CPUs)
		}
	}
	switch {
	case out.Runs < CPUProfileMinRuns:
		out.Verdict = CPUDemandUnknown
	case float64(out.Bound)/float64(out.Runs) >= CPUBoundRunShare || len(ordinary) == 0:
		out.Verdict = CPUDemandScales
	default:
		slices.Sort(ordinary)
		peak := ordinary[percentileIndex(len(ordinary))]
		out.Verdict = CPUDemandCapped
		out.Ceiling = math.Ceil(peak*CPUCeilingMargin/cpuCeilingStep-1e-9) * cpuCeilingStep
	}
	return out
}
