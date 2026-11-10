package analysis

import (
	"cmp"
	"errors"
	"fmt"
	"math"
	"slices"
	"strconv"
	"strings"
	"time"
)

const HostPressureVersion = "host-pressure-v1"

// MaxPressureEvents bounds the events a finding lists, newest first, so a
// host that failed all day still fits a reader's window; the counts keep
// the rest.
const MaxPressureEvents = 20

// What a host pressure finding leads with.
const (
	HostPressured    = "pressure"
	HostNotPressured = "not_pressure"
)

// The kinds of thing that went wrong on a host.
const (
	EventFailure     = "failure"
	EventOOM         = "oom"
	EventSlowStartup = "slow_startup"
)

// The measures a threshold can be drawn on.
const (
	MeasureLoadPerCPU = "load_per_cpu"
	MeasureCPUPercent = "cpu_percent"
	MeasureMemoryUsed = "memory_used"
)

// HostMinute is one sample of a host. Any figure the agent did not report is
// nil; a sample with none at all is what an agent too old to measure itself
// leaves, and counts as no sample.
type HostMinute struct {
	At         time.Time
	CPUPercent *float64
	// LoadPerCPU is the one-minute load average over the host's cores.
	LoadPerCPU *float64
	// MemoryAvailableMB is judged against the reserve, as the scheduler
	// judges it; MemoryMB is the machine's size at that minute (a host can
	// be resized), from which the share in use is worked out.
	MemoryAvailableMB *int64
	MemoryMB          int64
}

// HostJob is a job that ran on the host. Repo, Workflow and JobName are
// written by a workflow, so whoever shows them to a model treats them as
// untrusted.
type HostJob struct {
	ID, Repo, Workflow, JobName string
	StartedAt, CompletedAt      *time.Time
	Conclusion, FaultKind       string
	OOMKilled                   bool
	// StartupSeconds is the runner's request to its registration, the
	// startup the queue-wait breakdown carves out of a wait.
	StartupSeconds *int64
}

// PressureThresholds is how a minute is judged pressured here: the load per
// core at which internal/scheduler's throttle can climb a rung, and the host's
// memory reserve. CPU occupancy is deliberately absent as a line of its own. A
// host whose runners are all inside their CPU quotas sits near 100% all day
// and is healthy, so a CPU line would make every busy host's failures "cluster
// in its busy minutes"; the throttle counts CPU alone only while an unlimited
// runner is present, which no sample records, and counts it beside a high load
// rather than on its own.
type PressureThresholds struct {
	LoadPerCPU float64 `json:"load_per_cpu"`
	// MemoryReserveMB is the host's own reserve (store.Host.MemoryReserve):
	// a minute whose available memory is at or below it is pressured.
	MemoryReserveMB int64 `json:"memory_reserve_mb"`
}

// DefaultPressure is the throttle's load line (scheduler.LoadPerCPUOverwhelmed,
// pinned equal by a test, because this package may not import the
// scheduler). The reserve is the host's, so the caller fills it in.
var DefaultPressure = PressureThresholds{LoadPerCPU: 2.0}

type HostPressureSnapshot struct {
	Since, Until     time.Time
	HostID, HostName string
	Minutes          []HostMinute
	// Jobs are those that started or completed on the host in the period.
	Jobs []HostJob
	// JobsTruncated says the job read stopped at MaxReadJobs, keeping the
	// newest: a busy host over a long period runs more than that, and the
	// failures among the oldest are then not there to place.
	JobsTruncated bool
	Thresholds    PressureThresholds
}

// BadEvent is one thing that went wrong on the host and the minute it came
// in. Repo, Workflow and JobName are workflow-written and untrusted.
type BadEvent struct {
	JobID    string    `json:"job_id"`
	Repo     string    `json:"repo"`
	Workflow string    `json:"workflow"`
	JobName  string    `json:"job_name"`
	Kind     string    `json:"kind"`
	At       time.Time `json:"at"`
	// Measured is false when no sample covers the event's minute; Pressured
	// is then false too, which says nothing either way.
	Measured  bool `json:"measured"`
	Pressured bool `json:"pressured"`
}

// PressureThreshold is the line on one measure that separates the minutes
// things went wrong in from the rest: EventShare of the events with that
// measure came at or above Value, which held for MinuteShare of the minutes
// it was measured.
type PressureThreshold struct {
	Measure string `json:"measure"`
	// Value is a load per core, a CPU percentage, or a fraction of memory
	// in use; it is rounded down, so the sentence and the shares are of the
	// same line.
	Value       float64 `json:"value"`
	EventShare  float64 `json:"event_share"`
	MinuteShare float64 `json:"minute_share"`
}

type HostPressureEvidence struct {
	HostID   string `json:"host_id"`
	HostName string `json:"host_name"`
	// Pressure is what a minute was judged pressured by.
	Pressure PressureThresholds `json:"pressure"`
	// Minutes counts the sampled minutes with any measure; ExpectedMinutes
	// is how many minutes the period holds.
	Minutes          int `json:"minutes"`
	PressuredMinutes int `json:"pressured_minutes"`
	ExpectedMinutes  int `json:"expected_minutes"`
	// Events is the newest MaxPressureEvents, newest first; EventsCut says
	// how many older ones were left out. The counts take in every event.
	Events          []BadEvent `json:"events"`
	EventsCut       int        `json:"events_cut"`
	MeasuredEvents  int        `json:"measured_events"`
	PressuredEvents int        `json:"pressured_events"`
	Unmeasured      int        `json:"unmeasured"`
	JobsTruncated   bool       `json:"jobs_truncated"`
	// JobNamesWithheld says the events' repository, workflow and job name
	// were left out, because the reader may read hosts and not jobs.
	JobNamesWithheld bool `json:"job_names_withheld"`
	// Threshold is set only when the events cluster and some measure's line
	// separates them; a line drawn where nothing clusters would be read as
	// the cause.
	Threshold *PressureThreshold `json:"threshold"`
}

type HostPressureFinding = Finding[HostPressureEvidence]

var hostPressureCodes = []string{"host.throttled", "jobs.oom_killed", "jobs.workflow_failing",
	"pool.queue_wait_high", "host.version_behind"}

// failedConclusions is store.FailedConclusions, pinned equal by a test: a job
// that timed out on a busy host is the commonest failure pressure causes.
var failedConclusions = []string{"failure", "timed_out", "startup_failure"}

// The fewest events that are judged at all, and the fewest, with samples for
// nine minutes in ten, that make a finding high.
const (
	minPressureEvents       = 3
	confidentPressureEvents = 5
)

// A startup is slow at twice the host's median and at least a minute: twice
// a ten-second median is still quick.
const slowStartupFloor = 60

// pressureMeasure is one measure a threshold can be drawn on. They are in
// the order a tie is broken: the scheduler's own measures before CPU, which
// only stands in for them.
type pressureMeasure struct {
	name   string
	words  string
	digits float64 // the value is rounded down to 1/digits
	value  func(HostMinute) *float64
	show   func(float64) string
}

var pressureMeasures = []pressureMeasure{
	{MeasureLoadPerCPU, "its load per core", 100, func(m HostMinute) *float64 { return m.LoadPerCPU },
		func(v float64) string { return strconv.FormatFloat(v, 'f', -1, 64) }},
	{MeasureMemoryUsed, "its memory in use", 1000, memoryUsed,
		func(v float64) string { return strconv.FormatFloat(math.Round(v*1000)/10, 'f', -1, 64) + "%" }},
	{MeasureCPUPercent, "its CPU", 10, func(m HostMinute) *float64 { return m.CPUPercent },
		func(v float64) string { return strconv.FormatFloat(v, 'f', -1, 64) + "%" }},
}

// memoryUsed is the share of the machine's memory in use, 1 - available/total.
func memoryUsed(m HostMinute) *float64 {
	if m.MemoryAvailableMB == nil || m.MemoryMB <= 0 {
		return nil
	}
	v := min(1, max(0, 1-float64(*m.MemoryAvailableMB)/float64(m.MemoryMB)))
	return &v
}

func (m HostMinute) measured() bool {
	return m.LoadPerCPU != nil || m.CPUPercent != nil || m.MemoryAvailableMB != nil
}

func (m HostMinute) pressured(t PressureThresholds) bool {
	return (m.LoadPerCPU != nil && *m.LoadPerCPU >= t.LoadPerCPU) ||
		(m.MemoryAvailableMB != nil && *m.MemoryAvailableMB <= t.MemoryReserveMB)
}

// placed is an event with the sample it was judged by.
type placed struct {
	BadEvent
	sample *HostMinute
}

// HostPressure says whether what went wrong on a host (failures, OOM kills,
// slow startups) came in the minutes it was under pressure, with the line on
// one measure that separates them; or that it did not.
func HostPressure(s HostPressureSnapshot) (HostPressureFinding, error) {
	f := HostPressureFinding{Analysis: "host_pressure", Version: HostPressureVersion, Since: s.Since, Until: s.Until,
		NextSteps: []NextStep{}, Codes: []string{}}
	switch {
	case s.Since.IsZero() || !s.Since.Before(s.Until):
		return f, errors.New("host pressure snapshot needs a period with a start before its end")
	case s.HostID == "" && s.HostName == "":
		return f, errors.New("host pressure snapshot names no host, which no sentence could name")
	case s.Thresholds.LoadPerCPU <= 0:
		// A zero line would call every measured minute pressured.
		return f, errors.New("host pressure snapshot has no load per core to judge pressure by")
	}
	host := s.HostName
	if host == "" {
		host = s.HostID
	}
	inPeriod := func(t *time.Time) bool { return t != nil && !t.Before(s.Since) && t.Before(s.Until) }

	// One sample a minute, keyed by the minute it covers. A sample counts
	// when its minute overlaps the period, as the minutes expected do.
	first, end := s.Since.Truncate(time.Minute), s.Until.Add(time.Minute-1).Truncate(time.Minute)
	samples := map[time.Time]HostMinute{}
	for _, m := range s.Minutes {
		k := m.At.Truncate(time.Minute)
		if m.measured() && !k.Before(first) && k.Before(end) {
			samples[k] = m
		}
	}
	e := HostPressureEvidence{HostID: s.HostID, HostName: s.HostName, Pressure: s.Thresholds,
		Minutes: len(samples), ExpectedMinutes: int(end.Sub(first) / time.Minute), JobsTruncated: s.JobsTruncated}
	for _, m := range samples {
		if m.pressured(s.Thresholds) {
			e.PressuredMinutes++
		}
	}

	var events []placed
	for _, ev := range badEvents(s.Jobs, inPeriod) {
		p := placed{BadEvent: ev}
		// The sample of the event's minute, or the latest of the two before:
		// a minute's sample can land a little late, and pressure two minutes
		// old is still the pressure the job ran under.
		for back := range 3 {
			if m, ok := samples[ev.At.Truncate(time.Minute).Add(-time.Duration(back)*time.Minute)]; ok {
				p.sample, p.Measured, p.Pressured = &m, true, m.pressured(s.Thresholds)
				break
			}
		}
		switch {
		case !p.Measured:
			e.Unmeasured++
		case p.Pressured:
			e.MeasuredEvents++
			e.PressuredEvents++
		default:
			e.MeasuredEvents++
		}
		events = append(events, p)
	}
	slices.SortFunc(events, func(a, b placed) int {
		return cmp.Or(b.At.Compare(a.At), strings.Compare(a.JobID, b.JobID), strings.Compare(a.Kind, b.Kind))
	})
	e.EventsCut = max(0, len(events)-MaxPressureEvents)
	e.Events = []BadEvent{}
	for _, p := range events[:min(len(events), MaxPressureEvents)] {
		e.Events = append(e.Events, p.BadEvent)
	}
	var measured []placed
	for _, p := range events {
		if p.Measured {
			measured = append(measured, p)
		}
	}
	f.Evidence = e

	// "of the period" only when every minute of it was measured; otherwise
	// a share of the sampled minutes is not a share of the period.
	ofPeriod := func(minutes int) string {
		if minutes == e.ExpectedMinutes {
			return "of the period"
		}
		return "of the minutes it was measured"
	}
	capacityStep := func(why string) NextStep {
		return NextStep{Setting: "host.capacity", Code: "host.throttled", Text: fmt.Sprintf(
			"Lower %s's capacity so fewer runners share it at once: %s, and the throttle only steps a host down once that pressure has begun.", host, why)}
	}
	calm := e.Minutes - e.PressuredMinutes
	// An event no sample covers still happened. Every sentence that counts
	// measured events says how many fell outside them, so a burst during a
	// gap in the agent's samples is never dropped from the count unseen.
	gap, theMeasured := "", func(what string) string { return fmt.Sprintf("its %d %s", len(measured), what) }
	if e.Unmeasured > 0 {
		gap = fmt.Sprintf("; %d of %d fell where the host sent no samples", e.Unmeasured, len(events))
		theMeasured = func(what string) string { return fmt.Sprintf("the %d %s in measured minutes", len(measured), what) }
	}
	// A cut job read kept the newest jobs, so "none" or "only two" is true of
	// those alone: the sentences say so, and the reason says why.
	among, older, cut := "", "", ""
	if s.JobsTruncated {
		among = fmt.Sprintf(" among the newest %d jobs it ran", MaxReadJobs)
		older = "; the older ones were not read"
		cut = fmt.Sprintf("; also, the period held more than %d jobs on this host; the newest %d were read, so narrow since", MaxReadJobs, MaxReadJobs)
	}
	switch {
	case len(events) == 0:
		f.Confidence, f.ConfidenceReason = Low, "there were no failures, OOM kills or slow startups to compare with its load"+among+cut
		f.Summary = fmt.Sprintf("%s had no failures, OOM kills or slow startups%s in this period%s.", host, among, older)
	case e.Minutes == 0:
		f.Confidence = Low
		f.ConfidenceReason = "no samples with its load, CPU or memory cover this period: an agent too old to report them sends none, " +
			"and retention.samples decides how long they are kept"
		f.Summary = fmt.Sprintf("%s sent no load, CPU or memory samples for this period, so whether its failures follow its load cannot be said.", host)
		f.NextSteps = append(f.NextSteps,
			NextStep{Code: "host.version_behind", Text: fmt.Sprintf(
				"If %s's agent is older than the controller, upgrade it: an agent too old to measure its host reports no load or memory to judge by.", host)},
			NextStep{Setting: "retention.samples", Text: "If the period is older than retention.samples keeps, its samples are gone; ask about a more recent period, or keep samples longer for next time."})
	case len(events) < minPressureEvents:
		few := fmt.Sprintf("only %s: too few to say whether they follow its load", anyBad(len(events)))
		f.Confidence, f.ConfidenceReason = Low, few+cut
		f.Summary = fmt.Sprintf("%s had only %s%s in this period: too few to say whether they follow its load%s.", host, anyBad(len(events)), among, older)
	case len(measured) < minPressureEvents:
		f.Confidence = Low
		f.ConfidenceReason = fmt.Sprintf("only %d of its %s came in a minute with a sample: too few to say whether they follow its load",
			len(measured), anyBad(len(events)))
		f.Summary = fmt.Sprintf("Only %d of %s's %s came in a minute with a sample: too few to say whether they follow its load.",
			len(measured), host, anyBad(len(events)))
	case calm == 0:
		f.Confidence, f.ConfidenceReason = Low, "it was under pressure in every measured minute, so there were no calm minutes to compare with"
		f.Summary = fmt.Sprintf("%s was under pressure in every minute it was measured, so there are no calm minutes to set %s against%s.",
			host, theMeasured(badWords(measured)), gap)
		f.NextSteps = append(f.NextSteps, capacityStep("it was under pressure in every minute it was measured"))
	default:
		pe, ce := e.PressuredEvents, len(measured)-e.PressuredEvents
		// At least twice as many events a pressured minute as a calm one,
		// kept in integers so exactly twice is twice, and never on fewer
		// pressured events than the fewest judged at all: one failure in
		// the one busy minute of a day is not a cluster.
		clustered := pe >= minPressureEvents && e.PressuredMinutes > 0 && pe*calm >= 2*ce*e.PressuredMinutes
		// Every count here is of measured events, so it names their kinds
		// alone; a kind that fell only where there were no samples is named
		// by the gap's clause and the confidence reason, never here.
		what := badWords(measured)
		switch {
		case clustered:
			f.Dominant = HostPressured
			th, above, with := threshold(measured, samples)
			if th != nil {
				f.Summary = fmt.Sprintf("%s's %s cluster in its busy minutes: %d of %d came while %s was at or above %s, which was true for %s %s%s.",
					host, badWords(with), above, len(with), th.words, th.shown, share(th.minutesAbove, th.minutes), ofPeriod(th.minutes), gap)
				f.Evidence.Threshold = &th.PressureThreshold
			} else {
				f.Summary = fmt.Sprintf("%s's %s cluster in its busy minutes: %d of %d came while it was under pressure, which was true for %s %s%s.",
					host, what, pe, len(measured), share(e.PressuredMinutes, e.Minutes), ofPeriod(e.Minutes), gap)
			}
			if hasKind(measured, EventOOM, true) {
				f.NextSteps = append(f.NextSteps, NextStep{Code: "jobs.oom_killed", Setting: "pool.resources.memory_mb", Text: fmt.Sprintf(
					"Jobs on %s were killed for their memory in its busy minutes; the memory a pool gives each runner is also what the scheduler reserves for it, so raising it places fewer runners on %s at once.", host, host)})
			}
			// Said of all of them only when all of them came under pressure.
			came := fmt.Sprintf("its %s came", what)
			if ce > 0 {
				came = fmt.Sprintf("%d of its %d %s came", pe, len(measured), what)
			}
			f.NextSteps = append(f.NextSteps, capacityStep(came+" in the minutes it was under pressure"))
			if ce > pe {
				// Most came while it was calm, and lowering its capacity
				// would not have saved those.
				var calmOnes []placed
				for _, p := range measured {
					if !p.Pressured {
						calmOnes = append(calmOnes, p)
					}
				}
				f.NextSteps = append(f.NextSteps, calmSteps(host, calmOnes, "while it was calm, so its load does not explain them")...)
			}
		case e.PressuredMinutes == 0:
			f.Dominant = HostNotPressured
			// What is known is that pressure was absent, not that load
			// played no part: CPU held by an unlimited runner, which the
			// throttle also counts, is not in a sample.
			f.Summary = fmt.Sprintf("%s was not under pressure in any minute it was measured, so none of %s came under pressure%s.",
				host, theMeasured(what), gap)
		case ce*2 >= len(measured):
			f.Dominant = HostNotPressured
			f.Summary = fmt.Sprintf("%s's %s do not cluster in its busy minutes: %d of %d came while it was calm%s.", host, what, ce, len(measured), gap)
		default:
			// Most came while it was busy, but it was busy most of the
			// time: saying how many came while calm would hide that.
			f.Dominant = HostNotPressured
			f.Summary = fmt.Sprintf("%s's %s do not cluster in its busy minutes: %d of %d came while it was under pressure, but it was under pressure for %s %s%s.",
				host, what, pe, len(measured), share(e.PressuredMinutes, e.Minutes), ofPeriod(e.Minutes), gap)
		}
		if f.Dominant == HostNotPressured {
			f.NextSteps = append(f.NextSteps, calmSteps(host, measured, "whether or not it was busy, so its load is not why")...)
		}
		var reasons []string
		if len(measured) < confidentPressureEvents {
			reasons = append(reasons, fmt.Sprintf("only %d %s came in a measured minute, too few to be sure of the pattern", len(measured), badWords(measured)))
		}
		if e.Unmeasured*10 > len(events) {
			reasons = append(reasons, fmt.Sprintf("%d of its %d %s came in minutes with no sample", e.Unmeasured, len(events), badWords(events)))
		}
		if clustered && ce > pe {
			reasons = append(reasons, fmt.Sprintf("only %d of %d came while it was under pressure, and the other %d came while it was calm", pe, len(measured), ce))
		}
		if e.Minutes*10 < e.ExpectedMinutes*9 {
			reasons = append(reasons, "samples cover "+coverage(e.Minutes, e.ExpectedMinutes)+" of the period")
		}
		if s.JobsTruncated {
			reasons = append(reasons, fmt.Sprintf("the period held more than %d jobs on this host; the newest %d were read, so narrow since", MaxReadJobs, MaxReadJobs))
		}
		f.Confidence = High
		if len(reasons) > 0 {
			f.Confidence, f.ConfidenceReason = Medium, strings.Join(reasons, "; also, ")
		}
	}
	for _, step := range f.NextSteps {
		if step.Code != "" && !slices.Contains(f.Codes, step.Code) {
			f.Codes = append(f.Codes, step.Code)
		}
	}
	return f, nil
}

// badEvents is what went wrong in the period: a failed job or one the fleet
// recorded a fault for, at its completion; an OOM kill, which says why the
// job failed and so is that one event, not a failure as well; and a slow
// startup, at the job's start.
func badEvents(jobs []HostJob, inPeriod func(*time.Time) bool) []BadEvent {
	var startups []int64
	for _, j := range jobs {
		if j.StartupSeconds != nil && inPeriod(j.StartedAt) {
			startups = append(startups, *j.StartupSeconds)
		}
	}
	slices.Sort(startups)
	var median int64
	if len(startups) > 0 {
		median = startups[percentileIndex(len(startups), 0.5)]
	}
	var out []BadEvent
	for _, j := range jobs {
		ev := BadEvent{JobID: j.ID, Repo: j.Repo, Workflow: j.Workflow, JobName: j.JobName}
		if inPeriod(j.CompletedAt) {
			switch {
			case j.OOMKilled:
				ev.Kind, ev.At = EventOOM, *j.CompletedAt
				out = append(out, ev)
			case j.FaultKind != "" || slices.Contains(failedConclusions, j.Conclusion):
				ev.Kind, ev.At = EventFailure, *j.CompletedAt
				out = append(out, ev)
			}
		}
		if j.StartupSeconds != nil && inPeriod(j.StartedAt) && *j.StartupSeconds >= slowStartupFloor && *j.StartupSeconds >= 2*median {
			ev.Kind, ev.At = EventSlowStartup, *j.StartedAt
			out = append(out, ev)
		}
	}
	return out
}

// percentileIndex is the Overview's position for a percentile, round((n-1)*q)
// into the sorted values, so a line drawn here and a figure there cannot
// differ by a rounding rule.
func percentileIndex(n int, q float64) int {
	return int(math.Round(float64(n-1) * q))
}

// drawn is a threshold with what its sentence needs.
type drawn struct {
	PressureThreshold
	words, shown          string
	minutes, minutesAbove int
}

// threshold draws a line on each measure at the 20th percentile of the
// events' values, so four in five of them are at or above it. A line
// separates only when the share of events at or above it is at least twice
// the share of minutes: a line most minutes are above says nothing about
// when things went wrong, however many events are above it too, and with
// every event above the line (as three events always are) a rule about the
// events below it would pass anything. The rule also means the events above
// are at least twice as dense, a minute, as those below, the rule that says
// the events cluster.
//
// Of the lines that separate, the one leaving the fewest minutes above it is
// kept, but CPU only when neither load nor memory separates: the throttle
// never acts on CPU alone, so a CPU line beside a step about the throttle
// would point at a measure it does not use. It returns the line, how many
// events came at or above it, and the events that measure covers.
func threshold(events []placed, samples map[time.Time]HostMinute) (*drawn, int, []placed) {
	var best *drawn
	var bestAbove int
	var bestWith []placed
	for _, m := range pressureMeasures {
		var values []float64
		var with []placed
		for _, p := range events {
			if v := m.value(*p.sample); v != nil {
				values = append(values, *v)
				with = append(with, p)
			}
		}
		if len(values) == 0 {
			continue
		}
		slices.Sort(values)
		line := math.Floor(values[percentileIndex(len(values), 0.2)]*m.digits+1e-9) / m.digits
		above := 0
		for _, v := range values {
			if v >= line {
				above++
			}
		}
		minutes, minutesAbove := 0, 0
		for _, s := range samples {
			if v := m.value(s); v != nil {
				minutes++
				if *v >= line {
					minutesAbove++
				}
			}
		}
		if above*minutes < 2*minutesAbove*len(values) {
			continue
		}
		if m.name == MeasureCPUPercent && best != nil {
			continue
		}
		d := &drawn{PressureThreshold: PressureThreshold{Measure: m.name, Value: line,
			EventShare:  float64(above) / float64(len(values)),
			MinuteShare: float64(minutesAbove) / float64(minutes)},
			words: m.words, shown: m.show(line), minutes: minutes, minutesAbove: minutesAbove}
		if best == nil || d.MinuteShare < best.MinuteShare {
			best, bestAbove, bestWith = d, above, with
		}
	}
	return best, bestAbove, bestWith
}

// calmSteps say where to look for what the host's load does not explain:
// one step for each kind of thing that went wrong, in a fixed order. when
// says which events those are, "while it was calm, so ...".
func calmSteps(host string, events []placed, when string) []NextStep {
	var out []NextStep
	if hasKind(events, EventFailure, false) {
		out = append(out, NextStep{Code: "jobs.workflow_failing", Text: fmt.Sprintf(
			"%s's failures came %s; what each job failed on, a step of its own or a fault the fleet recorded, says what to fix.", host, when)})
	}
	if hasKind(events, EventOOM, false) {
		out = append(out, NextStep{Code: "jobs.oom_killed", Setting: "pool.resources.memory_mb", Text: fmt.Sprintf(
			"Jobs on %s were killed for their memory limit %s: the limit is too small for them, and the memory their pool gives each runner is what to raise.", host, when)})
	}
	if hasKind(events, EventSlowStartup, false) {
		out = append(out, NextStep{Code: "pool.queue_wait_high", Setting: "pool.min_runners", Text: fmt.Sprintf(
			"Runners on %s were slow to start %s; warm runners kept by a pool's minimum runners hide startup from its jobs.", host, when)})
	}
	return out
}

// hasKind reports whether an event of a kind is among events, only among
// the pressured ones when pressured is set.
func hasKind(events []placed, kind string, pressured bool) bool {
	for _, p := range events {
		if p.Kind == kind && (p.Pressured || !pressured) {
			return true
		}
	}
	return false
}

// badWords names the kinds among events, so a sentence's count counts what
// it names: "failures", "failures and OOM kills".
func badWords(events []placed) string {
	var words []string
	for _, k := range []struct{ kind, words string }{{EventFailure, "failures"}, {EventOOM, "OOM kills"}, {EventSlowStartup, "slow startups"}} {
		if hasKind(events, k.kind, false) {
			words = append(words, k.words)
		}
	}
	if len(words) < 2 {
		return strings.Join(words, "")
	}
	return strings.Join(words[:len(words)-1], ", ") + " and " + words[len(words)-1]
}

// anyBad counts every kind together, for a sentence about too few of them.
func anyBad(n int) string {
	if n == 1 {
		return "1 failure, OOM kill or slow startup"
	}
	return fmt.Sprintf("%d failures, OOM kills and slow startups", n)
}

// share is a fraction as a whole percentage that never rounds to a claim:
// a minute in a day is not 0%, and all but one is not 100%.
func share(part, whole int) string {
	p := int(math.Round(float64(part) * 100 / float64(whole)))
	switch {
	case part > 0 && p == 0:
		return "under 1%"
	case part < whole && p == 100:
		return "over 99%"
	}
	return fmt.Sprintf("%d%%", p)
}

// coverage rounds down, so a period short of nine minutes in ten never
// reads as 90% covered.
func coverage(part, whole int) string {
	if p := part * 100 / whole; p > 0 || part == 0 {
		return fmt.Sprintf("%d%%", p)
	}
	return "under 1%"
}

// JobNamesWithheldText is the step a finding carries when its events' job
// names were left out: it says why, and what would show them.
const JobNamesWithheldText = "The repository, workflow and job name of each event were left out, because this token cannot read jobs; use a token that also has the jobs:read scope to see which jobs they were."

// WithholdJobNames is the finding for a reader who may read hosts and not
// jobs. A host's view names no job, so the events lose their repository,
// workflow and job name, which jobs.read guards; every count, time, verdict
// and job id stays, so the answer to the question is the same answer. A
// finding with no events has nothing to withhold and is returned as it is.
func WithholdJobNames(f HostPressureFinding) HostPressureFinding {
	if len(f.Evidence.Events) == 0 {
		return f
	}
	// A copy, so the caller's finding keeps its names.
	events := slices.Clone(f.Evidence.Events)
	for i := range events {
		events[i].Repo, events[i].Workflow, events[i].JobName = "", "", ""
	}
	f.Evidence.Events = events
	f.Evidence.JobNamesWithheld = true
	f.NextSteps = append(slices.Clone(f.NextSteps), NextStep{Text: JobNamesWithheldText})
	return f
}
