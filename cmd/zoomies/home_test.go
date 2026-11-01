package main

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/eyupio/zoomies/internal/installer"
	"github.com/eyupio/zoomies/internal/terminalui"
)

func homeKey(t *testing.T, m homeModel, key string) (homeModel, tea.Cmd) {
	t.Helper()
	next, cmd := m.Update(keyMsg(key))
	return next.(homeModel), cmd
}

func TestHomeOpensFleetCommandsWithoutStartingADashboard(t *testing.T) {
	m, cmd := homeKey(t, homeModel{width: 80, height: 24}, "enter")
	if !m.fleet || m.choice != nil || cmd != nil {
		t.Fatal("fleet entry must open the task menu")
	}
	m, cmd = homeKey(t, m, "enter")
	if !reflect.DeepEqual(m.choice, []string{"status"}) || cmd == nil {
		t.Fatalf("choice = %v", m.choice)
	}
	for _, item := range append(homeItems(), fleetHomeItems()...) {
		if len(item.args) > 0 && item.args[0] == "tui" {
			t.Fatal("home must offer CLI tasks")
		}
	}
}

func TestHomeRequiresExplicitApprovalForAnUpgradeAndEnterCancels(t *testing.T) {
	m := homeModel{cursor: 3, width: 80, height: 24}
	m, cmd := homeKey(t, m, "enter")
	if !m.confirming || m.choice != nil || cmd != nil {
		t.Fatal("upgrade ran before confirmation")
	}
	m, _ = homeKey(t, m, "enter")
	if m.confirming || m.choice != nil {
		t.Fatal("enter must take the safe default")
	}
	m, _ = homeKey(t, m, "enter")
	m, cmd = homeKey(t, m, "y")
	if cmd == nil || !reflect.DeepEqual(m.choice, []string{"upgrade"}) {
		t.Fatalf("approved choice = %v", m.choice)
	}
}

func TestHomeSearchUsesTheCommandRegistryAndRecoversFromNoMatches(t *testing.T) {
	m, _ := homeKey(t, homeModel{}, "/")
	for _, key := range []string{"d", "o", "c", "t", "o", "r"} {
		m, _ = homeKey(t, m, key)
	}
	if len(m.items()) != 1 || m.items()[0].title != "doctor" {
		t.Fatalf("search = %#v", m.items())
	}
	m, _ = homeKey(t, m, "enter")
	m, cmd := homeKey(t, m, "enter")
	if cmd == nil || !reflect.DeepEqual(m.choice, []string{"doctor", "--help"}) {
		t.Fatal("reference must open help, not execute")
	}
	m = homeModel{catalogue: true, query: "does-not-exist"}
	m, cmd = homeKey(t, m, "enter")
	if m.choice != nil || cmd != nil {
		t.Fatal("empty search selected a command")
	}
	m, _ = homeKey(t, m, "esc")
	if m.catalogue || m.query != "" {
		t.Fatal("escape must return home")
	}
}

func TestHomeFitsSmallAndWideTerminalsWithoutLosingTheSelectedTask(t *testing.T) {
	for _, size := range [][2]int{{20, 8}, {32, 12}, {60, 18}, {80, 24}, {140, 40}} {
		for _, catalogue := range []bool{false, true} {
			m := homeModel{width: size[0], height: size[1], catalogue: catalogue}
			m.cursor = len(m.items()) - 1
			view := m.View().Content
			lines := strings.Split(ansi.Strip(view), "\n")
			if len(lines) != size[1] {
				t.Fatalf("%v: got %d rows", size, len(lines))
			}
			for _, line := range lines {
				if terminalui.Width(line) > size[0] {
					t.Fatalf("%v: wide row %q", size, line)
				}
			}
			if size[0] >= 32 && size[1] >= 12 && !strings.Contains(view, m.items()[m.cursor].title) {
				t.Fatalf("%v: selected task missing", size)
			}
		}
	}
	m := homeModel{width: 20, height: 8}
	m, cmd := homeKey(t, m, "enter")
	if m.choice != nil || cmd != nil {
		t.Fatal("a hidden selection must not run")
	}
}

func TestFleetMenuEntriesResolveToWorkingCommandHelp(t *testing.T) {
	for _, item := range fleetHomeItems() {
		t.Run(item.title, func(t *testing.T) {
			e, _, errs := newTestEnv(t)
			args := append(append([]string{}, item.args...), "--help")
			if code := dispatch(context.Background(), e, args); code != exitOK {
				t.Fatalf("%v: %d %s", args, code, errs.String())
			}
		})
	}
}

func TestTopLevelDoctorAndUpgradeAliasesKeepTheirFlags(t *testing.T) {
	for _, name := range []string{"doctor", "upgrade"} {
		e, out, _ := newTestEnv(t)
		if code := dispatch(context.Background(), e, []string{name, "--help"}); code != 0 {
			t.Fatal(code)
		}
		want := out.String()
		e, out, _ = newTestEnv(t)
		if code := dispatch(context.Background(), e, []string{"--" + name, "--help"}); code != 0 || out.String() != want {
			t.Fatalf("%s alias changed help or flags", name)
		}
	}
}

func TestAnUpgradeFromHomeResumesTheSelectedCommandAfterReexec(t *testing.T) {
	e, _, errs := newTestEnv(t)
	candidateRelease(t)
	binary := filepath.Join(t.TempDir(), "zoomies")
	if err := os.WriteFile(binary, []byte("previous release"), 0755); err != nil {
		t.Fatal(err)
	}
	stubUpgradeSeams(t, binary, nil)
	oldUpgrade := runUpgradeInstall
	t.Cleanup(func() { runUpgradeInstall = oldUpgrade })
	runUpgradeInstall = func(context.Context, installer.UpgradeOptions) error { return nil }
	var continued, continuedEnv []string
	reexecBinary = func(_ string, args, env []string) error { continued, continuedEnv = args, env; return nil }
	args := []string{"upgrade", "--version", "v9.9.9", "--installed-binary", binary, "--no-backup", "--no-animation"}
	if code := dispatch(context.WithValue(context.Background(), homeInvocationKey{}, true), e, args); code != 0 {
		t.Fatalf("upgrade exited %d: %s", code, errs.String())
	}
	if !reflect.DeepEqual(continued, args) {
		t.Fatalf("continued with %q, want %q", continued, args)
	}
	if !slices.Contains(continuedEnv, "ZOOMIES_RETURN_HOME=1") {
		t.Fatal("the new release will not return home")
	}
}

func TestHomeAnimationCanBeDisabledWithoutDisablingNavigation(t *testing.T) {
	m := homeModel{motion: false}
	if m.Init() != nil {
		t.Fatal("disabled motion scheduled a tick")
	}
	m, cmd := homeKey(t, m, "j")
	if m.cursor != 1 || cmd != nil {
		t.Fatal("navigation depends on animation")
	}
}

func keyMsg(s string) tea.KeyPressMsg {
	switch s {
	case "enter":
		return tea.KeyPressMsg{Code: tea.KeyEnter}
	case "tab":
		return tea.KeyPressMsg{Code: tea.KeyTab}
	case "esc":
		return tea.KeyPressMsg{Code: tea.KeyEsc}
	}
	return tea.KeyPressMsg{Code: []rune(s)[0], Text: s}
}

func TestTheStandaloneFleetDashboardIsNoLongerACommand(t *testing.T) {
	e, _, errs := newTestEnv(t)
	if code := dispatch(context.Background(), e, []string{"tui"}); code != exitUsage || !strings.Contains(errs.String(), "not a zoomies command") {
		t.Fatal("the removed dashboard is still registered")
	}
}

func TestAnUpgradeContinuationClearsItsMarkersBeforeAnotherMenuTask(t *testing.T) {
	e, _, errs := newTestEnv(t)
	t.Setenv("ZOOMIES_RETURN_HOME", "1")
	t.Setenv("ZOOMIES_UPGRADE_STARTED", "1")
	t.Setenv(installer.SelfUpdateEnv, "1")
	if code := dispatchWithHome(context.Background(), e, []string{"version", "--short"}); code != 0 {
		t.Fatal(errs.String())
	}
	for _, key := range []string{"ZOOMIES_RETURN_HOME", "ZOOMIES_UPGRADE_STARTED", installer.SelfUpdateEnv} {
		if os.Getenv(key) != "" {
			t.Fatalf("%s would skip part of the next upgrade", key)
		}
	}
}

func TestThePawIndicatorPausesDuringApprovalAndResumesAfterCancellation(t *testing.T) {
	m := homeModel{width: 80, height: 24, motion: true, cursor: 3}
	m, _ = homeKey(t, m, "enter")
	before := m.frame
	if !strings.Contains(m.View().Content, "y continue") {
		t.Fatal("approval keys must stay visible")
	}
	next, cmd := m.Update(terminalui.TickMsg{})
	m = next.(homeModel)
	if m.frame != before || cmd == nil {
		t.Fatal("approval advanced the animation or lost its scheduler")
	}
	m, _ = homeKey(t, m, "n")
	next, _ = m.Update(terminalui.TickMsg{})
	if next.(homeModel).frame == before {
		t.Fatal("the indicator did not resume")
	}
}

func TestHomeShowsTheTaglineOnlyOnceAcrossTerminalSizes(t *testing.T) {
	for _, size := range [][2]int{{32, 12}, {60, 18}, {80, 24}, {100, 28}, {140, 40}} {
		m := homeModel{width: size[0], height: size[1]}
		view := strings.ToLower(m.View().Content)
		if n := strings.Count(view, "off the lead"); n > 1 {
			t.Fatalf("%v: tagline repeated %d times", size, n)
		}
		for _, old := range []string{"squirrel", "cat has", "room to roam", "waiting for your say-so"} {
			if strings.Contains(view, old) {
				t.Fatalf("%v: old animation copy %q", size, old)
			}
		}
	}
}
