package agent

import "math"

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
