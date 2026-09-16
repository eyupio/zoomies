//go:build !(linux || darwin)

package backup

// DiskFree has no portable answer away from the platforms Zoomies measures
// disks on; the page simply does not show a figure.
func DiskFree(string) (int64, bool) { return 0, false }
