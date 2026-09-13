package installer

import (
	"net"
	"os"
	"os/user"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
)

// A port the installer would bind is checked by binding it, because a port
// free in /proc can still be refused by a container's network namespace. The
// honest test is the one the operator's host will perform.
//
// The port here is held on every interface, which is the scope checkPort uses:
// Windows lets a wildcard bind succeed over one held on the loopback address
// alone, so a test that held only 127.0.0.1 would be asking a question with
// two different answers depending on the platform.
func TestAPortInUseIsReportedWithTheReasonItCouldNotBeTaken(t *testing.T) {
	ln, err := net.Listen("tcp", ":0")
	if err != nil {
		t.Fatalf("taking a port: %v", err)
	}
	defer func() { _ = ln.Close() }()
	port := ln.Addr().(*net.TCPAddr).Port

	busy := checkPort(port)
	if busy.Port != port {
		t.Errorf("port = %d, want the one that was asked about", busy.Port)
	}
	if busy.Free {
		t.Fatalf("port %d is held by this test and was reported free", port)
	}
	if strings.TrimSpace(busy.Detail) == "" {
		t.Error("a port that could not be bound was reported with nothing to say why")
	}
	if PortFree("", port) {
		t.Errorf("PortFree said %d was free on every interface while this test held it", port)
	}

	// And once it is given up, both answers change.
	if err := ln.Close(); err != nil {
		t.Fatalf("giving the port back: %v", err)
	}
	if free := checkPort(port); !free.Free || free.Detail != "" {
		t.Errorf("checkPort after the listener closed = %+v, want free and nothing to explain", free)
	}
	if !PortFree("127.0.0.1", port) {
		t.Errorf("PortFree said %d was taken after the listener closed", port)
	}
}

// The host is described well enough to fill a plan: a name for the runner
// pool, an OS and architecture for the image, and the directories the rest of
// setup writes to.
//
// The installed binary is named rather than left to default. Detect asks an
// installed binary its version by running it, and the default is this
// process -- so a test that left it out ran the test binary again, as a child,
// with every test in the package inside it. On Windows the still-running copy
// then held its own file open and `go test` could not clean up after itself.
func TestDetectDescribesTheHostThePlanNeeds(t *testing.T) {
	cfg, state := t.TempDir(), t.TempDir()
	binary := filepath.Join(t.TempDir(), "zoomies")
	d := Detect(t.Context(), Options{
		NonInteractive: true, ConfigDir: cfg, StateDir: state, InstalledBinary: binary,
	})

	if d.OS != runtime.GOOS || d.Arch != runtime.GOARCH {
		t.Errorf("detected %s/%s, want this host's own %s/%s", d.OS, d.Arch, runtime.GOOS, runtime.GOARCH)
	}
	if d.Hostname == "" {
		t.Error("no hostname; the first pool's runners are named after it")
	}
	if d.User == "" {
		t.Error("no user; the installer decides whether it can create a service account from it")
	}
	if d.ConfigDir != cfg || d.StateDir != state {
		t.Errorf("directories = %q and %q, want the ones the caller gave", d.ConfigDir, d.StateDir)
	}
	if d.UID != os.Geteuid() || d.Root != (os.Geteuid() == 0) {
		t.Errorf("uid = %d, root = %v; want this process's own identity", d.UID, d.Root)
	}
	if d.Interactive {
		t.Error("a run told not to prompt was detected as interactive")
	}
	if len(d.Ports) == 0 {
		t.Error("no port was checked, so the summary cannot warn about one already in use")
	}
	if d.BinaryPath != binary {
		t.Errorf("binary path = %q, want the one install.sh named, %q", d.BinaryPath, binary)
	}
}

// install.sh probed this host moments ago, before any privilege change, so
// what it found is trusted over anything re-derived here.
func TestTheScriptsFindingsWinOverLocalProbing(t *testing.T) {
	d := Detect(t.Context(), Options{
		NonInteractive:  true,
		ConfigDir:       t.TempDir(),
		StateDir:        t.TempDir(),
		InstalledBinary: filepath.Join(t.TempDir(), "zoomies"),
		DetectedDistro:  "alpine",
		DetectedInit:    string(InitOpenRC),
	})
	if d.Distro != "alpine" {
		t.Errorf("distro = %q, want the script's own answer", d.Distro)
	}
	if d.Init != InitOpenRC {
		t.Errorf("init = %q, want the script's own answer", d.Init)
	}
	// An init system nobody supervises with is not claimed as one that is.
	if d.HasSystemd || d.HasLaunchd {
		t.Errorf("host reports systemd=%v launchd=%v under OpenRC", d.HasSystemd, d.HasLaunchd)
	}
}

// The account questions are asked of the host, because uninstall runs userdel
// only for an account that is really there, and the summary names the user as
// `ls -l` would.
func TestTheLocalAccountQuestionsAreAnsweredByTheHost(t *testing.T) {
	me, err := user.Current()
	if err != nil {
		t.Skipf("this host has no current user to ask about: %v", err)
	}
	if !userExists(me.Username) {
		t.Errorf("userExists(%q) = false for the account running this test", me.Username)
	}
	if userExists("zoomies-no-such-account-9f3a") {
		t.Error("an account that does not exist was reported present, so uninstall would run userdel for nothing")
	}

	uid, err := strconv.Atoi(me.Uid)
	if err != nil {
		t.Skipf("uid %q is not a number on this platform", me.Uid)
	}
	if got := currentUserName(uid); got != me.Username {
		t.Errorf("currentUserName(%d) = %q, want %q", uid, got, me.Username)
	}
	// A uid with no account still reads as something an operator can match up
	// with what they see on disk.
	if got := ownerName(1 << 30); got != strconv.Itoa(1<<30) {
		t.Errorf("ownerName for an unknown uid = %q, want the number itself", got)
	}
}

// The distro is what a pool's platform is matched against, so it always says
// something -- "unknown" is a worse answer than a name, and an empty one
// would match nothing at all.
func TestTheDistroIsAlwaysAnswered(t *testing.T) {
	if got := readDistroID(); strings.TrimSpace(got) == "" {
		t.Error("readDistroID returned nothing; a pool's platform would match no image")
	}
	if runtime.GOOS == "darwin" && readDistroID() != "macos" {
		t.Errorf("readDistroID() = %q on darwin, want macos", readDistroID())
	}
}

// The name a host is known by never comes back empty: it goes into the first
// pool's runner names, and "-runner-1" would be a puzzle in the UI.
func TestTheHostnameFallsBackRatherThanBeingEmpty(t *testing.T) {
	if hostname() == "" {
		t.Error("hostname() = \"\", want a fallback")
	}
}
