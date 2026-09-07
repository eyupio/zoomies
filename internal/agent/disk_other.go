//go:build !unix

package agent

// diskSpace has no portable answer off unix. A host that cannot measure its
// own disk reports none, which the controller reads as "unknown" rather than
// as "empty" -- the difference between placing nothing there and placing
// everything there.
func diskSpace(string) (total, avail int64, ok bool) { return 0, 0, false }
