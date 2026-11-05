package store

import (
	"context"
	"testing"
	"time"
)

// The enrichment loop asks for work every second and nearly always there is none.
// Taking the single writer to be told so makes every webhook and heartbeat queue
// behind a question whose answer is no, so the question is put to the readers and
// the writer is taken only for a claim that has something to claim.
func TestAnEnrichmentClaimWithNothingDueNeverTakesTheWriter(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	takes := countWriterTakes(s)

	got, err := s.ClaimEnrichment(ctx, 4)
	if err != nil || len(got) != 0 {
		t.Fatalf("claiming from an empty table = %d items, err = %v; want none", len(got), err)
	}
	if n := takes.Load(); n != 0 {
		t.Errorf("an empty table took the writer %d time(s) to say so", n)
	}

	if err := s.QueueEnrichment(ctx, "job", "job_1"); err != nil {
		t.Fatalf("QueueEnrichment: %v", err)
	}
	queued := takes.Load()
	got, err = s.ClaimEnrichment(ctx, 4)
	if err != nil || len(got) != 1 || got[0].Target != "job_1" {
		t.Fatalf("claiming a due item = %+v, err = %v; want job_1", got, err)
	}
	if n := takes.Load() - queued; n != 1 {
		t.Errorf("claiming a due item took the writer %d time(s); a claim is a write and takes it once", n)
	}

	// The claim holds the item back for a minute, so there is nothing due again,
	// and asking again is the idle question once more.
	claimed := takes.Load()
	got, err = s.ClaimEnrichment(ctx, 4)
	if err != nil || len(got) != 0 {
		t.Fatalf("claiming an item already claimed = %d items, err = %v; want none", len(got), err)
	}
	if n := takes.Load() - claimed; n != 0 {
		t.Errorf("a table whose only row is held back took the writer %d time(s) to say nothing was due", n)
	}
}

// The same question put by the Eli watch loop every three seconds, whether or not
// anybody has asked for a watch. A watch not yet due is nothing to claim.
func TestAWatchClaimWithNothingDueNeverTakesTheWriter(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	takes := countWriterTakes(s)

	if _, err := s.ClaimDueEliRepairWatch(ctx, s.Now(), time.Minute); err != ErrNotFound {
		t.Fatalf("claiming with no watches = %v; want ErrNotFound", err)
	}
	if n := takes.Load(); n != 0 {
		t.Errorf("an empty table took the writer %d time(s) to say so", n)
	}

	inst := watchInstallation(t, s)
	w, err := s.AdmitEliRepairWatch(ctx, EliRepairWatch{InstallationID: inst.ID, Repo: "acme/repo", PullNumber: 1, OwnerID: "usr_one", Checks: true})
	if err != nil {
		t.Fatalf("AdmitEliRepairWatch: %v", err)
	}
	admitted := takes.Load()

	// A watch is due from the moment it is admitted, so an hour before that is
	// a time at which it is not.
	if _, err := s.ClaimDueEliRepairWatch(ctx, w.NextCheckAt.Add(-time.Hour), time.Minute); err != ErrNotFound {
		t.Fatalf("claiming before the watch was due = %v; want ErrNotFound", err)
	}
	if n := takes.Load() - admitted; n != 0 {
		t.Errorf("a watch that was not due took the writer %d time(s) to be passed over", n)
	}

	claimed, err := s.ClaimDueEliRepairWatch(ctx, w.NextCheckAt.Add(time.Second), time.Minute)
	if err != nil || claimed.ID != w.ID {
		t.Fatalf("claiming a due watch = %+v, err = %v; want %s", claimed, err, w.ID)
	}
	if n := takes.Load() - admitted; n != 1 {
		t.Errorf("claiming a due watch took the writer %d time(s); a claim is a write and takes it once", n)
	}

	// Leased now, so not due to anybody else: the second observer is passed over
	// without the writer too.
	leased := takes.Load()
	if _, err := s.ClaimDueEliRepairWatch(ctx, w.NextCheckAt.Add(2*time.Second), time.Minute); err != ErrNotFound {
		t.Fatalf("claiming a watch that is leased = %v; want ErrNotFound", err)
	}
	if n := takes.Load() - leased; n != 0 {
		t.Errorf("a leased watch took the writer %d time(s) to be passed over", n)
	}
}
