package agent

import (
	"context"
	"strings"
	"sync/atomic"
	"testing"
)

func TestDiskAdmissionPausesAndResumesWithoutStoppingRunners(t *testing.T) {
	var free atomic.Int64
	free.Store(160 << 30)
	h := newHarness(t, 4, func(o *Options) {
		o.StopFreeGiB = 150
		o.diskSpace = func(string) (int64, int64, bool) { return 1000 << 30, free.Load(), true }
	})
	h.tr.tasks <- []Task{createTask("first", "running")}
	if r := h.nextResult(); !r.OK {
		t.Fatal(r.Error)
	}
	free.Store((150 << 30) - 1)
	h.tr.tasks <- []Task{createTask("low", "paused")}
	if r := h.nextResult(); r.OK || !strings.Contains(r.Error, "new runners paused") {
		t.Fatalf("low disk accepted: %+v", r)
	}
	if created, stopped, removed := h.be.counts(); created != 1 || stopped != 0 || removed != 0 {
		t.Fatalf("running job changed: %d %d %d", created, stopped, removed)
	}
	free.Store(150 << 30)
	h.tr.tasks <- []Task{createTask("recovered", "new")}
	if r := h.nextResult(); !r.OK {
		t.Fatal(r.Error)
	}
	if created, _, _ := h.be.counts(); created != 2 {
		t.Fatal(created)
	}
}

func TestDiskAdmissionDisabledAndUnmeasurable(t *testing.T) {
	a := &Agent{opts: Options{diskSpace: func(string) (int64, int64, bool) { return 0, 0, false }}}
	if why := a.diskRefusal(); why != "" {
		t.Fatal(why)
	}
	a.opts.StopFreeGiB = 1
	if why := a.diskRefusal(); !strings.Contains(why, "cannot measure") {
		t.Fatal(why)
	}
}

func TestDiskPauseIsAdvertisedAndClears(t *testing.T) {
	var free atomic.Int64
	free.Store(50 << 30)
	a, tr, _, _ := newAgent(t, 2, func(o *Options) {
		o.StopFreeGiB = 80
		o.diskSpace = func(string) (int64, int64, bool) { return 1000 << 30, free.Load(), true }
	})
	for _, available := range []bool{false, true} {
		if available {
			free.Store(80 << 30)
		}
		if err := a.heartbeat(context.Background()); err != nil {
			t.Fatal(err)
		}
		beat := <-tr.beats
		if len(beat.Backends) == 0 || beat.Backends[0].Available != available {
			t.Fatalf("backends: %+v", beat.Backends)
		}
	}
}

// Delivery is at-least-once, so a create whose result was lost comes round
// again, possibly after the disk has fallen below the reserve. The runner is
// already there and costs no more disk; refusing it would report a live job's
// runner as failed.
func TestARedeliveredCreateBelowTheReserveIsAcknowledgedNotRefused(t *testing.T) {
	var free atomic.Int64
	free.Store(160 << 30)
	h := newHarness(t, 4, func(o *Options) {
		o.StopFreeGiB = 150
		o.diskSpace = func(string) (int64, int64, bool) { return 1000 << 30, free.Load(), true }
	})
	h.tr.tasks <- []Task{createTask("first", "running")}
	first := h.nextResult()
	if !first.OK {
		t.Fatal(first.Error)
	}
	free.Store(10 << 30)
	h.tr.tasks <- []Task{createTask("again", "running")}
	again := h.nextResult()
	if !again.OK || again.Handle != first.Handle {
		t.Fatalf("redelivery below the reserve was not acknowledged: %+v", again)
	}
	if created, _, _ := h.be.counts(); created != 1 {
		t.Fatalf("created %d runners, want only the first", created)
	}
}

// A pause advertises every backend as unavailable, but the host's size comes
// from those same probes. If the pause replaced the daemon's CPU and memory
// with the agent's own cgroup limits, a containerised agent would report a
// smaller machine while paused and the controller would size jobs from it.
func TestThePauseDoesNotChangeTheSizeTheHostReports(t *testing.T) {
	var free atomic.Int64
	a, tr, be, _ := newAgent(t, 2, func(o *Options) {
		o.StopFreeGiB = 80
		o.diskSpace = func(string) (int64, int64, bool) { return 1000 << 30, free.Load(), true }
	})
	be.cpus, be.memoryMB = 32, 65536
	var sizes [2][2]int64
	for i, bytes := range []int64{200 << 30, 50 << 30} {
		free.Store(bytes)
		if err := a.heartbeat(context.Background()); err != nil {
			t.Fatal(err)
		}
		beat := <-tr.beats
		sizes[i] = [2]int64{int64(beat.CPUs), beat.MemoryMB}
		if paused := !beat.Backends[0].Available; paused != (i == 1) {
			t.Fatalf("heartbeat %d: backend available = %v", i, !paused)
		}
	}
	if sizes[0] != sizes[1] || sizes[0][0] != 32 {
		t.Fatalf("size changed with the pause: %v", sizes)
	}
}
