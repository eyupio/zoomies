package controller

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/eyupio/zoomies/internal/agent"
	"github.com/eyupio/zoomies/internal/backend"
	"github.com/eyupio/zoomies/internal/store"
)

func TestElasticCPUDemandUsesBusyCPUOrNewCgroupThrottling(t *testing.T) {
	now := time.Now()
	previous, _ := json.Marshal(backend.Stats{CPUThrottling: &backend.CPUThrottling{Periods: 10, ThrottledPeriods: 2}})
	r := &store.Runner{ResourceSample: previous}

	busy := backend.Stats{SampledAt: &now, CPUPercent: 175}
	if !elasticCPUDemanding(r, busy, 2, now) {
		t.Fatal("a runner using at least 80% of its 2 CPU guarantee was not demanding")
	}
	throttled := backend.Stats{SampledAt: &now, CPUPercent: 20, CPUThrottling: &backend.CPUThrottling{Periods: 12, ThrottledPeriods: 3}}
	if !elasticCPUDemanding(r, throttled, 2, now) {
		t.Fatal("new cgroup throttling was not treated as CPU demand")
	}
	stale := now.Add(-store.HostUsageMaxAge - time.Second)
	if elasticCPUDemanding(r, backend.Stats{SampledAt: &stale, CPUPercent: 200}, 2, now) {
		t.Fatal("a stale sample requested elastic CPU")
	}
}

func TestElasticCPUControllerLendsOnlyOnAFreshCalmCapableHost(t *testing.T) {
	h := newHarness(t)
	_, pool, host := h.fleet()
	pool.CPUBurst = store.CPUBurstPolicy{Mode: store.CPUBurstAutomatic}
	if err := h.st.UpdatePool(h.ctx, pool); err != nil {
		t.Fatal(err)
	}
	runner := h.runnerRow(pool, host, store.RunnerBusy)
	runner.AllocatedCPUs = 3.87
	runner.AllocationSource = store.AllocationFromHost
	if err := h.st.UpdateRunner(h.ctx, runner); err != nil {
		t.Fatal(err)
	}
	now := h.c.Now()
	cpu := 20.0
	host.Usage = store.HostUsage{CPUPercent: &cpu, SampledAt: now}
	sampled := now

	directives := h.c.elasticCPUTargets(h.ctx, host, agent.HeartbeatRequest{
		Features: []string{agent.FeatureElasticCPU},
		Runners: []agent.RunnerReport{{RunnerID: runner.ID, Stats: backend.Stats{
			SampledAt: &sampled, CPUPercent: 400,
		}}},
	}, now)
	if len(directives) != 1 || directives[0].RunnerID != runner.ID || directives[0].BaseCPUs != runner.AllocatedCPUs || directives[0].TargetCPUs <= directives[0].BaseCPUs {
		t.Fatalf("directives = %+v, want one safe CPU boost", directives)
	}

	cpu = 90
	if got := h.c.elasticCPUTargets(h.ctx, host, agent.HeartbeatRequest{
		Features: []string{agent.FeatureElasticCPU},
		Runners:  []agent.RunnerReport{{RunnerID: runner.ID, Stats: backend.Stats{SampledAt: &sampled, CPUPercent: 400}}},
	}, now); len(got) != 0 {
		t.Fatalf("busy host received elastic CPU: %+v", got)
	}
}

func TestCPUResourceStatusSaysWhatTheNumbersMean(t *testing.T) {
	h := &store.Host{CPUs: 8, Capacity: 2}
	p := &store.Pool{CPUBurst: store.CPUBurstPolicy{Mode: store.CPUBurstAutomatic}}
	sample, _ := json.Marshal(backend.Stats{CPUAllocationFactor: 2})
	r := &store.Runner{AllocatedCPUs: 2, ResourceSample: sample}

	got := cpuResourceView(r, p, h)
	if got == nil || got.State != "maximum_zoomies" || got.Label != "Squirrel spotted — maximum zoomies" || got.CurrentCPUs != 4 || got.GuaranteedCPUs != 2 {
		t.Fatalf("resource status = %+v, want a truthful maximum-zoomies label over 2 -> 4 CPUs", got)
	}

	sample, _ = json.Marshal(backend.Stats{CPUAllocationFactor: .5})
	r.ResourceSample = sample
	p.CPUBurst.Mode = store.CPUBurstOff
	got = cpuResourceView(r, p, h)
	if got.State != "throttled" || got.Label != "Leash tightened — host under pressure" || got.CurrentCPUs != 1 {
		t.Fatalf("resource status = %+v, want a host-pressure throttle at 1 CPU", got)
	}

	p.DockerMode = store.DockerDinD
	r.AllocationSource = store.AllocationFromPool
	got = cpuResourceView(r, p, h)
	if got.GuaranteedCPUs != 4 || got.CurrentCPUs != 2 {
		t.Fatalf("fixed DinD resource status = %+v, want both 2-CPU containers represented", got)
	}

	// A pool with elasticity off holds its runner at its share, and a runner
	// doing exactly that has a state of its own rather than none: no state at
	// all reads as a quota nobody measured, which a held one is not.
	sample, _ = json.Marshal(backend.Stats{CPUAllocationFactor: 1})
	r.ResourceSample = sample
	got = cpuResourceView(r, p, h)
	if got == nil || got.State != "sit_and_stay" || got.Label != "Sit and stay — CPU held at its share" || got.Reason != "elastic_off" {
		t.Fatalf("resource status = %+v, want a runner held at its share to say so", got)
	}
	if got.CurrentCPUs != got.GuaranteedCPUs || got.CeilingCPUs != got.GuaranteedCPUs {
		t.Fatalf("resource status = %+v, want current and ceiling both at the guarantee", got)
	}
	// A runner nothing has sampled yet says the same: no sample is a factor
	// of one, not an unknown.
	r.ResourceSample = nil
	if got = cpuResourceView(r, p, h); got == nil || got.State != "sit_and_stay" {
		t.Fatalf("resource status = %+v, want sit_and_stay before the first sample", got)
	}
}
