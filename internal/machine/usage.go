package machine

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
)

// Usage describes the whole machine, including work not owned by Zoomies.
// A missing value means unmeasured, never idle. Linux procfs is the only
// source for now; other platforms retain reservation-based placement.
type Usage struct {
	CPUPercent        *float64
	MemoryAvailableMB *int64
}

// UsageSampler takes CPU deltas between heartbeats without sleeping or asking
// Docker to do more work while it may already be overloaded.
type UsageSampler struct {
	mu          sync.Mutex
	root        string
	total, idle uint64
	cpus        int
}

func (s *UsageSampler) Sample(cpus int, memoryMB int64) Usage {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out Usage
	stat, err := os.ReadFile(filepath.Join(s.root, "/proc/stat"))
	if err == nil {
		total, idle, count, ok := cpuTicks(string(stat))
		if ok && count == cpus && count == s.cpus && total > s.total && idle >= s.idle && idle-s.idle <= total-s.total {
			percent := 100 * (1 - float64(idle-s.idle)/float64(total-s.total))
			out.CPUPercent = &percent
		}
		if ok && count == cpus {
			s.total, s.idle, s.cpus = total, idle, count
		} else {
			s.total, s.idle, s.cpus = 0, 0, 0
		}
	} else {
		s.total, s.idle, s.cpus = 0, 0, 0
	}
	mem, err := os.ReadFile(filepath.Join(s.root, "/proc/meminfo"))
	if err == nil {
		total, available, ok := availableMemory(string(mem))
		// Never label an agent cgroup's figures as its sibling runners' host,
		// or a remote daemon's machine as the one this process can see.
		if ok && total == memoryMB {
			out.MemoryAvailableMB = &available
		}
	}
	return out
}

func cpuTicks(raw string) (total, idle uint64, cpus int, ok bool) {
	for _, line := range strings.Split(raw, "\n") {
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		if fields[0] == "cpu" {
			if len(fields) < 5 {
				return 0, 0, 0, false
			}
			// Guest time is already included in user/nice. Count only the first
			// eight counters. I/O wait counts as unavailable CPU headroom here.
			for i := 1; i < len(fields) && i <= 8; i++ {
				v, err := strconv.ParseUint(fields[i], 10, 64)
				if err != nil || total+v < total {
					return 0, 0, 0, false
				}
				total += v
				if i == 4 {
					idle = v
				}
			}
			ok = true
		} else if strings.HasPrefix(fields[0], "cpu") {
			if _, err := strconv.Atoi(strings.TrimPrefix(fields[0], "cpu")); err == nil {
				cpus++
			}
		}
	}
	return total, idle, cpus, ok && cpus > 0
}

func availableMemory(raw string) (total, available int64, ok bool) {
	total, available = -1, -1
	for _, line := range strings.Split(raw, "\n") {
		fields := strings.Fields(line)
		if len(fields) != 3 || fields[2] != "kB" {
			continue
		}
		if fields[0] != "MemTotal:" && fields[0] != "MemAvailable:" {
			continue
		}
		v, err := strconv.ParseInt(fields[1], 10, 64)
		if err != nil || v < 0 {
			return 0, 0, false
		}
		if fields[0] == "MemTotal:" {
			total = v
		} else {
			available = v
		}
	}
	// MemAvailable includes reclaimable caches; MemFree alone would call a
	// healthy build host full just because Linux put idle memory to use.
	return total / 1024, available / 1024, total >= 1024 && available >= 0 && available <= total
}
