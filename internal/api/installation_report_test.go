package api

import (
	"encoding/csv"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/eyupio/zoomies/internal/auth"
	"github.com/eyupio/zoomies/internal/store"
)

// reportInstallation is an installation whose target is a repository, with
// jobs in two more, so every repository name a report could leak is known.
func reportInstallation(t *testing.T, h *harness) *store.Installation {
	t.Helper()
	inst := h.installation()
	inst.Target, inst.TargetType = "acme/secret-target", store.TargetRepo
	if err := h.st.UpdateInstallation(h.ctx, inst); err != nil {
		t.Fatalf("UpdateInstallation: %v", err)
	}
	pool := &store.Pool{Name: "report", InstallationID: inst.ID, Labels: store.StringSlice{"report"}, Backend: store.BackendDocker,
		MaxRunners: 1, Ephemeral: true, DockerMode: store.DockerNone, Enabled: true}
	if err := h.st.CreatePool(h.ctx, pool); err != nil {
		t.Fatalf("CreatePool: %v", err)
	}
	queued := time.Now().Add(-2 * time.Hour)
	started := queued.Add(time.Minute)
	for i, repo := range []string{"acme/secret-plans", "acme/other-secret"} {
		if _, err := h.st.UpsertJob(h.ctx, &store.Job{GitHubJobID: int64(900 + i), Repo: repo, Workflow: "build", PoolID: pool.ID,
			JobName: "test", Matched: true, InstallationID: inst.ID, State: store.JobCompleted,
			QueuedAt: queued, StartedAt: &started, CompletedAt: &started, RunnerName: "GitHub Actions 2"}); err != nil {
			t.Fatalf("UpsertJob: %v", err)
		}
	}
	return inst
}

var reportSecrets = []string{"acme/secret-plans", "acme/other-secret", "acme/secret-target"}

// A token that may read usage and nothing else is exactly the identity the
// report must not name a repository to: repository names are what jobs.read
// reads, and an installation whose target is a repository is what
// installations.read reads. It still gets every figure.
func TestTheInstallationReportNamesNoRepositoryTheCallerMayNotRead(t *testing.T) {
	h := newHarness(t)
	inst := reportInstallation(t, h)
	narrow := h.token("usage-only", store.RoleViewer, auth.ActionUsageRead.Scope())

	resp := h.do(request{method: http.MethodGet, token: narrow, path: "/api/v1/installations/" + inst.ID + "/report?window=24h"})
	resp.mustStatus(t, http.StatusOK, "installation report for a usage-only token")
	for _, secret := range reportSecrets {
		if strings.Contains(string(resp.body), secret) {
			t.Errorf("the report names %s to a token that may not read it:\n%s", secret, resp.body)
		}
	}
	var body struct {
		Installation installationRef          `json:"installation"`
		Counts       store.InstallationCounts `json:"counts"`
		Unavailable  []string                 `json:"unavailable"`
	}
	resp.into(t, &body)
	if body.Installation.ID != inst.ID || body.Installation.Target != "" {
		t.Errorf("installation = %+v, want the id alone", body.Installation)
	}
	if body.Counts.Observed != 2 || body.Counts.Eligible != 2 || body.Counts.RanElsewhere != 2 {
		t.Errorf("counts = %+v, want both jobs observed, eligible and run elsewhere", body.Counts)
	}
	if body.Unavailable == nil {
		t.Error("unavailable is null; a client should be able to range over it")
	}

	// Installations without jobs is still not enough for a repository target.
	half := h.token("installations-no-jobs", store.RoleViewer, auth.ActionUsageRead.Scope(), auth.ActionInstallationsRead.Scope())
	resp = h.do(request{method: http.MethodGet, token: half, path: "/api/v1/installations/" + inst.ID + "/report"})
	resp.mustStatus(t, http.StatusOK, "installation report without jobs.read")
	if strings.Contains(string(resp.body), "acme/secret-target") {
		t.Errorf("a repository target was shown to a caller who may not read jobs:\n%s", resp.body)
	}

	// A viewer reads all three and is shown the target.
	_, cookie := h.user("viewer", store.RoleViewer)
	resp = h.do(request{method: http.MethodGet, cookie: cookie, path: "/api/v1/installations/" + inst.ID + "/report"})
	resp.mustStatus(t, http.StatusOK, "installation report for a viewer")
	resp.into(t, &body)
	if body.Installation.Target != "acme/secret-target" {
		t.Errorf("a viewer was not shown the target: %+v", body.Installation)
	}
	for _, secret := range reportSecrets[:2] {
		if strings.Contains(string(resp.body), secret) {
			t.Errorf("the report names job repository %s; it is per installation and should name none", secret)
		}
	}
}

// The report is a section of the usage page, so it is gated the same way: a
// token without usage.read is refused, whatever else it may read.
func TestTheInstallationReportNeedsUsageRead(t *testing.T) {
	h := newHarness(t)
	inst := reportInstallation(t, h)
	tok := h.token("no-usage", store.RoleViewer, auth.ActionInstallationsRead.Scope(), auth.ActionJobsRead.Scope())
	resp := h.do(request{method: http.MethodGet, token: tok, path: "/api/v1/installations/" + inst.ID + "/report"})
	resp.mustStatus(t, http.StatusForbidden, "installation report without usage.read")

	_, cookie := h.user("viewer", store.RoleViewer)
	for _, c := range []struct {
		path string
		want int
	}{
		{"/api/v1/installations/inst_missing/report", http.StatusNotFound},
		{"/api/v1/installations/" + inst.ID + "/report?window=soon", http.StatusBadRequest},
		{"/api/v1/installations/" + inst.ID + "/report?window=9000h", http.StatusBadRequest},
	} {
		h.do(request{method: http.MethodGet, cookie: cookie, path: c.path}).mustStatus(t, c.want, c.path)
	}
}

// The export grouped by installation carries the report's counts, and every
// other grouping leaves the columns blank rather than zero, which a
// spreadsheet would sum.
func TestUsageCSVCarriesTheInstallationCounts(t *testing.T) {
	h := newHarness(t)
	inst := reportInstallation(t, h)
	_, cookie := h.user("viewer", store.RoleViewer)
	from, to := time.Now().Add(-24*time.Hour).UTC().Format(time.RFC3339), time.Now().Add(time.Hour).UTC().Format(time.RFC3339)
	read := func(group string) (map[string]int, [][]string) {
		resp := h.do(request{method: http.MethodGet, cookie: cookie,
			path: "/api/v1/usage.csv?group_by=" + group + "&from=" + from + "&to=" + to})
		resp.mustStatus(t, http.StatusOK, "usage csv by "+group)
		rows, err := csv.NewReader(strings.NewReader(string(resp.body))).ReadAll()
		if err != nil || len(rows) < 2 {
			t.Fatalf("csv by %s: %v, %d rows", group, err, len(rows))
		}
		col := map[string]int{}
		for i, name := range rows[0] {
			col[name] = i
		}
		return col, rows[1:]
	}
	col, rows := read("installation")
	var found bool
	for _, row := range rows {
		if row[0] != inst.ID {
			continue
		}
		found = true
		for name, want := range map[string]string{"jobs_observed": "2", "jobs_eligible": "2", "jobs_ran_elsewhere": "2",
			"jobs_ran_here": "0", "jobs_fleet_fault": "0", "cleanup_pending": "0"} {
			if got := row[col[name]]; got != want {
				t.Errorf("%s = %q, want %q", name, got, want)
			}
		}
		if row[col["counts_from"]] == "" {
			t.Error("counts_from is blank on an installation row")
		}
	}
	if !found {
		t.Fatalf("no row for %s in %v", inst.ID, rows)
	}
	col, rows = read("repository")
	for _, row := range rows {
		if row[col["jobs_observed"]] != "" || row[col["counts_from"]] != "" {
			t.Errorf("a repository row carries installation counts: %v", row)
		}
	}
}
