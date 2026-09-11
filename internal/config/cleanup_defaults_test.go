package config

import (
	"testing"
	"time"
)

// Finished runner containers are disposable execution state. The agent still
// waits for the controller to acknowledge their terminal report, so zero means
// removal on the next reconcile pass rather than removal before the result is
// safe. A non-zero default makes disk consumption grow with job throughput.
func TestFinishedRunnerWorkloadsAreNotRetainedByDefault(t *testing.T) {
	if got := Default().Agent.FinishedRetention; got != 0*time.Second {
		t.Fatalf("FinishedRetention = %s, want 0 so each completed job releases its container disk", got)
	}
}
