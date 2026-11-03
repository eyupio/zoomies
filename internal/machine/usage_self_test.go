package machine

import (
	"os"
	"path/filepath"
	"testing"
)

// A controller with no container runtime to ask has only procfs to learn its
// machine's size from, and it has to be the same procfs its figures are read
// from, or a figure could be about one machine and its size about another.
func TestTheMachinesSizeIsReadFromTheProcfsItsFiguresComeFrom(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "proc"), 0o700); err != nil {
		t.Fatal(err)
	}
	write := func(name, body string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(root, "proc", name), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	s := UsageSampler{root: root}
	if _, _, ok := s.Machine(); ok {
		t.Fatal("a machine with no procfs reported a size")
	}

	write("stat", "cpu 50 0 0 50 0 0 0 0 0 0\ncpu0 0\ncpu1 0\ncpu2 0\ncpu3 0\nintr 1\n")
	if _, _, ok := s.Machine(); ok {
		t.Fatal("a machine with no meminfo reported a size")
	}
	write("meminfo", "MemTotal: 8388608 kB\nMemFree: 1024 kB\nMemAvailable: 4194304 kB\n")
	cpus, memoryMB, ok := s.Machine()
	if !ok || cpus != 4 || memoryMB != 8192 {
		t.Fatalf("machine = %d CPUs, %d MB, %v; want 4 CPUs and 8192 MB, counted from the cpuN lines and MemTotal", cpus, memoryMB, ok)
	}

	// The size it reports is one the sampler accepts, so a figure read beside it is
	// about the machine the size describes.
	write("stat", "cpu 80 0 0 80 0 0 0 40 0 0\ncpu0 0\ncpu1 0\ncpu2 0\ncpu3 0\n")
	if u := s.Sample(cpus, memoryMB); u.MemoryAvailableMB == nil || *u.MemoryAvailableMB != 4096 {
		t.Fatalf("the sampler refused the size Machine reported: %+v", u)
	}

	// No CPU lines at all is not a machine of no CPUs.
	write("stat", "cpu 1 2 3 4 5 6 7 8\n")
	if _, _, ok := s.Machine(); ok {
		t.Fatal("an aggregate line with no cpuN lines reported a size")
	}
}
