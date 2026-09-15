package config

import (
	"testing"
	"time"
)

// The runner image refuses a wait outside one second to one hour with a
// configuration exit, and a refused wait is a Docker pool that starts no
// runner at all. The validator has to say so before the fleet finds out.
func TestDockerWaitIsBoundedByWhatTheImageAccepts(t *testing.T) {
	c := Default()
	if c.Runners.DockerWait != 2*time.Minute {
		t.Fatalf("default docker wait = %s, want the image's own two minutes", c.Runners.DockerWait)
	}
	for _, value := range []time.Duration{-time.Second, time.Hour + time.Second, 48 * time.Hour} {
		c.Runners.DockerWait = value
		if !hasCode(c.Validate(), "runners.docker_wait") {
			t.Fatalf("a wait of %s the image would refuse was accepted", value)
		}
	}
	for _, value := range []time.Duration{0, time.Second, 2 * time.Minute, time.Hour} {
		c.Runners.DockerWait = value
		if hasCode(c.Validate(), "runners.docker_wait") {
			t.Fatalf("a wait of %s the image accepts was refused", value)
		}
	}
}

// One value for the whole fleet is wrong for every runner in it when the
// variable is the runner's own name or credentials. Case does not matter:
// the image reads the upper-case name, and a lower-case spelling would not
// be a harmless second variable, it would be a typo that hid the refusal.
func TestRunnerEnvMayNotNameTheRunnerContract(t *testing.T) {
	c := Default()
	c.Runners.Env = map[string]string{"HTTPS_PROXY": "http://proxy:3128", "zoomies_runner_name": "one-name-for-all"}
	fs := c.Validate()
	if !hasCode(fs, "runners.env_reserved") {
		t.Fatal("a fleet-wide runner name was accepted")
	}
	c.Runners.Env = map[string]string{"HTTPS_PROXY": "http://proxy:3128", "GOFLAGS": "-mod=mod"}
	if hasCode(c.Validate(), "runners.env_reserved") {
		t.Fatal("ordinary variables were refused")
	}
}

// The two settings are the page's, not the file's: they are stored, editable,
// and in force without a restart, because the controller reads them when it
// builds each create task.
func TestRunnerSettingsAreStoredAndLive(t *testing.T) {
	for _, key := range []string{"runners.docker_wait", "runners.env"} {
		s, ok := LookupSetting(key)
		if !ok {
			t.Fatalf("%s is not registered", key)
		}
		if !s.Stored() || !s.Live || s.Secret {
			t.Fatalf("%s = %+v, want stored, live and not secret", key, s)
		}
	}
	c := Default()
	if _, err := c.SetValueString("runners.env", "HTTPS_PROXY=http://proxy:3128,NO_PROXY=localhost"); err != nil {
		t.Fatalf("SetValueString: %v", err)
	}
	if c.Runners.Env["NO_PROXY"] != "localhost" {
		t.Fatalf("runners.env = %v", c.Runners.Env)
	}
}
