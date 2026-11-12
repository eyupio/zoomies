package backend

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

const fullID = "3f2a9c1e5b7d4f608192a3b4c5d6e7f8091a2b3c4d5e6f708192a3b4c5d6e7f8"

// cgroupTree writes a fake /proc and /sys/fs/cgroup holding one process and the
// memory.events of the folder its container was given.
func cgroupTree(t *testing.T, pid, procLine, eventsDir, events string) (procRoot, cgroupRoot string) {
	t.Helper()
	root := t.TempDir()
	procRoot, cgroupRoot = filepath.Join(root, "proc"), filepath.Join(root, "cgroup")
	if err := os.MkdirAll(filepath.Join(procRoot, pid), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(procRoot, pid, "cgroup"), []byte(procLine+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if eventsDir != "" {
		dir := filepath.Join(cgroupRoot, eventsDir)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "memory.events"), []byte(events), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return procRoot, cgroupRoot
}

const eventsWithKill = "low 0\nhigh 0\nmax 41\noom 3\noom_kill 2\noom_group_kill 0\n"

// The kernel's count is only this runner's if the folder it is read from is
// this container's. A daemon in a virtual machine or on another host reports a
// process ID that names some other process here, and that process's kills must
// never be put on a job.
func TestTheKillCountIsReadOnlyFromACgroupThatNamesTheContainer(t *testing.T) {
	scope := "system.slice/docker-" + fullID + ".scope"
	for name, tc := range map[string]struct {
		procLine, eventsDir string
		want                bool
	}{
		"systemd driver":                       {"0::/" + scope, scope, true},
		"cgroupfs driver":                      {"0::/docker/" + fullID, "docker/" + fullID, true},
		"the first process moved into a child": {"0::/" + scope + "/init", scope, true},
		"another process's cgroup":             {"0::/system.slice/ssh.service", "system.slice/ssh.service", false},
		"cgroup v1 only":                       {"5:memory:/docker/" + fullID, "docker/" + fullID, false},
		"no memory controller there":           {"0::/" + scope, "", false},
		"a path that climbs out":               {"0::/../" + fullID, "", false},
	} {
		procRoot, cgroupRoot := cgroupTree(t, "4242", tc.procLine, tc.eventsDir, eventsWithKill)
		dir := cgroupDirOf(procRoot, cgroupRoot, 4242, fullID)
		if (dir != "") != tc.want {
			t.Errorf("%s: cgroup folder = %q, want found=%v", name, dir, tc.want)
			continue
		}
		if tc.want {
			if n, ok := readOOMKills(filepath.Join(dir, "memory.events")); !ok || n != 2 {
				t.Errorf("%s: kills = %d, %v; want 2", name, n, ok)
			}
		}
	}
}

// A build in a docker-in-docker sidecar runs in a container of the sidecar's own
// daemon. The kernel killed the compiler there, the sidecar's first process
// lived, the outer daemon's flag stayed false, and the job was recorded as the
// workflow's own failure with nothing to say it needed more memory. The
// kernel's count, read while the sidecar ran, is what carries it.
func TestAKillBelowTheSidecarThatTheDaemonsFlagMissedIsStillAttributed(t *testing.T) {
	e := newSidecarEngine(t,
		ContainerState{Status: "exited", ExitCode: 0},
		ContainerState{Status: "running", Running: true, Pid: 4242})
	b := dockerBackendFor(t, e.fakeEngine, DockerOptions{})

	list := func() Workload {
		t.Helper()
		got, err := b.List(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		for _, w := range got {
			if !w.Sidecar {
				return w
			}
		}
		t.Fatal("no runner listed")
		return Workload{}
	}
	if w := list(); w.Status.OOMKilled {
		t.Fatalf("status = %+v before any kill was counted; a clean job must stay clean", w.Status)
	}

	// The fake's sidecar is "dind1", so the cgroup has to name that.
	b.procRoot, b.cgroupRoot = cgroupTree(t, "4242", "0::/docker/dind1", "docker/dind1", eventsWithKill)
	b.noteOOMKills(context.Background(), "dind1")

	w := list()
	if !w.Status.OOMKilled || !w.Status.SidecarOOMKilled || w.Status.Phase != PhaseFailed {
		t.Fatalf("status = %+v, want the kernel's count carried onto the runner as a sidecar kill", w.Status)
	}
}

// A count of nothing, a container with no process yet and a file that is not
// there are all "cannot tell", and none of them may mark a job.
func TestNoKillIsRememberedWhereTheKernelCountedNone(t *testing.T) {
	e := newSidecarEngine(t,
		ContainerState{Status: "exited", ExitCode: 0},
		ContainerState{Status: "running", Running: true, Pid: 4242})
	b := dockerBackendFor(t, e.fakeEngine, DockerOptions{})
	b.procRoot, b.cgroupRoot = cgroupTree(t, "4242", "0::/docker/dind1", "docker/dind1", "low 0\noom 0\noom_kill 0\n")
	b.noteOOMKills(context.Background(), "dind1")
	b.noteOOMKills(context.Background(), "run1") // exited: no process to find
	if n := b.oomKillsSeen("dind1") + b.oomKillsSeen("run1"); n != 0 {
		t.Fatalf("kills remembered = %d, want none", n)
	}
	if _, ok := readOOMKills(filepath.Join(t.TempDir(), "memory.events")); ok {
		t.Fatal("a missing memory.events read as an answer")
	}
}

// The ledger follows the host's own listing, so a host that has run ten
// thousand runners remembers the ones it still has.
func TestKillsOfContainersThatAreGoneAreForgotten(t *testing.T) {
	b := &DockerBackend{}
	b.oomKills.kills = map[string]int64{"gone": 3, "here": 1}
	b.oomKills.dirs = map[string]string{"gone": "/x", "here": "/y"}
	b.forgetOOMKillsExcept(map[string]bool{"here": true})
	if b.oomKillsSeen("gone") != 0 || b.oomKillsSeen("here") != 1 || len(b.oomKills.dirs) != 1 {
		t.Fatalf("ledger = %+v", b.oomKills.kills)
	}
}
