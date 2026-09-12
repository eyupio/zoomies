//go:build windows

package backend

import (
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"sync"
	"syscall"
	"time"

	"golang.org/x/sys/windows"
)

// The Windows half of the process backend. It exists so that a Windows host
// can join a fleet and run actions/runner's win-x64 build as a process, which
// is the shape decision 26 in the roadmap chose: no Windows containers, and so
// no ephemeral container -- a job gets a fresh work directory and a single-use
// registration on a machine that keeps its state, exactly what the process
// backend already means on Linux. The docs say so where the guarantee is
// claimed.
//
// Nothing in this file has been run on a Windows host by this repository's
// tests yet; the support matrix records it as built and unit-tested, and the
// beta is where it earns the next column.
const (
	// exeSuffix is what a runner binary's name ends in: bin/Runner.Listener
	// is bin/Runner.Listener.exe in the win-x64 archive.
	exeSuffix = ".exe"
	// configScript is the registration script a non-ephemeral pool runs.
	configScript = "config.cmd"
	// removeGrace is how long a directory removal keeps trying after it first
	// fails. Windows refuses to delete a file another process holds open, and
	// a runner that has just been terminated can leave a handle on its own
	// log or its work tree for a moment after the process is gone. One
	// RemoveAll would turn that moment into runners.cleanup_failed; a retry
	// with backoff turns it into a removal that converges.
	removeGrace = 30 * time.Second
)

// noShellDetail is why the process backend is unavailable on a Windows host
// with no command interpreter, which is a host so unusual it is worth naming.
const noShellDetail = "cmd.exe is not on PATH, so nothing actions/runner starts could run here; " +
	"the process backend on Windows needs the command interpreter and PowerShell that every Windows installation ships, " +
	"so check the service's PATH and the ComSpec variable"

// HasShell reports whether this host has a command interpreter, which the
// runner's config.cmd and every `run:` step need.
func HasShell() bool {
	_, err := exec.LookPath("cmd")
	return err == nil
}

// jobs tracks the job object each runner this agent started was put in, by
// pid. A job object is Windows' answer to a process group: terminating it
// terminates every process that was ever assigned to it, including the
// Runner.Worker the listener spawns per job and whatever msbuild that worker
// started, so killing a runner does not leave its children behind on a
// machine whose state persists.
var jobs = struct {
	sync.Mutex
	byPID map[int]windows.Handle
}{byPID: map[int]windows.Handle{}}

// detachRunner starts the runner in a console process group of its own, so a
// Ctrl-C at the agent's console does not reach it. The job object is added
// after the start, because a process is assigned to a job by handle and the
// handle does not exist before then.
func detachRunner(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: windows.CREATE_NEW_PROCESS_GROUP}
}

// adoptRunner puts a started runner into a job object of its own.
//
// The job deliberately has no kill-on-close limit: when the agent exits, its
// handle closes and the job object simply stops being tracked, so an agent
// restart leaves the runner running exactly as it does on Linux. What the job
// is for is the other direction -- a runner this agent decides to kill takes
// its whole tree with it.
//
// Every failure here is swallowed. A runner that could not be put in a job is
// still a runner; killing it later falls back to taskkill, which walks the
// tree by parentage instead, and a start that succeeded must not be reported
// as a failure over an accounting detail.
func adoptRunner(cmd *exec.Cmd) {
	if cmd.Process == nil {
		return
	}
	job, err := windows.CreateJobObject(nil, nil)
	if err != nil {
		return
	}
	proc, err := windows.OpenProcess(windows.PROCESS_SET_QUOTA|windows.PROCESS_TERMINATE, false, uint32(cmd.Process.Pid))
	if err != nil {
		_ = windows.CloseHandle(job)
		return
	}
	defer windows.CloseHandle(proc)
	if err := windows.AssignProcessToJobObject(job, proc); err != nil {
		_ = windows.CloseHandle(job)
		return
	}
	jobs.Lock()
	jobs.byPID[cmd.Process.Pid] = job
	jobs.Unlock()
}

// releaseRunner closes the job handle of a runner that has been reaped.
func releaseRunner(pid int) {
	jobs.Lock()
	defer jobs.Unlock()
	if job, ok := jobs.byPID[pid]; ok {
		_ = windows.CloseHandle(job)
		delete(jobs.byPID, pid)
	}
}

// signalRunner can only kill on Windows. There is no interrupt to deliver to
// a process that has no console of ours to receive it on -- a service has no
// console at all -- so a drain is a kill here, and the docs say so.
//
// A runner this agent started is killed through its job object, tree and all.
// One adopted after an agent restart has no job handle here, so taskkill /T
// walks its tree by parent pid instead; that misses a child whose parent has
// already exited, which is the honest limit of the adopted case.
func signalRunner(proc *os.Process, sig syscall.Signal) error {
	if sig != syscall.SIGKILL {
		return syscall.EWINDOWS
	}
	jobs.Lock()
	job, ok := jobs.byPID[proc.Pid]
	jobs.Unlock()
	if ok {
		if err := windows.TerminateJobObject(job, 137); err == nil {
			return nil
		}
	}
	out, err := exec.Command("taskkill", "/T", "/F", "/PID", strconv.Itoa(proc.Pid)).CombinedOutput()
	if err != nil {
		if !processAlive(proc.Pid) {
			return os.ErrProcessDone
		}
		return fmt.Errorf("taskkill: %w: %s", err, lastLine(out))
	}
	return nil
}

// stillActive is the exit code GetExitCodeProcess reports for a process that
// has not exited (STATUS_PENDING).
const stillActive = 259

// processAlive reports whether a pid is still running. Opening the process
// for limited query is the least privilege that answers the question, and a
// pid nothing answers for is a process that is gone.
func processAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	h, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(pid))
	if err != nil {
		return false
	}
	defer windows.CloseHandle(h)
	var code uint32
	if err := windows.GetExitCodeProcess(h, &code); err != nil {
		return false
	}
	return code == stillActive
}

// platformChildEnv is what a Windows runner needs beyond HOME and PATH. The
// .NET runtime refuses to start without SystemRoot, cmd.exe wants ComSpec and
// PATHEXT, and the runner's own home is USERPROFILE rather than HOME. These
// are passed through from the agent rather than invented, and the list is an
// allowlist for the same reason childEnv's is: a job must not inherit
// whatever else happened to be in the service's environment.
func platformChildEnv(dir string) []string {
	env := []string{"USERPROFILE=" + dir}
	for _, k := range []string{
		"SystemRoot", "SystemDrive", "windir", "ComSpec", "PATHEXT",
		"TEMP", "TMP", "ProgramData", "ProgramFiles", "ProgramFiles(x86)", "ProgramW6432",
		"ALLUSERSPROFILE", "LOCALAPPDATA", "APPDATA", "PSModulePath",
		"NUMBER_OF_PROCESSORS", "PROCESSOR_ARCHITECTURE", "USERNAME", "USERDOMAIN",
	} {
		if v, ok := os.LookupEnv(k); ok {
			env = append(env, k+"="+v)
		}
	}
	return env
}
