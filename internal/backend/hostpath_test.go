package backend

import (
	"errors"
	"strings"
	"testing"
)

func TestMountPointsReadsTheFifthFieldOfMountinfo(t *testing.T) {
	info := strings.Join([]string{
		`36 35 98:0 /mnt1 / rw,noatime master:1 - ext3 /dev/root rw,errors=continue`,
		`812 790 0:52 /volumes/zoomies-data/_data /var/lib/zoomies rw,relatime - ext4 /dev/vda rw`,
		`813 812 0:52 /srv/shared /var/lib/zoomies/shared rw,relatime - ext4 /dev/vda rw`,
		`900 790 0:60 / /mnt/with\040space rw - tmpfs tmpfs rw`,
	}, "\n")
	got := mountPoints(strings.NewReader(info))
	for _, want := range []string{"/", "/var/lib/zoomies", "/var/lib/zoomies/shared", "/mnt/with space"} {
		if !got[want] {
			t.Errorf("%s is not a mount point in %v", want, got)
		}
	}
	if got["/var/lib/zoomies/work"] {
		t.Error("a folder inside a mount was read as a mount of its own")
	}
}

// Outside a container the folder is the host's by definition; inside one it
// is only the host's when it is mounted from there.
func TestSharedFolderProblemOnlyInAContainerThatDoesNotMountIt(t *testing.T) {
	mounted := func(string) bool { return true }
	unmounted := func(string) bool { return false }
	if p := SharedFolderProblem("/var/lib/zoomies/shared", false, unmounted); p != "" {
		t.Errorf("outside a container: %q", p)
	}
	if p := SharedFolderProblem("/var/lib/zoomies/shared", true, mounted); p != "" {
		t.Errorf("mounted in a container: %q", p)
	}
	p := SharedFolderProblem("/var/lib/zoomies/shared", true, unmounted)
	for _, want := range []string{"not mounted into this container", "- /var/lib/zoomies/shared:/var/lib/zoomies/shared", "zoomies upgrade --yes"} {
		if !strings.Contains(p, want) {
			t.Errorf("problem %q does not say %q", p, want)
		}
	}
}

// An older Compose file can lack the socket mount altogether. Inside a
// container that is not a daemon to install -- the advice outside one -- but a
// mount to add, and the detail the Hosts page shows says which.
func TestAMissingSocketInAContainerIsAMissingMount(t *testing.T) {
	was := runningInContainer
	runningInContainer = func() bool { return true }
	t.Cleanup(func() { runningInContainer = was })
	sock := t.TempDir() + "/docker.sock"
	b, err := NewDocker(DockerOptions{Host: "unix://" + sock, Logger: quietLogger()})
	if err != nil {
		t.Fatal(err)
	}
	detail := b.unreachableDetail(errors.New("dial unix: no such file"))
	for _, want := range []string{"inside this container", "- " + sock + ":" + sock, "zoomies upgrade --yes"} {
		if !strings.Contains(detail, want) {
			t.Errorf("detail %q does not say %q", detail, want)
		}
	}
}
