package controller

import (
	"strings"
	"testing"
	"time"

	"github.com/eyupio/zoomies/internal/agent"
	"github.com/eyupio/zoomies/internal/backend"
	"github.com/eyupio/zoomies/internal/config"
	"github.com/eyupio/zoomies/internal/scheduler"
	"github.com/eyupio/zoomies/internal/store"
)

// throttleFixture is a joined host and the means to heartbeat it with a
// measurement, which is the only way a throttle is ever decided.
type throttleFixture struct {
	h      *harness
	tr     agent.Transport
	hostID string
	cpu    float64
	load   float64
	memory int64
}

func newThrottleFixture(t *testing.T) *throttleFixture {
	t.Helper()
	h := newHarness(t)
	tr := h.c.EmbeddedTransport()
	joined, err := tr.Join(h.ctx, agent.JoinRequest{ProtocolVersion: agent.ProtocolVersion,
		Name: "pressed", Capacity: 4, CPUs: 8, MemoryMB: 16384,
		Backends: []backend.Info{{Kind: store.BackendDocker, Available: true, Limits: enforcesEverything}}})
	if err != nil {
		t.Fatal(err)
	}
	tr.SetCredentials(joined.HostID, joined.AgentToken)
	// Calm to begin with: CPU well under the bar, one runnable task per two
	// cores, half the memory free.
	return &throttleFixture{h: h, tr: tr, hostID: joined.HostID, cpu: 30, load: 4, memory: 8192}
}

// beat sends the fixture's current measurement and returns the answer and
// the host as the store now has it.
func (f *throttleFixture) beat(t *testing.T) (*agent.HeartbeatResponse, *store.Host) {
	t.Helper()
	cpu, load, memory := f.cpu, f.load, f.memory
	resp, err := f.tr.Heartbeat(f.h.ctx, agent.HeartbeatRequest{Usage: &store.HostUsage{
		CPUPercent: &cpu, LoadAverage1: &load, MemoryAvailableMB: &memory}})
	if err != nil {
		t.Fatal(err)
	}
	host, err := f.h.st.GetHost(f.h.ctx, f.hostID)
	if err != nil {
		t.Fatal(err)
	}
	return resp, host
}

func (f *throttleFixture) audits(t *testing.T, action string) int {
	t.Helper()
	rows, _, err := f.h.st.ListAudit(f.h.ctx, store.AuditFilter{Actions: []string{action}, TargetID: f.hostID}, store.Page{Limit: 50})
	if err != nil {
		t.Fatalf("ListAudit: %v", err)
	}
	return len(rows)
}

// A host whose load average has run past twice its cores is a host being
// pushed harder than it can go, and the pressure holds alone would only
// bounce it in and out of admission. The ladder takes a quarter of its slots
// and tells its agent to run the jobs at three quarters of their quota, and
// every surface an operator reads -- the row, the view, the problems drawer,
// the audit trail, the metrics -- says the same thing about it.
func TestASustainedLoadAverageThrottlesAHost(t *testing.T) {
	f := newThrottleFixture(t)
	h := f.h
	f.load = 20
	resp, host := f.beat(t)

	if resp.Throttle == nil || resp.Throttle.Level != 1 || resp.Throttle.CPUFactor != 0.75 {
		t.Fatalf("heartbeat answered %+v, want the agent told about rung 1 at three quarters", resp.Throttle)
	}
	if host.Throttle.Level != 1 || host.EffectiveCapacity() != 3 || host.Throttle.Since == nil {
		t.Fatalf("host row = %+v with effective capacity %d, want rung 1 of 4 slots leaving 3", host.Throttle, host.EffectiveCapacity())
	}
	view := h.c.HostView(host)
	if view.Throttle == nil || view.Throttle.Level != 1 || view.EffectiveCapacity != 3 || view.Free != 3 ||
		!strings.Contains(view.ThrottleReason, "load average is 20.0") {
		t.Fatalf("host view = throttle %+v, effective %d, free %d, reason %q", view.Throttle, view.EffectiveCapacity, view.Free, view.ThrottleReason)
	}
	p := h.problem(t, "host.throttled")
	if p.Severity != config.SeverityWarning || !strings.Contains(p.Detail, "pressed") || !strings.Contains(p.Detail, "3 of 4 slots") {
		t.Fatalf("host.throttled = %+v; it has to name the host and what it was stepped down to", p)
	}
	if f.audits(t, "host.throttle") != 1 {
		t.Fatalf("host.throttle audit rows = %d, want 1", f.audits(t, "host.throttle"))
	}
	for name, want := range map[string]float64{
		"zoomies_host_throttle_level":     1,
		"zoomies_host_load_average_1m":    20,
		"zoomies_host_effective_capacity": 3,
		"zoomies_host_capacity":           4,
	} {
		labels := map[string]string{}
		if strings.HasSuffix(name, "_level") || strings.HasSuffix(name, "_1m") {
			labels["host"] = host.ID
		}
		if got, ok := gatherValue(t, h.c, name, labels); !ok || got != want {
			t.Errorf("%s = %v (%v), want %v", name, got, ok, want)
		}
	}
	stats, err := h.c.Stats(h.ctx, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if stats.Hosts.Capacity != 3 {
		t.Errorf("Stats.Hosts.Capacity = %d, want the 3 slots the throttle leaves rather than the 4 configured", stats.Hosts.Capacity)
	}
	pool := h.pool(h.installation(), "linux-x64")
	if got := eligibleCapacity(pool, []*store.Host{host}, h.c.Now()); got != 3 {
		t.Errorf("capacity-demand eligible capacity = %d, want 3; a provisioner told 4 would never be asked for the slot the queue is short of", got)
	}

	// The next rung waits for ThrottleStep, because a quota lowered on a
	// live container takes a while to show in a load average that itself
	// averages over a minute.
	if resp, _ := f.beat(t); resp.Throttle.Level != 1 {
		t.Fatalf("a second overwhelmed beat inside ThrottleStep moved the rung to %d", resp.Throttle.Level)
	}
	h.advance(scheduler.ThrottleStep)
	resp, host = f.beat(t)
	if resp.Throttle.Level != 2 || resp.Throttle.CPUFactor != 0.5 || host.EffectiveCapacity() != 2 {
		t.Fatalf("after ThrottleStep: %+v, effective %d; want rung 2 at half quota leaving 2 slots", resp.Throttle, host.EffectiveCapacity())
	}
}

// A throttle comes off one rung at a time, and only after a stretch of calm
// long enough to mean something: a host that recovered for a minute and was
// pushed straight back over would otherwise oscillate with the ladder. The
// step down is audited as a lift, so the trail reads as a pair.
func TestAThrottleLiftsOneStepAfterCalm(t *testing.T) {
	f := newThrottleFixture(t)
	h := f.h
	f.load = 20
	f.beat(t)
	h.advance(scheduler.ThrottleStep)
	if _, host := f.beat(t); host.Throttle.Level != 2 {
		t.Fatalf("setup: level %d, want 2", host.Throttle.Level)
	}

	f.load = 2
	_, host := f.beat(t)
	if host.Throttle.Level != 2 || host.Throttle.CalmSince == nil {
		t.Fatalf("one calm beat: %+v; want the rung kept and the calm streak started", host.Throttle)
	}
	h.advance(scheduler.ThrottleRecovery / 2)
	if _, host := f.beat(t); host.Throttle.Level != 2 {
		t.Fatalf("half a recovery of calm already stepped the host down to %d", host.Throttle.Level)
	}
	h.advance(scheduler.ThrottleRecovery / 2)
	resp, host := f.beat(t)
	if host.Throttle.Level != 1 || resp.Throttle.Level != 1 || resp.Throttle.CPUFactor != 0.75 {
		t.Fatalf("after ThrottleRecovery of calm: row %+v, answer %+v; want rung 1", host.Throttle, resp.Throttle)
	}
	if f.audits(t, "host.throttle_lift") != 1 {
		t.Fatalf("host.throttle_lift audit rows = %d, want 1", f.audits(t, "host.throttle_lift"))
	}
	h.advance(scheduler.ThrottleRecovery)
	resp, host = f.beat(t)
	if host.Throttle.Active() || resp.Throttle.Level != 0 || resp.Throttle.CPUFactor != 1 {
		t.Fatalf("after a second recovery: row %+v, answer %+v; want no throttle and the agent told to restore every quota", host.Throttle, resp.Throttle)
	}
	if h.c.HostView(host).Throttle != nil || contains(h.problemCodes(), "host.throttled") {
		t.Fatal("a lifted throttle is still on the view or in the problems drawer")
	}
}

// An operator who has fixed the cause -- a pool given limits, a capacity
// lowered -- should not have to wait out the recovery. The clear lifts every
// rung at once, publishes the host, and writes no audit row of its own: the
// API records who asked, and a system row beside it would say the ladder
// decided something a person did.
func TestAnOperatorCanClearAThrottle(t *testing.T) {
	f := newThrottleFixture(t)
	h := f.h
	f.load = 20
	f.beat(t)
	h.advance(scheduler.ThrottleStep)
	f.beat(t)

	// Drain the nudge the heartbeats left, so the one below is the clear's.
	select {
	case <-h.c.nudges:
	default:
	}
	host, err := h.c.ClearHostThrottle(h.ctx, f.hostID)
	if err != nil {
		t.Fatalf("ClearHostThrottle: %v", err)
	}
	if host.Throttle.Active() || host.EffectiveCapacity() != 4 {
		t.Fatalf("cleared host = %+v with effective capacity %d", host.Throttle, host.EffectiveCapacity())
	}
	stored, err := h.st.GetHost(h.ctx, f.hostID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.Throttle.Active() {
		t.Fatal("the clear was not persisted")
	}
	if f.audits(t, "host.throttle_lift") != 0 || f.audits(t, "host.throttle_clear") != 0 {
		t.Fatal("ClearHostThrottle wrote an audit row; that is the caller's, under the operator's name")
	}
	if len(h.c.nudges) != 1 {
		t.Fatal("the clear did not nudge a pass; the slots it gave back would wait for the interval")
	}

	// Nothing pins it: the pressure is still there, so the next beat puts
	// the host straight back on the first rung.
	if resp, _ := f.beat(t); resp.Throttle.Level != 1 {
		t.Fatalf("after a premature clear the next beat answered %+v, want rung 1 again", resp.Throttle)
	}
	// And an unthrottled host is returned unchanged rather than refused.
	if _, err := h.c.ClearHostThrottle(h.ctx, f.hostID); err != nil {
		t.Fatalf("clearing again: %v", err)
	}
}

// A host whose agent stopped sending measurements -- downgraded to a build
// without them, or gone -- must not stay throttled for as long as nobody
// heartbeats. Housekeeping runs the ladder for it, and the ladder lifts a
// throttle its measurements have been stale on for StaleThrottleReset.
func TestHousekeepingLiftsAThrottleWhoseHostStoppedMeasuring(t *testing.T) {
	f := newThrottleFixture(t)
	h := f.h
	f.load = 20
	if _, host := f.beat(t); host.Throttle.Level != 1 {
		t.Fatalf("setup: %+v", host.Throttle)
	}

	h.advance(scheduler.StaleThrottleReset - time.Second)
	h.c.settleThrottles(h.ctx, h.c.Now())
	host, err := h.st.GetHost(h.ctx, f.hostID)
	if err != nil {
		t.Fatal(err)
	}
	if !host.Throttle.Active() {
		t.Fatal("a stale sample lifted the throttle before StaleThrottleReset; a host that merely missed a few beats would be handed its slots back")
	}
	h.advance(2 * time.Second)
	h.c.settleThrottles(h.ctx, h.c.Now())
	host, err = h.st.GetHost(h.ctx, f.hostID)
	if err != nil {
		t.Fatal(err)
	}
	if host.Throttle.Active() {
		t.Fatalf("after StaleThrottleReset the host is still on rung %d", host.Throttle.Level)
	}
	if f.audits(t, "host.throttle_lift") != 1 {
		t.Fatalf("host.throttle_lift audit rows = %d, want 1 for the stale reset", f.audits(t, "host.throttle_lift"))
	}
}

// scheduler.host_throttling off means no host is ever put on a rung, and a
// host already on one when the setting is turned off is lifted rather than
// left there for ever with nothing to step it down.
func TestThrottlingOffDecidesNothingAndLiftsWhatStands(t *testing.T) {
	f := newThrottleFixture(t)
	h := f.h
	h.c.UpdateConfig(func(cfg *config.Config) { cfg.Scheduler.HostThrottling = false })
	f.load = 20
	resp, host := f.beat(t)
	if host.Throttle.Active() || resp.Throttle == nil || resp.Throttle.Level != 0 || resp.Throttle.CPUFactor != 1 {
		t.Fatalf("with throttling off: row %+v, answer %+v; want no rung and the agent still told so", host.Throttle, resp.Throttle)
	}

	// A rung left over from before the setting was turned off.
	now := h.c.Now()
	if err := h.st.SetHostThrottle(h.ctx, f.hostID, store.HostThrottle{Level: 2, Since: &now, ChangedAt: &now, Reason: "earlier"}); err != nil {
		t.Fatal(err)
	}
	h.c.settleThrottles(h.ctx, h.c.Now())
	host, err := h.st.GetHost(h.ctx, f.hostID)
	if err != nil {
		t.Fatal(err)
	}
	if host.Throttle.Active() {
		t.Fatalf("housekeeping left a host on rung %d with throttling off", host.Throttle.Level)
	}
	if f.audits(t, "host.throttle_lift") != 1 {
		t.Fatalf("host.throttle_lift audit rows = %d, want 1", f.audits(t, "host.throttle_lift"))
	}
}

// A host that joins again starts on no rung: the throttle was decided from
// measurements of a machine that has just been rebuilt or restarted. If the
// pressure is still there its first heartbeats put it straight back.
func TestARejoiningHostStartsOnNoRung(t *testing.T) {
	f := newThrottleFixture(t)
	h := f.h
	f.load = 20
	if _, host := f.beat(t); !host.Throttle.Active() {
		t.Fatal("setup: not throttled")
	}
	joined, err := f.tr.Join(h.ctx, agent.JoinRequest{ProtocolVersion: agent.ProtocolVersion,
		Name: "pressed", Capacity: 4, CPUs: 8, MemoryMB: 16384,
		Backends: []backend.Info{{Kind: store.BackendDocker, Available: true, Limits: enforcesEverything}}})
	if err != nil {
		t.Fatal(err)
	}
	host, err := h.st.GetHost(h.ctx, joined.HostID)
	if err != nil {
		t.Fatal(err)
	}
	if host.Throttle.Active() {
		t.Fatalf("a re-joined host is still on rung %d", host.Throttle.Level)
	}
}

// The reason a host takes no new runner used to fall through to "upgrade its
// agent" for every case the sentence had no words for, so a host held for
// pressure or throttled to its last slot was told to upgrade. Each is its own
// job and gets its own sentence.
func TestUnavailableReasonNamesAHoldAndAThrottle(t *testing.T) {
	now := time.Now()
	fresh := now.Add(-time.Second)
	held := &store.Host{Name: "held", Capacity: 4, CPUs: 8, MemoryMB: 16384, LastHeartbeat: now,
		Usage: store.HostUsage{SampledAt: fresh, CPUHeld: true}}
	throttled := &store.Host{Name: "throttled", Capacity: 4, CPUs: 8, MemoryMB: 16384, LastHeartbeat: now,
		ActiveRunners: 2, Throttle: store.HostThrottle{Level: 2}}
	roomy := &store.Host{Name: "roomy", Capacity: 4, CPUs: 8, MemoryMB: 16384, LastHeartbeat: now,
		ActiveRunners: 1, Throttle: store.HostThrottle{Level: 2}}
	old := &store.Host{Name: "old", Capacity: 4, LastHeartbeat: now, Incompatible: true}
	for _, tc := range []struct {
		host *store.Host
		want string
	}{
		{held, "under pressure"},
		{throttled, "throttled to 2 of its 4 slots"},
		{old, "protocol"},
		// A throttled host with a slot free is not unavailable at all; the
		// sentence for it is the last resort, never the upgrade advice.
		{roomy, "takes no new runners right now"},
	} {
		if got := unavailableReason(tc.host, now); !strings.Contains(got, tc.want) {
			t.Errorf("%s: reason %q does not say %q", tc.host.Name, got, tc.want)
		}
	}
	if !roomy.Available(now) {
		t.Error("a throttled host with a slot left under its effective capacity should still be available")
	}
	if throttled.Available(now) {
		t.Error("a throttled host at its effective capacity should not be available")
	}
}
