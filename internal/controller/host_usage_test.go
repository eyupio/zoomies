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

// The disk figures the pulse already carries have to be reachable from the two
// places an operator or an assistant asks: the host's API view and the
// Prometheus endpoint. Disk-bound is the judgement the scratch advice rests on,
// and I/O wait is the figure that separates it from a CPU-bound host.
func TestTheDiskPulseIsReadableFromTheViewAndMetrics(t *testing.T) {
	h := newHarness(t)
	_, _, host := h.fleet()
	wait := 31.0
	since := h.c.Now().Add(-11 * time.Minute)
	if err := h.st.SetHostUsage(h.ctx, host.ID, store.HostUsage{
		SampledAt:       h.c.Now(),
		IOWaitPercent:   &wait,
		IOWaitHighSince: &since,
	}); err != nil {
		t.Fatalf("SetHostUsage: %v", err)
	}
	host, err := h.st.GetHost(h.ctx, host.ID)
	if err != nil {
		t.Fatalf("GetHost: %v", err)
	}
	view := h.c.HostView(host)
	if !view.UsageFresh || !view.DiskBound {
		t.Fatalf("view usage_fresh=%v disk_bound=%v, want a fresh disk-bound host", view.UsageFresh, view.DiskBound)
	}
	if view.Usage == nil || view.Usage.IOWaitPercent == nil || *view.Usage.IOWaitPercent != wait {
		t.Fatalf("view usage = %+v, want I/O wait %v", view.Usage, wait)
	}
	for name, want := range map[string]float64{
		"zoomies_host_io_wait_percent":  wait,
		"zoomies_host_disk_total_bytes": float64(host.DiskTotalMB) * (1 << 20),
		"zoomies_host_disk_free_bytes":  float64(host.DiskFreeMB) * (1 << 20),
		"zoomies_host_disk_bound":       1,
	} {
		if got, ok := gatherValue(t, h.c, name, map[string]string{"host": host.ID}); !ok || got != want {
			t.Errorf("%s = %v (%v), want %v", name, got, ok, want)
		}
	}
}
