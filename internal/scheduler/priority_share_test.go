package scheduler

import (
	"strings"
	"testing"
	"time"

	"github.com/eyupio/zoomies/internal/store"
)

// A top-priority pool with a deep backlog used to take every create of every
// tick, so a lower tier's jobs waited for as long as that backlog lasted. The
// share across tiers bounds that wait to one interval, without taking a burst
// at the top away from it before a lower tier has waited at all.
func TestCreateBudgetIsSharedAcrossPrioritiesOnlyAfterAFullInterval(t *testing.T) {
	const interval = 30 * time.Second
	cases := []struct {
		name      string
		lowWaited time.Duration
		interval  time.Duration
		budget    int
		wantHigh  int
		wantLow   int
		deferred  bool
		// first is the pool whose create executes first. The share goes
		// ahead of the tier it was taken from, so that registration
		// admission cutting a plan short cannot cut the share out of it.
		first string
	}{
		{"a lower tier that has waited a full interval gets a share", interval, interval, 3, 2, 1, true, "low"},
		{"a lower tier that waited longer than an interval gets a share", 2 * interval, interval, 3, 2, 1, true, "low"},
		{"a lower tier that has not waited an interval does not take from the top", interval - time.Second, interval, 3, 3, 0, false, "high"},
		{"with no interval priority alone decides", time.Hour, 0, 3, 3, 0, false, "high"},
		{"an uncapped tick serves both tiers in priority order", interval, interval, 0, 5, 1, false, "high"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			high, low := testPool("high", "high"), testPool("low", "low")
			high.Priority, low.Priority = 10, 0
			var jobs []*store.Job
			for _, id := range []string{"h1", "h2", "h3", "h4", "h5"} {
				jobs = append(jobs, queued(id, time.Minute, "high"))
			}
			jobs = append(jobs, queued("l1", tc.lowWaited, "low"))
			s := snap([]*store.Pool{high, low}, nil, jobs, []*store.Host{testHost("host_a", 20, 0)})
			s.Policy.MaxCreatesPerTick = tc.budget
			s.Policy.Interval = tc.interval

			plan := Decide(s)
			byName := map[string]PoolPlan{}
			for _, pp := range plan.Pools {
				byName[pp.PoolName] = pp
			}
			if got := countOf(byName["high"].Actions, ActionCreate); got != tc.wantHigh {
				t.Errorf("high creates = %d, want %d", got, tc.wantHigh)
			}
			if got := countOf(byName["low"].Actions, ActionCreate); got != tc.wantLow {
				t.Errorf("low creates = %d, want %d", got, tc.wantLow)
			}
			if len(plan.Actions) == 0 || plan.Actions[0].PoolName != tc.first {
				t.Errorf("first action = %+v, want a create for %s", plan.Actions, tc.first)
			}
			said := strings.Contains(byName["high"].Reason, "deferred for fairness across priorities")
			if said != tc.deferred {
				t.Errorf("high reason = %q, want deferred-for-fairness %v", byName["high"].Reason, tc.deferred)
			}
		})
	}
}

// The share is one create per waiting pool, not a split of the budget: it
// stops the starvation, and priority still decides everything after it.
func TestTheShareAcrossPrioritiesIsOneCreatePerWaitingPool(t *testing.T) {
	high, mid, low := testPool("high", "high"), testPool("mid", "mid"), testPool("low", "low")
	high.Priority, mid.Priority, low.Priority = 10, 5, 0
	var jobs []*store.Job
	for _, id := range []string{"h1", "h2", "h3", "h4"} {
		jobs = append(jobs, queued(id, time.Minute, "high"))
	}
	jobs = append(jobs, queued("m1", time.Minute, "mid"), queued("m2", time.Minute, "mid"),
		queued("l1", time.Minute, "low"), queued("l2", time.Minute, "low"))
	s := snap([]*store.Pool{high, mid, low}, nil, jobs, []*store.Host{testHost("host_a", 20, 0)})
	s.Policy.MaxCreatesPerTick = 4
	s.Policy.Interval = 30 * time.Second

	got := map[string]int{}
	reasons := map[string]string{}
	for _, pp := range Decide(s).Pools {
		got[pp.PoolName] = countOf(pp.Actions, ActionCreate)
		reasons[pp.PoolName] = pp.Reason
	}
	if got["high"] != 2 || got["mid"] != 1 || got["low"] != 1 {
		t.Fatalf("creates = %v, want high 2, mid 1, low 1", got)
	}
	// mid is above the lowest pool that took a share, so its shortfall is the
	// share's doing too; low's is only the budget.
	for _, name := range []string{"high", "mid"} {
		if !strings.Contains(reasons[name], "deferred for fairness across priorities") {
			t.Errorf("%s reason = %q, want it to say it was deferred for fairness", name, reasons[name])
		}
	}
	if strings.Contains(reasons["low"], "deferred for fairness") || !strings.Contains(reasons["low"], "global limit") {
		t.Errorf("low reason = %q, want only the global limit", reasons["low"])
	}
}
