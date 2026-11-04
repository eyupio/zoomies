package machine

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

// clockTicksPerSecond is USER_HZ, the unit /proc/PID/stat counts CPU time in. It
// is 100 on every architecture Zoomies builds for, and the kernel only offers it
// through sysconf, which a static binary built without cgo cannot call.
const clockTicksPerSecond = 100

// ProcessSampler measures how much of the machine's CPU this process itself used
// between two calls.
//
// It exists to be read beside the machine's own busy figure. On a small virtual
// machine that figure counts everything: this process, the container runtime,
// the tunnel, and the time the hypervisor kept. A controller that is asked "why
// is my host busy" needs to know which of those it is, and nothing else in
// procfs says so for one process in the same units.
type ProcessSampler struct {
	mu   sync.Mutex
	root string
	// now is a seam for a test, which has no clock of its own to move; the real
	// one reads the wall clock.
	now    func() time.Time
	ticks  uint64
	at     time.Time
	primed bool
}

// Share is the percentage of the whole machine's CPU capacity (all of its CPUs,
// so a process using one core of four is 25) that this process used since the
// last call. It reports false on the first call, which has nothing to compare
// with, on a platform with no procfs, and whenever the figure cannot be true:
// a counter that went backwards, an interval that did not move forward, or a
// share beyond the machine's capacity.
//
// A figure that is dropped still moves the baseline, so one bad reading costs
// one share and not every share after it.
func (s *ProcessSampler) Share(cpus int) (float64, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	raw, err := os.ReadFile(filepath.Join(s.root, "/proc/self/stat"))
	if err != nil {
		s.primed = false
		return 0, false
	}
	ticks, ok := processTicks(string(raw))
	if !ok {
		s.primed = false
		return 0, false
	}
	now := time.Now()
	if s.now != nil {
		now = s.now()
	}
	prevTicks, prevAt, wasPrimed := s.ticks, s.at, s.primed
	s.ticks, s.at, s.primed = ticks, now, true
	elapsed := now.Sub(prevAt).Seconds()
	if !wasPrimed || cpus <= 0 || ticks < prevTicks || elapsed <= 0 {
		return 0, false
	}
	used := float64(ticks-prevTicks) / clockTicksPerSecond
	share := 100 * used / elapsed / float64(cpus)
	// A process cannot use more than every CPU there is. Beyond that the reading
	// is of something else (an interval cut short, a clock that moved), and
	// reporting it would make the controller look like the cause of a load it
	// could not have made.
	if share > 100 {
		return 0, false
	}
	return share, true
}

// processTicks adds the user and system time of the process described by the
// contents of /proc/PID/stat, in clock ticks.
//
// The second field is the command name in parentheses, and a name may itself
// hold spaces and brackets, so the line is split after the last closing
// bracket, where the fields are plain. The first of those is field 3 (the
// state), which puts the user time, field 14, at index 11 and the system time,
// field 15, at index 12.
func processTicks(raw string) (uint64, bool) {
	end := strings.LastIndexByte(raw, ')')
	if end < 0 {
		return 0, false
	}
	fields := strings.Fields(raw[end+1:])
	if len(fields) < 13 {
		return 0, false
	}
	user, err := strconv.ParseUint(fields[11], 10, 64)
	if err != nil {
		return 0, false
	}
	system, err := strconv.ParseUint(fields[12], 10, 64)
	if err != nil || user+system < user {
		return 0, false
	}
	return user + system, true
}
