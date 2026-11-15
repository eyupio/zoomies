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
