package terminalui

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"golang.org/x/term"
)

// Task owns the output only while work that cannot prompt is running. The
// caller gets its real error and every output byte back before it can prompt,
// start another terminal program or replace this process with a new release.
func Task(ctx context.Context, out io.Writer, label string, disabled bool, work func(context.Context, io.Writer) error) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	f, ok := out.(*os.File)
	if disabled || !Motion() || !ok || !term.IsTerminal(int(f.Fd())) {
		return work(ctx, out)
	}
	return runTask(ctx, out, label, work, func(m taskModel) error {
		_, err := tea.NewProgram(m, tea.WithContext(ctx), tea.WithInput(nil), tea.WithOutput(out), tea.WithFPS(30)).Run()
		return err
	})
}

type taskResult struct{ err error }

type taskOutput struct {
	sync.Mutex
	// Embedding would expose WriteString and ReadFrom without the lock when
	// io.WriteString or io.Copy receives the log writer.
	buffer bytes.Buffer
	colour bool
}

func (b *taskOutput) Write(p []byte) (int, error) {
	b.Lock()
	defer b.Unlock()
	return b.buffer.Write(p)
}

func (b *taskOutput) latest() string {
	b.Lock()
	defer b.Unlock()
	s := strings.TrimSpace(ansi.Strip(b.buffer.String()))
	if i := strings.LastIndexAny(s, "\r\n"); i >= 0 {
		s = s[i+1:]
	}
	return s
}

func runTask(ctx context.Context, out io.Writer, label string, work func(context.Context, io.Writer) error, render func(taskModel) error) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	logs := taskOutput{colour: Colour()}
	done := make(chan taskResult, 1)
	finished := make(chan struct{})
	var result taskResult
	start := time.Now()
	// Work starts once. A renderer failure must never retry a backup or swap.
	go func() {
		result.err = work(ctx, &logs)
		done <- result
		close(finished)
	}()
	uiErr := render(taskModel{label: label, start: start, now: start, done: done, logs: &logs, colour: Colour(), width: 80})
	if uiErr != nil {
		cancel()
	}
	// Joining also preserves the operation's rollback and cleanup on Ctrl-C.
	<-finished
	_, writeErr := out.Write(logs.buffer.Bytes())
	return errors.Join(result.err, uiErr, writeErr)
}

type taskModel struct {
	label            string
	start, now       time.Time
	done             <-chan taskResult
	logs             *taskOutput
	frame, width     int
	colour, finished bool
}

func (m taskModel) Init() tea.Cmd {
	return tea.Batch(Tick(), func() tea.Msg { return <-m.done })
}

func (m taskModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
	case TickMsg:
		m.frame++
		m.now = time.Time(msg)
		return m, Tick()
	case taskResult:
		m.finished = true
		return m, tea.Quit
	}
	return m, nil
}

func (m taskModel) View() tea.View {
	if m.finished {
		return tea.NewView("")
	}
	elapsed := m.now.Sub(m.start).Truncate(time.Second)
	elapsedText := Paint(m.colour, "2", elapsed.String())
	status := fmt.Sprintf(" %s  %s", Paws(m.frame, m.colour, true), Paint(m.colour, "1", m.label))
	line := Pad(status, max(m.width-Width(elapsedText)-1, 0)) + " " + elapsedText
	text := Fit(line, m.width)
	if last := m.logs.latest(); last != "" {
		text += "\n " + Paint(m.colour, "2", Fit(last, max(m.width-2, 1)))
	}
	lines := strings.Split(text, "\n")
	for i := range lines {
		lines[i] = Fit(lines[i], m.width)
	}
	return tea.NewView(strings.Join(lines, "\n"))
}

// Palette-aware writers retain styling in the transcript even while the
// renderer owns the terminal. A redirected task never uses this writer.
func (b *taskOutput) TerminalColour() bool { return b.colour }
