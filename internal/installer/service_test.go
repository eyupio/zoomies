package installer

import (
	"context"
	"errors"
	"path/filepath"
	"runtime"
	"slices"
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
	if err := restrictDir(context.Background(), "linux", `/var/lib/zoomies`, run); err != nil {
		t.Fatalf("linux: %v", err)
	}
	if len(calls) != 0 {
		t.Fatalf("a Linux join must not touch permissions, ran %v", calls)
	}

	if err := restrictDir(context.Background(), "windows", `C:\ProgramData\zoomies`, run); err != nil {
		t.Fatalf("windows: %v", err)
	}
	if len(calls) != 1 || calls[0][0] != "icacls" {
		t.Fatalf("expected one icacls call, got %v", calls)
	}
	args := strings.Join(calls[0], " ")
	for _, want := range []string{`C:\ProgramData\zoomies`, "/inheritance:r", "SYSTEM:(OI)(CI)F", "Administrators:(OI)(CI)F"} {
		if !strings.Contains(args, want) {
			t.Errorf("icacls call is missing %q: %s", want, args)
		}
	}
	if strings.Contains(args, "Users") {
		t.Errorf("Users must not be granted anything: %s", args)
	}

	failing := func(context.Context, string, ...string) (string, error) { return "", errors.New("Access is denied.") }
	if err := restrictDir(context.Background(), "windows", `C:\ProgramData\zoomies`, failing); err == nil {
		t.Fatal("a refused icacls must be reported: a join that silently leaves the credentials world-readable is worse than one that stops")
	}
}
