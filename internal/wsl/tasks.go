package wsl

import (
	"context"
	"fmt"
	"strings"

	"github.com/eyupio/zoomies/internal/wsl/silent"
)

// The scheduled tasks Windows runs to start the distro when nothing else has.
// Both run the same launcher, which checks first, so a task firing while the
// distro is already up does nothing.
const (
	taskOnStart  = `Zoomies\WslOnStart`
	taskEvery5   = `Zoomies\WslEvery5Minutes`
	launcherName = "zoomies-wsl.exe"
	// oldScriptName is what the tasks ran before the launcher: a PowerShell
	// script, whose console flashed (or opened Windows Terminal) on every run.
	oldScriptName = "wsl-ensure.ps1"
)

// legacyTasks are task names an earlier arrangement used for the same two jobs
// (start at boot, keep the distro up every five minutes). They are removed, and
// only when their definition runs wsl.exe, so a task someone else made under
// one of these names for another purpose is left alone.
var legacyTasks = []string{"Zoomies WSL", "Zoomies WSL keepalive"}

// decodeConsole turns schtasks' output into text. Piped through interop it is
// UTF-16LE with or without a byte-order mark.
func decodeConsole(b []byte) string { return silent.Decode(b) }

// taskAction is what a task runs: the launcher, with the distro. The launcher is
// a GUI-subsystem program, so Task Scheduler gives it no console, and it starts
// wsl.exe with no window either (see internal/wsl/silent).
func taskAction(winLauncher, distro string) string {
	return `"` + winLauncher + `" ensure "` + distro + `"`
}

// xmlText escapes what schtasks escapes in an exported definition, so a path
// or a distro name with an ampersand still matches.
func xmlText(s string) string {
	return strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;").Replace(s)
}

type taskSpec struct {
	name       string
	args       []string // the schedule part of /Create
	trigger    string   // what the exported XML must contain to be this schedule
	needsAdmin bool
}

func taskSpecs() []taskSpec {
	return []taskSpec{
		{name: taskOnStart, args: []string{"/SC", "ONSTART"}, trigger: "<BootTrigger>", needsAdmin: true},
		{name: taskEvery5, args: []string{"/SC", "MINUTE", "/MO", "5"}, trigger: "<Interval>PT5M</Interval>"},
	}
}

// ensureTask creates or repairs one task. It is current when it exists, has the
// right schedule and runs the launcher for this distro; otherwise it is created
// again with /F, which replaces the task in place, so an installation that still
// has the PowerShell version is repaired under its own name, with no duplicate.
// No credentials are ever passed: without /RU and /RP a task runs as the
// current user and only while they are signed in, and an ONSTART task needs
// elevation, which is reported rather than asked for.
func ensureTask(ctx context.Context, sys System, spec taskSpec, winLauncher, distro string, dry bool) Step {
	step := Step{Name: "task " + spec.name}
	action := taskAction(winLauncher, distro)
	if out, err := sys.Run(ctx, SchtasksPath, "/Query", "/TN", spec.name, "/XML"); err == nil {
		xml := decodeConsole(out)
		if strings.Contains(xml, spec.trigger) && strings.Contains(xml, xmlText(winLauncher)) &&
			strings.Contains(xml, xmlText(`ensure "`+distro+`"`)) && !strings.Contains(strings.ToLower(xml), "powershell") {
			step.Status, step.Detail = StatusOK, "already in place"
			return step
		}
	}
	if dry {
		step.Status, step.Detail = StatusPending, "create the Windows scheduled task "+spec.name+" so it starts the distro without opening a window"
		return step
	}
	args := append([]string{"/Create", "/TN", spec.name, "/TR", action}, spec.args...)
	args = append(args, "/F")
	out, err := sys.Run(ctx, SchtasksPath, args...)
	if err != nil {
		step.Status, step.Detail = taskFailure(spec.name, err, out)
		return step
	}
	step.Status, step.Detail = StatusChanged, "created"
	return step
}

// taskFailure words a schtasks refusal for the operator.
func taskFailure(name string, err error, out []byte) (Status, string) {
	msg := strings.TrimSpace(decodeConsole([]byte(err.Error())) + " " + decodeConsole(out))
	msg = strings.ReplaceAll(msg, "\x00", "")
	if strings.Contains(strings.ToLower(msg), "access is denied") {
		return StatusWarn, "Windows refused it without elevation, so it was skipped; " + name + " needs an elevated session once (this never asks for a password)"
	}
	return StatusWarn, fmt.Sprintf("schtasks failed: %s", msg)
}

// removeLegacyTask deletes a task an earlier arrangement left, when it is one
// of ours (its definition runs wsl.exe). Absent is the normal case.
func removeLegacyTask(ctx context.Context, sys System, name string, dry bool) (Step, bool) {
	step := Step{Name: "old task " + name}
	out, err := sys.Run(ctx, SchtasksPath, "/Query", "/TN", name, "/XML")
	if err != nil {
		return step, false
	}
	// Our own launcher is named zoomies-wsl.exe, so look for wsl.exe with it
	// taken out.
	def := strings.ReplaceAll(strings.ToLower(decodeConsole(out)), launcherName, "")
	if !strings.Contains(def, "wsl.exe") {
		return step, false
	}
	if dry {
		step.Status, step.Detail = StatusPending, "delete the old task "+name+", which opens a window; the new tasks replace it"
		return step, true
	}
	if dout, derr := sys.Run(ctx, SchtasksPath, "/Delete", "/TN", name, "/F"); derr != nil {
		step.Status, step.Detail = taskFailure(name, derr, dout)
		return step, true
	}
	step.Status, step.Detail = StatusChanged, "deleted; the new tasks replace it"
	return step, true
}
