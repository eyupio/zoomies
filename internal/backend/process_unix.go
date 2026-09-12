//go:build !windows

package backend

import (
	"errors"
	"os"
	"os/exec"
	"syscall"
	"time"
)

// The parts of the process backend that differ by operating system. Each has a
// twin in process_windows.go, and the two files together are the whole of what
// "the process backend runs on Windows" cost: everything above them is the same
// lifecycle, the same layout on disk, and the same records in it.
const (
	// exeSuffix is what a runner binary's name ends in here, which is nothing.
	exeSuffix = ""
	// configScript is the registration script a non-ephemeral pool runs.
	configScript = "config.sh"
	// removeGrace is how long a directory removal keeps trying after it first
	// fails. POSIX unlinks a file whose process is still running, so a
	// removal here either works or names a real problem, and there is nothing
	// to wait for.
	removeGrace = 0 * time.Second
)

// noShellDetail is why the process backend is unavailable on a host with no
// shell, in the words the Hosts page shows.
const noShellDetail = "no shell is installed (sh is not in PATH), so nothing actions/runner starts could run here; " +
	"the published Zoomies image is built without one on purpose -- from a container, use the docker or podman backend, " +
	"and use the process backend only on a host with a shell, tar and libicu"

// HasShell reports whether this host has a shell, which the runner's config.sh
// and every `run:` step need.
//
// It is checked before ICU because it decides what kind of host this is. The
// published Zoomies image is distroless -- no shell at all, on purpose -- so an
// agent running in it can never use this backend, and telling that operator to
// apt-get install libicu, into an image with no apt, sends them the wrong way.
func HasShell() bool {
	_, err := exec.LookPath("sh")
	return err == nil
}

// detachRunner starts the runner as the leader of a process group of its own.
//
// Two things follow. A signal meant for the runner reaches its whole worker
// tree -- Runner.Listener spawns Runner.Worker per job, and interrupting the
// listener alone orphaned the worker with the job still running. And a signal
// meant for the agent does not reach the runner: a terminal's Ctrl-C, or a
// service manager stopping the unit, stays with the agent's own group.
func detachRunner(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

// adoptRunner is called once the runner has started. The process group above
// is all the grouping a POSIX host needs, so there is nothing to add.
func adoptRunner(*exec.Cmd) {}

// releaseRunner is called once the runner has been reaped. Nothing was held
// for it here.
func releaseRunner(int) {}

// signalRunner delivers sig to the runner's process group. A group that is
// already gone answers os.ErrProcessDone, so callers treat it as the other
// "already exited" answers.
func signalRunner(proc *os.Process, sig syscall.Signal) error {
	err := syscall.Kill(-proc.Pid, sig)
	if errors.Is(err, syscall.ESRCH) {
		return os.ErrProcessDone
	}
	return err
}

// processAlive reports whether a pid is still running. Signal 0 performs the
// permission and existence checks without delivering anything.
func processAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	proc, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	return proc.Signal(syscall.Signal(0)) == nil
}

// platformChildEnv is what a runner's environment needs beyond the portable
// part childEnv builds. A POSIX runner needs nothing more than HOME and PATH.
func platformChildEnv(string) []string { return nil }
