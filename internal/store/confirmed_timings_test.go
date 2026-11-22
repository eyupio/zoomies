package store

import (
	"context"
	"database/sql"
	"testing"
	"time"
)

func TestADeploymentReviewDoesNotStartTheSchedulingClock(t *testing.T) {
	now := time.Date(2026, 9, 9, 12, 0, 0, 0, time.UTC)
	s := newTestStoreAt(t, func() time.Time { return now })
	ctx := context.Background()
	job, err := s.UpsertJob(ctx, &Job{GitHubJobID: 9301, Repo: "acme/review", State: JobWaiting, Matched: true})
	if err != nil || job.EligibleAt != nil {
		t.Fatalf("a held job was marked eligible: %+v, %v", job, err)
	}
	now = now.Add(time.Hour)
	job, err = s.UpsertJob(ctx, &Job{GitHubJobID: 9301, Repo: "acme/review", State: JobQueued, Matched: true})
	if err != nil || job.EligibleAt == nil || !job.EligibleAt.Equal(now) {
		t.Fatalf("approval did not start the scheduling clock: %+v, %v", job, err)
	}
	if !job.QueuedAt.Equal(now) {
		t.Fatal("queue wait still includes the deployment review")
	}
	approved := now
	now = now.Add(time.Minute)
	job, err = s.UpsertJob(ctx, &Job{GitHubJobID: 9301, Repo: "acme/review", State: JobQueued, Matched: true})
	if err != nil || !job.QueuedAt.Equal(approved) || !job.EligibleAt.Equal(approved) {
		t.Fatalf("a replay moved the approval boundary: %+v, %v", job, err)
	}
}

// GitHub sends "queued" when a review passes, but deliveries are at least once
// and can arrive out of order, and the poller finds jobs the webhook never
// announced. A held job whose "in_progress" is the first thing seen after the
// review moves from waiting straight to running, and the boundary that marks the
// end of the review was only moved by "queued", so the hours spent waiting for a
// person to approve were reported as queue wait on a job the fleet picked up at
// once, which is what sends an operator looking for a capacity problem.
func TestAHeldJobThatRunsWithoutAQueuedDeliveryDoesNotCountItsReviewAsQueueWait(t *testing.T) {
	for _, tc := range []struct {
		name string
		// started is what the in_progress delivery says about when the job began.
		started func(now time.Time) *time.Time
		// wantQueued is where the queue wait starts: the start, if it is known,
		// since nothing says the job queued any earlier than that.
		wantQueued func(now time.Time) time.Time
	}{
		{"with its start time", func(now time.Time) *time.Time { return &now }, func(now time.Time) time.Time { return now }},
		{"without one", func(time.Time) *time.Time { return nil }, func(now time.Time) time.Time { return now }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			now := time.Date(2026, 9, 9, 12, 0, 0, 0, time.UTC)
			s := newTestStoreAt(t, func() time.Time { return now })
			ctx := context.Background()
			if _, err := s.UpsertJob(ctx, &Job{GitHubJobID: 9302, Repo: "acme/review", State: JobWaiting, Matched: true}); err != nil {
				t.Fatal(err)
			}
			now = now.Add(3 * time.Hour)
			job, err := s.UpsertJob(ctx, &Job{GitHubJobID: 9302, Repo: "acme/review", State: JobInProgress, Matched: true, StartedAt: tc.started(now)})
			if err != nil {
				t.Fatal(err)
			}
			if want := tc.wantQueued(now); !job.QueuedAt.Equal(want) {
				t.Errorf("QueuedAt = %s after a 3 hour review, want %s: the review is counted as queue wait", job.QueuedAt, want)
			}
			if job.StartedAt != nil && job.QueueWait() > time.Minute {
				t.Errorf("queue wait = %s for a job that ran as soon as it was approved", job.QueueWait())
			}
		})
	}
}

func TestCleanupCompletesOnlyAfterBothPositiveConfirmations(t *testing.T) {
	for _, hostFirst := range []bool{false, true} {
		name := "registration first"
		if hostFirst {
			name = "host first"
		}
		t.Run(name, func(t *testing.T) {
			now := time.Date(2026, 9, 9, 12, 0, 0, 0, time.UTC)
			s := newTestStoreAt(t, func() time.Time { return now })
			ctx := context.Background()
			_, pool, host := seedPool(t, s)
			r := &Runner{PoolID: pool.ID, HostID: host.ID, Name: "confirmed-cleanup"}
			if err := s.CreateRunner(ctx, r); err != nil {
				t.Fatal(err)
			}
			if err := s.ClearCleanupFailure(ctx, r.ID); err != nil {
				t.Fatal(err)
			}
			got, err := s.GetRunner(ctx, r.ID)
			if err != nil || got.CleanedUpAt != nil {
				t.Fatalf("a successful stop is not removal: %+v, %v", got, err)
			}
			if done, err := s.ConfirmRunnerCleanup(ctx, r.ID, hostFirst); err != nil || done {
				t.Fatalf("first confirmation completed cleanup: %v, %v", done, err)
			}
			got, err = s.GetRunner(ctx, r.ID)
			if err != nil || got.CleanedUpAt != nil {
				t.Fatalf("first confirmation stamped completion: %+v, %v", got, err)
			}
			now = now.Add(2 * time.Minute)
			if done, err := s.ConfirmRunnerCleanup(ctx, r.ID, !hostFirst); err != nil || !done {
				t.Fatalf("second confirmation did not complete cleanup: %v, %v", done, err)
			}
			got, err = s.GetRunner(ctx, r.ID)
			if err != nil || got.CleanedUpAt == nil || !got.CleanedUpAt.Equal(now) {
				t.Fatalf("completion must use the final confirmation: %+v, %v", got, err)
			}
			now = now.Add(time.Hour)
			for _, side := range []bool{hostFirst, !hostFirst} {
				if done, err := s.ConfirmRunnerCleanup(ctx, r.ID, side); err != nil || done {
					t.Fatalf("duplicate confirmation counted another completion: %v, %v", done, err)
				}
			}
			after, err := s.GetRunner(ctx, r.ID)
			if err != nil || !after.CleanedUpAt.Equal(*got.CleanedUpAt) {
				t.Fatalf("redelivery overwrote the cleanup timing: %+v, %v", after, err)
			}
		})
	}
}

func TestTimingMigrationPreservesEstimatesWithoutInventingConfirmations(t *testing.T) {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	_, err = db.Exec(`CREATE TABLE runners (id TEXT PRIMARY KEY, cleaned_up_at INTEGER);
 INSERT INTO runners VALUES ('old', 123), ('unknown', NULL);
 CREATE TABLE jobs (id TEXT PRIMARY KEY, state TEXT, eligible_at INTEGER);
 INSERT INTO jobs VALUES ('held', 'waiting', 100), ('queued', 'queued', 200);`)
	if err != nil {
		t.Fatal(err)
	}
	body, err := migrationFS.ReadFile("migrations/0023_runner_confirmed_timings.sql")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(string(body)); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"old", "unknown"} {
		var cleaned, estimated, host, issued sql.NullInt64
		if err := db.QueryRow(`SELECT cleaned_up_at, cleanup_estimated_at, host_removed_at, create_task_issued_at FROM runners WHERE id=?`, id).Scan(&cleaned, &estimated, &host, &issued); err != nil {
			t.Fatal(err)
		}
		if cleaned.Valid || host.Valid || issued.Valid {
			t.Fatal("migration invented an observed timing")
		}
		if (id == "old" && (!estimated.Valid || estimated.Int64 != 123)) || (id == "unknown" && estimated.Valid) {
			t.Fatal("migration did not preserve the historical estimate")
		}
	}
	for _, id := range []string{"held", "queued"} {
		var eligible sql.NullInt64
		if err := db.QueryRow(`SELECT eligible_at FROM jobs WHERE id=?`, id).Scan(&eligible); err != nil {
			t.Fatal(err)
		}
		if (id == "held" && eligible.Valid) || (id == "queued" && (!eligible.Valid || eligible.Int64 != 200)) {
			t.Fatalf("wrong eligibility for %s: %+v", id, eligible)
		}
	}
}
