package config

import (
	"strings"
	"testing"
)

// A container deployment's .env is where its settings used to live. Each one
// becomes the row the controller would read the same value back from, and
// what cannot live in the database is handed back to stay where it is.
func TestImportEnvironmentTurnsEachSettingIntoItsRow(t *testing.T) {
	key := testKey(t)
	vars := map[string]string{
		"ZOOMIES_BIND":               "0.0.0.0:8080",
		"ZOOMIES_TRUSTED_PROXIES":    "10.0.0.0/8,172.16.0.0/12",
		"ZOOMIES_AGENT_EMBEDDED":     "true",
		"ZOOMIES_OIDC_CLIENT_SECRET": "hunter2",
		"ZOOMIES_ENCRYPTION_KEY":     "not-a-stored-setting",
		"ZOOMIES_DB_PATH":            "/var/lib/zoomies/zoomies.db",
		"ZOOMIES_IMAGE":              "ghcr.io/eyupio/zoomies:v1",
		"DOCKER_GID":                 "998",
	}
	rows, imported, left, err := ImportEnvironment(vars, key)
	if err != nil {
		t.Fatalf("ImportEnvironment: %v", err)
	}
	if len(rows) != 4 || len(imported) != 4 {
		t.Fatalf("rows = %+v, want four", rows)
	}
	if got := strings.Join(left, ","); got != "ZOOMIES_DB_PATH,ZOOMIES_ENCRYPTION_KEY,ZOOMIES_IMAGE" {
		t.Errorf("left = %s, want the bootstrap and Compose variables and nothing else", got)
	}

	cfg := Default()
	if findings := ApplyStored(cfg, rows, key); len(findings) != 0 {
		t.Fatalf("the rows do not read back: %v", findings)
	}
	if cfg.Server.Bind != "0.0.0.0:8080" || !cfg.Agent.Embedded || len(cfg.Server.TrustedProxies) != 2 || cfg.OIDC.ClientSecret != "hunter2" {
		t.Errorf("read back: bind %q, embedded %v, proxies %v, secret %q", cfg.Server.Bind, cfg.Agent.Embedded, cfg.Server.TrustedProxies, cfg.OIDC.ClientSecret)
	}
	for _, r := range rows {
		if r.Key == "oidc.client_secret" && (!r.Secret || strings.Contains(r.Value, "hunter2")) {
			t.Errorf("the credential was stored in the clear: %+v", r)
		}
	}
	for _, im := range imported {
		if im.Setting.Secret && im.Text != "" {
			t.Errorf("a credential's value is returned for display: %+v", im)
		}
	}
}

// Half a move is the worst outcome: the half that failed stays pinning the
// database and nobody notices. So one bad value stores nothing, and says which.
func TestImportEnvironmentStoresNothingWhenAValueWillNotDo(t *testing.T) {
	rows, _, _, err := ImportEnvironment(map[string]string{
		"ZOOMIES_BIND":           "0.0.0.0:8080",
		"ZOOMIES_AGENT_CAPACITY": "lots",
	}, nil)
	if err == nil || rows != nil || !strings.Contains(err.Error(), "ZOOMIES_AGENT_CAPACITY") {
		t.Fatalf("rows = %v, err = %v; want nothing and the variable named", rows, err)
	}

	// A credential with no key to seal it is the same refusal.
	if _, _, _, err := ImportEnvironment(map[string]string{"ZOOMIES_OIDC_CLIENT_SECRET": "x"}, nil); err == nil ||
		!strings.Contains(err.Error(), "ZOOMIES_OIDC_CLIENT_SECRET") {
		t.Errorf("err = %v, want a refusal naming the credential", err)
	}
}

func TestSettingForEnvKnowsOnlyStoredSettings(t *testing.T) {
	if s, ok := SettingForEnv("ZOOMIES_EXTERNAL_URL"); !ok || s.Key != "server.external_url" {
		t.Errorf("ZOOMIES_EXTERNAL_URL = %+v, %v", s, ok)
	}
	for _, name := range []string{"ZOOMIES_ENCRYPTION_KEY", "ZOOMIES_DB_PATH", "ZOOMIES_JOIN_TOKEN", "ZOOMIES_IMAGE", "PATH"} {
		if _, ok := SettingForEnv(name); ok {
			t.Errorf("%s is not a setting the database holds", name)
		}
	}
}
