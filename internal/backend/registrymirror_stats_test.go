package backend

import (
	"testing"

	"github.com/eyupio/zoomies/internal/registrymirror"
	"github.com/eyupio/zoomies/internal/store"
)

type mirrorStatsBackend struct {
	Backend
	kind  store.BackendKind
	stats *registrymirror.Stats
}

func (b mirrorStatsBackend) Kind() store.BackendKind                    { return b.kind }
func (b mirrorStatsBackend) RegistryMirrorStats() *registrymirror.Stats { return b.stats }

// A host that runs no mirror reports nothing at all, so its heartbeat is the one
// an agent without the feature would have sent, and a host that does run one
// reports what it has done.
func TestARegistryReportsTheMirrorOnlyFromABackendThatRunsOne(t *testing.T) {
	var none *Registry
	if got := none.RegistryMirrorStats(); got != nil {
		t.Errorf("a nil registry reported %+v", got)
	}
	if got := NewRegistry(plainBackend{kind: store.BackendProcess}).RegistryMirrorStats(); got != nil {
		t.Errorf("a backend with no mirror reported %+v", got)
	}
	// A backend whose mirror never started has nothing to report, and is not a
	// reason to stop looking: the first one that has started is the one reported.
	idle := NewRegistry(mirrorStatsBackend{kind: store.BackendDocker, stats: nil})
	if got := idle.RegistryMirrorStats(); got != nil {
		t.Errorf("a backend whose mirror never started reported %+v", got)
	}
	reg := NewRegistry(
		plainBackend{kind: store.BackendProcess},
		mirrorStatsBackend{kind: store.BackendDocker},
		mirrorStatsBackend{kind: store.BackendPodman, stats: &registrymirror.Stats{Requests: 9, BytesStored: 100}},
	)
	got := reg.RegistryMirrorStats()
	if got == nil || got.Requests != 9 || got.BytesStored != 100 {
		t.Errorf("registry stats = %+v, want the mirror that has started", got)
	}
}

// The heartbeat asks the backend through this, found by assertion, so a method
// that drifted would drop the block silently.
func TestTheContainerBackendsReportTheirMirror(t *testing.T) {
	var (
		_ RegistryMirrorStatser = (*DockerBackend)(nil)
		_ RegistryMirrorStatser = (*PodmanBackend)(nil)
	)
}
