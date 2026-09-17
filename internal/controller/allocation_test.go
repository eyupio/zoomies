package controller

import (
	"testing"

	"github.com/eyupio/zoomies/internal/agent"
	"github.com/eyupio/zoomies/internal/config"
	"github.com/eyupio/zoomies/internal/store"
)

// measuredHost seeds a host whose agent has measured the machine and whose
// daemon has said it can apply every limit: the host a default allocation is
// computed for.
func (h *harness) measuredHost(name string, cpus int, memoryMB int64, capacity int, limits store.LimitSupport) *store.Host {
	h.t.Helper()
	host := &store.Host{
		Name:     name,
		Capacity: capacity,
		Backends: store.StringSlice{"docker"},
		BackendInfo: store.HostBackends{
			{Kind: store.BackendDocker, Available: true, Version: "27.1.1", Limits: limits},
		},
		Labels:        store.StringMap{},
		OS:            "linux",
		Arch:          "amd64",
		CPUs:          cpus,
		MemoryMB:      memoryMB,
		DiskTotalMB:   500 * 1024,
		DiskFreeMB:    400 * 1024,
		LastHeartbeat: h.c.Now(),
	}
	if err := h.st.CreateHost(h.ctx, host); err != nil {
		h.t.Fatalf("CreateHost: %v", err)
	}
	return host
}

var enforcesEverything = store.LimitSupport{Known: true, CPU: true, Memory: true, Pids: true}

// A pool that sets no limits used to start runners with no limits at all, so
// eight of them on one host could each take every core. Now such a runner is
// given one slot's share of the host it lands on, the row says where the
// figure came from, and the task the agent receives carries the same share --
// the row and the container must never disagree about what a runner was
// given, because the row is what an operator reads after an OOM kill.
func TestAPoolWithNoLimitsGetsOneSlotsShareOfItsHost(t *testing.T) {
	h := newHarness(t)
	inst := h.installation()
	pool := h.pool(inst, "linux-x64")
	pool.MinRunners = 1
	if err := h.st.UpdatePool(h.ctx, pool); err != nil {
		t.Fatalf("UpdatePool: %v", err)
	}
	// Eight cores less the half-core floor is 7.5, and four slots of that is
	// 1.875, floored to the hundredth the pool form takes. Memory is the
	// machine less its reserve -- a twentieth of 16 GB, which is above the
	// flat floor -- in four.
	host := h.measuredHost("measured", 8, 16384, 4, enforcesEverything)

	if err := h.c.Reconcile(h.ctx); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	h.c.lifecycleCalls.Wait()
	r := h.onlyRunner()
	if r.AllocatedCPUs != 1.87 || r.AllocatedMemoryMB != 3891 || r.AllocationSource != store.AllocationFromHost {
		t.Fatalf("row allocation = %v CPUs, %d MB from %q; want 1.87, 3891 from %q",
			r.AllocatedCPUs, r.AllocatedMemoryMB, r.AllocationSource, store.AllocationFromHost)
	}
	task := h.taskOfKind(host.ID, agent.TaskCreateRunner)
	if task.Spec == nil {
		t.Fatal("the create task carries no spec")
	}
	if task.Spec.Resources.CPUs != 1.87 || task.Spec.Resources.MemoryMB != 3891 || task.Spec.ResourcesSource != store.AllocationFromHost {
		t.Fatalf("task spec = %+v from %q; the agent would apply something other than what the row records",
			task.Spec.Resources, task.Spec.ResourcesSource)
	}
	view := h.c.runnerView(h.ctx, r)
	if view.AllocatedCPUs != 1.87 || view.AllocatedMemoryMB != 3891 || view.AllocationSource != store.AllocationFromHost {
		t.Fatalf("runner view = %v CPUs, %d MB from %q; the Runners page cannot say what the runner was given",
			view.AllocatedCPUs, view.AllocatedMemoryMB, view.AllocationSource)
	}
}

// With defaults switched off the fleet behaves exactly as it did before they
// existed: a pool's own limits are the whole answer, and a pool with none
// starts an unlimited runner whose row says nothing about an allocation.
func TestDefaultLimitsOffLeavesARunnerUnlimited(t *testing.T) {
	h := newHarness(t)
	h.c.UpdateConfig(func(cfg *config.Config) { cfg.Scheduler.DefaultRunnerLimits = false })
	inst := h.installation()
	pool := h.pool(inst, "linux-x64")
	pool.MinRunners = 1
	if err := h.st.UpdatePool(h.ctx, pool); err != nil {
		t.Fatalf("UpdatePool: %v", err)
	}
	host := h.measuredHost("measured", 8, 16384, 4, enforcesEverything)

	if err := h.c.Reconcile(h.ctx); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	h.c.lifecycleCalls.Wait()
	r := h.onlyRunner()
	if r.AllocatedCPUs != 0 || r.AllocatedMemoryMB != 0 || r.AllocationSource != "" {
		t.Fatalf("row allocation = %v CPUs, %d MB from %q; want none with defaults off",
			r.AllocatedCPUs, r.AllocatedMemoryMB, r.AllocationSource)
	}
	task := h.taskOfKind(host.ID, agent.TaskCreateRunner)
	if task.Spec.Resources != (store.Resources{}) || task.Spec.ResourcesSource != "" {
		t.Fatalf("task spec = %+v from %q; want no limits with defaults off", task.Spec.Resources, task.Spec.ResourcesSource)
	}
	// And the host counts it among the runners nothing binds, which is the
	// count the CPU hold reads before it means anything.
	got, err := h.st.GetHost(h.ctx, host.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.UnlimitedRunners != 1 || h.c.HostView(got).UnlimitedRunners != 1 {
		t.Fatalf("unlimited runners = %d (view %d), want 1", got.UnlimitedRunners, h.c.HostView(got).UnlimitedRunners)
	}
}

// A pool's own limits win over the default, and the row says they are the
// pool's: an operator who set memory_mb and sees an OOM kill should be sent
// to the pool, not to the host's capacity.
func TestAPoolsOwnLimitsAreRecordedAsThePools(t *testing.T) {
	h := newHarness(t)
	inst := h.installation()
	pool := h.pool(inst, "linux-x64")
	pool.MinRunners = 1
	pool.Resources = store.Resources{CPUs: 2, MemoryMB: 4096}
	if err := h.st.UpdatePool(h.ctx, pool); err != nil {
		t.Fatalf("UpdatePool: %v", err)
	}
	h.measuredHost("measured", 8, 16384, 4, enforcesEverything)

	if err := h.c.Reconcile(h.ctx); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	r := h.onlyRunner()
	if r.AllocatedCPUs != 2 || r.AllocatedMemoryMB != 4096 || r.AllocationSource != store.AllocationFromPool {
		t.Fatalf("row allocation = %v CPUs, %d MB from %q; want the pool's 2 and 4096 from %q",
			r.AllocatedCPUs, r.AllocatedMemoryMB, r.AllocationSource, store.AllocationFromPool)
	}
}
