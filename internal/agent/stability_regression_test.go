package agent

import (
	"context"
	"errors"
	"github.com/eyupio/zoomies/internal/store"
	"testing"
	"time"
)

func TestMissingWorkloadObservationSurvivesFailedDelivery(t *testing.T) {
	a, tr, _, clock := newAgent(t, 2)
	track(a, "runner-review", "wl-review", true)
	clock.advance(missingGrace + time.Second)
	reports, err := a.ReconcileOnce(context.Background())
	if err != nil || len(reports) != 1 {
		t.Fatalf("reports=%v err=%v", reports, err)
	}
	tr.reportErr = errors.New("temporary partition")
	a.sendReports(context.Background(), reports)
	again, err := a.ReconcileOnce(context.Background())
	if err != nil || len(again) != 1 {
		t.Fatalf("terminal observation lost: %v %v", again, err)
	}
	tr.reportErr = nil
	a.sendReports(context.Background(), again)
	a.ReconcileOnce(context.Background())
	if len(a.Runners()) != 0 {
		t.Fatal("acknowledged missing workload still tracked")
	}
}
func TestFailedStartupInventoryAdoptsBeforeOrphanCleanup(t *testing.T) {
	a, _, be, clock := newAgent(t, 2)
	be.setWorkloads(running("wl-review", "runner-review"))
	be.listErr = errors.New("daemon restarting")
	a.adoptExisting(context.Background())
	be.listErr = nil
	a.polled.Store(true)
	a.ReconcileOnce(context.Background())
	clock.advance(orphanGrace + time.Second)
	a.ReconcileOnce(context.Background())
	if _, _, removed := be.counts(); removed != 0 {
		t.Fatalf("removed %d live workloads", removed)
	}
	if len(a.Runners()) != 1 {
		t.Fatal("recovered inventory was not adopted")
	}
}
func TestExpiredBoostRetriesFailedWithdrawal(t *testing.T) {
	a, tr, be, _ := newAgent(t, 4)
	createdRunner(a, "run_a", "wl-a", store.Resources{CPUs: 2})
	beat(t, a, tr, be, &ThrottleDirective{CPUFactor: 1}, ElasticCPUDirective{RunnerID: "run_a", CPUFactor: 2, BaseCPUs: 2, TargetCPUs: 4})
	tr.beatErr = errors.New("controller unreachable")
	be.updateErr = errors.New("daemon unavailable")
	for range boostExpiryMisses {
		_ = a.heartbeat(context.Background())
	}
	be.updateErr = nil
	before := len(be.resourceUpdates())
	_ = a.heartbeat(context.Background())
	if len(be.resourceUpdates()) != before+1 {
		t.Fatal("failed withdrawal not retried")
	}
	if got := be.resourceUpdates()[before].res.CPUs; got != 2 {
		t.Fatalf("restored quota=%v", got)
	}
}

func TestBoostWatchdogWithdrawsWithoutHeartbeatCompletion(t *testing.T) {
	a, tr, be, _ := newAgent(t, 4)
	createdRunner(a, "run_a", "wl-a", store.Resources{CPUs: 2})
	beat(t, a, tr, be, &ThrottleDirective{CPUFactor: 1}, ElasticCPUDirective{RunnerID: "run_a", CPUFactor: 2, BaseCPUs: 2, TargetCPUs: 4})
	a.mu.Lock()
	expires := a.boostRenewedAt.Add(boostExpiryMisses * a.heartbtI)
	a.mu.Unlock()
	be.updateErr = errors.New("temporary quota failure")
	a.expireBoostLease(context.Background(), expires)
	be.updateErr = nil
	a.expireBoostLease(context.Background(), expires.Add(time.Second))
	updates := be.resourceUpdates()
	if len(updates) == 0 || updates[len(updates)-1].res.CPUs != 2 {
		t.Fatalf("expired CPU loan retained: %v", updates)
	}
}
