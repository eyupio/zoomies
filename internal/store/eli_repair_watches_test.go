package store

import (
	"context"
	"testing"
	"time"
)

func watchInstallation(t *testing.T, s *Store) *Installation {
	t.Helper()
	inst := &Installation{AppID: 1, InstallationID: 2, Target: "acme", TargetType: TargetOrg}
	if err := s.CreateInstallation(context.Background(), inst); err != nil {
		t.Fatal(err)
	}
	return inst
}

func TestAnExplicitAdmissionIsTheOnlyThingThatCreatesOrResumesAWatch(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	inst := watchInstallation(t, s)
	policy := time.Date(2026, 11, 3, 12, 0, 0, 0, time.UTC)
	in := EliRepairWatch{InstallationID: inst.ID, Repo: "Acme/Repo", PullNumber: 7, OwnerID: "usr_one",
		PolicyProviderID: "asp_one", PolicyUpdatedAt: policy, Checks: true, HeadSHA: "head-a", BaseRef: "main", BaseSHA: "base-a"}
	started := s.Now()
	first, err := s.AdmitEliRepairWatch(ctx, in)
	if err != nil {
		t.Fatal(err)
	}
	if !first.Enabled || first.ID == "" || !first.Checks || first.Conflicts {
		t.Fatalf("admitted watch %+v", first)
	}
	if first.NextCheckAt.UnixMilli() < started.UnixMilli() || first.NextCheckAt.UnixMilli() > s.Now().UnixMilli() {
		t.Fatalf("a new watch is not due now: %s", first.NextCheckAt)
	}

	// The same person asking again is idempotent, even with GitHub's casing.
	in.Repo, in.HeadSHA, in.BaseSHA = "acme/repo", "head-b", "base-b"
	second, err := s.AdmitEliRepairWatch(ctx, in)
	if err != nil {
		t.Fatal(err)
	}
	if second.ID != first.ID || second.HeadSHA != "head-b" || second.PolicyUpdatedAt != policy {
		t.Fatalf("a repeated admission made a second watch or lost its identity: %+v", second)
	}

	if err := s.PauseEliRepairWatch(ctx, first.ID, "Paused for this pull request."); err != nil {
		t.Fatal(err)
	}
	if err := s.PauseEliRepairWatch(ctx, "erw_missing", "Paused."); err != ErrNotFound {
		t.Fatalf("pausing an unknown watch answered %v", err)
	}
	paused, err := s.EliRepairWatch(ctx, first.ID)
	if err != nil {
		t.Fatal(err)
	}
	if paused.Enabled || paused.PausedReason == "" {
		t.Fatalf("pause did not stick: %+v", paused)
	}
	if _, err := s.ClaimDueEliRepairWatch(ctx, s.Now().Add(time.Minute), time.Minute); err == nil {
		t.Fatal("a paused watch was claimed")
	}

	// Only an explicit admission resumes it. Looking it up or changing a policy
	// is not an admission.
	if _, err := s.EliRepairWatchForPull(ctx, "ACME/REPO", 7); err != nil {
		t.Fatal(err)
	}
	if _, err := s.exec(ctx, `UPDATE eli_repair_watches SET check_state='passed' WHERE id=?`, first.ID); err != nil {
		t.Fatal(err)
	}
	in.HeadSHA, in.BaseSHA = "head-c", "base-c"
	resumed, err := s.AdmitEliRepairWatch(ctx, in)
	if err != nil {
		t.Fatal(err)
	}
	if !resumed.Enabled || resumed.PausedReason != "" || resumed.ID != first.ID || resumed.CheckState != "unknown" {
		t.Fatalf("explicit admission did not resume the same watch cleanly: %+v", resumed)
	}
}

func TestAWatchLeaseKeepsOneObserverAtATimeAndExpires(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	inst := watchInstallation(t, s)
	w, err := s.AdmitEliRepairWatch(ctx, EliRepairWatch{InstallationID: inst.ID, Repo: "acme/repo", PullNumber: 1, OwnerID: "usr_one", Checks: true})
	if err != nil {
		t.Fatal(err)
	}
	now := s.Now().Add(time.Second)
	claimed, err := s.ClaimDueEliRepairWatch(ctx, now, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if claimed.ID != w.ID || claimed.LeaseToken == "" || !claimed.LeaseUntil.Equal(now.Add(time.Minute)) {
		t.Fatalf("claimed %+v", claimed)
	}
	if _, err := s.ClaimDueEliRepairWatch(ctx, now.Add(time.Second), time.Minute); err != ErrNotFound {
		t.Fatalf("a second observer claimed a leased watch: %v", err)
	}
	next := now.Add(30 * time.Second)
	if err := s.ReleaseEliRepairWatch(ctx, w.ID, "wrong-token", "head", "main", "base", "unknown", next); err != ErrConflict {
		t.Fatalf("a stale holder released the lease: %v", err)
	}
	if err := s.ReleaseEliRepairWatch(ctx, w.ID, claimed.LeaseToken, "head-b", "main", "base-b", "failed", next); err != nil {
		t.Fatal(err)
	}
	released, err := s.EliRepairWatch(ctx, w.ID)
	if err != nil {
		t.Fatal(err)
	}
	if released.LeaseToken != "" || released.HeadSHA != "head-b" || released.BaseSHA != "base-b" || released.NextCheckAt.UnixMilli() != next.UnixMilli() {
		t.Fatalf("release did not record the observation: %+v", released)
	}
	if _, err := s.ClaimDueEliRepairWatch(ctx, next.Add(-time.Second), time.Minute); err != ErrNotFound {
		t.Fatalf("a watch was claimed before its next check: %v", err)
	}
	again, err := s.ClaimDueEliRepairWatch(ctx, next, time.Minute)
	if err != nil || again.ID != w.ID {
		t.Fatalf("a released watch was not claimable at its next check: %+v, %v", again, err)
	}
}

func TestRestartReleasesInterruptedWatchWorkButNotPausedWatches(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	inst := watchInstallation(t, s)
	live, err := s.AdmitEliRepairWatch(ctx, EliRepairWatch{InstallationID: inst.ID, Repo: "acme/live", PullNumber: 1, OwnerID: "usr_one", Checks: true})
	if err != nil {
		t.Fatal(err)
	}
	paused, err := s.AdmitEliRepairWatch(ctx, EliRepairWatch{InstallationID: inst.ID, Repo: "acme/paused", PullNumber: 2, OwnerID: "usr_one", Checks: true})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.PauseEliRepairWatch(ctx, paused.ID, "Paused for this pull request."); err != nil {
		t.Fatal(err)
	}
	now := s.Now().Add(time.Second)
	claimed, err := s.ClaimDueEliRepairWatch(ctx, now, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if claimed.ID != live.ID {
		t.Fatalf("claimed %s, want the live watch", claimed.ID)
	}
	if err := s.RecoverEliRepairWatches(ctx, now.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	recovered, err := s.EliRepairWatch(ctx, live.ID)
	if err != nil {
		t.Fatal(err)
	}
	if recovered.LeaseToken != "" || recovered.NextCheckAt.UnixMilli() != now.Add(time.Minute).UnixMilli() {
		t.Fatalf("restart did not release and requeue the interrupted observation: %+v", recovered)
	}
	stillPaused, err := s.EliRepairWatch(ctx, paused.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stillPaused.Enabled || stillPaused.NextCheckAt.UnixMilli() != paused.NextCheckAt.UnixMilli() {
		t.Fatalf("restart changed a paused watch: %+v", stillPaused)
	}
	next, err := s.ClaimDueEliRepairWatch(ctx, now.Add(time.Minute), time.Minute)
	if err != nil || next.ID != live.ID {
		t.Fatalf("the recovered watch was not claimable after restart: %+v, %v", next, err)
	}
}

func TestAnAttemptCarriesTheWatchThatAuthorisedIt(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	inst := watchInstallation(t, s)
	w, err := s.AdmitEliRepairWatch(ctx, EliRepairWatch{InstallationID: inst.ID, Repo: "acme/repo", PullNumber: 1, OwnerID: "usr_one", Checks: true})
	if err != nil {
		t.Fatal(err)
	}
	r := &EliRepair{Repo: "acme/repo", InstallationID: inst.ID, PullNumber: 1, Trigger: "automatic", DedupKey: "watch:1", WatchID: w.ID}
	if _, err := s.EnqueueEliRepair(ctx, r); err != nil {
		t.Fatal(err)
	}
	got, err := s.GetEliRepair(ctx, r.ID, "", false)
	if err != nil || got.WatchID != w.ID {
		t.Fatalf("watch link lost: %+v, %v", got, err)
	}
}

func TestRepeatedAdmissionLeavesAnActiveObservationAlone(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	inst := watchInstallation(t, s)
	w, err := s.AdmitEliRepairWatch(ctx, EliRepairWatch{InstallationID: inst.ID, Repo: "acme/repo", PullNumber: 1, OwnerID: "usr_one", Checks: true})
	if err != nil {
		t.Fatal(err)
	}
	now := s.Now().Add(time.Second)
	claimed, err := s.ClaimDueEliRepairWatch(ctx, now, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	repeated, err := s.AdmitEliRepairWatch(ctx, EliRepairWatch{
		InstallationID: inst.ID, Repo: "ACME/REPO", PullNumber: 1, OwnerID: "usr_two", Checks: true,
		HeadSHA: "new-head", BaseRef: "main", BaseSHA: "new-base",
	})
	if err != nil {
		t.Fatal(err)
	}
	if repeated.ID != w.ID || repeated.LeaseToken != claimed.LeaseToken ||
		repeated.LeaseUntil.UnixMilli() != claimed.LeaseUntil.UnixMilli() ||
		repeated.NextCheckAt.UnixMilli() != claimed.NextCheckAt.UnixMilli() {
		t.Fatalf("repeated admission disturbed the active lease: %+v, claimed %+v", repeated, claimed)
	}
	if repeated.HeadSHA != "" || repeated.BaseSHA != "" {
		t.Fatalf("repeated admission changed an observation in flight: %+v", repeated)
	}
	if _, err := s.ClaimDueEliRepairWatch(ctx, now.Add(time.Second), time.Hour); err != ErrNotFound {
		t.Fatalf("a second observer claimed the leased watch: %v", err)
	}
	if err := s.ReleaseEliRepairWatch(ctx, repeated.ID, claimed.LeaseToken, "head", "main", "base", "waiting", now.Add(time.Minute)); err != nil {
		t.Fatalf("the original observer could not record its result: %v", err)
	}
}

func TestAStaleObserverCannotPauseOrStopANewerWatch(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	inst := watchInstallation(t, s)
	w, err := s.AdmitEliRepairWatch(ctx, EliRepairWatch{InstallationID: inst.ID, Repo: "acme/repo", PullNumber: 1, OwnerID: "usr_one", Checks: true})
	if err != nil {
		t.Fatal(err)
	}
	claimed, err := s.ClaimDueEliRepairWatch(ctx, s.Now().Add(time.Second), time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.PauseEliRepairWatchLease(ctx, w.ID, "stale-token", "Paused."); err != ErrConflict {
		t.Fatalf("a stale observer paused the watch: %v", err)
	}
	still, err := s.EliRepairWatch(ctx, w.ID)
	if err != nil || !still.Enabled || still.LeaseToken != claimed.LeaseToken {
		t.Fatalf("a stale pause changed the live watch: %+v, %v", still, err)
	}
	if err := s.PauseEliRepairWatchLease(ctx, w.ID, claimed.LeaseToken, "Paused."); err != nil {
		t.Fatal(err)
	}
	resumed, err := s.AdmitEliRepairWatch(ctx, EliRepairWatch{InstallationID: inst.ID, Repo: "acme/repo", PullNumber: 1, OwnerID: "usr_one", Checks: true})
	if err != nil {
		t.Fatal(err)
	}
	claimed, err = s.ClaimDueEliRepairWatch(ctx, s.Now().Add(2*time.Second), time.Minute)
	if err != nil || claimed.ID != resumed.ID {
		t.Fatalf("re-admitted watch was not claimable: %+v, %v", claimed, err)
	}
	if err := s.StopEliRepairWatchLease(ctx, w.ID, "stale-token", "Stopped."); err != ErrConflict {
		t.Fatalf("a stale observer stopped the watch: %v", err)
	}
	fresh, err := s.EliRepairWatch(ctx, w.ID)
	if err != nil || !fresh.Enabled || fresh.StopReason != "" || fresh.LeaseToken != claimed.LeaseToken {
		t.Fatalf("a stale stop changed the new watch: %+v, %v", fresh, err)
	}
}
