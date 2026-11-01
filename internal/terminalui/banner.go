package terminalui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

var bannerLetters = map[rune][5]string{
	'Z': {"█████", "   █ ", "  █  ", " █   ", "█████"},
	'O': {" ███ ", "█   █", "█   █", "█   █", " ███ "},
	'M': {"█   █", "██ ██", "█ █ █", "█   █", "█   █"},
	'I': {"█████", "  █  ", "  █  ", "  █  ", "█████"},
	'E': {"█████", "█    ", "████ ", "█    ", "█████"},
	'S': {" ████", "█    ", " ███ ", "    █", "████ "},
}

// Banner keeps the identity in one place. Small windows use a compact
// wordmark so the selected task keeps its space.
func Banner(width, height int, colour bool, version string) []string {
	if height < 22 || width < 52 {
		return []string{Brand(colour) + "  " + Paint(colour, "2", Fit(version, max(width-12, 1)))}
	}
	lines := make([]string, 5)
	for y := range lines {
		var plain []string
		for _, letter := range "ZOOMIES" {
			plain = append(plain, bannerLetters[letter][y])
		}
		var b strings.Builder
		for x, cell := range []rune(strings.Join(plain, " ")) {
			if cell == ' ' {
				b.WriteByte(' ')
				continue
			}
			tone := "33"
			if x > 13 {
				tone = "39"
			}
			if x > 28 {
				tone = "45"
			}
			b.WriteString(Paint(colour, "1;38;5;"+tone, string(cell)))
		}
		lines[y] = b.String()
	}
	if width >= 82 {
		side := []string{Paint(colour, "1", "GitHub Actions runner fleets"), Paint(colour, "2", "Self-hosted. On demand."), "", Paint(colour, "2", version), ""}
		for i := range lines {
			lines[i] = Pad(lines[i], 50) + side[i]
		}
	}
	return lines
}

// Panel delegates border, padding and cell measurements to Lip Gloss. The
// contents carry their own palette, which also makes NO_COLOR deterministic.
func Panel(lines []string, width, height int, colour, active bool) string {
	inside := make([]string, max(height-2, 0))
	copy(inside, lines)
	for i := range inside {
		inside[i] = Pad(inside[i], max(width-4, 0))
	}
	style := lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).Padding(0, 1)
	if colour {
		tone := "238"
		if active {
			tone = "39"
		}
		style = style.BorderForeground(lipgloss.Color(tone))
	}
	return style.Render(strings.Join(inside, "\n"))
}
