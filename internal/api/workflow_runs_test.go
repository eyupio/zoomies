package api

import (
	"net/http"
	"testing"
	"time"

	"github.com/eyupio/zoomies/internal/store"
)

// The Workflows page lists runs and opens each to its jobs, and both halves
// come from the same rows: a run's row sums the jobs GET /jobs?run_id= lists
// under it, so the two cannot disagree about how the run is getting on.
func TestWorkflowRunsAreListedAndOpenToTheirJobs(t *testing.T) {
	h := newHarness(t)
	inst := h.installation()
	pool := h.pool(inst, "linux")
	running := h.job(pool, store.JobInProgress) // run 1, queued a minute ago

	queued := time.Now().Add(-2 * time.Minute)
	started := queued.Add(time.Second)
	done := queued.Add(30 * time.Second)
	for _, j := range []*store.Job{
		{GitHubJobID: 201, JobName: "build", State: store.JobCompleted, Conclusion: "success"},
		{GitHubJobID: 202, JobName: "test", State: store.JobCompleted, Conclusion: "failure"},
	} {
		j.GitHubRunID, j.RunNumber, j.RunAttempt = 2, 42, 1
		j.Repo, j.Workflow, j.HeadBranch = "acme/widgets", "ci", "main"
		j.Labels = store.StringSlice{"self-hosted", "linux", "x64"}
		j.InstallationID, j.PoolID, j.Matched = pool.InstallationID, pool.ID, true
		j.QueuedAt, j.StartedAt, j.CompletedAt = queued, &started, &done
		j.HTMLURL = "https://github.com/acme/widgets/actions/runs/2/job/" + j.JobName
		if _, err := h.st.UpsertJob(h.ctx, j); err != nil {
			t.Fatalf("recording %s: %v", j.JobName, err)
		}
	}
	_, cookie := h.user("viewer", store.RoleViewer)

	type run struct {
		GitHubRunID int64  `json:"github_run_id"`
		RunNumber   int64  `json:"run_number"`
		State       string `json:"state"`
		Conclusion  string `json:"conclusion"`
		HTMLURL     string `json:"html_url"`
		Managed     bool   `json:"managed"`
		Jobs        struct {
			Total     int `json:"total"`
			Succeeded int `json:"succeeded"`
			Failed    int `json:"failed"`
		} `json:"jobs"`
		DurationMS int64 `json:"duration_ms"`
	}
	var page struct {
		Items []run `json:"items"`
		Total int   `json:"total"`
	}
	resp := h.do(request{method: http.MethodGet, path: "/api/v1/workflow-runs?sort=queued_at&order=asc", cookie: cookie})
	resp.mustStatus(t, http.StatusOK, "listing workflow runs")
	resp.into(t, &page)
	if page.Total != 2 || len(page.Items) != 2 {
		t.Fatalf("runs = %+v (total %d), want the two runs", page.Items, page.Total)
	}
	failed := page.Items[0]
	if failed.GitHubRunID != 2 || failed.RunNumber != 42 || failed.State != "completed" || failed.Conclusion != "failure" {
		t.Fatalf("first run = %+v, want run 2 (#42) completed as a failure", failed)
	}
	if failed.Jobs.Total != 2 || failed.Jobs.Succeeded != 1 || failed.Jobs.Failed != 1 || !failed.Managed {
		t.Fatalf("run 2 = %+v, want two jobs, one each way, on this fleet", failed)
	}
	if failed.HTMLURL != "https://github.com/acme/widgets/actions/runs/2" {
		t.Fatalf("run 2 links to %q, want the run's own page", failed.HTMLURL)
	}
	if failed.DurationMS != 29_000 {
		t.Fatalf("run 2 took %dms, want the 29s from first start to last finish", failed.DurationMS)
	}
	if got := page.Items[1]; got.GitHubRunID != running.GitHubRunID || got.State != "in_progress" || got.Conclusion != "" {
		t.Fatalf("second run = %+v, want run 1 still in progress", got)
	}

	// The status views name the run's own status.
	for _, tc := range []struct {
		query string
		want  int64
	}{
		{"failed=true", 2},
		{"state=in_progress", 1},
		{"conclusion=failure", 2},
	} {
		resp := h.do(request{method: http.MethodGet, path: "/api/v1/workflow-runs?" + tc.query, cookie: cookie})
		resp.mustStatus(t, http.StatusOK, "listing runs by "+tc.query)
		resp.into(t, &page)
		if page.Total != 1 || len(page.Items) != 1 || page.Items[0].GitHubRunID != tc.want {
			t.Errorf("%s: runs = %+v (total %d), want run %d alone", tc.query, page.Items, page.Total, tc.want)
		}
	}

	// Opening a run is the job listing narrowed to it.
	var jobs struct {
		Items []struct {
			GitHubRunID int64  `json:"github_run_id"`
			JobName     string `json:"job_name"`
		} `json:"items"`
		Total int `json:"total"`
	}
	resp = h.do(request{method: http.MethodGet, path: "/api/v1/jobs?repo=acme/widgets&run_id=2&sort=queued_at&order=asc", cookie: cookie})
	resp.mustStatus(t, http.StatusOK, "listing a run's jobs")
	resp.into(t, &jobs)
	if jobs.Total != 2 || len(jobs.Items) != 2 || jobs.Items[0].GitHubRunID != 2 || jobs.Items[1].GitHubRunID != 2 {
		t.Fatalf("jobs of run 2 = %+v (total %d), want its two", jobs.Items, jobs.Total)
	}

	// A run ID that is not one is refused rather than matching nothing.
	h.do(request{method: http.MethodGet, path: "/api/v1/jobs?run_id=latest", cookie: cookie}).
		mustStatus(t, http.StatusBadRequest, "a run ID that is not a number")
	h.do(request{method: http.MethodGet, path: "/api/v1/workflow-runs?state=running", cookie: cookie}).
		mustStatus(t, http.StatusBadRequest, "a state GitHub does not have")
	h.do(request{method: http.MethodGet, path: "/api/v1/workflow-runs"}).
		mustStatus(t, http.StatusUnauthorized, "listing runs without signing in")
}
