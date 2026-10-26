package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/eyupio/zoomies/internal/analysis"
	"github.com/eyupio/zoomies/internal/mcp"
)

// errAnalyseShape is an answer this CLI cannot lay out as the analysis it
// asked for: another analysis, a version it does not know, or not a finding at
// all. It is refused rather than printed, because a field that means something
// else in a later version would be shown as what it meant in this one. The
// sentence is the MCP tools' own, so a person and an agent are told the same.
var errAnalyseShape = errors.New("the controller's analysis is not in the shape this tool expects; upgrade the controller and the CLI together")

// analyseNameWidth is how much of a name a workflow wrote fits in a column
// before it is cut: enough to tell two jobs apart, not so much that one long
// matrix name pushes every other column off the screen.
const analyseNameWidth = 40

// analyseMessageWidth bounds a runner's failure message, which is a host's
// whole line and can quote a stack trace.
const analyseMessageWidth = 160

// runAnalyse is the analyses in a terminal. Each is one read of the
// controller's route and nothing else: the CLI lays the finding out, it does
// not reason about the fleet, so it, the page and Eli cannot disagree.
func runAnalyse(ctx context.Context, e *env, args []string) error {
	return runGroup(ctx, e, "analyse", "Why a period was slow, whether a release was worse, and which host or pool is failing. Each reads the last 24 hours unless --since and --until say otherwise.", []*subcommand{
		{"queue-wait", "[--pool id]", "Why the jobs queued in a period waited, cause by cause, for the fleet and each pool", analyseQueueWait},
		{"compare", "[--by version|period] [--a v] [--b v]", "What got worse and what got better between two releases, or a period and the one before", analyseCompare},
		{"host-pressure", "--host id", "Whether one host's failures, OOM kills and slow starts came when it was busy", analyseHostPressure},
		{"startup-failures", "[--pool id] [--host id]", "Which host or pool keeps failing to start runners, since when and why", analyseStartupFailures},
		{"recurring-failures", "", "Which jobs keep failing, at which step, and whether the fleet, a flaky job or a broken change is behind it", analyseRecurringFailures},
	}, args)
}

// analyseWindow is the period every analysis reads, in the forms `jobs list`
// takes: a duration ago or a date.
type analyseWindow struct{ since, until *string }

func registerAnalyseWindow(fs *flagSet) analyseWindow {
	return analyseWindow{
		since: fs.String("since", "", "start of the period, included: a duration ago such as 6h or 7d, or a date (default 24 hours before --until)"),
		until: fs.String("until", "", "end of the period, not included, in the same forms (default now)"),
	}
}

// set puts the period on the query as the instants the route reads.
func (w analyseWindow) set(cmd string, q url.Values) error {
	// A slice, not a map: with both unreadable, the error names the same one
	// on every run.
	for _, f := range []struct{ name, raw string }{{"since", *w.since}, {"until", *w.until}} {
		name, raw := f.name, f.raw
		if raw == "" {
			continue
		}
		when, err := mcp.ParseWhen(raw)
		if err != nil {
			return usagef(cmd, "--%s %q: %v", name, raw, err)
		}
		q.Set(name, when.UTC().Format(time.RFC3339))
	}
	return nil
}

// readFinding asks a route for its finding and refuses one of another
// analysis or version. The two fields are read alone first, so an answer of
// another shape is refused before its evidence is read as this one's. A
// refusal from the route keeps its message, the route's own sentence saying
// which field, which release or which id and what to change, made safe for the
// terminal and with its stable code beside it.
func readFinding[E any](ctx context.Context, cf *clientFlags, path string, q url.Values, name, version string) (analysis.Finding[E], []byte, error) {
	var f analysis.Finding[E]
	client, err := cf.client()
	if err != nil {
		return f, nil, err
	}
	raw, err := client.get(ctx, path, q, nil)
	if err != nil {
		return f, nil, plainAPIError(err)
	}
	var id struct {
		Analysis string `json:"analysis"`
		Version  string `json:"version"`
	}
	if json.Unmarshal(raw, &id) != nil || id.Analysis != name || id.Version != version {
		return f, nil, errAnalyseShape
	}
	if json.Unmarshal(raw, &f) != nil {
		return f, nil, errAnalyseShape
	}
	return f, raw, nil
}

func analyseQueueWait(ctx context.Context, e *env, args []string) error {
	fs := newFlagSet(e, "zoomies analyse queue-wait [--since t] [--until t] [--pool id]",
		"Why the jobs queued in a period waited: every second put down to one cause, for the fleet and each pool, with each pool's longest waits.")
	win := registerAnalyseWindow(fs)
	pool := fs.String("pool", "", "only this pool's jobs and conditions (a pool ID)")
	cf := registerClientFlags(fs, true)
	fs.example(
		"zoomies analyse queue-wait",
		"zoomies analyse queue-wait --since 6h --pool pool_01abc",
		"zoomies analyse queue-wait --since 2026-10-12 --until 2026-10-13 --output json",
	)
	if err := fs.parse(args); err != nil {
		return err
	}
	if err := fs.noMoreArgs(); err != nil {
		return err
	}
	q := url.Values{}
	if err := win.set("analyse queue-wait", q); err != nil {
		return err
	}
	if *pool != "" {
		q.Set("pool_id", *pool)
	}
	p, err := cf.printer(e)
	if err != nil {
		return err
	}
	f, raw, err := readFinding[analysis.QueueWaitEvidence](ctx, cf, "/analysis/queue-wait", q, "queue_wait", analysis.QueueWaitVersion)
	if err != nil {
		return err
	}
	if p.structured() {
		return p.emit(raw)
	}

	printFindingHead(p, f.Summary, period(f.Since, f.Until), f.Confidence, f.ConfidenceReason)
	ev := f.Evidence
	fmt.Fprintln(p.out)
	p.keyValues([][2]string{
		{"Jobs", fmt.Sprintf("%s read, %d waited, %s of waiting", plural(ev.Jobs, "job"), ev.Waited, analyseSeconds(ev.WaitSeconds))},
		{"Fleet", causeShares(ev.Fleet)},
	})
	if ev.NoRunnerRecord > 0 {
		p.note("%s waited past eligibility with no runner request on record.", plural(ev.NoRunnerRecord, "job"))
	}
	if ev.RunnerKept > 0 {
		p.note("%s waited past eligibility on a runner its pool keeps between jobs.", plural(ev.RunnerKept, "job"))
	}
	if ev.JobsTruncated {
		p.note("The job read was cut at the newest %d; a shorter period reads all of it.", analysis.MaxReadJobs)
	}
	if ev.RunnersTruncated {
		p.note("The runner read was cut at the newest %d; a shorter period reads all of it.", analysis.MaxReadJobs)
	}
	if ev.PoolsCut > 0 {
		p.note("Pools left out of the evidence below, counted in the fleet's totals: %d; --pool reads one whole.", ev.PoolsCut)
	}
	if len(ev.Pools) > 0 {
		fmt.Fprintln(p.out)
		rows := make([][]string, 0, len(ev.Pools))
		for _, pw := range ev.Pools {
			name := plain(pw.PoolName)
			if pw.Deleted {
				name += " (deleted)"
			}
			rows = append(rows, []string{plain(pw.PoolID), dash(name), strconv.Itoa(pw.Jobs), analyseSeconds(pw.WaitSeconds), dash(plain(string(pw.Dominant))), causeShares(pw.Causes)})
		}
		p.table([]string{"pool", "name", "jobs", "waiting", "dominant", "causes"}, rows)

		var longest [][]string
		for _, pw := range ev.Pools {
			for _, w := range pw.Longest {
				longest = append(longest, []string{plain(w.JobID), plain(pw.PoolID), analyseSeconds(w.WaitSeconds), plain(string(w.Cause)),
					workflowName(w.Repo), workflowName(w.Workflow), workflowName(w.JobName)})
			}
		}
		if len(longest) > 0 {
			fmt.Fprintln(p.out)
			p.note("Each pool's longest waits:")
			p.table([]string{"job", "pool", "waited", "cause", "repository", "workflow", "job name"}, longest)
		}
	}
	printNextSteps(p, f.NextSteps, f.Codes)
	return nil
}

func analyseCompare(ctx context.Context, e *env, args []string) error {
	fs := newFlagSet(e, "zoomies analyse compare [--by version|period] [--a v] [--b v] [--since t] [--until t]",
		"What got worse and what got better: two releases' jobs, or a period against the one of the same length before it, measure by measure.")
	win := registerAnalyseWindow(fs)
	by := fs.String("by", "version", "version compares two releases; period compares the period with the one before it")
	a := fs.String("a", "", "the earlier release, such as v1.3.2 (default the newest whose last job came before b's); only --by version")
	b := fs.String("b", "", "the later release, such as v1.3.3 (default the newest to complete a job in the period); only --by version")
	cf := registerClientFlags(fs, true)
	fs.example(
		"zoomies analyse compare --since 14d",
		"zoomies analyse compare --a v1.3.2 --b v1.3.3 --since 30d",
		"zoomies analyse compare --by period --since 7d",
	)
	if err := fs.parse(args); err != nil {
		return err
	}
	if err := fs.noMoreArgs(); err != nil {
		return err
	}
	switch *by {
	case "version":
	case "period":
		// The route refuses these too; saying so here spends no request and
		// names the flags the person typed rather than the route's parameters.
		if *a != "" || *b != "" {
			return usagef("analyse compare", "--a and --b name releases, which a comparison by period does not read; leave them out, or use --by version")
		}
	default:
		return usagef("analyse compare", "--by %q: a comparison is by version or period", *by)
	}
	q := url.Values{"by": {*by}}
	if err := win.set("analyse compare", q); err != nil {
		return err
	}
	if *a != "" {
		q.Set("a", *a)
	}
	if *b != "" {
		q.Set("b", *b)
	}
	p, err := cf.printer(e)
	if err != nil {
		return err
	}
	f, raw, err := readFinding[analysis.CompareEvidence](ctx, cf, "/analysis/compare", q, "compare", analysis.CompareVersion)
	if err != nil {
		return err
	}
	if p.structured() {
		return p.emit(raw)
	}

	// No period line: each side covers its own span (a release's first job to
	// its last, or the whole days the roll-up read), and the period asked for
	// is neither, so the sides' own dates are the ones shown.
	printFindingHead(p, f.Summary, "", f.Confidence, f.ConfidenceReason)
	ev := f.Evidence
	fmt.Fprintln(p.out)
	kv := [][2]string{{"By", plain(ev.By)}}
	if len(ev.Excluded) > 0 {
		names := make([]string, 0, len(ev.Excluded))
		for _, x := range ev.Excluded {
			names = append(names, plain(x))
		}
		kv = append(kv, [2]string{"Left out", strings.Join(names, " and ") + " jobs, from every figure"})
	}
	p.keyValues(kv)
	fmt.Fprintln(p.out)
	sides := []struct {
		name string
		side analysis.CompareSide
	}{{"a", ev.A}, {"b", ev.B}}
	rows := make([][]string, 0, 2)
	for _, s := range sides {
		g := s.side.Figures
		row := []string{s.name, dash(plain(s.side.Label)), instantOrDash(s.side.Since), instantOrDash(s.side.Until), strconv.Itoa(g.Jobs)}
		if s.side.CountsOnly {
			// The roll-up keeps no outcomes or OOM kills, and a zero there is
			// not a measurement.
			row = append(row, "-", "-", strconv.Itoa(g.FleetFailed), "-")
		} else {
			row = append(row, strconv.Itoa(g.Succeeded), strconv.Itoa(g.Failed), strconv.Itoa(g.FleetFailed), strconv.Itoa(g.OOMKilled))
		}
		slots := "-"
		if s.side.MeanSlots != nil {
			slots = strconv.FormatFloat(*s.side.MeanSlots, 'f', 1, 64)
		}
		rows = append(rows, append(row, slots))
	}
	p.table([]string{"side", "label", "from", "to", "jobs", "succeeded", "failed", "fleet failures", "OOM kills", "mean slots"}, rows)
	for _, s := range sides {
		if !s.side.AskedUntil.IsZero() && !s.side.AskedUntil.Equal(s.side.Until) {
			p.note("%s: asked until %s, read to %s.", s.name, instant(s.side.AskedUntil), instant(s.side.Until))
		}
		if s.side.CountsOnly {
			p.note("%s: counts only, read from the daily roll-up in whole UTC days, which keeps no outcomes, OOM kills or times.", s.name)
		}
		if s.side.JobsGoneBefore != nil {
			p.note("%s: jobs before %s are gone (retention.jobs), and no roll-up records which release ran them.", s.name, instant(*s.side.JobsGoneBefore))
		}
		if s.side.NoRecordBefore != nil {
			p.note("%s: the fleet keeps no counts before %s.", s.name, instant(*s.side.NoRecordBefore))
		}
	}
	if ev.PercentilesUnavailable {
		p.note("Percentiles are unavailable: a side was read from the daily roll-up, which keeps counts only.")
	}
	if len(ev.Changes) > 0 {
		fmt.Fprintln(p.out)
		changes := make([][]string, 0, len(ev.Changes))
		for _, c := range ev.Changes {
			ratio := "-"
			if c.Ratio != nil {
				ratio = strconv.FormatFloat(*c.Ratio, 'f', 2, 64)
			}
			changes = append(changes, []string{plain(c.Measure), plain(c.Direction), mcp.ChangeFigure(c.From, c.Unit), mcp.ChangeFigure(c.To, c.Unit), ratio})
		}
		p.table([]string{"measure", "change", "a", "b", "ratio"}, changes)
	}
	printNextSteps(p, f.NextSteps, f.Codes)
	return nil
}

func analyseHostPressure(ctx context.Context, e *env, args []string) error {
	fs := newFlagSet(e, "zoomies analyse host-pressure --host id [--since t] [--until t]",
		"Whether what went wrong on one host came when it was busy: its failures, OOM kills and slow startups against that minute's load and memory.")
	win := registerAnalyseWindow(fs)
	host := fs.String("host", "", "the host to read (a host ID, required)")
	cf := registerClientFlags(fs, true)
	fs.example(
		"zoomies analyse host-pressure --host host_01abc",
		"zoomies analyse host-pressure --host host_01abc --since 7d --output json",
	)
	if err := fs.parse(args); err != nil {
		return err
	}
	if err := fs.noMoreArgs(); err != nil {
		return err
	}
	if strings.TrimSpace(*host) == "" {
		return usagef("analyse host-pressure", "name the host with --host <host-id>; host pressure is about one host (`zoomies hosts list` shows their IDs)")
	}
	q := url.Values{"host_id": {*host}}
	if err := win.set("analyse host-pressure", q); err != nil {
		return err
	}
	p, err := cf.printer(e)
	if err != nil {
		return err
	}
	f, raw, err := readFinding[analysis.HostPressureEvidence](ctx, cf, "/analysis/host-pressure", q, "host_pressure", analysis.HostPressureVersion)
	if err != nil {
		return err
	}
	if p.structured() {
		return p.emit(raw)
	}

	printFindingHead(p, f.Summary, period(f.Since, f.Until), f.Confidence, f.ConfidenceReason)
	ev := f.Evidence
	fmt.Fprintln(p.out)
	pressured := "load per core at or above " + strconv.FormatFloat(ev.Pressure.LoadPerCPU, 'f', -1, 64)
	if ev.Pressure.MemoryReserveMB > 0 {
		pressured += fmt.Sprintf(", or available memory at or below its reserve of %d MB", ev.Pressure.MemoryReserveMB)
	}
	hostName := plain(ev.HostID)
	if ev.HostName != "" {
		hostName += " (" + plain(ev.HostName) + ")"
	}
	kv := [][2]string{
		{"Host", hostName},
		{"Pressured", pressured},
		{"Minutes", fmt.Sprintf("%d sampled of %d, %d pressured", ev.Minutes, ev.ExpectedMinutes, ev.PressuredMinutes)},
		{"Events", fmt.Sprintf("%d measured, %d under pressure, %d in a minute no sample covers", ev.MeasuredEvents, ev.PressuredEvents, ev.Unmeasured)},
	}
	if t := ev.Threshold; t != nil {
		kv = append(kv, [2]string{"Threshold", fmt.Sprintf("%s at or above %s: %s of events against %s of minutes",
			plain(t.Measure), strconv.FormatFloat(t.Value, 'f', -1, 64), analysePercent(t.EventShare), analysePercent(t.MinuteShare))})
	}
	p.keyValues(kv)
	if ev.JobsTruncated {
		p.note("The job read was cut at the newest %d; a shorter period reads all of it.", analysis.MaxReadJobs)
	}
	if ev.EventsCut > 0 {
		p.note("Older events left out of the evidence below, counted above: %d; an earlier --until reads them.", ev.EventsCut)
	}
	if len(ev.Events) > 0 {
		fmt.Fprintln(p.out)
		// Without jobs.read the route blanks the names, and the controller's
		// own step says so once. Empty columns would read as jobs with no
		// name, so they are left out rather than shown as dashes.
		headers := []string{"at", "job", "kind", "verdict"}
		if !ev.JobNamesWithheld {
			headers = append(headers, "repository", "workflow", "job name")
		}
		rows := make([][]string, 0, len(ev.Events))
		for _, x := range ev.Events {
			verdict := "no sample covers its minute"
			switch {
			case x.Pressured:
				verdict = "under pressure"
			case x.Measured:
				verdict = "not under pressure"
			}
			row := []string{instant(x.At), plain(x.JobID), plain(x.Kind), verdict}
			if !ev.JobNamesWithheld {
				row = append(row, workflowName(x.Repo), workflowName(x.Workflow), workflowName(x.JobName))
			}
			rows = append(rows, row)
		}
		p.table(headers, rows)
	}
	printNextSteps(p, f.NextSteps, f.Codes)
	return nil
}

func analyseStartupFailures(ctx context.Context, e *env, args []string) error {
	fs := newFlagSet(e, "zoomies analyse startup-failures [--pool id] [--host id] [--since t] [--until t]",
		"Which host or pool keeps failing to start runners: how many starts failed, since when, at which stage, with which fault, and why.")
	win := registerAnalyseWindow(fs)
	pool := fs.String("pool", "", "only this pool's starts (a pool ID)")
	host := fs.String("host", "", "only this host's starts (a host ID)")
	cf := registerClientFlags(fs, true)
	fs.example(
		"zoomies analyse startup-failures",
		"zoomies analyse startup-failures --host host_01abc --since 7d",
		"zoomies analyse startup-failures --pool pool_01abc --output json",
	)
	if err := fs.parse(args); err != nil {
		return err
	}
	if err := fs.noMoreArgs(); err != nil {
		return err
	}
	q := url.Values{}
	if err := win.set("analyse startup-failures", q); err != nil {
		return err
	}
	if *pool != "" {
		q.Set("pool_id", *pool)
	}
	if *host != "" {
		q.Set("host_id", *host)
	}
	p, err := cf.printer(e)
	if err != nil {
		return err
	}
	f, raw, err := readFinding[analysis.StartupEvidence](ctx, cf, "/analysis/startup-failures", q, "startup_failures", analysis.StartupFailuresVersion)
	if err != nil {
		return err
	}
	if p.structured() {
		return p.emit(raw)
	}

	printFindingHead(p, f.Summary, period(f.Since, f.Until), f.Confidence, f.ConfidenceReason)
	ev := f.Evidence
	fmt.Fprintln(p.out)
	p.keyValues([][2]string{{"Starts", fmt.Sprintf("%d with an outcome, %d failed, %d with no outcome on record", ev.Starts, ev.Failures, ev.Unknown)}})
	if ev.Truncated {
		p.note("The runner read was cut at the newest %d; a shorter period reads all of it.", analysis.MaxReadJobs)
	}
	if ev.GroupsCut > 0 {
		p.note("Hosts and pools left out of the evidence below: %d; --pool or --host reads one.", ev.GroupsCut)
	}
	if len(ev.Groups) > 0 {
		fmt.Fprintln(p.out)
		rows := make([][]string, 0, len(ev.Groups))
		var last [][2]string
		for _, g := range ev.Groups {
			now := "still failing"
			if g.RecoveredAt != nil {
				now = "recovered " + instant(*g.RecoveredAt)
			}
			explained := "-"
			if g.Explanation != "" {
				explained = fmt.Sprintf("%s (%d)", plain(g.Explanation), g.Explained)
			}
			rows = append(rows, []string{plain(g.Scope), plain(g.ID), dash(plain(g.Name)), dash(plain(g.PoolID)),
				fmt.Sprintf("%d of %d", g.Failures, g.Starts), strconv.Itoa(g.Unknown), instant(g.FailingSince), now,
				dash(plain(g.Stage)), dash(plain(g.FaultKind)), explained})
			if g.LastRunnerID != "" || g.LastMessage != "" {
				// The message is a host's report of why a runner would not
				// start, and can quote what a workflow or an image wrote.
				line := "runner " + dash(plain(g.LastRunnerID))
				if g.LastMessage != "" {
					line += ": " + truncate(plain(g.LastMessage), analyseMessageWidth)
				}
				last = append(last, [2]string{plain(g.ID), line})
			}
		}
		p.table([]string{"scope", "id", "name", "pool", "failed", "no outcome", "failing since", "now", "stage", "fault", "explained by"}, rows)
		if len(last) > 0 {
			fmt.Fprintln(p.out)
			p.note("Each one's last failed start:")
			p.keyValues(last)
		}
	}
	printNextSteps(p, f.NextSteps, f.Codes)
	return nil
}

func analyseRecurringFailures(ctx context.Context, e *env, args []string) error {
	fs := newFlagSet(e, "zoomies analyse recurring-failures [--since t] [--until t]",
		"Which jobs keep failing: how often, since when, at which step, and whether the runner, a rerun of the same revision or a lasting break is behind it. It reads outcomes, so it cannot say why a test failed.")
	win := registerAnalyseWindow(fs)
	cf := registerClientFlags(fs, true)
	fs.example(
		"zoomies analyse recurring-failures",
		"zoomies analyse recurring-failures --since 7d",
		"zoomies analyse recurring-failures --output json",
	)
	if err := fs.parse(args); err != nil {
		return err
	}
	if err := fs.noMoreArgs(); err != nil {
		return err
	}
	q := url.Values{}
	if err := win.set("analyse recurring-failures", q); err != nil {
		return err
	}
	p, err := cf.printer(e)
	if err != nil {
		return err
	}
	f, raw, err := readFinding[analysis.RecurringEvidence](ctx, cf, "/analysis/recurring-failures", q, "recurring_failures", analysis.RecurringFailuresVersion)
	if err != nil {
		return err
	}
	if p.structured() {
		return p.emit(raw)
	}

	printFindingHead(p, f.Summary, period(f.Since, f.Until), f.Confidence, f.ConfidenceReason)
	ev := f.Evidence
	fmt.Fprintln(p.out)
	p.keyValues([][2]string{{"Jobs", fmt.Sprintf("%d finished with a pass or a failure, %d failed", ev.Jobs, ev.Failures)}})
	if ev.Truncated {
		p.note("The job read was cut at the newest %d; a shorter period reads all of it.", analysis.MaxReadJobs)
	}
	if ev.GroupsCut > 0 {
		p.note("Jobs left out of the evidence below: %d; a shorter period reads fewer.", ev.GroupsCut)
	}
	if len(ev.Groups) > 0 {
		fmt.Fprintln(p.out)
		rows := make([][]string, 0, len(ev.Groups))
		for _, g := range ev.Groups {
			now := "passed since"
			if g.StillFailing {
				now = "still failing"
			}
			rows = append(rows, []string{plain(g.Repository), plain(g.Workflow), plain(g.Job),
				fmt.Sprintf("%d of %d", g.Failures, g.Runs), strconv.Itoa(g.Branches), instant(g.FirstFailureAt), now,
				dash(plain(g.FailedStep)), strconv.Itoa(g.RunnerFaults), strconv.Itoa(g.RetryPasses), plain(g.Class)})
		}
		p.table([]string{"repository", "workflow", "job", "failed", "branches", "first failure", "now", "step", "runner faults", "rerun passed", "looks like"}, rows)
	}
	printNextSteps(p, f.NextSteps, f.Codes)
	return nil
}

// printFindingHead is what every finding says before its evidence: the
// sentence, the period it covers, and how far it may be leaned on.
func printFindingHead(p *printer, summary, period string, confidence analysis.Confidence, reason string) {
	fmt.Fprintln(p.out, plain(summary))
	fmt.Fprintln(p.out)
	conf := plain(string(confidence))
	if reason != "" {
		conf += ": " + plain(reason)
	}
	var kv [][2]string
	if period != "" {
		kv = append(kv, [2]string{"Period", period})
	}
	p.keyValues(append(kv, [2]string{"Confidence", conf}))
}

// printNextSteps numbers the finding's steps with the setting and the code
// each names. A step may name neither (the one that says job names were left
// out is a step all the same), and then it is printed as its sentence alone.
func printNextSteps(p *printer, steps []analysis.NextStep, codes []string) {
	if len(steps) > 0 {
		fmt.Fprintln(p.out)
		fmt.Fprintln(p.out, p.paint(colourYellow, "Next:"))
	}
	for i, s := range steps {
		line := fmt.Sprintf("  %d. %s", i+1, plain(s.Text))
		var names []string
		if s.Setting != "" {
			names = append(names, "setting "+plain(s.Setting))
		}
		if s.Code != "" {
			names = append(names, "code "+plain(s.Code))
		}
		if len(names) > 0 {
			line += " (" + strings.Join(names, ", ") + ")"
		}
		fmt.Fprintln(p.out, line)
		if s.Link != "" {
			fmt.Fprintln(p.out, "     "+plain(s.Link))
		}
	}
	if len(codes) > 0 {
		names := make([]string, 0, len(codes))
		for _, c := range codes {
			names = append(names, plain(c))
		}
		fmt.Fprintln(p.out)
		fmt.Fprintln(p.out, p.paint(colourYellow, "Codes: ")+strings.Join(names, ", "))
	}
}

// workflowName is a name a workflow wrote, made safe for the terminal before
// it is cut, so a cut cannot leave half an escape sequence behind.
func workflowName(s string) string { return dash(truncate(plain(s), analyseNameWidth)) }

// instant is a time to the second in UTC, the form --since and --until take,
// so a time read off the screen can be pasted back into a flag.
func instant(t time.Time) string { return t.UTC().Format(time.RFC3339) }

func instantOrDash(t time.Time) string {
	if t.IsZero() {
		return "-"
	}
	return instant(t)
}

func period(since, until time.Time) string {
	if since.IsZero() || until.IsZero() {
		return ""
	}
	return instant(since) + " to " + instant(until)
}

func analyseSeconds(s int64) string { return analysis.Words(time.Duration(s) * time.Second) }

func analysePercent(share float64) string {
	return strconv.FormatFloat(share*100, 'f', 0, 64) + "%"
}

func causeShares(shares []analysis.CauseShare) string {
	parts := make([]string, 0, len(shares))
	for _, c := range shares {
		parts = append(parts, fmt.Sprintf("%s %s (%s)", plain(string(c.Cause)), analyseSeconds(c.Seconds), analysePercent(c.Share)))
	}
	if len(parts) == 0 {
		return "none"
	}
	return strings.Join(parts, ", ")
}
