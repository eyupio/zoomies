package controller

import (
	"strings"
	"testing"
	"time"

	"github.com/eyupio/zoomies/internal/agent"
	"github.com/eyupio/zoomies/internal/backend"
	"github.com/eyupio/zoomies/internal/scheduler"
	"github.com/eyupio/zoomies/internal/store"
)

func TestHeartbeatUsageReachesPlacementViewsAndMetrics(t *testing.T) {
	h := newHarness(t)
	tr := h.c.EmbeddedTransport()
	joined, err := tr.Join(h.ctx, agent.JoinRequest{ProtocolVersion: agent.ProtocolVersion,
		Name: "shared-host", Capacity: 4, CPUs: 8, MemoryMB: 16384,
		Backends: []backend.Info{{Kind: store.BackendDocker, Available: true}}})
	if err != nil {
		t.Fatal(err)
	}
	tr.SetCredentials(joined.HostID, joined.AgentToken)
	cpu, memory := 99.0, int64(8192)
	beat := func() *store.Host {
		t.Helper()
		if _, err := tr.Heartbeat(h.ctx, agent.HeartbeatRequest{Usage: &store.HostUsage{CPUPercent: &cpu, MemoryAvailableMB: &memory}}); err != nil {
			t.Fatal(err)
		}
		host, err := h.st.GetHost(h.ctx, joined.HostID)
		if err != nil {
			t.Fatal(err)
		}
		return host
	}
	if host := beat(); !scheduler.HostAvailable(host, h.c.Now()) {
		t.Fatal("one spike held the host")
	}
	h.advance(31 * time.Second)
	host := beat()
	view := h.c.HostView(host)
	if !host.Healthy(h.c.Now()) || scheduler.HostAvailable(host, h.c.Now()) || !view.UsageFresh || !strings.Contains(view.AdmissionReason, "CPU") {
		t.Fatalf("connected and pressure-held were conflated: %+v", view)
	}
	for name, want := range map[string]float64{
		"zoomies_host_cpu_usage_percent":      99,
		"zoomies_host_memory_available_bytes": 8192 * (1 << 20),
		"zoomies_host_admission_held":         1,
		"zoomies_host_usage_fresh":            1,
	} {
		if got, ok := gatherValue(t, h.c, name, map[string]string{"host": host.ID}); !ok || got != want {
			t.Fatalf("%s = %v (%v), want %v", name, got, ok, want)
		}
	}
	// An older peer omits usage. It must not restamp the old reading as fresh.
	h.advance(store.HostUsageMaxAge)
	if _, err := tr.Heartbeat(h.ctx, agent.HeartbeatRequest{}); err != nil {
		t.Fatal(err)
	}
	host, err = h.st.GetHost(h.ctx, host.ID)
	if err != nil {
		t.Fatal(err)
	}
	if h.c.HostView(host).UsageFresh {
		t.Fatal("heartbeat freshened an omitted usage sample")
	}
	if _, ok := gatherValue(t, h.c, "zoomies_host_cpu_usage_percent", map[string]string{"host": host.ID}); ok {
		t.Fatal("stale CPU still exported as current")
	}
	if err := h.st.SetHostCordoned(h.ctx, host.ID, true); err != nil {
		t.Fatal(err)
	}
	cpu = 20
	host = beat()
	if host.Usage.CPUHeld || !host.Cordoned || scheduler.HostAvailable(host, h.c.Now()) {
		t.Fatal("CPU recovery undid the operator's cordon")
	}
}
