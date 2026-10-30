package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"strconv"
	"time"

	tea "charm.land/bubbletea/v2"
	"golang.org/x/term"
)

// The dashboard is the one command in this package that owns the terminal for
// as long as it runs. It is still a pure API client: every number on the screen
// comes from the same routes `zoomies status`, the web UI and the MCP server
// read, so it cannot show an operator a fleet the rest of the product does not
// agree with.
//
// The model is deliberately free of I/O. Data arrives as messages from
// commands, the event stream is a goroutine behind a channel, and the view is a
// function of the model. That is what lets the whole screen be tested without a
// controller or a terminal.

const (
	// tuiRefreshEvery is the full reconcile. Events drive the fast path; this
	// catches anything an event cannot carry, such as a metric changing with
	// no row written.
	tuiRefreshEvery = 15 * time.Second
	// tuiDebounce collapses a burst of events (a scaling pass publishes many)
	// into one reload. Rendering every frame of a burst would cost more than
	// the delay it saves.
	tuiDebounce = 400 * time.Millisecond
	// tuiPageLimit bounds each list. A dashboard is for seeing the fleet, not
	// for paging through a season of jobs; the CLI's own list commands page.
	tuiPageLimit = 200
	tabCount     = 6
)

// tuiAPI is the part of the CLI's API client the dashboard uses. Keeping it to
// four verbs is what makes the model testable with a fake, and keeps the TUI
// from reaching for the controller or the store.
type tuiAPI interface {
	get(ctx context.Context, path string, q url.Values, out any) ([]byte, error)
	post(ctx context.Context, path string, q url.Values, body, out any) ([]byte, error)
	patch(ctx context.Context, path string, q url.Values, body, out any) ([]byte, error)
	del(ctx context.Context, path string, q url.Values, out any) ([]byte, error)
}

func runTUI(ctx context.Context, e *env, args []string) error {
	fs := newFlagSet(e, "zoomies tui [--window 1h]",
		"A live dashboard for a controller: counts, pools, runners, hosts, jobs and problems.\n\n"+
			"It keeps itself current from the controller's event stream and falls back to the held\n"+
			"long poll when a proxy will not carry a stream. Operator actions ask for confirmation\n"+
			"before they change anything.")
	cf := registerClientFlags(fs, false)
	window := fs.String("window", "1h", "the window the completed/failed counts and queue waits cover")
	fs.example("zoomies tui", "zoomies tui --window 24h")
	if err := fs.parse(args); err != nil {
		return err
	}
	if err := fs.noMoreArgs(); err != nil {
		return err
	}
	if !terminalWriter(e.out) || !terminalReader(e.in) {
		return usagef("tui", "needs a terminal on input and output; use `zoomies status` when the output is piped")
	}
	client, err := cf.client()
	if err != nil {
		return err
	}

	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	events := make(chan tea.Msg, 128)
	go followTUIEvents(runCtx, client, events)

	m := newTUIModel(runCtx, client, client.base, *window, events)
	p := tea.NewProgram(m, tea.WithContext(runCtx), tea.WithInput(e.in), tea.WithOutput(e.out))
	if _, err := p.Run(); err != nil {
		// Cancelling the context the program runs in is how this command
		// stops when the operator interrupts it; that is not a failure.
		if errors.Is(err, tea.ErrProgramKilled) || errors.Is(err, context.Canceled) {
			return nil
		}
		return err
	}
	return nil
}

// terminalWriter and terminalReader are deliberately not isTerminal: NO_COLOR
// asks for no colour, not for no dashboard, and the CLI's isTerminal conflates
// the two.
func terminalWriter(w io.Writer) bool {
	f, ok := w.(*os.File)
	return ok && term.IsTerminal(int(f.Fd()))
}

func terminalReader(r io.Reader) bool {
	f, ok := r.(*os.File)
	return ok && term.IsTerminal(int(f.Fd()))
}

type tuiTab int

const (
	tabOverview tuiTab = iota
	tabProblems
	tabPools
	tabRunners
	tabHosts
	tabJobs
)

var tuiTabNames = [tabCount]string{"Overview", "Problems", "Pools", "Runners", "Hosts", "Jobs"}

// tuiData is one refresh of everything the tabs show. It is one message rather
// than one per tab so that the header, the overview and a tab cannot disagree
// about the moment they describe.
type tuiData struct {
	meta     metaResponse
	stats    statsResponse
	problems problemsResponse
	scaling  []scalingItem
	pools    []poolItem
	runners  []runnerItem
	hosts    []hostItem
	jobs     []jobItem
}

type tuiModel struct {
	ctx    context.Context
	api    tuiAPI
	base   string
	window string
	colour bool

	width, height int
	active        tuiTab
	data          tuiData
	loaded        bool

	// cursor and offset are per tab, so switching away and back keeps the
	// operator where they were.
	cursor [tabCount]int
	offset [tabCount]int

	loading   bool
	updated   time.Time
	now       time.Time
	conn      string
	connErr   error
	status    string
	statusErr bool
	confirm   *tuiConfirm
	help      bool

	events     <-chan tea.Msg
	refreshSeq int
}

func newTUIModel(ctx context.Context, api tuiAPI, base, window string, events <-chan tea.Msg) tuiModel {
	now := time.Now()
	return tuiModel{
		ctx:    ctx,
		api:    api,
		base:   base,
		window: window,
		colour: os.Getenv("NO_COLOR") == "" && os.Getenv("TERM") != "dumb",
		now:    now,
		conn:   "connecting",
		// The initial load is in flight from Init; starting false would let
		// an event that arrives first start a second one.
		loading: true,
		events:  events,
	}
}

func (m tuiModel) Init() tea.Cmd {
	return tea.Batch(
		m.load(),
		waitTUIEvent(m.events),
		tea.Every(tuiRefreshEvery, func(t time.Time) tea.Msg { return tuiTickMsg(t) }),
	)
}

// Messages. Data comes from a pull, changes from the push, and actions from a
// person; each is a distinct type so Update cannot mistake one for another.
type (
	tuiDataMsg struct {
		data tuiData
		at   time.Time
		err  error
	}
	tuiEventMsg     struct{ kind string }
	tuiHeartbeatMsg struct{}
	tuiResyncMsg    struct{}
	tuiConnMsg      struct {
		state string
		err   error
	}
	tuiActionResultMsg struct {
		text   string
		err    error
		reload bool
	}
	tuiRefreshDueMsg   struct{ seq int }
	tuiTickMsg         time.Time
	tuiEventsClosedMsg struct{}
)

func (m tuiModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	m.now = time.Now()
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height

	case tea.KeyPressMsg:
		return m.updateKey(msg)

	case tuiDataMsg:
		m.loading = false
		m.updated = msg.at
		if msg.err != nil {
			m.status, m.statusErr = msg.err.Error(), true
			return m, nil
		}
		m.data = msg.data
		m.loaded = true
		// A successful refresh is the end of whatever was wrong; keeping a
		// stale error under the table would make the next failure invisible.
		if m.statusErr {
			m.status, m.statusErr = "", false
		}
		// A list can shrink under the cursor.
		m.clampCursor()

	case tuiEventMsg:
		// The transport, not the message, says whether this arrived on the
		// stream or the poll; a long-polled event is still an event.
		cmds := []tea.Cmd{waitTUIEvent(m.events)}
		if tuiKindMatters(msg.kind) {
			if cmd := m.scheduleRefresh(); cmd != nil {
				cmds = append(cmds, cmd)
			}
		}
		return m, tea.Batch(cmds...)

	case tuiHeartbeatMsg:
		m.conn, m.connErr = "live", nil
		return m, waitTUIEvent(m.events)

	case tuiResyncMsg:
		cmds := []tea.Cmd{waitTUIEvent(m.events)}
		if !m.loading {
			var cmd tea.Cmd
			m, cmd = m.startLoad()
			cmds = append(cmds, cmd)
		}
		m.status, m.statusErr = "The event stream could not replay everything; the screen was reloaded.", false
		return m, tea.Batch(cmds...)

	case tuiConnMsg:
		m.conn, m.connErr = msg.state, msg.err
		return m, waitTUIEvent(m.events)

	case tuiEventsClosedMsg:
		m.conn = "closed"
		return m, nil

	case tuiRefreshDueMsg:
		if msg.seq != m.refreshSeq || m.loading {
			return m, nil
		}
		return m.startLoad()

	case tuiTickMsg:
		cmd := tea.Every(tuiRefreshEvery, func(t time.Time) tea.Msg { return tuiTickMsg(t) })
		if m.loading {
			return m, cmd
		}
		m, load := m.startLoad()
		return m, tea.Batch(cmd, load)

	case tuiActionResultMsg:
		if msg.err != nil {
			m.status, m.statusErr = msg.err.Error(), true
			return m, nil
		}
		m.status, m.statusErr = msg.text, false
		if msg.reload && !m.loading {
			return m.startLoad()
		}
	}
	return m, nil
}

// waitTUIEvent reads one message from the stream's channel. Re-issuing it after
// every message is what keeps exactly one reader attached without a second
// goroutine competing for the same channel.
// tuiKindMatters keeps an event that cannot change any tab from starting a
// reload. A busy fleet publishes an audit frame for every action and a webhook
// frame for every delivery; neither is on this screen. The fifteen-second
// refresh still covers anything this list gets wrong.
func tuiKindMatters(kind string) bool {
	switch kind {
	case "audit", "webhook.delivery",
		"kennel.updated", "kennel.deleted", "kennel.summary",
		"provider.updated", "provider.deleted", "machine.updated", "machine.deleted",
		"updates.updated", "transfer.updated":
		return false
	}
	return true
}

func waitTUIEvent(ch <-chan tea.Msg) tea.Cmd {
	if ch == nil {
		return nil
	}
	return func() tea.Msg {
		msg, ok := <-ch
		if !ok {
			return tuiEventsClosedMsg{}
		}
		return msg
	}
}

func (m *tuiModel) scheduleRefresh() tea.Cmd {
	m.refreshSeq++
	seq := m.refreshSeq
	return tea.Tick(tuiDebounce, func(time.Time) tea.Msg { return tuiRefreshDueMsg{seq: seq} })
}

func (m tuiModel) startLoad() (tuiModel, tea.Cmd) {
	m.loading = true
	return m, m.load()
}

func (m tuiModel) load() tea.Cmd {
	ctx, api, window := m.ctx, m.api, m.window
	return func() tea.Msg {
		data, err := fetchTUIData(ctx, api, window)
		return tuiDataMsg{data: data, at: time.Now(), err: err}
	}
}

// fetchTUIData is the whole read surface of the dashboard. It stops at the
// first refusal rather than showing a screen stitched together from half a
// fleet: a partial list that looks complete is worse than an error naming the
// call that failed.
func fetchTUIData(ctx context.Context, api tuiAPI, window string) (tuiData, error) {
	var d tuiData
	if _, err := api.get(ctx, "/meta", nil, &d.meta); err != nil {
		return d, err
	}
	if _, err := api.get(ctx, "/stats", url.Values{"window": {window}}, &d.stats); err != nil {
		return d, err
	}
	if _, err := api.get(ctx, "/problems", nil, &d.problems); err != nil {
		return d, err
	}
	d.problems.sanitise()

	var scaling listResponse[scalingItem]
	if _, err := api.get(ctx, "/scaling-events", url.Values{"limit": {"5"}}, &scaling); err != nil {
		return d, err
	}
	d.scaling = scaling.Items

	var pools listResponse[poolItem]
	if _, err := api.get(ctx, "/pools", nil, &pools); err != nil {
		return d, err
	}
	d.pools = pools.Items

	var runners listResponse[runnerItem]
	rq := url.Values{"limit": {strconv.Itoa(tuiPageLimit)}}
	if _, err := api.get(ctx, "/runners", rq, &runners); err != nil {
		return d, err
	}
	for i := range runners.Items {
		runners.Items[i].sanitise()
	}
	d.runners = runners.Items

	var hosts listResponse[hostItem]
	if _, err := api.get(ctx, "/hosts", nil, &hosts); err != nil {
		return d, err
	}
	d.hosts = hosts.Items

	var jobs listResponse[jobItem]
	jq := url.Values{"limit": {strconv.Itoa(tuiPageLimit)}}
	if _, err := api.get(ctx, "/jobs", jq, &jobs); err != nil {
		return d, err
	}
	for i := range jobs.Items {
		jobs.Items[i].sanitise()
	}
	d.jobs = jobs.Items

	return d, nil
}

// ---------------------------------------------------------------------------
// The event stream
// ---------------------------------------------------------------------------

// followTUIEvents keeps the screen current. It prefers the stream the web UI
// uses, and falls back to the same held long poll as `zoomies events recent`
// when a proxy will not carry a stream. A dropped stream is not retried: the
// fallback is the recovery, and the periodic full refresh repairs anything a
// lost frame would have carried.
func followTUIEvents(ctx context.Context, api *apiClient, out chan<- tea.Msg) {
	defer close(out)
	resp, err := api.stream(ctx, "/events", nil, "text/event-stream")
	if err == nil {
		pushTUI(out, tuiConnMsg{state: "live"})
		err = readSSE(resp.Body, func(f sseFrame) error {
			switch f.event {
			case "heartbeat":
				pushTUI(out, tuiHeartbeatMsg{})
			case "resync":
				pushTUI(out, tuiResyncMsg{})
			case "":
			default:
				pushTUI(out, tuiEventMsg{kind: f.event})
			}
			return nil
		})
		resp.Body.Close()
	}
	if ctx.Err() != nil {
		return
	}
	pushTUI(out, tuiConnMsg{state: "polling", err: err})
	pollTUIEvents(ctx, api, out)
}

// pollTUIEvents is the fallback: one held request at a time, with the cursor
// the answer carried. `now` for the first call means the dashboard starts from
// the present rather than replaying a buffer it has no use for.
func pollTUIEvents(ctx context.Context, api *apiClient, out chan<- tea.Msg) {
	since := "now"
	for ctx.Err() == nil {
		q := url.Values{}
		q.Set("since", since)
		q.Set("wait", "15s")
		q.Set("limit", strconv.Itoa(tuiPageLimit))
		var page recentEventsResponse
		if _, err := api.get(ctx, "/events/recent", q, &page); err != nil {
			if ctx.Err() != nil {
				return
			}
			pushTUI(out, tuiConnMsg{state: "polling", err: err})
			if !sleepCtx(ctx, 5*time.Second) {
				return
			}
			continue
		}
		if page.Next != "" {
			since = page.Next
		}
		for _, item := range page.Items {
			pushTUI(out, tuiEventMsg{kind: item.Kind})
		}
		if !page.Complete {
			pushTUI(out, tuiResyncMsg{})
		}
	}
}

// pushTUI never blocks. A full channel means the renderer is behind, and the
// next full refresh will repair whatever was dropped; blocking here would
// stall the stream until the operator closed the confirmation they are reading.
func pushTUI(out chan<- tea.Msg, msg tea.Msg) {
	select {
	case out <- msg:
	default:
	}
}

func sleepCtx(ctx context.Context, d time.Duration) bool {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-t.C:
		return true
	}
}

// ---------------------------------------------------------------------------
// Keys and actions
// ---------------------------------------------------------------------------

func (m tuiModel) updateKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	key := msg.String()

	if m.confirm != nil {
		switch key {
		case "ctrl+c":
			return m, tea.Quit
		case "y", "Y", "enter":
			action := m.confirm.action
			m.confirm = nil
			return m, m.runAction(action)
		case "n", "N", "esc", "q":
			m.confirm = nil
			m.status, m.statusErr = "Cancelled.", false
		}
		return m, nil
	}
	if m.help {
		if key == "ctrl+c" {
			return m, tea.Quit
		}
		m.help = false
		return m, nil
	}

	switch key {
	case "ctrl+c", "q":
		return m, tea.Quit
	case "?":
		m.help = true
	case "r":
		if m.loading {
			m.status, m.statusErr = "Already refreshing.", false
			return m, nil
		}
		return m.startLoad()
	case "tab", "right", "l":
		m.active = (m.active + 1) % tabCount
		m.clampCursor()
	case "shift+tab", "left", "h":
		m.active = (m.active + tabCount - 1) % tabCount
		m.clampCursor()
	case "1", "2", "3", "4", "5", "6":
		m.active = tuiTab(int(key[0] - '1'))
		m.clampCursor()
	case "up", "k":
		m.moveCursor(-1)
	case "down", "j":
		m.moveCursor(1)
	case "home", "g":
		if m.active == tabOverview {
			m.offset[tabOverview] = 0
		} else {
			m.cursor[m.active] = 0
		}
	case "end", "G":
		if m.active == tabOverview {
			// The view clamps this to the last screenful; the model cannot see
			// the terminal's height without repeating the renderer.
			m.offset[tabOverview] = len(m.overviewLines())
		} else {
			m.cursor[m.active] = max(m.itemCount(m.active)-1, 0)
		}
	case "enter", "a", "d":
		return m.askAction(key)
	}
	return m, nil
}

func (m *tuiModel) moveCursor(delta int) {
	if m.active == tabOverview {
		m.offset[tabOverview] = max(m.offset[tabOverview]+delta, 0)
		return
	}
	n := m.itemCount(m.active)
	if n == 0 {
		m.cursor[m.active] = 0
		return
	}
	m.cursor[m.active] = min(max(m.cursor[m.active]+delta, 0), n-1)
}

func (m *tuiModel) clampCursor() {
	n := m.itemCount(m.active)
	if n == 0 {
		m.cursor[m.active], m.offset[m.active] = 0, 0
		return
	}
	m.cursor[m.active] = min(max(m.cursor[m.active], 0), n-1)
}

func (m tuiModel) itemCount(tab tuiTab) int {
	switch tab {
	case tabProblems:
		return len(m.data.problems.Items)
	case tabPools:
		return len(m.data.pools)
	case tabRunners:
		return len(m.data.runners)
	case tabHosts:
		return len(m.data.hosts)
	case tabJobs:
		return len(m.data.jobs)
	}
	return 0
}

type tuiActionKind int

const (
	actApplyRemedy tuiActionKind = iota
	actDismissProblem
	actDrain
	actRerun
	actAcknowledge
)

type tuiAction struct {
	kind     tuiActionKind
	code     string
	target   string
	remedy   string
	severity string
	id       string
	label    string
}

type tuiConfirm struct {
	prompt string
	detail string
	action tuiAction
}

// askAction turns a key into the question it should ask. Every change goes
// through a confirmation, including the reversible ones: the modal is also
// where the consequence is spelled out, and a drain or a rerun that arrives
// without its consequence is the one an operator regrets.
func (m tuiModel) askAction(key string) (tea.Model, tea.Cmd) {
	switch m.active {
	case tabProblems:
		item, ok := m.selectedProblem()
		if !ok {
			return m, nil
		}
		if key == "d" {
			m.confirm = &tuiConfirm{
				prompt: fmt.Sprintf("Dismiss every %q problem for this account?", item.Code),
				detail: "The controller keeps dismissals per account, and a problem that becomes worse is shown again.",
				action: tuiAction{kind: actDismissProblem, code: item.Code, severity: severityOrInfo(item.Severity), label: item.Title},
			}
			return m, nil
		}
		if item.Remedy == nil {
			m.status, m.statusErr = fmt.Sprintf("%s has no proposed change; it needs a person rather than a button.", item.Code), false
			return m, nil
		}
		m.confirm = &tuiConfirm{
			prompt: fmt.Sprintf("Apply %q on %s?", item.Remedy.Label, dash(item.TargetID)),
			detail: dash(item.Remedy.Effect),
			action: tuiAction{kind: actApplyRemedy, code: item.Code, target: item.TargetID, remedy: item.Remedy.ID, label: item.Remedy.Label},
		}

	case tabRunners:
		if key == "a" {
			return m, nil
		}
		item, ok := m.selectedRunner()
		if !ok {
			return m, nil
		}
		m.confirm = &tuiConfirm{
			prompt: fmt.Sprintf("Drain %s?", dash(item.Name)),
			detail: "A job still running is given five minutes, then the runner stops.",
			action: tuiAction{kind: actDrain, id: item.ID, label: item.Name},
		}

	case tabJobs:
		item, ok := m.selectedJob()
		if !ok {
			return m, nil
		}
		if key == "a" {
			m.confirm = &tuiConfirm{
				prompt: fmt.Sprintf("Mark %s as known?", truncate(dash(item.JobName), 50)),
				detail: "The failure keeps its conclusion; a re-run that passes is what settles it.",
				action: tuiAction{kind: actAcknowledge, id: item.ID, label: item.JobName},
			}
			return m, nil
		}
		m.confirm = &tuiConfirm{
			prompt: fmt.Sprintf("Re-run the failed jobs of %s's run?", dash(item.Repo)),
			detail: "GitHub has no job-level rerun, so every failed job in the run is re-run, not only this one.",
			action: tuiAction{kind: actRerun, id: item.ID, label: item.JobName},
		}
	}
	return m, nil
}

func (m tuiModel) selectedProblem() (problemItem, bool) {
	item := m.cursor[tabProblems]
	items := m.data.problems.Items
	if item < 0 || item >= len(items) {
		return problemItem{}, false
	}
	return items[item], true
}

func (m tuiModel) selectedRunner() (runnerItem, bool) {
	item := m.cursor[tabRunners]
	items := m.data.runners
	if item < 0 || item >= len(items) {
		return runnerItem{}, false
	}
	return items[item], true
}

func (m tuiModel) selectedJob() (jobItem, bool) {
	item := m.cursor[tabJobs]
	items := m.data.jobs
	if item < 0 || item >= len(items) {
		return jobItem{}, false
	}
	return items[item], true
}

func severityOrInfo(s string) string {
	switch s {
	case "error", "warning", "info":
		return s
	}
	return "info"
}

func (m tuiModel) runAction(a tuiAction) tea.Cmd {
	ctx, api := m.ctx, m.api
	return func() tea.Msg {
		switch a.kind {
		case actApplyRemedy:
			var done struct {
				Applied bool `json:"applied"`
				Remedy  struct {
					Label string `json:"label"`
				} `json:"remedy"`
			}
			_, err := api.post(ctx, "/problems/apply", nil, map[string]any{
				"code": a.code, "target_id": a.target, "remedy_id": a.remedy,
			}, &done)
			if err != nil {
				return tuiActionResultMsg{err: err}
			}
			return tuiActionResultMsg{
				text:   fmt.Sprintf("Applied %q on %s; runners created from now on use it.", done.Remedy.Label, dash(a.target)),
				reload: true,
			}

		case actDismissProblem:
			body := map[string]any{
				"set": []map[string]any{{
					"key":      "type:" + a.code,
					"severity": severityOrInfo(a.severity),
				}},
			}
			if _, err := api.patch(ctx, "/auth/problem-dismissals", nil, body, nil); err != nil {
				return tuiActionResultMsg{err: err}
			}
			return tuiActionResultMsg{text: fmt.Sprintf("Dismissed %s for this account.", a.code), reload: true}

		case actDrain:
			q := url.Values{"confirm": {"true"}}
			if _, err := api.post(ctx, "/runners/"+url.PathEscape(a.id)+"/drain", q, nil, nil); err != nil {
				return tuiActionResultMsg{err: err}
			}
			return tuiActionResultMsg{
				text:   fmt.Sprintf("Draining %s; it exits once its current job finishes.", dash(a.label)),
				reload: true,
			}

		case actRerun:
			var out rerunResponse
			if _, err := api.post(ctx, "/jobs/"+url.PathEscape(a.id)+"/rerun", nil, nil, &out); err != nil {
				return tuiActionResultMsg{err: err}
			}
			return tuiActionResultMsg{
				text:   fmt.Sprintf("GitHub accepted the request; run %d will re-run its failed jobs.", out.RunID),
				reload: true,
			}

		case actAcknowledge:
			if _, err := api.post(ctx, "/jobs/"+url.PathEscape(a.id)+"/acknowledge", nil, nil, nil); err != nil {
				return tuiActionResultMsg{err: err}
			}
			return tuiActionResultMsg{
				text:   fmt.Sprintf("Marked %s as known; a re-run that passes is what settles it.", truncate(dash(a.label), 50)),
				reload: true,
			}
		}
		return tuiActionResultMsg{err: errors.New("unknown dashboard action")}
	}
}
