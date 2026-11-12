package backend

import (
	"bufio"
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
)

// A build in a docker-in-docker sidecar runs in a container of the sidecar's own
// daemon, in a cgroup below the sidecar's. When the kernel kills the compiler
// there for the sidecar's memory limit, the sidecar's own first process is
// untouched, and the outer daemon's OOMKilled flag (the only thing anybody
// read) can stay false. The job then read as the workflow's own failure: no
// fault, no advice, and nothing told the history a job of this kind needs more
// memory, so it failed the same way the next time.
//
// The kernel keeps the count the daemon does not: memory.events in a
// container's cgroup counts every kill in it and below it. The agent reads it
// while the container runs, because a stopped container's cgroup is gone, and
// remembers the largest figure it saw.

// oomKillLedger is the kills the agent has seen, by container ID, and the
// cgroup folder each container was found in.
type oomKillLedger struct {
	mu    sync.Mutex
	kills map[string]int64
	dirs  map[string]string
}

// noteOOMKills reads how many processes the kernel has killed for memory in a
// running container and below it, and remembers it. It is best effort and says
// nothing when it cannot tell: a daemon in a virtual machine or on another
// host has no cgroup this agent can read, and neither does cgroup v1.
func (b *DockerBackend) noteOOMKills(ctx context.Context, container string) {
	dir := b.cgroupDir(ctx, container)
	if dir == "" {
		return
	}
	n, ok := readOOMKills(filepath.Join(dir, "memory.events"))
	if !ok || n <= 0 {
		return
	}
	l := &b.oomKills
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.kills == nil {
		l.kills = map[string]int64{}
	}
	if n > l.kills[container] {
		l.kills[container] = n
	}
}

// oomKillsSeen is how many kills were seen in a container while it ran.
func (b *DockerBackend) oomKillsSeen(container string) int64 {
	b.oomKills.mu.Lock()
	defer b.oomKills.mu.Unlock()
	return b.oomKills.kills[container]
}

// forgetOOMKillsExcept drops what is remembered of containers that are gone, so
// the ledger is no larger than the host's own listing.
func (b *DockerBackend) forgetOOMKillsExcept(live map[string]bool) {
	l := &b.oomKills
	l.mu.Lock()
	defer l.mu.Unlock()
	for id := range l.kills {
		if !live[id] {
			delete(l.kills, id)
		}
	}
	for id := range l.dirs {
		if !live[id] {
			delete(l.dirs, id)
		}
	}
}

// cgroupDir finds a running container's cgroup folder, once, from the cgroup
// its first process is in. The path has to name the container: a daemon that
// is not on this machine reports a process ID that means nothing here, or
// worse, means some other process, whose kills are not this runner's.
func (b *DockerBackend) cgroupDir(ctx context.Context, container string) string {
	l := &b.oomKills
	l.mu.Lock()
	dir, known := l.dirs[container]
	l.mu.Unlock()
	if known {
		return dir
	}
	insp, err := b.api.ContainerInspect(ctx, container)
	if err != nil || insp.State == nil || insp.State.Pid <= 0 || insp.ID == "" {
		// Not cached: a container still starting has no process yet.
		return ""
	}
	dir = cgroupDirOf(firstNonEmpty(b.procRoot, "/proc"), firstNonEmpty(b.cgroupRoot, "/sys/fs/cgroup"), insp.State.Pid, insp.ID)
	l.mu.Lock()
	if l.dirs == nil {
		l.dirs = map[string]string{}
	}
	l.dirs[container] = dir
	l.mu.Unlock()
	return dir
}

// cgroupDirOf reads /proc/<pid>/cgroup for the unified (v2) hierarchy's path
// and returns the folder under cgroupRoot, or "" where there is none, the
// path does not name the container, or the folder keeps no memory.events.
func cgroupDirOf(procRoot, cgroupRoot string, pid int, containerID string) string {
	body, err := os.ReadFile(filepath.Join(procRoot, strconv.Itoa(pid), "cgroup"))
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(string(body), "\n") {
		rel, ok := strings.CutPrefix(line, "0::")
		if !ok {
			continue
		}
		rel = strings.TrimSpace(rel)
		if !strings.Contains(rel, containerID) || strings.Contains(rel, "..") {
			return ""
		}
		// The first process may sit in a child the container made for itself
		// (a sidecar's daemon moves into one); the container's own folder is the
		// one that names it, and counts everything below.
		for !strings.Contains(filepath.Base(rel), containerID) {
			rel = filepath.Dir(rel)
		}
		dir := filepath.Join(cgroupRoot, rel)
		if _, err := os.Stat(filepath.Join(dir, "memory.events")); err != nil {
			return ""
		}
		return dir
	}
	return ""
}

// readOOMKills reads the oom_kill line of a memory.events file.
func readOOMKills(path string) (int64, bool) {
	body, err := os.ReadFile(path)
	if err != nil {
		return 0, false
	}
	sc := bufio.NewScanner(bytes.NewReader(body))
	for sc.Scan() {
		if v, ok := strings.CutPrefix(sc.Text(), "oom_kill "); ok {
			n, err := strconv.ParseInt(strings.TrimSpace(v), 10, 64)
			return n, err == nil
		}
	}
	return 0, false
}
