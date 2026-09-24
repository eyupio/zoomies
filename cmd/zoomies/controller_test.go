package main

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/eyupio/zoomies/internal/agent"
	"github.com/eyupio/zoomies/internal/config"
	"github.com/eyupio/zoomies/internal/cryptox"
	"github.com/eyupio/zoomies/internal/store"
)

func TestControllerRefusesToStartOnAConfigurationError(t *testing.T) {
	// The point is that it stops before opening the database or binding a
	// port: a controller that half-starts on a bad configuration is worse than
	// one that says what to change.
	e, out, errOut := newTestEnv(t)
	isolateHost(t)
	path := writeConfig(t, badConfig)

	if code := dispatch(context.Background(), e, []string{"controller", "--config", path}); code != exitError {
		t.Fatalf("exit code = %d, want %d", code, exitError)
	}
	if strings.Contains(out.String(), "listening on") {
		t.Errorf("a banner was printed for a controller that never started:\n%s", out)
	}
	for _, want := range []string{"server.bind", "log.level", "fix:"} {
		if !strings.Contains(errOut.String(), want) {
			t.Errorf("the report does not mention %q:\n%s", want, errOut)
		}
	}
}

func TestControllerReportsAnUnreadableConfigFile(t *testing.T) {
	e, _, _ := newTestEnv(t)
	isolateHost(t)

	code := dispatch(context.Background(), e, []string{"controller", "--config", filepath.Join(t.TempDir(), "absent.yaml")})
	if code != exitError {
		t.Fatalf("exit code = %d, want %d", code, exitError)
	}
}

func TestBannerNamesTheSixThingsAnOperatorChecks(t *testing.T) {
	cfg := config.Default()
	cfg.Server.Bind = "127.0.0.1:8080"
	cfg.Server.ExternalURL = "https://zoomies.example.com"
	cfg.Database.Path = "/var/lib/zoomies/zoomies.db"
	cfg.Agent.Embedded = true
	cfg.Agent.Backend = "docker"
	cfg.Agent.Name = "builder-1"
	cfg.Agent.Capacity = 4

	var buf bytes.Buffer
	printBanner(&buf, cfg, nil)

	for _, want := range []string{
		"▄██▄ ██████   ██████ ▄██▄",
		"off the lead, on the job",
		"http://127.0.0.1:8080",
		"https://zoomies.example.com",
		"https://zoomies.example.com/webhooks/github",
		"docker",
		"builder-1",
		"/var/lib/zoomies/zoomies.db",
		"(no file: defaults plus ZOOMIES_* environment)",
	} {
		if !strings.Contains(buf.String(), want) {
			t.Errorf("the banner does not mention %q:\n%s", want, buf.String())
		}
	}
}

func TestBannerSaysWhenThereIsNoExternalURL(t *testing.T) {
	// An empty webhook URL is the single most common reason a fleet does not
	// scale, so the banner has to name it rather than print a blank.
	cfg := config.Default()
	cfg.Agent.Embedded = false

	var buf bytes.Buffer
	printBanner(&buf, cfg, nil)

	if !strings.Contains(buf.String(), "server.external_url") {
		t.Errorf("a missing external URL was not called out:\n%s", buf.String())
	}
	if !strings.Contains(buf.String(), "hosts no runners") {
		t.Errorf("a controller without an embedded agent should say so:\n%s", buf.String())
	}
}

func TestLogLevelCanBeChangedWhileRunning(t *testing.T) {
	level := new(slog.LevelVar)
	level.Set(slog.LevelInfo)

	var buf bytes.Buffer
	inner := slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})
	log := slog.New(&levelHandler{Handler: inner, level: level})

	log.Debug("not yet")
	if buf.Len() != 0 {
		t.Errorf("a debug line escaped at info level: %s", buf.String())
	}

	level.Set(slog.LevelDebug)
	log.Debug("now")
	if !strings.Contains(buf.String(), "now") {
		t.Errorf("the level change did not take effect: %s", buf.String())
	}

	// Loggers derived before the change must follow it too: the controller and
	// the API both capture their own With(...) loggers at startup.
	derived := log.With("component", "test")
	buf.Reset()
	level.Set(slog.LevelError)
	derived.Warn("suppressed")
	if buf.Len() != 0 {
		t.Errorf("a derived logger ignored the level change: %s", buf.String())
	}
}

func TestBuildBackendsRefusesABackendThisHostCannotProvide(t *testing.T) {
	cfg := config.Default()
	cfg.Agent.WorkDir = t.TempDir()
	cfg.Agent.Backend = "wibble"

	t.Setenv("ZOOMIES_STATE_DIR", t.TempDir())
	_, err := buildBackends(context.Background(), cfg, discardLogger())
	if err == nil {
		t.Fatal("an unknown backend was accepted")
	}
	if !strings.Contains(err.Error(), "agent.backend") {
		t.Errorf("the error does not name the setting: %v", err)
	}
}

// The compose deployment names one socket, for one backend. Giving that socket
// to the Podman backend as well made the Hosts page show the same permission
// denial twice, and -- worse -- a reachable Docker socket answered the Podman
// probe too, so a Docker host advertised a Podman backend it did not have.
func TestBuildBackendsGivesTheExplicitSocketOnlyToTheConfiguredBackend(t *testing.T) {
	cfg := config.Default()
	cfg.Agent.WorkDir = t.TempDir()
	cfg.Agent.Backend = "docker"
	cfg.Agent.DockerHost = "unix://" + filepath.Join(t.TempDir(), "docker.sock")

	t.Setenv("ZOOMIES_STATE_DIR", t.TempDir())
	reg, err := buildBackends(context.Background(), cfg, discardLogger())
	if err != nil {
		t.Fatalf("buildBackends: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	for _, info := range reg.Probe(ctx) {
		switch info.Kind {
		case store.BackendDocker:
			if info.Endpoint != cfg.Agent.DockerHost {
				t.Errorf("docker endpoint = %q, want the configured %q", info.Endpoint, cfg.Agent.DockerHost)
			}
		case store.BackendPodman:
			if info.Endpoint == cfg.Agent.DockerHost {
				t.Errorf("the podman backend was given the docker socket %q", info.Endpoint)
			}
		}
	}
}

// The compose deployment configures docker and nothing else, and the image it
// runs in has neither a Podman socket nor a shell. Registering Podman and the
// process backend anyway gave the Hosts page two red rows of advice that could
// not be followed -- "install Podman", "apt-get install libicu" -- on every
// such host.
func TestBuildBackendsLeavesOutBackendsThisHostVisiblyLacks(t *testing.T) {
	cfg := config.Default()
	cfg.Agent.WorkDir = t.TempDir()
	cfg.Agent.Backend = "docker"
	cfg.Agent.DockerHost = "unix://" + filepath.Join(t.TempDir(), "docker.sock")
	// No shell, and nowhere a per-user Podman socket could be found.
	t.Setenv("PATH", t.TempDir())
	t.Setenv("XDG_RUNTIME_DIR", t.TempDir())

	t.Setenv("ZOOMIES_STATE_DIR", t.TempDir())
	reg, err := buildBackends(context.Background(), cfg, discardLogger())
	if err != nil {
		t.Fatalf("buildBackends: %v", err)
	}
	kinds := reg.Kinds()
	if len(kinds) != 1 || kinds[0] != store.BackendDocker {
		t.Fatalf("registered %v, want only the configured docker backend (its socket may be down, and is still reported on)", kinds)
	}
}

// The upgrade path: an instance that has been running on a configuration file
// meets this build with an empty settings table.
//
// Without the import it would get a settings page showing every value as "from
// the file" and offering to store a second copy of each one. With it, the
// file's settings are the fleet's settings, the file stays as the layer
// underneath them, and nothing about what the controller runs changes.
func TestAConfigurationFileIsCarriedIntoTheDatabaseOnce(t *testing.T) {
	dir := isolateHost(t)
	ctx := context.Background()
	path := filepath.Join(dir, "zoomies.yaml")
	if err := os.WriteFile(path, []byte(
		"scheduler:\n  interval: 25s\nretention:\n  jobs: 1440h\n"), 0o600); err != nil {
		t.Fatalf("writing the file: %v", err)
	}

	cfg, err := config.Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	st := keyStore(t)
	key, err := cryptox.GenerateKey()
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}

	findings, err := seedSettingsFromFile(ctx, st, cfg, key, discardLogger())
	if err != nil {
		t.Fatalf("seedSettingsFromFile: %v", err)
	}
	if len(findings) != 1 || findings[0].Code != "settings.imported_from_file" {
		t.Errorf("the import said nothing an operator would see: %v", findings)
	}
	rows, err := st.InstanceSettings(ctx)
	if err != nil {
		t.Fatalf("InstanceSettings: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("imported %d settings, want 2: %+v", len(rows), rows)
	}

	// Once, and only once. An operator who then clears a setting back to its
	// default must not find the file poured back in at the next start.
	if err := st.DeleteInstanceSettings(ctx, []string{"scheduler.interval"}); err != nil {
		t.Fatalf("DeleteInstanceSettings: %v", err)
	}
	again, err := seedSettingsFromFile(ctx, st, cfg, key, discardLogger())
	if err != nil {
		t.Fatalf("the second import: %v", err)
	}
	if len(again) != 0 {
		t.Errorf("the import ran a second time: %v", again)
	}
	if _, err := st.GetInstanceSetting(ctx, "scheduler.interval"); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("a cleared setting came back from the file: %v", err)
	}
}

// A fleet already configured through the settings page is not one the file has
// anything to say about, so nothing is imported over it.
func TestAFleetWithStoredSettingsIsNotOverwrittenByItsFile(t *testing.T) {
	dir := isolateHost(t)
	ctx := context.Background()
	path := filepath.Join(dir, "zoomies.yaml")
	if err := os.WriteFile(path, []byte("scheduler:\n  interval: 25s\n"), 0o600); err != nil {
		t.Fatalf("writing the file: %v", err)
	}
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	st := keyStore(t)
	if err := st.PutInstanceSettings(ctx, "an administrator", []store.InstanceSetting{
		{Key: "scheduler.interval", Value: "5s"},
	}); err != nil {
		t.Fatalf("PutInstanceSettings: %v", err)
	}

	key, _ := cryptox.GenerateKey()
	if _, err := seedSettingsFromFile(ctx, st, cfg, key, discardLogger()); err != nil {
		t.Fatalf("seedSettingsFromFile: %v", err)
	}
	row, err := st.GetInstanceSetting(ctx, "scheduler.interval")
	if err != nil {
		t.Fatalf("GetInstanceSetting: %v", err)
	}
	if row.Value != "5s" {
		t.Errorf("the file overwrote a stored setting: %q", row.Value)
	}
}

// A log level stored in the database is in force from the first line the
// assembled configuration could have affected.
//
// The gate is set before the store opens -- the store's own startup has things
// to say, and there is nowhere else to say them -- so that first setting knows
// only the file and the environment. Without a second one after the rows are
// read, log.level would be the one setting the registry calls live and the one
// a fresh start quietly ignored.
func TestAStoredLogLevelIsInForceAfterTheDatabaseIsRead(t *testing.T) {
	isolateHost(t)
	cfg := config.Default()
	_, level := setupLogging(cfg)
	if level.Level() != slog.LevelInfo {
		t.Fatalf("the default level is %s, want info", level.Level())
	}

	key, err := cryptox.GenerateKey()
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}
	if _, err := cfg.Rebuild([]store.InstanceSetting{{Key: "log.level", Value: "debug"}}, key); err != nil {
		t.Fatalf("Rebuild: %v", err)
	}
	applyLogLevel(level, cfg)

	if level.Level() != slog.LevelDebug {
		t.Errorf("the gate is at %s after a stored debug level, want debug", level.Level())
	}
}

func TestControllerShutdownOutlastsEmbeddedAgent(t *testing.T) {
	if stopGrace <= agent.ShutdownTimeout {
		t.Fatalf("controller grace %s must exceed agent shutdown %s", stopGrace, agent.ShutdownTimeout)
	}
}

// An agent lays out the shared folder when it starts, so a release that adds a
// folder to config.SharedLayout has it on every host by the next start, and a
// pool's tool cache has somewhere to live.
func TestBuildBackendsLaysOutTheSharedFolder(t *testing.T) {
	notInAContainer(t)
	state := t.TempDir()
	t.Setenv("ZOOMIES_STATE_DIR", state)
	cfg := config.Default()
	cfg.Agent.WorkDir = t.TempDir()
	if _, err := buildBackends(context.Background(), cfg, discardLogger()); err != nil {
		t.Fatalf("buildBackends: %v", err)
	}
	for _, sub := range config.SharedLayout {
		if fi, err := os.Stat(filepath.Join(state, "shared", filepath.FromSlash(sub))); err != nil || !fi.IsDir() {
			t.Errorf("shared/%s was not created: %v", sub, err)
		}
	}
}

// notInAContainer makes this process describe a host that is not a container,
// whatever the machine running the tests is.
func notInAContainer(t *testing.T) {
	t.Helper()
	was := inContainer
	inContainer = func() bool { return false }
	t.Cleanup(func() { inContainer = was })
}

// A containerised agent whose container does not mount the shared folder from
// the host would create it inside its own data volume, and hand runners a path
// the host's daemon resolves to an empty folder of root's. It leaves the tool
// cache off instead, creates nothing, and says how to mount it.
func TestAContainerWithoutTheSharedMountKeepsNoToolCache(t *testing.T) {
	state := t.TempDir()
	t.Setenv("ZOOMIES_STATE_DIR", state)
	dir, problem := sharedDirIn(discardLogger(), true, func(string) bool { return false })
	if dir != "" {
		t.Errorf("dir = %q, want none", dir)
	}
	if !strings.Contains(problem, "not mounted into this container") || !strings.Contains(problem, "zoomies upgrade --yes") {
		t.Errorf("problem = %q, want one saying it is not mounted and how to mount it", problem)
	}
	if _, err := os.Stat(filepath.Join(state, "shared")); !os.IsNotExist(err) {
		t.Error("the folder was created inside the container anyway")
	}

	// Mounted from the host, it is used.
	dir, problem = sharedDirIn(discardLogger(), true, func(string) bool { return true })
	if dir == "" || problem != "" {
		t.Errorf("a mounted folder was refused: %q, %q", dir, problem)
	}
}
