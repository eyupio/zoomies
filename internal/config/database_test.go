package config

import (
	"os"
	"strings"
	"testing"
	"time"

	"github.com/eyupio/zoomies/internal/cryptox"
	"github.com/eyupio/zoomies/internal/store"
)

// isolateEnvironment removes every ZOOMIES_* variable for the duration of a
// test, so a test about the layers is not also a test of whatever the machine
// running it happens to export.
//
// This repository builds itself on its own runners, and those set
// ZOOMIES_DOCKER_HOST and ZOOMIES_RUNNER_VERSION -- so a test asserting which
// settings the environment is holding passed on every laptop and failed in the
// one place it had to pass.
func isolateEnvironment(t *testing.T) {
	t.Helper()
	for _, entry := range os.Environ() {
		key, _, _ := strings.Cut(entry, "=")
		if !strings.HasPrefix(key, "ZOOMIES_") && key != "DOCKER_HOST" {
			continue
		}
		was := os.Getenv(key)
		if err := os.Unsetenv(key); err != nil {
			t.Fatalf("unsetting %s: %v", key, err)
		}
		t.Cleanup(func() { _ = os.Setenv(key, was) })
	}
}

func testKey(t *testing.T) *cryptox.Key {
	t.Helper()
	key, err := cryptox.GenerateKey()
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}
	return key
}

// The four layers stack in one order, and the order is the whole design.
//
// The environment is last on purpose: it is the way back in when a stored
// setting has locked somebody out of the page that would fix it, and it is what
// keeps every containerised deployment working across the change that
// introduced the database. The cost is that a value an administrator typed can
// be invisibly overridden, which is why the layer each value came from is
// recorded rather than left to be worked out.
func TestTheEnvironmentBeatsTheDatabaseAndTheDatabaseBeatsTheFile(t *testing.T) {
	isolateEnvironment(t)
	key := testKey(t)
	cfg := Default()
	cfg.Scheduler.Interval = 11
	cfg.note("scheduler.interval", SourceFile)
	cfg.Server.Bind = "10.0.0.1:8080"
	cfg.note("server.bind", SourceFile)

	t.Setenv("ZOOMIES_BIND", "127.0.0.1:7000")
	rows := []store.InstanceSetting{
		{Key: "scheduler.interval", Value: "22s"},
		{Key: "server.bind", Value: "0.0.0.0:9090"},
	}
	if _, err := cfg.Rebuild(rows, key); err != nil {
		t.Fatalf("Rebuild: %v", err)
	}

	if got := cfg.Scheduler.Interval.String(); got != "22s" {
		t.Errorf("the database should beat the file: scheduler.interval = %s", got)
	}
	if cfg.Source("scheduler.interval") != SourceDatabase {
		t.Errorf("scheduler.interval came from %s", cfg.Source("scheduler.interval"))
	}
	if cfg.Server.Bind != "127.0.0.1:7000" {
		t.Errorf("the environment should beat the database: server.bind = %s", cfg.Server.Bind)
	}
	if cfg.Source("server.bind") != SourceEnvironment {
		t.Errorf("server.bind came from %s", cfg.Source("server.bind"))
	}

	pinned := cfg.PinnedByEnvironment()
	if len(pinned) != 1 || pinned[0].Key != "server.bind" {
		t.Errorf("PinnedByEnvironment = %v, want just server.bind", pinned)
	}
}

// A stored row this build cannot use does not stop the controller.
//
// A downgrade is the ordinary way to meet one -- somebody set a value in a
// newer version and rolled back -- and refusing to boot over a setting the
// older binary never had turns "that feature is not available" into "the
// controller is down". The row is named in a finding instead, and the layer
// underneath it decides.
func TestAStoredValueThisBuildCannotUseLeavesTheLayerBeneathIt(t *testing.T) {
	key := testKey(t)
	cfg := Default()
	want := cfg.Retention.Jobs

	findings := ApplyStored(cfg, []store.InstanceSetting{
		{Key: "retention.jobs", Value: "forever"},
		{Key: "a.setting.from.the.future", Value: "1"},
	}, key)

	if cfg.Retention.Jobs != want {
		t.Errorf("retention.jobs = %s, want the default %s left in place", cfg.Retention.Jobs, want)
	}
	codes := map[string]bool{}
	for _, f := range findings {
		codes[f.Code] = true
	}
	if !codes["settings.stored_invalid"] || !codes["settings.stored_unknown"] {
		t.Errorf("findings = %v, want both an invalid and an unknown row reported", codes)
	}
	// Neither stops startup: an unusable row is a thing to fix, not a reason to
	// take the fleet down.
	if err := findings.Err(); err != nil {
		t.Errorf("a bad stored row refused to start: %v", err)
	}
}

// A credential is sealed on the way in and opened on the way out, so a copy of
// the database taken without the key holds nothing usable.
func TestACredentialIsSealedInTheDatabase(t *testing.T) {
	key := testKey(t)
	s, ok := LookupSetting("capacity_demand.signing_secret")
	if !ok {
		t.Fatal("no capacity_demand.signing_secret")
	}

	row, err := EncodeStored(s, "hunter2", key)
	if err != nil {
		t.Fatalf("EncodeStored: %v", err)
	}
	if !row.Secret {
		t.Error("the row is not marked as holding a secret")
	}
	if strings.Contains(row.Value, "hunter2") {
		t.Errorf("the secret is in the row in the clear: %q", row.Value)
	}

	cfg := Default()
	if f := ApplyStored(cfg, []store.InstanceSetting{row}, key); len(f) > 0 {
		t.Fatalf("opening a sealed row produced findings: %v", f)
	}
	if cfg.CapacityDemand.SigningSecret != "hunter2" {
		t.Errorf("the secret did not come back: %q", cfg.CapacityDemand.SigningSecret)
	}

	// And a different key does not quietly produce an instance running without
	// the credential it thinks it has.
	other := testKey(t)
	findings := ApplyStored(Default(), []store.InstanceSetting{row}, other)
	if findings.Err() == nil {
		t.Error("a credential sealed with another key did not stop startup")
	}
}

// A setting the running process cannot apply is reported as waiting rather than
// pretended about. A setting the environment is pinning never waits, because
// its stored value is not coming into force at the next start either.
func TestOnlyASettingThatWillActuallyChangeIsPending(t *testing.T) {
	isolateEnvironment(t)
	key := testKey(t)
	running := Default()
	t.Setenv("ZOOMIES_AGENT_CAPACITY", "4")
	if _, err := running.Rebuild(nil, key); err != nil {
		t.Fatalf("Rebuild: %v", err)
	}

	rows := []store.InstanceSetting{
		{Key: "server.bind", Value: "0.0.0.0:9090"}, // waits for a restart
		{Key: "agent.capacity", Value: "8"},         // pinned by the environment
		{Key: "scheduler.interval", Value: "30s"},   // live, so never pending
	}
	pending := PendingRestart(running, rows, key)
	if len(pending) != 1 || pending[0] != "server.bind" {
		t.Errorf("PendingRestart = %v, want just server.bind", pending)
	}
}

// The importer takes what the file spells and nothing else.
//
// A key the file never mentioned is left alone because the defaults are worked
// out on the host that reads them -- the database path from its state
// directory, the agent's capacity from its cores -- so a row claiming to be
// "the default" would freeze one machine's answers for every machine after it.
func TestTheImporterTakesOnlyWhatTheFileSpells(t *testing.T) {
	isolateEnvironment(t)
	dir := t.TempDir()
	path := dir + "/zoomies.yaml"
	if err := writeFileForTest(path, `
server:
  external_url: https://ci.example.com
scheduler:
  interval: 25s
agent:
  labels:
    tier: build
`); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	rows, err := SeedFromFile(cfg, testKey(t))
	if err != nil {
		t.Fatalf("SeedFromFile: %v", err)
	}

	got := map[string]string{}
	for _, row := range rows {
		got[row.Key] = row.Value
	}
	want := map[string]string{
		"server.external_url": "https://ci.example.com",
		"scheduler.interval":  "25s",
		"agent.labels":        "tier=build",
	}
	if len(got) != len(want) {
		t.Errorf("imported %d settings, want %d: %v", len(got), len(want), got)
	}
	for key, value := range want {
		if got[key] != value {
			t.Errorf("%s imported as %q, want %q", key, got[key], value)
		}
	}
	// The bootstrap keys stay in the file, where they have to be.
	if _, taken := got["database.path"]; taken {
		t.Error("database.path was imported into the database it names")
	}
}

// The file a fresh install writes says only what that host needs before it can
// read the rest. It used to marshal all eighty-nine settings, which -- now that
// a key spelled in the file is a key the file is in charge of -- would hand the
// whole configuration back to a text editor on the day it was installed.
func TestTheWrittenFileSaysOnlyWhatItHasTo(t *testing.T) {
	path := t.TempDir() + "/zoomies.yaml"
	cfg := Default()
	cfg.Server.ExternalURL = "https://ci.example.com"
	if err := cfg.Save(path); err != nil {
		t.Fatalf("Save: %v", err)
	}

	doc, err := readFile(path)
	if err != nil {
		t.Fatal(err)
	}
	keys := keysIn(doc)
	for _, want := range []string{"database.path", "security.encryption_key_file", "server.external_url"} {
		if !contains(keys, want) {
			t.Errorf("the written file does not spell %s: %v", want, keys)
		}
	}
	for _, unwanted := range []string{"scheduler.interval", "retention.jobs", "agent.capacity", "log.level"} {
		if contains(keys, unwanted) {
			t.Errorf("the written file pins %s, which nobody chose", unwanted)
		}
	}

	// And it still parses, which is the only hard constraint on its shape.
	back, err := Load(path)
	if err != nil {
		t.Fatalf("the written file does not load: %v", err)
	}
	if back.Server.ExternalURL != "https://ci.example.com" {
		t.Errorf("the round trip lost the external URL: %q", back.Server.ExternalURL)
	}
}

// agent.work_dir's default is derived from the euid of whatever process asks
// for it: `zoomies agent join` always runs as root, and root's default happens
// to be the same path the join chose here. That must not be reason enough to
// drop it from the file -- the service the join installs almost always runs
// as an unprivileged user, whose own default for the same key is a different
// path, and a value missing from the file is recomputed from that default
// rather than left alone.
func TestAgentWorkDirIsWrittenEvenWhenItMatchesTheFreshDefault(t *testing.T) {
	path := t.TempDir() + "/zoomies.yaml"
	cfg := Default()
	cfg.Agent.Embedded = false
	cfg.Agent.WorkDir = Default().Agent.WorkDir // exactly what sparse() compares against
	if err := cfg.Save(path); err != nil {
		t.Fatalf("Save: %v", err)
	}

	doc, err := readFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !contains(keysIn(doc), "agent.work_dir") {
		t.Fatalf("the written file does not spell agent.work_dir, though nothing else names it: %v", keysIn(doc))
	}

	// A load that computes a different default for the key -- standing in for
	// the unprivileged service user -- must still recover the pinned value.
	back, err := Load(path)
	if err != nil {
		t.Fatalf("the written file does not load: %v", err)
	}
	if back.Agent.WorkDir != cfg.Agent.WorkDir {
		t.Errorf("agent.work_dir round-tripped as %q, want %q", back.Agent.WorkDir, cfg.Agent.WorkDir)
	}
}

func contains(list []string, want string) bool {
	for _, s := range list {
		if s == want {
			return true
		}
	}
	return false
}

// writeFileForTest writes a configuration file, so a test can say what an
// operator's file holds rather than build one field by field.
func writeFileForTest(path, body string) error {
	return os.WriteFile(path, []byte(strings.TrimLeft(body, "\n")), 0o600)
}

// The old name for the scaling-history window still works, and no longer eats a
// value somebody typed on the settings page.
//
// normalize runs last, after the database layer, so the legacy carry-over used
// to overwrite a stored value unconditionally: the administrator got a 200, an
// audit row and a stored setting, and the controller quietly ran the number in
// a years-old file. A key in the file still beats the built-in default, which
// is what honouring it at all means.
func TestTheLegacyRetentionKeyDoesNotEatAStoredValue(t *testing.T) {
	isolateEnvironment(t)
	key := testKey(t)

	fromFileOnly := Default()
	fromFileOnly.Retention.Audit = 48 * time.Hour
	if _, err := fromFileOnly.Rebuild(nil, key); err != nil {
		t.Fatalf("Rebuild: %v", err)
	}
	if fromFileOnly.Retention.ScalingEvents != 48*time.Hour {
		t.Errorf("the legacy key stopped working: %s", fromFileOnly.Retention.ScalingEvents)
	}

	stored := Default()
	stored.Retention.Audit = 48 * time.Hour
	if _, err := stored.Rebuild([]store.InstanceSetting{
		{Key: "retention.scaling_events", Value: "72h"},
	}, key); err != nil {
		t.Fatalf("Rebuild: %v", err)
	}
	if stored.Retention.ScalingEvents != 72*time.Hour {
		t.Errorf("a stored value was eaten by the legacy key: %s", stored.Retention.ScalingEvents)
	}
	// And the finding asking for the rename is still raised, because the file's
	// key is still there and still wants removing.
	var asked bool
	for _, f := range stored.Validate() {
		if f.Code == "retention.audit_renamed" {
			asked = true
		}
	}
	if !asked {
		t.Error("nothing asked the operator to remove the old key")
	}
}
