package api

import (
	"net/http"
	"testing"
	"time"

	"github.com/eyupio/zoomies/internal/store"
)

// The Jobs page asks for managed=true by default, so the parameter has to reach
// the store: GitHub reports every job in an installed repository, and a fleet
// view that silently includes hosted-runner jobs answers "how is my fleet
// doing?" with somebody else's numbers.
func TestListJobsManagedLeavesOutJobsThisFleetNeverRan(t *testing.T) {
	h := newHarness(t)
	inst := h.installation()
	pool := h.pool(inst, "linux")
	mine := h.job(pool, store.JobCompleted)

	now := time.Now().Add(-time.Minute)
	started := now.Add(time.Second)
	done := now.Add(time.Second * 30)
	if _, err := h.st.UpsertJob(h.ctx, &store.Job{
		GitHubJobID: 99, GitHubRunID: 99, Repo: "acme/widgets", Workflow: "ci",
		JobName: "hosted", Labels: store.StringSlice{"ubuntu-latest"},
		State: store.JobCompleted, Conclusion: "success",
		QueuedAt: now, StartedAt: &started, CompletedAt: &done,
	}); err != nil {
		t.Fatalf("recording the hosted job: %v", err)
	}

	_, cookie := h.user("viewer", store.RoleViewer)

	var page struct {
		Items []struct {
			ID      string `json:"id"`
			JobName string `json:"job_name"`
		} `json:"items"`
		Total int `json:"total"`
	}
	resp := h.do(request{method: "GET", path: "/api/v1/jobs?managed=true", cookie: cookie})
	resp.mustStatus(t, 200, "listing managed jobs")
	resp.into(t, &page)
	if page.Total != 1 || len(page.Items) != 1 || page.Items[0].ID != mine.ID {
		t.Fatalf("managed jobs = %+v (total %d), want only %s", page.Items, page.Total, mine.ID)
	}

	resp = h.do(request{method: "GET", path: "/api/v1/jobs", cookie: cookie})
	resp.mustStatus(t, 200, "listing every job")
	resp.into(t, &page)
	if page.Total != 2 {
		t.Fatalf("unfiltered total = %d, want both jobs -- the toggle has to be able to show them", page.Total)
	}
}

// The drawer's timeline and the failed filter are the two things this change
// gives an operator, and both have to be reachable over the API the UI uses.
func TestJobTimelineAndFailedFilterAreServed(t *testing.T) {
	h := newHarness(t)
	inst := h.installation()
	pool := h.pool(inst, "linux")
	green := h.job(pool, store.JobCompleted)
	red := h.job(pool, store.JobInProgress)
	if _, _, err := h.st.SetJobRunnerFault(h.ctx, red.ID, "runner zoomies-x stopped while this job was running: exited with code 137"); err != nil {
		t.Fatalf("SetJobRunnerFault: %v", err)
	}
	if err := h.st.AppendJobEvent(h.ctx, &store.JobEvent{JobID: red.ID, Kind: store.JobEventQueued, Source: "webhook", Message: "GitHub queued it"}); err != nil {
		t.Fatalf("AppendJobEvent: %v", err)
	}
	if err := h.st.AppendJobEvent(h.ctx, &store.JobEvent{JobID: red.ID, Kind: store.JobEventRunnerLost, Source: "agent", Message: "the runner stopped", RunnerName: "zoomies-x"}); err != nil {
		t.Fatalf("AppendJobEvent: %v", err)
	}
	_, cookie := h.user("viewer", store.RoleViewer)

	var timeline struct {
		Items []struct {
			Kind       string `json:"kind"`
			Source     string `json:"source"`
			Message    string `json:"message"`
			RunnerName string `json:"runner_name"`
			At         string `json:"at"`
		} `json:"items"`
	}
	resp := h.do(request{method: "GET", path: "/api/v1/jobs/" + red.ID + "/events", cookie: cookie})
	resp.mustStatus(t, 200, "reading the timeline")
	resp.into(t, &timeline)
	if len(timeline.Items) != 2 || timeline.Items[0].Kind != "queued" || timeline.Items[1].Kind != "runner_lost" {
		t.Fatalf("timeline = %+v, want queued then runner_lost", timeline.Items)
	}
	if timeline.Items[1].RunnerName != "zoomies-x" || timeline.Items[1].At == "" {
		t.Fatalf("runner_lost entry = %+v, want the runner named and a timestamp", timeline.Items[1])
	}

	resp = h.do(request{method: "GET", path: "/api/v1/jobs/" + green.ID + "/events", cookie: cookie})
	resp.mustStatus(t, 200, "a job with no history")
	resp.into(t, &timeline)
	if timeline.Items == nil || len(timeline.Items) != 0 {
		t.Fatalf("an empty timeline should be an empty list, got %+v", timeline.Items)
	}

	resp = h.do(request{method: "GET", path: "/api/v1/jobs/job_missing/events", cookie: cookie})
	resp.mustStatus(t, 404, "a timeline for a job that does not exist")

	var page struct {
		Items []struct {
			ID          string `json:"id"`
			RunnerFault string `json:"runner_fault"`
			Steps       []any  `json:"steps"`
			FailedStep  *struct {
				Name string `json:"name"`
			} `json:"failed_step"`
		} `json:"items"`
		Total int `json:"total"`
	}
	resp = h.do(request{method: "GET", path: "/api/v1/jobs?failed=true", cookie: cookie})
	resp.mustStatus(t, 200, "listing failed jobs")
	resp.into(t, &page)
	if page.Total != 1 || len(page.Items) != 1 || page.Items[0].ID != red.ID {
		t.Fatalf("failed jobs = %+v (total %d), want only %s", page.Items, page.Total, red.ID)
	}
	if page.Items[0].RunnerFault == "" || page.Items[0].Steps == nil || page.Items[0].FailedStep != nil {
		t.Fatalf("failed job = %+v, want its fault, an empty steps list and no failed step", page.Items[0])
	}
}

// Which installation a job belongs to is now half of why a pool can or cannot
// run it, so the answer has to be on the wire: the Jobs page explains an
// unclaimed job from the row it was handed, and a field the server keeps to
// itself is a field the page cannot say anything about.
func TestAJobCarriesTheInstallationItBelongsTo(t *testing.T) {
	h := newHarness(t)
	inst := h.installation()
	pool := h.pool(inst, "linux")
	mine := h.job(pool, store.JobQueued)
	_, cookie := h.user("viewer", store.RoleViewer)

	var out struct {
		ID             string `json:"id"`
		InstallationID string `json:"installation_id"`
	}
	resp := h.do(request{method: "GET", path: "/api/v1/jobs/" + mine.ID, cookie: cookie})
	resp.mustStatus(t, 200, "getting a job")
	resp.into(t, &out)
	if out.InstallationID != inst.ID {
		t.Fatalf("installation_id = %q, want %q", out.InstallationID, inst.ID)
	}
}

// The explanation is its own route rather than a field on the job, so this is
// the check that it is reachable, authorised like the rest of the jobs surface,
// and carries the shape the drawer and the CLI will both render.
func TestTheJobExplanationIsItsOwnRoute(t *testing.T) {
	h := newHarness(t)
	inst := h.installation()
	h.host("vm-1")
	pool := h.pool(inst, "linux-x64")
	_ = pool

	job, err := h.st.UpsertJob(h.ctx, &store.Job{
		GitHubJobID: 7001, Repo: "acme/widgets", Workflow: "CI", JobName: "build",
		Labels: store.NormalizeLabels([]string{"self-hosted", "cuda12"}),
		State:  store.JobQueued, QueuedAt: time.Now().Add(-time.Minute),
	})
	if err != nil {
		t.Fatalf("UpsertJob: %v", err)
	}

	u, _ := h.user("viewer", store.RoleViewer)
	resp := h.do(request{method: http.MethodGet, path: "/api/v1/jobs/" + job.ID + "/explanation", cookie: h.session(u)})
	resp.mustStatus(t, http.StatusOK, "explanation")
	var out struct {
		JobID   string `json:"job_id"`
		State   string `json:"state"`
		Summary string `json:"summary"`
		Fix     string `json:"fix"`
		Waiting bool   `json:"waiting"`
		Blocked bool   `json:"blocked"`
	}
	resp.into(t, &out)

	if out.JobID != job.ID || out.State != string(store.JobQueued) {
		t.Errorf("the explanation is not about the job asked for: %+v", out)
	}
	// A viewer can read it, and what they read is an answer rather than an
	// empty string they have to interpret.
	if out.Summary == "" {
		t.Error("the explanation has no summary; the page that renders it would show nothing")
	}
	if !out.Blocked || out.Fix == "" {
		t.Errorf("a job no pool claims should be blocked with something to do: %+v", out)
	}

	// A job that does not exist is a 404 rather than an explanation of nothing.
	missing := h.do(request{method: http.MethodGet, path: "/api/v1/jobs/job_nope/explanation", cookie: h.session(u)})
	missing.mustStatus(t, http.StatusNotFound, "explanation for a missing job")
}
