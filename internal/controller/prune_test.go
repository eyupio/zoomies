package controller

import (
	"testing"
	"time"

	"github.com/eyupio/zoomies/internal/config"
	"github.com/eyupio/zoomies/internal/events"
	"github.com/eyupio/zoomies/internal/store"
)

// The prune loop is the only caller of five delete queries, and its shape
// carries two rules that live nowhere else: a zero window means keep
// everything, and a pruned runner is announced so the Runners page stops
// showing rows that no longer exist. Neither was tested, and both fail
// silently: history that quietly disappears looks like history nobody wrote,
// and a stale page looks like a page nobody reloaded.
func TestPruningHonoursEachWindowAndKeepsEverythingWhenOneIsZero(t *testing.T) {
	h := newHarness(t)
	_, pool, _ := h.fleet()
	now := h.c.Now()

	seed := func() {
		t.Helper()
		if err := h.st.AppendScalingEvent(h.ctx, &store.ScalingEvent{
			PoolID: pool.ID, PoolName: pool.Name, To: 1, Reason: "old",
			CreatedAt: now.Add(-48 * time.Hour),
		}); err != nil {
			t.Fatalf("AppendScalingEvent: %v", err)
		}
		if err := h.st.RecordSample(h.ctx, store.FleetSample{At: now.Add(-48 * time.Hour), TotalRunners: 1}); err != nil {
			t.Fatalf("RecordSample: %v", err)
		}
		if err := h.st.RecordDelivery(h.ctx, &store.WebhookDelivery{
			DeliveryID: "old", Event: "workflow_job", Status: "accepted",
			ReceivedAt: now.Add(-48 * time.Hour),
		}); err != nil {
			t.Fatalf("RecordDelivery: %v", err)
		}
	}

	// A cleared window means "keep everything", which is what an operator who
	// emptied the setting meant -- not "delete everything older than now".
	h.c.UpdateConfig(func(c *config.Config) {
		c.Retention = config.Retention{}
	})
	seed()
	h.c.prune(h.ctx)

	events, err := h.st.ListScalingEvents(h.ctx, "", 10)
	if err != nil {
		t.Fatalf("ListScalingEvents: %v", err)
	}
	samples, err := h.st.ListSamples(h.ctx, now.Add(-72*time.Hour))
	if err != nil {
		t.Fatalf("ListSamples: %v", err)
	}
	deliveries, err := h.st.ListDeliveries(h.ctx, "", 10)
	if err != nil {
		t.Fatalf("ListDeliveries: %v", err)
	}
	if len(events) != 1 || len(samples) != 1 || len(deliveries) != 1 {
		t.Fatalf("a zero window deleted history: %d scaling events, %d samples, %d deliveries left, want 1 of each",
			len(events), len(samples), len(deliveries))
	}

	// And a window that is set takes exactly what is older than it.
	h.c.UpdateConfig(func(c *config.Config) {
		c.Retention = config.Retention{
			ScalingEvents: 24 * time.Hour, Samples: 24 * time.Hour, Webhooks: 24 * time.Hour,
			Jobs: 24 * time.Hour, Runners: 24 * time.Hour,
		}
	})
	h.c.prune(h.ctx)

	if events, err = h.st.ListScalingEvents(h.ctx, "", 10); err != nil || len(events) != 0 {
		t.Errorf("scaling events after the prune = %d (err %v), want none: they follow the scaling-events window", len(events), err)
	}
	if samples, err = h.st.ListSamples(h.ctx, now.Add(-72*time.Hour)); err != nil || len(samples) != 0 {
		t.Errorf("samples after the prune = %d (err %v), want none", len(samples), err)
	}
	if deliveries, err = h.st.ListDeliveries(h.ctx, "", 10); err != nil || len(deliveries) != 0 {
		t.Errorf("deliveries after the prune = %d (err %v), want none", len(deliveries), err)
	}
}

// Every pruned runner is announced. The delete happens in the database, which
// tells nobody, so without this the Runners page keeps showing runners that
// were deleted an hour ago until somebody reloads it.
func TestAPrunedRunnerIsAnnounced(t *testing.T) {
	h := newHarness(t)
	_, pool, host := h.fleet()
	r := h.runnerRow(pool, host, store.RunnerIdle)
	if _, err := h.st.TransitionRunner(h.ctx, r.ID, store.RunnerRemoved, "removed"); err != nil {
		t.Fatalf("TransitionRunner: %v", err)
	}

	sub := h.listen(events.KindRunnerDeleted)

	// An hour of retention and an hour on the clock: the row is old enough to
	// go without the test having to wait for it.
	h.c.UpdateConfig(func(c *config.Config) {
		c.Retention = config.Retention{Runners: time.Hour}
	})
	h.advance(2 * time.Hour)
	h.c.prune(h.ctx)

	select {
	case <-sub.C:
	case <-time.After(2 * time.Second):
		t.Fatal("the pruned runner was never announced; every open Runners page still shows it")
	}
}

// A prune that takes thousands of rows must not take every open tab with it.
//
// A subscriber's queue is 256 deep and the bus drops a subscriber that falls
// behind. The hourly prune deletes everything past the retention window in one
// pass, and announcing each row filled that queue several times over: every
// tab was cut off, showed itself as disconnected, reconnected and refetched six
// endpoints. The storm was the announcement rather than the deletion, so the
// announcement is bounded.
func TestABulkPruneAnnouncesOnceRatherThanDroppingEverySubscriber(t *testing.T) {
	h := newHarness(t)
	_, pool, host := h.fleet()

	for i := range announceEach + 5 {
		r := h.runnerRow(pool, host, store.RunnerIdle)
		if _, err := h.st.TransitionRunner(h.ctx, r.ID, store.RunnerRemoved, "removed"); err != nil {
			t.Fatalf("TransitionRunner %d: %v", i, err)
		}
	}

	sub := h.listen(events.KindRunnerDeleted, events.KindResync)
	h.c.UpdateConfig(func(c *config.Config) {
		c.Retention = config.Retention{Runners: time.Hour}
	})
	h.advance(2 * time.Hour)
	h.c.prune(h.ctx)

	// One frame, and it is the one that means "fetch the resources again".
	select {
	case ev := <-sub.C:
		if ev.Kind != events.KindResync {
			t.Fatalf("the first frame was %q; a row-by-row announcement of a bulk prune fills every subscriber's queue and cuts it off", ev.Kind)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("a bulk prune announced nothing at all, so every open page keeps showing rows that are gone")
	}
	select {
	case ev := <-sub.C:
		t.Fatalf("a second frame (%q) followed the resync; one is the whole point", ev.Kind)
	case <-time.After(200 * time.Millisecond):
	}
}
