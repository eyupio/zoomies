package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/eyupio/zoomies/internal/store"
)

// The installer upgrading a deployment has to know what that deployment runs,
// and the settings page writes to its database -- so an answer read from the
// file or the .env alone is wrong the moment an operator has used the page.
// The layers stack as the controller stacks them, and the environment
// consulted is the deployment's, never the process asking.
func TestEffectiveLayersTheDatabaseOverTheFileAndTheDeploymentsEnvironmentOverBoth(t *testing.T) {
	file := filepath.Join(t.TempDir(), "zoomies.yaml")
	if err := os.WriteFile(file, []byte("agent:\n  embedded: true\n  capacity: 3\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	rows := []store.InstanceSetting{{Key: "agent.embedded", Value: "false"}, {Key: "agent.capacity", Value: "6"}}

	cfg, _, err := Effective(file, rows, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Agent.Embedded || cfg.Agent.Capacity != 6 {
		t.Errorf("embedded=%v capacity=%d, want the database's false and 6 over the file", cfg.Agent.Embedded, cfg.Agent.Capacity)
	}

	cfg, _, err = Effective(file, rows, nil, map[string]string{"ZOOMIES_AGENT_CAPACITY": "8"})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Agent.Capacity != 8 || cfg.Agent.Embedded {
		t.Errorf("capacity=%d embedded=%v, want the deployment's environment to win for what it sets and nothing else", cfg.Agent.Capacity, cfg.Agent.Embedded)
	}

	// The upgrade's own environment is not the service's.
	t.Setenv("ZOOMIES_AGENT_EMBEDDED", "true")
	cfg, _, err = Effective("", rows, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Agent.Embedded {
		t.Error("Effective read the calling process's environment as the deployment's")
	}
}
