package store

import (
	"context"
	"slices"
	"testing"
	"time"
)

// A job is counted in the hour it finished, so a list reached from that hour
// has to ask when jobs finished and not when they were queued: a job queued at
// 09:50 and done at 10:10 belongs to the 10:00 square.
func TestCompletedWindowSelectsByWhenAJobFinished(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	at := func(m int) time.Time { return versionsEpoch.Add(time.Duration(m) * time.Minute) }
	none := time.Duration(0)

	// Finished at 10 minutes, having been queued before the window opened.
	early := versionJob(t, s, 1, "v1", "build", at(0), none, 10*time.Minute, "success")
	// Finished exactly when the window opens: the start is included.
	onStart := versionJob(t, s, 2, "v1", "build", at(5), none, 5*time.Minute, "success")
	// Queued and finished inside it.
	inside := versionJob(t, s, 3, "v1", "build", at(12), none, time.Minute, "success")
	// Finished exactly when the window closes: the end is not included.
	onEnd := versionJob(t, s, 4, "v1", "build", at(15), none, 5*time.Minute, "success")
	// Finished before it opened.
	before := versionJob(t, s, 5, "v1", "build", at(2), none, 3*time.Minute, "success")
	// Still waiting: it has finished in no window.
	waiting, _, err := s.ApplyJob(ctx, &Job{
		GitHubJobID: 2000, Repo: "acme/widgets", Workflow: "ci", JobName: "build",
		Labels: StringSlice{"self-hosted"}, State: JobQueued, QueuedAt: at(12), Matched: true, PoolID: "pool_a",
	})
	if err != nil {
		t.Fatal(err)
	}

	from, to := at(10), at(20)
	page := Page{Limit: 20, Sort: "queued_at", Desc: false}
	got, total, err := s.ListJobs(ctx, JobFilter{CompletedSince: &from, CompletedUntil: &to}, page)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{early.ID, onStart.ID, inside.ID}
	if !slices.Equal(ids(got), want) || total != 3 {
		t.Fatalf("finished in [10, 20) = %v (total %d), want %v; excluded: end %s, before %s, waiting %s",
			ids(got), total, want, onEnd.ID, before.ID, waiting.ID)
	}

	// The two windows compose: of the jobs queued in [11, 16), only the one that
	// also finished in [10, 20) is left.
	qFrom, qTo := at(11), at(16)
	got, total, err = s.ListJobs(ctx, JobFilter{Since: &qFrom, Until: &qTo, CompletedSince: &from, CompletedUntil: &to}, page)
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{inside.ID}; !slices.Equal(ids(got), want) || total != 1 {
		t.Fatalf("queued in [11, 16) and finished in [10, 20) = %v (total %d), want %v", ids(got), total, want)
	}
}
