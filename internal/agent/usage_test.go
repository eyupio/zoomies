package agent

import (
	"testing"

	"github.com/eyupio/zoomies/internal/backend"
	"github.com/eyupio/zoomies/internal/machine"
	"github.com/eyupio/zoomies/internal/store"
)

func TestRemoteDaemonCannotBorrowLocalHostUsage(t *testing.T) {
	a, _, _, _ := newAgent(t, 2)
	m := machine.Detect()
	for _, endpoint := range []string{"tcp://remote:2375", "https://remote:2376", ""} {
		if got := a.hostUsage([]backend.Info{{Kind: store.BackendDocker, Available: true, Endpoint: endpoint}}, m.CPUs, m.MemoryMB); got != nil {
			t.Fatalf("local utilisation attributed to %q: %+v", endpoint, got)
		}
	}
}

func TestHeartbeatCarriesWholeHostUsageEvenWhenTheAgentHasASmallerCgroup(t *testing.T) {
	a, tr, _, _ := newAgent(t, 2)
	a.opts.Machine = &machine.Facts{CPUs: 2, MemoryMB: 2048}
	a.backendInfo = []backend.Info{{Kind: store.BackendDocker, Available: true,
		Endpoint: "unix:///var/run/docker.sock", CPUs: 16, MemoryMB: 32768}}
	a.probedAt = a.now()
	cpu, memory, load := 72.0, int64(12000), 21.5
	a.opts.SampleUsage = func(cpus int, memoryMB int64) machine.Usage {
		if cpus != 16 || memoryMB != 32768 {
			t.Fatalf("sample used agent cgroup size %d/%d", cpus, memoryMB)
		}
		return machine.Usage{CPUPercent: &cpu, MemoryAvailableMB: &memory, LoadAverage1: &load}
	}
	if err := a.heartbeat(t.Context()); err != nil {
		t.Fatal(err)
	}
	beat := <-tr.beats
	if beat.Usage == nil || beat.Usage.CPUPercent == nil || *beat.Usage.CPUPercent != cpu ||
		beat.Usage.MemoryAvailableMB == nil || *beat.Usage.MemoryAvailableMB != memory || beat.CPUs != 16 ||
		beat.Usage.LoadAverage1 == nil || *beat.Usage.LoadAverage1 != load {
		t.Fatalf("measurement did not reach heartbeat: %+v", beat)
	}
}
