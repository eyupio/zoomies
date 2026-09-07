package agent

import (
	"os"
	"testing"

	"github.com/eyupio/zoomies/internal/backend"
	"github.com/eyupio/zoomies/internal/machine"
	"github.com/eyupio/zoomies/internal/store"
)

// The work directory is where a runner's checkout and its caches land, so it
// is that filesystem that decides whether a job has anywhere to go -- not the
// root one, and not the one the agent's binary happens to sit on.
func TestTheAgentMeasuresTheDiskItsRunnersWillUse(t *testing.T) {
	if _, _, ok := diskSpace(t.TempDir()); !ok {
		t.Skip("this platform has no portable way to measure a filesystem")
	}
	a := &Agent{opts: Options{WorkDir: t.TempDir()}}

	total, free := a.workDirSpace()

	if total <= 0 {
		t.Fatalf("total = %d MB, want the size of a filesystem that exists", total)
	}
	if free <= 0 || free > total {
		t.Fatalf("free = %d MB of %d MB total, which is not a filesystem", free, total)
	}
}

// A directory that is not there yet cannot be measured, and saying "zero" for
// it would read as a full disk. Nothing is the honest answer, and the
// controller keeps whatever it already knew.
func TestAWorkDirectoryThatIsNotThereMeasuresNothing(t *testing.T) {
	missing := t.TempDir() + string(os.PathSeparator) + "not-created-yet"
	a := &Agent{opts: Options{WorkDir: missing}}

	if total, free := a.workDirSpace(); total != 0 || free != 0 {
		t.Fatalf("measured %d MB total and %d MB free of a directory that does not exist", total, free)
	}
	// Asserted at the measurement too, not only after the conversion to
	// megabytes: a wrong answer smaller than a megabyte rounds to the same
	// zero as no answer, so the megabyte figures alone cannot tell them apart.
	if total, avail, ok := diskSpace(missing); ok {
		t.Fatalf("diskSpace reported %d/%d bytes for a directory that does not exist", avail, total)
	}
}

// A runner started through Docker or Podman is a sibling on the host, outside
// whatever cgroup the agent is held to. A containerised controller -- the usual
// deployment -- would otherwise describe a sixty-four core host as the two cores
// its own container may use, and the fleet would be placed at a fraction of its
// real size.
func TestAHostIsSizedByTheDaemonThatWillRunTheRunners(t *testing.T) {
	self := machine.Facts{CPUs: 2, MemoryMB: 2048}
	infos := []backend.Info{
		{Kind: store.BackendDocker, Available: true, CPUs: 64, MemoryMB: 262_144},
	}

	cpus, memoryMB := hostSize(infos, self)

	if cpus != 64 || memoryMB != 262_144 {
		t.Fatalf("host = %d cpus, %d MB; want the daemon's view of the machine", cpus, memoryMB)
	}
}

// Where no daemon answers, the agent's own view is the honest one: a runner the
// process backend starts is a child of the agent and is held to the same cgroup.
func TestAHostWithNoDaemonIsSizedByWhatTheAgentMayUse(t *testing.T) {
	self := machine.Facts{CPUs: 2, MemoryMB: 2048}

	for _, name := range []string{"no backends at all", "a backend that is not there"} {
		t.Run(name, func(t *testing.T) {
			var infos []backend.Info
			if name != "no backends at all" {
				// Unavailable, and reporting a machine anyway: a stale probe
				// must not size a host the agent cannot place anything on.
				infos = []backend.Info{
					{Kind: store.BackendDocker, Available: false, CPUs: 64, MemoryMB: 262_144},
				}
			}
			cpus, memoryMB := hostSize(infos, self)
			if cpus != 2 || memoryMB != 2048 {
				t.Fatalf("host = %d cpus, %d MB; want the agent's own view", cpus, memoryMB)
			}
		})
	}
}

// A daemon that says nothing about its machine -- an older one, or a backend
// that is not a container runtime -- leaves the agent's own view standing
// rather than zeroing the host.
func TestADaemonThatSaysNothingDoesNotShrinkTheHost(t *testing.T) {
	self := machine.Facts{CPUs: 8, MemoryMB: 16_384}
	infos := []backend.Info{{Kind: store.BackendDocker, Available: true}}

	cpus, memoryMB := hostSize(infos, self)

	if cpus != 8 || memoryMB != 16_384 {
		t.Fatalf("host = %d cpus, %d MB; a silent daemon must not shrink it", cpus, memoryMB)
	}
}
