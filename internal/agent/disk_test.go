package agent

import (
	"context"
	"math"
	"os"
	"testing"
	"time"

	"github.com/eyupio/zoomies/internal/backend"
	"github.com/eyupio/zoomies/internal/machine"
	"github.com/eyupio/zoomies/internal/store"
)

// The work directory is where a runner's checkout and its caches land, so it
// is that filesystem that decides whether a job has anywhere to go -- not the
// root one, and not the one the agent's binary happens to sit on.
//
// The figures are asserted against the bytes they are converted from rather
// than for being plausible. "Positive, and free no larger than total" holds
// whatever the unit is, so it would have passed just as well on raw bytes --
// and the columns these land in are called disk_total_mb and disk_free_mb, the
// controller's write-suppression floor is denominated in MB, and a 1,048,576x
// slip would read a 250 GB volume as 250 PB.
func TestTheAgentMeasuresTheDiskItsRunnersWillUse(t *testing.T) {
	dir := t.TempDir()
	rawTotal, rawAvail, ok := diskSpace(dir)
	if !ok {
		t.Skip("this platform has no portable way to measure a filesystem")
	}
	a := &Agent{opts: Options{WorkDir: dir}}

	total, free := a.workDirSpace()

	const mb = 1 << 20
	if total != rawTotal/mb {
		t.Errorf("total = %d, want %d: the figure is reported in MB, not bytes", total, rawTotal/mb)
	}
	// Free space moves under us -- anything else on this machine may write
	// between the two measurements -- so it is checked for being the same
	// quantity rather than the same number. A unit slip is six orders of
	// magnitude and nowhere near this tolerance.
	if delta := free - rawAvail/mb; delta > 64 || delta < -64 {
		t.Errorf("free = %d MB, want about %d MB", free, rawAvail/mb)
	}
	if free <= 0 || free > total {
		t.Errorf("free = %d MB of %d MB total, which is not a filesystem", free, total)
	}
}

// And it is that filesystem, not whichever one the process happens to be
// standing on. A work directory is very often a dedicated data volume -- it is
// the reason the setting exists -- so measuring the root filesystem instead
// would report a number that has nothing to do with where the jobs will run.
func TestTheAgentMeasuresTheWorkDirectorysOwnFilesystem(t *testing.T) {
	// A second mount to tell the two apart. /dev/shm is a tmpfs on Linux and
	// is sized quite differently from the root filesystem, which is what makes
	// it usable as the distinguishing one.
	const other = "/dev/shm"
	if _, err := os.Stat(other); err != nil {
		t.Skip("no second filesystem to distinguish the work directory from")
	}
	elsewhere, _, ok := diskSpace(other)
	if !ok {
		t.Skip("this platform has no portable way to measure a filesystem")
	}
	root, _, ok := diskSpace(string(os.PathSeparator))
	if !ok || root == elsewhere {
		t.Skip("the two candidate paths are the same filesystem, so this proves nothing")
	}

	dir, err := os.MkdirTemp(other, "zoomies-workdir-")
	if err != nil {
		t.Skipf("cannot create a directory on %s: %v", other, err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })

	a := &Agent{opts: Options{WorkDir: dir}}
	total, _ := a.workDirSpace()

	const mb = 1 << 20
	if total != elsewhere/mb {
		t.Errorf("total = %d MB, want %s's %d MB rather than the root filesystem's %d MB",
			total, other, elsewhere/mb, root/mb)
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

// The arithmetic on its own, because the figures that break it cannot be
// staged on a real filesystem: a block count wide enough to overflow a signed
// multiply, or an available count above the total. Without this every check in
// diskFromStatfs would be a line nothing could ever fail.
func TestWhatCannotBeAFilesystemIsRefused(t *testing.T) {
	const tb = 1 << 40
	tests := []struct {
		name           string
		blocks, bavail uint64
		bsize          int64
		total, avail   int64
		ok             bool
	}{
		{"an ordinary disk", 66_053_021, 5_419_506, 4096, 270_553_174_016, 22_198_296_576, true},
		{"a full disk", 1000, 0, 4096, 4_096_000, 0, true},
		{"no block size at all", 1000, 500, 0, 0, 0, false},
		{"a negative block size", 1000, 500, -4096, 0, 0, false},
		{"a block count that overflows the multiply", math.MaxUint64, 1, 4096, 0, 0, false},
		// The one that matters: this wraps to 4096 bytes, which passes every
		// check on the result. A four-petabyte volume would be reported as a
		// four-kilobyte one and every host on it declared out of room.
		{"an overflow that wraps to a plausible size", (1 << 52) + 1, 1, 4096, 0, 0, false},
		{"an available count that overflows the multiply", 1000, math.MaxUint64, 4096, 0, 0, false},
		{"more available than exists", 100, 200, 4096, 0, 0, false},
		{"a filesystem of no size", 0, 0, 4096, 0, 0, false},
		{"a large but honest disk", tb, tb / 2, 1, tb, tb / 2, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			total, avail, ok := diskFromStatfs(tc.blocks, tc.bavail, tc.bsize)
			if ok != tc.ok || total != tc.total || avail != tc.avail {
				t.Fatalf("diskFromStatfs(%d, %d, %d) = %d, %d, %v; want %d, %d, %v",
					tc.blocks, tc.bavail, tc.bsize, total, avail, ok, tc.total, tc.avail, tc.ok)
			}
		})
	}
}

// TestTheAgentReportsTheHostItCanActuallyFill is the wiring between measuring a
// host and telling the controller about it.
//
// hostSize and workDirSpace were each tested as functions, and both requests
// were built by hand in every other test, so nothing connected the two: the two
// lines that put the figures on the wire could be deleted and the whole suite
// stayed green. A containerised agent would then go back to reporting its
// two-core cgroup share for a sixty-four-core host, and the fleet would be
// placed at a fraction of its size.
func TestTheAgentReportsTheHostItCanActuallyFill(t *testing.T) {
	tr := newFakeTransport()
	be := newFakeBackend(store.BackendDocker)
	// The agent is in a small container; the daemon it drives is on the real
	// machine. A Docker runner is a sibling on that machine rather than a
	// child in this cgroup, so the daemon's figures are the ones that decide
	// what can be placed here.
	be.cpus, be.memoryMB = 64, 262_144
	workDir := t.TempDir()

	a, err := New(Options{
		Name:              "vm-1",
		WorkDir:           workDir,
		Capacity:          2,
		Backends:          backend.NewRegistry(be),
		DefaultBackend:    store.BackendDocker,
		Transport:         tr,
		HeartbeatInterval: time.Second,
		Logger:            testLogger(),
		Machine:           &machine.Facts{OS: "linux", Arch: "amd64", CPUs: 2, MemoryMB: 2048},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	if err := a.Join(context.Background(), "join-token"); err != nil {
		t.Fatalf("Join: %v", err)
	}
	if tr.joinReq == nil {
		t.Fatal("the agent sent no join request")
	}
	if tr.joinReq.CPUs != 64 || tr.joinReq.MemoryMB != 262_144 {
		t.Errorf("join reported %d CPUs and %d MB; the agent sent its own cgroup share rather than the host",
			tr.joinReq.CPUs, tr.joinReq.MemoryMB)
	}

	wantTotal, wantFree := a.workDirSpace()
	if wantTotal == 0 {
		t.Skip("this platform has no portable way to measure a filesystem")
	}
	if tr.joinReq.DiskTotalMB != wantTotal {
		t.Errorf("join reported %d MB of disk, want the work directory's %d MB",
			tr.joinReq.DiskTotalMB, wantTotal)
	}
	if tr.joinReq.DiskFreeMB == 0 {
		t.Errorf("join reported no free disk, though %d MB was measurable", wantFree)
	}

	// And every beat carries it too, or a host that grew -- or a disk that
	// filled -- would be believed at its joining size until it re-joined.
	if err := a.heartbeat(context.Background()); err != nil {
		t.Fatalf("heartbeat: %v", err)
	}
	select {
	case beat := <-tr.beats:
		if beat.CPUs != 64 || beat.MemoryMB != 262_144 {
			t.Errorf("the beat reported %d CPUs and %d MB; the agent sent its own cgroup share rather than the host",
				beat.CPUs, beat.MemoryMB)
		}
		if beat.DiskTotalMB != wantTotal {
			t.Errorf("the beat reported %d MB of disk, want the work directory's %d MB",
				beat.DiskTotalMB, wantTotal)
		}
		if beat.DiskFreeMB == 0 {
			t.Error("the beat reported no free disk, so a filling disk would never be noticed")
		}
	case <-time.After(time.Second):
		t.Fatal("no heartbeat was sent")
	}
}
