//go:build !(linux || darwin)

package agent

// diskSpace has no portable answer away from the platforms Zoomies ships for.
// A host that cannot measure its own disk reports none, which the controller
// reads as "unknown" rather than as "empty" -- the difference between placing
// nothing there and placing everything there.
func diskSpace(string) (total, avail int64, ok bool) { return 0, 0, false }
