package store

import (
	"strings"
	"testing"
	"time"
)

// seedJob writes one job row directly, because the point of these tests is the
// query rather than the ingest path that normally fills the table.
func seedJob(t *testing.T, s *Store, ghID int64, state JobState, runnerID string) *Job {
	t.Helper()
	j, err := s.UpsertJob(t.Context(), &Job{
		GitHubJobID: ghID, Repo: "acme/api", JobName: "build", State: state,
		RunnerID: runnerID, QueuedAt: time.Now().Add(-time.Minute),
	})
	if err != nil {
		t.Fatalf("UpsertJob: %v", err)
	}
	return j
}

// A runner failed as lost has had its current_job_id cleared, so the only way
// left to ask "is anything still running on this container?" is from the job's
// side. Getting that answer wrong in either direction costs something real:
// a false empty deletes a container with a job in it, and a false hit leaves
// the container on the host for ever.
func TestRunningJobsForARunnerAreTheInProgressOnesOnly(t *testing.T) {
	s := newTestStore(t)
	ctx := t.Context()

	want := seedJob(t, s, 1, JobInProgress, "run_target")
	seedJob(t, s, 2, JobCompleted, "run_target")
	seedJob(t, s, 3, JobQueued, "run_target")
	seedJob(t, s, 4, JobInProgress, "run_other")
	seedJob(t, s, 5, JobInProgress, "")

	got, err := s.ListRunningJobsForRunner(ctx, "run_target")
	if err != nil {
		t.Fatalf("ListRunningJobsForRunner: %v", err)
	}
	if len(got) != 1 || got[0].ID != want.ID {
		t.Fatalf("got %d jobs %v, want only the in-progress job on that runner", len(got), got)
	}
}

// An empty runner id must not mean "every job with no runner". The caller is
// asking about one container, and a runner row with no id is a bug elsewhere;
// answering it with somebody else's jobs would keep a container alive for ever
// on the strength of work it is not doing.
func TestRunningJobsForNoRunnerIsEmpty(t *testing.T) {
	s := newTestStore(t)
	seedJob(t, s, 6, JobInProgress, "")

	got, err := s.ListRunningJobsForRunner(t.Context(), "")
	if err != nil {
		t.Fatalf("ListRunningJobsForRunner: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("got %d jobs, want none for an empty runner id", len(got))
	}
}

// This query runs on every heartbeat that carries a report for a terminal
// runner, so it must not walk a jobs table that grows without bound.
//
// What keeps it cheap is the equality on state: it is what lets the planner
// seek idx_jobs_state_queued to the in-progress rows, which a fleet has few
// of, and check runner_id across those. Dropping it -- the tempting
// simplification, since runner_id alone reads like the question being asked --
// turns this into a full scan, which is the regression this test exists to
// catch. Clause order is not the point; SQLite reorders those itself.
func TestRunningJobsForARunnerSeeksRatherThanScans(t *testing.T) {
	s := newTestStore(t)
	rows, err := s.read.QueryContext(t.Context(), "EXPLAIN QUERY PLAN "+runningJobsForRunnerSQL, "run_x")
	if err != nil {
		t.Fatalf("EXPLAIN QUERY PLAN: %v", err)
	}
	defer rows.Close()
	var plan []string
	for rows.Next() {
		var id, parent, notused int
		var detail string
		if err := rows.Scan(&id, &parent, &notused, &detail); err != nil {
			t.Fatalf("scanning the plan: %v", err)
		}
		plan = append(plan, detail)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("reading the plan: %v", err)
	}
	joined := strings.Join(plan, "; ")
	if !strings.Contains(joined, "idx_jobs_state_queued") {
		t.Fatalf("plan = %q, want it to use idx_jobs_state_queued", joined)
	}
	if strings.Contains(joined, "SCAN jobs") {
		t.Fatalf("plan = %q, want a seek rather than a scan of the jobs table", joined)
	}
}
