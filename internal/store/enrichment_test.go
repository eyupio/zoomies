package store

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

func TestEnrichmentRetriesAndPreservesConcurrentEvents(t *testing.T) {
	now := time.Now()
	s := newTestStoreAt(t, func() time.Time { return now })
	ctx := context.Background()
	if err := s.QueueEnrichment(ctx, "job", "job_a"); err != nil {
		t.Fatal(err)
	}
	first, err := s.ClaimEnrichment(ctx, 4)
	if err != nil || len(first) != 1 {
		t.Fatalf("claim=%v %v", first, err)
	}
	if got, err := s.ClaimEnrichment(ctx, 4); err != nil || len(got) != 0 {
		t.Fatalf("double claim=%v %v", got, err)
	}
	if err := s.QueueEnrichment(ctx, "job", "job_a"); err != nil {
		t.Fatal(err)
	}
	if err := s.FinishEnrichment(ctx, first[0], true); err != nil {
		t.Fatal(err)
	}
	now = now.Add(time.Minute)
	second, err := s.ClaimEnrichment(ctx, 4)
	if err != nil || len(second) != 1 || second[0].Generation <= first[0].Generation {
		t.Fatalf("new event lost=%v %v", second, err)
	}
	if err := s.FinishEnrichment(ctx, second[0], false); err != nil {
		t.Fatal(err)
	}
	now = now.Add(time.Minute)
	retry, err := s.ClaimEnrichment(ctx, 4)
	if err != nil || len(retry) != 1 {
		t.Fatalf("retry=%v %v", retry, err)
	}
	if err := s.FinishEnrichment(ctx, retry[0], true); err != nil {
		t.Fatal(err)
	}
	now = now.Add(time.Minute)
	if got, err := s.ClaimEnrichment(ctx, 4); err != nil || len(got) != 0 {
		t.Fatalf("acknowledged item repeated=%v %v", got, err)
	}
}

func TestBackupDoesNotAcquireApplicationWriter(t *testing.T) {
	s, _ := onDiskStore(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	s.wmu.Lock()
	defer s.wmu.Unlock()
	if err := s.Backup(ctx, filepath.Join(t.TempDir(), "copy.db")); err != nil {
		t.Fatal(err)
	}
}
