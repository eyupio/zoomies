package store

import (
	"context"
	"testing"
	"time"
)

func TestRepositoryWatchDecisionsAreExplicitAndRotate(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	inst := watchInstallation(t, s)
	for i := 0; i < 25; i++ {
		repo := "acme/repo-" + string(rune('a'+i))
		if _, err := s.SetEliRepairWatchRepository(ctx, EliRepairWatchRepository{InstallationID: inst.ID, Repo: repo, OwnerID: "usr_one", Checks: true}); err != nil {
			t.Fatal(err)
		}
	}
	first, err := s.DueEliRepairWatchRepositories(ctx, 20)
	if err != nil || len(first) != 20 {
		t.Fatalf("first pass %d, %v", len(first), err)
	}
	now := s.Now().Add(time.Minute)
	for _, sub := range first {
		if err := s.MarkEliRepairWatchRepositoryDiscovered(ctx, sub.ID, now); err != nil {
			t.Fatal(err)
		}
	}
	second, err := s.DueEliRepairWatchRepositories(ctx, 5)
	if err != nil || len(second) != 5 {
		t.Fatalf("second pass %d, %v", len(second), err)
	}
	for _, sub := range second {
		for _, seen := range first {
			if sub.ID == seen.ID {
				t.Fatalf("%s was scanned twice before the others", sub.ID)
			}
		}
	}
	repeated, err := s.SetEliRepairWatchRepository(ctx, EliRepairWatchRepository{InstallationID: inst.ID, Repo: first[0].Repo, OwnerID: "usr_two", Checks: true})
	if err != nil || repeated.ID != first[0].ID || repeated.LastDiscoveredAt.IsZero() {
		t.Fatalf("refreshing a decision lost its progress: %+v, %v", repeated, err)
	}
}
