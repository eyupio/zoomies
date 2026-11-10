package controller

import (
	"strings"
	"testing"
	"time"

	"github.com/eyupio/zoomies/internal/scheduler"
	"github.com/eyupio/zoomies/internal/store"
)

// The recommendation reads what a host's runners were measured to use: its CPU
// in use, divided by the runners at work, in each sampled minute with one at
// work. A minute with nothing running says nothing about a runner.
func TestPoolPerformanceMeasuresWhatARunnerUsesFromTheHostSamples(t *testing.T) {
	h := newHarness(t)
	_, pool, host := h.fleet()
	now := h.c.Now().Truncate(time.Minute)
	cpu := func(v float64) *float64 { return &v }
	var samples []store.HostSample
	for i := range 80 {
		// Two runners at work on a four-CPU host that is half busy: one core each.
		samples = append(samples, store.HostSample{
			HostID: host.ID, At: now.Add(-time.Duration(i+1) * time.Minute), Capacity: 4,
			ActiveRunners: 2, CPUPercent: cpu(50), CPUs: 4,
		})
	}
	// An idle minute, and one with no CPU figure, are not a runner's use.
	samples = append(samples,
		store.HostSample{HostID: host.ID, At: now.Add(-90 * time.Minute), Capacity: 4, ActiveRunners: 0, CPUPercent: cpu(2), CPUs: 4},
		store.HostSample{HostID: host.ID, At: now.Add(-91 * time.Minute), Capacity: 4, ActiveRunners: 2, CPUs: 4},
	)
	if err := h.st.RecordHostSamples(h.ctx, samples); err != nil {
		t.Fatalf("RecordHostSamples: %v", err)
	}

	view, err := h.c.PoolPerformance(h.ctx, pool)
	if err != nil {
		t.Fatalf("PoolPerformance: %v", err)
	}
	if view.Measured.Samples != 80 || view.Measured.CPUPerRunner != 1 {
		t.Fatalf("measured = %+v, want 80 minutes at one core a runner", view.Measured)
	}
	if view.PoolID != pool.ID {
		t.Errorf("pool = %q, want %q", view.PoolID, pool.ID)
	}
	// Nothing waited, so there is no case for more slots, and it says why.
	if view.Verdict != scheduler.PerformanceKeep || !strings.Contains(view.Reason, "would not shorten the wait") {
		t.Errorf("verdict %q, reason %q; want a keep that says nothing waited for room", view.Verdict, view.Reason)
	}
}

// A pool not yet made is read against every host that is present, which is what
// the wizard asks while the pool is still a draft.
func TestPoolPerformanceOfAPoolNotYetMadeReadsEveryPresentHost(t *testing.T) {
	h := newHarness(t)
	h.fleet()
	view, err := h.c.PoolPerformance(h.ctx, nil)
	if err != nil {
		t.Fatalf("PoolPerformance: %v", err)
	}
	if view.PoolID != "" || view.Verdict != scheduler.PerformanceUnknown {
		t.Fatalf("view = %+v, want an unknown verdict for no pool and no samples", view.PerformanceRecommendation)
	}
	if view.SlotsBefore == 0 {
		t.Errorf("slots before = 0, want the present host's capacity counted")
	}
}
