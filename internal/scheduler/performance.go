package scheduler

import (
	"fmt"
	"math"
	"slices"
	"strings"

	"github.com/eyupio/zoomies/internal/store"
)

// A recommendation for how big a runner should be, worked out from what the
// fleet measured of itself rather than from a rule of thumb.
//
// A host's slot count is its machine, less the reserve, divided by what one
// runner asks (RunnerAsk), and for a pool that leaves its size to the host that
// ask is the fleet's default, two cores and four gigabytes out of the box. The
// figure was chosen for nobody in particular, and a fleet whose runners use
// less than that waits for slots while its processors idle. This reads the
// evidence, says whether that is happening here, and if so what ask would fit
// the measured load with room to spare, and how many slots that gives each host.
//
// It is pure: the controller hands in what was measured and reads nothing here.
// It never recommends fewer slots than a host has now, and says nothing at all
// where the evidence is thin, because a recommendation made up from nothing is
// worse than none.

const (
	// PerformanceMinSamples is how many minutes with a runner at work, across the
	// fleet, the measurement needs before it is believed. An hour: a quiet
	// afternoon is not a fleet's load.
	PerformanceMinSamples = 60
	// PerformanceMinJobs is how many measured jobs the memory figure needs.
	PerformanceMinJobs = 20
	// PerformanceMinCeilingShare is the share of queue wait that has to have been
	// spent waiting for room before anything is recommended. Below it, jobs wait
	// for something else (GitHub, a runner starting, a throttle) and more slots
	// would not shorten the wait.
	PerformanceMinCeilingShare = 0.3
	// PerformanceCPUHeadroom is what a runner's measured use is multiplied by to
	// give its share. A runner that averages half a core is given three quarters
	// of one and may be lent more while the host has any idle: elastic CPU is what
	// pays for a build that wants every core.
	PerformanceCPUHeadroom = 1.5
	// performanceCPUStep and performanceMemoryStep round an ask up to a figure
	// that reads as a size.
	performanceCPUStep    = 0.25
	performanceMemoryStep = 256
)

// PerformanceHost is one machine as the recommendation sees it: the host as it
// stands, and what its runners were measured to use.
type PerformanceHost struct {
	Host *store.Host
	// Samples is how many sampled minutes had at least one runner at work.
	Samples int
	// CPUPerRunner is the cores one working runner used on average, from the
	// host's CPU use divided by its runners at work in each of those minutes.
	CPUPerRunner float64
}

// PerformanceEvidence is everything the recommendation reads.
type PerformanceEvidence struct {
	// Hosts are the machines that can run the pool: connected, not cordoned or
	// retired.
	Hosts []PerformanceHost
	// Ask is what a runner asks now, as AskOf reads it.
	Ask RunnerAsk
	// MinimumCPUs and MinimumMemoryMB are the least a runner will take
	// (runners.minimum_cpus, runners.minimum_memory_mb): the recommendation never
	// goes under them.
	MinimumCPUs     float64
	MinimumMemoryMB int64
	// WaitSeconds is the time jobs of the pool spent queued in the window and
	// CeilingSeconds the part of it spent with no room for another runner.
	WaitSeconds    float64
	CeilingSeconds float64
	// PoolMax is the pool's maximum runners now; 0 for a pool not yet made.
	PoolMax int
	// PeakMemoryMB is the most memory each recently completed job of the pool
	// used, and OOMKilled how many of them the kernel killed for memory.
	PeakMemoryMB []int64
	OOMKilled    int
	// Followers names the other enabled pools that follow the fleet's default
	// size, so a change to it is not a surprise to them.
	Followers []string
}

// Verdicts of a PerformanceRecommendation.
const (
	// PerformanceRaise says more slots would help and gives the ask that makes
	// them.
	PerformanceRaise = "raise"
	// PerformanceKeep says the evidence is there and the answer is to leave it.
	PerformanceKeep = "keep"
	// PerformanceUnknown says there is not enough measured to say.
	PerformanceUnknown = "unknown"
)

// PerformanceHostChange is what the recommendation does to one host's slots.
type PerformanceHostChange struct {
	HostID   string `json:"host_id"`
	HostName string `json:"host_name"`
	Before   int    `json:"before"`
	After    int    `json:"after"`
	// LimitedBy is the figure that set After: "cpu", "memory" or "disk".
	LimitedBy string `json:"limited_by,omitempty"`
	// Caution is a sentence where this host is the reason After is not higher, or
	// where the change needs watching on it.
	Caution string `json:"caution,omitempty"`
}

// PerformanceRecommendation is the answer: a verdict, why, and the change.
type PerformanceRecommendation struct {
	Verdict string `json:"verdict"`
	Reason  string `json:"reason"`
	// Current and Recommended are the runner ask before and after. They are equal
	// unless the verdict is raise.
	Current     RunnerAsk               `json:"current"`
	Recommended RunnerAsk               `json:"recommended"`
	Hosts       []PerformanceHostChange `json:"hosts"`
	SlotsBefore int                     `json:"slots_before"`
	SlotsAfter  int                     `json:"slots_after"`
	// MaxRunners is the pool maximum that leaves room for SlotsAfter: the pool's
	// own where that already does.
	MaxRunners int `json:"max_runners,omitempty"`
	// Measured is the figures the recommendation rests on, for a person to check
	// it against.
	Measured PerformanceMeasured `json:"measured"`
	// Cautions are the things to know before applying it.
	Cautions []string `json:"cautions,omitempty"`
	// Followers are the other pools whose runners the change resizes.
	Followers []string `json:"followers,omitempty"`
}

// PerformanceMeasured is what was read, in the units the sentence uses.
type PerformanceMeasured struct {
	Samples       int     `json:"samples"`
	CPUPerRunner  float64 `json:"cpu_per_runner"`
	PeakMemoryP90 int64   `json:"peak_memory_p90_mb,omitempty"`
	Jobs          int     `json:"jobs"`
	CeilingShare  float64 `json:"ceiling_share"`
	WaitedSeconds float64 `json:"waited_seconds"`
	OOMKilledJobs int     `json:"oom_killed_jobs"`
}

// RecommendPerformance works out whether the runner ask should change and what
// it should change to.
func RecommendPerformance(e PerformanceEvidence) PerformanceRecommendation {
	rec := PerformanceRecommendation{Verdict: PerformanceUnknown, Current: e.Ask, Recommended: e.Ask, Followers: slices.Clone(e.Followers)}
	slices.Sort(rec.Followers)

	samples, weighted := 0, 0.0
	for _, h := range e.Hosts {
		if h.Samples > 0 && h.CPUPerRunner > 0 {
			samples += h.Samples
			weighted += h.CPUPerRunner * float64(h.Samples)
		}
	}
	rec.Measured = PerformanceMeasured{
		Samples: samples, Jobs: len(e.PeakMemoryMB), WaitedSeconds: e.WaitSeconds, OOMKilledJobs: e.OOMKilled,
	}
	if samples > 0 {
		rec.Measured.CPUPerRunner = weighted / float64(samples)
	}
	if e.WaitSeconds > 0 {
		rec.Measured.CeilingShare = e.CeilingSeconds / e.WaitSeconds
	}
	p90 := percentile90(e.PeakMemoryMB)
	rec.Measured.PeakMemoryP90 = p90
	rec.SlotsBefore = totalBefore(e.Hosts)
	rec.SlotsAfter = rec.SlotsBefore

	switch {
	case len(e.Hosts) == 0:
		rec.Reason = "No connected host can run this pool, so there is nothing to measure or resize."
		return rec
	case e.Ask.Pool != "":
		// The largest ask across the pools decides each host's slots, so while one
		// pool types its own size, the fleet default is not what sets the count and
		// changing it would resize nothing.
		rec.Verdict = PerformanceKeep
		rec.Reason = fmt.Sprintf("Pool %s sets its own size, %s CPU and %s memory, and the largest size any pool asks for is what decides how many runners a host holds. The fleet default would change nothing here; change that pool's size instead.",
			e.Ask.Pool, formatCPUs(e.Ask.CPUs), formatMB(e.Ask.MemoryMB))
		return rec
	case samples < PerformanceMinSamples:
		rec.Reason = fmt.Sprintf("Only %d minutes of load have been measured on these hosts, and %d are needed before a size is recommended. Check again after the fleet has run for a while.",
			samples, PerformanceMinSamples)
		return rec
	case e.OOMKilled > 0:
		rec.Verdict = PerformanceKeep
		rec.Reason = fmt.Sprintf("%s killed for memory in the window, so memory is the limit here and smaller runners would make it worse.", plural(e.OOMKilled, "job"))
		return rec
	case rec.Measured.CeilingShare < PerformanceMinCeilingShare:
		rec.Verdict = PerformanceKeep
		rec.Reason = fmt.Sprintf("Only %.0f%% of the time jobs waited was for want of room for another runner, so more slots would not shorten the wait. The wait is somewhere else; see the queue wait breakdown.",
			rec.Measured.CeilingShare*100)
		return rec
	}

	ask := e.Ask
	ask.CPUs = min(e.Ask.CPUs, max(e.MinimumCPUs, roundUpTo(PerformanceCPUHeadroom*rec.Measured.CPUPerRunner, performanceCPUStep)))
	if len(e.PeakMemoryMB) >= PerformanceMinJobs && p90 > 0 {
		ask.MemoryMB = min(e.Ask.MemoryMB, max(e.MinimumMemoryMB, roundUpInt(int64(math.Ceil(float64(p90)*ProfileMemoryMargin)), performanceMemoryStep)))
	}
	ask.Pool, ask.Pair = "", false
	rec.Recommended = ask

	for _, h := range e.Hosts {
		rec.Hosts = append(rec.Hosts, performanceChange(h, ask))
	}
	slices.SortFunc(rec.Hosts, func(a, b PerformanceHostChange) int { return strings.Compare(a.HostName, b.HostName) })
	rec.SlotsAfter = 0
	for _, h := range rec.Hosts {
		rec.SlotsAfter += h.After
	}
	if rec.SlotsAfter <= rec.SlotsBefore {
		rec.Verdict = PerformanceKeep
		rec.Recommended = e.Ask
		rec.SlotsAfter = rec.SlotsBefore
		rec.Reason = fmt.Sprintf("Runners were measured at about %s of a core each, which a share of %s already covers on these hosts, so there are no more slots to find.",
			formatCPUs(rec.Measured.CPUPerRunner), formatCPUs(e.Ask.CPUs))
		rec.Hosts = nil
		return rec
	}
	rec.Verdict = PerformanceRaise
	rec.MaxRunners = max(e.PoolMax, rec.SlotsAfter)
	rec.Reason = fmt.Sprintf("Jobs spent %.0f%% of their waiting for room for another runner, while a runner used about %s of a core. A share of %s CPU and %s memory holds that with room to spare, and gives the fleet %d slots, up from %d.",
		rec.Measured.CeilingShare*100, formatCPUs(rec.Measured.CPUPerRunner), formatCPUs(ask.CPUs), formatMB(ask.MemoryMB), rec.SlotsAfter, rec.SlotsBefore)
	rec.Cautions = append(rec.Cautions,
		"A runner keeps the size it was created with, so the new slots fill as the runners now running finish their jobs, not at once.")
	if len(rec.Followers) > 0 {
		rec.Cautions = append(rec.Cautions, fmt.Sprintf("The fleet default also sizes %s, which would be resized the same way.", strings.Join(rec.Followers, ", ")))
	}
	for _, h := range rec.Hosts {
		if h.Caution != "" {
			rec.Cautions = append(rec.Cautions, h.Caution)
		}
	}
	return rec
}

// performanceChange is one host's slots at the recommended ask. A host is never moved
// down, and never past what its free disk holds on top of the runners it has.
func performanceChange(h PerformanceHost, ask RunnerAsk) PerformanceHostChange {
	host := h.Host
	out := PerformanceHostChange{HostID: host.ID, HostName: hostName(host), Before: host.Capacity, After: host.Capacity}
	fit, ok := RecommendHost(host, ask)
	if !ok {
		return out
	}
	after, limit := fit.Capacity, fit.LimitedBy
	if host.DiskFreeMB > 0 {
		room := host.ActiveRunners + int(host.DiskFreeMB/RunnerDiskMB)
		if room < after {
			after, limit = room, "disk"
			out.Caution = fmt.Sprintf("%s has %s of disk free, which holds %d runners at %s of working room each, so its slots stop at %d. Free some disk there to go further.",
				hostName(host), formatMB(host.DiskFreeMB), room, formatMB(RunnerDiskMB), after)
		}
	}
	if after > host.Capacity {
		out.After, out.LimitedBy = after, limit
	}
	return out
}

func totalBefore(hosts []PerformanceHost) int {
	n := 0
	for _, h := range hosts {
		n += h.Host.Capacity
	}
	return n
}

// percentile90 is the ninetieth percentile of the figures, by the nearest rank
// above, so one run in ten is allowed to be bigger.
func percentile90(v []int64) int64 {
	if len(v) == 0 {
		return 0
	}
	s := slices.Clone(v)
	slices.Sort(s)
	return s[percentileIndex(len(s))]
}

func roundUpTo(v, step float64) float64 { return math.Ceil(v/step-1e-9) * step }

func roundUpInt(v, step int64) int64 { return (v + step - 1) / step * step }
