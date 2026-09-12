//go:build !windows

package machine

// windowsFacts is only reached on Windows; detect switches on the platform
// before calling it, so this exists to keep the other builds honest.
func windowsFacts() (string, int64) { return "", 0 }
