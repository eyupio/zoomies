package installer

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/eyupio/zoomies/internal/config"
)

// oldControllerCompose is the Compose file a controller was installed with
// before its settings moved to the database: every one of them handed to the
// container from .env, and the certificate mounted by the variable's name.
const oldControllerCompose = `name: zoomies
services:
  zoomies:
    image: ${ZOOMIES_IMAGE}
    container_name: zoomies
    command: ["controller"]
    environment:
      ZOOMIES_EXTERNAL_URL: ${ZOOMIES_EXTERNAL_URL:?set this in .env}
      ZOOMIES_ENCRYPTION_KEY: ${ZOOMIES_ENCRYPTION_KEY:?set this in .env}
      ZOOMIES_BIND: ${ZOOMIES_BIND}
      ZOOMIES_TLS_MODE: ${ZOOMIES_TLS_MODE}
      ZOOMIES_TLS_CERT_FILE: ${ZOOMIES_TLS_CERT_FILE}
      ZOOMIES_TLS_KEY_FILE: ${ZOOMIES_TLS_KEY_FILE}
      ZOOMIES_TRUSTED_PROXIES: ${ZOOMIES_TRUSTED_PROXIES}
      ZOOMIES_DB_PATH: ${ZOOMIES_DB_PATH}
    volumes:
      - zoomies-data:/var/lib/zoomies
      - /var/lib/zoomies/shared:/var/lib/zoomies/shared
      - /var/run/docker.sock:/var/run/docker.sock
      - ${ZOOMIES_TLS_CERT_FILE}:${ZOOMIES_TLS_CERT_FILE}:ro
      - ${ZOOMIES_TLS_KEY_FILE}:${ZOOMIES_TLS_KEY_FILE}:ro
volumes:
  zoomies-data:
`

const oldControllerEnv = `# keep this comment
ZOOMIES_IMAGE=ghcr.io/eyupio/zoomies:v0.1
ZOOMIES_EXTERNAL_URL=https://zoomies.example.com
ZOOMIES_ENCRYPTION_KEY=c2VjcmV0LWtleS10aGF0LWlzLXRoaXJ0eS10d28h
ZOOMIES_BIND=0.0.0.0:8080
ZOOMIES_TLS_MODE=files
ZOOMIES_TLS_CERT_FILE=/etc/zoomies/cert.pem
ZOOMIES_TLS_KEY_FILE=/etc/zoomies/key.pem
ZOOMIES_TRUSTED_PROXIES=
ZOOMIES_DB_PATH=/var/lib/zoomies/zoomies.db
DOCKER_GID=998
`

// The environment the running container reports: what .env interpolated
// into the Compose file, plus what the image itself sets.
var oldControllerContainerEnv = []string{
	"ZOOMIES_EXTERNAL_URL=https://zoomies.example.com",
	"ZOOMIES_ENCRYPTION_KEY=c2VjcmV0LWtleS10aGF0LWlzLXRoaXJ0eS10d28h",
	"ZOOMIES_BIND=0.0.0.0:8080",
	"ZOOMIES_TLS_MODE=files",
	"ZOOMIES_TLS_CERT_FILE=/etc/zoomies/cert.pem",
	"ZOOMIES_TLS_KEY_FILE=/etc/zoomies/key.pem",
	"ZOOMIES_TRUSTED_PROXIES=",
	"ZOOMIES_DB_PATH=/var/lib/zoomies/zoomies.db",
	"ZOOMIES_STATE_DIR=/var/lib/zoomies",
	"PATH=/usr/local/bin:/usr/bin",
}

// controllerUpgrade is a Compose controller installed before this release,
// and fakes for the commands an upgrade of it runs.
type controllerUpgrade struct {
	opts    UpgradeOptions
	rec     DeploymentRecord
	out     bytes.Buffer
	calls   []string
	inputs  []string
	failOn  string
	failRun error
}

func newControllerUpgrade(t *testing.T) *controllerUpgrade {
	t.Helper()
	u := &controllerUpgrade{}
	opts, _ := upgradeFixture(t, DeploymentCompose)
	dir := opts.ConfigDir
	rec := DeploymentRecord{Deployment: DeploymentCompose, Directory: dir, EnvFile: filepath.Join(dir, ".env"),
		Image: "ghcr.io/eyupio/zoomies:v0.1", Mode: ModeSingle, Container: "zoomies", ComposeCommand: []string{"docker", "compose"}}
	if err := os.WriteFile(rec.EnvFile, []byte(oldControllerEnv), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(rec.ComposeFile(), []byte(oldControllerCompose), 0o640); err != nil {
		t.Fatal(err)
	}
	if _, err := WriteDeploymentRecord(dir, rec); err != nil {
		t.Fatal(err)
	}
	opts.Mode = ModeSingle
	opts.Image = "ghcr.io/eyupio/zoomies:v9.0"
	opts.AssumeYes = true
	opts.Out = &u.out
	envJSON, _ := json.Marshal(oldControllerContainerEnv)
	opts.run = func(_ context.Context, name string, args ...string) (string, error) {
		line := name + " " + strings.Join(args, " ")
		u.calls = append(u.calls, line)
		switch {
		case u.failOn != "" && strings.Contains(line, u.failOn):
			return "", errors.New("test refusal")
		case strings.Contains(line, "config --images"):
			return opts.Image, nil
		case strings.Contains(line, "{{json .Config.Env}}"):
			return string(envJSON), nil
		case name == "docker" && len(args) > 0 && args[0] == "inspect":
			return "true", nil
		}
		return "", nil
	}
	opts.runInput = func(_ context.Context, input, name string, args ...string) (string, error) {
		u.calls = append(u.calls, name+" "+strings.Join(args, " "))
		u.inputs = append(u.inputs, input)
		if u.failRun != nil {
			return "", u.failRun
		}
		return "server.bind is now 0.0.0.0:8080 (from ZOOMIES_BIND)", nil
	}
	u.opts, u.rec = opts, rec
	return u
}

func (u *controllerUpgrade) callIndex(part string) int {
	for i, c := range u.calls {
		if strings.Contains(c, part) {
			return i
		}
	}
	return -1
}

// The report behind this: the upgrade took a deployment's settings from its
// .env, when every setting lives in the database. A controller installed
// before that has them in its .env, where each one pins the settings page, so
// the upgrade stores them in the database -- with the controller stopped,
// through the new image -- and only then comments them out, so the
// controller comes back up on the same values from where they belong.
func TestAnUpgradeMovesAControllersSettingsIntoItsDatabase(t *testing.T) {
	u := newControllerUpgrade(t)
	if err := Upgrade(context.Background(), u.opts); err != nil {
		t.Fatalf("Upgrade: %v\n%s", err, u.out.String())
	}

	stop, run, up := u.callIndex(" stop "), u.callIndex("run --rm --no-deps -T zoomies config import-env"), u.callIndex(" up -d ")
	if stop < 0 || run < stop || up < run {
		t.Fatalf("want stop, then the import, then up; calls:\n%s", strings.Join(u.calls, "\n"))
	}
	if !strings.Contains(u.calls[run], "ZOOMIES_IMAGE="+u.opts.Image) {
		t.Errorf("the import did not run the new image: %s", u.calls[run])
	}
	if len(u.inputs) != 1 {
		t.Fatalf("inputs = %q", u.inputs)
	}
	in := u.inputs[0]
	for _, want := range []string{"ZOOMIES_EXTERNAL_URL=https://zoomies.example.com", "ZOOMIES_BIND=0.0.0.0:8080",
		"ZOOMIES_TLS_MODE=files", "ZOOMIES_TLS_CERT_FILE=/etc/zoomies/cert.pem"} {
		if !strings.Contains(in, want+"\n") {
			t.Errorf("the import was not given %s:\n%s", want, in)
		}
	}
	// The key opens the database; an empty value that is the default has
	// nothing to keep.
	for _, not := range []string{"ZOOMIES_ENCRYPTION_KEY", "ZOOMIES_DB_PATH", "ZOOMIES_TRUSTED_PROXIES", "PATH="} {
		if strings.Contains(in, not) {
			t.Errorf("the import was given %s:\n%s", not, in)
		}
	}

	env, _ := os.ReadFile(u.rec.EnvFile)
	vars, _ := ParseEnvFile(u.rec.EnvFile)
	for _, gone := range []string{"ZOOMIES_EXTERNAL_URL", "ZOOMIES_BIND", "ZOOMIES_TLS_MODE", "ZOOMIES_TLS_CERT_FILE", "ZOOMIES_TRUSTED_PROXIES"} {
		if _, ok := vars[gone]; ok {
			t.Errorf("%s is still set in .env:\n%s", gone, env)
		}
	}
	for _, kept := range []string{"ZOOMIES_ENCRYPTION_KEY", "ZOOMIES_DB_PATH", "DOCKER_GID"} {
		if _, ok := vars[kept]; !ok {
			t.Errorf("%s was taken out of .env, which the controller needs before its database opens", kept)
		}
	}
	if vars["ZOOMIES_IMAGE"] != u.opts.Image {
		t.Errorf("image = %q", vars["ZOOMIES_IMAGE"])
	}
	if !strings.Contains(string(env), "# ZOOMIES_BIND=0.0.0.0:8080") || !strings.Contains(string(env), "server.bind lives in the database now") ||
		!strings.Contains(string(env), "# keep this comment") {
		t.Errorf(".env does not say where the settings went, or lost its own comments:\n%s", env)
	}

	compose, _ := os.ReadFile(u.rec.ComposeFile())
	if strings.Contains(string(compose), "ZOOMIES_BIND") || strings.Contains(string(compose), "${ZOOMIES_TLS_CERT_FILE}") {
		t.Errorf("the Compose file still hands the container a moved setting:\n%s", compose)
	}
	for _, want := range []string{"ZOOMIES_ENCRYPTION_KEY", "/etc/zoomies/cert.pem:/etc/zoomies/cert.pem:ro", "${ZOOMIES_IMAGE}"} {
		if !strings.Contains(string(compose), want) {
			t.Errorf("the Compose file lost %s:\n%s", want, compose)
		}
	}
	if matches, _ := filepath.Glob(u.rec.ComposeFile() + ".bak.*"); len(matches) == 0 {
		t.Error("the Compose file as it was is not kept")
	}
	if !strings.Contains(u.out.String(), "ZOOMIES_BIND (server.bind) = 0.0.0.0:8080") || !strings.Contains(u.out.String(), "Moved 6 settings") {
		t.Errorf("the operator was not told what moved:\n%s", u.out.String())
	}
}

// A new image that cannot store them -- an older one pinned, a database it
// cannot open -- moves nothing: the files stay as they were, and the upgrade
// goes on, because the controller runs as it did.
func TestAnUpgradeThatCannotStoreTheSettingsLeavesThemWhereTheyAre(t *testing.T) {
	u := newControllerUpgrade(t)
	u.failRun = errors.New("unknown command \"import-env\"")
	composeBefore, _ := os.ReadFile(u.rec.ComposeFile())
	if err := Upgrade(context.Background(), u.opts); err != nil {
		t.Fatalf("Upgrade: %v", err)
	}
	vars, _ := ParseEnvFile(u.rec.EnvFile)
	if vars["ZOOMIES_BIND"] != "0.0.0.0:8080" {
		t.Errorf("ZOOMIES_BIND was taken out although nothing was stored")
	}
	if compose, _ := os.ReadFile(u.rec.ComposeFile()); !bytes.Equal(compose, composeBefore) {
		t.Errorf("the Compose file was edited although nothing was stored")
	}
	if u.callIndex(" up -d ") < 0 || !strings.Contains(u.out.String(), "Could not move the settings") {
		t.Errorf("the upgrade did not go on, or did not say why nothing moved:\n%s", u.out.String())
	}
}

// A recreate that fails rolls back to the old image, which may predate the
// settings it would now find only in the database: the files go back as they
// were before the move, not just before the image line.
func TestAFailedUpgradePutsTheSettingsBackInTheEnvironment(t *testing.T) {
	u := newControllerUpgrade(t)
	u.failOn = "--force-recreate --timeout 1200 zoomies"
	if err := Upgrade(context.Background(), u.opts); err == nil {
		t.Fatal("the upgrade reported success")
	}
	env, _ := os.ReadFile(u.rec.EnvFile)
	if string(env) != oldControllerEnv {
		t.Errorf(".env was not put back:\n%s", env)
	}
	if compose, _ := os.ReadFile(u.rec.ComposeFile()); string(compose) != oldControllerCompose {
		t.Errorf("the Compose file was not put back:\n%s", compose)
	}
}

// Nothing changes without the operator's say: unattended, the upgrade lists
// what it would move and the command that moves it.
func TestAnUnattendedUpgradeOnlySaysWhichSettingsItWouldMove(t *testing.T) {
	u := newControllerUpgrade(t)
	u.opts.AssumeYes = false
	if err := Upgrade(context.Background(), u.opts); err != nil {
		t.Fatalf("Upgrade: %v", err)
	}
	if len(u.inputs) != 0 || u.callIndex(" stop ") >= 0 {
		t.Errorf("settings were moved without approval: %v", u.calls)
	}
	if vars, _ := ParseEnvFile(u.rec.EnvFile); vars["ZOOMIES_BIND"] == "" {
		t.Error("ZOOMIES_BIND was commented out without approval")
	}
	if !strings.Contains(u.out.String(), "ZOOMIES_BIND (server.bind)") || !strings.Contains(u.out.String(), "zoomies upgrade --yes") {
		t.Errorf("the operator was not told what would move and how:\n%s", u.out.String())
	}
}

// An agent has no database to move anything to, so its environment is its
// configuration and the upgrade leaves it alone.
func TestAnAgentsEnvironmentIsNotOffered(t *testing.T) {
	opts, _ := upgradeFixture(t, DeploymentCompose)
	var out bytes.Buffer
	opts.Out = &out
	opts.AssumeYes = true
	opts.run = func(_ context.Context, name string, args ...string) (string, error) {
		line := name + " " + strings.Join(args, " ")
		if strings.Contains(line, "{{json .Config.Env}}") {
			return `["ZOOMIES_AGENT_CAPACITY=4","ZOOMIES_DOCKER_HOST=unix:///var/run/docker.sock"]`, nil
		}
		if strings.Contains(line, "config --images") {
			return opts.Image, nil
		}
		if name == "docker" && len(args) > 0 && args[0] == "inspect" {
			return "true", nil
		}
		return "", nil
	}
	opts.runInput = func(context.Context, string, string, ...string) (string, error) {
		t.Error("an agent's settings were imported into a database it does not have")
		return "", nil
	}
	if err := Upgrade(context.Background(), opts); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out.String(), "override its database") {
		t.Errorf("an agent was offered a move:\n%s", out.String())
	}
}

// A docker deployment's container carries its environment in its own
// configuration, which the replacement copies: commenting the file out would
// change nothing. So the import runs on the stopped controller's volumes, and
// the replacement is created without the variables.
func TestADockerUpgradeCreatesTheReplacementWithoutTheMovedVariables(t *testing.T) {
	var created map[string]json.RawMessage
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path
		switch {
		case strings.HasSuffix(path, "/zoomies/json"):
			_ = json.NewEncoder(w).Encode(map[string]any{"Id": "old", "Name": "/zoomies",
				"Config":     map[string]any{"Image": "ghcr.io/eyupio/zoomies:v0.1", "Env": oldControllerContainerEnv},
				"HostConfig": map[string]any{"AutoRemove": false}, "State": map[string]any{"Running": true}})
		case strings.HasSuffix(path, "/zoomies-before-upgrade/json"):
			w.WriteHeader(404)
		case strings.HasSuffix(path, "/containers/create"):
			body, _ := io.ReadAll(r.Body)
			_ = json.Unmarshal(body, &created)
			w.WriteHeader(201)
			_, _ = w.Write([]byte(`{"Id":"new"}`))
		case strings.HasSuffix(path, "/new/json"):
			_, _ = w.Write([]byte(`{"Id":"new","State":{"Running":true}}`))
		default:
			w.WriteHeader(204)
		}
	}))
	defer server.Close()

	opts, _ := upgradeFixture(t, DeploymentDocker)
	dir := opts.ConfigDir
	rec := DeploymentRecord{Deployment: DeploymentDocker, Directory: dir, EnvFile: filepath.Join(dir, DockerEnvFileName),
		Image: "ghcr.io/eyupio/zoomies:v0.1", Mode: ModeSingle, Container: "zoomies"}
	if err := os.WriteFile(rec.EnvFile, []byte(oldControllerEnv), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := WriteDeploymentRecord(dir, rec); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	opts.Out, opts.Mode, opts.Image, opts.AssumeYes = &out, ModeSingle, "ghcr.io/eyupio/zoomies:v9.0", true
	opts.DockerHost = server.URL
	opts.run = func(context.Context, string, ...string) (string, error) { return "", nil }
	var importArgs string
	opts.runInput = func(_ context.Context, input, name string, args ...string) (string, error) {
		importArgs = name + " " + strings.Join(args, " ")
		if !strings.Contains(input, "ZOOMIES_BIND=0.0.0.0:8080") {
			t.Errorf("input = %q", input)
		}
		return "", nil
	}
	if err := Upgrade(context.Background(), opts); err != nil {
		t.Fatalf("Upgrade: %v\n%s", err, out.String())
	}
	for _, want := range []string{"--volumes-from old", "--network none", "--env-file " + rec.EnvFile, opts.Image + " config import-env"} {
		if !strings.Contains(importArgs, want) {
			t.Errorf("the import ran as %q, want %s", importArgs, want)
		}
	}
	var env []string
	_ = json.Unmarshal(created["Env"], &env)
	joined := strings.Join(env, "\n")
	if strings.Contains(joined, "ZOOMIES_BIND") || strings.Contains(joined, "ZOOMIES_TLS_MODE") ||
		!strings.Contains(joined, "ZOOMIES_ENCRYPTION_KEY=") || !strings.Contains(joined, "PATH=") {
		t.Errorf("the replacement's environment is wrong:\n%s", joined)
	}
	if vars, _ := ParseEnvFile(rec.EnvFile); vars["ZOOMIES_BIND"] != "" || vars["ZOOMIES_ENCRYPTION_KEY"] == "" {
		t.Errorf("%s was not commented out as the container was: %v", rec.EnvFile, vars)
	}
}

func TestCommentOutEnvKeepsNoCredentialInTheComment(t *testing.T) {
	secret, _ := config.SettingForEnv("ZOOMIES_OIDC_CLIENT_SECRET")
	bind, _ := config.SettingForEnv("ZOOMIES_BIND")
	body := "# already a comment: ZOOMIES_BIND=1.2.3.4:1\nexport ZOOMIES_BIND=0.0.0.0:8080\nZOOMIES_OIDC_CLIENT_SECRET=hunter2\nOTHER=x"
	got := string(commentOutEnv([]byte(body), []movedSetting{{env: "ZOOMIES_BIND", setting: bind}, {env: "ZOOMIES_OIDC_CLIENT_SECRET", setting: secret}},
		time.Date(2026, 9, 24, 0, 0, 0, 0, time.UTC)))
	if strings.Contains(got, "hunter2") {
		t.Errorf("the credential is still in the file:\n%s", got)
	}
	for _, want := range []string{"# already a comment: ZOOMIES_BIND=1.2.3.4:1\n", "# export ZOOMIES_BIND=0.0.0.0:8080\n",
		"on 2026-09-24: server.bind lives in the database now", "\nOTHER=x"} {
		if !strings.Contains(got, want) {
			t.Errorf("want %q in:\n%s", want, got)
		}
	}
	vars, _ := ParseEnv(strings.NewReader(got))
	if len(vars) != 1 || vars["OTHER"] != "x" {
		t.Errorf("vars after = %v", vars)
	}
}

// A Compose file can name a variable as a list item as well as a mapping,
// and anywhere else in the service; a literal $ in a value is Compose's to
// interpolate unless it is doubled.
func TestRemoveComposeEnvHandlesAListAndEscapesADollar(t *testing.T) {
	proxies, _ := config.SettingForEnv("ZOOMIES_TRUSTED_PROXIES")
	cert, _ := config.SettingForEnv("ZOOMIES_TLS_CERT_FILE")
	body := `services:
  zoomies:
    image: ${ZOOMIES_IMAGE}
    environment:
      - ZOOMIES_TRUSTED_PROXIES=10.0.0.0/8
      - ZOOMIES_ENCRYPTION_KEY=${ZOOMIES_ENCRYPTION_KEY}
    volumes:
      - "${ZOOMIES_TLS_CERT_FILE:?set it}:/cert.pem:ro"
`
	got, err := removeComposeEnv([]byte(body), []movedSetting{
		{env: "ZOOMIES_TRUSTED_PROXIES", setting: proxies, value: "10.0.0.0/8"},
		{env: "ZOOMIES_TLS_CERT_FILE", setting: cert, value: "/etc/$weird/cert.pem"},
	})
	if err != nil {
		t.Fatal(err)
	}
	s := string(got)
	if strings.Contains(s, "ZOOMIES_TRUSTED_PROXIES") || !strings.Contains(s, "ZOOMIES_ENCRYPTION_KEY=${ZOOMIES_ENCRYPTION_KEY}") {
		t.Errorf("environment list:\n%s", s)
	}
	if !strings.Contains(s, "/etc/$$weird/cert.pem:/cert.pem:ro") || !strings.Contains(s, "${ZOOMIES_IMAGE}") {
		t.Errorf("substitution:\n%s", s)
	}
	if _, err := removeComposeEnv([]byte("services:\n  other: {}\n"), nil); err == nil {
		t.Error("a Compose file with no zoomies service was edited")
	}
}

// A controller's Compose file hands its container no setting: each would pin
// the database, and the settings page would show it locked. An agent's does,
// because an agent has no database and its environment is its configuration.
func TestAControllersComposeFileHandsItNoSetting(t *testing.T) {
	controller, err := RenderComposeFile(ComposeFileSpec{Mode: ModeSingle, Publish: true, MountSocket: true, MountShared: true,
		TLSCertFile: "/etc/zoomies/cert.pem", TLSKeyFile: "/etc/zoomies/key.pem"})
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range interpolation.FindAllStringSubmatch(controller, -1) {
		name := m[1] + m[2]
		if _, stored := config.SettingForEnv(name); stored {
			t.Errorf("the controller's Compose file reads %s, which would pin a setting over the database", name)
		}
	}
	if !strings.Contains(controller, "- /etc/zoomies/cert.pem:/etc/zoomies/cert.pem:ro") {
		t.Errorf("the certificate is not mounted by its own path:\n%s", controller)
	}

	agent, err := RenderComposeFile(ComposeFileSpec{Mode: ModeAgent, MountSocket: true, MountShared: true})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"ZOOMIES_AGENT_CAPACITY: ${ZOOMIES_AGENT_CAPACITY}", "ZOOMIES_DOCKER_HOST: ${ZOOMIES_DOCKER_HOST}", "ZOOMIES_LOG_LEVEL"} {
		if !strings.Contains(agent, want) {
			t.Errorf("an agent's Compose file lost %s:\n%s", want, agent)
		}
	}
}
