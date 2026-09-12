package installer

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
)

// The Windows service manager. It has no build tag because nothing in it is
// Windows-specific to compile: it renders a command line and drives sc.exe,
// and both are things a test on any platform can watch through the fake
// command runner. The only Windows-only code the agent needs to run as a
// service is in cmd/zoomies, where the service control dispatcher lives.

// ServiceWindows registers the agent with the Windows service manager, which
// is how a Windows host keeps an agent running across logins and reboots the
// way systemd does on Linux.
const ServiceWindows ServiceKind = "windows"

// WindowsServiceName is the name the service is registered under. It is the
// unit name, so `sc.exe query zoomies-agent` and `systemctl status
// zoomies-agent` are the same question on either platform.
func WindowsServiceName(unit string) string {
	if unit == "" {
		return UnitController
	}
	return unit
}

// WindowsServiceCommand renders the command line the service manager runs.
//
// sc.exe takes the whole line as one argument and the service manager splits
// it again by the usual CommandLineToArgv rules, so every path is quoted
// whether or not it contains a space: C:\Program Files is the common case,
// and a line that only works for paths without spaces is a line that fails on
// the default install location.
//
// The log file is on the command line because a Windows service has no
// stderr anyone can read and no journal to send it to; --log-file is the
// agent's own answer to that.
func WindowsServiceCommand(spec ServiceSpec) (string, error) {
	if err := spec.defaults(); err != nil {
		return "", err
	}
	parts := []string{quoteWindowsArg(spec.ExecPath), spec.Command, "--config", quoteWindowsArg(spec.ConfigFile)}
	if spec.LogFile != "" {
		parts = append(parts, "--log-file", quoteWindowsArg(spec.LogFile))
	}
	return strings.Join(parts, " "), nil
}

// quoteWindowsArg wraps an argument in double quotes, escaping any it already
// carries, which is the CommandLineToArgv convention.
func quoteWindowsArg(s string) string {
	return `"` + strings.ReplaceAll(s, `"`, `\"`) + `"`
}

// windowsManager drives sc.exe for one service.
type windowsManager struct {
	unit string
	run  commandRunner
	// log is the file the service was installed with, remembered so Logs and
	// LogCommand can point at it.
	log string
}

func (m *windowsManager) Kind() ServiceKind { return ServiceWindows }

func (m *windowsManager) name() string { return WindowsServiceName(m.unit) }

// Install registers the service, replacing one that already exists so that a
// re-join with a different configuration path takes effect rather than
// leaving the old command line registered.
//
// The `key= value` spelling with the space after the equals sign is sc.exe's
// own: the key and the value are separate arguments to it, and writing them
// as one is the classic way an sc.exe invocation silently does nothing.
func (m *windowsManager) Install(ctx context.Context, spec ServiceSpec) (string, error) {
	spec.Unit = m.unit
	if err := spec.defaults(); err != nil {
		return "", err
	}
	cmd, err := WindowsServiceCommand(spec)
	if err != nil {
		return "", err
	}
	m.log = spec.LogFile
	if m.exists(ctx) {
		_, _ = m.run(ctx, "sc.exe", "stop", m.name())
		if _, err := m.run(ctx, "sc.exe", "delete", m.name()); err != nil {
			return "", err
		}
	}
	if _, err := m.run(ctx, "sc.exe", "create", m.name(),
		"binPath=", cmd,
		"start=", "auto",
		"DisplayName=", spec.Description,
	); err != nil {
		return "", fmt.Errorf("%w (this step needs an elevated prompt; run it as Administrator)", err)
	}
	// Failure actions are what Restart=always is on Linux: an agent that
	// crashes comes back, with a pause so a crash loop is not a busy loop.
	_, _ = m.run(ctx, "sc.exe", "failure", m.name(), "reset=", "86400", "actions=", "restart/5000/restart/5000/restart/30000")
	_, _ = m.run(ctx, "sc.exe", "description", m.name(), spec.Description)
	return m.name(), nil
}

func (m *windowsManager) Enable(ctx context.Context) error {
	_, err := m.run(ctx, "sc.exe", "config", m.name(), "start=", "auto")
	return err
}

func (m *windowsManager) Start(ctx context.Context) error {
	_, err := m.run(ctx, "sc.exe", "start", m.name())
	return err
}

// Stop is quiet about a service that is not registered or already stopped,
// because uninstall has to be safe to re-run.
func (m *windowsManager) Stop(ctx context.Context) error {
	if !m.exists(ctx) {
		return nil
	}
	out, err := m.run(ctx, "sc.exe", "stop", m.name())
	if err != nil && strings.Contains(out, "1062") {
		// ERROR_SERVICE_NOT_ACTIVE: stopping what is stopped is success.
		return nil
	}
	return err
}

func (m *windowsManager) Disable(ctx context.Context) error {
	if !m.exists(ctx) {
		return nil
	}
	_, err := m.run(ctx, "sc.exe", "config", m.name(), "start=", "disabled")
	return err
}

func (m *windowsManager) Remove(ctx context.Context) error {
	if !m.exists(ctx) {
		return nil
	}
	_, err := m.run(ctx, "sc.exe", "delete", m.name())
	return err
}

// Status reads the STATE line of `sc.exe query`, which is the word an
// operator would read themselves: RUNNING, STOPPED, START_PENDING.
func (m *windowsManager) Status(ctx context.Context) (string, error) {
	out, err := m.run(ctx, "sc.exe", "query", m.name())
	if err != nil {
		return "not installed", nil
	}
	for _, line := range strings.Split(out, "\n") {
		s := strings.TrimSpace(line)
		if !strings.HasPrefix(s, "STATE") {
			continue
		}
		fields := strings.Fields(s)
		return strings.ToLower(fields[len(fields)-1]), nil
	}
	return "installed", nil
}

// Logs tails the file the service writes, because a Windows service has no
// journal and the event log is not where a Go agent's structured lines go.
func (m *windowsManager) Logs(_ context.Context, n int) (string, error) {
	if m.log == "" {
		return "", errors.New("installer: the service was installed without a log file, so there is nothing to show; look at its --log-file argument in `sc.exe qc " + m.name() + "`")
	}
	return tailFile(m.log, n)
}

func (m *windowsManager) LogCommand() string {
	if m.log == "" {
		return "sc.exe qc " + m.name()
	}
	return "Get-Content -Wait " + quoteWindowsArg(m.log)
}

func (m *windowsManager) exists(ctx context.Context) bool {
	_, err := m.run(ctx, "sc.exe", "query", m.name())
	return err == nil
}

// prepareDirs creates the installer's directories and, on Windows, replaces
// each one's inherited ACL with one that only SYSTEM and Administrators can
// read.
//
// %ProgramData% is readable by every local user by default, and the agent's
// credentials live under it. On POSIX the 0600 on the credentials file is
// what keeps another account out; on Windows that mode is ignored and the
// directory's ACL is the only thing that does the same job. The platform and
// the runner are arguments so the icacls invocation is checked on every
// platform, not only the one it runs on.
func prepareDirs(ctx context.Context, goos string, run commandRunner, dirs ...string) error {
	for _, dir := range dirs {
		if err := os.MkdirAll(dir, 0o750); err != nil {
			return fmt.Errorf("installer: creating %s: %w", dir, err)
		}
		if err := restrictDir(ctx, goos, dir, run); err != nil {
			return fmt.Errorf("installer: restricting %s to administrators: %w (run this from an elevated prompt)", dir, err)
		}
	}
	return nil
}

// restrictDir is the ACL half of prepareDirs.
func restrictDir(ctx context.Context, goos, dir string, run commandRunner) error {
	if goos != "windows" {
		return nil
	}
	_, err := run(ctx, "icacls", dir, "/inheritance:r",
		"/grant:r", "SYSTEM:(OI)(CI)F", "/grant:r", "Administrators:(OI)(CI)F")
	return err
}

// windowsServiceInstalled reports whether the agent service is registered on
// this host, for uninstall, which has no unit file to look for.
func windowsServiceInstalled(ctx context.Context, unit string) bool {
	if lookPath("sc.exe") == "" {
		return false
	}
	_, err := runCommand(ctx, "sc.exe", "query", WindowsServiceName(unit))
	return err == nil
}
