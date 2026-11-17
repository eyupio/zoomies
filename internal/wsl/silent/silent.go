// Package silent is the logic of the Windows launcher the scheduled tasks run
// (cmd/zoomies-wsl), kept apart from its main so that it is tested on every
// platform.
//
// Why a launcher at all: wsl.exe and powershell.exe are console programs.
// Task Scheduler gives a console program a console, and where Windows Terminal
// is the default terminal that console is a visible terminal window, every
// time the task fires. -WindowStyle Hidden cannot help, because it is read
// after the window exists, and the task's own Hidden flag only hides the task
// from the list. The launcher is built for the GUI subsystem, so it never gets
// a console, and starts wsl.exe with CREATE_NO_WINDOW, so neither does its
// child.
package silent

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"strings"
	"unicode/utf16"
)

// HoldSeconds is how long the command that starts the distro holds it: long
// enough for systemd to start the agent, which then holds it open for good.
const HoldSeconds = "120"

// Runner starts wsl.exe. Both calls run it with no window.
type Runner interface {
	// Output runs wsl.exe with args and returns its standard output.
	Output(ctx context.Context, args ...string) ([]byte, error)
	// Run runs wsl.exe with args and returns its exit code. A non-zero exit
	// is a code and a nil error; err is only for a process that could not be
	// started or waited for.
	Run(ctx context.Context, args ...string) (code int, err error)
}

// Decode turns wsl.exe's output into text. It prints UTF-16LE, which arrives
// with NULs between the letters, with or without a byte-order mark.
func Decode(b []byte) string {
	if len(b) >= 2 && (b[0] == 0xFF && b[1] == 0xFE || len(b) > 3 && b[1] == 0 && b[3] == 0) {
		if b[0] == 0xFF && b[1] == 0xFE {
			b = b[2:]
		}
		u := make([]uint16, 0, len(b)/2)
		for i := 0; i+1 < len(b); i += 2 {
			u = append(u, uint16(b[i])|uint16(b[i+1])<<8)
		}
		return string(utf16.Decode(u))
	}
	return strings.ReplaceAll(strings.TrimPrefix(string(b), "\xef\xbb\xbf"), "\x00", "")
}

// IsRunning reports whether distro is in the output of `wsl -l --running -q`.
// Names are compared whole and ignoring case, as Windows does.
func IsRunning(out []byte, distro string) bool {
	if distro == "" {
		return false
	}
	for _, l := range strings.Split(Decode(out), "\n") {
		if strings.EqualFold(strings.TrimSpace(l), distro) {
			return true
		}
	}
	return false
}

// StartArgs is the command that starts the distro and holds it.
func StartArgs(distro string) []string {
	return []string{"-d", distro, "--exec", "/bin/sleep", HoldSeconds}
}

// Ensure starts distro unless it is already running, and returns the exit code
// the launcher should end with. Nothing is written to a console: every line
// goes to log, which the launcher points at a file, so a failure is
// diagnosable without a terminal. Failing to ask what is running is not
// fatal, because starting a distro that is already up does no harm.
func Ensure(ctx context.Context, r Runner, distro string, log io.Writer) int {
	if distro == "" {
		fmt.Fprintln(log, "no distro name was given, so there is nothing to start")
		return 2
	}
	out, err := r.Output(ctx, "-l", "--running", "-q")
	switch {
	case err != nil:
		fmt.Fprintf(log, "asking wsl.exe what is running failed (%v); starting %s anyway\n", err, distro)
	case IsRunning(bytes.TrimSpace(out), distro):
		return 0
	}
	code, err := r.Run(ctx, StartArgs(distro)...)
	if err != nil {
		fmt.Fprintf(log, "starting %s failed: %v\n", distro, err)
		return 1
	}
	if code != 0 {
		fmt.Fprintf(log, "wsl.exe exited with %d while starting %s\n", code, distro)
		return code
	}
	return 0
}
