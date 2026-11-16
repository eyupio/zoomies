package agent

import (
	"fmt"
	"github.com/eyupio/zoomies/internal/backend"
	"math"
	"slices"
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

// Advertise the admission pause without changing the operator's cordon or
// cached runtime probes. An older controller already understands availability.
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
