package main

import (
	"context"
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

// runPane drives a launcher choice to completion the way Bubble Tea would: it
// runs the returned command and feeds the result back as a message.
func runPane(t *testing.T, m homeModel, args []string) homeModel {
	t.Helper()
	next, cmd := m.choose(args)
	m = next.(homeModel)
	if m.run == nil || cmd == nil {
		t.Fatalf("%v did not open a pane", args)
	}
	for _, msg := range drain(cmd) {
		if done, ok := msg.(homeRunDone); ok {
			next, _ = m.Update(done)
			m = next.(homeModel)
		}
	}
	return m
}

func drain(cmd tea.Cmd) []tea.Msg {
	var out []tea.Msg
	if cmd == nil {
		return out
	}
	switch msg := cmd().(type) {
	case tea.BatchMsg:
		for _, c := range msg {
			out = append(out, drain(c)...)
		}
	default:
		out = append(out, msg)
	}
	return out
}

func testPaneModel(t *testing.T) homeModel {
	e, _, _ := newTestEnv(t)
	return homeModel{width: 80, height: 24, ctx: context.Background(), env: e}
}

func TestACommandChosenFromHomeRunsInsideThePaneAndKeepsItsOutput(t *testing.T) {
	m := runPane(t, testPaneModel(t), []string{"upgrade", "--help"})
	if !m.run.done || m.run.code != 0 {
		t.Fatalf("done=%v code=%d", m.run.done, m.run.code)
	}
	if got := strings.Join(m.run.out.lines(80), "\n"); !strings.Contains(got, "upgrade") {
		t.Fatalf("the command's output never reached the pane: %q", got)
	}
	if !strings.Contains(ansi.Strip(fmt.Sprint(m.View().Content)), "Finished") {
		t.Fatal("the pane does not say the command finished")
	}
}

func TestThePaneShowsAFailingExitCodeAndEnterReturnsHome(t *testing.T) {
	m := runPane(t, testPaneModel(t), []string{"no-such-command"})
	if m.run.code == 0 {
		t.Fatal("an unknown command exited 0")
	}
	m, cmd := homeKey(t, m, "enter")
	if m.run != nil || cmd != nil {
		t.Fatal("enter on a finished pane must go home")
	}
}

func TestOnlyTheGuidedInstallerTakesTheRealTerminal(t *testing.T) {
	for _, tc := range []struct {
		args []string
		want bool
	}{
		{[]string{"init"}, true},
		{[]string{"init", "--help"}, false},
		{[]string{"upgrade"}, false},
		{[]string{"doctor"}, false},
	} {
		if got := homeNeedsTerminal(tc.args); got != tc.want {
			t.Errorf("%v: %v, want %v", tc.args, got, tc.want)
		}
	}
	m, cmd := testPaneModel(t).choose([]string{"init"})
	if hm := m.(homeModel); hm.run != nil || hm.choice == nil || cmd == nil {
		t.Fatal("init must quit the launcher so its form owns the terminal")
	}
}

func TestPaneOutputRewritesProgressLinesAndWrapsLongOnes(t *testing.T) {
	p := &paneOutput{}
	p.Write([]byte("10%\r50%\rdone\nabcdefghij\n"))
	got := p.lines(4)
	want := []string{"done", "abcd", "efgh", "ij"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("lines = %q, want %q", got, want)
	}
}

func TestCtrlCStopsARunningCommandBeforeItQuitsTheLauncher(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	m := testPaneModel(t)
	m.run = newHomeRun([]string{"doctor"}, false)
	m.run.cancel = cancel
	m, cmd := homeKey(t, m, "ctrl+c")
	if cmd != nil || !m.run.cancelling || ctx.Err() == nil {
		t.Fatal("the first ctrl+c must cancel the command, not quit")
	}
	if _, cmd = homeKey(t, m, "ctrl+c"); cmd == nil {
		t.Fatal("a second ctrl+c must always get out")
	}
}
