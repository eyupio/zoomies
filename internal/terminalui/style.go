// Package terminalui holds the terminal presentation shared by the CLI and installer.
package terminalui

import (
	"os"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

// Motion is independent of colour: NO_COLOR still leaves a navigable interface.
func Motion() bool {
	return os.Getenv("ZOOMIES_NO_ANIMATION") == "" && os.Getenv("CI") == "" && os.Getenv("TERM") != "dumb"
}

func Colour() bool {
	_, off := os.LookupEnv("NO_COLOR")
	return !off && os.Getenv("TERM") != "dumb"
}

func Paint(on bool, code, text string) string {
	if !on || text == "" {
		return text
	}
	return "\x1b[" + code + "m" + text + "\x1b[0m"
}

// Fit measures terminal cells, including wide glyphs and combining characters.
func Fit(text string, width int) string {
	return ansi.Truncate(text, max(width, 0), "…")
}

func Width(text string) int { return ansi.StringWidth(text) }

func Pad(text string, width int) string {
	text = Fit(text, width)
	return text + strings.Repeat(" ", max(0, width-Width(text)))
}

// Frame clips both axes so resizing cannot push the footer into scrollback.
func Frame(lines []string, width, height int) string {
	if width <= 0 {
		width = 80
	}
	if height <= 0 {
		height = 24
	}
	out := make([]string, height)
	for i := 0; i < min(len(lines), height); i++ {
		out[i] = Fit(lines[i], width)
	}
	return strings.Join(out, "\n")
}

func Brand(on bool) string { return Paint(on, "1;36", "zoomies") }

// Paws uses the two small, staggered marks and 2.4-second rhythm of Eli's
// working indicator. Its fixed display width keeps the task label still.
func Paws(frame int, colour, active bool) string {
	first, second := "🐾", "  "
	firstStyle, secondStyle := "36", "2;36"
	if active {
		switch (frame % 24) / 4 {
		case 0:
			firstStyle = "2;36"
		case 1:
			firstStyle = "1;36"
		case 2:
			second = "🐾"
			firstStyle = "36"
		case 3:
			second = "🐾"
			firstStyle = "2;36"
			secondStyle = "1;36"
		case 4:
			first = "  "
			second = "🐾"
			secondStyle = "36"
		case 5:
			first = "  "
			second = "🐾"
		}
	}
	return Paint(colour, firstStyle, first) + " " + Paint(colour, secondStyle, second)
}

type TickMsg time.Time

func Tick() tea.Cmd {
	return tea.Tick(100*time.Millisecond, func(t time.Time) tea.Msg { return TickMsg(t) })
}
