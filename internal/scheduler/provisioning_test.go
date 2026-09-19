package scheduler

import (
	"github.com/eyupio/zoomies/internal/store"
	"testing"
	"time"
)

func TestProvisioningControlsAndLimits(t *testing.T) {
	p := testPool("linux", "linux")
	j := queued("job", time.Second, "linux")
	snap := Snapshot{Now: now, Pools: []*store.Pool{p}, Hosts: []*store.Host{testHost("host_a", 10, 0)}, Jobs: []*store.Job{j}, Policy: testPolicy()}
	snap.Policy.ScaleUpDelay = time.Minute
	createsFor := func() int {
		n := 0
		for _, p := range Decide(snap).Pools {
			n += creates(p.Actions)
		}
		return n
	}
	if n := createsFor(); n != 0 {
		t.Fatalf("delay bypassed: %d", n)
	}
	j.ProvisionNow = true
	if n := createsFor(); n != 1 {
		t.Fatalf("run now did not bypass delay: %d", n)
	}
	for _, state := range []string{"paused", "deleted"} {
		j.Provisioning = state
		if n := createsFor(); n != 0 {
			t.Fatalf("%s created %d", state, n)
		}
	}
	j.Provisioning = ""
	p.MaxRunners = 0
	if n := createsFor(); n != 0 {
		t.Fatalf("run now bypassed pool maximum: %d", n)
	}
	p.MaxRunners = 10
	snap.Hosts[0].Capacity = 0
	if n := createsFor(); n != 0 {
		t.Fatalf("run now bypassed host capacity: %d", n)
	}
}

func TestProvisioningFairnessAcrossPassesAndPriority(t *testing.T) {
	a, b := testPool("a", "a"), testPool("b", "b")
	ja, jb := queued("a", time.Minute, "a"), queued("b", time.Minute, "b")
	snap := Snapshot{Now: now, Pools: []*store.Pool{b, a}, Jobs: []*store.Job{jb, ja}, Hosts: []*store.Host{testHost("host_a", 10, 0)}, Policy: testPolicy(), LastProvisioned: map[string]time.Time{a.ID: ago(time.Second), b.ID: ago(time.Minute)}}
	snap.Policy.MaxCreatesPerTick = 1
	winner := func() string {
		for _, p := range Decide(snap).Pools {
			if creates(p.Actions) > 0 {
				return p.PoolID
			}
		}
		return ""
	}
	if got := winner(); got != b.ID {
		t.Fatalf("least recently served pool: %s", got)
	}
	snap.LastProvisioned[b.ID] = now
	if got := winner(); got != a.ID {
		t.Fatalf("next pass did not rotate: %s", got)
	}
	jb.ProvisionNow = true
	if got := winner(); got != b.ID {
		t.Fatalf("run now not prioritised: %s", got)
	}
	a.Priority = 10
	if got := winner(); got != a.ID {
		t.Fatalf("run now bypassed pool priority: %s", got)
	}
}

func TestProvisioningOrderingIsStable(t *testing.T) {
	a, b, c := queued("a", time.Minute), queued("b", time.Minute), queued("c", time.Second)
	c.ProvisionNow = true
	got := sortedJobs([]*store.Job{b, c, a})
	if got[0] != c || got[1] != a || got[2] != b {
		t.Fatalf("ordering: %+v", got)
	}
}

func TestExecutionOrderPreservesFairAllocation(t *testing.T) {
	a, b := testPool("a", "a"), testPool("b", "b")
	a.MinRunners, b.MinRunners = 2, 2
	snap := Snapshot{Now: now, Pools: []*store.Pool{a, b}, Hosts: []*store.Host{testHost("host", 10, 0)}, Policy: testPolicy(), LastProvisioned: map[string]time.Time{a.ID: now}}
	snap.Policy.MaxCreatesPerTick = 4
	plan := Decide(snap)
	want := []string{b.ID, a.ID, b.ID, a.ID}
	if len(plan.Actions) != len(want) {
		t.Fatalf("actions: %+v", plan.Actions)
	}
	for i, a := range plan.Actions {
		if a.PoolID != want[i] {
			t.Fatalf("action %d: %s, want %s", i, a.PoolID, want[i])
		}
	}
	if plan.Pools[0].PoolID != a.ID {
		t.Fatal("display order changed")
	}
	a.Priority = 10
	plan = Decide(snap)
	if plan.Actions[0].PoolID != a.ID || plan.Actions[1].PoolID != a.ID {
		t.Fatal("execution lost priority tiers")
	}
}
