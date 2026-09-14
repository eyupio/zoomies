package store

import (
	"context"
	"testing"
	"time"
)

// A minute recorded twice keeps the later measurement, because the sampler
// runs on a ticker and a restart mid-minute would otherwise leave a row for
// each life of the controller.
func TestRecordingHostSamplesReplacesTheMinute(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	at := time.Date(2026, 3, 1, 9, 0, 20, 0, time.UTC)
	cpu := 40.0
	if err := s.RecordHostSamples(ctx, []HostSample{{HostID: "host_a", At: at, Capacity: 4, ActiveRunners: 1, CPUPercent: &cpu}}); err != nil {
		t.Fatalf("RecordHostSamples: %v", err)
	}
	later := 85.0
	if err := s.RecordHostSamples(ctx, []HostSample{{HostID: "host_a", At: at.Add(30 * time.Second), Capacity: 4, ActiveRunners: 3, CPUPercent: &later}}); err != nil {
		t.Fatalf("RecordHostSamples again: %v", err)
	}
	got, err := s.ListHostSamples(ctx, at.Add(-time.Hour), "")
	if err != nil {
		t.Fatalf("ListHostSamples: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("samples = %d, want the one minute", len(got))
	}
	if got[0].ActiveRunners != 3 || got[0].CPUPercent == nil || *got[0].CPUPercent != 85 {
		t.Errorf("sample = %+v, want the later measurement", got[0])
	}
	if !got[0].At.Equal(at.Truncate(time.Minute)) {
		t.Errorf("at = %s, want the minute %s", got[0].At, at.Truncate(time.Minute))
	}
}

// Nil stays nil across the round trip. A host that has not measured its CPU
// must come back as a gap in the chart, not as a machine at zero.
func TestHostSamplesKeepUnmeasuredFiguresAsGaps(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	at := time.Date(2026, 3, 1, 9, 0, 0, 0, time.UTC)
	mem := int64(0)
	if err := s.RecordHostSamples(ctx, []HostSample{
		{HostID: "host_old", At: at, Capacity: 2},
		{HostID: "host_new", At: at, Capacity: 2, MemoryAvailableMB: &mem},
	}); err != nil {
		t.Fatalf("RecordHostSamples: %v", err)
	}
	got, err := s.ListHostSamples(ctx, at, "")
	if err != nil {
		t.Fatalf("ListHostSamples: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("samples = %d, want both hosts", len(got))
	}
	byHost := map[string]HostSample{}
	for _, h := range got {
		byHost[h.HostID] = h
	}
	if old := byHost["host_old"]; old.CPUPercent != nil || old.MemoryAvailableMB != nil || old.ReservedCPUs != nil {
		t.Errorf("unmeasured host came back with figures: %+v", old)
	}
	if fresh := byHost["host_new"]; fresh.MemoryAvailableMB == nil || *fresh.MemoryAvailableMB != 0 {
		t.Errorf("a measured zero came back as %v, want 0", fresh.MemoryAvailableMB)
	}
	one, err := s.ListHostSamples(ctx, at, "host_new")
	if err != nil || len(one) != 1 || one[0].HostID != "host_new" {
		t.Errorf("filtered by host = %+v, %v; want just host_new", one, err)
	}
}

func TestPruningHostSamplesKeepsTheOnesInsideTheWindow(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	now := time.Now()
	if err := s.RecordHostSamples(ctx, []HostSample{
		{HostID: "host_a", At: now.Add(-48 * time.Hour)},
		{HostID: "host_a", At: now.Add(-time.Hour)},
	}); err != nil {
		t.Fatalf("RecordHostSamples: %v", err)
	}
	n, err := s.PruneHostSamples(ctx, now.Add(-24*time.Hour))
	if err != nil {
		t.Fatalf("PruneHostSamples: %v", err)
	}
	if n != 1 {
		t.Errorf("pruned %d, want 1", n)
	}
	left, err := s.ListHostSamples(ctx, now.Add(-72*time.Hour), "")
	if err != nil {
		t.Fatalf("ListHostSamples: %v", err)
	}
	if len(left) != 1 || left[0].At.Before(now.Add(-2*time.Hour)) {
		t.Fatalf("left = %+v, want only the recent sample", left)
	}
}
