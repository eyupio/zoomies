package installer

import (
	"context"
	"fmt"
	"io"
	"runtime"

	"github.com/eyupio/zoomies/internal/wsl"
	"github.com/eyupio/zoomies/internal/wsl/launcher"
)

// wslOffer is what an install, a join or an upgrade knows when it asks whether
// to set up WSL2 self-management.
type wslOffer struct {
	Out io.Writer
	In  io.Reader
	// Interactive is whether a person is at the terminal to answer. AssumeYes
	// is --yes, the operator's answer given in advance.
	Interactive bool
	AssumeYes   bool
	// Check changes nothing: it says what would be offered.
	Check bool
	// Binary is the zoomies binary the distro's boot command will run.
	Binary string
	// User is the account the agent runs as; empty reads it from the unit.
	User string

	// isWSL and sys stand in for the host, so a test never reads the real one.
	isWSL func() bool
	sys   wsl.System
}

// offerWSLSelfManagement says what a WSL2 distro needs to stay up and offers to
// do it. It does nothing anywhere else, and says nothing when everything is
// already in place. It is a best effort, never an error: the install or upgrade
// it follows has already succeeded, and a refusal here changes nothing about
// that. Nothing it does restarts a service or ends a process, so it is safe to
// offer while jobs are running.
func offerWSLSelfManagement(ctx context.Context, o wslOffer) {
	isWSL := o.isWSL
	if isWSL == nil {
		isWSL = runningUnderWSL
	}
	if !isWSL() {
		return
	}
	sys := o.sys
	if sys == nil {
		sys = wsl.OS{}
	}
	ui := PaletteFor(o.Out)
	opts := wsl.Options{Binary: o.Binary, User: o.User, DryRun: true, Launcher: launcher.Exe(runtime.GOARCH)}
	plan := wsl.Ensure(ctx, sys, opts)
	pending := plan.Pending()
	if len(plan.Steps) > 0 && plan.Steps[0].Status == wsl.StatusSkipped {
		return // the kernel says WSL but the process does not see WSL2: leave it be
	}
	if len(pending) == 0 {
		for _, w := range plan.Warnings() {
			ui.Warn(o.Out, "WSL2: %s: %s", w.Name, w.Detail)
		}
		if len(plan.Warnings()) == 0 {
			ui.Hint(o.Out, "WSL2: this distro already starts at boot and stays up")
		}
		return
	}

	fmt.Fprintln(o.Out)
	ui.Rule(o.Out, "WSL2")
	fmt.Fprintln(o.Out, "This is a WSL2 distro. WSL stops it when the last Windows session closes, which")
	fmt.Fprintln(o.Out, "stops the runners on it, and nothing starts it again after a reboot. Zoomies can:")
	for _, s := range pending {
		fmt.Fprintf(o.Out, "  - %s\n", s.Detail)
	}
	fmt.Fprintln(o.Out, "Nothing is restarted, so a job that is running is not touched. No password is asked for.")

	command := "sudo zoomies agent --wsl-ensure"
	switch {
	case o.Check:
		ui.Hint(o.Out, "No changes made. An install or upgrade offers this; or run: %s", command)
		return
	case o.AssumeYes:
	case o.Interactive:
		if !askApproval(o.In, o.Out, "Set this up now? [Y/n] ") {
			ui.Hint(o.Out, "Skipped. Run %s whenever you want it.", command)
			return
		}
	default:
		ui.Hint(o.Out, "Not set up, because nobody is here to ask. Run %s, or repeat this with --yes.", command)
		return
	}

	rep := wsl.Ensure(ctx, sys, wsl.Options{Binary: o.Binary, User: o.User, Launcher: launcher.Exe(runtime.GOARCH)})
	for _, s := range rep.Steps {
		switch s.Status {
		case wsl.StatusChanged:
			ui.Done(o.Out, "%s: %s", s.Name, s.Detail)
		case wsl.StatusWarn:
			ui.Warn(o.Out, "%s: %s", s.Name, s.Detail)
		}
	}
	if rep.Changed() {
		ui.Hint(o.Out, "A change to /etc/wsl.conf applies the next time the distro starts; nothing here restarts it.")
	}
}
