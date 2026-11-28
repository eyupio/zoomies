package wsl

import (
	"bytes"
	"context"
	"errors"
	"io/fs"
	"path"
	"strings"
)

// Status is how one step of Ensure ended.
type Status string

const (
	StatusOK      Status = "ok"      // already as wanted; nothing was changed
	StatusChanged Status = "changed" // written or created
	StatusSkipped Status = "skipped" // not applicable here, and why
	StatusWarn    Status = "warning" // could not be done; what to do about it
	StatusPending Status = "pending" // DryRun only: would be written or created
)

// Step is one thing Ensure checked.
type Step struct {
	Name   string
	Status Status
	Detail string
}

// Report is what one Ensure found and did.
type Report struct {
	Distro string
	Steps  []Step
}

// Warnings returns the steps that could not be completed.
func (r Report) Warnings() []Step {
	var w []Step
	for _, s := range r.Steps {
		if s.Status == StatusWarn {
			w = append(w, s)
		}
	}
	return w
}

// Pending returns the steps a DryRun found would change something.
func (r Report) Pending() []Step {
	var p []Step
	for _, s := range r.Steps {
		if s.Status == StatusPending {
			p = append(p, s)
		}
	}
	return p
}

// Changed reports whether any step wrote something.
func (r Report) Changed() bool {
	for _, s := range r.Steps {
		if s.Status == StatusChanged {
			return true
		}
	}
	return false
}

// Options names what Ensure sets up for.
type Options struct {
	// Binary is the zoomies binary the boot command runs.
	Binary string
	// User is the account the agent service runs as, for linger. Empty means
	// it is read from the installed unit.
	User string
	// ConfPath overrides /etc/wsl.conf, for tests.
	ConfPath string
	// DryRun reports what would change as StatusPending and writes nothing,
	// creates no task and enables no linger. It only reads, which is what lets
	// an installer show the operator the list before asking.
	DryRun bool
	// Launcher is the Windows program the scheduled tasks run (the embedded
	// cmd/zoomies-wsl build for this architecture). Nil means this build has
	// none, and the tasks are left as they are.
	Launcher []byte
}

// Ensure brings the distro's self-management into the wanted state, and does
// nothing when it is already there, so it is safe to run from a scheduled task
// every few minutes. A step that cannot be done is a warning in the report and
// never an error: a task that fails on every run teaches nobody anything, and
// the other steps are still worth taking.
//
// It restarts nothing and ends no process, so it cannot interrupt a running
// job. A changed wsl.conf takes effect the next time the distro starts.
func Ensure(ctx context.Context, sys System, opts Options) Report {
	rep := Report{}
	add := func(s Step) { rep.Steps = append(rep.Steps, s) }
	if !Detect(sys) {
		add(Step{Name: "wsl2", Status: StatusSkipped, Detail: "this is not a WSL2 distro, so there is nothing to do"})
		return rep
	}
	rep.Distro = Distro(ctx, sys)
	add(Step{Name: "wsl2", Status: StatusOK, Detail: "WSL2 detected, distro " + orUnknown(rep.Distro)})
	add(ensureBootCommand(sys, opts))
	add(ensureLinger(ctx, sys, opts))
	for _, s := range ensureTasks(ctx, sys, rep.Distro, opts) {
		add(s)
	}
	return rep
}

func orUnknown(s string) string {
	if s == "" {
		return "(unknown)"
	}
	return s
}

func ensureBootCommand(sys System, opts Options) Step {
	step := Step{Name: "wsl.conf boot.command"}
	path := opts.ConfPath
	if path == "" {
		path = ConfPath
	}
	if opts.Binary == "" {
		step.Status, step.Detail = StatusSkipped, "the zoomies binary's path is unknown"
		return step
	}
	data, err := sys.ReadFile(path)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		step.Status, step.Detail = StatusWarn, "reading "+path+": "+err.Error()
		return step
	}
	want, changed := WithBootCommand(string(data), BootCommand(opts.Binary))
	if !changed {
		step.Status, step.Detail = StatusOK, "already in "+path
		return step
	}
	if opts.DryRun {
		step.Status, step.Detail = StatusPending, "set boot.command in "+path+" so the distro starts the agent's checks at boot"
		return step
	}
	if sys.Geteuid() != 0 {
		step.Status, step.Detail = StatusWarn, "writing "+path+" needs root; run `sudo zoomies agent --wsl-ensure` once"
		return step
	}
	if err == nil && !sys.Exists(path+".zoomies-bak") {
		if werr := sys.WriteFile(path+".zoomies-bak", data, 0o644); werr != nil {
			step.Status, step.Detail = StatusWarn, "keeping a copy of "+path+": "+werr.Error()
			return step
		}
	}
	if werr := sys.WriteFile(path, []byte(want), 0o644); werr != nil {
		step.Status, step.Detail = StatusWarn, "writing "+path+": "+werr.Error()
		return step
	}
	step.Status, step.Detail = StatusChanged, "set in "+path+"; it runs the next time the distro starts"
	if !HasSystemd(want) {
		step.Detail += " (systemd is not on in it, so the agent is not a service here; see docs/hosts-and-pools.md)"
	}
	return step
}

// agentUser is who linger is enabled for.
func agentUser(sys System, opts Options) string {
	if opts.User != "" {
		return opts.User
	}
	if b, err := sys.ReadFile(unitFile); err == nil {
		for _, l := range strings.Split(string(b), "\n") {
			if k, v, ok := strings.Cut(strings.TrimSpace(l), "="); ok && k == "User" {
				return strings.TrimSpace(v)
			}
		}
	}
	return ""
}

func ensureLinger(ctx context.Context, sys System, opts Options) Step {
	step := Step{Name: "systemd linger"}
	user := agentUser(sys, opts)
	if user == "" {
		step.Status, step.Detail = StatusSkipped, "the agent's user is unknown (no installed zoomies-agent unit)"
		return step
	}
	if sys.Exists(path.Join(lingerDir, user)) {
		step.Status, step.Detail = StatusOK, "enabled for "+user
		return step
	}
	if opts.DryRun {
		step.Status, step.Detail = StatusPending, "enable systemd linger for "+user+" so its services outlive login sessions"
		return step
	}
	if _, err := sys.Run(ctx, "loginctl", "enable-linger", user); err != nil {
		step.Status, step.Detail = StatusWarn, "loginctl enable-linger "+user+" failed: "+err.Error()
		return step
	}
	step.Status, step.Detail = StatusChanged, "enabled for "+user
	return step
}

func ensureTasks(ctx context.Context, sys System, distro string, opts Options) []Step {
	dry := opts.DryRun
	skip := func(why string) []Step {
		return []Step{{Name: "windows scheduled tasks", Status: StatusSkipped, Detail: why}}
	}
	if distro == "" {
		return skip("the distro's name could not be found, so no task can start it")
	}
	if strings.ContainsAny(distro, "\"\r\n") {
		return skip("the distro's name has a character a task cannot carry")
	}
	if !sys.Exists(SchtasksPath) {
		return skip("Windows interop is off here (" + SchtasksPath + " is missing); enable [interop] in /etc/wsl.conf")
	}
	if len(opts.Launcher) == 0 {
		return []Step{{Name: "windows scheduled tasks", Status: StatusWarn,
			Detail: "this zoomies build has no Windows launcher, so the tasks were left as they are (an old one may open a terminal window); use a release build or run `make build`"}}
	}
	res, err := writeLauncher(ctx, sys, opts.Launcher, dry)
	if err != nil {
		return []Step{{Name: "windows launcher", Status: StatusWarn, Detail: err.Error()}}
	}
	steps := []Step{{Name: "windows launcher", Status: StatusOK, Detail: res.windowsPath}}
	switch {
	case res.pending:
		steps[0].Status, steps[0].Detail = StatusPending, "write the launcher "+res.windowsPath
	case res.changed:
		steps[0].Status = StatusChanged
	}
	// What the old arrangement left is only taken away once both new tasks are
	// in place. A task that could not be replaced (made elevated, and this run
	// is not) still runs the old script, and deleting that, or the older tasks
	// that keep the distro up, would leave nothing that works.
	replaced := true
	for _, spec := range taskSpecs() {
		st := ensureTask(ctx, sys, spec, res.windowsPath, distro, dry)
		if st.Status == StatusWarn {
			replaced = false
		}
		steps = append(steps, st)
	}
	if !replaced {
		return append(steps, Step{Name: "old tasks and script", Status: StatusSkipped,
			Detail: "kept, because a new task could not be created; they are removed once both are in place"})
	}
	for _, name := range legacyTasks {
		if s, found := removeLegacyTask(ctx, sys, name, dry); found {
			steps = append(steps, s)
		}
	}
	if old := path.Join(res.dir, oldScriptName); sys.Exists(old) && !dry {
		if err := sys.Remove(old); err == nil {
			steps = append(steps, Step{Name: "old start script", Status: StatusChanged, Detail: "removed " + oldScriptName + ", which the launcher replaces"})
		}
	}
	return steps
}

type launcherResult struct {
	windowsPath string
	dir         string // the Linux path of its folder
	changed     bool
	pending     bool
}

// writeLauncher puts the launcher under %LOCALAPPDATA%, which needs no
// elevation, writing it only when its content differs. A copy a running task
// has locked cannot be replaced; that is a warning, and the next run tries
// again.
func writeLauncher(ctx context.Context, sys System, exe []byte, dry bool) (launcherResult, error) {
	var res launcherResult
	out, err := sys.Run(ctx, CmdExePath, "/c", "echo", "%LOCALAPPDATA%")
	if err != nil {
		return res, errors.New("asking Windows for %LOCALAPPDATA%: " + err.Error())
	}
	local := strings.TrimSpace(decodeConsole(out))
	if local == "" || strings.Contains(local, "%") {
		return res, errors.New("the Windows side did not say where %LOCALAPPDATA% is")
	}
	unixOut, err := sys.Run(ctx, "wslpath", "-u", local)
	if err != nil {
		return res, errors.New("translating " + local + " to a Linux path: " + err.Error())
	}
	// A Linux path, whatever the host the tests run on: slashes only.
	dir := path.Join(strings.TrimSpace(string(unixOut)), "Zoomies")
	file := path.Join(dir, launcherName)
	res.dir = dir
	res.windowsPath = local + `\Zoomies\` + launcherName
	if cur, err := sys.ReadFile(file); err == nil && bytes.Equal(cur, exe) {
		return res, nil
	}
	if dry {
		res.pending = true
		return res, nil
	}
	if err := sys.MkdirAll(dir, 0o755); err != nil {
		return res, errors.New("creating " + dir + ": " + err.Error())
	}
	if err := sys.WriteFile(file, exe, 0o755); err != nil {
		return res, errors.New("writing " + file + " (a task that is running it may hold it; this is retried on the next run): " + err.Error())
	}
	res.changed = true
	return res, nil
}
