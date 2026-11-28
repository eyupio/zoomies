package agent

import (
	"context"
	"testing"

	"github.com/eyupio/zoomies/internal/backend"
	"github.com/eyupio/zoomies/internal/cacheservice"
	"github.com/eyupio/zoomies/internal/registrymirror"
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

type maintainingBackend struct {
	*fakeBackend
	policies []backend.CachePolicy
	passes   int
}

func (b *maintainingBackend) SetCachePolicies(p []backend.CachePolicy) { b.policies = p }
func (b *maintainingBackend) MaintainCaches(context.Context)           { b.passes++ }

func TestAHeartbeatRefreshesIdleCachePoliciesAndReconciliationRunsWithoutACreate(t *testing.T) {
	be := &maintainingBackend{fakeBackend: newFakeBackend("docker")}
	a, tr, _, _ := newAgent(t, 2, func(o *Options) { o.Backends = backend.NewRegistry(be) })
	if err := a.Join(context.Background(), "join-token"); err != nil {
		t.Fatal(err)
	}
	tr.beatResp = &HeartbeatResponse{OK: true, CachePolicies: []backend.CachePolicy{{PoolID: "pool_one"}}}
	nextBeat(t, a, tr)
	if len(be.policies) != 1 {
		t.Fatal("heartbeat policy did not reach the backend")
	}
	if _, err := a.ReconcileOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	if be.passes != 1 {
		t.Fatal("idle reconciliation did not maintain caches")
	}
	tr.beatResp = &HeartbeatResponse{OK: true, MutationsPaused: true}
	nextBeat(t, a, tr)
	if _, err := a.ReconcileOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	if be.passes != 1 || len(be.policies) != 0 {
		t.Fatal("fenced recovery allowed cache deletion")
	}
	tr.beatResp = &HeartbeatResponse{OK: true}
	nextBeat(t, a, tr)
	if len(be.policies) != 0 {
		t.Fatal("an older controller left a stale deletion policy")
	}
}

type mirroringBackend struct {
	*fakeBackend
	report *backend.RegistryMirrorReport
}

func (m *mirroringBackend) RegistryMirrorReport() *backend.RegistryMirrorReport { return m.report }

// What a host's registry mirror says about itself reaches the controller on the
// heartbeat as the backend reports it at that beat, and a host that runs no
// mirror sends a beat with no block, which an older controller reads the same
// way.
func TestTheHeartbeatCarriesWhatTheRegistryMirrorHasDone(t *testing.T) {
	plain, tr, _, _ := newAgent(t, 2)
	if err := plain.Join(context.Background(), "join-token"); err != nil {
		t.Fatalf("Join: %v", err)
	}
	if beat := nextBeat(t, plain, tr); beat.RegistryMirror != nil {
		t.Fatalf("a host with no mirror reported %+v", beat.RegistryMirror)
	}

	be := &mirroringBackend{fakeBackend: newFakeBackend("docker")}
	a, tr, _, _ := newAgent(t, 2, func(o *Options) { o.Backends = backend.NewRegistry(be) })
	if err := a.Join(context.Background(), "join-token"); err != nil {
		t.Fatalf("Join: %v", err)
	}
	if beat := nextBeat(t, a, tr); beat.RegistryMirror != nil {
		t.Fatalf("a mirror that is off reported %+v", beat.RegistryMirror)
	}
	be.report = &backend.RegistryMirrorReport{Stats: registrymirror.Stats{Requests: 4, Hits: 3, BytesStored: 2048}, Problem: backend.MirrorProblemFolder}
	first := nextBeat(t, a, tr)
	if first.RegistryMirror == nil || first.RegistryMirror.Requests != 4 || first.RegistryMirror.BytesStored != 2048 || first.RegistryMirror.Problem != backend.MirrorProblemFolder {
		t.Fatalf("registry mirror = %+v, want 4 requests, 2048 bytes held and the problem it named", first.RegistryMirror)
	}

	// Read afresh each beat: the counters move and a problem can end.
	be.report = &backend.RegistryMirrorReport{Stats: registrymirror.Stats{Requests: 11, Hits: 9, BytesStored: 4096}}
	if second := nextBeat(t, a, tr); second.RegistryMirror == nil || second.RegistryMirror.Requests != 11 || second.RegistryMirror.Problem != "" {
		t.Errorf("second beat carried %+v, want the report as it is now", second.RegistryMirror)
	}
}
