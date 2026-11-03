package controller

import (
	"testing"

	"github.com/eyupio/zoomies/internal/events"
)

// The page that draws the controller's machine has to follow it without being
// reloaded, and nothing writes a row when the housekeeping tick takes a sample, so
// the frame is worked out with the other derived views and sent when its bytes
// change: in the shape the route serves, once per new sample, and not again for a
// pass that found the same thing.
func TestTheControllersMachineIsSentOnTheStreamWhenItsReadingMoves(t *testing.T) {
	h := newHarness(t)
	sub := h.listen(events.KindControllerMachine)

	h.c.machine.read = reading(68, 36)
	h.c.sampleMachine()
	h.c.publishDerived(h.ctx)
	got := nextOfKind(t, sub, events.KindControllerMachine)
	usage, _ := got["usage"].(map[string]any)
	if got["cpus"] != 4.0 || usage["steal_percent"] != 36.0 || usage["cpu_percent"] != 68.0 || got["usage_fresh"] != true {
		t.Fatalf("frame = %v, want the shape GET /controller/machine serves", got)
	}

	// The same reading again is the same bytes: nothing is sent for it.
	h.c.publishDerived(h.ctx)
	nothingFor(t, sub)

	// A new sample moves them, and is sent.
	h.advance(30 * 1e9)
	h.c.machine.read = reading(41, 5)
	h.c.sampleMachine()
	h.c.publishDerived(h.ctx)
	next := nextOfKind(t, sub, events.KindControllerMachine)
	if u, _ := next["usage"].(map[string]any); u["cpu_percent"] != 41.0 || u["steal_percent"] != 5.0 {
		t.Fatalf("frame = %v, want the new sample", next)
	}
}

// A controller nobody is watching does not do the watchers' work, and that
// includes this one: with no subscriber the frame is not worked out at all.
func TestNothingAboutTheControllersMachineIsComputedForNobody(t *testing.T) {
	h := newHarness(t)
	h.c.machine.read = reading(68, 36)
	h.c.sampleMachine()
	h.c.publishDerived(h.ctx)
	if h.c.lastMachine != nil {
		t.Fatalf("the machine was worked out with nobody connected: %s", h.c.lastMachine)
	}
}
