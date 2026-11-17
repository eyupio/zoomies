// Package wsl keeps a Zoomies agent alive inside a WSL2 distro.
//
// A WSL2 distro is not a machine that stays up. It is stopped when the last
// Windows-side session attached to it closes, whatever is running inside, so an
// agent that is correctly a systemd service (PID 1 is its parent) still goes
// away when the terminal that first started the distro is closed, and nothing
// brings it back after a reboot. This package holds the three things that
// prevent that: a session the agent owns for as long as it lives (Keepalive),
// systemd linger and a boot command inside the distro, and Windows scheduled
// tasks that start the distro when nobody has (Ensure).
//
// Nothing here restarts anything. Every step is a write that takes effect the
// next time the distro starts, or a process that only holds the distro up, so
// no step can end a job that is running: there is nothing to defer until idle.
//
// All host access goes through System so tests never touch the machine.
package wsl

import (
	"context"
	"os"
	"os/exec"
	"runtime"
	"strings"
)

// Conventional locations. WSL mounts the Windows system drive at /mnt/c unless
// the operator changed automount.root, in which case interop is probed for and
// the step skips with a reason rather than guessing.
const (
	ConfPath     = "/etc/wsl.conf"
	SchtasksPath = "/mnt/c/Windows/System32/schtasks.exe"
	WSLExePath   = "/mnt/c/Windows/System32/wsl.exe"
	CmdExePath   = "/mnt/c/Windows/System32/cmd.exe"
	lingerDir    = "/var/lib/systemd/linger"
	unitFile     = "/etc/systemd/system/zoomies-agent.service"
)

// System is everything this package needs from the host.
type System interface {
	GOOS() string
	Getenv(key string) string
	ReadFile(path string) ([]byte, error)
	WriteFile(path string, data []byte, perm os.FileMode) error
	MkdirAll(path string, perm os.FileMode) error
	Remove(path string) error
	Exists(path string) bool
	Geteuid() int
	// Run executes a command and returns its standard output. Standard error
	// is folded into the error so a refusal's wording reaches the operator.
	Run(ctx context.Context, name string, args ...string) ([]byte, error)
}

// OS is the System of the machine the process runs on.
type OS struct{}

func (OS) GOOS() string                      { return runtime.GOOS }
func (OS) Getenv(k string) string            { return os.Getenv(k) }
func (OS) ReadFile(p string) ([]byte, error) { return os.ReadFile(p) }
func (OS) WriteFile(p string, d []byte, m os.FileMode) error {
	return os.WriteFile(p, d, m)
}
func (OS) MkdirAll(p string, m os.FileMode) error { return os.MkdirAll(p, m) }
func (OS) Remove(p string) error                  { return os.Remove(p) }
func (OS) Geteuid() int                           { return os.Geteuid() }
func (OS) Exists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

// Run starts the command in /mnt/c when it exists: cmd.exe and the tasks it
// starts warn about, and sometimes refuse, a working directory that is a UNC
// path into the distro's own filesystem.
func (OS) Run(ctx context.Context, name string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	if _, err := os.Stat("/mnt/c"); err == nil {
		cmd.Dir = "/mnt/c"
	}
	var stderr strings.Builder
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		if msg := strings.TrimSpace(stderr.String()); msg != "" {
			return out, &runError{err: err, msg: msg}
		}
		if msg := strings.TrimSpace(string(out)); msg != "" {
			return out, &runError{err: err, msg: msg}
		}
	}
	return out, err
}

type runError struct {
	err error
	msg string
}

func (e *runError) Error() string { return e.err.Error() + ": " + e.msg }
func (e *runError) Unwrap() error { return e.err }

// Detect reports whether this process runs inside WSL2: WSL_DISTRO_NAME, which
// WSL sets in login sessions but not in systemd's services, or the WSL2 kernel's name in /proc/version, which is always there. WSL1 is
// not a target: it has no kernel to name and no distro lifetime to hold.
func Detect(sys System) bool {
	if sys.GOOS() != "linux" {
		return false
	}
	if sys.Getenv("WSL_DISTRO_NAME") != "" {
		return true
	}
	b, err := sys.ReadFile("/proc/version")
	return err == nil && strings.Contains(strings.ToLower(string(b)), "microsoft-standard-wsl2")
}

// Distro is the name Windows knows this distro by. A systemd service does not
// inherit WSL_DISTRO_NAME, so when the variable is missing it asks wslpath for
// the Windows path of "/" (\\wsl.localhost\Ubuntu\), which carries the
// name. An empty answer is honest: the Windows-side steps then skip, because
// starting the wrong distro would be worse than starting none.
func Distro(ctx context.Context, sys System) string {
	if d := strings.TrimSpace(sys.Getenv("WSL_DISTRO_NAME")); d != "" {
		return d
	}
	out, err := sys.Run(ctx, "wslpath", "-w", "/")
	if err != nil {
		return ""
	}
	return distroFromWindowsPath(string(out))
}

// distroFromWindowsPath pulls "Ubuntu" out of \\wsl.localhost\Ubuntu\ or the
// older \\wsl$\Ubuntu\ form.
func distroFromWindowsPath(p string) string {
	p = strings.TrimSpace(strings.ReplaceAll(p, "/", `\`))
	p = strings.TrimLeft(p, `\`)
	parts := strings.Split(p, `\`)
	if len(parts) < 2 {
		return ""
	}
	if h := strings.ToLower(parts[0]); h != "wsl.localhost" && h != "wsl$" {
		return ""
	}
	return parts[1]
}
