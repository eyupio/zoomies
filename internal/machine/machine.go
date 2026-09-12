// Package machine works out what host this process is running on: which
// distribution and release, and how much machine there is to use.
//
// It is its own package because two callers need the same answer and neither
// should depend on the other: the agent reports these facts to the controller
// so a pool asking for Ubuntu 24.04 can be placed correctly, and the
// configuration loader uses them to give an unnamed host a name that says what
// it is instead of whatever the cloud provider called it.
package machine

import (
	"context"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/eyupio/zoomies/internal/naming"
)

// Facts is what this host actually is: which distribution and release it
// runs, and how much of it there is.
//
// The controller needs all of it. The distribution and release decide whether
// a pool asking for Ubuntu 24.04 may be placed here -- "linux" cannot pick a
// runner image -- and the size is what lets a host be named for what it is
// rather than for whatever the cloud provider called it.
type Facts struct {
	// OS is the kernel, as Go names it: linux, darwin, windows.
	OS string
	// Distro and OSVersion are the distribution and its release, e.g.
	// "ubuntu" and "24.04". Empty when the host cannot say, which is not an
	// error: an unstated platform simply constrains nothing.
	Distro    string
	OSVersion string
	Arch      string
	// CPUs and MemoryMB are what this agent may actually use, which is not
	// always what the machine has -- an agent in a container gets the cgroup's
	// share, and reporting the host's 64 cores from inside a two-core cgroup
	// would size every pool wrong.
	CPUs     int
	MemoryMB int64
}

// Spec renders the machine in the naming grammar's terms, so that a host can
// be named for what it is.
func (m Facts) Spec(machine string) naming.Spec {
	os := m.Distro
	if naming.NormalizeOS(os) == "" {
		os = m.OS
	}
	return naming.Spec{
		CPUs:     float64(m.CPUs),
		MemoryGB: int(m.MemoryMB / 1024),
		OS:       os,
		Version:  m.OSVersion,
		Arch:     m.Arch,
		Suffix:   machine,
	}
}

// Detect works out what this host is. Every probe is best-effort: a field it
// cannot establish is left empty rather than guessed, because a host that
// claims the wrong platform is worse than one that claims none.
func Detect() Facts { return detect("/") }

// detect is Detect against an arbitrary filesystem root, which is what makes
// the Linux probes testable without a Linux host.
func detect(root string) Facts {
	m := Facts{OS: runtime.GOOS, Arch: runtime.GOARCH, CPUs: runtime.NumCPU()}
	switch runtime.GOOS {
	case "darwin":
		m.Distro = naming.OSMacOS
		m.OSVersion = macOSVersion()
		m.MemoryMB = sysctlMB("hw.memsize")
	case "windows":
		m.Distro = naming.OSWindows
		m.OSVersion, m.MemoryMB = windowsFacts()
	default:
		m.Distro, m.OSVersion = readOSRelease(filepath.Join(root, "etc", "os-release"))
		m.MemoryMB = readMemTotalMB(filepath.Join(root, "proc", "meminfo"))
	}
	// A cgroup limit is the real ceiling when there is one. The compose
	// deployment runs the controller's embedded agent in a container, so this
	// is the common case rather than an exotic one.
	if quota := cgroupCPUQuota(root); quota > 0 && quota < m.CPUs {
		m.CPUs = quota
	}
	if limit := cgroupMemoryMB(root); limit > 0 && (m.MemoryMB == 0 || limit < m.MemoryMB) {
		m.MemoryMB = limit
	}
	return m
}

// readOSRelease reads the distribution and release from /etc/os-release, the
// one file every Linux distribution Zoomies supports agrees on.
func readOSRelease(path string) (distro, version string) {
	b, err := os.ReadFile(path)
	if err != nil {
		return "", ""
	}
	for _, line := range strings.Split(string(b), "\n") {
		key, value, ok := strings.Cut(strings.TrimSpace(line), "=")
		if !ok {
			continue
		}
		value = strings.Trim(strings.TrimSpace(value), `"'`)
		switch key {
		case "ID":
			distro = value
		case "VERSION_ID":
			version = value
		}
	}
	// Normalise so that a host reporting "Ubuntu" and a pool asking for
	// "ubuntu" are the same platform. An ID outside the catalogue is dropped:
	// it would never match a pool, and keeping it would put a value in the
	// column that only looks like a promise.
	return naming.NormalizeOS(distro), version
}

// readMemTotalMB reads MemTotal from /proc/meminfo, which is in kibibytes.
func readMemTotalMB(path string) int64 {
	b, err := os.ReadFile(path)
	if err != nil {
		return 0
	}
	for _, line := range strings.Split(string(b), "\n") {
		rest, ok := strings.CutPrefix(line, "MemTotal:")
		if !ok {
			continue
		}
		fields := strings.Fields(rest)
		if len(fields) == 0 {
			return 0
		}
		kb, err := strconv.ParseInt(fields[0], 10, 64)
		if err != nil {
			return 0
		}
		return kb / 1024
	}
	return 0
}

// cgroupCPUQuota reads a cgroup v2 CPU limit as a whole number of cores,
// rounding up: a container limited to 1.5 cores can still burst onto two.
//
// "max" means unlimited, and so does an absent file, which is the case on a
// host running the agent directly.
func cgroupCPUQuota(root string) int {
	b, err := os.ReadFile(filepath.Join(root, "sys", "fs", "cgroup", "cpu.max"))
	if err != nil {
		return 0
	}
	fields := strings.Fields(string(b))
	if len(fields) != 2 || fields[0] == "max" {
		return 0
	}
	quota, err1 := strconv.ParseFloat(fields[0], 64)
	period, err2 := strconv.ParseFloat(fields[1], 64)
	if err1 != nil || err2 != nil || period <= 0 || quota <= 0 {
		return 0
	}
	return int(math.Ceil(quota / period))
}

// cgroupMemoryMB reads a cgroup v2 memory limit. "max" means unlimited.
func cgroupMemoryMB(root string) int64 {
	b, err := os.ReadFile(filepath.Join(root, "sys", "fs", "cgroup", "memory.max"))
	if err != nil {
		return 0
	}
	v := strings.TrimSpace(string(b))
	if v == "max" {
		return 0
	}
	bytes, err := strconv.ParseInt(v, 10, 64)
	if err != nil || bytes <= 0 {
		return 0
	}
	return bytes / (1024 * 1024)
}

// macOSVersion asks sw_vers, because macOS has no os-release. It is only ever
// a development host -- agents run on Linux -- so a failure is silent.
func macOSVersion() string { return firstWord(runProbe("sw_vers", "-productVersion")) }

// sysctlMB reads a byte-valued sysctl and returns it in mebibytes.
func sysctlMB(key string) int64 {
	bytes, err := strconv.ParseInt(firstWord(runProbe("sysctl", "-n", key)), 10, 64)
	if err != nil || bytes <= 0 {
		return 0
	}
	return bytes / (1024 * 1024)
}

// runProbe runs a short-lived read-only command, returning "" if anything at
// all goes wrong. Detection must never be the reason an agent fails to start.
func runProbe(name string, args ...string) string {
	path, err := exec.LookPath(name)
	if err != nil {
		return ""
	}
	// A bounded context, not just a wait delay: a probe that hangs must not
	// hold up the agent's start-up.
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, path, args...).Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

func firstWord(s string) string {
	if f := strings.Fields(s); len(f) > 0 {
		return f[0]
	}
	return ""
}

// Hostname is this machine's short name, or "localhost" when the host will not
// say. It is the discriminator in a canonical host name.
func Hostname() string {
	h, err := os.Hostname()
	if err != nil || strings.TrimSpace(h) == "" {
		return "localhost"
	}
	return h
}

// DefaultHostName is the name an unnamed host is given: what it is, then which
// one it is, in the grammar of internal/naming --
// "zoomies-16vcpu-32gb-ubuntu-2404-build01".
//
// A machine that would say nothing about itself falls back to its bare
// hostname rather than to the bare prefix, because "zoomies" is not a name and
// the hostname at least distinguishes two hosts.
func DefaultHostName() string {
	host := Hostname()
	spec := Detect().Spec(host)
	if spec.Empty() {
		return naming.Slug(host)
	}
	return spec.String()
}
