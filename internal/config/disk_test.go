package config

import (
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The warnings used to be fixed at 10% and 10 GiB, so a host that never sets
// them has to keep warning exactly there, and the reserve that stops work has
// to be off until an operator turns it on.
func TestDiskDefaultsKeepTheOldWarningsAndLeaveTheReserveOff(t *testing.T) {
	d := Default().Agent.Disk
	if d.WarnFreePercent != 10 || d.WarnFreeGiB != 10 || d.StopFreeGiB != 0 {
		t.Fatalf("defaults = %+v", d)
	}
}

// Zero is a real answer for a warning level (it turns that warning off), so a
// file that says zero must not be read as "unset" and replaced by the default.
func TestDiskThresholdsFromTheFileOverrideTheDefaultsIncludingZero(t *testing.T) {
	p := filepath.Join(t.TempDir(), "zoomies.yaml")
	body := "agent:\n  disk:\n    warn_free_percent: 0\n    warn_free_gib: 20\n    stop_free_gib: 150\n"
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	c, err := Load(p)
	if err != nil {
		t.Fatal(err)
	}
	if d := c.Agent.Disk; d.WarnFreePercent != 0 || d.WarnFreeGiB != 20 || d.StopFreeGiB != 150 {
		t.Fatalf("loaded %+v", d)
	}
}

// A threshold that cannot mean anything must stop startup and say which
// setting is wrong, because an operator reading "invalid disk threshold" has
// three settings to check.
func TestAnUnusableDiskThresholdIsRefusedAndNamesTheSetting(t *testing.T) {
	for _, tc := range []struct {
		name    string
		disk    AgentDisk
		setting string
	}{
		{"percentage above 100", AgentDisk{WarnFreePercent: 101}, "agent.disk.warn_free_percent"},
		{"negative warning", AgentDisk{WarnFreeGiB: -1}, "agent.disk.warn_free_gib"},
		{"negative reserve", AgentDisk{StopFreeGiB: -1}, "agent.disk.stop_free_gib"},
		{"not a number", AgentDisk{StopFreeGiB: math.NaN()}, "agent.disk.stop_free_gib"},
		{"infinite", AgentDisk{WarnFreeGiB: math.Inf(1)}, "agent.disk.warn_free_gib"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := Default()
			c.Agent.Disk = tc.disk
			var found *Finding
			for _, f := range c.Validate() {
				if f.Code == "agent.disk" {
					found = &f
				}
			}
			if found == nil {
				t.Fatalf("accepted %+v", tc.disk)
			}
			if found.Setting != tc.setting || !strings.Contains(found.Title, tc.setting) || found.Severity != SeverityError {
				t.Fatalf("finding does not name %s as an error: %+v", tc.setting, *found)
			}
		})
	}
}
