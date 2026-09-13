//go:build drill

package drill

import (
	"fmt"
	"os"
	"path/filepath"
)

// The process backend expects an actions/runner release laid out under
// <work-dir>/_tools/<version>/, and skips its download entirely when
// bin/Runner.Listener is already there. That short-circuit is what lets a
// drill run with no network and no real runner: we stage a stub.
//
// A stub is honest here in a way it would not be elsewhere. What this tier
// qualifies is Zoomies -- that the controller decides to create a runner, that
// the task reaches a remote agent, that the backend puts a real process on the
// host with the right layout and environment, and that everything is taken
// away again afterwards. Whether Microsoft's runner binary can talk to GitHub
// is not this repository's behaviour, and putting the real one here would
// trade the whole drill for a download.
//
// What the stub does have to be is a *real process* that stays up until it is
// stopped, because the lifecycle under test is exactly "it is running, then it
// is not". It ends the way a real ephemeral runner ends -- by exiting once its
// job is over -- rather than by being killed, so that what the drill watches
// is Zoomies noticing a workload finished rather than Zoomies tearing one down.
const (
	toolsDirName = "_tools"
	listenerPath = "bin/Runner.Listener"
	// stubVersion is what the pool pins, so the backend looks for the tree we
	// staged rather than the version it would otherwise fetch.
	stubVersion = "2.999.0-drill"
)

// stubRunner is the script staged as Runner.Listener.
//
// It writes a file the drill can watch for, so "the workload started" is
// observed from the host rather than inferred from a Zoomies row, then waits
// to be killed. The trap makes it exit promptly on the backend's TERM, which
// is what a drill measuring recovery time needs.
//
// The trap is installed before that file exists, and the order is the whole
// contract with the drill: a shell that has not installed its handler yet is
// killed outright by the signal, so a drill that signals on the strength of
// the marker must not be able to arrive in between the two. Losing that race
// costs the drill the thing it is there to measure -- cleanup never runs, the
// marker it watches is left behind, and a prompt exit is recorded as a process
// killed by a signal.
const stubRunner = `#!/bin/sh
set -eu
: "${ACTIONS_RUNNER_INPUT_JITCONFIG:=}"
dir="$(cd "$(dirname "$0")/.." && pwd)"
cleanup() { rm -f "$dir/drill-started"; exit 0; }
# TERM is the backend stopping us, which is what a drain or a forced removal
# looks like from in here.
trap cleanup TERM INT
# Record that a real process reached this point, and with what. The drill waits
# for this, so nothing above it may be skippable and nothing below it may be
# needed to handle a signal.
{
  echo "pid=$$"
  echo "jit_present=$([ -n "${ACTIONS_RUNNER_INPUT_JITCONFIG}" ] && echo yes || echo no)"
} > "$dir/drill-started"
# Otherwise wait for the drill to say the job is over, and exit the way an
# ephemeral runner does. The sleep is short and backgrounded so the trap is
# handled promptly rather than after a long uninterruptible sleep.
while [ ! -f "$dir/drill-stop" ]; do
  sleep 0.2 &
  wait $!
done
cleanup
`

// stageStubRunner writes the stub release into an agent's work directory. It
// must run before the agent starts, because the backend looks for the tree the
// first time it is asked to create anything.
func stageStubRunner(workDir string) error {
	dir := filepath.Join(workDir, toolsDirName, stubVersion)
	if err := os.MkdirAll(filepath.Join(dir, "bin"), 0o750); err != nil {
		return fmt.Errorf("staging the stub runner tree: %w", err)
	}
	listener := filepath.Join(dir, listenerPath)
	if err := os.WriteFile(listener, []byte(stubRunner), 0o750); err != nil {
		return fmt.Errorf("writing the stub runner: %w", err)
	}
	// config.sh is only reached on the registration-token path; an ephemeral
	// pool hands over a JIT config instead. Staging it anyway means a drill
	// that deliberately uses a non-ephemeral pool fails on the behaviour it is
	// testing rather than on a missing file.
	config := filepath.Join(dir, "config.sh")
	if err := os.WriteFile(config, []byte("#!/bin/sh\nexit 0\n"), 0o750); err != nil {
		return fmt.Errorf("writing the stub config script: %w", err)
	}
	return nil
}

// startedMarker is the file the stub writes inside a runner's directory. Its
// presence is the drill's evidence that a workload really ran on this host.
func startedMarker(runnerDir string) string {
	return filepath.Join(runnerDir, "drill-started")
}

// finishJob tells the stub its job is over, so it exits as an ephemeral runner
// does. The drill calls this instead of killing anything: what is under test is
// Zoomies noticing a finished workload, and killing it would test the reaper.
func finishJob(runnerDir string) error {
	return os.WriteFile(filepath.Join(runnerDir, "drill-stop"), []byte("done\n"), 0o640)
}
