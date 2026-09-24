package scheduler

import (
	"strings"
	"testing"
	"time"

	"github.com/eyupio/zoomies/internal/store"
)

// limits.runners is a ceiling on the whole fleet, not on a pool or a pass: it
// counts the runners already live in every pool, it is shared between pools,
// and a pool it holds back must say that it did -- otherwise the operator
// reads "the next pass will continue" about a shortfall no pass will clear.
func TestTheRunnerCeilingCountsTheWholeFleetAndSaysSoWhenItBinds(t *testing.T) {
	cases := []struct {
		name        string
		ceiling     int
		wantCreates int
		wantCeiling bool
	}{
		{"no ceiling leaves every job served", 0, 6, false},
		{"a ceiling above demand changes nothing", 20, 6, false},
		{"a ceiling below demand allows only the difference", 3, 2, true},
		{"a fleet already at its ceiling creates nothing", 1, 0, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			a, z := testPool("aaa", "aaa"), testPool("zzz", "zzz")
			var jobs []*store.Job
			for _, id := range []string{"a1", "a2", "a3"} {
				jobs = append(jobs, queued(id, time.Minute, "aaa"))
			}
			for _, id := range []string{"z1", "z2", "z3"} {
				jobs = append(jobs, queued(id, time.Minute, "zzz"))
			}
			// One runner is already live in pool aaa, busy on something else:
			// it holds a place under the ceiling though it serves none of the
			// queued jobs.
			busy := testRunner("run_busy", a, store.RunnerBusy, time.Minute)
			s := snap([]*store.Pool{a, z}, []*store.Runner{busy}, jobs, []*store.Host{testHost("host_a", 16, 1)})
			s.Policy.MaxRunners = tc.ceiling

			plan := Decide(s)
			if got := countOf(plan.Actions, ActionCreate); got != tc.wantCreates {
				t.Fatalf("creates = %d, want %d", got, tc.wantCreates)
			}
			named := false
			for _, pp := range plan.Pools {
				if strings.Contains(pp.Reason, "limits.runners") {
					named = true
				}
			}
			if named != tc.wantCeiling {
				t.Fatalf("a pool's reason names limits.runners = %v, want %v: %+v", named, tc.wantCeiling, plan.Pools)
			}
		})
	}
}
