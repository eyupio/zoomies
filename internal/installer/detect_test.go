package installer

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/eyupio/zoomies/internal/store"
)

func TestExistingInstallAtFindsNothingInAnEmptyDirectory(t *testing.T) {
	e := ExistingInstallAt(t.TempDir(), t.TempDir(), "")
	if e.Present() {
		t.Fatalf("nothing is installed here, got %+v", e)
	}
	if e.HasState() {
		t.Fatal("no key and no database means no state")
	}
	if len(e.Items()) != 0 {
		t.Fatalf("nothing to list, got %v", e.Items())
	}
}

func TestExistingInstallAtFindsEachPiece(t *testing.T) {
	cfgDir, stateDir := t.TempDir(), t.TempDir()
	writeFile(t, cfgDir, "zoomies.yaml", "server:\n  bind: 127.0.0.1:8080\n")
	writeFile(t, cfgDir, "encryption.key", "not a real key\n")
	writeFile(t, stateDir, "zoomies.db", "")
	if err := os.MkdirAll(filepath.Join(stateDir, "work"), 0o750); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(stateDir, "work"), "agent.json", "{}")

	e := ExistingInstallAt(cfgDir, stateDir, "")
	if !e.Present() || !e.HasState() {
		t.Fatalf("want a present install with state, got %+v", e)
	}
	if e.ConfigFile == "" || e.KeyFile == "" || e.Database == "" || e.AgentState == "" {
		t.Fatalf("a piece was missed: %+v", e)
	}

	items := strings.Join(e.Items(), "\n")
	for _, want := range []string{"config", "encryption key", "database", "agent creds"} {
		if !strings.Contains(items, want) {
			t.Errorf("the summary should name %q:\n%s", want, items)
		}
	}
}

func TestExistingInstallStateIsWhatNeedsAConfirmation(t *testing.T) {
	cfgDir, stateDir := t.TempDir(), t.TempDir()
	writeFile(t, cfgDir, "zoomies.yaml", "")

	e := ExistingInstallAt(cfgDir, stateDir, "")
	if !e.Present() {
		t.Fatal("a config file is an existing install")
	}
	// Only the key and the database are irreplaceable, and only they trigger
	// the typed confirmation.
	if e.HasState() {
		t.Fatalf("a config file alone is not state: %+v", e)
	}
}

func TestExistingInstallReportsTheBinary(t *testing.T) {
	dir := t.TempDir()
	binary := writeFile(t, dir, "zoomies", "#!/bin/sh\nexit 1\n")
	e := ExistingInstallAt(t.TempDir(), t.TempDir(), binary)
	if e.Binary != binary {
		t.Fatalf("binary = %q, want %q", e.Binary, binary)
	}
	if !strings.Contains(strings.Join(e.Items(), "\n"), "binary") {
		t.Fatalf("the summary should name the binary: %v", e.Items())
	}
}

func TestDetectionLinesDescribeTheHost(t *testing.T) {
	d := Detection{
		OS: "linux", Arch: "amd64", Distro: "ubuntu", Init: InitSystemd,
		User: "root", UID: 0, Root: true,
		ConfigDir: "/etc/zoomies", StateDir: "/var/lib/zoomies",
		Docker: RuntimeInfo{Kind: "docker", Available: true, Rootless: true, Version: "27.1.1", Endpoint: "unix:///run/user/1000/docker.sock"},
		Podman: RuntimeInfo{Kind: "podman", Installed: true, Detail: "no socket at /run/podman/podman.sock"},
		Ports:  []PortStatus{{Port: 8080, Free: false, Detail: "address already in use"}},
	}
	lines := strings.Join(d.Lines(), "\n")

	for _, want := range []string{
		"linux/amd64 (ubuntu)",
		"systemd",
		"root",
		"rootless, 27.1.1 -- unix:///run/user/1000/docker.sock",
		"installed but its socket is not reachable",
		"port 8080 is not available",
		"no terminal attached",
	} {
		if !strings.Contains(lines, want) {
			t.Errorf("detection summary is missing %q:\n%s", want, lines)
		}
	}
}

func TestSocketHintOnlyGoesToItsOwnRuntime(t *testing.T) {
	opts := Options{DetectedRuntime: "docker", DetectedSocket: "/run/user/1000/docker.sock"}
	if got := socketHintFor(opts, "docker"); got != "unix:///run/user/1000/docker.sock" {
		t.Fatalf("docker hint = %q", got)
	}
	// Handing a Docker socket to the Podman probe would report a Podman daemon
	// that is not there.
	if got := socketHintFor(opts, "podman"); got != "" {
		t.Fatalf("podman hint = %q, want empty", got)
	}

	unavailable := Options{DetectedRuntime: "docker-unavailable", DetectedSocket: ""}
	if got := socketHintFor(unavailable, "docker"); got != "" {
		t.Fatalf("no socket means no hint, got %q", got)
	}
}

func TestFirstNonEmptyPrefersTheScriptsAnswer(t *testing.T) {
	if got := firstNonEmpty("", "  ", "fedora"); got != "fedora" {
		t.Fatalf("firstNonEmpty = %q", got)
	}
	if got := firstNonEmpty(" alpine ", "debian"); got != "alpine" {
		t.Fatalf("firstNonEmpty = %q", got)
	}
}

// ---------------------------------------------------------------------------
// What the detection step shows and names
// ---------------------------------------------------------------------------

// Fields is the inverse of the `%-12s` padding Lines writes, and the two are
// coupled by nothing but that number: Fields splits at column 12 whatever
// Lines put there. A key one character too long would quietly take its own
// last letters into the value and show the operator a mangled summary, so the
// round trip is the thing worth pinning.
func TestDetectionFieldsSplitEveryLineItRenders(t *testing.T) {
	d := Detection{
		OS: "linux", Arch: "amd64", Distro: "ubuntu", OSVersion: "24.04",
		CPUs: 8, MemoryMB: 16384,
		Init: InitSystemd, Hostname: "build-box",
		User: "ada", UID: 1000, GID: 1000,
		Docker:      RuntimeInfo{Kind: store.BackendDocker, Available: true, Version: "27.1", Endpoint: "unix:///var/run/docker.sock"},
		Podman:      RuntimeInfo{Kind: store.BackendPodman},
		Compose:     ComposeInfo{Command: []string{"docker", "compose"}, Available: true},
		Ports:       []PortStatus{{Port: 8080, Free: false, Detail: "something else is listening"}},
		ConfigDir:   "/etc/zoomies",
		StateDir:    "/var/lib/zoomies",
		Interactive: false,
	}

	lines, fields := d.Lines(), d.Fields()
	if len(lines) != len(fields) {
		t.Fatalf("%d lines but %d fields", len(lines), len(fields))
	}
	for i, f := range fields {
		if f.Key == "" {
			t.Errorf("line %d (%q) split into an empty key", i, lines[i])
		}
		if strings.Contains(f.Key, " ") {
			t.Errorf("line %d split inside a value: key %q", i, f.Key)
		}
		if got := fmt.Sprintf("%-12s%s", f.Key, f.Value); got != lines[i] {
			t.Errorf("line %d does not round trip:\n got %q\nwant %q", i, got, lines[i])
		}
	}

	// The summary is what an operator reads before answering anything, so the
	// facts that change their answers have to be in it.
	joined := strings.Join(lines, "\n")
	for _, want := range []string{"ubuntu", "24.04", "systemd", "ada", "/etc/zoomies", "8080"} {
		if !strings.Contains(joined, want) {
			t.Errorf("the detection summary never mentions %q:\n%s", want, joined)
		}
	}
}

// A host is named after its shape, with the machine's own hostname as the
// discriminator. The hostname is whatever somebody typed into a DHCP lease, so
// it goes through the slug on the way in: a host name ends up on a runner and
// has to be a label, not a sentence with spaces and capitals in it.
func TestDetectionHostNameDescribesTheMachineAndSlugsItsHostname(t *testing.T) {
	shaped := Detection{CPUs: 8, MemoryMB: 16384, Distro: "ubuntu", OSVersion: "24.04", Arch: "amd64", Hostname: "ci-1"}
	name := shaped.HostName()
	for _, want := range []string{"8vcpu", "16gb", "ubuntu"} {
		if !strings.Contains(name, want) {
			t.Errorf("HostName() = %q, which does not carry the machine's %s", name, want)
		}
	}

	// Nothing measurable about the machine, so the name is the hostname --
	// still slugged, and still carrying the prefix every Zoomies name has.
	bare := Detection{Hostname: "Build Box 01"}
	got := bare.HostName()
	if strings.ContainsAny(got, " ") || strings.ToLower(got) != got {
		t.Errorf("HostName() = %q, which is not a usable label", got)
	}
	if !strings.HasSuffix(got, "build-box-01") {
		t.Errorf("HostName() = %q, want the slugged hostname at the end of it", got)
	}
}

// install.sh hands its own compose finding across as a string, and it is the
// answer that wins: the script asked this host moments ago. Parsing it has to
// survive the spacing a shell variable arrives with.
func TestParseComposeCommandTakesTheScriptsAnswer(t *testing.T) {
	for _, tc := range []struct {
		in   string
		want []string
	}{
		{"docker compose", []string{"docker", "compose"}},
		{"  docker-compose  ", []string{"docker-compose"}},
		{"", nil},
		{"   ", nil},
	} {
		got := ParseComposeCommand(tc.in)
		if len(got) != len(tc.want) {
			t.Errorf("ParseComposeCommand(%q) = %v, want %v", tc.in, got, tc.want)
			continue
		}
		for i := range got {
			if got[i] != tc.want[i] {
				t.Errorf("ParseComposeCommand(%q) = %v, want %v", tc.in, got, tc.want)
				break
			}
		}
	}

	// String is how the command is shown back, so it has to be what would be
	// typed rather than Go's rendering of a slice.
	if got := (ComposeInfo{Command: []string{"docker", "compose"}}).String(); got != "docker compose" {
		t.Errorf("ComposeInfo.String() = %q, want %q", got, "docker compose")
	}
}
