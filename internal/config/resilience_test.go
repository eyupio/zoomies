package config

import (
	"testing"
	"time"
)

func TestResilienceSettingsDefaultsAndBounds(t *testing.T) {
	c := Default()
	if c.Scheduler.RegistrationConcurrency != 1 || c.Agent.PrewarmTimeout != 5*time.Minute || c.Agent.PrewarmJitter != 30*time.Second {
		t.Fatal("unexpected resilience defaults")
	}
	for _, tc := range []struct {
		key    string
		change func(*Config)
	}{
		{"scheduler.registration_concurrency", func(c *Config) { c.Scheduler.RegistrationConcurrency = 0 }},
		{"scheduler.registration_concurrency", func(c *Config) { c.Scheduler.RegistrationConcurrency = 17 }},
		{"agent.prewarm_timeout", func(c *Config) { c.Agent.PrewarmTimeout = 0 }},
		{"agent.prewarm_timeout", func(c *Config) { c.Agent.PrewarmTimeout = 16 * time.Minute }},
		{"agent.prewarm_jitter", func(c *Config) { c.Agent.PrewarmJitter = -time.Second }},
		{"agent.prewarm_jitter", func(c *Config) { c.Agent.PrewarmJitter = 6 * time.Minute }},
	} {
		c := Default()
		tc.change(c)
		found := false
		for _, f := range c.Validate() {
			if f.Code == tc.key && f.Severity == SeverityError {
				found = true
			}
		}
		if !found {
			t.Fatalf("missing finding %s", tc.key)
		}
	}
}

func TestResilienceSettingsEnvironmentOverrides(t *testing.T) {
	t.Setenv("ZOOMIES_REGISTRATION_CONCURRENCY", "4")
	t.Setenv("ZOOMIES_AGENT_PREWARM_TIMEOUT", "8m")
	t.Setenv("ZOOMIES_AGENT_PREWARM_JITTER", "0s")
	c := Default()
	if err := c.applyEnv(); err != nil {
		t.Fatal(err)
	}
	if c.Scheduler.RegistrationConcurrency != 4 || c.Agent.PrewarmTimeout != 8*time.Minute || c.Agent.PrewarmJitter != 0 {
		t.Fatal("environment overrides did not reach resilience settings")
	}
}
