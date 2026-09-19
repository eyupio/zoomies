package scheduler

import "testing"

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
