package scheduler

import (
	"testing"
	"time"

	"github.com/eyupio/zoomies/internal/store"
)

func TestElasticCPUPlanLendsIdleCPUAndProtectsAStart(t *testing.T) {
	work := []ElasticCPUWorkload{{ID: "busy", BaseCPUs: 2, MaxCPUs: 8, Demanding: true}}
	if got := ElasticCPUPlan(8, 0, work)["busy"]; got != 8 {
		t.Fatalf("without queued work target = %v, want 8", got)
	}
	if got := ElasticCPUPlan(8, 2, work)["busy"]; got != 6 {
		t.Fatalf("with one start protected target = %v, want 6", got)
	}
}

func TestElasticCPUPlanIsFairAndReturnsUnusedCeilings(t *testing.T) {
	work := []ElasticCPUWorkload{
		{ID: "a", BaseCPUs: 2, MaxCPUs: 3, Demanding: true},
		{ID: "b", BaseCPUs: 2, MaxCPUs: 8, Demanding: true},
	}
	got := ElasticCPUPlan(8, 0, work)
	if got["a"] != 3 || got["b"] != 5 {
		t.Fatalf("targets = %v, want a=3 and b=5", got)
	}
}

func TestElasticCPUPlanCountsWorkloadsThatCannotBurst(t *testing.T) {
	work := []ElasticCPUWorkload{
		{ID: "fixed", BaseCPUs: 4, MaxCPUs: 4},
		{ID: "busy", BaseCPUs: 2, MaxCPUs: 8, Demanding: true},
	}
	got := ElasticCPUPlan(8, 0, work)
	if got["fixed"] != 4 || got["busy"] != 4 {
		t.Fatalf("targets = %v, want fixed=4 and busy=4", got)
	}
}

func TestElasticCPUPlanRestoresTheBaseWithoutDemand(t *testing.T) {
	got := ElasticCPUPlan(8, 0, []ElasticCPUWorkload{{ID: "quiet", BaseCPUs: 2, MaxCPUs: 8}})
	if got["quiet"] != 2 {
		t.Fatalf("quiet target = %v, want its base 2", got["quiet"])
	}
}

// Warm runners were the owners' commonest disappointment: sixteen CPUs, four
// slots, one busy job and three idle runners left nothing to lend, because
// every idle guarantee was charged in full. Only one of them can be handed the
// next job before the following plan, so only one is held back.
func TestElasticCPUPlanHoldsBackOneIdleGuaranteeNotAll(t *testing.T) {
	work := []ElasticCPUWorkload{
		{ID: "busy", BaseCPUs: 4, MaxCPUs: 16, Demanding: true},
		{ID: "idle-1", BaseCPUs: 4, MaxCPUs: 4, Idle: true},
		{ID: "idle-2", BaseCPUs: 4, MaxCPUs: 4, Idle: true},
		{ID: "idle-3", BaseCPUs: 4, MaxCPUs: 4, Idle: true},
	}
	got := ElasticCPUPlan(16, 0, work)
	if got["busy"] != 12 {
		t.Fatalf("busy target = %v, want 12: three idle runners hold back one guarantee between them", got["busy"])
	}
	for _, id := range []string{"idle-1", "idle-2", "idle-3"} {
		if got[id] != 4 {
			t.Errorf("%s target = %v, want its guarantee of 4", id, got[id])
		}
	}

	// One of them takes a job. The next plan charges it in full, and the
	// boost shrinks back so that everyone has their guarantee again.
	work[1].Idle = false
	got = ElasticCPUPlan(16, 0, work)
	if got["busy"] != 8 {
		t.Fatalf("after an idle runner turned busy the boost is %v, want 8", got["busy"])
	}
	// The two busy runners and the one idle guarantee still held back fit.
	if sum := got["busy"] + got["idle-1"] + 4; sum > 16 {
		t.Fatalf("busy targets plus the idle reserve are %v, more than the host's 16", sum)
	}
}

// A boost being used is load the plan chose, and the next plan takes it back
// for a start. Read as pressure it held new starts and kept a host on its
// throttle rung for doing what the plan asked.
func TestLentCPUInUseIsNotHostPressure(t *testing.T) {
	now := time.Now()
	cpu := 97.0
	h := &store.Host{CPUs: 16, Usage: store.HostUsage{CPUPercent: &cpu, SampledAt: now, LentCPUPercent: 40}}
	if hostUnderCPUPressure(h, now) {
		t.Error("a host at 97% with 40% of it lent CPU in use was under CPU pressure")
	}
	if !hostCalm(h) {
		t.Error("a host at 97% with 40% of it lent CPU in use was not calm")
	}
	h.Usage.LentCPUPercent = 0
	if !hostUnderCPUPressure(h, now) || hostCalm(h) {
		t.Error("a host at 97% with nothing lent was not judged under pressure")
	}
}
