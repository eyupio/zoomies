package controller

import (
	"strings"
	"testing"
	"time"

	"github.com/eyupio/zoomies/internal/scheduler"
	"github.com/eyupio/zoomies/internal/store"
)

// The diagnostics fixture carries one throttled host with a runner on it that
// was given the host's default share, so the host card's throttle notice, the
// runner detail's allocation and the drawer's host.throttled entry can all be
// looked at -- and it has to stay that way for as long as the instance is
// open, or a screenshot taken a minute after start shows a different fleet.
func TestSeedStuckAddsAThrottledHostThatStaysThrottled(t *testing.T) {
	h := newHarness(t)
	t.Setenv(StuckSeedEnvVar, "1")
	if err := h.c.SeedDemo(h.ctx); err != nil {
		t.Fatalf("SeedDemo: %v", err)
	}
	if err := h.c.SeedStuck(h.ctx); err != nil {
		t.Fatalf("SeedStuck: %v", err)
	}
	host, err := h.st.GetHostByName(h.ctx, StuckThrottledHostName)
	if err != nil {
		t.Fatalf("the fixture's throttled host is missing: %v", err)
	}
	if host.Throttle.Level != 2 || host.EffectiveCapacity() != 2 || !host.Usage.Fresh(h.c.Now()) {
		t.Fatalf("host = throttle %+v, effective %d, fresh %v; want rung 2 of 4 with a fresh sample", host.Throttle, host.EffectiveCapacity(), host.Usage.Fresh(h.c.Now()))
	}
	view := h.c.HostView(host)
	if !strings.Contains(view.ThrottleReason, "30.0") || view.Throttle == nil || view.EffectiveCapacity != 2 {
		t.Fatalf("view = reason %q, throttle %+v, effective %d", view.ThrottleReason, view.Throttle, view.EffectiveCapacity)
	}
	if !contains(h.problemCodes(), "host.throttled") {
		t.Fatalf("problems = %v, want host.throttled", h.problemCodes())
	}
	var placed *store.Runner
	for _, r := range h.runners() {
		if r.HostID == host.ID {
			placed = r
		}
	}
	if placed == nil || placed.State != store.RunnerBusy || placed.AllocationSource != store.AllocationFromHost ||
		placed.AllocatedCPUs != 1.87 || placed.AllocatedMemoryMB != 3968 {
		t.Fatalf("runner on the throttled host = %+v; want a busy one allocated the host's share", placed)
	}
	if got := len(h.runners()); got != 12 {
		t.Fatalf("the fixture has %d runners, want the demo's 12: the runner was to be moved, not added", got)
	}

	// Seeding again is a no-op.
	if err := h.c.SeedStuck(h.ctx); err != nil {
		t.Fatalf("second SeedStuck: %v", err)
	}
	hosts, err := h.st.ListHosts(h.ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(hosts) != 4 {
		t.Fatalf("seeded %d hosts, want the demo's 3 and the throttled one", len(hosts))
	}

	// Long enough for the sample to have gone stale and the reset to be due:
	// the demo beat keeps the sample fresh and housekeeping leaves the
	// fixture's rung alone, so the card keeps explaining itself.
	future := time.Now().Add(scheduler.StaleThrottleReset * 2)
	h.c.clock = func() time.Time { return future }
	h.c.beatDemoHosts(h.ctx)
	h.c.settleThrottles(h.ctx, future)
	host, err = h.st.GetHost(h.ctx, host.ID)
	if err != nil {
		t.Fatal(err)
	}
	if host.Throttle.Level != 2 || !host.Usage.Fresh(future) || !host.Healthy(future) {
		t.Fatalf("after housekeeping: throttle %+v, fresh %v, healthy %v; the fixture did not hold", host.Throttle, host.Usage.Fresh(future), host.Healthy(future))
	}
}
