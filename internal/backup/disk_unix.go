//go:build linux || darwin

package backup

import (
	"math"
	"syscall"
)

// DiskFree reports the bytes available on the filesystem holding path, and
// whether the question could be answered. It is what the settings page shows
// next to the backup directory, because "is there room for another copy" is
// the question an operator has just before there is not.
//
// The same two platforms internal/agent's disk measurement names, for the same
// reason: syscall.Statfs is not everywhere `unix` is.
func DiskFree(path string) (int64, bool) {
	var st syscall.Statfs_t
	if err := syscall.Statfs(path, &st); err != nil {
		return 0, false
	}
	bsize := int64(st.Bsize)
	if bsize <= 0 || uint64(st.Bavail) > math.MaxInt64/uint64(bsize) {
		return 0, false
	}
	return int64(st.Bavail) * bsize, true
}
