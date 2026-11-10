package scheduler

import (
	"cmp"
	"fmt"
	"math"
	"slices"
	"strings"

	"github.com/eyupio/zoomies/internal/naming"
	"github.com/eyupio/zoomies/internal/store"
)

// The modes of scheduler.history_sizing.
const (
	// HistoryOff places runners on the pool's size and the host's room alone,
	// as the fleet always did.
	HistoryOff = "off"
	// HistoryShadow works out what the jobs waiting are known to need and
	// says where it would have placed differently, without doing so. It is
	// the default for the reason placement_mode's shadow is: an operator sees
	// what the policy would do on their own fleet before it does it.
	HistoryShadow = "shadow"
	// HistoryOn places and sizes the runner for the largest known need among
	// the jobs waiting on its pool.
	HistoryOn = "on"
)

const (
	// HistoryDefaultPercentile is which of the waiting jobs' memory needs a runner
	// is sized for when scheduler.history_percentile is unset: the seventy-fifth,
	// so the heaviest quarter of the queue leans on elastic memory and the
	// memory valve instead of sizing every runner for itself.
	HistoryDefaultPercentile = 75
	// HistoryDefaultMaxMultiple is how many times its slot's share of a host's
	// memory a runner sized from history may be given when
	// scheduler.history_max_multiple is unset. Twice: enough for a job that wants
	// half as much again as an ordinary one, and a bound on how few runners a
	// host holds once the queue holds a heavy job.
	HistoryDefaultMaxMultiple = 2
)

const (
	// ProfilePercentile is the share of a job's recent runs its requirement
	// covers. The ninetieth, not the peak of peaks: one run that pulled a
	// cold cache should not size every run after it, and one that did less
	// than usual should not shrink them.
	ProfilePercentile = 0.9
	// ProfileMemoryMargin is added on top of that percentile for memory. A
	// job that used 5 GB on its heaviest ordinary run is killed at exactly 5
	// GB the next time its test data grows, so it is given a fifth more.
	//
	// CPU gets no margin on purpose. A build uses every core it is given, so
	// its peak is its limit; a margin on it would ask for a fifth more CPU
	// every run and ratchet the requirement up to the largest host. Memory
	// does not ratchet the same way: a job does not use memory because it is
	// there.
	ProfileMemoryMargin = 1.2
	// ProfileOOMGrowth is how much more than its peak a run the kernel killed
	// for memory is taken to have needed. Its peak is the limit it hit, which
	// is by definition not enough; half as much again is a guess, but a
	// guess that moves the next run off a host that has already failed it.
	ProfileOOMGrowth = 1.5
	// profileMemoryStep rounds a memory requirement up, so the figure in a
	// reason reads as a size rather than a measurement.
	profileMemoryStep = 64
)

// Requirement is what one queued job is expected to need, from the runs of it
// the fleet has measured. The zero value is "nothing known", which constrains
// nothing: a job with no history is placed exactly as it was before history
// sizing existed.
type Requirement struct {
	CPUs     float64 `json:"cpus,omitempty"`
	MemoryMB int64   `json:"memory_mb,omitempty"`
	// Samples is how many measured runs it was taken from, and OOMKilled how
	// many of them the kernel killed for memory.
	Samples   int `json:"samples,omitempty"`
	OOMKilled int `json:"oom_killed,omitempty"`
}

// Known reports whether the requirement says anything at all.
func (r Requirement) Known() bool { return r.CPUs > 0 || r.MemoryMB > 0 }

// String says the requirement the way a reason does: "~6 GB and 4 CPU".
func (r Requirement) String() string {
	parts := []string{}
	if r.MemoryMB > 0 {
		parts = append(parts, "~"+formatMB(r.MemoryMB))
	}
	if r.CPUs > 0 {
		parts = append(parts, "~"+formatCPUs(r.CPUs)+" CPU")
	}
	return strings.Join(parts, " and ")
}

// Profile turns a job's recent measured runs into its requirement: the
// ninetieth percentile of their peaks, with a margin on memory, and a run the
// kernel killed for memory counted as having needed more than it had.
func Profile(peaks []store.JobPeak) Requirement {
	var cpus []float64
	var mem []int64
	out := Requirement{}
	for _, p := range peaks {
		out.Samples++
		if p.OOMKilled {
			out.OOMKilled++
		}
		if p.CPUs > 0 {
			cpus = append(cpus, p.CPUs)
		}
		// A run the kernel killed needed more than the limit it hit, and that limit is
		// the one figure known to have been too little: the sampled peak can read under
		// it, because a spike between two 30-second samples is not seen. Taken as the
		// higher of the two, as the size classes already do.
		m := p.MemoryMB
		if p.OOMKilled {
			m = max(m, p.GrantedMemoryMB)
		}
		if m > 0 {
			if p.OOMKilled {
				m = int64(math.Ceil(float64(m) * ProfileOOMGrowth))
			}
			mem = append(mem, m)
		}
	}
	if len(cpus) > 0 {
		slices.Sort(cpus)
		out.CPUs = math.Ceil(cpus[percentileIndex(len(cpus))]*100) / 100
	}
	if len(mem) > 0 {
		slices.Sort(mem)
		m := math.Ceil(float64(mem[percentileIndex(len(mem))]) * ProfileMemoryMargin)
		out.MemoryMB = int64(math.Ceil(m/profileMemoryStep)) * profileMemoryStep
	}
	return out
}

// killedNeed is the memory the largest of a job's killed runs needed: the limit it
// hit, or its sampled peak if that is higher, with the growth a kill implies and the
// margin every requirement carries. Zero when no run was killed.
//
// The percentile ignores the heaviest tenth, which is exactly where a single kill sits
// once a job has about ten runs, so the placement path takes the larger of the two: a
// kill is not an outlier of the ordinary kind but a size known to have failed. The
// size classes do not use it, because they decide for themselves which kills still
// count -- one from before the class last moved is what moved it.
func killedNeed(peaks []store.JobPeak) int64 {
	var need int64
	for _, p := range peaks {
		if !p.OOMKilled {
			continue
		}
		limit := max(p.MemoryMB, p.GrantedMemoryMB)
		if limit <= 0 {
			continue
		}
		m := math.Ceil(float64(limit) * ProfileOOMGrowth * ProfileMemoryMargin)
		need = max(need, int64(math.Ceil(m/profileMemoryStep))*profileMemoryStep)
	}
	return need
}

// percentileIndex is the nearest-rank position of ProfilePercentile in n
// sorted samples.
func percentileIndex(n int) int {
	i := int(math.Ceil(ProfilePercentile*float64(n))) - 1
	return clamp(i, 0, n-1)
}

// requirementFor is one queued job's requirement on p: its profile, never
// below the pool's minimum where it says anything at all.
func (t *tick) requirementFor(p *store.Pool, j *store.Job) Requirement {
	peaks := t.history[store.JobUsageKey{Repo: j.Repo, Workflow: j.Workflow, JobName: j.JobName, PoolID: p.ID}]
	r := Profile(peaks)
	if !r.Known() {
		return Requirement{}
	}
	r.MemoryMB = max(r.MemoryMB, killedNeed(peaks))
	if r.CPUs > 0 {
		r.CPUs = max(r.CPUs, p.Resources.MinCPUs)
	}
	if r.MemoryMB > 0 {
		r.MemoryMB = max(r.MemoryMB, p.Resources.MinMemoryMB)
	}
	return r
}

// poolNeed is the requirement a runner created for p's queue is placed for.
//
// Memory only. A job is killed for want of memory and merely slowed for want of
// CPU, and elastic CPU lends a build the cores a host has idle, so a CPU figure
// is no reason to hold a runner back or to make it bigger. It was one: a build
// uses every core it is given, so a job's CPU peak is whatever it was given, and
// sizing every runner for the heaviest waiting job's cores left a handful of
// runners holding a whole host.
//
// GitHub, not Zoomies, decides which of the jobs waiting an idle runner takes,
// so a runner created for this queue may be handed any of them. It is not sized
// for the heaviest: that job is rare, and a runner for every one of its kind
// holds the fleet to a few. It is sized for the HistoryPercentile of the waiting
// jobs' needs, counting each job once, so the heavy tail leans on the memory
// valve, and no runner is given more than HistoryMaxMultiple times its slot's
// share for it.
//
// A job no host that could run this pool has the allocatable room for is left
// out, and said so in the second value: holding every runner back for it
// would keep the light jobs beside it waiting for ever, and a larger host is
// the only thing that helps it.
func (t *tick) poolNeed(p *store.Pool, queued []*store.Job) (Requirement, string) {
	var fitting []Requirement
	var unfit Requirement
	for _, j := range queued {
		r := t.requirementFor(p, j).memoryOnly()
		if !r.Known() {
			continue
		}
		if t.hosts.anyCouldHold(p, r) {
			fitting = append(fitting, r)
			continue
		}
		if r.MemoryMB > unfit.MemoryMB {
			unfit = r
		}
	}
	note := ""
	if unfit.Known() {
		note = fmt.Sprintf("jobs waiting on pool %s are known to need %s, and no host that can run the pool has that much to give", p.Name, unfit)
	}
	if len(fitting) == 0 {
		return Requirement{}, note
	}
	slices.SortFunc(fitting, func(a, b Requirement) int { return cmp.Compare(a.MemoryMB, b.MemoryMB) })
	need := fitting[percentileRank(len(fitting), historyPercentile(t.historyPercentile))]
	need.Samples, need.OOMKilled = 0, 0
	for _, r := range fitting {
		need.Samples += r.Samples
		need.OOMKilled += r.OOMKilled
	}
	return need, note
}

// memoryOnly is the requirement placement acts on: the memory, and no CPU.
func (r Requirement) memoryOnly() Requirement {
	r.CPUs = 0
	return r
}

// historyPercentile reads scheduler.history_percentile, with zero and anything
// outside 1 to 100 meaning the default.
func historyPercentile(v int) int {
	if v < 1 || v > 100 {
		return HistoryDefaultPercentile
	}
	return v
}

// historyMaxMultiple reads scheduler.history_max_multiple the same way, one to
// eight.
func historyMaxMultiple(v int) int {
	if v < 1 || v > 8 {
		return HistoryDefaultMaxMultiple
	}
	return v
}

// percentileRank is the index of the nearest-rank percentile in n sorted
// values: the smallest value that at least that share of them are at or under.
func percentileRank(n, percent int) int {
	return min(n-1, max(0, (n*percent+99)/100-1))
}

// historyCapMB is the most memory a runner sized from history may be given on h:
// HistoryMaxMultiple times its slot's share of the host's allocatable memory, or
// no cap where the host has not said how much it has or has no slots.
func (hs *hostSet) historyCapMB(h *store.Host) int64 {
	a := hs.alloc[h.ID]
	if !a.MemoryKnown || h.Capacity <= 0 || hs.maxMultiple <= 0 {
		return 0
	}
	return a.MemoryMB / int64(h.Capacity) * int64(hs.maxMultiple)
}

// anyCouldHold reports whether any host that could run p has the allocatable
// room for r at all, busy or not.
func (hs *hostSet) anyCouldHold(p *store.Pool, r Requirement) bool {
	for _, h := range hs.hosts {
		if !HostCouldRun(h, p) {
			continue
		}
		a := hs.alloc[h.ID]
		if a.CPUsKnown && a.CPUs+cpuEpsilon < r.CPUs {
			continue
		}
		if a.MemoryKnown && a.MemoryMB < r.MemoryMB {
			continue
		}
		return true
	}
	return false
}

// shortOf says what h cannot give a runner of p that needs r, or "" when it
// can. Only memory is asked, and never more of it than a runner may be given
// here (historyCapMB): a host is held off for a runner it could not carry at the
// size the history would give it, not for one bigger than that.
func (hs *hostSet) shortOf(h *store.Host, p *store.Pool, r Requirement) string {
	left, a := hs.leftFor(h, p), hs.alloc[h.ID]
	want := r.MemoryMB
	if c := hs.historyCapMB(h); c > 0 {
		want = min(want, c)
	}
	if a.MemoryKnown && want > 0 && left.MemoryMB < want {
		return fmt.Sprintf("held off %s: jobs waiting need ~%s, it has %s", hostName(h), formatMB(want), formatMB(max(left.MemoryMB, 0)))
	}
	return ""
}

// heldOff is every host that would take a runner of p now but is short of r,
// with the sentence saying by how much, in host order.
func (hs *hostSet) heldOff(p *store.Pool, r Requirement) map[string]string {
	out := map[string]string{}
	for _, h := range hs.hosts {
		if !hs.eligibleAsSized(h, p) {
			continue
		}
		if s := hs.shortOf(h, p, r); s != "" {
			out[h.ID] = s
		}
	}
	return out
}

// sortedPhrases joins the held-off sentences in host order.
func (hs *hostSet) sortedPhrases(m map[string]string) string {
	var out []string
	for _, h := range hs.hosts {
		if s, ok := m[h.ID]; ok {
			out = append(out, s)
		}
	}
	return strings.Join(out, "; ")
}

// sizeForNeed raises the charge of a runner placed for a known need, in memory
// where the pool leaves it to the host, and returns the size to create it at, or
// the size it already had when nothing moved. The need is held to capMB, what a runner
// may be given on its host, and a field the pool states is left alone: the operator wrote that limit,
// and a runner is never given more than it -- the need then only decides the host.
// CPU is never raised: see poolNeed.
func sizeForNeed(p *store.Pool, res Reservation, size *store.Resources, need Requirement, capMB int64) (Reservation, *store.Resources) {
	want := need.MemoryMB
	if capMB > 0 {
		want = min(want, capMB)
	}
	if p.Resources.MemoryMB > 0 || want <= res.MemoryMB {
		return res, size
	}
	res.MemoryMB = want
	grant := p.Resources
	grant.MinCPUs, grant.MinMemoryMB = 0, 0
	grant.CPUs = res.CPUs / fieldFactor(p, p.Resources.CPUs > 0)
	grant.MemoryMB = res.MemoryMB / int64(fieldFactor(p, p.Resources.MemoryMB > 0))
	return res, &grant
}

func hostName(h *store.Host) string {
	if h.Name != "" {
		return naming.ForSentence(h.Name)
	}
	return h.ID
}
