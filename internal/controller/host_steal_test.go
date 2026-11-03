package controller

import (
	"testing"
	"time"

	"github.com/eyupio/zoomies/internal/store"
)

// The figure that tells a host being starved by its hypervisor from one that is
// hard at work has to reach the three places somebody looks: the host's API view,
// its minute-by-minute history, and the Prometheus endpoint. A stale reading is a
// gap in all of them, never the last value held.
func TestStealIsReadableFromTheViewTheHistoryAndMetrics(t *testing.T) {
	h := newHarness(t)
	fresh := h.host("vm-starved")
	stale := h.host("vm-quiet")
	now := h.c.Now()

	cpu, steal := 68.0, 36.0
	if err := h.st.SetHostUsage(h.ctx, fresh.ID, store.HostUsage{CPUPercent: &cpu, StealPercent: &steal, SampledAt: now}); err != nil {
		t.Fatalf("SetHostUsage: %v", err)
	}
	old, oldSteal := 90.0, 50.0
	if err := h.st.SetHostUsage(h.ctx, stale.ID, store.HostUsage{CPUPercent: &old, StealPercent: &oldSteal,
		SampledAt: now.Add(-store.HostUsageMaxAge - time.Minute)}); err != nil {
		t.Fatalf("SetHostUsage (stale): %v", err)
	}

	host, err := h.st.GetHost(h.ctx, fresh.ID)
	if err != nil {
		t.Fatalf("GetHost: %v", err)
	}
	if view := h.c.HostView(host); view.Usage == nil || view.Usage.StealPercent == nil || *view.Usage.StealPercent != steal {
		t.Errorf("view usage = %+v, want steal %v", view.Usage, steal)
	}

	if got, ok := gatherValue(t, h.c, "zoomies_host_steal_percent", map[string]string{"host": fresh.ID}); !ok || got != steal {
		t.Errorf("zoomies_host_steal_percent = %v (%v), want %v", got, ok, steal)
	}
	if got, ok := gatherValue(t, h.c, "zoomies_host_steal_percent", map[string]string{"host": stale.ID}); ok {
		t.Errorf("a stale host reported steal %v, want it missing", got)
	}

	if err := h.c.sample(h.ctx); err != nil {
		t.Fatalf("sample: %v", err)
	}
	samples, err := h.st.ListHostSamples(h.ctx, now.Add(-time.Minute), "")
	if err != nil {
		t.Fatalf("ListHostSamples: %v", err)
	}
	byHost := map[string]store.HostSample{}
	for _, s := range samples {
		byHost[s.HostID] = s
	}
	if s := byHost[fresh.ID]; s.StealPercent == nil || *s.StealPercent != steal {
		t.Errorf("fresh host sample steal = %v, want %v", s.StealPercent, steal)
	}
	if s := byHost[stale.ID]; s.StealPercent != nil {
		t.Errorf("stale host sample carries steal %v, want a gap", *s.StealPercent)
	}
}
