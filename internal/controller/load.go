package controller

import (
	"math"
	"runtime"
	"sort"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
)

// loadAPIGroups and loadSteps are how many route groups and loop steps the load
// view lists. Both come from fixed sets, so neither is a guard against growth;
// they keep the answer to the busiest few, which is what a question about load
// wants, and they put a ceiling on its size. That matters because Eli is offered
// this view, and a result over the ceiling on what one tool may put in front of
// the model is cut from the front, which would leave JSON with no beginning.
const (
	loadAPIGroups = 15
	loadSteps     = 30
)

// wholePassSteps are the steps that time a loop's entire pass, so the time they
// record already contains the other steps of that loop. Adding a loop's steps up
// would count the pass twice; the view marks these so a reader does not.
var wholePassSteps = map[[2]string]bool{
	{loopDerived, stepAll}: true,
}

// StepLoadView is one step of one controller loop, as the metrics endpoint
// counts it, read back since the controller started.
type StepLoadView struct {
	Loop string `json:"loop"`
	Step string `json:"step"`
	// Whole is set on a step that covers its loop's whole pass, whose time
	// already contains the loop's other steps.
	Whole bool `json:"whole,omitempty"`
	// Count is how many times the step ran, and Seconds the time it took in all.
	// This is elapsed time, not CPU time: a step waiting on the database or on
	// GitHub is counted for as long as it waits.
	Count       uint64  `json:"count"`
	Seconds     float64 `json:"seconds"`
	MeanSeconds float64 `json:"mean_seconds"`
	// SlowestAtMostSeconds bounds the slowest run from above: the upper edge of
	// the timing bucket that run fell in. A run slower than the last bucket has
	// no upper edge, and SlowestOverSeconds says how far the buckets reach.
	SlowestAtMostSeconds *float64 `json:"slowest_at_most_seconds,omitempty"`
	SlowestOverSeconds   *float64 `json:"slowest_over_seconds,omitempty"`
	// UptimeSharePercent is Seconds as a share of the time the controller has been
	// running, which is how much of the controller's life this step occupied.
	UptimeSharePercent float64 `json:"uptime_share_percent"`
}

// RouteGroupLoadView is one group of API routes, as the controller has served it
// since it started.
type RouteGroupLoadView struct {
	Group string `json:"group"`
	// Requests counts every request, including the long-lived ones the timings
	// leave out, and ServerErrors those that ended in a 5xx.
	Requests     uint64 `json:"requests"`
	ServerErrors uint64 `json:"server_errors"`
	// Timed, Seconds and MeanSeconds are over the requests that were timed.
	Timed                uint64   `json:"timed"`
	Seconds              float64  `json:"seconds"`
	MeanSeconds          float64  `json:"mean_seconds"`
	SlowestAtMostSeconds *float64 `json:"slowest_at_most_seconds,omitempty"`
	SlowestOverSeconds   *float64 `json:"slowest_over_seconds,omitempty"`
	UptimeSharePercent   float64  `json:"uptime_share_percent"`
}

// ControllerLoadView is where the controller's own time goes, read from the
// timings it already keeps for the metrics endpoint, with the machine it runs on
// beside them.
//
// Everything here is counted since the process started, so a controller that was
// just restarted has little in it and UptimeSeconds is part of the answer. It
// says which loops and routes take the controller's time, and Machine says how
// much of the machine's CPU the process itself used. It does not say what a step
// does with the CPU: that is what a profile is for.
type ControllerLoadView struct {
	StartedAt     time.Time `json:"started_at"`
	UptimeSeconds float64   `json:"uptime_seconds"`
	Goroutines    int       `json:"goroutines"`
	// ReconcilePasses is the scheduling passes run, by what started them: against
	// Nudges it shows how many wake-ups each pass absorbed.
	ReconcilePasses map[string]uint64 `json:"reconcile_passes"`
	Nudges          uint64            `json:"nudges"`
	// Steps are the loop steps that have run, longest total time first, and
	// StepsNotShown how many quieter ones were left out.
	Steps         []StepLoadView `json:"steps"`
	StepsNotShown int            `json:"steps_not_shown"`
	// API is the busiest route groups by time, and APIGroupsNotShown how many
	// quieter ones were left out.
	API               []RouteGroupLoadView  `json:"api"`
	APIGroupsNotShown int                   `json:"api_groups_not_shown"`
	Machine           ControllerMachineView `json:"machine"`
}

// ControllerLoad reads the controller's timings back out of its metrics. It
// reads the four series directly and not the whole registry, so a request for it
// costs a few hundred counters and does not run every collector a scrape would.
func (c *Controller) ControllerLoad() ControllerLoadView {
	now := c.Now()
	uptime := now.Sub(c.startedAt).Seconds()
	if uptime < 0 {
		uptime = 0
	}
	share := func(seconds float64) float64 {
		if uptime <= 0 {
			return 0
		}
		return loadRound(100*seconds/uptime, 100)
	}

	view := ControllerLoadView{
		StartedAt:       c.startedAt,
		UptimeSeconds:   loadRound(uptime, 10),
		Goroutines:      runtime.NumGoroutine(),
		ReconcilePasses: map[string]uint64{},
		Steps:           []StepLoadView{},
		API:             []RouteGroupLoadView{},
		Machine:         c.ControllerMachine(),
	}

	for _, m := range collectSeries(c.metrics.reconcilePasses) {
		if n := m.GetCounter().GetValue(); n > 0 {
			view.ReconcilePasses[seriesLabel(m, "trigger")] = uint64(n)
		}
	}
	for _, m := range collectSeries(c.metrics.nudges) {
		view.Nudges = uint64(m.GetCounter().GetValue())
	}

	for _, m := range collectSeries(c.metrics.loopStep) {
		h := m.GetHistogram()
		if h.GetSampleCount() == 0 {
			continue
		}
		loop, step := seriesLabel(m, "loop"), seriesLabel(m, "step")
		atMost, over := slowestBucket(h)
		view.Steps = append(view.Steps, StepLoadView{
			Loop: loop, Step: step, Whole: wholePassSteps[[2]string{loop, step}],
			Count: h.GetSampleCount(), Seconds: loadRound(h.GetSampleSum(), 1e4),
			MeanSeconds:          loadRound(h.GetSampleSum()/float64(h.GetSampleCount()), 1e6),
			SlowestAtMostSeconds: atMost, SlowestOverSeconds: over,
			UptimeSharePercent: share(h.GetSampleSum()),
		})
	}
	sort.Slice(view.Steps, func(i, j int) bool {
		a, b := view.Steps[i], view.Steps[j]
		if a.Seconds != b.Seconds {
			return a.Seconds > b.Seconds
		}
		return a.Loop+"/"+a.Step < b.Loop+"/"+b.Step
	})

	if len(view.Steps) > loadSteps {
		view.StepsNotShown = len(view.Steps) - loadSteps
		view.Steps = view.Steps[:loadSteps]
	}

	groups := map[string]*RouteGroupLoadView{}
	group := func(name string) *RouteGroupLoadView {
		if g, ok := groups[name]; ok {
			return g
		}
		g := &RouteGroupLoadView{Group: name}
		groups[name] = g
		return g
	}
	for _, m := range collectSeries(c.metrics.apiRequests) {
		g := group(seriesLabel(m, "group"))
		n := uint64(m.GetCounter().GetValue())
		g.Requests += n
		if seriesLabel(m, "status") == "5xx" {
			g.ServerErrors += n
		}
	}
	for _, m := range collectSeries(c.metrics.apiDuration) {
		h := m.GetHistogram()
		g := group(seriesLabel(m, "group"))
		g.Timed = h.GetSampleCount()
		g.Seconds = loadRound(h.GetSampleSum(), 1e4)
		if g.Timed > 0 {
			g.MeanSeconds = loadRound(h.GetSampleSum()/float64(g.Timed), 1e6)
		}
		g.SlowestAtMostSeconds, g.SlowestOverSeconds = slowestBucket(h)
		g.UptimeSharePercent = share(h.GetSampleSum())
	}
	for _, g := range groups {
		if g.Requests > 0 || g.Timed > 0 {
			view.API = append(view.API, *g)
		}
	}
	sort.Slice(view.API, func(i, j int) bool {
		a, b := view.API[i], view.API[j]
		if a.Seconds != b.Seconds {
			return a.Seconds > b.Seconds
		}
		if a.Requests != b.Requests {
			return a.Requests > b.Requests
		}
		return a.Group < b.Group
	})
	if len(view.API) > loadAPIGroups {
		view.APIGroupsNotShown = len(view.API) - loadAPIGroups
		view.API = view.API[:loadAPIGroups]
	}
	return view
}

// collectSeries reads every series of a collector in the form the exposition
// format is written from, which is the only way to read a metric's value back.
//
// It gathers the one collector through a registry of its own and not the
// controller's, so the other collectors a scrape would run are not run, and it
// starts no goroutine to drain a channel, which the package's panic guard would
// have to account for.
func collectSeries(col prometheus.Collector) []*dto.Metric {
	reg := prometheus.NewRegistry()
	if reg.Register(col) != nil {
		return nil
	}
	// A family that failed to gather is left out and the rest are kept: a reading
	// of where the time goes is worth having without one series.
	families, _ := reg.Gather()
	var out []*dto.Metric
	for _, f := range families {
		out = append(out, f.GetMetric()...)
	}
	return out
}

func seriesLabel(m *dto.Metric, name string) string {
	for _, l := range m.GetLabel() {
		if l.GetName() == name {
			return l.GetValue()
		}
	}
	return ""
}

// slowestBucket says how slow the slowest observation was, as far as the buckets
// can tell. The first bucket whose cumulative count reaches the total holds the
// slowest run, since every run is at or under its upper edge and some run is over
// the edge before it. Past the last bucket there is no edge to give, so the
// answer is how far the buckets reach.
func slowestBucket(h *dto.Histogram) (atMost, over *float64) {
	count := h.GetSampleCount()
	if count == 0 {
		return nil, nil
	}
	var last float64
	for _, b := range h.GetBucket() {
		last = b.GetUpperBound()
		if b.GetCumulativeCount() >= count {
			edge := last
			return &edge, nil
		}
	}
	return nil, &last
}

// loadRound rounds to a multiple of 1/places, so the figures a reader sees are
// the ones that mean something: a tenth of a millisecond is the finest a timing
// here is worth.
func loadRound(v, places float64) float64 {
	return math.Round(v*places) / places
}
