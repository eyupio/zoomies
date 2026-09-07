package agent

import (
	"os"
	"testing"
)

// The work directory is where a runner's checkout and its caches land, so it
// is that filesystem that decides whether a job has anywhere to go -- not the
// root one, and not the one the agent's binary happens to sit on.
func TestTheAgentMeasuresTheDiskItsRunnersWillUse(t *testing.T) {
	if _, _, ok := diskSpace(t.TempDir()); !ok {
		t.Skip("this platform has no portable way to measure a filesystem")
	}
	a := &Agent{opts: Options{WorkDir: t.TempDir()}}

	total, free := a.workDirSpace()

	if total <= 0 {
		t.Fatalf("total = %d MB, want the size of a filesystem that exists", total)
	}
	if free <= 0 || free > total {
		t.Fatalf("free = %d MB of %d MB total, which is not a filesystem", free, total)
	}
}

// A directory that is not there yet cannot be measured, and saying "zero" for
// it would read as a full disk. Nothing is the honest answer, and the
// controller keeps whatever it already knew.
func TestAWorkDirectoryThatIsNotThereMeasuresNothing(t *testing.T) {
	missing := t.TempDir() + string(os.PathSeparator) + "not-created-yet"
	a := &Agent{opts: Options{WorkDir: missing}}

	if total, free := a.workDirSpace(); total != 0 || free != 0 {
		t.Fatalf("measured %d MB total and %d MB free of a directory that does not exist", total, free)
	}
	// Asserted at the measurement too, not only after the conversion to
	// megabytes: a wrong answer smaller than a megabyte rounds to the same
	// zero as no answer, so the megabyte figures alone cannot tell them apart.
	if total, avail, ok := diskSpace(missing); ok {
		t.Fatalf("diskSpace reported %d/%d bytes for a directory that does not exist", avail, total)
	}
}
