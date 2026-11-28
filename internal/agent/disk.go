package agent

import (
	"fmt"
	"math"
	"slices"

	"github.com/eyupio/zoomies/internal/backend"
)

// diskFromStatfs turns a filesystem's raw block counts into bytes, and refuses
// figures that cannot describe one.
//
// Separate from the syscall so that it can be tested at all: the values that
// break it -- a block count wide enough to overflow a signed 64-bit multiply,
// an available count above the total -- cannot be staged on a real filesystem,
// and every check here would otherwise be a line nothing could ever fail.
//
// The counts arrive unsigned because that is what every platform's statfs uses,
// and the danger is exactly there: converted straight to int64 a large enough
// value arrives as a negative, and a negative number of bytes free would pass
// every "was this measured" test downstream and read as a host with room.
func diskFromStatfs(blocks, bavail uint64, bsize int64) (total, avail int64, ok bool) {
	if bsize <= 0 {
		return 0, 0, false
	}
	if blocks > math.MaxInt64/uint64(bsize) || bavail > math.MaxInt64/uint64(bsize) {
		return 0, 0, false
	}
	total, avail = int64(blocks)*bsize, int64(bavail)*bsize
	if total <= 0 || avail < 0 || avail > total {
		return 0, 0, false
	}
	return total, avail, true
}

// diskRefusal says why a new runner may not start for want of disk, or "" when
// it may. It measures the filesystem holding the agent's work directory only:
// that is where the process backend keeps a runner and its job. A container
// backend keeps images and layers under the daemon's own root, which this does
// not look at, so the reserve is meant for hosts running the process backend.
//
// An unmeasurable disk refuses too. A reserve that waves work through when it
// cannot see the disk would fail exactly when it is needed.
func (a *Agent) diskRefusal() string {
	if a.opts.StopFreeGiB <= 0 {
		return ""
	}
	measure := a.opts.diskSpace
	if measure == nil {
		measure = diskSpace
	}
	_, free, ok := measure(a.opts.WorkDir)
	if !ok {
		return "cannot measure work directory free disk; new runners paused"
	}
	if float64(free) < a.opts.StopFreeGiB*(1<<30) {
		return fmt.Sprintf("work directory has %.1f GiB free, below %.1f GiB; new runners paused", float64(free)/(1<<30), a.opts.StopFreeGiB)
	}
	return ""
}

// diskBackends is the probes as the controller should see them while the
// reserve holds: every backend unavailable, with the reason as its detail, so
// an older controller that only knows availability stops placing runners here.
// It is applied to the heartbeat's Backends field alone. The probes the agent
// sizes the host from keep their CPU and memory figures, which a pause must not
// replace with the agent's own, smaller cgroup limits. The operator's cordon is
// left as it was.
func (a *Agent) diskBackends(infos []backend.Info) []backend.Info {
	why := a.diskRefusal()
	if why == "" {
		return infos
	}
	paused := slices.Clone(infos)
	for i := range paused {
		paused[i].Available = false
		paused[i].Detail = why
	}
	return paused
}
