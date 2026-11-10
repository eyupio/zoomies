package scheduler

import (
	"testing"

	"github.com/eyupio/zoomies/internal/store"
)

// cpuRun is one measured run: its peak CPU, and how many of its 1000 periods its
// quota held it back in.
func cpuRun(peak float64, throttled int64) store.JobPeak {
	return store.JobPeak{CPUs: peak, Periods: 1000, ThrottledPeriods: throttled}
}

func cpuRuns(n int, peak float64, throttled int64) []store.JobPeak {
	var out []store.JobPeak
	for range n {
		out = append(out, cpuRun(peak, throttled))
	}
	return out
}

func TestProfileCPUCapsAJobThatWasNeverHeldBackAtWhatItUsed(t *testing.T) {
	d := ProfileCPU(cpuRuns(8, 2.1, 5)) // peaks at 2.1 cores, held back in half a percent of its periods
	if d.Verdict != CPUDemandCapped || d.Runs != 8 || d.Bound != 0 {
		t.Fatalf("demand = %+v, want capped from eight unbound runs", d)
	}
	// 2.1 with a quarter more is 2.625, up to the next quarter of a core.
	if d.Ceiling != 2.75 {
		t.Fatalf("ceiling = %v, want 2.75", d.Ceiling)
	}
}

// The point of the throttle counters. A job held to two cores and a job that
// needed two both peak at two; only one of them was held back for it.
func TestProfileCPUDoesNotCapAJobThatWantedMoreThanItHad(t *testing.T) {
	d := ProfileCPU(cpuRuns(8, 2.0, 400)) // peaks at its quota, held back in 40% of its periods
	if d.Verdict != CPUDemandScales || d.Ceiling != 0 || d.Bound != 8 {
		t.Fatalf("demand = %+v, want a job that scales, with no ceiling", d)
	}
}

// One run in four that was held back is enough to say the job has something to
// gain: a ceiling would cost it that run.
func TestProfileCPUTakesAJobThatWasHeldBackOftenEnoughToScale(t *testing.T) {
	peaks := append(cpuRuns(6, 2.0, 5), cpuRuns(2, 2.0, 300)...) // two of eight, a quarter
	if d := ProfileCPU(peaks); d.Verdict != CPUDemandScales {
		t.Fatalf("demand = %+v, want scales at one run in four", d)
	}
	peaks = append(cpuRuns(7, 2.0, 5), cpuRun(2.0, 300)) // one of eight
	if d := ProfileCPU(peaks); d.Verdict != CPUDemandCapped {
		t.Fatalf("demand = %+v, want capped at one run in eight", d)
	}
}

// A job's runs say nothing until there are enough measured ones, and a run that
// was sampled for a few seconds or killed for memory is not one.
func TestProfileCPUSaysNothingUntilEnoughRunsWereMeasured(t *testing.T) {
	few := cpuRuns(4, 2.0, 0)
	if d := ProfileCPU(few); d.Verdict != CPUDemandUnknown {
		t.Fatalf("four runs gave %+v, want unknown", d)
	}
	short := cpuRuns(8, 2.0, 0)
	for i := range short {
		short[i].Periods = 20 // two seconds
	}
	if d := ProfileCPU(short); d.Verdict != CPUDemandUnknown || d.Runs != 0 {
		t.Fatalf("unsampled runs gave %+v, want unknown from none", d)
	}
	killed := cpuRuns(8, 2.0, 0)
	for i := range killed {
		killed[i].OOMKilled = true
	}
	if d := ProfileCPU(killed); d.Verdict != CPUDemandUnknown {
		t.Fatalf("killed runs gave %+v, want unknown", d)
	}
}

// The ceiling is the ninetieth percentile of the runs that were not held back,
// so one run that spiked does not set it for every run after.
func TestProfileCPUCeilingIsTheNinetiethPercentileOfOrdinaryRuns(t *testing.T) {
	var peaks []store.JobPeak
	for i := range 10 {
		peaks = append(peaks, cpuRun(1.0+float64(i)*0.1, 0)) // 1.0 to 1.9
	}
	d := ProfileCPU(peaks)
	// The ninth of ten is 1.8; a quarter more is 2.25.
	if d.Verdict != CPUDemandCapped || d.Ceiling != 2.25 {
		t.Fatalf("demand = %+v, want capped at 2.25", d)
	}
}
