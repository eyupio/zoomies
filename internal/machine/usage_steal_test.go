package machine

import (
	"os"
	"path/filepath"
	"testing"
)

// Steal is read from the same counters as CPU and I/O wait, and is only a figure
// beside a CPU delta: the three are shares of the same interval.
func TestUsageMeasuresStealFromTheSameDeltasAsCPU(t *testing.T) {
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
	// user nice system idle iowait irq softirq steal: 100 ticks, none stolen.
	write("stat", "cpu 50 0 0 50 0 0 0 0 0 0\ncpu0 0\ncpu1 0\n")
	s := UsageSampler{root: root}
	if first := s.Sample(2, 8192); first.StealPercent != nil {
		t.Fatalf("the first sample has no delta to take a share of: %v", *first.StealPercent)
	}

	// 100 more ticks, 50 of them kept from this machine by its hypervisor, and 70
	// of the interval busy in all, steal included.
	write("stat", "cpu 70 0 0 80 0 0 0 50 0 0\ncpu0 0\ncpu1 0\n")
	u := s.Sample(2, 8192)
	if u.StealPercent == nil || *u.StealPercent != 50 {
		t.Fatalf("steal = %v, want 50%% of the interval", u.StealPercent)
	}
	if u.CPUPercent == nil || *u.CPUPercent != 70 {
		t.Fatalf("cpu = %v, want 70%%: steal is part of what busy counts, so it is not taken out of it", u.CPUPercent)
	}

	// A physical machine reports a counter that does not move, and that is a
	// measured zero: not starved, which is not the same as not measured.
	write("stat", "cpu 90 0 0 120 0 0 0 50 0 0\ncpu0 0\ncpu1 0\n")
	if u := s.Sample(2, 8192); u.StealPercent == nil || *u.StealPercent != 0 {
		t.Fatalf("a counter that did not move reported %v, want a measured zero", u.StealPercent)
	}

	// A kernel whose cpu line stops before steal reports nothing, not zero.
	write("stat", "cpu 100 0 0 130 0 0 0\ncpu0 0\ncpu1 0\n")
	if u := s.Sample(2, 8192); u.StealPercent != nil {
		t.Fatalf("a line with no steal counter reported %v", *u.StealPercent)
	}

	// A counter that goes backwards is a different machine than last time.
	write("stat", "cpu 200 0 0 200 0 0 0 80 0 0\ncpu0 0\ncpu1 0\n")
	_ = s.Sample(2, 8192)
	write("stat", "cpu 300 0 0 300 0 0 0 10 0 0\ncpu0 0\ncpu1 0\n")
	if u := s.Sample(2, 8192); u.StealPercent != nil {
		t.Fatalf("a counter that went backwards reported %v", *u.StealPercent)
	}
}

// The eighth counter after the name is steal, and only there: a line whose
// eighth number is something else (a different kernel's layout) must not be read
// as it, and a number that is not a counter is not measured.
func TestStealIsTheEighthCounterOnTheAggregateLineOnly(t *testing.T) {
	if v, ok := stealTicks("cpu0 1 2 3 4 5 6 7 99\ncpu 1 2 3 4 5 6 7 8 9 10\n"); !ok || v != 8 {
		t.Fatalf("steal = %v %v, want the aggregate line's eighth counter, 8", v, ok)
	}
	for _, raw := range []string{"", "cpu 1 2 3 4 5 6 7", "cpu 1 2 3 4 5 6 7 x", "cpu0 1 2 3 4 5 6 7 8", "intr 1 2 3 4 5 6 7 8 9"} {
		if v, ok := stealTicks(raw); ok {
			t.Errorf("%q was read as steal %v", raw, v)
		}
	}
}
