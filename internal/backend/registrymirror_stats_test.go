package backend

import (
	"testing"

	"github.com/eyupio/zoomies/internal/registrymirror"
	"github.com/eyupio/zoomies/internal/store"
)

type mirrorReportBackend struct {
	Backend
	kind   store.BackendKind
	report *RegistryMirrorReport
}

func (b mirrorReportBackend) Kind() store.BackendKind                     { return b.kind }
func (b mirrorReportBackend) RegistryMirrorReport() *RegistryMirrorReport { return b.report }

// A host that runs no mirror reports nothing at all, so its heartbeat is the one
// an agent without the feature would have sent, and a host that does run one
// reports what it has done and what is wrong with it.
func TestARegistryReportsTheMirrorOnlyFromABackendThatRunsOne(t *testing.T) {
	var none *Registry
	if got := none.RegistryMirrorReport(); got != nil {
		t.Errorf("a nil registry reported %+v", got)
	}
	if got := NewRegistry(plainBackend{kind: store.BackendProcess}).RegistryMirrorReport(); got != nil {
		t.Errorf("a backend with no mirror reported %+v", got)
	}
	// A backend whose mirror is off has nothing to report, and is not a reason to
	// stop looking: the first one that reports is the one reported.
	idle := NewRegistry(mirrorReportBackend{kind: store.BackendDocker, report: nil})
	if got := idle.RegistryMirrorReport(); got != nil {
		t.Errorf("a backend whose mirror is off reported %+v", got)
	}
	reg := NewRegistry(
		plainBackend{kind: store.BackendProcess},
		mirrorReportBackend{kind: store.BackendDocker},
		mirrorReportBackend{kind: store.BackendPodman, report: &RegistryMirrorReport{
			Stats: registrymirror.Stats{Requests: 9, BytesStored: 100}, Problem: MirrorProblemListener,
		}},
	)
	got := reg.RegistryMirrorReport()
	if got == nil || got.Requests != 9 || got.BytesStored != 100 || got.Problem != MirrorProblemListener {
		t.Errorf("registry report = %+v, want the mirror that reported", got)
	}
}

// The heartbeat asks the backend through this, found by assertion, so a method
// that drifted would drop the block silently.
func TestTheContainerBackendsReportTheirMirror(t *testing.T) {
	var (
		_ RegistryMirrorReporter = (*DockerBackend)(nil)
		_ RegistryMirrorReporter = (*PodmanBackend)(nil)
	)
}
