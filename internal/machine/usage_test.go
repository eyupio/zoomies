package machine

import (
	"os"
	"path/filepath"
	"testing"
)

func TestUsageMeasuresDeltasAndAvailableMemoryWithoutSleeping(t *testing.T) {
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
	write("meminfo", "MemTotal: 8388608 kB\nMemFree: 1024 kB\nMemAvailable: 4194304 kB\n")
	write("stat", "cpu 100 0 0 100 0 0 0 0 99 0\ncpu0 0\ncpu1 0\n")
	s := UsageSampler{root: root}
	first := s.Sample(2, 8192)
	if first.CPUPercent != nil || first.MemoryAvailableMB == nil || *first.MemoryAvailableMB != 4096 {
		t.Fatalf("first sample must report available memory but no CPU delta: %+v", first)
	}
	write("stat", "cpu 125 0 0 175 0 0 0 0 124 0\ncpu0 0\ncpu1 0\n")
	u := s.Sample(2, 8192)
	if u.CPUPercent == nil || *u.CPUPercent != 25 {
		t.Fatalf("CPU = %v, want 25%% (guest already counted)", u.CPUPercent)
	}
	// A reboot or CPU topology change starts a new baseline, not an enormous
	// negative utilisation that attracts every queued job.
	write("stat", "cpu 1 0 0 1 0 0 0 0\ncpu0 0\ncpu1 0\n")
	if s.Sample(2, 8192).CPUPercent != nil {
		t.Fatal("counter reset became a valid CPU sample")
	}
	if got := s.Sample(1, 2048); got.CPUPercent != nil || got.MemoryAvailableMB != nil {
		t.Fatalf("physical host figures were attributed to a smaller cgroup: %+v", got)
	}
}

func TestMissingAndInvalidUsageNeverBecomeIdleReadings(t *testing.T) {
	s := UsageSampler{root: t.TempDir()}
	if u := s.Sample(2, 8192); u.CPUPercent != nil || u.MemoryAvailableMB != nil {
		t.Fatal("missing procfs became measured usage")
	}
	for _, raw := range []string{"", "cpu broken 1 2 3\ncpu0 0", "cpu 1 2 3 4"} {
		if _, _, _, ok := cpuTicks(raw); ok {
			t.Fatalf("accepted %q", raw)
		}
	}
	for _, raw := range []string{"MemTotal: 8192 kB\nMemFree: 4096 kB", "MemTotal: 8192 kB\nMemAvailable: 9000 kB", "MemTotal: 8192 kB\nMemAvailable: -1 kB"} {
		if _, _, ok := availableMemory(raw); ok {
			t.Fatalf("accepted %q", raw)
		}
	}
	if _, available, ok := availableMemory("MemTotal: 8192 kB\nMemAvailable: 0 kB"); !ok || available != 0 {
		t.Fatal("a measured full host was treated as unmeasured")
	}
}
