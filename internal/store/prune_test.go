package store

import (
	"context"
	"testing"
	"time"
)

// Retention is the only thing standing between a controller and a database that
// grows for ever, and three of its five queries had no test at all: a cutoff
// comparison written the wrong way round deletes everything or nothing, and
// both look like a working fleet until somebody goes looking for last week.
//
// Each of these seeds one row either side of the cutoff and checks that exactly
// the old one goes, which is the whole contract: strictly older than the
// cutoff, and the count is what the caller logs.

func TestPruningScalingEventsKeepsTheOnesInsideTheWindow(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	_, pool, _ := seedPool(t, s)
	now := time.Now()

	old := &ScalingEvent{PoolID: pool.ID, PoolName: pool.Name, From: 0, To: 1,
		Reason: "old", CreatedAt: now.Add(-48 * time.Hour)}
	recent := &ScalingEvent{PoolID: pool.ID, PoolName: pool.Name, From: 1, To: 2,
		Reason: "recent", CreatedAt: now.Add(-time.Hour)}
	for _, e := range []*ScalingEvent{old, recent} {
		if err := s.AppendScalingEvent(ctx, e); err != nil {
			t.Fatalf("AppendScalingEvent: %v", err)
		}
	}

	n, err := s.PruneScalingEvents(ctx, now.Add(-24*time.Hour))
	if err != nil {
		t.Fatalf("PruneScalingEvents: %v", err)
	}
	if n != 1 {
		t.Errorf("pruned %d rows, want 1: the count is what the controller logs when somebody asks where their history went", n)
	}
	left, err := s.ListScalingEvents(ctx, "", 10)
	if err != nil {
		t.Fatalf("ListScalingEvents: %v", err)
	}
	if len(left) != 1 || left[0].Reason != "recent" {
		t.Fatalf("scaling events after the prune = %+v, want just the recent one", left)
	}
}

func TestPruningDeliveriesKeepsTheOnesInsideTheWindow(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	now := time.Now()

	for _, d := range []*WebhookDelivery{
		{DeliveryID: "old", Event: "workflow_job", Status: "accepted", ReceivedAt: now.Add(-48 * time.Hour)},
		{DeliveryID: "recent", Event: "workflow_job", Status: "accepted", ReceivedAt: now.Add(-time.Hour)},
	} {
		if err := s.RecordDelivery(ctx, d); err != nil {
			t.Fatalf("RecordDelivery: %v", err)
		}
	}

	n, err := s.PruneDeliveries(ctx, now.Add(-24*time.Hour))
	if err != nil {
		t.Fatalf("PruneDeliveries: %v", err)
	}
	if n != 1 {
		t.Errorf("pruned %d deliveries, want 1", n)
	}
	left, err := s.ListDeliveries(ctx, "", 10)
	if err != nil {
		t.Fatalf("ListDeliveries: %v", err)
	}
	if len(left) != 1 || left[0].DeliveryID != "recent" {
		t.Fatalf("deliveries after the prune = %+v, want just the recent one", left)
	}
}

func TestPruningSamplesKeepsTheOnesInsideTheWindow(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	now := time.Now()

	for _, at := range []time.Time{now.Add(-48 * time.Hour), now.Add(-time.Hour)} {
		if err := s.RecordSample(ctx, FleetSample{At: at, TotalRunners: 1}); err != nil {
			t.Fatalf("RecordSample: %v", err)
		}
	}

	n, err := s.PruneSamples(ctx, now.Add(-24*time.Hour))
	if err != nil {
		t.Fatalf("PruneSamples: %v", err)
	}
	if n != 1 {
		t.Errorf("pruned %d samples, want 1", n)
	}
	left, err := s.ListSamples(ctx, now.Add(-72*time.Hour))
	if err != nil {
		t.Fatalf("ListSamples: %v", err)
	}
	if len(left) != 1 {
		t.Fatalf("samples after the prune = %d, want 1: the sparkline is drawn from these, and a prune that took both would flatten it", len(left))
	}
	// Which one survived, not merely how many: counting alone passes just as
	// happily when the comparison is the wrong way round and the prune has kept
	// the ancient sample and deleted the recent one.
	if kept := left[0].At; kept.Before(now.Add(-24 * time.Hour)) {
		t.Errorf("the sample left behind is from %s, which is older than the cutoff: the prune kept the wrong side", kept.UTC())
	}
}
