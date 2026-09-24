package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/eyupio/zoomies/internal/cryptox"
)

// settingsHost prepares a state directory with a configuration file, so a test
// can run `zoomies config …` against a real database the way an operator does.
func settingsHost(t *testing.T, body string) string {
	t.Helper()
	dir := isolateHost(t)
	path := filepath.Join(dir, "zoomies.yaml")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("writing %s: %v", path, err)
	}
	t.Setenv("ZOOMIES_STATE_DIR", dir)
	t.Setenv("ZOOMIES_CONFIG_DIR", dir)
	return path
}

// The command an operator reaches for when the settings page is the thing that
// is broken: a stored bind address nothing can connect to.
//
// It is the fix rather than the workaround. The environment layer already wins
// over the database, so a bad value is always recoverable by exporting a
// variable -- but a variable has to be remembered at every restart, and this
// puts the database right.
func TestConfigSetStoresASettingWithoutAControllerRunning(t *testing.T) {
	path := settingsHost(t, "database:\n  path: "+filepath.Join(t.TempDir(), "z.db")+"\n")
	ctx := context.Background()

	e, out, _ := newTestEnv(t)
	if err := runConfigSet(ctx, e, []string{"server.bind", "127.0.0.1:9999", "--config", path}); err != nil {
		t.Fatalf("config set: %v", err)
	}
	if !strings.Contains(out.String(), "server.bind is now 127.0.0.1:9999") {
		t.Errorf("config set said: %q", out.String())
	}

	e, out, _ = newTestEnv(t)
	if err := runConfigGet(ctx, e, []string{"server.bind", "--config", path}); err != nil {
		t.Fatalf("config get: %v", err)
	}
	if got := strings.TrimSpace(out.String()); got != "127.0.0.1:9999" {
		t.Errorf("config get printed %q", got)
	}

	e, out, _ = newTestEnv(t)
	if err := runConfigUnset(ctx, e, []string{"server.bind", "--config", path}); err != nil {
		t.Fatalf("config unset: %v", err)
	}
	if !strings.Contains(out.String(), "no longer stored") {
		t.Errorf("config unset said: %q", out.String())
	}
}

// A key that is read before the database opens cannot be stored in it, and the
// refusal says where to put it instead -- because an operator who has just been
// told "no" is about to go looking for the right place.
func TestConfigSetRefusesAKeyThatOpensTheDatabase(t *testing.T) {
	path := settingsHost(t, "database:\n  path: "+filepath.Join(t.TempDir(), "z.db")+"\n")

	e, _, _ := newTestEnv(t)
	err := runConfigSet(context.Background(), e, []string{"database.path", "/tmp/elsewhere.db", "--config", path})
	if err == nil {
		t.Fatal("database.path was stored in the database it names")
	}
	for _, want := range []string{"before the database can be opened", "ZOOMIES_DB_PATH"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the refusal does not mention %q: %v", want, err)
		}
	}
}

// A value that will not parse stops at the command rather than at the next
// start, when whoever typed it is no longer watching.
func TestConfigSetRefusesAValueThatWillNotParse(t *testing.T) {
	path := settingsHost(t, "database:\n  path: "+filepath.Join(t.TempDir(), "z.db")+"\n")

	e, _, _ := newTestEnv(t)
	err := runConfigSet(context.Background(), e, []string{"scheduler.interval", "soon", "--config", path})
	if err == nil {
		t.Fatal("a nonsense duration was stored")
	}
	if !strings.Contains(err.Error(), "is not a duration") {
		t.Errorf("the refusal does not say what was wrong: %v", err)
	}
}

// The listing says where each value came from, which is the question that
// makes a configuration problem hard: a value in the file and a value in the
// environment are identical once the process is running.
func TestConfigListSaysWhereEachValueCameFrom(t *testing.T) {
	path := settingsHost(t, "database:\n  path: "+filepath.Join(t.TempDir(), "z.db")+
		"\nscheduler:\n  interval: 25s\n")
	ctx := context.Background()

	e, _, _ := newTestEnv(t)
	if err := runConfigSet(ctx, e, []string{"retention.jobs", "720h", "--config", path}); err != nil {
		t.Fatalf("config set: %v", err)
	}

	t.Setenv("ZOOMIES_LOG_LEVEL", "debug")
	e, out, _ := newTestEnv(t)
	if err := runConfigList(ctx, e, []string{"--config", path}); err != nil {
		t.Fatalf("config list: %v", err)
	}
	text := out.String()
	for _, want := range []string{
		"scheduler.interval", "25s", "file",
		"retention.jobs", "720h", "database",
		"log.level", "debug", "environment",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("the listing does not mention %q:\n%s", want, text)
		}
	}
}

// A credential is never printed, and the listing still says one is set --
// which is the fact an operator debugging a configuration actually needs.
func TestConfigCommandsNeverPrintACredential(t *testing.T) {
	dir := t.TempDir()
	keyFile := filepath.Join(dir, "encryption.key")
	key, err := cryptox.GenerateKey()
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}
	if err := cryptox.WriteKeyFile(keyFile, key); err != nil {
		t.Fatalf("WriteKeyFile: %v", err)
	}
	path := settingsHost(t, "database:\n  path: "+filepath.Join(dir, "z.db")+
		"\nsecurity:\n  encryption_key_file: "+keyFile+"\n")
	ctx := context.Background()

	e, _, _ := newTestEnv(t)
	if err := runConfigSet(ctx, e, []string{"capacity_demand.signing_secret", "hunter2", "--config", path}); err != nil {
		t.Fatalf("config set: %v", err)
	}

	for _, run := range []func() string{
		func() string {
			e, out, _ := newTestEnv(t)
			_ = runConfigList(ctx, e, []string{"--config", path})
			return out.String()
		},
		func() string {
			e, out, _ := newTestEnv(t)
			_ = runConfigGet(ctx, e, []string{"capacity_demand.signing_secret", "--config", path})
			return out.String()
		},
	} {
		text := run()
		if strings.Contains(text, "hunter2") {
			t.Errorf("a credential was printed:\n%s", text)
		}
		if !strings.Contains(text, secretPlaceholder) {
			t.Errorf("nothing says the credential is set:\n%s", text)
		}
	}
}

// Setting a credential before there is a key to seal it with is refused, and
// the refusal says what to do. Generating a key here would be the wrong tool
// doing it: only the controller knows whether the database already holds
// something sealed with a different one, and minting a second key over that
// leaves an instance that starts, looks healthy, and cannot read its own
// GitHub credentials.
func TestConfigSetRefusesACredentialWithNoKeyToSealItWith(t *testing.T) {
	path := settingsHost(t, "database:\n  path: "+filepath.Join(t.TempDir(), "z.db")+"\n")

	e, _, _ := newTestEnv(t)
	err := runConfigSet(context.Background(), e,
		[]string{"capacity_demand.signing_secret", "hunter2", "--config", path})
	if err == nil {
		t.Fatal("a credential was stored with nothing to seal it")
	}
	if !strings.Contains(err.Error(), "encryption key") {
		t.Errorf("the refusal does not name what is missing: %v", err)
	}
}

// The upgrade hands a deployment's variables to a one-off container on
// standard input, and this is what stores them. Every setting lands in the
// database; the variables that cannot live there are named and left.
func TestConfigImportEnvStoresWhatAnEnvFileSets(t *testing.T) {
	path := settingsHost(t, "database:\n  path: "+filepath.Join(t.TempDir(), "z.db")+"\n")
	ctx := context.Background()

	e, out, errOut := newTestEnv(t)
	e.in = strings.NewReader("ZOOMIES_BIND=0.0.0.0:8080\nZOOMIES_AGENT_CAPACITY=6\nZOOMIES_IMAGE=ghcr.io/eyupio/zoomies:v1\nDOCKER_GID=998\n")
	if err := runConfigImportEnv(ctx, e, []string{"--config", path}); err != nil {
		t.Fatalf("config import-env: %v", err)
	}
	for _, want := range []string{"server.bind is now 0.0.0.0:8080 (from ZOOMIES_BIND)", "agent.capacity is now 6 (from ZOOMIES_AGENT_CAPACITY)"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("output does not say %q:\n%s", want, out.String())
		}
	}
	if !strings.Contains(errOut.String(), "left ZOOMIES_IMAGE") || strings.Contains(errOut.String(), "DOCKER_GID") {
		t.Errorf("stderr = %q, want ZOOMIES_IMAGE named and Compose's own variables not", errOut.String())
	}

	e, out, _ = newTestEnv(t)
	if err := runConfigGet(ctx, e, []string{"agent.capacity", "--config", path}); err != nil {
		t.Fatalf("config get: %v", err)
	}
	if got := strings.TrimSpace(out.String()); got != "6" {
		t.Errorf("agent.capacity = %q after the import, want 6", got)
	}
}

// One value the controller could not run with stores nothing at all.
func TestConfigImportEnvStoresNothingWhenAValueIsWrong(t *testing.T) {
	path := settingsHost(t, "database:\n  path: "+filepath.Join(t.TempDir(), "z.db")+"\n")
	ctx := context.Background()

	e, _, _ := newTestEnv(t)
	e.in = strings.NewReader("ZOOMIES_BIND=0.0.0.0:8080\nZOOMIES_AGENT_CAPACITY=lots\n")
	if err := runConfigImportEnv(ctx, e, []string{"--config", path}); err == nil || !strings.Contains(err.Error(), "ZOOMIES_AGENT_CAPACITY") {
		t.Fatalf("err = %v, want a refusal naming the variable", err)
	}
	e, out, _ := newTestEnv(t)
	if err := runConfigList(ctx, e, []string{"--config", path}); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out.String(), "server.bind") {
		t.Errorf("server.bind was stored although the import was refused:\n%s", out.String())
	}
}
