package agent

import (
	"context"
	"testing"

	"github.com/eyupio/zoomies/internal/backend"
	"github.com/eyupio/zoomies/internal/cacheservice"
)

// cachingBackend is a fake backend that also runs cache services, so the beat
// can be shown to ask the backend rather than to carry a figure of its own.
type cachingBackend struct {
	*fakeBackend
	stats []backend.CacheStats
}

func (c *cachingBackend) CacheStats() []backend.CacheStats { return c.stats }

// The figures a host's cache services keep reach the controller on the
// heartbeat, as the backend reports them at that beat, and a host whose
// backends run no service sends a beat with no block at all.
func TestTheHeartbeatCarriesWhatTheBackendsCacheServicesHaveDone(t *testing.T) {
	plain, tr, _, _ := newAgent(t, 2)
	if err := plain.Join(context.Background(), "join-token"); err != nil {
		t.Fatalf("Join: %v", err)
	}
	if beat := nextBeat(t, plain, tr); beat.Caches != nil {
		t.Fatalf("a host with no cache service reported %+v", beat.Caches)
	}

	be := &cachingBackend{fakeBackend: newFakeBackend("docker")}
	a, tr, _, _ := newAgent(t, 2, func(o *Options) { o.Backends = backend.NewRegistry(be) })
	if err := a.Join(context.Background(), "join-token"); err != nil {
		t.Fatalf("Join: %v", err)
	}
	be.stats = []backend.CacheStats{{PoolID: "pool_1", Stats: cacheservice.Stats{RestoreHits: 4, Entries: 2}}}
	first := nextBeat(t, a, tr)
	if len(first.Caches) != 1 || first.Caches[0].PoolID != "pool_1" || first.Caches[0].RestoreHits != 4 {
		t.Fatalf("caches = %+v, want pool_1 with 4 hits", first.Caches)
	}

	// Read afresh each beat: the counters move between them.
	be.stats = []backend.CacheStats{{PoolID: "pool_1", Stats: cacheservice.Stats{RestoreHits: 9, Entries: 2}}}
	if second := nextBeat(t, a, tr); second.Caches[0].RestoreHits != 9 {
		t.Errorf("second beat carried %+v, want the figures as they are now", second.Caches)
	}
}
