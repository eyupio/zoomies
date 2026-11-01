package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/eyupio/zoomies/internal/terminalui"
)

type homeItem struct {
	title, detail, group string
	args                 []string
	confirm              bool
}

func homeItems() []homeItem {
	return []homeItem{
		{"Your fleet", "Check status, inspect jobs and runners, and review problems using your controller connection.", "OBSERVE", []string{"@fleet"}, false},
		{"Host health", "Inspect this machine and read recommendations. This check never changes host settings.", "OBSERVE", []string{"doctor"}, false},
		{"Preview an upgrade", "Download, verify and check the next release. Review the deployment changes before choosing to upgrade.", "MAINTAIN", []string{"upgrade", "--dry-run"}, false},
		{"Upgrade this host", "Back up the database, verify the release, update the deployment and check health. Existing deployment approval questions still apply.", "MAINTAIN", []string{"upgrade"}, true},
		{"Set up this host", "Open the guided installer to configure a controller or runner host. You will review the installation before it is applied.", "GET STARTED", []string{"init"}, true},
		{"Explore commands", "Search every command and open its help. Find fleet operations, backups, configuration and more.", "REFERENCE", nil, false},
	}
}

// The launcher releases the terminal before another command starts, so Huh
// forms and password prompts never compete for input.
func runHome(ctx context.Context, e *env) error {
	m := homeModel{width: 80, height: 24, colour: terminalui.Colour(), motion: terminalui.Motion()}
	for ctx.Err() == nil {
		result, err := tea.NewProgram(m, tea.WithContext(ctx), tea.WithInput(e.in), tea.WithOutput(e.out), tea.WithFPS(30)).Run()
		if err != nil {
			if ctx.Err() != nil || errors.Is(err, tea.ErrProgramKilled) {
				return nil
			}
			return err
		}
		m = result.(homeModel)
		if m.choice == nil {
			return nil
		}
		args := m.choice
		m.choice = nil
		code := dispatch(context.WithValue(ctx, homeInvocationKey{}, true), e, args)
		if ctx.Err() != nil {
			return nil
		}
		// Keep the transcript visible until the operator chooses to return.
		quit, err := pauseHome(ctx, e, code)
		if err != nil {
			return err
		}
		if quit {
			return nil
		}
	}
	return nil
}

type homeModel struct {
	width, height, cursor, frame                            int
	colour, motion, catalogue, fleet, searching, confirming bool
	query                                                   string
	choice                                                  []string
}

func (m homeModel) items() []homeItem {
	if m.fleet {
		return fleetHomeItems()
	}
	if !m.catalogue {
		return homeItems()
	}
	var items []homeItem
	for _, c := range commands() {
		if m.query != "" && !strings.Contains(strings.ToLower(c.name+" "+c.brief), strings.ToLower(m.query)) {
			continue
		}
		items = append(items, homeItem{title: c.name, detail: c.brief, group: c.group, args: []string{c.name, "--help"}})
	}
	return items
}

func (m homeModel) Init() tea.Cmd {
	if m.motion {
		return terminalui.Tick()
	}
	return nil
}

func (m homeModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
	case terminalui.TickMsg:
		if m.motion {
			if !m.confirming {
				m.frame++
			}
			return m, terminalui.Tick()
		}
	case tea.KeyPressMsg:
		key := msg.String()
		if key == "ctrl+c" {
			m.choice = nil
			return m, tea.Quit
		}
		if (m.width > 0 && m.width < 32) || (m.height > 0 && m.height < 12) {
			if key == "q" || key == "esc" {
				return m, tea.Quit
			}
			return m, nil
		}
		if m.confirming {
			switch key {
			case "y", "Y":
				m.confirming = false
				m.choice = m.items()[m.cursor].args
				return m, tea.Quit
			case "n", "N", "esc", "q", "enter":
				m.confirming = false
			}
			return m, nil
		}
		if m.searching {
			switch key {
			case "esc":
				m.searching = false
				m.query = ""
			case "enter", "down":
				m.searching = false
			case "backspace":
				r := []rune(m.query)
				if len(r) > 0 {
					m.query = string(r[:len(r)-1])
				}
			default:
				if msg.Text != "" {
					m.query += plainCell(msg.Text)
				}
			}
			m.cursor = 0
			return m, nil
		}
		items := m.items()
		switch key {
		case "q":
			m.choice = nil
			return m, tea.Quit
		case "esc":
			if m.catalogue || m.fleet {
				m.catalogue, m.fleet = false, false
				m.query = ""
				m.cursor = 0
			} else {
				return m, tea.Quit
			}
		case "/":
			m.catalogue, m.searching, m.fleet, m.cursor = true, true, false, 0
		case "up", "k", "shift+tab":
			m.cursor = max(0, m.cursor-1)
		case "down", "j", "tab":
			m.cursor = min(max(len(items)-1, 0), m.cursor+1)
		case "home", "g":
			m.cursor = 0
		case "end", "G":
			m.cursor = max(len(items)-1, 0)
		case "pgdown":
			m.cursor = min(max(len(items)-1, 0), m.cursor+max(m.height-10, 1))
		case "pgup":
			m.cursor = max(0, m.cursor-max(m.height-10, 1))
		case "enter":
			if len(items) == 0 {
				return m, nil
			}
			item := items[m.cursor]
			if len(item.args) > 0 && item.args[0] == "@fleet" {
				m.fleet, m.cursor = true, 0
				return m, nil
			}
			if item.args == nil {
				m.catalogue, m.cursor = true, 0
				return m, nil
			}
			if item.confirm {
				m.confirming = true
				return m, nil
			}
			m.choice = item.args
			return m, tea.Quit
		}
	}
	return m, nil
}

func (m homeModel) paint(code, text string) string { return terminalui.Paint(m.colour, code, text) }

type homePause struct {
	code         int
	colour, quit bool
}

func (m homePause) Init() tea.Cmd { return nil }
func (m homePause) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if key, ok := msg.(tea.KeyPressMsg); ok {
		switch key.String() {
		case "q", "ctrl+c":
			m.quit = true
			return m, tea.Quit
		case "enter", "esc":
			return m, tea.Quit
		}
	}
	return m, nil
}
func (m homePause) View() tea.View {
	status := "Command finished"
	if m.code != 0 {
		status = fmt.Sprintf("Command finished with exit code %d; review the output above", m.code)
	}
	return tea.NewView("\n" + terminalui.Paint(m.colour, "2", status+". Enter home · q quit") + "\n")
}

func homeAvailable(e *env) bool {
	return terminalReader(e.in) && terminalWriter(e.out) && os.Getenv("TERM") != "dumb" && os.Getenv("CI") == ""
}

// These are read-only entry points. Changes stay in the commands that explain
// their flags and require the operator to name their target explicitly.
func fleetHomeItems() []homeItem {
	return []homeItem{
		{"Overview", "Job outcomes, queue waits, runner capacity and recent scaling.", "FLEET", []string{"status"}, false},
		{"Problems", "What needs attention and the remedies the controller proposes.", "FLEET", []string{"problems", "list"}, false},
		{"Runners", "Inspect the runners that exist right now and their current jobs.", "FLEET", []string{"runners", "list"}, false},
		{"Jobs", "Read recent jobs, their outcomes and time spent waiting.", "FLEET", []string{"jobs", "list"}, false},
		{"Pools", "Review runner pools, limits and utilisation.", "FLEET", []string{"pools", "list"}, false},
		{"Hosts", "Inspect enrolled agents, their health and capacity.", "FLEET", []string{"hosts", "list"}, false},
		{"Recent events", "Read recent fleet activity.", "FLEET", []string{"events", "recent"}, false},
	}
}

type homeInvocationKey struct{}

func pauseHome(ctx context.Context, e *env, code int) (bool, error) {
	p := homePause{code: code, colour: terminalui.Colour()}
	result, err := tea.NewProgram(p, tea.WithContext(ctx), tea.WithInput(e.in), tea.WithOutput(e.out)).Run()
	if ctx.Err() != nil {
		return true, nil
	}
	if err != nil {
		return true, err
	}
	return result.(homePause).quit, nil
}

// A successful binary replacement resumes the selected command in the new
// release. Afterwards it can return to the launcher just like a local task.
func dispatchWithHome(ctx context.Context, e *env, args []string) int {
	returnHome := os.Getenv("ZOOMIES_RETURN_HOME") == "1"
	if returnHome {
		_ = os.Unsetenv("ZOOMIES_RETURN_HOME")
	}
	code := dispatch(ctx, e, args)
	if returnHome {
		clearUpgradeContinuation()
	}
	if !returnHome || !homeAvailable(e) || ctx.Err() != nil {
		return code
	}
	quit, err := pauseHome(ctx, e, code)
	if err != nil {
		return report(e, "", err)
	}
	if quit {
		return code
	}
	return report(e, "", runHome(ctx, e))
}
