package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDiskThresholdDefaultsAndOverrides(t *testing.T) {
	d := Default().Agent.Disk
	if d.WarnFreePercent != 10 || d.WarnFreeGiB != 10 || d.StopFreeGiB != 0 {
		t.Fatal(d)
	}
	p := filepath.Join(t.TempDir(), "zoomies.yaml")
	if err := os.WriteFile(p, []byte("agent:\n  disk:\n    warn_free_percent: 0\n    warn_free_gib: 20\n    stop_free_gib: 150\n"), 0600); err != nil {
		t.Fatal(err)
	}
	c, err := Load(p)
	if err != nil {
		t.Fatal(err)
	}
	if d = c.Agent.Disk; d.WarnFreePercent != 0 || d.WarnFreeGiB != 20 || d.StopFreeGiB != 150 {
		t.Fatal(d)
	}
	for _, invalid := range []AgentDisk{{WarnFreePercent: 101}, {WarnFreeGiB: -1}, {StopFreeGiB: -1}} {
		c.Agent.Disk = invalid
		found := false
		for _, f := range c.Validate() {
			if f.Code == "agent.disk" {
				found = true
			}
		}
		if !found {
			t.Fatalf("accepted %+v", invalid)
		}
	}
}
