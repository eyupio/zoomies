//go:build linux || darwin

package agent

import "syscall"

// diskSpace reports the total and available bytes on the filesystem holding
// path, and whether the question could be answered at all.
//
// Available rather than free: the two differ by the reserve a filesystem keeps
// for root, and a runner is not root. Reporting the larger number would let the
// controller place work into space the job cannot actually write to.
//
// The build tag names the two platforms Zoomies ships for rather than `unix`,
// because `unix` is a promise this file cannot keep: syscall.Statfs does not
// exist on NetBSD, which has Statvfs1, nor on Solaris and illumos, which have
// Statvfs. A tag that covers them would not compile there, and a build failure
// is a worse answer than the honest "cannot measure" the other file gives.
//
// f_bsize rather than f_frsize, which is the unit POSIX defines f_blocks in:
// Darwin's Statfs_t has no Frsize field at all, and on every filesystem this
// will meet the two are equal. Where they are not, the figure is wrong by the
// ratio between them -- worth knowing, not worth a per-platform file for a
// number used at megabyte granularity to choose a host.
func diskSpace(path string) (total, avail int64, ok bool) {
	var st syscall.Statfs_t
	if err := syscall.Statfs(path, &st); err != nil {
		return 0, 0, false
	}
	// Bsize is signed on some platforms and unsigned on others, so it goes
	// through int64 before it multiplies anything; the counts are unsigned
	// everywhere. What the numbers mean, and which of them cannot be a
	// filesystem, is diskFromStatfs's business.
	return diskFromStatfs(uint64(st.Blocks), uint64(st.Bavail), int64(st.Bsize))
}
