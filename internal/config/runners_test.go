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

// ZOOMIES_DOCKER_WAIT named the runner image's own whole-seconds wait long
// before runners.docker_wait existed, and an operator's or an embedded
// controller's environment carrying that exact spelling is not a typo to
// refuse: it is read as seconds, on this setting and no other.
func TestDockerWaitEnvAcceptsWholeSecondsForBackwardsCompatibility(t *testing.T) {
	c := Default()
	if _, err := c.SetValueString("runners.docker_wait", "120"); err != nil {
		t.Fatalf("SetValueString(\"120\"): %v", err)
	}
	if c.Runners.DockerWait != 120*time.Second {
		t.Fatalf("runners.docker_wait = %s, want 2m from a bare \"120\"", c.Runners.DockerWait)
	}
	// A duration string still wins in its own right.
	if _, err := c.SetValueString("runners.docker_wait", "3m"); err != nil {
		t.Fatalf("SetValueString(\"3m\"): %v", err)
	}
	if c.Runners.DockerWait != 3*time.Minute {
		t.Fatalf("runners.docker_wait = %s, want 3m", c.Runners.DockerWait)
	}
	// No other duration setting gains this leniency: a bare number elsewhere
	// is still refused, the way an operator setting it is told to expect.
	if _, err := c.SetValueString("scheduler.provision_timeout", "120"); err == nil {
		t.Fatal("scheduler.provision_timeout accepted a bare number")
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

// The size a pool that says nothing gets, and the fleet's right to change it.
//
// It is a setting rather than a constant because the right answer is the shape
// of the machines and the jobs: a fleet of small boxes, or one of compilers,
// says so once here rather than on every pool it creates.
func TestTheDefaultRunnerSizeIsTwoCoresAndFourGigabytes(t *testing.T) {
	c := Default()
	cpus, memoryMB := c.Runners.DefaultRunnerSize()
	if cpus != DefaultRunnerCPUs || memoryMB != DefaultRunnerMemoryMB {
		t.Fatalf("default runner size = %v CPU, %d MB, want %v and %d",
			cpus, memoryMB, DefaultRunnerCPUs, DefaultRunnerMemoryMB)
	}
}

// Zero is not "no limit" here, because no limit is the state this setting
// exists to make unreachable: a runner without one takes the whole machine
// while the fleet charges it one slot's share. Nothing having been said falls
// back to the built-in figures.
func TestAnUnsetDefaultRunnerSizeFallsBackRatherThanMeaningUnlimited(t *testing.T) {
	c := Default()
	c.Runners.DefaultCPUs, c.Runners.DefaultMemoryMB = 0, 0
	cpus, memoryMB := c.Runners.DefaultRunnerSize()
	if cpus != DefaultRunnerCPUs || memoryMB != DefaultRunnerMemoryMB {
		t.Fatalf("an unset default gave %v CPU and %d MB, want the built-in %v and %d",
			cpus, memoryMB, DefaultRunnerCPUs, DefaultRunnerMemoryMB)
	}
}

// A share of a CPU is the one figure in this configuration that is genuinely
// fractional, so the environment has to carry it as one -- rounding it to
// whole cores would take half a core from every operator running small jobs on
// a small machine.
func TestTheDefaultRunnerSizeIsSettableFromTheEnvironment(t *testing.T) {
	t.Setenv("ZOOMIES_RUNNER_DEFAULT_CPUS", "1.5")
	t.Setenv("ZOOMIES_RUNNER_DEFAULT_MEMORY_MB", "3072")

	c := Default()
	if err := c.applyEnv(); err != nil {
		t.Fatalf("applyEnv: %v", err)
	}
	cpus, memoryMB := c.Runners.DefaultRunnerSize()
	if cpus != 1.5 || memoryMB != 3072 {
		t.Fatalf("size from the environment = %v CPU, %d MB, want 1.5 and 3072", cpus, memoryMB)
	}
}
