package installer

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/eyupio/zoomies/internal/config"
	"github.com/eyupio/zoomies/internal/store"
)

func reviewValue(t *testing.T, lines []ReviewLine, key string) string {
	t.Helper()
	for _, l := range lines {
		if l.Key == key {
			return l.Value
		}
	}
	t.Fatalf("the review screen has no %q line: %+v", key, lines)
	return ""
}

func hasReviewKey(lines []ReviewLine, key string) bool {
	for _, l := range lines {
		if l.Key == key {
			return true
		}
	}
	return false
}

// An installer that writes to /etc and /var/lib without saying what it is
// about to do is the reason people ctrl-c at the last question. Every setting
// that changes the host has to be on the screen the operator approves.
func TestReviewNamesEverythingThatChangesTheHost(t *testing.T) {
	p := Plan{
		Mode: ModeSingle, Deployment: DeploymentNative, Service: ServiceSystemd,
		ServiceUser: "zoomies", ServiceGroup: "zoomies",
		Embedded: true, Backend: store.BackendDocker, DockerHost: "unix:///var/run/docker.sock",
		Capacity: 4, Bind: "127.0.0.1:8080", TLSMode: config.TLSOff,
		ExternalURL: "http://localhost:8080", AdminUser: "admin",
		GitHub: GitHubPlan{Target: "acme", TargetType: store.TargetOrg},
	}

	lines := p.Review()

	if got := reviewValue(t, lines, "this host"); !strings.Contains(got, "embedded agent") {
		t.Errorf("this host = %q, want it to say the agent is embedded", got)
	}
	if got := reviewValue(t, lines, "run as"); !strings.Contains(got, "systemd") {
		t.Errorf("run as = %q, want the service manager named", got)
	}
	if got := reviewValue(t, lines, "account"); got != "zoomies:zoomies" {
		t.Errorf("account = %q", got)
	}
	// The socket is on the screen because which daemon a runner reaches is the
	// difference between a container escape landing on root and landing on an
	// unprivileged account.
	if got := reviewValue(t, lines, "backend"); !strings.Contains(got, "/var/run/docker.sock") {
		t.Errorf("backend = %q, want the socket named", got)
	}
	if got := reviewValue(t, lines, "capacity"); got != "4 runners at once" {
		t.Errorf("capacity = %q", got)
	}
	if got := reviewValue(t, lines, "listener"); !strings.Contains(got, "127.0.0.1:8080") {
		t.Errorf("listener = %q", got)
	}
	if got := reviewValue(t, lines, "github"); !strings.Contains(got, "acme (org)") {
		t.Errorf("github = %q", got)
	}
	if got := reviewValue(t, lines, "admin"); got != "admin" {
		t.Errorf("admin = %q", got)
	}
	// A native deployment builds nothing, so there is no image to show.
	if hasReviewKey(lines, "image") {
		t.Error("a native deployment was offered an image line")
	}
}

// A rootless daemon is the safe answer and the screen has to say which one was
// found, because "docker" alone does not distinguish them.
func TestReviewMarksARootlessDaemon(t *testing.T) {
	p := Plan{
		Mode: ModeSingle, Deployment: DeploymentNative, Embedded: true,
		Backend: store.BackendDocker, DockerHost: "unix:///run/user/1000/docker.sock",
		Rootless: true, Capacity: 1, TLSMode: config.TLSOff,
	}

	if got := reviewValue(t, p.Review(), "backend"); !strings.Contains(got, "rootless") {
		t.Errorf("backend = %q, want it marked rootless", got)
	}
	// One runner is not "1 runners".
	if got := reviewValue(t, p.Review(), "capacity"); got != "1 runner at once" {
		t.Errorf("capacity = %q", got)
	}
}

// A containerised deployment makes its administrator in the browser and shows
// the image instead of an account, because there is no account on this host to
// show and the image is the single line saying which build it becomes.
func TestReviewOfAContainerShowsTheImageAndNoAccount(t *testing.T) {
	p := Plan{
		Mode: ModeSingle, Deployment: DeploymentCompose, DeployDir: "/opt/zoomies",
		Image: "ghcr.io/eyupio/zoomies:1.0.0", Bind: "0.0.0.0:8080",
		PublishAddr: "0.0.0.0", PublishedPort: 443, TLSMode: config.TLSOff,
		ExternalURL: "https://zoomies.example.com",
	}

	lines := p.Review()

	if got := reviewValue(t, lines, "image"); got != "ghcr.io/eyupio/zoomies:1.0.0" {
		t.Errorf("image = %q", got)
	}
	if hasReviewKey(lines, "account") {
		t.Error("a containerised deployment was offered a service account")
	}
	if got := reviewValue(t, lines, "admin"); !strings.Contains(got, "browser") {
		t.Errorf("admin = %q, want it created in the browser", got)
	}
	// A published port is two addresses, and the operator needs both: the one
	// they will reach and the one inside the container.
	if got := reviewValue(t, lines, "listener"); !strings.Contains(got, "0.0.0.0:443") || !strings.Contains(got, "0.0.0.0:8080") {
		t.Errorf("listener = %q, want both sides of the publish", got)
	}
	if got := reviewValue(t, lines, "run as"); !strings.Contains(got, "/opt/zoomies") {
		t.Errorf("run as = %q, want the project directory", got)
	}
}

// Skipping GitHub is a decision, not an omission, and it has to read as one on
// the screen where the operator is deciding.
func TestReviewSaysWhenGitHubIsBeingLeftForLater(t *testing.T) {
	skipped := Plan{Mode: ModeSingle, Deployment: DeploymentNative, TLSMode: config.TLSOff,
		GitHub: GitHubPlan{Skip: true}}
	if got := reviewValue(t, skipped.Review(), "github"); !strings.Contains(got, "later") {
		t.Errorf("github = %q, want it to say later", got)
	}

	fresh := Plan{Mode: ModeSingle, Deployment: DeploymentNative, TLSMode: config.TLSOff}
	if got := reviewValue(t, fresh.Review(), "github"); !strings.Contains(got, "browser") {
		t.Errorf("github = %q, want the browser handshake", got)
	}
}

// Every mode gets a sentence of its own, because "controller" and "controller
// with an embedded agent" are different machines and the difference is the
// whole reason a fleet has more than one host.
func TestReviewDescribesEachMode(t *testing.T) {
	for mode, want := range map[Mode]string{
		ModeSingle:     "embedded agent",
		ModeController: "join it separately",
		ModeAgent:      "a runner host",
	} {
		p := Plan{Mode: mode, Deployment: DeploymentNative, TLSMode: config.TLSOff}
		if got := reviewValue(t, p.Review(), "this host"); !strings.Contains(got, want) {
			t.Errorf("%s: this host = %q, want it to mention %q", mode, got, want)
		}
	}
}

// A native install with no service manager has nothing to restart it, which is
// a real choice with a real cost -- so the screen says so rather than leaving
// the "run as" line reading like any other.
func TestReviewSaysWhenNothingWillRestartIt(t *testing.T) {
	p := Plan{Mode: ModeSingle, Deployment: DeploymentNative, Service: ServiceNone, TLSMode: config.TLSOff}

	if got := reviewValue(t, p.Review(), "run as"); !strings.Contains(got, "no service manager") {
		t.Errorf("run as = %q, want the absence of a supervisor named", got)
	}
}

// An operator who can see exactly which files are involved can decide in a
// second; one who cannot has to take the installer's word for it.
func TestWritesNamesTheFilesEachDeploymentTouches(t *testing.T) {
	native := Plan{
		Deployment: DeploymentNative, Service: ServiceSystemd,
		ConfigFile: "/etc/zoomies/zoomies.yaml", KeyFile: "/etc/zoomies/key",
		DBPath: "/var/lib/zoomies/zoomies.db",
	}
	got := native.Writes()
	for _, want := range []string{"/etc/zoomies/zoomies.yaml", "/var/lib/zoomies/zoomies.db"} {
		if !containsSubstring(got, want) {
			t.Errorf("Writes() = %v, want it to name %q", got, want)
		}
	}
	// The key's mode is part of the promise: 0600 is what stops anything that
	// can read the config from decrypting every stored secret.
	if !containsSubstring(got, "0600") {
		t.Errorf("Writes() = %v, want the key's mode shown", got)
	}
	if !containsSubstring(got, SystemdUnitPath(UnitController)) {
		t.Errorf("Writes() = %v, want the unit file named", got)
	}

	// With no service manager there is no unit to write.
	bare := native
	bare.Service = ServiceNone
	if containsSubstring(bare.Writes(), ".service") {
		t.Errorf("a serviceless install claimed it would write a unit: %v", bare.Writes())
	}

	// A containerised deployment writes into its project directory and nowhere
	// else on the host; the database lives in a volume.
	compose := Plan{Deployment: DeploymentCompose, DeployDir: "/opt/zoomies", StateDir: "/var/lib/zoomies"}
	got = compose.Writes()
	if len(got) != 3 {
		t.Fatalf("Writes() = %v, want three paths", got)
	}
	if got[0] != filepath.Join("/opt/zoomies", "docker-compose.yml") || got[1] != filepath.Join("/opt/zoomies", ".env") {
		t.Errorf("Writes() = %v, want the compose project's two files first", got)
	}
	if !strings.Contains(got[2], "volume") {
		t.Errorf("the database line = %q, want it to say the volume", got[2])
	}
}

// "Did a previous run finish?" is asked read-only, because a read-write open
// would migrate the database as a side effect of the question -- and a
// database that cannot be opened is not evidence of anything either way.
func TestFinishedIsFalseUntilThereIsAnAdministrator(t *testing.T) {
	ctx := t.Context()
	dir := t.TempDir()

	if finished(ctx, "") {
		t.Error("an empty path was reported as a finished install")
	}
	if finished(ctx, filepath.Join(dir, "absent.db")) {
		t.Error("a database that does not exist was reported as a finished install")
	}

	// A file that is not a database answers false rather than failing the run.
	junk := writeFile(t, dir, "junk.db", "not a database")
	if finished(ctx, junk) {
		t.Error("an unreadable database was reported as a finished install")
	}

	// A real database with no users is an abandoned run: the remaining steps
	// are all safe to repeat.
	path := filepath.Join(dir, "zoomies.db")
	st, err := store.Open(ctx, store.Options{Path: path})
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	if err := st.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if finished(ctx, path) {
		t.Error("a database with no administrator was reported as finished")
	}

	st, err = store.Open(ctx, store.Options{Path: path})
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	if err := st.CreateUser(ctx, &store.User{Username: "admin", Role: store.RoleAdmin}); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	if err := st.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if !finished(ctx, path) {
		t.Error("a database with an administrator was not reported as finished")
	}
}

func containsSubstring(haystack []string, needle string) bool {
	for _, s := range haystack {
		if strings.Contains(s, needle) {
			return true
		}
	}
	return false
}
