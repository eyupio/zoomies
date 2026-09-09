package controller

import (
	"testing"
)

// A fetch and a frame must summarise the same period.
//
// They did not: the fetch defaulted to a day and the frame to an hour, so an
// Overview showed a day's completed counts and wait percentiles for the second
// or two before its first frame landed and then silently swapped them for an
// hour's. The spec said an hour the whole time.
func TestAFetchAndAFrameSummariseTheSameWindow(t *testing.T) {
	h := newHarness(t)

	fetched, err := h.c.Stats(h.ctx, 0)
	if err != nil {
		t.Fatalf("Stats: %v", err)
	}
	framed, err := h.c.Stats(h.ctx, statsEventWindow)
	if err != nil {
		t.Fatalf("Stats: %v", err)
	}
	if fetched.Window != framed.Window {
		t.Errorf("a fetch covers %s and a frame covers %s; the tiles change under the operator when the first frame arrives",
			fetched.Window, framed.Window)
	}
}
