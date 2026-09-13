package installer

import (
	"net"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/eyupio/zoomies/internal/store"
)

// newSocketAt makes a real unix socket a test can stat, since these checks read
// the filesystem rather than take a caller's word for the mode.
func newSocketAt(t *testing.T, mode os.FileMode) (path string, facts socketFacts) {
	t.Helper()
	path = filepath.Join(t.TempDir(), "docker.sock")
	l, err := net.Listen("unix", path)
	if err != nil {
		t.Skipf("unix sockets unavailable here: %v", err)
	}
	t.Cleanup(func() { _ = l.Close() })
	if err := os.Chmod(path, mode); err != nil {
		t.Fatal(err)
	}
	facts, ok := statSocket(path)
	if !ok {
		// Off unix a socket has no owning group, so none of the group handling
		// these checks are about applies here.
		t.Skip("this platform reports no owner for a socket")
	}
	return path, facts
}

// A plain container carries its group in its own run command, so the advice
// cannot be about the env file that only compose reads. Sending an operator to
// edit a file that nothing consults is worse than saying nothing.
func TestSocketAdviceForAPlainContainerNamesTheRunFlag(t *testing.T) {
	path, facts := newSocketAt(t, 0o660)
	if facts.gid == 0 {
		t.Skip("this socket belongs to group root, which is a different refusal")
	}

	i, out := newUnattendedInstaller(t, nil)
	i.checkContainerSocketAccess(Plan{
		Deployment: DeploymentDocker, Mode: ModeSingle, Backend: store.BackendDocker,
		DockerHost: "unix://" + path, DockerGID: facts.gid + 1, Embedded: true,
	})

	body := out.String()
	if !strings.Contains(body, "--group-add") {
		t.Errorf("a plain container must be told about --group-add:\n%s", body)
	}
	if strings.Contains(body, "DOCKER_GID=") {
		t.Errorf("a plain container was sent to the compose env file:\n%s", body)
	}
	// The wrong gid means no runner can be created at all, which is the fact
	// the operator needs -- a container that comes up healthy and queues
	// forever is the failure this exists to prevent.
	if !strings.Contains(body, "no runner could be created") {
		t.Errorf("the cost must be stated:\n%s", body)
	}
}

// A socket the container could not open is refused with advice that matches
// what is actually wrong with it: a group of root is never joined, and a mode
// with no group bits cannot be fixed by joining anything.
func TestSocketAdviceMatchesWhyItCannotBeOpened(t *testing.T) {
	path, facts := newSocketAt(t, 0o600)
	if facts.uid == ImageUID {
		t.Skip("this socket is already owned by the image's own account")
	}

	i, out := newUnattendedInstaller(t, nil)
	i.checkContainerSocketAccess(Plan{
		Deployment: DeploymentCompose, Mode: ModeAgent, Backend: store.BackendDocker,
		DockerHost: "unix://" + path, DockerGID: facts.gid,
	})

	body := out.String()
	switch judgeContainerSocket(facts, facts.gid) {
	case socketRootGroup:
		// Zoomies will not put a container in the root group, so the way out
		// is a group of the socket's own or a rootless daemon.
		if !strings.Contains(body, "group root") {
			t.Errorf("a root-owned socket must be named as one:\n%s", body)
		}
		if !strings.Contains(body, "groupadd docker") && !strings.Contains(body, "rootless") {
			t.Errorf("the way out must be offered:\n%s", body)
		}
	case socketNoGroupBits:
		if !strings.Contains(body, "grants nothing to its group") {
			t.Errorf("the mode must be explained:\n%s", body)
		}
		if !strings.Contains(body, "rootless") {
			t.Errorf("the way out must be offered:\n%s", body)
		}
	default:
		t.Skipf("a mode-0600 socket is usable by the image's account here: %s", body)
	}
}

// A host that runs no runners has no socket to reach, so checking one would be
// advice about a problem the operator does not have.
func TestSocketIsNotCheckedOnAHostThatRunsNoRunners(t *testing.T) {
	path, facts := newSocketAt(t, 0o600)

	i, out := newUnattendedInstaller(t, nil)
	for _, p := range []Plan{
		{Deployment: DeploymentCompose, Mode: ModeController, Backend: store.BackendDocker,
			DockerHost: "unix://" + path, DockerGID: facts.gid},
		// The bare-process backend needs no daemon at all.
		{Deployment: DeploymentCompose, Mode: ModeSingle, Embedded: true,
			Backend: store.BackendProcess, DockerHost: "unix://" + path},
		// A TCP endpoint has no path to stat.
		{Deployment: DeploymentCompose, Mode: ModeSingle, Embedded: true,
			Backend: store.BackendDocker, DockerHost: "tcp://docker.example.com:2376"},
	} {
		i.checkContainerSocketAccess(p)
	}
	if out.String() != "" {
		t.Fatalf("a host with no socket to reach was given socket advice:\n%s", out)
	}
}

// A rootless socket belongs to the operator's own user and the image runs as
// uid 65532, so the two do not meet. It is a warning rather than a refusal
// because it often works anyway, and the way out differs per deployment.
func TestRootlessSocketWarningNamesTheRightFix(t *testing.T) {
	compose := Plan{Rootless: true, Embedded: true, Mode: ModeSingle,
		Backend: store.BackendDocker, DockerHost: "unix:///run/user/1000/docker.sock",
		Deployment: DeploymentCompose, DeployDir: "/opt/zoomies"}

	i, out := newUnattendedInstaller(t, nil)
	i.warnAboutRootlessSocket(compose)
	body := out.String()
	if !strings.Contains(body, "uid 65532") {
		t.Errorf("the mismatch must be named:\n%s", body)
	}
	if !strings.Contains(body, `user: "$(id -u):$(id -g)"`) {
		t.Errorf("compose must be told the compose fix:\n%s", body)
	}
	if !strings.Contains(body, filepath.Join("/opt/zoomies", ComposeFileName)) {
		t.Errorf("the file to edit must be named:\n%s", body)
	}

	plain := compose
	plain.Deployment = DeploymentDocker
	i, out = newUnattendedInstaller(t, nil)
	i.warnAboutRootlessSocket(plain)
	if !strings.Contains(out.String(), "--user $(id -u):$(id -g)") {
		t.Errorf("a plain container must be told the run-command fix:\n%s", out)
	}

	// A root socket, or a host that runs no runners, has nothing to warn about.
	i, out = newUnattendedInstaller(t, nil)
	i.warnAboutRootlessSocket(Plan{Embedded: true, Backend: store.BackendDocker})
	i.warnAboutRootlessSocket(Plan{Rootless: true, Mode: ModeController, Backend: store.BackendDocker})
	i.warnAboutRootlessSocket(Plan{Rootless: true, Embedded: true, Backend: store.BackendProcess})
	if out.String() != "" {
		t.Fatalf("a deployment with no rootless socket was warned about one:\n%s", out)
	}
}

// A crash halfway through a write must never leave a half-written unit that
// systemd would then refuse to parse, so the write goes through a temporary
// file and a rename.
func TestWriteFileAtomicLeavesNoHalfWrittenFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "nested", "zoomies.service")

	if err := writeFileAtomic(path, []byte("[Unit]\n"), 0o644); err != nil {
		t.Fatalf("writeFileAtomic: %v", err)
	}
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != "[Unit]\n" {
		t.Fatalf("file = %q", body)
	}
	// Windows models only a read-only bit, so the mode is a POSIX fact.
	if runtime.GOOS != "windows" {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm() != 0o644 {
			t.Fatalf("mode = %v, want 0644", info.Mode().Perm())
		}
	}

	// Rewriting replaces it rather than appending, and leaves no temporary
	// file behind for the next run to trip over.
	if err := writeFileAtomic(path, []byte("[Unit]\nDescription=Zoomies\n"), 0o600); err != nil {
		t.Fatalf("writeFileAtomic again: %v", err)
	}
	body, err = os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != "[Unit]\nDescription=Zoomies\n" {
		t.Fatalf("file = %q, want it replaced", body)
	}
	entries, err := os.ReadDir(filepath.Dir(path))
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".") {
			t.Errorf("a temporary file was left behind: %s", e.Name())
		}
	}
}
