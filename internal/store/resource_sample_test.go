package store

import (
	"context"
	"testing"
)

func TestResourceSampleRoundTripPreservesFreshnessAndZeroUsage(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	_, pool, host := seedPool(t, s)
	r := &Runner{PoolID: pool.ID, HostID: host.ID, Name: "sample", State: RunnerRegistering}
	if err := s.CreateRunner(ctx, r); err != nil {
		t.Fatal(err)
	}
	sample := []byte(`{"sampled_at":"2026-09-19T00:00:00Z","cpu_throttling":{"periods":10,"throttled_periods":3,"throttled_nanoseconds":1200}}`)
	if err := s.SetRunnerResourceSample(ctx, r.ID, 0, 0, sample); err != nil {
		t.Fatal(err)
	}
	got, err := s.GetRunner(ctx, r.ID)
	if err != nil {
		t.Fatal(err)
	}
	if string(got.ResourceSample) != string(sample) || got.CPUPercent != 0 || got.MemoryBytes != 0 {
		t.Fatalf("sample: %+v", got)
	}
	got.Message = "updated"
	if err := s.UpdateRunner(ctx, got); err != nil {
		t.Fatal(err)
	}
	got, err = s.GetRunner(ctx, r.ID)
	if err != nil || string(got.ResourceSample) != string(sample) {
		t.Fatalf("sample lost on update: %+v, %v", got, err)
	}
}
