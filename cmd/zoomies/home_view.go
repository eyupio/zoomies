package main

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/lipgloss"
	"github.com/eyupio/zoomies/internal/terminalui"
	"github.com/eyupio/zoomies/internal/version"
)

func (m homeModel) View() tea.View {
	w, h := m.width, m.height
	if w <= 0 {
		w = 80
	}
	if h <= 0 {
		h = 24
	}
	if w < 32 || h < 12 {
		v := tea.NewView(terminalui.Frame([]string{" zoomies", " Enlarge to 32 columns × 12 rows.", " q quit"}, w, h))
		v.AltScreen = true
		return v
	}
	width := min(w-4, 112)
	margin := strings.Repeat(" ", 2+(w-4-width)/2)
	lines := terminalui.Banner(width, h, m.colour, version.Short())
	if len(lines) > 1 {
		lines = append(lines, m.paint("2", "Off the lead. On the job."))
	}

	title := "What would you like to do?"
	if m.fleet {
		title = "Your fleet"
	}
	if m.catalogue {
		title = "Command reference  /  "
		if m.query == "" && !m.searching {
			title += "press / to search"
		} else {
			title += m.query
		}
		if m.searching {
			title += "▏"
		}
	}
	lines = append(lines, m.paint("1", title))
	bodyHeight := min(h-len(lines)-3, 11)
	items := m.items()
	body := ""
	if m.confirming {
		body = m.confirmCard(items[m.cursor], width, bodyHeight)
	} else if width >= 72 {
		menuWidth := 29
		menu := terminalui.Panel(m.menuRows(items, menuWidth-4, bodyHeight-2), menuWidth, bodyHeight, m.colour, true)
		detail := terminalui.Panel(m.detailRows(items, width-menuWidth-5), width-menuWidth-1, bodyHeight, m.colour, false)
		body = lipgloss.JoinHorizontal(lipgloss.Top, menu, " ", detail)
	} else {
		body = terminalui.Panel(m.menuRows(items, width-4, bodyHeight-2), width, bodyHeight, m.colour, true)
	}
	lines = append(lines, strings.Split(body, "\n")...)

	preview := "Ready"
	if len(items) > 0 && !m.confirming {
		item := items[m.cursor]
		if item.args != nil && item.args[0] != "@fleet" {
			preview = "zoomies " + strings.Join(item.args, " ")
		}
		if m.catalogue {
			preview = fmt.Sprintf("%d/%d  ·  %s", m.cursor+1, len(items), preview)
		}
	}
	if m.confirming {
		preview = "y continue   enter / n cancel"
	}
	footerPreview := terminalui.Paws(m.frame, m.colour, m.motion) + "  " + m.paint("36", preview)
	if m.confirming {
		footerPreview = m.paint("36", preview)
	}
	lines = append(lines, "", footerPreview, m.paint("2", "↑↓ / j k move   enter open   / search   esc back   q quit"))
	for i := range lines {
		lines[i] = margin + lines[i]
	}
	top := make([]string, max((h-len(lines))/2, 0))
	lines = append(top, lines...)
	v := tea.NewView(terminalui.Frame(lines, w, h))
	v.AltScreen, v.WindowTitle = true, "Zoomies"
	return v
}

func (m homeModel) menuRows(items []homeItem, width, height int) []string {
	label := "ACTIONS"
	if m.fleet {
		label = "FLEET OPERATIONS"
	}
	if m.catalogue {
		label = "COMMANDS"
	}
	lines := []string{m.paint("2", label)}
	page := max(height-1, 1)
	start := max(0, m.cursor-page+1)
	for i := start; i < min(start+page, len(items)); i++ {
		text := "  " + items[i].title
		if i == m.cursor {
			text = m.paint("1;37;44", terminalui.Pad("› "+items[i].title, width))
		} else {
			text = m.paint("2", text)
		}
		lines = append(lines, text)
	}
	if len(items) == 0 {
		lines = append(lines, "No commands match.", "Press / to search again.")
	}
	return lines
}

func (m homeModel) detailRows(items []homeItem, width int) []string {
	if len(items) == 0 {
		return []string{m.paint("1", "Nothing here yet"), "Try another search."}
	}
	item := items[m.cursor]
	lines := []string{m.paint("36", item.group), m.paint("1", item.title), ""}
	lines = append(lines, wrapText(item.detail, width)...)

	return lines
}

func (m homeModel) confirmCard(item homeItem, width, height int) string {
	lines := []string{m.paint("1;33", item.title+"?"), ""}
	lines = append(lines, wrapText(item.detail, max(width-4, 1))...)
	// The answer keys are also in the fixed footer, including at the minimum
	// size where the explanation needs more room than the card has.
	return terminalui.Panel(lines, width, height, m.colour, true)
}
