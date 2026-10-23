package backend

import (
	"context"
	"errors"
	"math"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"
)

// ScratchUse is one in-memory folder's use at a sample: the path it is mounted
// at in the container, what is written in it, and its ceiling.
//
// It exists because a folder that fills is the one failure of the setting a job
// cannot show afterwards. A compiler deletes its work directory as the link
// fails, and the runner's own output is the runner's diagnostics, never a
// step's "no space left on device"; by the time the job is reported failed the
// folder is empty again. So each sample measures the folders, and the fullest
// each got is kept on the job, where the explanation can read it.
type ScratchUse struct {
	Path      string `json:"path"`
	UsedBytes int64  `json:"used_bytes"`
	SizeBytes int64  `json:"size_bytes"`
}

// Why a container's in-memory folders were not measured. They are stable codes:
// they travel in Stats.ScratchUnmeasured, are stored with a runner's sample and
// are read by the controller, which turns them into the sentence that tells an
// operator what to change. Each is a different change, which is why there are
// four and not one.
const (
	// ScratchExecRefused is an engine, or something in front of it, that will
	// not run a command in a container: a socket proxy that blocks exec, an
	// authorisation plugin, an engine without the endpoint.
	ScratchExecRefused = "exec_refused"
	// ScratchNoDF is an image with no df to run.
	ScratchNoDF = "no_df"
	// ScratchUnreadable is a df that ran and answered with something that is not
	// a table of the folders asked for, or an engine that failed in some way
	// that is not a refusal. Either way the figure is not guessed.
	ScratchUnreadable = "unreadable"
	// ScratchTimedOut is an engine that took the request and did not answer it
	// in the time a sample can spare.
	ScratchTimedOut = "timed_out"
)

// scratchTimeout is how long one container's folders are given to be measured.
// The agent gives a whole sample five seconds, and the stats call the
// measurement runs beside takes a second of the daemon's at the least, so the
// command must be quick or be abandoned: a sample is never worth losing to it.
// It is a variable so a test can shorten it.
var scratchTimeout = 2 * time.Second

// execer is the one call a measurement needs of the engine.
type execer interface {
	ContainerExec(ctx context.Context, id string, cmd, env []string) (ExecResult, error)
}

// scratchReading is what measuring one container came to: the folders that
// were measured, and, when folders exist that were not, why.
type scratchReading struct {
	use        []ScratchUse
	unmeasured string
}

// collectScratch gathers the readings of a sample's containers, the runner's
// first. Each was started with a budget, so waiting for it is bounded. When
// some folders could not be measured the reason of the first is the sample's,
// beside the folders that were: a pair whose image store cannot be read still
// has a work folder that can.
func collectScratch(readings []<-chan scratchReading) (use []ScratchUse, unmeasured string) {
	for _, ch := range readings {
		r := <-ch
		use = append(use, r.use...)
		if unmeasured == "" {
			unmeasured = r.unmeasured
		}
	}
	return use, unmeasured
}

// scratchPaths is every tmpfs an inspected container was created with, in the
// order of their paths, so a sample reads the same way twice: its Tmpfs
// mounts, and a volume mounted on a tmpfs, which is how a docker-in-docker
// sidecar keeps the work folder it shares with its runner. A path that is not
// absolute is left out, because it would reach df as an option. Nothing for a
// container that could not be inspected or keeps nothing in memory.
func scratchPaths(insp *ContainerInspect, err error) []string {
	if err != nil || insp == nil || insp.HostConfig == nil {
		return nil
	}
	var paths []string
	for p := range insp.HostConfig.Tmpfs {
		if strings.HasPrefix(p, "/") {
			paths = append(paths, p)
		}
	}
	for _, m := range insp.HostConfig.Mounts {
		if m.Type == "volume" && m.VolumeOptions != nil && m.VolumeOptions.DriverConfig != nil &&
			m.VolumeOptions.DriverConfig.Options["type"] == "tmpfs" && strings.HasPrefix(m.Target, "/") {
			paths = append(paths, m.Target)
		}
	}
	if len(paths) == 0 {
		return nil
	}
	sort.Strings(paths)
	return paths
}

// probeScratch starts measuring a container's folders and returns the channel
// the reading arrives on, so the measurement runs beside the stats call and not
// after it. The channel holds the reading whether or not anyone collects it: a
// sample that fails for another reason abandons it, and nothing is left waiting
// to send.
func probeScratch(ctx context.Context, x execer, id string, paths []string) <-chan scratchReading {
	out := make(chan scratchReading, 1)
	if len(paths) == 0 {
		out <- scratchReading{}
		return out
	}
	// The budget is read here, in the sampler's own goroutine, not in the
	// measurement's: a variable a test shortens must not be raced for.
	budget := scratchTimeout
	go func() {
		ctx, cancel := context.WithTimeout(ctx, budget)
		defer cancel()
		out <- measureScratch(ctx, x, id, paths)
	}()
	return out
}

// measureScratch reads how full each folder is by running df in the container.
//
// The folder is not read from the host through /proc/<pid>/root, which looks
// simpler and needs no command at all. The kernel lets only a process with the
// container's own uid, or CAP_SYS_PTRACE, look through another process's root,
// and the agent has neither where it is installed as documented: an
// unprivileged account on a host whose /proc hides every other user's
// processes, or a container in a pid namespace of its own. It would read
// nothing and say nothing. A command run through the engine's API needs only
// what the agent already has, a socket, and works the same for Docker, Podman
// and a rootless engine.
//
// The command is df with POSIX output in kilobytes and the folders as its
// arguments: read-only, one process for as long as it takes to stat a few
// mounts, and no shell. A folder is only measured where it is itself a mount
// point; df answering with the root disk for a path means no folder was given.
func measureScratch(ctx context.Context, x execer, id string, paths []string) scratchReading {
	if len(paths) == 0 {
		return scratchReading{}
	}
	cmd := append([]string{"df", "-P", "-k"}, paths...)
	res, err := x.ContainerExec(ctx, id, cmd, []string{"LC_ALL=C"})
	if err != nil {
		return scratchReading{unmeasured: whyNotMeasured(err)}
	}
	// The shell's codes for a command it could not run and one it could not
	// find. The daemon's sentence is also the answer when the code did not
	// arrive: an image with no df is the commonest way this fails, and the
	// advice for it is different from every other.
	if res.ExitCode == 126 || res.ExitCode == 127 ||
		(res.ExitCode < 0 && strings.Contains(res.Stderr, "executable file") && strings.Contains(res.Stderr, "not found")) {
		return scratchReading{unmeasured: ScratchNoDF}
	}
	// Both streams are read: df prints its table on stdout, but an engine that
	// hands the output back without framing it leaves it in the stream the
	// client keeps unframed text in, and a row is only believed for the folder
	// it names in any case.
	use := parseDF(res.Stdout+"\n"+res.Stderr, paths)
	if len(use) == 0 {
		return scratchReading{unmeasured: ScratchUnreadable}
	}
	return scratchReading{use: use}
}

// whyNotMeasured names the reason an exec could not be run. A container that is
// gone or not running yet is nothing to report: it is a runner coming or going,
// and the next sample will find it.
func whyNotMeasured(err error) string {
	switch status := StatusCode(err); {
	case errors.Is(err, ErrNotFound), status == http.StatusConflict:
		return ""
	case status == http.StatusUnauthorized, status == http.StatusForbidden,
		status == http.StatusMethodNotAllowed, status == http.StatusNotImplemented:
		return ScratchExecRefused
	case errors.Is(err, ErrDaemonBusy), errors.Is(err, context.DeadlineExceeded):
		return ScratchTimedOut
	default:
		return ScratchUnreadable
	}
}

// parseDF reads the table df -P -k prints, in the order of the folders asked
// for. Each row is trusted for a folder only when its mount point is that
// folder exactly and its figures are a size and a use within it. Anything else
// is skipped: the header, df's warnings on the same stream, and the row for the
// disk under a folder that was never mounted. What is returned is therefore
// measured and never made up, because a zero for a folder nobody measured reads
// as a folder with room, which is the one wrong answer this must not give.
func parseDF(out string, paths []string) []ScratchUse {
	asked := make(map[string]bool, len(paths))
	for _, p := range paths {
		asked[p] = true
	}
	found := make(map[string]ScratchUse, len(paths))
	for _, line := range strings.Split(out, "\n") {
		// Filesystem, 1024-blocks, Used, Available, Capacity, Mounted on.
		f := strings.Fields(line)
		if len(f) < 6 {
			continue
		}
		mount := strings.Join(f[5:], " ")
		if !asked[mount] {
			continue
		}
		if _, dup := found[mount]; dup {
			continue
		}
		blocks, errSize := strconv.ParseInt(f[1], 10, 64)
		used, errUsed := strconv.ParseInt(f[2], 10, 64)
		if errSize != nil || errUsed != nil || blocks <= 0 || used < 0 || used > blocks || blocks > math.MaxInt64>>10 {
			continue
		}
		found[mount] = ScratchUse{Path: mount, UsedBytes: used << 10, SizeBytes: blocks << 10}
	}
	var use []ScratchUse
	for _, p := range paths {
		if u, ok := found[p]; ok {
			use = append(use, u)
		}
	}
	return use
}
