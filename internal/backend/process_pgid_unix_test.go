//go:build !windows

package backend

import (
	"context"
	"syscall"
	"testing"
	"time"
)

// The runner leads a process group of its own. Without that, interrupting the
// listener orphaned the worker running the job, and a service manager stopping
// the agent's unit took every runner in the cgroup down with it -- the
// opposite of "restarting an agent must never kill a job".
func TestProcessRunnerLeadsItsOwnProcessGroup(t *testing.T) {
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
