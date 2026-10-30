package main

import (
	"fmt"
	"io"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
)

// View is a function of the model and nothing else. The one exception is the
// terminal's size, which Bubble Tea delivers as a message like any other.

func (m tuiModel) View() tea.View {
	var v tea.View
	v.AltScreen = true
	v.WindowTitle = "zoomies"
	v.SetContent(m.render())
	return v
}

func (m tuiModel) render() string {
	width, height := m.width, m.height
	if width <= 0 {
		width = 80
	}
	if height <= 0 {
		height = 24
	}
	// Two header lines, two footer lines. The body never gets less than three
	// rows, however short the terminal: a dashboard with no room for an answer
	// should say so rather than paint over its own chrome.
	bodyHeight := max(height-4, 3)

	lines := []string{m.headerLine(width), m.tabBar()}
	lines = append(lines, m.bodyLines(bodyHeight)...)
	lines = append(lines, m.footerLines()...)
	return strings.Join(lines, "\n")
}

func (m tuiModel) headerLine(width int) string {
	base := truncate(m.base, max(width-30, 20))
	left := " " + m.paint(colourBold+";"+colourCyan, "zoomies") + "  " + m.paint(colourDim, base)
	right := m.connLabel() + "  " + m.paint(colourDim, m.updatedLabel())
	return spread(left, right, width)
}

func (m tuiModel) tabBar() string {
	var b strings.Builder
	for i, name := range tuiTabNames {
		label := " " + name + " "
		if tuiTab(i) == m.active {
			b.WriteString(m.paint(colourBold+";"+colourBlue, label))
		} else {
			b.WriteString(m.paint(colourDim, label))
		}
	}
	return b.String()
}

func (m tuiModel) connLabel() string {
	switch m.conn {
	case "live":
		return m.paint(colourGreen, "live")
	case "polling":
		return m.paint(colourYellow, "polling")
	case "closed":
		return m.paint(colourRed, "closed")
	default:
		return m.paint(colourDim, m.conn)
	}
}

func (m tuiModel) updatedLabel() string {
	if m.updated.IsZero() {
		return "not loaded yet"
	}
	d := m.now.Sub(m.updated)
	if d < 2*time.Second {
		return "updated just now"
	}
	return "updated " + compactDuration(d) + " ago"
}

func (m tuiModel) bodyLines(height int) []string {
	switch {
	case m.help:
		return fitBody(m.helpLines(), height)
	case m.confirm != nil:
		return fitBody(m.confirmLines(), height)
	}
	if !m.loaded {
		if m.statusErr {
			return fitBody([]string{"", "  " + m.paint(colourRed, m.status)}, height)
		}
		return fitBody([]string{"", "  " + m.paint(colourDim, "Loading the fleet...")}, height)
	}

	switch m.active {
	case tabOverview:
		lines := m.overviewLines()
		if len(lines) > height {
			start := min(max(m.offset[tabOverview], 0), len(lines)-height)
			lines = lines[start : start+height]
		}
		return fitBody(lines, height)
	case tabProblems:
		return m.bodyProblems(height)
	case tabPools:
		return m.bodyPools(height)
	case tabRunners:
		return m.bodyRunners(height)
	case tabHosts:
		return m.bodyHosts(height)
	case tabJobs:
		return m.bodyJobs(height)
	}
	return fitBody(nil, height)
}

// overviewLines is the same facts as `zoomies status`, laid out for a tab
// rather than a page. It cannot simply call printStatus: the one-shot view
// writes sentences as long as they need to be, and the dashboard clips anything
// wider than the terminal rather than wrapping it under the tab bar.
func (m tuiModel) overviewLines() []string {
	width := m.width
	if width <= 0 {
		width = 80
	}
	st := m.data.stats

	var b strings.Builder
	p := m.printerTo(&b)
	fmt.Fprintf(&b, " %s   %s\n\n", truncate(m.base, max(width-20, 20)), p.paint(colourDim, m.data.meta.Version))
	p.keyValues([][2]string{
		{"jobs", fmt.Sprintf("%d queued, %d running", st.QueuedJobs, st.RunningJobs)},
		{"outcomes", fmt.Sprintf("%d in %s: %d succeeded, %d failed, %d cancelled, %d unknown",
			st.Completed, prettyWindow(st.Window), st.Succeeded, st.Failed, st.Cancelled, st.Unknown)},
		{"queue wait", fmt.Sprintf("median %s, p95 %s", millis(st.MedianWaitMS), millis(st.P95WaitMS))},
		{"runners", fmt.Sprintf("%d live: %d busy, %d idle, %d starting, %d draining, %d failed",
			st.Runners.Total, st.Runners.Busy, st.Runners.Idle,
			st.Runners.Provisioning+st.Runners.Registering, st.Runners.Draining, st.Runners.Failed)},
		{"hosts", fmt.Sprintf("%d of %d healthy, %d cordoned, %d of %d slots used",
			st.Hosts.Healthy, st.Hosts.Total, st.Hosts.Cordoned, st.Hosts.Used, st.Hosts.Capacity)},
	})

	if m.data.meta.PollingOnly {
		fmt.Fprintf(&b, "\n %s\n", p.paint(colourYellow,
			"No webhook has arrived: scaling on the fallback poller, which is slower."))
	}

	if len(st.Pools) > 0 {
		fmt.Fprintln(&b, "\nPools")
		for _, pool := range st.Pools {
			line := fmt.Sprintf(" %-24s %s %3.0f%%  %d/%d runners",
				truncate(pool.PoolName, 24), p.bar(pool.Utilisation, 12), pool.Utilisation*100, pool.Live, pool.Max)
			if pool.Busy > 0 {
				line += fmt.Sprintf("  %d busy", pool.Busy)
			}
			if pool.Queued > 0 {
				line += fmt.Sprintf("  %d queued", pool.Queued)
			}
			fmt.Fprintln(&b, line)
		}
	}

	if len(m.data.scaling) > 0 {
		fmt.Fprintln(&b, "\nRecent scaling")
		for _, ev := range m.data.scaling {
			fmt.Fprintf(&b, " %-8s %s\n", m.relTime(ev.CreatedAt), truncate(ev.Reason, max(width-12, 20)))
		}
	}

	fmt.Fprintln(&b)
	items := m.data.problems.Items
	if m.data.problems.OK || len(items) == 0 {
		fmt.Fprintln(&b, " "+p.paint(colourGreen, "Nothing needs your attention."))
	} else {
		fmt.Fprintf(&b, " %d thing(s) need your attention:\n", len(items))
		shown := min(len(items), 5)
		for _, item := range items[:shown] {
			room := max(width-15-len(item.Code), 10)
			fmt.Fprintf(&b, "  [%s] %s  (%s)\n", severityOrInfo(item.Severity), truncate(dash(item.Title), room), item.Code)
		}
		if len(items) > shown {
			fmt.Fprintf(&b, "  ... and %d more on the Problems tab\n", len(items)-shown)
		}
	}
	return strings.Split(strings.TrimRight(b.String(), "\n"), "\n")
}

func (m tuiModel) bodyProblems(height int) []string {
	items := m.data.problems.Items
	if len(items) == 0 {
		return fitBody([]string{"", "  " + m.paint(colourGreen, "Nothing needs your attention.")}, height)
	}
	rows := make([][]string, 0, len(items))
	for _, item := range items {
		rows = append(rows, []string{
			m.severity(item.Severity),
			item.Code,
			truncate(dash(item.Title), 56),
			dash(item.TargetID),
			remedyCell(item),
		})
	}
	return m.listLines([]string{"severity", "code", "problem", "target", "proposed"}, rows, tabProblems, height)
}

func (m tuiModel) bodyPools(height int) []string {
	if len(m.data.pools) == 0 {
		return fitBody([]string{"", "  " + m.paint(colourDim, "No pools yet.")}, height)
	}
	rows := make([][]string, 0, len(m.data.pools))
	for _, pool := range m.data.pools {
		rows = append(rows, []string{
			pool.Name,
			m.enabled(pool.Enabled),
			fmt.Sprintf("%d/%d", pool.Counts.Live, pool.MaxRunners),
			fmt.Sprint(pool.Counts.Busy),
			queuedNote(pool.QueuedJobs),
			fmt.Sprintf("%3.0f%%", pool.Utilisation*100),
		})
	}
	return m.listLines([]string{"pool", "enabled", "live/max", "busy", "queued", "used"}, rows, tabPools, height)
}

func (m tuiModel) bodyRunners(height int) []string {
	if len(m.data.runners) == 0 {
		return fitBody([]string{"", "  " + m.paint(colourDim, "No live runners.")}, height)
	}
	rows := make([][]string, 0, len(m.data.runners))
	for _, runner := range m.data.runners {
		job := "-"
		if runner.CurrentJob != nil {
			job = truncate(runner.CurrentJob.Repo+" "+runner.CurrentJob.JobName, 40)
		}
		rows = append(rows, []string{
			runner.Name,
			runner.ID,
			m.state(runner.State),
			dash(runner.PoolName),
			dash(runner.HostName),
			job,
			m.relTime(runner.CreatedAt),
		})
	}
	return m.listLines([]string{"name", "id", "state", "pool", "host", "job", "age"}, rows, tabRunners, height)
}

func (m tuiModel) bodyHosts(height int) []string {
	if len(m.data.hosts) == 0 {
		return fitBody([]string{"", "  " + m.paint(colourDim, "No hosts.")}, height)
	}
	rows := make([][]string, 0, len(m.data.hosts))
	for _, host := range m.data.hosts {
		capacity := host.Capacity
		if host.EffectiveCapacity > 0 {
			capacity = host.EffectiveCapacity
		}
		rows = append(rows, []string{
			host.Name,
			host.ID,
			m.hostState(host.Healthy, host.Cordoned),
			fmt.Sprintf("%d/%d", host.ActiveRunners, capacity),
			fmt.Sprint(host.Free),
			m.relTime(host.LastHeartbeat),
		})
	}
	return m.listLines([]string{"name", "id", "state", "used/slots", "free", "heartbeat"}, rows, tabHosts, height)
}

func (m tuiModel) bodyJobs(height int) []string {
	if len(m.data.jobs) == 0 {
		return fitBody([]string{"", "  " + m.paint(colourDim, "No jobs.")}, height)
	}
	rows := make([][]string, 0, len(m.data.jobs))
	for _, job := range m.data.jobs {
		state := job.State
		if job.Conclusion != "" {
			state = job.Conclusion
		}
		rows = append(rows, []string{
			dash(job.Repo),
			truncate(dash(job.JobName), 34),
			m.state(state),
			dash(job.PoolName),
			millis(job.QueueWaitMS),
			m.relTime(job.QueuedAt),
		})
	}
	return m.listLines([]string{"repo", "job", "state", "pool", "wait", "queued"}, rows, tabJobs, height)
}

// listLines fits a table to the terminal and keeps the selected row in view.
// The marker is a cell rather than an escape so the column arithmetic in
// printer.table, which already ignores ANSI for width, stays the only copy.
func (m tuiModel) listLines(headers []string, rows [][]string, tab tuiTab, height int) []string {
	page := max(height-1, 1)
	footer := len(rows) > page
	if footer {
		page--
	}
	cursor := m.cursor[tab]
	start := m.offset[tab]
	if cursor < start {
		start = cursor
	}
	if cursor >= start+page {
		start = cursor - page + 1
	}
	start = min(max(start, 0), max(len(rows)-page, 0))
	end := min(start+page, len(rows))

	var b strings.Builder
	p := m.printerTo(&b)
	printRows := make([][]string, 0, end-start)
	for i := start; i < end; i++ {
		marker := "  "
		if i == cursor {
			marker = "> "
		}
		printRows = append(printRows, append([]string{marker}, rows[i]...))
	}
	p.table(append([]string{""}, headers...), printRows)
	lines := strings.Split(strings.TrimRight(b.String(), "\n"), "\n")
	if footer {
		lines = append(lines, m.paint(colourDim, fmt.Sprintf("  %d-%d of %d", start+1, end, len(rows))))
	}
	return fitBody(lines, height)
}

func (m tuiModel) helpLines() []string {
	return []string{
		"",
		"  " + m.paint(colourBold, "zoomies dashboard"),
		"",
		"  1-6, tab, shift+tab   switch tab",
		"  j/k, up/down          move through the rows",
		"  g/G, home/end         first or last row",
		"  enter                 act on the selected row",
		"  a                     acknowledge a failed job on Jobs",
		"  d                     drain a runner, or dismiss a problem",
		"  r                     refresh now",
		"  ?                     close this help",
		"  q                     quit",
		"",
		"  " + m.paint(colourDim, "Every change is confirmed first. It uses the same API"),
		"  " + m.paint(colourDim, "as the web UI and the rest of the CLI, and nothing else."),
	}
}

func (m tuiModel) confirmLines() []string {
	c := m.confirm
	lines := []string{
		"",
		"  " + m.paint(colourBold+";"+colourYellow, "Confirm"),
		"",
		"  " + c.prompt,
	}
	if c.detail != "" && c.detail != "-" {
		lines = append(lines, "", "  "+m.paint(colourDim, c.detail))
	}
	return append(lines, "", "  "+m.paint(colourDim, "y confirm   n cancel"))
}

func (m tuiModel) footerLines() []string {
	status, colour := m.status, colourDim
	switch {
	case status != "" && m.statusErr:
		colour = colourRed
	case status == "" && m.connErr != nil:
		status = "live events unavailable: " + m.connErr.Error() + "; the screen is on the 15s poll"
		colour = colourYellow
	case status == "" && m.loading:
		status = "refreshing..."
	}
	if status == "" {
		status = " "
	}
	hint := " tab switch  j/k move  enter act  r refresh  ? help  q quit"
	if m.width >= 100 {
		hint = " tab switch   j/k move   enter act   a acknowledge   d drain/dismiss   r refresh   ? help   q quit"
	}
	return []string{
		m.paint(colourDim, hint),
		m.paint(colour, " "+status),
	}
}

func (m tuiModel) printerTo(w io.Writer) *printer {
	return &printer{out: w, colour: m.colour, now: func() time.Time { return m.now }}
}

func (m tuiModel) paint(code, s string) string {
	if !m.colour || s == "" {
		return s
	}
	return colourise(code, s)
}

func (m tuiModel) state(s string) string {
	return (&printer{colour: m.colour}).state(s)
}

func (m tuiModel) severity(s string) string {
	switch s {
	case "error":
		return m.paint(colourRed, "error")
	case "warning":
		return m.paint(colourYellow, "warning")
	default:
		return m.paint(colourDim, severityOrInfo(s))
	}
}

func (m tuiModel) enabled(on bool) string {
	if on {
		return m.paint(colourDim, "yes")
	}
	return m.paint(colourYellow, "no")
}

func (m tuiModel) hostState(healthy, cordoned bool) string {
	switch {
	case cordoned:
		return m.paint(colourYellow, "cordoned")
	case !healthy:
		return m.paint(colourRed, "unreachable")
	default:
		return m.paint(colourGreen, "healthy")
	}
}

func (m tuiModel) relTime(t time.Time) string {
	return (&printer{now: func() time.Time { return m.now }}).relTime(t)
}

func remedyCell(item problemItem) string {
	if item.Remedy == nil {
		return "-"
	}
	return "ready"
}

// spread puts one label on the left and one on the right of a line. When they
// cannot both fit, the gap collapses rather than the words overlapping.
func spread(left, right string, width int) string {
	gap := width - displayWidth(left) - displayWidth(right)
	if gap < 1 {
		gap = 1
	}
	return left + strings.Repeat(" ", gap) + right
}

// fitBody pads or cuts the body to exactly height lines, so the footer stays
// where the operator last saw it rather than moving with the data.
func fitBody(lines []string, height int) []string {
	out := make([]string, height)
	copy(out, lines[:min(len(lines), height)])
	return out
}
