package config

import "testing"

// Finished runner containers are disposable execution state. The agent still
// waits for the controller to acknowledge their terminal report, so zero means
// removal on the next reconcile pass rather than removal before the result is
// safe. A non-zero default makes disk consumption grow with job throughput.
func TestFinishedRunnerWorkloadsAreNotRetainedByDefault(t *testing.T) {
	if got := Default().Agent.FinishedRetention; got != 0 {
		t.Fatalf("FinishedRetention = %s, want 0 so each completed job releases its container disk", got)
	}
}

func TestDockerBuildCacheBudgetDefaultsAndValidation(t *testing.T) {
	c := Default()
	if c.Agent.DockerBuildCacheMB != 5120 {
		t.Fatalf("cache target = %d", c.Agent.DockerBuildCacheMB)
	}
	for _, value := range []int{-1, 1048577} {
		c.Agent.DockerBuildCacheMB = value
		if !hasCode(c.Validate(), "agent.docker_build_cache_mb") {
			t.Fatalf("invalid cache budget %d accepted", value)
		}
	}
	c.Agent.DockerBuildCacheMB = 0
	if hasCode(c.Validate(), "agent.docker_build_cache_mb") {
		t.Fatal("disabled cleanup rejected")
	}
}
