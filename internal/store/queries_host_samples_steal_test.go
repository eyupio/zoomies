package store

import (
	"context"
	"testing"
	"time"
)

// The history is where a steal spike that has passed is looked at, so it has to
// be kept, replaced with the minute it belongs to, and left a gap where the host
// did not measure it.
func TestHostSamplesKeepStealAndLeaveItAGapWhereItWasNotMeasured(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	at := time.Date(2026, 11, 3, 10, 30, 20, 0, time.UTC)
	cpu, steal := 68.0, 36.0
	if err := s.RecordHostSamples(ctx, []HostSample{
		{HostID: "host_a", At: at, Capacity: 4, CPUPercent: &cpu, StealPercent: &steal},
		{HostID: "host_old", At: at, Capacity: 4, CPUPercent: &cpu},
	}); err != nil {
		t.Fatalf("RecordHostSamples: %v", err)
	}
	// The same minute again, with a later figure: the later measurement stands.
	later := 41.0
	if err := s.RecordHostSamples(ctx, []HostSample{{HostID: "host_a", At: at.Add(30 * time.Second), Capacity: 4, CPUPercent: &cpu, StealPercent: &later}}); err != nil {
		t.Fatalf("RecordHostSamples again: %v", err)
	}
	got, err := s.ListHostSamples(ctx, at.Add(-time.Hour), "")
	if err != nil {
		t.Fatalf("ListHostSamples: %v", err)
	}
	byHost := map[string]HostSample{}
	for _, h := range got {
		byHost[h.HostID] = h
	}
	if a := byHost["host_a"]; a.StealPercent == nil || *a.StealPercent != later {
		t.Errorf("host_a steal = %v, want the later %v", a.StealPercent, later)
	}
	if old := byHost["host_old"]; old.StealPercent != nil {
		t.Errorf("a host that did not measure steal came back with %v, want a gap and not a zero", *old.StealPercent)
	}
}
