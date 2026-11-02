package main

import (
	"context"
	"fmt"
	"io"
	"slices"
	"strings"
	"sync"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/eyupio/zoomies/internal/terminalui"
)

// A command chosen from the launcher runs inside it. Its output lands in a
// scrollable pane instead of the operator's shell, so the launcher never drops
// back to the OS terminal between steps. The one exception is a command that
// has to ask a person something: see homeNeedsTerminal.

// homeNeedsTerminal reports whether a launcher choice must own the real
// terminal. Only the guided installer qualifies: its Huh form needs raw keys,
// and a captured pane cannot answer it. Everything else is output, and a
// prompt it would have raised is declined because the pane is not a terminal.
func homeNeedsTerminal(args []string) bool {
	return len(args) > 0 && args[0] == "init" && !slices.Contains(args, "--help")
}

// homeRelease hands the real terminal back for as long as the returned
// function has not been called. A command that replaces this process (the
// upgrade does, once a new release is verified) must not do it while Bubble
// Tea holds the screen in raw mode on the alternate screen.
type homeRelease func() (restore func())

type homeReleaseKey struct{}

// releaseHomeTerminal is a no-op outside the launcher.
func releaseHomeTerminal(ctx context.Context) (restore func()) {
	if release, ok := ctx.Value(homeReleaseKey{}).(homeRelease); ok && release != nil {
		return release()
	}
	return func() {}
}

// paneOutput collects what a command writes. It implements TerminalColour so
// the installer's palette keeps its colours even though this is not a file.
type paneOutput struct {
	mu     sync.Mutex
	text   strings.Builder
	colour bool
}

func (p *paneOutput) Write(b []byte) (int, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.text.Write(b)
}

func (p *paneOutput) TerminalColour() bool { return p.colour }

// lines wraps the output to the pane. A carriage return rewrites the line, so
// a progress meter shows its last state rather than every frame it drew.
func (p *paneOutput) lines(width int) []string {
	p.mu.Lock()
	raw := p.text.String()
	p.mu.Unlock()
	raw = strings.TrimRight(raw, "\n")
	if raw == "" {
		return nil
	}
	var out []string
	for _, line := range strings.Split(raw, "\n") {
		if i := strings.LastIndex(strings.TrimRight(line, "\r"), "\r"); i >= 0 {
			line = line[i+1:]
		}
		line = strings.ReplaceAll(strings.TrimRight(line, "\r"), "\t", "    ")
		out = append(out, strings.Split(ansi.Hardwrap(line, max(width, 1), true), "\n")...)
	}
	return out
}

type homeRun struct {
	args         []string
	out          *paneOutput
	cancel       context.CancelFunc
	cancelling   bool
	done         bool
	code         int
	offset       int
	follow       bool
	continuation bool
}

type homeRunDone struct {
	run  *homeRun
	code int
}

type homeRunRefresh struct{}

func newHomeRun(args []string, colour bool) *homeRun {
	return &homeRun{args: args, out: &paneOutput{colour: colour}, follow: true}
}

// start runs the command on its own goroutine through Bubble Tea's command
// mechanism. Its input is empty, so a prompt reads end of file and takes the
// non-interactive answer instead of waiting for keys nobody can send.
func (r *homeRun) start(ctx context.Context, release homeRelease) tea.Cmd {
	ctx, r.cancel = context.WithCancel(ctx)
	ctx = context.WithValue(ctx, homeInvocationKey{}, true)
	if release != nil {
		ctx = context.WithValue(ctx, homeReleaseKey{}, release)
	}
	e := &env{out: r.out, err: r.out, in: strings.NewReader("")}
	run := tea.Cmd(func() tea.Msg {
		code := dispatch(ctx, e, r.args)
		if r.continuation {
			clearUpgradeContinuation()
		}
		return homeRunDone{run: r, code: code}
	})
	return tea.Batch(run, homeRefresh())
}

func homeRefresh() tea.Cmd {
	return tea.Tick(100*time.Millisecond, func(_ time.Time) tea.Msg { return homeRunRefresh{} })
}

// homePaneRows is the rows left for output once the header and footer are drawn.
func homePaneRows(h int) int { return max(h-5, 1) }

func (r *homeRun) scroll(key string, total, rows int) {
	last := max(total-rows, 0)
	if r.follow {
		r.offset = last
	}
	switch key {
	case "up", "k":
		r.offset, r.follow = max(r.offset-1, 0), false
	case "down", "j":
		r.offset = min(r.offset+1, last)
		r.follow = r.offset == last
	case "pgup", "b":
		r.offset, r.follow = max(r.offset-rows, 0), false
	case "pgdown", "space", " ", "f":
		r.offset = min(r.offset+rows, last)
		r.follow = r.offset == last
	case "home", "g":
		r.offset, r.follow = 0, false
	case "end", "G":
		r.offset, r.follow = last, true
	}
}

func (m homeModel) updateRun(msg tea.Msg) (tea.Model, tea.Cmd) {
	r := m.run
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
	case homeRunRefresh:
		if !r.done {
			return m, homeRefresh()
		}
	case homeRunDone:
		if msg.run == r {
			r.done, r.code = true, msg.code
		}
	case tea.KeyPressMsg:
		key := msg.String()
		switch {
		case key == "ctrl+c" && !r.done && !r.cancelling:
			// The first press asks the command to stop. A command that
			// ignores its context must still be escapable, hence the second.
			r.cancelling = true
			r.cancel()
		case key == "ctrl+c":
			m.choice = nil
			return m, tea.Quit
		case r.done && key == "q":
			m.choice = nil
			return m, tea.Quit
		case r.done && (key == "enter" || key == "esc"):
			m.run = nil
		default:
			rows := homePaneRows(max(m.height, 12))
			r.scroll(key, len(r.out.lines(m.paneWidth())), rows)
		}
	}
	return m, nil
}

func (m homeModel) paneWidth() int {
	w := m.width
	if w <= 0 {
		w = 80
	}
	return max(w-4, 1)
}

func (m homeModel) runView() tea.View {
	r := m.run
	w, h := m.width, m.height
	if w <= 0 {
		w = 80
	}
	if h <= 0 {
		h = 24
	}
	rows := homePaneRows(h)
	all := r.out.lines(m.paneWidth())
	last := max(len(all)-rows, 0)
	offset := min(r.offset, last)
	if r.follow {
		offset = last
	}
	visible := all[offset:min(offset+rows, len(all))]

	status := m.paint("36", "Running")
	switch {
	case r.done && r.code == 0:
		status = m.paint("32", "Finished")
	case r.done:
		status = m.paint("31", fmt.Sprintf("Exit code %d, review the output", r.code))
	case r.cancelling:
		status = m.paint("33", "Stopping")
	}
	lines := []string{
		m.paint("1", "zoomies "+strings.Join(r.args, " ")) + "  " + status,
		"",
	}
	for _, l := range visible {
		lines = append(lines, " "+l)
	}
	for len(lines) < rows+2 {
		lines = append(lines, "")
	}
	hint := "↑↓ scroll   ctrl+c stop"
	if r.cancelling {
		hint = "waiting for the command to stop   ctrl+c again quits"
	}
	if r.done {
		hint = "↑↓ scroll   enter home   q quit"
	}
	if len(all) > rows {
		hint = fmt.Sprintf("%d-%d of %d   %s", offset+1, offset+len(visible), len(all), hint)
	}
	lines = append(lines, "", m.paint("2", hint))
	v := tea.NewView(terminalui.Frame(lines, w, h))
	v.AltScreen, v.WindowTitle = true, "Zoomies"
	return v
}

var _ io.Writer = (*paneOutput)(nil)
