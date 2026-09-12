package installer

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"
)

// requirePOSIX skips a test whose subject is a POSIX fact -- a path that
// starts with a slash, a file mode, a unit file -- on Windows, where the
// installer's job is done by the service manager in service_scm.go and its
// own tests.
func requirePOSIX(t *testing.T) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("this test is about a POSIX fact (rooted paths, modes or unit files) with no Windows counterpart")
	}
}

func controllerSpec() ServiceSpec {
	return ServiceSpec{
		Unit:       UnitController,
		ExecPath:   "/usr/local/bin/zoomies",
		ConfigFile: "/etc/zoomies/zoomies.yaml",
		User:       "zoomies",
		Group:      "zoomies",
		StateDir:   "/var/lib/zoomies",
		ConfigDir:  "/etc/zoomies",
		Bind:       "127.0.0.1:8080",
	}
}

func TestRenderSystemdUnit(t *testing.T) {
	out, err := RenderSystemdUnit(controllerSpec())
	if err != nil {
		t.Fatalf("RenderSystemdUnit: %v", err)
	}
	for _, want := range []string{
		"Description=Zoomies GitHub Actions runner fleet controller",
		"User=zoomies",
		"Group=zoomies",
		"ExecStart=/usr/local/bin/zoomies controller --config /etc/zoomies/zoomies.yaml",
		"WorkingDirectory=/var/lib/zoomies",
		"ReadWritePaths=/var/lib/zoomies",
		"StateDirectory=zoomies",
		"ConfigurationDirectory=zoomies",
		"TimeoutStopSec=60s",
		"NoNewPrivileges=yes",
		"ProtectSystem=strict",
		"WantedBy=multi-user.target",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("unit is missing %q\n%s", want, out)
		}
	}
	if strings.Contains(out, "{{") {
		t.Errorf("unrendered template directive left in the unit:\n%s", out)
	}
	if strings.Contains(out, "SupplementaryGroups") {
		t.Error("no group was asked for, so the unit must not join one")
	}
	// A loopback listener on 8080 needs no capability at all.
	if !strings.Contains(out, "CapabilityBoundingSet=\n") {
		t.Errorf("expected an empty CapabilityBoundingSet:\n%s", out)
	}
	if strings.Contains(out, "CAP_NET_BIND_SERVICE") {
		t.Error("port 8080 does not need CAP_NET_BIND_SERVICE")
	}
}

func TestRenderSystemdUnitPrivilegedPort(t *testing.T) {
	spec := controllerSpec()
	spec.Bind = "0.0.0.0:443"
	out, err := RenderSystemdUnit(spec)
	if err != nil {
		t.Fatalf("RenderSystemdUnit: %v", err)
	}
	if !strings.Contains(out, "AmbientCapabilities=CAP_NET_BIND_SERVICE") {
		t.Errorf("a listener on 443 needs CAP_NET_BIND_SERVICE:\n%s", out)
	}
}

func TestRenderSystemdUnitNonStandardDirectories(t *testing.T) {
	spec := controllerSpec()
	spec.StateDir = "/srv/zoomies/state"
	spec.ConfigDir = "/srv/zoomies/etc"
	spec.ConfigFile = "/srv/zoomies/etc/zoomies.yaml"
	out, err := RenderSystemdUnit(spec)
	if err != nil {
		t.Fatalf("RenderSystemdUnit: %v", err)
	}
	// StateDirectory= would create /var/lib/zoomies, which is not where this
	// install lives; ReadWritePaths has to do the work instead.
	if strings.Contains(out, "StateDirectory=") {
		t.Errorf("StateDirectory must be left out for a custom path:\n%s", out)
	}
	if !strings.Contains(out, "ReadWritePaths=/srv/zoomies/state") {
		t.Errorf("ReadWritePaths must name the real state directory:\n%s", out)
	}
	if !strings.Contains(out, "WorkingDirectory=/srv/zoomies/state") {
		t.Errorf("WorkingDirectory must name the real state directory:\n%s", out)
	}
}

func TestRenderSystemdUnitDockerGroup(t *testing.T) {
	spec := controllerSpec()
	spec.SupplementaryGroups = []string{"docker"}
	spec.RuntimeName = "docker"
	spec.WantsDocker = true
	out, err := RenderSystemdUnit(spec)
	if err != nil {
		t.Fatalf("RenderSystemdUnit: %v", err)
	}
	if !strings.Contains(out, "SupplementaryGroups=docker") {
		t.Errorf("the root socket needs the group:\n%s", out)
	}
	if !strings.Contains(out, "Wants=docker.service") {
		t.Errorf("a Docker-backed agent must start after the daemon:\n%s", out)
	}
}

func TestRenderAgentUnit(t *testing.T) {
	spec := controllerSpec()
	spec.Unit = UnitAgent
	spec.Command = ""
	spec.Description = ""
	spec.StopTimeout = 0

	out, err := RenderSystemdUnit(spec)
	if err != nil {
		t.Fatalf("RenderSystemdUnit: %v", err)
	}
	if !strings.Contains(out, "ExecStart=/usr/local/bin/zoomies agent --config /etc/zoomies/zoomies.yaml") {
		t.Errorf("the agent unit must run the agent:\n%s", out)
	}
	if !strings.Contains(out, "Restart=always") {
		t.Errorf("an agent restarts always:\n%s", out)
	}
	// A graceful agent stop waits for in-flight jobs, which is minutes.
	if !strings.Contains(out, "TimeoutStopSec=600s") {
		t.Errorf("the agent's stop timeout must be generous:\n%s", out)
	}
}

func TestRenderSystemdUnitRequiresPaths(t *testing.T) {
	for name, mutate := range map[string]func(*ServiceSpec){
		"no binary": func(s *ServiceSpec) { s.ExecPath = "" },
		"no config": func(s *ServiceSpec) { s.ConfigFile = "" },
		"no user":   func(s *ServiceSpec) { s.User = "" },
		"no state":  func(s *ServiceSpec) { s.StateDir = "" },
	} {
		spec := controllerSpec()
		mutate(&spec)
		if _, err := RenderSystemdUnit(spec); err == nil {
			t.Errorf("%s: want an error rather than a unit that cannot start", name)
		}
	}
}

func TestRenderLaunchdPlist(t *testing.T) {
	spec := controllerSpec()
	spec.User = "ada"
	spec.Group = "staff"
	spec.StateDir = "/Users/ada/Library/Application Support/zoomies"
	spec.ConfigDir = spec.StateDir
	spec.ConfigFile = spec.StateDir + "/zoomies.yaml"

	out, err := RenderLaunchdPlist(spec)
	if err != nil {
		t.Fatalf("RenderLaunchdPlist: %v", err)
	}
	for _, want := range []string{
		"<string>sh.zoomies.controller</string>",
		"<string>/usr/local/bin/zoomies</string>",
		"<string>controller</string>",
		"<string>--config</string>",
		"<key>RunAtLoad</key>",
		"zoomies.log",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("plist is missing %q\n%s", want, out)
		}
	}
	if strings.Contains(out, "{{") {
		t.Errorf("unrendered template directive left in the plist:\n%s", out)
	}
}

func TestRenderLaunchdPlistAgentLabel(t *testing.T) {
	spec := controllerSpec()
	spec.Unit = UnitAgent
	spec.Command = ""
	out, err := RenderLaunchdPlist(spec)
	if err != nil {
		t.Fatalf("RenderLaunchdPlist: %v", err)
	}
	if !strings.Contains(out, "sh.zoomies.agent") {
		t.Errorf("the agent job needs its own label:\n%s", out)
	}
}

func TestServiceSpecDefaults(t *testing.T) {
	s := ServiceSpec{ExecPath: "/x", ConfigFile: "/y", User: "u", StateDir: "/s"}
	if err := s.defaults(); err != nil {
		t.Fatalf("defaults: %v", err)
	}
	if s.Unit != UnitController || s.Command != "controller" {
		t.Fatalf("wrong defaults: %+v", s)
	}
	if s.Group != "u" {
		t.Fatalf("group should default to the user, got %q", s.Group)
	}
	if s.StopTimeout != 60*time.Second {
		t.Fatalf("controller stop timeout = %s", s.StopTimeout)
	}
	if !strings.HasSuffix(s.LogFile, "zoomies.log") {
		t.Fatalf("log file = %q", s.LogFile)
	}
}

func TestDetectServiceKind(t *testing.T) {
	cases := []struct {
		name string
		det  Detection
		want ServiceKind
	}{
		{"systemd wins", Detection{HasSystemd: true, HasLaunchd: true}, ServiceSystemd},
		{"the service manager on windows", Detection{HasSCM: true, Docker: RuntimeInfo{Available: true}}, ServiceWindows},
		{"launchd on macOS", Detection{HasLaunchd: true}, ServiceLaunchd},
		{"compose when docker is there", Detection{Docker: RuntimeInfo{Available: true}}, ServiceCompose},
		{"nothing at all", Detection{}, ServiceNone},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := DetectServiceKind(tc.det); got != tc.want {
				t.Fatalf("DetectServiceKind = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestNewServiceManagerRefusesWhatItCannotDrive(t *testing.T) {
	for _, kind := range []ServiceKind{ServiceCompose, ServiceNone, "sysvinit"} {
		if _, err := NewServiceManager(kind, UnitController); err == nil {
			t.Errorf("%q: want an error rather than a manager that shells out to nothing", kind)
		}
	}
}

func TestReadUnitIdentityKeepsAnUpgradeOnTheSameAccount(t *testing.T) {
	spec := controllerSpec()
	spec.User = "zoomies-svc"
	spec.Group = "zoomies-grp"
	spec.SupplementaryGroups = []string{"docker"}
	body, err := RenderSystemdUnit(spec)
	if err != nil {
		t.Fatalf("RenderSystemdUnit: %v", err)
	}
	path := writeFile(t, t.TempDir(), "zoomies.service", body)

	user, group, groups := ReadUnitIdentity(path)
	if user != "zoomies-svc" || group != "zoomies-grp" {
		t.Fatalf("ReadUnitIdentity = %q/%q, want zoomies-svc/zoomies-grp", user, group)
	}
	if len(groups) != 1 || groups[0] != "docker" {
		t.Fatalf("supplementary groups = %v", groups)
	}

	if u, g, gs := ReadUnitIdentity(filepath.Join(t.TempDir(), "absent.service")); u != "" || g != "" || gs != nil {
		t.Fatalf("a missing unit has no identity, got %q/%q/%v", u, g, gs)
	}
}

// The command line the Windows service manager runs is split again by the
// usual CommandLineToArgv rules, so a path with a space -- C:\Program Files
// is the default install location -- has to be quoted, and every path is
// quoted rather than only the ones that need it today.
func TestWindowsServiceCommandQuotesEveryPath(t *testing.T) {
	cmd, err := WindowsServiceCommand(ServiceSpec{
		Unit:       UnitAgent,
		ExecPath:   `C:\Program Files\zoomies\zoomies.exe`,
		ConfigFile: `C:\ProgramData\zoomies\zoomies.yaml`,
		StateDir:   `C:\ProgramData\zoomies`,
		User:       "LocalSystem",
	})
	if err != nil {
		t.Fatalf("WindowsServiceCommand: %v", err)
	}
	// The log file is defaulted beside the state directory with the host's
	// own separator, which is why the expectation is joined rather than
	// spelled: on Linux this test sees a slash where Windows would see a
	// backslash, and either is the right answer on its platform.
	want := `"C:\Program Files\zoomies\zoomies.exe" agent --config "C:\ProgramData\zoomies\zoomies.yaml" --log-file "` +
		filepath.Join(`C:\ProgramData\zoomies`, "zoomies-agent.log") + `"`
	if cmd != want {
		t.Fatalf("command line\n got %s\nwant %s", cmd, want)
	}
}

// sc.exe's `key= value` spelling is two arguments with the space between
// them, and an invocation that joins them is one that registers nothing and
// says so only in its exit code. The manager is watched through the fake
// runner, which is what makes the shape testable without a Windows host.
func TestWindowsServiceManagerDrivesScExe(t *testing.T) {
	var calls [][]string
	exists := false
	m := &windowsManager{unit: UnitAgent, run: func(_ context.Context, name string, args ...string) (string, error) {
		calls = append(calls, append([]string{name}, args...))
		if len(args) > 0 && args[0] == "query" {
			if !exists {
				return "", errors.New("[SC] EnumQueryServicesStatus:OpenService FAILED 1060")
			}
			return "SERVICE_NAME: zoomies-agent\n        STATE              : 4  RUNNING\n", nil
		}
		if len(args) > 0 && args[0] == "create" {
			exists = true
		}
		return "", nil
	}}
	spec := ServiceSpec{Unit: UnitAgent, ExecPath: `C:\zoomies\zoomies.exe`, ConfigFile: `C:\ProgramData\zoomies\zoomies.yaml`, StateDir: `C:\ProgramData\zoomies`, User: "LocalSystem"}
	name, err := m.Install(context.Background(), spec)
	if err != nil {
		t.Fatalf("Install: %v", err)
	}
	if name != "zoomies-agent" {
		t.Errorf("service name = %q, want zoomies-agent, the unit name", name)
	}

	var create []string
	for _, c := range calls {
		if c[0] == "sc.exe" && c[1] == "create" {
			create = c
		}
	}
	if create == nil {
		t.Fatalf("no sc.exe create among %v", calls)
	}
	for i, a := range create {
		if strings.HasSuffix(a, "=") && strings.Contains(a, " ") {
			t.Errorf("argument %q joins a key and its value; sc.exe wants them apart", a)
		}
		if a == "binPath=" && !strings.HasPrefix(create[i+1], `"C:\zoomies\zoomies.exe" agent`) {
			t.Errorf("binPath is %q, want the rendered command line", create[i+1])
		}
		if a == "start=" && create[i+1] != "auto" {
			t.Errorf("a service that does not start at boot is a host that drops out on reboot: start= %q", create[i+1])
		}
	}

	if status, _ := m.Status(context.Background()); status != "running" {
		t.Errorf("status = %q, want the STATE word from sc.exe query, lowercased", status)
	}
	if !strings.Contains(m.LogCommand(), `zoomies-agent.log`) {
		t.Errorf("the log command must point at the file the service writes: %q", m.LogCommand())
	}

	// Installing again replaces the registration rather than failing on it.
	calls = nil
	if _, err := m.Install(context.Background(), spec); err != nil {
		t.Fatalf("second Install: %v", err)
	}
	var seen []string
	for _, c := range calls {
		seen = append(seen, c[1])
	}
	if !slices.Contains(seen, "delete") || !slices.Contains(seen, "create") {
		t.Errorf("a re-install must delete the old registration and create the new one, ran %v", seen)
	}
}

// %ProgramData% is readable by every local user by default, and the agent's
// credentials live under it. On Windows the join replaces the directory's
// inherited ACL with SYSTEM and Administrators, and nowhere else does it
// touch permissions at all -- a chmod-shaped change on Linux would fight the
// service user the installer already set up.
func TestJoinRestrictsTheDirectoryToAdministratorsOnWindowsOnly(t *testing.T) {
	var calls [][]string
	run := func(_ context.Context, name string, args ...string) (string, error) {
		calls = append(calls, append([]string{name}, args...))
		return "", nil
	}
	root := t.TempDir()
	config, state := filepath.Join(root, "etc"), filepath.Join(root, "state")
	if err := prepareDirs(context.Background(), "linux", run, config, state); err != nil {
		t.Fatalf("linux: %v", err)
	}
	for _, dir := range []string{config, state} {
		if _, err := os.Stat(dir); err != nil {
			t.Errorf("%s was not created: %v", dir, err)
		}
	}
	if len(calls) != 0 {
		t.Fatalf("a Linux join must not touch permissions, ran %v", calls)
	}

	if err := prepareDirs(context.Background(), "windows", run, config, state); err != nil {
		t.Fatalf("windows: %v", err)
	}
	if len(calls) != 2 || calls[0][0] != "icacls" || calls[1][0] != "icacls" {
		t.Fatalf("expected one icacls call per directory, got %v", calls)
	}
	args := strings.Join(calls[0], " ")
	for _, want := range []string{config, "/inheritance:r", "SYSTEM:(OI)(CI)F", "Administrators:(OI)(CI)F"} {
		if !strings.Contains(args, want) {
			t.Errorf("icacls call is missing %q: %s", want, args)
		}
	}
	// The grants, not the whole line: on Windows the temporary directory
	// itself lives under C:\Users.
	for i, a := range calls[0] {
		if a == "/grant:r" && strings.HasPrefix(calls[0][i+1], "Users") {
			t.Errorf("Users must not be granted anything: %s", args)
		}
	}

	failing := func(context.Context, string, ...string) (string, error) { return "", errors.New("Access is denied.") }
	err := prepareDirs(context.Background(), "windows", failing, config)
	if err == nil {
		t.Fatal("a refused icacls must be reported: a join that silently leaves the credentials world-readable is worse than one that stops")
	}
	if !strings.Contains(err.Error(), "elevated") {
		t.Errorf("the error must say what to do about it: %v", err)
	}

	// A directory that cannot be created is reported as such, before any
	// ACL is attempted on it.
	calls = nil
	blocked := filepath.Join(root, "file")
	if err := os.WriteFile(blocked, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := prepareDirs(context.Background(), "windows", run, filepath.Join(blocked, "under")); err == nil {
		t.Fatal("creating a directory under a file must fail")
	}
	if len(calls) != 0 {
		t.Errorf("no ACL must be set on a directory that was not created, ran %v", calls)
	}
}

// ---------------------------------------------------------------------------
// The POSIX managers, driven through the fake runner
// ---------------------------------------------------------------------------
//
// systemd and launchd are the two supervisors nearly every Zoomies host
// actually uses, and until now only the Windows manager had its argv checked.
// A manager is the whole of what stands between the installer and the host's
// service supervisor: a wrong verb, or a unit named without its suffix, is an
// install that fails on a real machine and on no developer's.

// recordingRunner stands in for the supervisor a manager talks to. It records
// every argv and answers from a table, which is what lets both lifecycles be
// checked on a host that has neither.
type recordingRunner struct {
	calls  [][]string
	answer func(args []string) (string, error)
}

func (r *recordingRunner) run(_ context.Context, name string, args ...string) (string, error) {
	r.calls = append(r.calls, append([]string{name}, args...))
	if r.answer != nil {
		return r.answer(args)
	}
	return "", nil
}

// ran reports whether exactly this command line was run.
func (r *recordingRunner) ran(argv ...string) bool {
	return slices.ContainsFunc(r.calls, func(c []string) bool { return slices.Equal(c, argv) })
}

func (r *recordingRunner) lines() []string {
	out := make([]string, 0, len(r.calls))
	for _, c := range r.calls {
		out = append(out, strings.Join(c, " "))
	}
	return out
}

// Every systemd verb names the unit as `zoomies.service`, and each lifecycle
// step runs the one command it claims to.
func TestSystemdManagerDrivesSystemctl(t *testing.T) {
	r := &recordingRunner{answer: func(args []string) (string, error) {
		if len(args) > 0 && args[0] == "list-unit-files" {
			return "zoomies.service enabled enabled", nil
		}
		return "", nil
	}}
	m := &systemdManager{unit: UnitController, run: r.run}

	if m.Kind() != ServiceSystemd {
		t.Errorf("Kind() = %q, want %q", m.Kind(), ServiceSystemd)
	}
	if m.unitName() != "zoomies.service" {
		t.Fatalf("unitName() = %q, want zoomies.service", m.unitName())
	}

	for _, step := range []struct {
		name string
		call func() error
		want []string
	}{
		{"Enable", func() error { return m.Enable(context.Background()) }, []string{"systemctl", "enable", "zoomies.service"}},
		{"Start", func() error { return m.Start(context.Background()) }, []string{"systemctl", "start", "zoomies.service"}},
		{"Stop", func() error { return m.Stop(context.Background()) }, []string{"systemctl", "stop", "zoomies.service"}},
		{"Disable", func() error { return m.Disable(context.Background()) }, []string{"systemctl", "disable", "zoomies.service"}},
	} {
		r.calls = nil
		if err := step.call(); err != nil {
			t.Fatalf("%s: %v", step.name, err)
		}
		if !r.ran(step.want...) {
			t.Errorf("%s ran %v, want %q among them", step.name, r.lines(), strings.Join(step.want, " "))
		}
	}

	// Printed for the operator to paste, so it is part of the contract.
	if got, want := m.LogCommand(), "journalctl -u zoomies.service -f"; got != want {
		t.Errorf("LogCommand() = %q, want %q", got, want)
	}
}

// Uninstall has to be safe to run twice, so stopping a unit systemd has never
// heard of is silence -- and it does not ask systemctl to stop something it
// would only complain about.
func TestSystemdStopAndDisableSayNothingAboutAUnitThatIsNotLoaded(t *testing.T) {
	r := &recordingRunner{answer: func(args []string) (string, error) {
		if len(args) > 0 && args[0] == "list-unit-files" {
			// What systemctl prints when it matches nothing.
			return "0 unit files listed.", nil
		}
		return "", nil
	}}
	m := &systemdManager{unit: UnitController, run: r.run}

	if err := m.Stop(context.Background()); err != nil {
		t.Errorf("Stop on an absent unit: %v, want silence", err)
	}
	if err := m.Disable(context.Background()); err != nil {
		t.Errorf("Disable on an absent unit: %v, want silence", err)
	}
	for _, c := range r.calls {
		if len(c) > 1 && (c[1] == "stop" || c[1] == "disable") {
			t.Errorf("ran %q against a unit systemd does not have", strings.Join(c, " "))
		}
	}
}

// `systemctl is-active` exits non-zero for everything except "active", so a
// manager that trusted the exit code would report a stopped service as an
// error with no word in it. The word is the part worth showing.
func TestSystemdStatusPrefersTheWordSystemctlPrints(t *testing.T) {
	m := &systemdManager{unit: UnitController, run: (&recordingRunner{answer: func(args []string) (string, error) {
		if len(args) > 0 && args[0] == "is-active" {
			return "failed", errors.New("exit status 3")
		}
		return "", nil
	}}).run}

	status, err := m.Status(context.Background())
	if err != nil {
		t.Fatalf("Status: %v, want the word and no error", err)
	}
	if status != "failed" {
		t.Errorf("Status = %q, want %q", status, "failed")
	}

	// A failure with nothing to show is still a failure: there is no word to
	// prefer, so the error is the answer.
	silent := &systemdManager{unit: UnitController, run: (&recordingRunner{answer: func([]string) (string, error) {
		return "", errors.New("systemctl is not on PATH")
	}}).run}
	if _, err := silent.Status(context.Background()); err == nil {
		t.Error("Status with no output and a failure should return the error")
	}
}

// Remove takes the unit file and the failed state with it. Leaving the latter
// behind is how a reinstall of a service that once crashed starts out in a
// state systemd refuses to start.
func TestSystemdRemoveTakesTheUnitFileAndTheFailedState(t *testing.T) {
	requirePOSIX(t)
	r := &recordingRunner{}
	path := filepath.Join(t.TempDir(), "zoomies.service")
	if err := os.WriteFile(path, []byte("[Unit]\n"), 0o644); err != nil {
		t.Fatalf("seeding the unit file: %v", err)
	}
	m := &systemdManager{unit: UnitController, run: r.run, path: path}

	if err := m.Remove(context.Background()); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("the unit file is still there: %v", err)
	}
	if !r.ran("systemctl", "reset-failed", "zoomies.service") {
		t.Errorf("ran %v, want a reset-failed among them", r.lines())
	}
	// An interrupted uninstall is re-run, so the second time is not an error.
	if err := m.Remove(context.Background()); err != nil {
		t.Errorf("second Remove: %v, want silence", err)
	}
}

// A launchd job lives in a domain, and which one depends on who is running:
// the system domain for root, the caller's GUI session otherwise. A job
// bootstrapped into the wrong domain never runs and says so nowhere.
func TestLaunchdManagerTargetsTheDomainItsJobLivesIn(t *testing.T) {
	root := &launchdManager{unit: UnitController, root: true}
	if got := root.domain(); got != "system" {
		t.Errorf("root domain = %q, want system", got)
	}
	user := &launchdManager{unit: UnitController}
	if want := "gui/" + strconv.Itoa(os.Getuid()); user.domain() != want {
		t.Errorf("user domain = %q, want %q", user.domain(), want)
	}
	// The label the manager targets has to be the one the plist declares, or
	// every launchctl call addresses a job that does not exist.
	if got, want := (&launchdManager{unit: UnitAgent}).label(), (ServiceSpec{Unit: UnitAgent}).Label(); got != want {
		t.Errorf("agent label = %q, but the plist declares %q", got, want)
	}
	if got, want := root.label(), (ServiceSpec{Unit: UnitController}).Label(); got != want {
		t.Errorf("controller label = %q, but the plist declares %q", got, want)
	}
}

// Start bootstraps the plist into the domain and then kickstarts the label.
// Bootstrap alone loads a job whose RunAtLoad moment has already passed, which
// on a reinstall is a service that is loaded and not running.
func TestLaunchdStartBootstrapsThenKickstarts(t *testing.T) {
	r := &recordingRunner{}
	plist := "/Library/LaunchDaemons/sh.zoomies.controller.plist"
	m := &launchdManager{unit: UnitController, root: true, run: r.run, path: plist}

	if err := m.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if !r.ran("launchctl", "bootstrap", "system", plist) {
		t.Errorf("ran %v, want the plist bootstrapped into the system domain", r.lines())
	}
	if !r.ran("launchctl", "kickstart", "system/sh.zoomies.controller") {
		t.Errorf("ran %v, want the job kickstarted", r.lines())
	}
}

// Install boots the old job out before the new plist lands. A job already
// loaded from a previous install keeps running the command the old plist
// named, so an upgrade that skipped this would leave the previous binary
// serving until the machine rebooted.
func TestLaunchdInstallBootsOutTheOldJobAndWritesThePlist(t *testing.T) {
	requirePOSIX(t)
	home := t.TempDir()
	t.Setenv("HOME", home)
	r := &recordingRunner{}
	m := &launchdManager{unit: UnitController, run: r.run}

	path, err := m.Install(context.Background(), controllerSpec())
	if err != nil {
		t.Fatalf("Install: %v", err)
	}
	if want := filepath.Join(home, "Library", "LaunchAgents", "sh.zoomies.controller.plist"); path != want {
		t.Fatalf("plist written to %q, want %q", path, want)
	}
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading the plist back: %v", err)
	}
	if !strings.Contains(string(body), "sh.zoomies.controller") {
		t.Errorf("the plist does not declare the label the manager targets:\n%s", body)
	}
	if !r.ran("launchctl", "bootout", m.domain()+"/sh.zoomies.controller") {
		t.Errorf("ran %v, want the old job booted out before the write", r.lines())
	}
}

// Status answers "not loaded" rather than failing for a job launchd has never
// heard of, because that is the ordinary state before the first start and the
// health step prints it either way.
func TestLaunchdStatusReadsTheStateLineOrSaysNotLoaded(t *testing.T) {
	missing := &launchdManager{unit: UnitController, root: true, run: (&recordingRunner{answer: func([]string) (string, error) {
		return "", errors.New("Could not find service in domain")
	}}).run}
	if got, err := missing.Status(context.Background()); err != nil || got != "not loaded" {
		t.Errorf(`Status = %q, %v; want "not loaded" and no error`, got, err)
	}

	running := &launchdManager{unit: UnitController, root: true, run: (&recordingRunner{answer: func([]string) (string, error) {
		return "sh.zoomies.controller = {\n\tstate = running\n\tpid = 4242\n}", nil
	}}).run}
	if got, _ := running.Status(context.Background()); got != "running" {
		t.Errorf("Status = %q, want the state launchctl printed", got)
	}
}

// macOS has no journal, so the logs are a file the plist points at -- and a
// manager built without one (a status check on an existing install never calls
// Install) still has to name the file the installer would have used.
func TestLaunchdLogsTailTheFileThePlistNames(t *testing.T) {
	requirePOSIX(t)
	path := filepath.Join(t.TempDir(), "zoomies.log")
	var b strings.Builder
	for i := 1; i <= 10; i++ {
		fmt.Fprintf(&b, "line %d\n", i)
	}
	if err := os.WriteFile(path, []byte(b.String()), 0o644); err != nil {
		t.Fatalf("seeding the log: %v", err)
	}

	m := &launchdManager{unit: UnitController, log: path}
	out, err := m.Logs(context.Background(), 3)
	if err != nil {
		t.Fatalf("Logs: %v", err)
	}
	if want := "line 8\nline 9\nline 10"; out != want {
		t.Errorf("Logs(3) = %q, want the last three lines (%q)", out, want)
	}
	if got, want := m.LogCommand(), "tail -f "+path; got != want {
		t.Errorf("LogCommand() = %q, want %q", got, want)
	}

	bare := &launchdManager{unit: UnitAgent}
	if want := "tail -f " + filepath.Join("/var/log", "zoomies-agent.log"); bare.LogCommand() != want {
		t.Errorf("LogCommand() with no plist in hand = %q, want %q", bare.LogCommand(), want)
	}
}

// Every sc.exe verb the lifecycle uses, and the two refusals that have to read
// as success: uninstall is re-run, and stopping something already stopped is
// what Windows calls error 1062.
func TestWindowsServiceManagerLifecycleIsSafeToRepeat(t *testing.T) {
	registered := true
	stopFails := false
	r := &recordingRunner{}
	r.answer = func(args []string) (string, error) {
		switch {
		case len(args) > 0 && args[0] == "query":
			if !registered {
				return "", errors.New("[SC] EnumQueryServicesStatus:OpenService FAILED 1060")
			}
			return "SERVICE_NAME: zoomies-agent\n        STATE              : 1  STOPPED\n", nil
		case len(args) > 0 && args[0] == "stop" && stopFails:
			return "[SC] ControlService FAILED 1062:\n\nThe service has not been started.", errors.New("exit status 1")
		}
		return "", nil
	}
	m := &windowsManager{unit: UnitAgent, run: r.run}

	if err := m.Enable(context.Background()); err != nil {
		t.Fatalf("Enable: %v", err)
	}
	if !r.ran("sc.exe", "config", "zoomies-agent", "start=", "auto") {
		t.Errorf("ran %v, want the service configured to start at boot", r.lines())
	}

	r.calls = nil
	if err := m.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if !r.ran("sc.exe", "start", "zoomies-agent") {
		t.Errorf("ran %v, want a start", r.lines())
	}

	// A service that is registered but not running: sc.exe refuses the stop
	// with 1062, and that is the state the caller asked for.
	stopFails = true
	if err := m.Stop(context.Background()); err != nil {
		t.Errorf("Stop on an already-stopped service: %v, want silence", err)
	}
	stopFails = false

	if got, _ := m.Status(context.Background()); got != "stopped" {
		t.Errorf("Status = %q, want the STATE word lowercased", got)
	}

	// Nothing registered: every removal step is a no-op rather than an error,
	// so an interrupted uninstall can simply be run again.
	registered = false
	r.calls = nil
	for _, step := range []struct {
		name string
		call func() error
	}{
		{"Stop", func() error { return m.Stop(context.Background()) }},
		{"Disable", func() error { return m.Disable(context.Background()) }},
		{"Remove", func() error { return m.Remove(context.Background()) }},
	} {
		if err := step.call(); err != nil {
			t.Errorf("%s with nothing registered: %v, want silence", step.name, err)
		}
	}
	for _, c := range r.calls {
		if len(c) > 1 && (c[1] == "stop" || c[1] == "delete" || c[1] == "config") {
			t.Errorf("ran %q against a service sc.exe does not have", strings.Join(c, " "))
		}
	}
	if got, _ := m.Status(context.Background()); got != "not installed" {
		t.Errorf("Status with nothing registered = %q, want \"not installed\"", got)
	}
}

// A service installed without a log file cannot be tailed, so both the logs
// and the command that would show them point at the one place that still knows
// what the service was told to do.
func TestWindowsServiceLogsPointSomewhereWhenThereIsNoFile(t *testing.T) {
	m := &windowsManager{unit: UnitAgent, run: (&recordingRunner{}).run}

	if _, err := m.Logs(context.Background(), 10); err == nil {
		t.Error("Logs with no log file should say so rather than return nothing")
	}
	if got, want := m.LogCommand(), "sc.exe qc zoomies-agent"; got != want {
		t.Errorf("LogCommand() = %q, want %q", got, want)
	}
}
