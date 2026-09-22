//go:build drill

package drill

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A row in the drill record is a claim that a named commit was proven to do
// something. Four rows in that file once claimed a commit passed a drill it
// reliably fails, because the runs had used an older build and the row was
// stamped from `git rev-parse HEAD` rather than from the binary.
//
// So the binary is asked, and where the answer might still not describe what
// ran, the row says so instead of asserting.
func TestTheRecordNamesTheBinaryItRanNotTheCheckout(t *testing.T) {
	stamp, caveat := provenance(builtBinary())
	if stamp == "" {
		t.Fatal("a row would be written with no commit at all")
	}
	// This tier refuses to run without a built binary, so the stamp here is
	// the real one and the caveat says whether it can be trusted.
	t.Logf("stamp=%q caveat=%q", stamp, caveat)

	if got := builtCommit(builtBinary()); got == "" {
		t.Error("the binary did not report the commit it was built from, so every row falls back to the checkout")
	}
}

// The stale build is the case that produced the false rows, and it is the one
// worth holding: a binary that is not this commit has to be called out.
func TestAStaleBinaryIsCalledOutRatherThanStamped(t *testing.T) {
	// A stand-in binary that reports a commit no checkout is on.
	dir := t.TempDir()
	fake := filepath.Join(dir, "zoomies")
	script := "#!/bin/sh\nprintf '{\"commit\":\"0000000\"}\\n'\n"
	if err := os.WriteFile(fake, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}

	stamp, caveat := provenance(fake)
	if stamp != "0000000" {
		t.Errorf("the row was stamped %q; it must name the commit the binary reports", stamp)
	}
	if !strings.Contains(caveat, "rebuild") {
		t.Errorf("a binary built from another commit produced the caveat %q; it has to tell somebody to rebuild", caveat)
	}
}

// A binary that says nothing must not be silently attributed to the checkout.
func TestABinaryThatNamesNoCommitSaysSo(t *testing.T) {
	dir := t.TempDir()
	mute := filepath.Join(dir, "zoomies")
	if err := os.WriteFile(mute, []byte("#!/bin/sh\nexit 1\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	_, caveat := provenance(mute)
	if !strings.Contains(caveat, "may not be about the code that ran") {
		t.Errorf("a binary reporting no commit produced the caveat %q", caveat)
	}
}
