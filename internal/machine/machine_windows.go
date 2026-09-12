//go:build windows

package machine

import (
	"strconv"
	"unsafe"

	"golang.org/x/sys/windows"
)

// windowsFacts reports the Windows release and the physical memory.
//
// The release is the marketing number a pool would ask for -- "10" or "11" --
// rather than the kernel's 10.0.x, because a pool that says os=windows
// version=11 is written by a person, and Windows 11 reports itself as
// kernel 10.0 build 22000 and up. Server editions report the same kernel and
// are called "10" here, which is the honest limit of the version API.
func windowsFacts() (version string, memoryMB int64) {
	v := windows.RtlGetVersion()
	switch {
	case v == nil:
	case v.MajorVersion == 10 && v.BuildNumber >= 22000:
		version = "11"
	default:
		version = strconv.Itoa(int(v.MajorVersion))
	}
	return version, physicalMemoryMB()
}

// memoryStatusEx is MEMORYSTATUSEX from sysinfoapi.h.
type memoryStatusEx struct {
	length               uint32
	memoryLoad           uint32
	totalPhys            uint64
	availPhys            uint64
	totalPageFile        uint64
	availPageFile        uint64
	totalVirtual         uint64
	availVirtual         uint64
	availExtendedVirtual uint64
}

var procGlobalMemoryStatusEx = windows.NewLazySystemDLL("kernel32.dll").NewProc("GlobalMemoryStatusEx")

// physicalMemoryMB asks kernel32 directly: x/sys/windows wraps the version
// and disk calls but not this one, and one procedure lookup is smaller than
// a dependency that does.
func physicalMemoryMB() int64 {
	var st memoryStatusEx
	st.length = uint32(unsafe.Sizeof(st))
	r, _, _ := procGlobalMemoryStatusEx.Call(uintptr(unsafe.Pointer(&st)))
	if r == 0 {
		return 0
	}
	return int64(st.totalPhys / (1024 * 1024))
}
