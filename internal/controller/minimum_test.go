package controller

import (
	"testing"

	"github.com/eyupio/zoomies/internal/config"
	"github.com/eyupio/zoomies/internal/store"
)

// The rule a pool's minimum in force follows, field by field. A pool's own
// figure always wins; a zero follows the fleet, unless the fleet's figure is
// at or above the pool's typed standard, where a minimum means nothing and
// following it would only have made the pool's size a lie.
func TestAPoolsMinimumFollowsTheFleetWhereItSetsNone(t *testing.T) {
	fleet := config.Runners{MinimumCPUs: 2, MinimumMemoryMB: 4096}
	cases := []struct {
		name       string
		pool       store.Resources
		fleet      config.Runners
		wantCPUs   float64
		wantMemory int64
	}{
		{name: "an automatic pool with none takes the fleet's", fleet: fleet, wantCPUs: 2, wantMemory: 4096},
		{name: "a fixed pool with none takes the fleet's below its standard",
			pool: store.Resources{CPUs: 8, MemoryMB: 16384}, fleet: fleet, wantCPUs: 2, wantMemory: 4096},
		{name: "a pool's own minimum wins",
			pool:  store.Resources{CPUs: 8, MemoryMB: 16384, MinCPUs: 6, MinMemoryMB: 8192},
			fleet: fleet, wantCPUs: 6, wantMemory: 8192},
		{name: "the two fields are independent",
			pool:  store.Resources{MinMemoryMB: 1024},
			fleet: fleet, wantCPUs: 2, wantMemory: 1024},
		{name: "a fleet minimum at the standard is ignored",
			pool: store.Resources{CPUs: 2, MemoryMB: 4096}, fleet: fleet, wantCPUs: 0, wantMemory: 0},
		{name: "a fleet minimum above the standard is ignored",
			pool: store.Resources{CPUs: 1, MemoryMB: 2048}, fleet: fleet, wantCPUs: 0, wantMemory: 0},
		{name: "a fleet with no minimum changes nothing", pool: store.Resources{CPUs: 8}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cpus, memoryMB := EffectiveMinimum(tc.pool, tc.fleet)
			if cpus != tc.wantCPUs || memoryMB != tc.wantMemory {
				t.Errorf("EffectiveMinimum(%+v) = %v cores, %d MB; want %v, %d",
					tc.pool, cpus, memoryMB, tc.wantCPUs, tc.wantMemory)
			}
		})
	}
}

// The fleet's minimum is read on every pass, not copied into the pool: a pool
// that set none waits for a whole standard while the fleet has none, is placed
// reduced on the pass after an operator sets one, and its stored row still
// says zero -- which is what keeps it following the next change too.
func TestTheFleetMinimumPlacesAPoolReducedOnTheNextPassWithoutRewritingIt(t *testing.T) {
	h := newHarness(t)
	inst := h.installation()
	pool := h.pool(inst, "linux-x64")
	pool.MinRunners = 1
	pool.Resources = store.Resources{CPUs: 2, MemoryMB: 32 * 1024}
	if err := h.st.UpdatePool(h.ctx, pool); err != nil {
		t.Fatalf("UpdatePool: %v", err)
	}
	// 16 GB less its reserve is well short of 32 GB and well above 8.
	h.measuredHost("short", 8, 16384, 1, enforcesEverything)

	if err := h.c.Reconcile(h.ctx); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	h.c.lifecycleCalls.Wait()
	if rs, _ := h.st.ListRunnersForPool(h.ctx, pool.ID); len(rs) != 0 {
		t.Fatalf("placed %d runners with no minimum anywhere; want none", len(rs))
	}

	h.c.UpdateConfig(func(c *config.Config) { c.Runners.MinimumMemoryMB = 8 * 1024 })
	if err := h.c.Reconcile(h.ctx); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	h.c.lifecycleCalls.Wait()
	r := h.onlyRunner()
	if r.AllocationSource != store.AllocationReduced {
		t.Fatalf("allocation source = %q, want %q", r.AllocationSource, store.AllocationReduced)
	}
	if r.AllocatedMemoryMB >= 32*1024 || r.AllocatedMemoryMB < 8*1024 {
		t.Errorf("memory = %d MB, want between the fleet's 8 GB minimum and the 32 GB standard", r.AllocatedMemoryMB)
	}
	stored, err := h.st.GetPool(h.ctx, pool.ID)
	if err != nil {
		t.Fatalf("GetPool: %v", err)
	}
	if stored.Resources.MinCPUs != 0 || stored.Resources.MinMemoryMB != 0 {
		t.Errorf("stored minimum = %+v; the fleet's figure was frozen into the pool", stored.Resources)
	}
}

// A minimum typed on the pool is the operator's answer for that pool, and a
// fleet figure must not lower it: here the pool's own 24 GB is more than the
// short host can spare, so it stays queued even though the fleet would take 8.
func TestAPoolsOwnMinimumOverridesTheFleets(t *testing.T) {
	h := newHarness(t)
	h.c.UpdateConfig(func(c *config.Config) { c.Runners.MinimumMemoryMB = 8 * 1024 })
	inst := h.installation()
	pool := h.pool(inst, "linux-x64")
	pool.MinRunners = 1
	pool.Resources = store.Resources{CPUs: 2, MemoryMB: 32 * 1024, MinMemoryMB: 24 * 1024}
	if err := h.st.UpdatePool(h.ctx, pool); err != nil {
		t.Fatalf("UpdatePool: %v", err)
	}
	h.measuredHost("short", 8, 16384, 1, enforcesEverything)

	if err := h.c.Reconcile(h.ctx); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	h.c.lifecycleCalls.Wait()
	if rs, _ := h.st.ListRunnersForPool(h.ctx, pool.ID); len(rs) != 0 {
		t.Fatalf("placed %d runners below the pool's own 24 GB minimum; want none", len(rs))
	}
}

// A fleet minimum at or above a fixed pool's standard is not a minimum for that
// pool, so it must not make the pool reducible -- or the pool page would offer
// "lower this pool's minimum" for a figure nobody set on it.
func TestAFleetMinimumAtAPoolsStandardIsIgnored(t *testing.T) {
	h := newHarness(t)
	h.c.UpdateConfig(func(c *config.Config) { c.Runners.MinimumMemoryMB = 32 * 1024 })
	inst := h.installation()
	pool := h.pool(inst, "linux-x64")
	pool.MinRunners = 1
	pool.Resources = store.Resources{CPUs: 2, MemoryMB: 32 * 1024}
	if err := h.st.UpdatePool(h.ctx, pool); err != nil {
		t.Fatalf("UpdatePool: %v", err)
	}
	if sizingPool(pool, h.c.cfg().Runners).Resources.Reducible() {
		t.Fatal("the pool is reducible by a fleet minimum equal to its own standard")
	}
	h.measuredHost("short", 8, 16384, 1, enforcesEverything)
	if err := h.c.Reconcile(h.ctx); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	h.c.lifecycleCalls.Wait()
	if rs, _ := h.st.ListRunnersForPool(h.ctx, pool.ID); len(rs) != 0 {
		t.Fatalf("placed %d runners on a host short of the standard; want none", len(rs))
	}
}
