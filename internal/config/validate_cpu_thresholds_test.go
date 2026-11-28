package config

import (
	"testing"
	"time"
)

func TestCPUThresholdsAreValidated(t *testing.T) {
	if got := findingsFor(Default(), "scheduler.cpu_release_percent"); len(got) != 0 {
		t.Fatalf("the defaults were refused: %v", got)
	}
	for _, tc := range []struct {
		name, code string
		set        func(c *Config)
	}{
		{"start line above 100", "scheduler.cpu_hold_percent", func(c *Config) { c.Scheduler.CPUHoldPercent = 101 }},
		{"paced line under 1", "scheduler.cpu_paced_percent", func(c *Config) { c.Scheduler.CPUPacedPercent = 0.5 }},
		{"elastic line above 100", "scheduler.elastic_cpu_busy_percent", func(c *Config) { c.Scheduler.ElasticCPUBusyPercent = 150 }},
		{"window past the ladder's step", "scheduler.cpu_hold_window", func(c *Config) { c.Scheduler.CPUHoldWindow = 3 * time.Minute }},
		{"release not below the start line", "scheduler.cpu_release_percent", func(c *Config) {
			c.Scheduler.CPUHoldPercent, c.Scheduler.CPUReleasePercent = 80, 80
		}},
		{"release above a default start line", "scheduler.cpu_release_percent", func(c *Config) {
			c.Scheduler.CPUHoldPercent, c.Scheduler.CPUReleasePercent = 0, 96
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := Default()
			tc.set(c)
			if got := findingsFor(c, tc.code); len(got) != 1 {
				t.Fatalf("want one %s finding, got %v", tc.code, got)
			}
		})
	}
	c := Default()
	c.Scheduler.CPUHoldPercent, c.Scheduler.CPUHoldWindow, c.Scheduler.CPUReleasePercent = 80, 10*time.Second, 70
	for _, code := range []string{"scheduler.cpu_hold_percent", "scheduler.cpu_hold_window", "scheduler.cpu_release_percent"} {
		if got := findingsFor(c, code); len(got) != 0 {
			t.Fatalf("a lower hold was refused: %v", got)
		}
	}
}
