package controller

import (
	"github.com/eyupio/zoomies/internal/agent"
	"github.com/eyupio/zoomies/internal/store"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"testing"
	"time"
)

func TestLostLeaseStopsRunnerCreation(t *testing.T) {
	h := newHarness(t)
	h.fleet()
	h.deliverJob(jobEvent{Action: "queued", JobID: 1, Labels: []string{"self-hosted", "linux", "x64", "demo"}})
	h.c.leaseLost.Store(&store.ControllerLease{Holder: "rival"})
	if err := h.c.Reconcile(h.ctx); err != nil {
		t.Fatal(err)
	}
	h.c.lifecycleCalls.Wait()
	if len(h.runners()) != 0 {
		t.Fatal("created runner without authority")
	}
}
func TestRecoveryFenceStopsCleanupDispatch(t *testing.T) {
	h := newHarness(t)
	_, p, host := h.fleet()
	r := h.runnerRow(p, host, store.RunnerFailed)
	if err := h.st.RecordCleanupFailure(h.ctx, r.ID, "host unreachable"); err != nil {
		t.Fatal(err)
	}
	h.fence("restored")
	if err := h.c.Reconcile(h.ctx); err != nil {
		t.Fatal(err)
	}
	if h.hasTaskOfKind(host.ID, agent.TaskRemoveRunner) {
		t.Fatal("fenced controller dispatched removal")
	}
}
func TestRecoveryFenceWithholdsPreviouslyQueuedTasks(t *testing.T) {
	h := newHarness(t)
	_, _, host := h.fleet()
	h.c.enqueue(host.ID, agent.Task{Kind: agent.TaskRemoveRunner, RunnerID: "run_unknown"})
	h.fence("restored")
	b, err := h.c.PollTasks(h.ctx, host.ID, time.Millisecond)
	if err != nil || len(b.Tasks) != 0 {
		t.Fatalf("fenced poll=%v %v", b, err)
	}
	if err := h.c.Unfence(h.ctx); err != nil {
		t.Fatal(err)
	}
	b, err = h.c.PollTasks(h.ctx, host.ID, time.Millisecond)
	if err != nil || len(b.Tasks) != 1 {
		t.Fatalf("intent was lost: %v %v", b, err)
	}
}
func TestDuplicateCompletionCountsOnce(t *testing.T) {
	h := newHarness(t)
	_, p, _ := h.fleet()
	e := jobEvent{Action: "completed", JobID: 1, Conclusion: "success", Labels: []string{"self-hosted", "linux", "x64", "demo"}}
	h.deliverJob(e)
	h.deliverJob(e)
	if got := testutil.ToFloat64(h.c.metrics.jobsTotal.WithLabelValues(p.Name, "success")); got != 1 {
		t.Fatalf("completion count=%v", got)
	}
}
func TestExpiredAuthorityCannotAdmitWork(t *testing.T) {
	h := newHarness(t)
	h.c.lease = &store.ControllerLease{Holder: "ours", RenewedAt: h.c.Now().Add(-LeaseTTL)}
	if h.c.mayAct() {
		t.Fatal("expired lease admitted work")
	}
	at := h.c.Now()
	h.c.leaseRenewed.Store(&at)
	if !h.c.mayAct() {
		t.Fatal("renewed lease not honoured")
	}
}
