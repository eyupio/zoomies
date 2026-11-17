package config

import (
	"os"
	"path/filepath"
	"testing"
)

// The registry mirror opens a listener on the host and changes a sidecar's
// command, so it is off until an operator turns it on, as the cache service is.
// It has no size of its own either: zero is how an operator says "the default",
// and the backend, not this file, knows what that is.
func TestTheRegistryMirrorIsOffByDefaultAndHasNoSizeOfItsOwn(t *testing.T) {
	c := Default()
	if c.Agent.RegistryMirror.Enabled {
		t.Fatal("agent.registry_mirror.enabled defaults to on; it opens a listener and must be asked for")
	}
	if c.Agent.RegistryMirror.MaxBytes != 0 {
		t.Fatalf("agent.registry_mirror.max_bytes defaults to %d, want 0 (the backend's default)", c.Agent.RegistryMirror.MaxBytes)
	}
}

func TestTheRegistryMirrorSettingsAreReadFromTheFileAndTheEnvironmentWins(t *testing.T) {
	unsetenv(t, "ZOOMIES_AGENT_REGISTRY_MIRROR_ENABLED")
	unsetenv(t, "ZOOMIES_AGENT_REGISTRY_MIRROR_MAX_BYTES")
	path := filepath.Join(t.TempDir(), "zoomies.yaml")
	if err := os.WriteFile(path, []byte("agent:\n  registry_mirror:\n    enabled: true\n    max_bytes: 2147483648\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.Agent.RegistryMirror.Enabled || cfg.Agent.RegistryMirror.MaxBytes != 2<<30 {
		t.Fatalf("file not read: %+v", cfg.Agent.RegistryMirror)
	}

	t.Setenv("ZOOMIES_AGENT_REGISTRY_MIRROR_ENABLED", "false")
	t.Setenv("ZOOMIES_AGENT_REGISTRY_MIRROR_MAX_BYTES", "5368709120")
	cfg, err = Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Agent.RegistryMirror.Enabled || cfg.Agent.RegistryMirror.MaxBytes != 5<<30 {
		t.Fatalf("environment did not win: %+v", cfg.Agent.RegistryMirror)
	}
}

// A mirror too small to hold one image layer would evict as fast as it fetched,
// so a size is either left to the default or is at least a gibibyte. A bad value
// is refused whether or not the mirror is on, so it is caught when it is saved
// and not on the day somebody turns the mirror on.
func TestTheRegistryMirrorsSizeIsZeroOrAtLeastOneGiB(t *testing.T) {
	const gib = int64(1) << 30
	for _, tc := range []struct {
		name    string
		bytes   int64
		enabled bool
		refused bool
	}{
		{"zero is the default", 0, true, false},
		{"exactly one gibibyte", gib, true, false},
		{"ten gibibytes", 10 * gib, true, false},
		{"one byte under a gibibyte", gib - 1, true, true},
		{"one byte", 1, true, true},
		{"negative", -1, true, true},
		{"negative with the mirror off", -1, false, true},
		{"too small with the mirror off", 1, false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := Default()
			c.Agent.RegistryMirror.Enabled = tc.enabled
			c.Agent.RegistryMirror.MaxBytes = tc.bytes
			findings := c.Validate()
			if got := hasCode(findings, "agent.registry_mirror.max_bytes"); got != tc.refused {
				t.Fatalf("max_bytes %d (enabled %v): refused = %v, want %v", tc.bytes, tc.enabled, got, tc.refused)
			}
			for _, f := range findings {
				if f.Code == "agent.registry_mirror.max_bytes" && (f.Severity != SeverityError || f.Setting != "agent.registry_mirror.max_bytes" || f.Fix == "") {
					t.Fatalf("finding = %+v, want an error on the setting that says what to change", f)
				}
			}
		})
	}
}
