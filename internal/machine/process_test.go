package machine

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// processStat is a /proc/PID/stat line with the fields around the two that
// matter filled in as the kernel writes them, so the test fails if the parser is
// reading a column it should not.
func processStat(comm string, user, system uint64) string {
	return fmt.Sprintf("1234 (%s) S 1 1234 1234 0 -1 4194560 100 0 0 0 %d %d 0 0 20 0 8 0 5000 1000 200\n", comm, user, system)
}

// fakeProcess is a sampler reading a procfs in a temporary folder, with a clock
// the test moves.
type fakeProcess struct {
	t       *testing.T
	root    string
	sampler *ProcessSampler
	clock   time.Time
}

func newFakeProcess(t *testing.T) *fakeProcess {
	t.Helper()
	f := &fakeProcess{t: t, root: t.TempDir(), clock: time.Unix(1_000_000, 0)}
	if err := os.MkdirAll(filepath.Join(f.root, "proc", "self"), 0o755); err != nil {
		t.Fatal(err)
	}
	f.sampler = &ProcessSampler{root: f.root, now: func() time.Time { return f.clock }}
	return f
}

// reads moves the clock on by elapsed, writes the process's counters and takes a
// share for a machine of cpus CPUs.
func (f *fakeProcess) reads(cpus int, elapsed time.Duration, stat string) (float64, bool) {
	f.t.Helper()
	f.clock = f.clock.Add(elapsed)
	if err := os.WriteFile(filepath.Join(f.root, "proc", "self", "stat"), []byte(stat), 0o600); err != nil {
		f.t.Fatal(err)
	}
	return f.sampler.Share(cpus)
}

func TestProcessShareIsItsCPUTimeOverTheIntervalAcrossEveryCPU(t *testing.T) {
	f := newFakeProcess(t)
	if _, ok := f.reads(4, 0, processStat("zoomies", 1000, 500)); ok {
		t.Fatal("the first reading has nothing to compare with and must not report a share")
	}
	// Two seconds of CPU (150 user and 50 system ticks) in ten seconds, on four
	// CPUs that could have given forty: a twentieth.
	share, ok := f.reads(4, 10*time.Second, processStat("zoomies", 1150, 550))
	if !ok {
		t.Fatal("two readings an interval apart must give a share")
	}
	if want := 5.0; share < want-1e-9 || share > want+1e-9 {
		t.Fatalf("share = %v, want %v: user and system time added, over the interval, over every CPU", share, want)
	}
}

// A command name may hold spaces and brackets, and the fields after it are
// counted from the last bracket. Counting from the first space would read the
// wrong columns and report a share that looks plausible.
func TestAProcessNameWithBracketsAndSpacesDoesNotShiftTheFields(t *testing.T) {
	f := newFakeProcess(t)
	f.reads(2, 0, processStat("a (b) c", 100, 100))
	// Both counters move, and by different amounts, because a parser that reads
	// the columns two places along would see the system time as the user time and
	// the same total difference from a change to either one alone.
	share, ok := f.reads(2, 10*time.Second, processStat("a (b) c", 160, 140))
	if !ok {
		t.Fatal("a share must be reported for a name with brackets in it")
	}
	// One second of CPU in ten (100 ticks of user and system together), across
	// two CPUs.
	if want := 5.0; share < want-1e-9 || share > want+1e-9 {
		t.Fatalf("share = %v, want %v", share, want)
	}
}

func TestACounterThatWentBackwardsOrAClockThatStoodStillIsNotAShare(t *testing.T) {
	f := newFakeProcess(t)
	f.reads(4, 0, processStat("zoomies", 1000, 500))
	if _, ok := f.reads(4, 10*time.Second, processStat("zoomies", 10, 5)); ok {
		t.Fatal("a counter that went backwards is not the process sampled last time")
	}
	if _, ok := f.reads(4, 0, processStat("zoomies", 110, 55)); ok {
		t.Fatal("an interval that did not move forward has no share to take")
	}
	// The dropped readings moved the baseline, so the next one is a share again.
	// A baseline stuck on the first reading would never recover.
	share, ok := f.reads(4, 10*time.Second, processStat("zoomies", 210, 55))
	if !ok {
		t.Fatal("one bad reading must not silence every share after it")
	}
	if want := 2.5; share < want-1e-9 || share > want+1e-9 {
		t.Fatalf("share = %v, want %v", share, want)
	}
}

// More CPU than every CPU could give is a reading of something else, such as an
// interval cut short. Reported, it would make the controller look like the cause
// of a load it could not have made.
func TestAShareBeyondEveryCPUIsDropped(t *testing.T) {
	f := newFakeProcess(t)
	f.reads(4, 0, processStat("zoomies", 0, 0))
	if share, ok := f.reads(4, 10*time.Second, processStat("zoomies", 5000, 0)); ok {
		t.Fatalf("fifty seconds of CPU in ten, on four CPUs, reported as %v%%", share)
	}
}

func TestWithoutProcfsThereIsNoShareAndTheBaselineStartsAgain(t *testing.T) {
	f := newFakeProcess(t)
	f.reads(2, 0, processStat("zoomies", 100, 0))
	if err := os.Remove(filepath.Join(f.root, "proc", "self", "stat")); err != nil {
		t.Fatal(err)
	}
	f.clock = f.clock.Add(time.Second)
	if _, ok := f.sampler.Share(2); ok {
		t.Fatal("a platform with no procfs has no share")
	}
	// The reading after a gap cannot be compared with the one before it.
	if _, ok := f.reads(2, 10*time.Second, processStat("zoomies", 200, 0)); ok {
		t.Fatal("the first reading after a gap has nothing to compare with")
	}
}

func TestAMachineWithNoCPUsHasNoShare(t *testing.T) {
	f := newFakeProcess(t)
	f.reads(0, 0, processStat("zoomies", 100, 0))
	if _, ok := f.reads(0, 10*time.Second, processStat("zoomies", 200, 0)); ok {
		t.Fatal("a share is a fraction of the CPUs there are, and there were none")
	}
}
