//go:build linux || darwin

package agent

import (
	"syscall"
	"testing"
)

// TestTheAgentReportsSpaceARunnerCanActuallyWriteTo is the distinction
// diskSpace exists to make, and the one thing about it a comment cannot keep
// true: f_bavail rather than f_bfree.
//
// The two differ by the reserve the filesystem keeps for root, and a runner is
// deliberately not root. Reporting the larger number would have the controller
// place work into space the job cannot write to -- a build that fills the disk
// and fails at the end, on a host the fleet thought had room.
//
// Swapping one identifier for the other is a one-token change that reads as a
// tidy-up, so it needs a test rather than the comment above it.
func TestTheAgentReportsSpaceARunnerCanActuallyWriteTo(t *testing.T) {
	dir := t.TempDir()
	var st syscall.Statfs_t
	if err := syscall.Statfs(dir, &st); err != nil {
		t.Skipf("this platform cannot measure a filesystem here: %v", err)
	}
	if st.Bfree == st.Bavail {
		t.Skip("this filesystem keeps no reserve, so free and available are the same number")
	}

	_, avail, ok := diskSpace(dir)
	if !ok {
		t.Fatal("diskSpace could not measure a directory that exists")
	}

	bsize := int64(st.Bsize)
	wantAvail, wantFree := int64(st.Bavail)*bsize, int64(st.Bfree)*bsize
	if avail == wantFree {
		t.Fatalf("available = %d bytes, which is f_bfree: %d of that is reserved and a runner cannot write to it",
			avail, wantFree-wantAvail)
	}
	if avail != wantAvail {
		t.Fatalf("available = %d bytes, want f_bavail's %d", avail, wantAvail)
	}
}
