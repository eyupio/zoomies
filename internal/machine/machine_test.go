package machine

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/eyupio/zoomies/internal/naming"
)

// fakeRoot builds a filesystem root carrying the files detect reads.
func fakeRoot(t *testing.T, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	for name, body := range files {
		path := filepath.Join(root, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("MkdirAll: %v", err)
		}
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatalf("WriteFile: %v", err)
		}
	}
	return root
}

const ubuntuOSRelease = `PRETTY_NAME="Ubuntu 24.04.1 LTS"
NAME="Ubuntu"
VERSION_ID="24.04"
VERSION="24.04.1 LTS (Noble Numbat)"
ID=ubuntu
ID_LIKE=debian
`

func TestDetectReadsTheDistribution(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("the os-release probe is the Linux path")
	}
	root := fakeRoot(t, map[string]string{
		"etc/os-release": ubuntuOSRelease,
		"proc/meminfo":   "MemTotal:       32906892 kB\nMemFree:         1234 kB\n",
	})
	m := detect(root)
	if m.Distro != "ubuntu" || m.OSVersion != "24.04" {
		t.Errorf("distro/version = %q/%q, want ubuntu/24.04", m.Distro, m.OSVersion)
	}
	if m.MemoryMB != 32135 {
		t.Errorf("MemoryMB = %d, want 32135 (32906892 kB)", m.MemoryMB)
	}
	if m.CPUs != runtime.NumCPU() {
		t.Errorf("CPUs = %d, want this machine's %d", m.CPUs, runtime.NumCPU())
	}
}

func TestDetectPrefersACgroupLimit(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("cgroups are the Linux path")
	}
	// An agent inside a two-core container must not tell the controller it has
	// the host's cores: every pool sized from that number would be wrong.
	root := fakeRoot(t, map[string]string{
		"etc/os-release":           ubuntuOSRelease,
		"proc/meminfo":             "MemTotal:       32906892 kB\n",
		"sys/fs/cgroup/cpu.max":    "150000 100000\n",
		"sys/fs/cgroup/memory.max": "2147483648\n",
	})
	m := detect(root)
	// 1.5 cores rounds up: the container can still burst onto the second.
	if m.CPUs != 2 {
		t.Errorf("CPUs = %d, want 2 from a 1.5-core quota", m.CPUs)
	}
	if m.MemoryMB != 2048 {
		t.Errorf("MemoryMB = %d, want 2048 from the cgroup limit", m.MemoryMB)
	}
}

func TestDetectIgnoresAnUnlimitedCgroup(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("cgroups are the Linux path")
	}
	root := fakeRoot(t, map[string]string{
		"etc/os-release":           ubuntuOSRelease,
		"proc/meminfo":             "MemTotal:       32906892 kB\n",
		"sys/fs/cgroup/cpu.max":    "max 100000\n",
		"sys/fs/cgroup/memory.max": "max\n",
	})
	m := detect(root)
	if m.CPUs != runtime.NumCPU() {
		t.Errorf("CPUs = %d, want this machine's %d", m.CPUs, runtime.NumCPU())
	}
	if m.MemoryMB != 32135 {
		t.Errorf("MemoryMB = %d, want the host's total", m.MemoryMB)
	}
}

func TestDetectSaysNothingRatherThanGuessing(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("the os-release probe is the Linux path")
	}
	// A distribution the catalogue does not know must not end up in the
	// column: it would never match a pool, and it would look like a promise.
	root := fakeRoot(t, map[string]string{
		"etc/os-release": "ID=voidlinux\nVERSION_ID=rolling\n",
	})
	m := detect(root)
	if m.Distro != "" {
		t.Errorf("Distro = %q, want empty for a distribution Zoomies does not know", m.Distro)
	}
	// An empty root: no os-release, no meminfo, no cgroup. Detection must
	// still return something usable rather than fail.
	bare := detect(t.TempDir())
	if bare.OS != runtime.GOOS || bare.Arch != runtime.GOARCH || bare.CPUs < 1 {
		t.Errorf("detect on a bare root = %+v", bare)
	}
}

func TestSpecNamesTheMachine(t *testing.T) {
	m := Facts{OS: "linux", Distro: "ubuntu", OSVersion: "24.04", Arch: "arm64", CPUs: 16, MemoryMB: 32768}
	if got, want := m.Spec("build01").String(), "zoomies-16vcpu-32gb-ubuntu-2404-arm64-build01"; got != want {
		t.Errorf("Spec = %q, want %q", got, want)
	}
	// A host that cannot say what distribution it is falls back to the kernel,
	// which at least tells macOS and Windows apart from Linux.
	mac := Facts{OS: "darwin", OSVersion: "15", Arch: "arm64", CPUs: 8}
	if got, want := mac.Spec("laptop").String(), "zoomies-8vcpu-macos-15-arm64-laptop"; got != want {
		t.Errorf("Spec = %q, want %q", got, want)
	}
}

func TestDefaultHostNameIsBranded(t *testing.T) {
	name := DefaultHostName()
	if name == "" {
		t.Fatal("DefaultHostName returned nothing")
	}
	// It is either a canonical name or, on a machine that says nothing about
	// itself, the slugged hostname. Neither may be empty or unusable.
	if err := naming.Validate(name); err != nil {
		t.Errorf("DefaultHostName returned %q, which is not a usable name: %v", name, err)
	}
}
