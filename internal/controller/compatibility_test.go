package controller

import (
	"strings"
	"testing"

	"github.com/eyupio/zoomies/internal/agent"
	"github.com/eyupio/zoomies/internal/scheduler"
)

// The protocol was checked at join and nowhere else, so an agent that joined
// before a bump kept polling and receiving tasks it could not understand. The
// answer is exclusion rather than refusal: refusing the heartbeat would send
// every agent in the fleet into its re-join path at the same moment, which is
// the outage the upgrade was meant to avoid.
func TestAnAgentThatFallsOutOfProtocolIsExcludedRatherThanRefused(t *testing.T) {
	h := newHarness(t)
	tr, id := joinedHost(t, h, 500_000, 200_000)

	// The bump, from the fleet's point of view: the same agent, now speaking a
	// version this controller does not.
	resp, err := tr.Heartbeat(h.ctx, agent.HeartbeatRequest{ProtocolVersion: agent.ProtocolVersion + 1})
	if err != nil {
		t.Fatalf("the heartbeat was refused, which would restart every agent at once: %v", err)
	}
	if !resp.Incompatible {
		t.Error("the response does not tell the agent it is incompatible")
	}
	if !resp.Cordoned {
		t.Error("the response does not stop the agent asking for work; an agent that ignores the new field would keep asking")
	}
	// Both numbers, because an operator has to know which side to upgrade.
	for _, want := range []string{"protocol version", "Upgrade this agent"} {
		if !strings.Contains(resp.IncompatibleReason, want) {
			t.Errorf("the reason does not mention %q: %q", want, resp.IncompatibleReason)
		}
	}

	host, err := h.st.GetHost(h.ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if !host.Incompatible || host.ProtocolVersion != agent.ProtocolVersion+1 {
		t.Fatalf("the host row does not record the mismatch: %+v", host)
	}
	// Excluded from placement exactly as a cordon excludes it.
	if scheduler.HostAvailable(host, h.c.Now()) {
		t.Error("an incompatible host is still available for placement")
	}

	// And it comes back on its own when the agent is upgraded, without a
	// re-join: the fleet heals host by host as binaries are replaced.
	if _, err := tr.Heartbeat(h.ctx, agent.HeartbeatRequest{ProtocolVersion: agent.ProtocolVersion}); err != nil {
		t.Fatalf("Heartbeat: %v", err)
	}
	host, err = h.st.GetHost(h.ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if host.Incompatible {
		t.Error("an upgraded agent is still marked incompatible")
	}
	if !scheduler.HostAvailable(host, h.c.Now()) {
		t.Error("an upgraded agent's host did not become available again")
	}
}

// An agent old enough not to send a version is the one case this cannot judge.
// Guessing would empty a fleet the moment its controller learnt to ask.
func TestAnAgentThatSendsNoProtocolIsNotJudged(t *testing.T) {
	h := newHarness(t)
	tr, id := joinedHost(t, h, 500_000, 200_000)

	if _, err := tr.Heartbeat(h.ctx, agent.HeartbeatRequest{}); err != nil {
		t.Fatalf("Heartbeat: %v", err)
	}
	host, err := h.st.GetHost(h.ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if host.Incompatible {
		t.Error("a host that reported no protocol was marked incompatible")
	}
	if !scheduler.HostAvailable(host, h.c.Now()) {
		t.Error("a host that reported no protocol was excluded from placement")
	}
}

// A pool that cannot place because every host runs an old agent has to say so.
// It is the blockage an operator is least likely to guess: nothing is broken,
// the hosts are up and heartbeating, and the fleet has simply stopped placing.
func TestAPoolBlockedByOldAgentsSaysWhichVersionToFix(t *testing.T) {
	h := newHarness(t)
	inst := h.installation()
	pool := h.pool(inst, "linux-x64")
	host := h.host("vm-1")
	host.Incompatible = true
	host.ProtocolVersion = agent.ProtocolVersion + 1
	if err := h.st.UpdateHost(h.ctx, host); err != nil {
		t.Fatal(err)
	}
	h.deliverJob(jobEvent{Action: "queued", JobID: 1, Labels: []string{"self-hosted", "linux", "x64", "demo"}})

	if err := h.c.Reconcile(h.ctx); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	if runners := h.runners(); len(runners) != 0 {
		t.Fatalf("a runner was placed on an incompatible host: %d", len(runners))
	}

	plan, _ := h.c.getLastPlan()
	if plan == nil {
		t.Fatal("no plan")
	}
	var blocked, fix string
	for _, pp := range plan.Pools {
		if pp.PoolID == pool.ID {
			blocked, fix = pp.Blocked, pp.BlockedFix
		}
	}
	if !strings.Contains(blocked, "cannot talk to") {
		t.Errorf("the reason does not name the incompatible agents: %q", blocked)
	}
	if !strings.Contains(fix, "upgrade the agent") {
		t.Errorf("the fix does not say to upgrade the agent: %q", fix)
	}
}

// An agent one release behind does not ignore a task kind it has never heard
// of -- it reports the task failed. The controller used to treat any kind it
// could not name as lifecycle work and mark a perfectly healthy runner failed
// for it, which is the opposite of what a skewed fleet needs.
func TestAFailureOnAnUnknownTaskKindLeavesTheRunnerAlone(t *testing.T) {
	for _, tc := range []struct {
		kind agent.TaskKind
		want bool
	}{
		{agent.TaskCreateRunner, true},
		{agent.TaskStopRunner, true},
		{agent.TaskRemoveRunner, true},
		{agent.TaskStreamLogs, false},
		{agent.TaskCancelLogs, false},
		{agent.TaskPrewarmImage, false},
		{agent.TaskKind("something_a_later_release_added"), false},
	} {
		if got := lifecycleTask(tc.kind); got != tc.want {
			t.Errorf("lifecycleTask(%q) = %v, want %v", tc.kind, got, tc.want)
		}
	}
}
