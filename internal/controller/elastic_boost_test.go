package controller

import (
	"math"
	"testing"
	"time"

	"github.com/eyupio/zoomies/internal/agent"
	"github.com/eyupio/zoomies/internal/backend"
	"github.com/eyupio/zoomies/internal/events"
	"github.com/eyupio/zoomies/internal/store"
)

// elasticPool switches a pool's CPU burst mode and saves it.
func (h *harness) elasticPool(p *store.Pool, mode store.CPUBurstMode) {
	h.t.Helper()
	p.CPUBurst = store.CPUBurstPolicy{Mode: mode}
	if err := h.st.UpdatePool(h.ctx, p); err != nil {
		h.t.Fatal(err)
	}
}

// sharedRunner seeds a runner launched with a host share of cpus.
func (h *harness) sharedRunner(p *store.Pool, host *store.Host, state store.RunnerState, cpus float64) *store.Runner {
	h.t.Helper()
	r := h.runnerRow(p, host, state)
	r.AllocatedCPUs = cpus
	r.AllocationSource = store.AllocationFromHost
	if err := h.st.UpdateRunner(h.ctx, r); err != nil {
		h.t.Fatal(err)
	}
	return r
}

// calm gives a host a fresh, quiet CPU reading.
func calm(host *store.Host, now time.Time) {
	cpu := 20.0
	host.Usage = store.HostUsage{CPUPercent: &cpu, SampledAt: now}
}

// busyReport is a fresh sample of a runner using cpus cores.
func busyReport(r *store.Runner, cpus float64, now time.Time) agent.RunnerReport {
	at := now
	return agent.RunnerReport{RunnerID: r.ID, Stats: backend.Stats{SampledAt: &at, CPUPercent: cpus * 100}}
}

func targetOf(directives []agent.ElasticCPUDirective, id string) float64 {
	for _, d := range directives {
		if d.RunnerID == id {
			return d.TargetCPUs
		}
	}
	return 0
}

// The runner page and the runners table repaint a runner only from a
// runner.updated frame. A boost is written with the heartbeat's sample and
// nothing else, so without a frame of its own "Squirrel spotted" appeared on a
// reload and never while anyone was watching.
func TestABoostIsAnnouncedToTheLiveUI(t *testing.T) {
	h := newHarness(t)
	_, pool, host := h.fleet()
	h.elasticPool(pool, store.CPUBurstAutomatic)
	runner := h.sharedRunner(pool, host, store.RunnerBusy, 3.87)
	sub := h.listen(events.KindRunnerUpdated)

	now := h.c.Now()
	boosted := busyReport(runner, 7, now)
	boosted.Stats.CPUAllocationFactor = 2
	if _, err := h.c.Heartbeat(h.ctx, host.ID, agent.HeartbeatRequest{Runners: []agent.RunnerReport{boosted}}); err != nil {
		t.Fatalf("Heartbeat: %v", err)
	}
	frame := nextOfKind(t, sub, events.KindRunnerUpdated)
	cpu, _ := frame["cpu_resource"].(map[string]any)
	if frame["id"] != runner.ID || cpu == nil || cpu["state"] != "maximum_zoomies" || cpu["factor"] != float64(2) {
		t.Fatalf("runner.updated = %v, want the runner's GET shape with a doubled cpu_resource", frame)
	}

	// The same factor again changes nothing a person can see.
	if _, err := h.c.Heartbeat(h.ctx, host.ID, agent.HeartbeatRequest{Runners: []agent.RunnerReport{boosted}}); err != nil {
		t.Fatalf("Heartbeat: %v", err)
	}
	nothingFor(t, sub)
}

// An observe runner is never boosted, so a share the applied plan handed it
// was CPU nobody used and nobody else was lent. Beside an automatic runner on
// the same host it halved the automatic runner's boost for nothing.
func TestAnObservingRunnerReservesNoCPUFromAnAutomaticOne(t *testing.T) {
	h := newHarness(t)
	inst, auto, host := h.fleet()
	h.elasticPool(auto, store.CPUBurstAutomatic)
	watch := h.pool(inst, "watching", "watching")
	h.elasticPool(watch, store.CPUBurstObserve)
	a := h.sharedRunner(auto, host, store.RunnerBusy, 3.87)
	o := h.sharedRunner(watch, host, store.RunnerBusy, 3.87)
	now := h.c.Now()
	calm(host, now)

	got := h.c.elasticCPUTargets(h.ctx, host, agent.HeartbeatRequest{
		Features: []string{agent.FeatureElasticCPU},
		Runners:  []agent.RunnerReport{busyReport(a, 3.87, now), busyReport(o, 3.87, now)},
	}, now)
	alloc := host.Allocatable().CPUs
	if len(got) != 1 || math.Abs(targetOf(got, a.ID)-(alloc-3.87)) > 0.011 {
		t.Fatalf("directives = %+v, want the automatic runner lent all %.2f CPUs the observing one's guarantee leaves", got, alloc-3.87)
	}
	// Observe mode still answers its question: what the boost would have been.
	labels := map[string]string{"pool": watch.Name, "mode": string(store.CPUBurstObserve), "outcome": "burst"}
	if n, _ := gatherValue(t, h.c, "zoomies_elastic_cpu_decisions_total", labels); n != 1 {
		t.Errorf("observe burst decisions = %v, want 1", n)
	}
}

// Warm runners are what owners noticed most: sixteen CPUs, four slots, one
// busy job and three idle runners lent nothing, because each idle guarantee
// was charged in full. Only one of them can be handed a job before the next
// plan, so only one is held back -- and the moment one turns busy, the next
// plan gives everyone their guarantee again.
func TestIdleWarmRunnersLeaveABusyOneRoomToBoost(t *testing.T) {
	h := newHarness(t)
	_, pool, host := h.fleet()
	h.elasticPool(pool, store.CPUBurstAutomatic)
	busy := h.sharedRunner(pool, host, store.RunnerBusy, 3.87)
	var idle []*store.Runner
	for range 3 {
		idle = append(idle, h.sharedRunner(pool, host, store.RunnerIdle, 3.87))
	}
	now := h.c.Now()
	calm(host, now)
	alloc := host.Allocatable().CPUs
	beat := func() []agent.ElasticCPUDirective {
		return h.c.elasticCPUTargets(h.ctx, host, agent.HeartbeatRequest{
			Features: []string{agent.FeatureElasticCPU},
			Runners:  []agent.RunnerReport{busyReport(busy, 3.87, now)},
		}, now)
	}

	if got := targetOf(beat(), busy.ID); math.Abs(got-(alloc-3.87)) > 0.011 {
		t.Fatalf("busy runner beside three idle ones was lent %.2f CPUs, want %.2f: one idle guarantee held back, not three", got, alloc-3.87)
	}

	idle[0].State = store.RunnerBusy
	if err := h.st.UpdateRunner(h.ctx, idle[0]); err != nil {
		t.Fatal(err)
	}
	target := targetOf(beat(), busy.ID)
	if target == 0 {
		target = 3.87
	}
	// Two busy guarantees, the boost, and the one idle guarantee still held.
	if total := target + 3.87 + 3.87; total > alloc+0.001 {
		t.Fatalf("after an idle runner took a job the plan promised %.2f CPUs of %.2f; its guarantee was not reclaimed", total, alloc)
	}
}

// A Docker Desktop or VM daemon is smaller than the machine the agent
// measured, and refuses a quota above its own cores outright. A boost past it
// was no boost at all, and a warning on every heartbeat.
func TestABoostNeverAsksTheDaemonForMoreCoresThanItHas(t *testing.T) {
	h := newHarness(t)
	_, pool, host := h.fleet()
	h.elasticPool(pool, store.CPUBurstAutomatic)
	host.BackendInfo = store.HostBackends{{Kind: store.BackendDocker, Available: true, CPUs: 4}}
	runner := h.sharedRunner(pool, host, store.RunnerBusy, 2)
	now := h.c.Now()
	calm(host, now)

	got := h.c.elasticCPUTargets(h.ctx, host, agent.HeartbeatRequest{
		Features: []string{agent.FeatureElasticCPU},
		Runners:  []agent.RunnerReport{busyReport(runner, 2, now)},
	}, now)
	if target := targetOf(got, runner.ID); target != 4 {
		t.Fatalf("target = %v on a daemon with 4 CPUs, want 4", target)
	}
}

// A runner already on its way up for a queued job is charged its guarantee in
// the ledger, and is where that job will land. Holding back a second share for
// the same job shrank every boost by one share during every start.
func TestAStartingRunnerIsNotHeldBackTwiceForItsJob(t *testing.T) {
	h := newHarness(t)
	_, pool, host := h.fleet()
	h.elasticPool(pool, store.CPUBurstAutomatic)
	busy := h.sharedRunner(pool, host, store.RunnerBusy, 3.87)
	now := h.c.Now()
	calm(host, now)
	lent := func() float64 {
		t.Helper()
		return targetOf(h.c.elasticCPUTargets(h.ctx, host, agent.HeartbeatRequest{
			Features: []string{agent.FeatureElasticCPU},
			Runners:  []agent.RunnerReport{busyReport(busy, 3.87, now)},
		}, now), busy.ID)
	}

	h.sharedRunner(pool, host, store.RunnerProvisioning, 3.87)
	before := lent()
	h.queuedJob(t, pool, pool.Labels)
	if got := lent(); got != before {
		t.Fatalf("a job with a runner already starting for it cut the boost from %.2f to %.2f CPUs", before, got)
	}
	h.queuedJob(t, pool, pool.Labels)
	if got := lent(); got >= before {
		t.Fatalf("a second queued job, with no runner starting for it, left the boost at %.2f CPUs; its room must be held", got)
	}
}

// A docker-in-docker build runs in the daemon, which saturates its own half
// of the slot while the pair together reads about half busy. Judged on the
// sum alone, the build waited most of its run before it was lent anything.
func TestADinDPairDemandsCPUWhenItsBusierHalfIsSaturated(t *testing.T) {
	now := time.Now()
	r := &store.Runner{}
	pair := backend.Stats{SampledAt: &now, CPUPercent: 210, BusiestHalfPercent: 98}
	if !elasticCPUDemanding(r, pair, 4, now) {
		t.Fatal("a pair whose daemon is using 98% of its half was not demanding")
	}
	pair.BusiestHalfPercent = 55
	if elasticCPUDemanding(r, pair, 4, now) {
		t.Fatal("a pair using about half of each half was demanding")
	}
}

// A host at full CPU because its runners are using what they were lent must
// not trip the sustained-CPU hold, so the heartbeat records how much of it
// was lent, from the runners' own samples.
func TestTheHeartbeatRecordsHowMuchOfTheHostWasLentCPU(t *testing.T) {
	h := newHarness(t)
	_, pool, host := h.fleet()
	h.elasticPool(pool, store.CPUBurstAutomatic)
	runner := h.sharedRunner(pool, host, store.RunnerBusy, 4)
	now := h.c.Now()
	report := busyReport(runner, 8, now)
	report.Stats.CPUAllocationFactor = 2

	if got := h.c.lentCPUPercent(h.ctx, host, []agent.RunnerReport{report}, now); got != 25 {
		t.Fatalf("lent = %v%%, want 25%%: 4 of the host's 16 CPUs are lent and in use", got)
	}
	report.Stats.CPUAllocationFactor = 1
	if got := h.c.lentCPUPercent(h.ctx, host, []agent.RunnerReport{report}, now); got != 0 {
		t.Fatalf("lent = %v%% with no boost standing, want 0", got)
	}
}
