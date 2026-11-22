//go:build !windows

package backend

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// The runner leads a process group of its own. Without that, interrupting the
// listener orphaned the worker running the job, and a service manager stopping
// the agent's unit took every runner in the cgroup down with it -- the
// opposite of "restarting an agent must never kill a job".
func TestProcessRunnerLeadsItsOwnProcessGroup(t *testing.T) {
	t.Parallel()
	requireUnix(t)
	b, _ := newStubProcessBackend(t)
	ctx := context.Background()

	h, err := b.Create(ctx, processSpec())
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	t.Cleanup(func() { _ = b.Remove(ctx, h) })
	waitForPhase(t, b, h, PhaseRunning, 5*time.Second)

	pid := readPID(string(h))
	if pid <= 0 {
		t.Fatal("no pid recorded")
	}
	pgid, err := syscall.Getpgid(pid)
	if err != nil {
		t.Fatalf("Getpgid: %v", err)
	}
	if pgid != pid {
		t.Fatalf("the runner's process group is %d, want its own pid %d; it is sharing the agent's group", pgid, pid)
	}
}

// stubLeavesAGroupMember stands in for a runner whose job started something in
// the background and finished: the listener exits at once, and the process it
// left behind carries on in the runner's group.
const stubLeavesAGroupMember = `#!/bin/sh
sleep 300 &
echo $! > straggler.pid
echo "listener started"
exit 0
`

// stillRuns says whether a process is doing anything. A killed process whose
// parent has not collected it is a zombie, which still answers signal 0, so
// where the kernel says so the state is read as well.
func stillRuns(pid int) bool {
	data, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))
	if err != nil {
		return processAlive(pid)
	}
	end := strings.LastIndexByte(string(data), ')')
	if end < 0 || end+2 >= len(data) {
		return processAlive(pid)
	}
	return data[end+2] != 'Z' && data[end+2] != 'X'
}

// The runner leads a group so that a signal for it reaches its whole tree, but
// the only place that signal was sent from was Stop, and Stop is for a runner
// that is still running. A runner that finished by itself, which is what an
// ephemeral one does, was reaped and nothing touched its group again: whatever
// its job had left in the background went on running on the host, outside any
// workspace and any limit, until somebody noticed.
func TestWhateverAFinishedRunnerLeftBehindIsStopped(t *testing.T) {
	t.Parallel()
	requireUnix(t)
	root := t.TempDir()
	installStubRunner(t, root, stubVersion, stubLeavesAGroupMember, stubConfigOK)
	b, err := NewProcess(ProcessOptions{WorkDir: root, RunnerVersion: stubVersion, Logger: quietLogger()})
	if err != nil {
		t.Fatalf("NewProcess: %v", err)
	}
	ctx := context.Background()

	h, err := b.Create(ctx, processSpec())
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	t.Cleanup(func() { _ = b.Remove(ctx, h) })
	waitForPhase(t, b, h, PhaseExited, 10*time.Second)

	raw, err := os.ReadFile(filepath.Join(string(h), "straggler.pid"))
	if err != nil {
		t.Fatalf("the stub did not record the process it left behind: %v", err)
	}
	straggler, err := strconv.Atoi(strings.TrimSpace(string(raw)))
	if err != nil || straggler <= 0 {
		t.Fatalf("straggler.pid holds %q", raw)
	}
	// Whatever the outcome, do not leave a five minute sleep on the machine.
	t.Cleanup(func() { _ = syscall.Kill(straggler, syscall.SIGKILL) })

	deadline := time.Now().Add(5 * time.Second)
	for stillRuns(straggler) && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	if stillRuns(straggler) {
		t.Errorf("process %d, started by the runner and left in its group, is still running after the runner exited", straggler)
	}
}
