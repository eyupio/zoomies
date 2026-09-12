//go:build windows

package agent

import "golang.org/x/sys/windows"

// diskSpace reports the total and available bytes on the volume holding path.
//
// The first figure GetDiskFreeSpaceEx returns is the space available to the
// calling user, after quotas, which is the Windows spelling of the reserve
// disk_unix.go explains: what a runner can actually write, not what the
// volume has free.
func diskSpace(path string) (total, avail int64, ok bool) {
	p, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return 0, 0, false
	}
	var availToCaller, totalBytes, totalFree uint64
	if err := windows.GetDiskFreeSpaceEx(p, &availToCaller, &totalBytes, &totalFree); err != nil {
		return 0, 0, false
	}
	return int64(totalBytes), int64(availToCaller), true
}
