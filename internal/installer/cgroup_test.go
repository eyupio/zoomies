package installer

import (
	"bytes"
	"context"
	"os"
	"strings"
	"testing"

	"github.com/eyupio/zoomies/internal/store"
)

// A pool's CPU quota or memory limit is refused by exactly this daemon, so
// the uid it runs the socket under is what a fix has to target -- and it has
// to survive the usual shapes a rootless endpoint takes.
func TestRootlessUIDReadsTheUserRunDirectory(t *testing.T) {
	cases := []struct {
		endpoint string
		want     int
		ok       bool
	}{
		{"unix:///run/user/1000/podman/podman.sock", 1000, true},
		{"unix:///run/user/0/docker.sock", 0, true},
		{"unix:///var/run/docker.sock", 0, false},
		{"tcp://127.0.0.1:2375", 0, false},
		{"", 0, false},
	}
	for _, c := range cases {
		got, ok := rootlessUID(c.endpoint)
		if ok != c.ok || (ok && got != c.want) {
			t.Errorf("rootlessUID(%q) = (%d, %v), want (%d, %v)", c.endpoint, got, ok, c.want, c.ok)
		}
	}
}

// fullSupport and noSupport are the two ends of store.LimitSupport that mean
// "nothing to fix here": either the daemon already applies every limit, or it
// has never been asked and Known is false.
var (
	fullSupport = store.LimitSupport{Known: true, CPU: true, Memory: true, Pids: true}
	noSupport   = store.LimitSupport{}
)

func cgroupTestUI() (*ui, *bytes.Buffer) {
	var b bytes.Buffer
	return newUI(&b), &b
}

func rootlessSystemdDetection(root bool) Detection {
	return Detection{OS: "linux", HasSystemd: true, Root: root}
}

// Nothing here is worth writing a drop-in for: the process backend has no
// daemon, a root daemon's limits are a kernel fact rather than a delegation
// one, and a daemon that already enforces every limit -- or has never said
// whether it can -- needs setup to do nothing at all.
func TestEnsureCgroupDelegationSkipsWhenThereIsNothingToFix(t *testing.T) {
	cases := []struct {
		name     string
		kind     store.BackendKind
		rootless bool
		limits   store.LimitSupport
		det      Detection
	}{
		{"process backend", store.BackendProcess, true, store.LimitSupport{Known: true}, rootlessSystemdDetection(true)},
		{"root daemon", store.BackendPodman, false, store.LimitSupport{Known: true}, rootlessSystemdDetection(true)},
		{"limits not yet known", store.BackendPodman, true, noSupport, rootlessSystemdDetection(true)},
		{"limits already fully enforced", store.BackendPodman, true, fullSupport, rootlessSystemdDetection(true)},
		{"not linux", store.BackendPodman, true, store.LimitSupport{Known: true}, Detection{OS: "darwin", HasSystemd: false, Root: true}},
		{"no systemd", store.BackendPodman, true, store.LimitSupport{Known: true}, Detection{OS: "linux", HasSystemd: false, Root: true}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			u, out := cgroupTestUI()
			r := &recordingRunner{}
			var called bool
			orig := cgroupDelegator
			cgroupDelegator = func(context.Context, commandRunner, int, string, store.BackendKind) error {
				called = true
				return nil
			}
			defer func() { cgroupDelegator = orig }()

			ensureCgroupDelegation(t.Context(), u, c.det, c.kind, c.rootless, "unix:///run/user/1000/podman/podman.sock", c.limits, r.run)

			if called {
				t.Error("the delegation step ran when there was nothing for it to fix")
			}
			if out.Len() != 0 {
				t.Errorf("nothing should be printed when there is nothing to fix, got %q", out.String())
			}
		})
	}
}

// An endpoint that does not name a uid -- a root socket, a TCP one -- gives
// setup nothing to delegate to, so it says nothing rather than guessing.
func TestEnsureCgroupDelegationSkipsAnEndpointWithNoUID(t *testing.T) {
	u, out := cgroupTestUI()
	ensureCgroupDelegation(t.Context(), u, rootlessSystemdDetection(true), store.BackendPodman, true,
		"unix:///var/run/podman.sock", store.LimitSupport{Known: true}, (&recordingRunner{}).run)
	if out.Len() != 0 {
		t.Errorf("an endpoint with no uid should print nothing, got %q", out.String())
	}
}

// A non-root installer cannot write a system unit's drop-in, and the
// operator is told exactly what would fix it -- re-run under sudo -- rather
// than being left to work out why nothing happened.
func TestEnsureCgroupDelegationWithoutRootAsksForSudo(t *testing.T) {
	u, out := cgroupTestUI()
	var called bool
	orig := cgroupDelegator
	cgroupDelegator = func(context.Context, commandRunner, int, string, store.BackendKind) error {
		called = true
		return nil
	}
	defer func() { cgroupDelegator = orig }()

	ensureCgroupDelegation(t.Context(), u, rootlessSystemdDetection(false), store.BackendPodman, true,
		"unix:///run/user/1000/podman/podman.sock", store.LimitSupport{Known: true}, (&recordingRunner{}).run)

	if called {
		t.Fatal("delegation ran without root")
	}
	if !strings.Contains(out.String(), "sudo") {
		t.Errorf("output = %q, want it to say to re-run with sudo", out.String())
	}
}

// A host still on cgroup v1 has nothing to delegate: the fix is a kernel
// command line and a reboot, and setup says so instead of writing a drop-in
// that would do nothing until then.
func TestEnsureCgroupDelegationOnCgroupV1WarnsInsteadOfWriting(t *testing.T) {
	origCgroup := cgroupV2Unified
	cgroupV2Unified = func() bool { return false }
	defer func() { cgroupV2Unified = origCgroup }()

	u, out := cgroupTestUI()
	var called bool
	orig := cgroupDelegator
	cgroupDelegator = func(context.Context, commandRunner, int, string, store.BackendKind) error {
		called = true
		return nil
	}
	defer func() { cgroupDelegator = orig }()

	ensureCgroupDelegation(t.Context(), u, rootlessSystemdDetection(true), store.BackendPodman, true,
		"unix:///run/user/1000/podman/podman.sock", store.LimitSupport{Known: true}, (&recordingRunner{}).run)

	if called {
		t.Fatal("delegation ran on a cgroup v1 host")
	}
	if !strings.Contains(out.String(), "cgroup v1") {
		t.Errorf("output = %q, want it to name cgroup v1 as the reason", out.String())
	}
	if !strings.Contains(out.String(), "unified_cgroup_hierarchy") {
		t.Errorf("output = %q, want the kernel command line fix", out.String())
	}
}

// The one case setup can actually fix on its own: root, systemd, cgroup v2,
// and a daemon that has said it cannot apply a limit. This is the path that
// writes the drop-in and restarts the daemon -- stood in for here so the
// test needs neither.
func TestEnsureCgroupDelegationFixesTheDaemonOnACgroupV2Host(t *testing.T) {
	origCgroup := cgroupV2Unified
	cgroupV2Unified = func() bool { return true }
	defer func() { cgroupV2Unified = origCgroup }()

	u, out := cgroupTestUI()
	var gotUID int
	var gotKind store.BackendKind
	orig := cgroupDelegator
	cgroupDelegator = func(_ context.Context, _ commandRunner, uid int, _ string, kind store.BackendKind) error {
		gotUID, gotKind = uid, kind
		return nil
	}
	defer func() { cgroupDelegator = orig }()

	ensureCgroupDelegation(t.Context(), u, rootlessSystemdDetection(true), store.BackendPodman, true,
		"unix:///run/user/1000/podman/podman.sock", store.LimitSupport{Known: true, CPU: false, Memory: true, Pids: true}, (&recordingRunner{}).run)

	if gotUID != 1000 {
		t.Errorf("delegated to uid %d, want 1000 (from the socket path)", gotUID)
	}
	if gotKind != store.BackendPodman {
		t.Errorf("delegated for backend %q, want podman", gotKind)
	}
	if !strings.Contains(out.String(), "delegated") {
		t.Errorf("output = %q, want it to say the controllers were delegated", out.String())
	}
}

// A restart that fails leaves the operator with the drop-in on disk but no
// confirmation it took effect, so the failure is a warning naming what broke
// rather than a silent success.
func TestEnsureCgroupDelegationReportsAFailedRestart(t *testing.T) {
	origCgroup := cgroupV2Unified
	cgroupV2Unified = func() bool { return true }
	defer func() { cgroupV2Unified = origCgroup }()

	u, out := cgroupTestUI()
	orig := cgroupDelegator
	cgroupDelegator = func(context.Context, commandRunner, int, string, store.BackendKind) error {
		return context.DeadlineExceeded
	}
	defer func() { cgroupDelegator = orig }()

	ensureCgroupDelegation(t.Context(), u, rootlessSystemdDetection(true), store.BackendPodman, true,
		"unix:///run/user/1000/podman/podman.sock", store.LimitSupport{Known: true}, (&recordingRunner{}).run)

	if !strings.Contains(out.String(), "could not delegate") {
		t.Errorf("output = %q, want it to say the delegation failed", out.String())
	}
}

// stubCgroupDropInWrite makes writeCgroupDropIn a no-op, so a test of
// delegateCgroupControllers needs neither root nor a real
// /etc/systemd/system to run against.
func stubCgroupDropInWrite(t *testing.T) {
	t.Helper()
	orig := writeCgroupDropIn
	writeCgroupDropIn = func(string, []byte, os.FileMode) error { return nil }
	t.Cleanup(func() { writeCgroupDropIn = orig })
}

// delegateCgroupControllers itself: the drop-in's path and content, and the
// commands it runs, in the order that matters -- the restart only means
// anything once the reload has picked up the new drop-in.
func TestDelegateCgroupControllersRunsDaemonReloadThenRestartsInTheUsersSession(t *testing.T) {
	stubCgroupDropInWrite(t)
	r := &recordingRunner{}
	if err := delegateCgroupControllers(t.Context(), r.run, 1000, "runner", store.BackendPodman); err != nil {
		t.Fatalf("delegateCgroupControllers: %v", err)
	}
	if !r.ran("systemctl", "daemon-reload") {
		t.Errorf("commands run = %v, want systemctl daemon-reload among them", r.lines())
	}
	if !r.ran("runuser", "-u", "runner", "--", "env", "XDG_RUNTIME_DIR=/run/user/1000", "systemctl", "--user", "restart", "podman") {
		t.Errorf("commands run = %v, want the restart run inside runner's own session", r.lines())
	}
}

// The drop-in itself: every controller a pool's limits can ask for, at the
// path user@<uid>.service.d looks for one.
func TestDelegateCgroupControllersWritesEveryController(t *testing.T) {
	var gotPath string
	var gotContent []byte
	orig := writeCgroupDropIn
	writeCgroupDropIn = func(p string, data []byte, _ os.FileMode) error {
		gotPath, gotContent = p, data
		return nil
	}
	t.Cleanup(func() { writeCgroupDropIn = orig })

	r := &recordingRunner{}
	if err := delegateCgroupControllers(t.Context(), r.run, 1000, "runner", store.BackendPodman); err != nil {
		t.Fatalf("delegateCgroupControllers: %v", err)
	}
	if want := "/etc/systemd/system/user@1000.service.d/zoomies-delegate.conf"; gotPath != want {
		t.Errorf("drop-in path = %q, want %q", gotPath, want)
	}
	for _, controller := range []string{"cpu", "cpuset", "io", "memory", "pids"} {
		if !strings.Contains(string(gotContent), controller) {
			t.Errorf("drop-in content = %q, missing controller %q", gotContent, controller)
		}
	}
}

// A daemon-reload that fails means the drop-in was never picked up, so the
// restart must not run against a slice that still has the old unit.
func TestDelegateCgroupControllersStopsIfDaemonReloadFails(t *testing.T) {
	stubCgroupDropInWrite(t)
	r := &recordingRunner{answer: func(args []string) (string, error) {
		if len(args) > 0 && args[0] == "daemon-reload" {
			return "", context.DeadlineExceeded
		}
		return "", nil
	}}
	if err := delegateCgroupControllers(t.Context(), r.run, 1000, "runner", store.BackendPodman); err == nil {
		t.Fatal("delegateCgroupControllers succeeded despite a failed daemon-reload")
	}
	if r.ran("runuser", "-u", "runner", "--", "env", "XDG_RUNTIME_DIR=/run/user/1000", "systemctl", "--user", "restart", "podman") {
		t.Error("the restart ran even though daemon-reload failed")
	}
}
