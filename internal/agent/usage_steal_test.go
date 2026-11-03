package agent

import (
	"testing"

	"github.com/eyupio/zoomies/internal/backend"
	"github.com/eyupio/zoomies/internal/machine"
	"github.com/eyupio/zoomies/internal/store"
)

// The agent is the one place that reads the machine, so the figure has to leave
// it: a steal the sampler measured and the heartbeat dropped would be a gap on
// every host, however well everything after it worked.
func TestTheHeartbeatsUsageCarriesWhatTheHypervisorKept(t *testing.T) {
	a, _, _, _ := newAgent(t, 2)
	cpu, steal := 68.0, 36.0
	a.opts.SampleUsage = func(int, int64) machine.Usage { return machine.Usage{CPUPercent: &cpu, StealPercent: &steal} }
	local := []backend.Info{{Kind: store.BackendDocker, Available: true, Endpoint: "unix:///var/run/docker.sock"}}

	got := a.hostUsage(local, 2, 2048)
	if got == nil || got.StealPercent == nil || *got.StealPercent != steal {
		t.Fatalf("usage = %+v, want the steal the sampler measured", got)
	}

	// A machine that did not measure it sends none, which is not a zero.
	a.opts.SampleUsage = func(int, int64) machine.Usage { return machine.Usage{CPUPercent: &cpu} }
	if got := a.hostUsage(local, 2, 2048); got == nil || got.StealPercent != nil {
		t.Fatalf("usage = %+v, want no steal where none was measured", got)
	}
}
