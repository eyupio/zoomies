//go:build unix

package agent

import "syscall"

// diskSpace reports the total and available bytes on the filesystem holding
// path, and whether the question could be answered at all.
//
// Available rather than free: the two differ by the reserve a filesystem keeps
// for root, and a runner is not root. Reporting the larger number would let the
// controller place work into space the job cannot actually write to.
func diskSpace(path string) (total, avail int64, ok bool) {
	var st syscall.Statfs_t
	if err := syscall.Statfs(path, &st); err != nil {
		return 0, 0, false
	}
	// Bsize is signed on some platforms and unsigned on others, so it goes
	// through int64 before it multiplies anything.
	size := int64(st.Bsize)
	if size <= 0 {
		return 0, 0, false
	}
	return int64(st.Blocks) * size, int64(st.Bavail) * size, true
}
