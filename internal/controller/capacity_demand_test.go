package controller

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/eyupio/zoomies/internal/scheduler"
	"github.com/eyupio/zoomies/internal/store"
)

func TestCapacityDemandSignatureCooldownAndDuplicateTicks(t *testing.T) {
	h := newHarness(t)
	var mu sync.Mutex
	var bodies [][]byte
	var signatures []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		mu.Lock()
		bodies = append(bodies, body)
		signatures = append(signatures, r.Header.Get("X-Zoomies-Signature-256"))
		mu.Unlock()
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()
	h.cfg.CapacityDemand.DestinationURL = srv.URL
	h.cfg.CapacityDemand.SigningSecret = "receiver-secret"
	h.cfg.CapacityDemand.Cooldown = time.Hour
	h.cfg.CapacityDemand.Timeout = time.Second
	p := &store.Pool{ID: "pool-cap", Backend: store.BackendDocker, HostSelector: store.StringMap{"zone": "a"}}

	h.c.deliverCapacityEvent(h.ctx, p, 4, 2, 90, 2, capacityDemandEvent, false)
	h.c.deliveries.Wait()
	h.c.deliverCapacityEvent(h.ctx, p, 4, 2, 90, 2, capacityDemandEvent, false)
	h.c.deliveries.Wait()
	mu.Lock()
	defer mu.Unlock()
	if len(bodies) != 1 {
		t.Fatalf("requests = %d, want one deduplicated request", len(bodies))
	}
	mac := hmac.New(sha256.New, []byte("receiver-secret"))
	mac.Write(bodies[0])
	want := "sha256=" + hex.EncodeToString(mac.Sum(nil))
	if !hmac.Equal([]byte(signatures[0]), []byte(want)) {
		t.Fatalf("signature = %q, want %q", signatures[0], want)
	}
	// The receiver page tells its reader to check the schema version first and
	// to read current_capacity + required_runner_slots as the capacity the pool
	// should have, so those three fields are the contract this test holds.
	var got CapacityDemandEvent
	if err := json.Unmarshal(bodies[0], &got); err != nil {
		t.Fatalf("payload %s: %v", bodies[0], err)
	}
	if got.SchemaVersion != 1 || got.EventID == "" || got.Type != capacityDemandEvent {
		t.Fatalf("payload = %+v, want schema_version 1, an event id and type %q", got, capacityDemandEvent)
	}
	if got.CurrentCapacity+got.RequiredRunnerSlots != 6 {
		t.Fatalf("current_capacity %d + required_runner_slots %d = %d, want the 6 slots the pool should have", got.CurrentCapacity, got.RequiredRunnerSlots, got.CurrentCapacity+got.RequiredRunnerSlots)
	}
}

func TestCapacityDemandFailureAndRecovery(t *testing.T) {
	h := newHarness(t)
	now := time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC)
	h.c.clock = func() time.Time { return now }
	var mu sync.Mutex
	failing := true
	requests := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		requests++
		if failing {
			http.Error(w, "not now", http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()
	h.cfg.CapacityDemand.DestinationURL = srv.URL
	h.cfg.CapacityDemand.SigningSecret = "secret"
	h.cfg.CapacityDemand.Cooldown = time.Hour
	h.cfg.CapacityDemand.Timeout = time.Second
	p := &store.Pool{ID: "pool-recovery", Backend: store.BackendDocker}

	h.c.deliverCapacityEvent(h.ctx, p, 1, 1, 30, 1, capacityDemandEvent, false)
	h.c.deliveries.Wait()
	d, err := h.st.GetCapacityDemandDelivery(h.ctx, p.ID, capacityDemandEvent)
	if err != nil || d.DeliveredAt != nil || d.Attempts != 3 || d.LastError == "" {
		t.Fatalf("failed delivery = %+v, %v", d, err)
	}
	problems, err := h.c.Problems(h.ctx)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, problem := range problems {
		found = found || (problem.Code == "capacity_demand.delivery_failed" && problem.TargetID == p.ID)
	}
	if !found {
		t.Fatal("latest failed delivery was not surfaced in Problems")
	}

	mu.Lock()
	failing = false
	mu.Unlock()
	// A failed burst is also cooled down, preventing every reconciliation tick
	// (or a restart) from becoming a new delivery storm.
	h.c.deliverCapacityEvent(h.ctx, p, 1, 1, 30, 1, capacityDemandEvent, false)
	h.c.deliveries.Wait()
	mu.Lock()
	if requests != 3 {
		t.Fatalf("requests during cooldown = %d, want 3", requests)
	}
	mu.Unlock()
	now = now.Add(time.Hour)
	h.c.deliverCapacityEvent(h.ctx, p, 1, 1, 30, 1, capacityDemandEvent, false)
	h.c.deliveries.Wait()
	d, err = h.st.GetCapacityDemandDelivery(h.ctx, p.ID, capacityDemandEvent)
	if err != nil || d.DeliveredAt == nil || d.LastError != "" {
		t.Fatalf("recovered delivery = %+v, %v", d, err)
	}
	mu.Lock()
	defer mu.Unlock()
	if requests != 4 {
		t.Fatalf("requests = %d, want three bounded retries then one recovery", requests)
	}
}

func TestScaleDownRequiresSustainedExcessCapacity(t *testing.T) {
	h := newHarness(t)
	now := time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC)
	h.c.clock = func() time.Time { return now }
	requests := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { requests++; w.WriteHeader(204) }))
	defer srv.Close()
	h.cfg.CapacityDemand.DestinationURL = srv.URL
	h.cfg.CapacityDemand.SigningSecret = "secret"
	h.cfg.CapacityDemand.Cooldown = time.Minute
	h.cfg.CapacityDemand.Timeout = time.Second
	p := &store.Pool{ID: "pool-down", Backend: store.BackendDocker}

	h.c.deliverCapacityEvent(h.ctx, p, 8, 0, 0, -4, scaleDownEvent, true)
	h.c.deliveries.Wait()
	if requests != 0 {
		t.Fatal("scale-down emitted before sustained cooldown")
	}
	now = now.Add(time.Minute)
	h.c.deliverCapacityEvent(h.ctx, p, 8, 0, 0, -4, scaleDownEvent, true)
	h.c.deliveries.Wait()
	if requests != 1 {
		t.Fatalf("requests = %d, want one sustained scale-down opportunity", requests)
	}
}

// The request to the receiver used to run inside the reconcile pass, under
// its lock, so a receiver that accepted the connection and never answered
// held scaling for three attempts of the timeout. The pass now records the
// attempt and moves on; the request finishes on its own.
func TestASlowCapacityReceiverDoesNotHoldTheReconcilePass(t *testing.T) {
	h := newHarness(t)
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		<-release
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()
	h.cfg.CapacityDemand.DestinationURL = srv.URL
	h.cfg.CapacityDemand.SigningSecret = "secret"
	h.cfg.CapacityDemand.Cooldown = time.Hour
	h.cfg.CapacityDemand.Timeout = 10 * time.Second
	p := &store.Pool{ID: "pool-slow", Backend: store.BackendDocker}

	started := time.Now()
	h.c.deliverCapacityEvent(h.ctx, p, 1, 1, 30, 1, capacityDemandEvent, false)
	if took := time.Since(started); took > time.Second {
		t.Fatalf("deliverCapacityEvent took %s with the receiver hanging; the pass must not wait on it", took)
	}
	// The attempt is already on record, which is what stops the next pass
	// from sending a second request while this one is still in the air.
	d, err := h.st.GetCapacityDemandDelivery(h.ctx, p.ID, capacityDemandEvent)
	if err != nil || d.AttemptedAt == nil || d.DeliveredAt != nil {
		t.Fatalf("delivery before the receiver answered = %+v, %v; want the attempt recorded and no outcome yet", d, err)
	}
	h.c.deliverCapacityEvent(h.ctx, p, 1, 1, 30, 1, capacityDemandEvent, false)

	close(release)
	h.c.deliveries.Wait()
	d, err = h.st.GetCapacityDemandDelivery(h.ctx, p.ID, capacityDemandEvent)
	if err != nil || d.DeliveredAt == nil || d.Attempts != 1 {
		t.Fatalf("delivery after the receiver answered = %+v, %v; want one delivered attempt", d, err)
	}
}

// The one shortfall Zoomies never used to announce.
//
// A receiver exists to add hosts, and the case it most needs to hear about is
// the pool with queued work and no host that could run it at all -- scaling
// from zero. The publisher only ever fired on BlockedAtCapacity, which means
// "every host is busy", so a fleet with nothing to be busy said nothing.
func TestTheCapacityDemandEventFiresWhenNoHostCanEverRunThePool(t *testing.T) {
	h := newHarness(t)
	var mu sync.Mutex
	var bodies [][]byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		mu.Lock()
		bodies = append(bodies, body)
		mu.Unlock()
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()
	h.cfg.CapacityDemand.DestinationURL = srv.URL
	h.cfg.CapacityDemand.SigningSecret = "secret"
	h.cfg.CapacityDemand.Cooldown = time.Hour
	h.cfg.CapacityDemand.Timeout = time.Second

	p := &store.Pool{ID: "pool_zero", Name: "linux-x64", Backend: store.BackendDocker}
	snap := scheduler.Snapshot{Now: time.Now(), Pools: []*store.Pool{p}}
	plan := scheduler.Plan{Pools: []scheduler.PoolPlan{{
		PoolID:                p.ID,
		PoolName:              p.Name,
		Current:               0,
		Desired:               3,
		QueuedMatched:         3,
		BlockedNoEligibleHost: true,
	}}}

	h.c.publishCapacitySignals(h.ctx, snap, plan)
	h.c.deliveries.Wait()

	mu.Lock()
	defer mu.Unlock()
	if len(bodies) != 1 {
		t.Fatalf("requests = %d, want one demand event for a pool with nowhere to run", len(bodies))
	}
	var got CapacityDemandEvent
	if err := json.Unmarshal(bodies[0], &got); err != nil {
		t.Fatalf("payload %s: %v", bodies[0], err)
	}
	if got.Type != capacityDemandEvent {
		t.Fatalf("type = %q, want %q", got.Type, capacityDemandEvent)
	}
	// The event is a reading: an empty fleet has no capacity, and the three
	// slots it is missing are the three the pool should have.
	if got.CurrentCapacity != 0 || got.CurrentCapacity+got.RequiredRunnerSlots != 3 {
		t.Fatalf("current_capacity %d + required_runner_slots %d, want 0 + 3",
			got.CurrentCapacity, got.RequiredRunnerSlots)
	}
}

// And a pool that is blocked for neither reason still says nothing, so a
// receiver is not woken by a fleet that is merely waiting out a scale-up delay.
func TestNoCapacityDemandWithoutABlockage(t *testing.T) {
	h := newHarness(t)
	requests := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests++
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()
	h.cfg.CapacityDemand.DestinationURL = srv.URL
	h.cfg.CapacityDemand.SigningSecret = "secret"
	h.cfg.CapacityDemand.Cooldown = time.Hour
	h.cfg.CapacityDemand.Timeout = time.Second

	p := &store.Pool{ID: "pool_quiet", Name: "linux-x64", Backend: store.BackendDocker}
	h.c.publishCapacitySignals(h.ctx,
		scheduler.Snapshot{Now: time.Now(), Pools: []*store.Pool{p}},
		scheduler.Plan{Pools: []scheduler.PoolPlan{{PoolID: p.ID, PoolName: p.Name, Desired: 1, QueuedMatched: 1}}})
	h.c.deliveries.Wait()

	if requests != 0 {
		t.Fatalf("requests = %d, want none for a pool nothing is blocking", requests)
	}
}
