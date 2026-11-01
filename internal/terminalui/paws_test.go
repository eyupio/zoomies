package terminalui

import (
	"strings"
	"testing"
)

func TestPawAnimationKeepsTaskLabelsStillAndRespectsReducedMotion(t *testing.T) {
	seen := map[string]bool{}
	static := Paws(0, false, false)
	for frame := 0; frame < 48; frame++ {
		for _, colour := range []bool{false, true} {
			icon := Paws(frame, colour, true)
			if Width(icon) != 5 {
				t.Fatalf("frame %d shifts the label: %q", frame, icon)
			}
			if !colour && strings.Contains(icon, "\x1b") {
				t.Fatal("plain indicator has ANSI colour")
			}
		}
		seen[Paws(frame, false, true)] = true
		if Paws(frame, false, false) != static {
			t.Fatal("reduced motion changes the paw indicator")
		}
	}
	if len(seen) < 2 || !strings.Contains(static, "🐾") {
		t.Fatal("the indicator has no paw steps")
	}
}

func TestBannerCompactsToLeaveRoomForTheSelectedTask(t *testing.T) {
	for _, size := range [][2]int{{28, 12}, {44, 18}, {76, 24}, {112, 32}} {
		rows := Banner(size[0], size[1], false, "dev")
		if len(rows) >= size[1]-7 {
			t.Fatalf("banner crowds a %v terminal", size)
		}
		for _, row := range rows {
			if Width(row) > size[0] {
				t.Fatalf("banner too wide at %v: %q", size, row)
			}
		}
	}
}
