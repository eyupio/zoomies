package agent

import (
	"context"
	"testing"
	"time"
)

func TestOrphanCleanupWaitsForInflightCreate(t *testing.T) {
	a, _, be, clock := newAgent(t, 2)
	a.polled.Store(true)
	w := running("sidecar", "runner-1")
	w.Sidecar = true
	be.setWorkloads(w)
	a.markOrphan(w.Handle, a.now())
	clock.advance(orphanGrace + time.Second)
	if !a.claim(w.RunnerID) {
		t.Fatal("could not claim create")
	}
	if _, err := a.ReconcileOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, _, removed := be.counts(); removed != 0 {
		t.Fatal("reaped sidecar while its runner was still being created")
	}
	a.release(w.RunnerID)
	if _, err := a.ReconcileOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, _, removed := be.counts(); removed != 1 {
		t.Fatal("abandoned sidecar was not cleaned after create finished")
	}
}
