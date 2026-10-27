package github

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"github.com/eyupio/zoomies/internal/prrepair"
	"github.com/eyupio/zoomies/internal/store"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
)

func repairTestClient(t *testing.T, handler http.HandlerFunc) *appClient {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	raw, e := newGitHubClient(srv.Client(), srv.URL+"/", srv.URL+"/")
	if e != nil {
		t.Fatal(e)
	}
	return newAppClient(raw, raw, "acme/repo", store.TargetRepo, 1, "https://github.com")
}

const testPull = `{"number":1,"state":"open","head":{"ref":"feature","sha":"old","repo":{"full_name":"acme/repo","default_branch":"main"}}}`

func TestRepairCommitPinsItsParentAndNeverForcesTheBranch(t *testing.T) {
	writes := 0
	c := repairTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/repos/acme/repo/pulls/1":
			w.Write([]byte(testPull))
		case "/repos/acme/repo/git/commits/old":
			w.Write([]byte(`{"sha":"old","tree":{"sha":"tree"}}`))
		case "/repos/acme/repo/git/trees":
			var body struct {
				BaseTree string `json:"base_tree"`
				Tree     []struct {
					Mode string `json:"mode"`
					Path string `json:"path"`
				}
			}
			if e := json.NewDecoder(r.Body).Decode(&body); e != nil {
				t.Error(e)
			}
			if body.BaseTree != "tree" || len(body.Tree) != 1 || body.Tree[0].Mode != "100755" {
				t.Errorf("tree %+v", body)
			}
			writes++
			w.Write([]byte(`{"sha":"new-tree"}`))
		case "/repos/acme/repo/git/commits":
			var body struct {
				Parents []string `json:"parents"`
			}
			json.NewDecoder(r.Body).Decode(&body)
			if len(body.Parents) != 1 || body.Parents[0] != "old" {
				t.Errorf("parents %+v", body)
			}
			writes++
			w.Write([]byte(`{"sha":"fixed"}`))
		case "/repos/acme/repo/git/refs/heads/feature":
			var body struct {
				SHA   string `json:"sha"`
				Force bool   `json:"force"`
			}
			json.NewDecoder(r.Body).Decode(&body)
			if r.Method != "PATCH" || body.Force || body.SHA != "fixed" {
				t.Errorf("unsafe ref %+v", body)
			}
			writes++
			w.Write([]byte(`{"ref":"refs/heads/feature","object":{"sha":"fixed"}}`))
		default:
			t.Errorf("unexpected %s %s", r.Method, r.URL)
			http.NotFound(w, r)
		}
	})
	sha, e := c.CommitRepair(context.Background(), "acme/repo", RepairPull{Number: 1, Branch: "feature", HeadSHA: "old"}, prrepair.Plan{Files: []prrepair.File{{Path: "run.sh", Content: "exit 0", Mode: "100755"}}})
	if e != nil || sha != "fixed" || writes != 3 {
		t.Fatalf("sha %s writes %d: %v", sha, writes, e)
	}
}
func TestRepairRefusesAPushWhenThePRHeadChanged(t *testing.T) {
	c := repairTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" {
			t.Fatal("wrote after head changed")
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(strings.ReplaceAll(testPull, `"old"`, `"new"`)))
	})
	_, e := c.CommitRepair(context.Background(), "acme/repo", RepairPull{Number: 1, Branch: "feature", HeadSHA: "old"}, prrepair.Plan{})
	if e == nil {
		t.Fatal("stale PR accepted")
	}
}
func TestRepairChecksIncludeLegacyStatusesAndWaitForAnyChecks(t *testing.T) {
	for _, tc := range []struct{ checks, status, want string }{
		{`{"total_count":0,"check_runs":[]}`, `{"total_count":0,"state":"pending"}`, "waiting"},
		{`{"total_count":1,"check_runs":[{"status":"completed","conclusion":"success"}]}`, `{"total_count":1,"state":"failure"}`, "failed"},
		{`{"total_count":1,"check_runs":[{"status":"completed","conclusion":"success"}]}`, `{"total_count":1,"state":"pending"}`, "waiting"},
		{`{"total_count":1,"check_runs":[{"status":"completed","conclusion":"success"}]}`, `{"total_count":0,"state":"pending"}`, "passed"},
		{`{"total_count":1,"check_runs":[{"status":"in_progress"}]}`, `{"total_count":1,"state":"success"}`, "waiting"},
		{`{"total_count":1,"check_runs":[{"status":"completed","conclusion":"cancelled"}]}`, `{"total_count":0,"state":"pending"}`, "waiting"},
		{`{"total_count":1,"check_runs":[{"status":"completed","conclusion":"skipped"}]}`, `{"total_count":0,"state":"pending"}`, "waiting"},
		{`{"total_count":1,"check_runs":[{"status":"completed","conclusion":"neutral"}]}`, `{"total_count":0,"state":"pending"}`, "waiting"},
		{`{"total_count":2,"check_runs":[{"status":"in_progress"},{"status":"completed","conclusion":"failure"}]}`, `{"total_count":0,"state":"pending"}`, "failed"},
		{`{"total_count":2,"check_runs":[{"status":"completed","conclusion":"success"}]}`, `{"total_count":0,"state":"pending"}`, "waiting"},
	} {
		t.Run(tc.want, func(t *testing.T) {
			c := repairTestClient(t, func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				if strings.HasSuffix(r.URL.Path, "/status") {
					w.Write([]byte(tc.status))
				} else {
					w.Write([]byte(tc.checks))
				}
			})
			got, e := c.RepairChecks(context.Background(), "acme/repo", "sha")
			if e != nil || got != tc.want {
				t.Fatalf("%s %v", got, e)
			}
		})
	}
}
func TestRepairSourceRefusesSymlinksAndSubmodules(t *testing.T) {
	s := &RepairSource{files: map[string]repairTreeEntry{"link": {Type: "blob", Mode: "120000"}, "sub": {Type: "commit", Mode: "160000"}}}
	for _, path := range []string{"link", "link/file", "sub", "sub/file"} {
		if _, e := s.ReadFile(context.Background(), path); e == nil {
			t.Errorf("read %s", path)
		}
	}
}

// A pull request that changes a generated file of a few thousand lines used
// to stop the whole repair with "could not be read within Eli's repair
// limits", naming nothing. A changed file Eli may not have (too large, or not
// text) is left out and named to the model as unavailable, the rest of the
// source is read, and the repair goes on; the same when the source as a whole
// reaches its budget. Only GitHub refusing is a failure.
func TestRepairSourceLeavesOutAFileItMayNotHaveAndNamesIt(t *testing.T) {
	big := strings.Repeat("x", prrepair.MaxBlobBytes+1)
	blob := func(content string) string {
		return `{"encoding":"base64","size":` + itoa(len(content)) + `,"content":"` + base64.StdEncoding.EncodeToString([]byte(content)) + `"}`
	}
	c := repairTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/repos/acme/repo/git/commits/old":
			w.Write([]byte(`{"sha":"old","tree":{"sha":"tree"}}`))
		case "/repos/acme/repo/git/trees/tree":
			w.Write([]byte(`{"truncated":false,"tree":[{"path":"main.go","type":"blob","mode":"100644","sha":"b1"},{"path":"skills/reference.md","type":"blob","mode":"100644","sha":"b2"}]}`))
		case "/repos/acme/repo/pulls/1/files":
			w.Write([]byte(`[{"filename":"skills/reference.md","status":"modified"},{"filename":"main.go","status":"modified"}]`))
		case "/repos/acme/repo/git/blobs/b1":
			w.Write([]byte(blob("package main\n")))
		case "/repos/acme/repo/git/blobs/b2":
			w.Write([]byte(blob(big)))
		default:
			t.Errorf("unexpected request %s", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	})
	src, err := c.RepairSource(context.Background(), "acme/repo", RepairPull{Number: 1, HeadSHA: "old", Title: "t", Branch: "feature"}, false)
	if err != nil {
		t.Fatalf("the repair stopped: %v", err)
	}
	if len(src.Snapshot.Files) != 1 || src.Snapshot.Files[0].Path != "main.go" {
		t.Errorf("files read = %+v", src.Snapshot.Files)
	}
	if len(src.Snapshot.Skipped) != 1 || !strings.HasPrefix(src.Snapshot.Skipped[0], "skills/reference.md: ") || !strings.Contains(src.Snapshot.Skipped[0], "512 KiB") {
		t.Errorf("skipped = %v", src.Snapshot.Skipped)
	}
}

func itoa(n int) string { return strconv.Itoa(n) }

// GitHub omits PR links on some runs. Commit lookup must recover the PR
// without admitting a closed PR, a fork or a different head.
func TestRepairRunResolvesMissingPullLinksAndKeepsAdmissionChecks(t *testing.T) {
	for _, tc := range []struct {
		name, candidates, pull, message string
	}{
		{"open PR", `[{"number":1}]`, testPull, ""},
		{"missing PR state", `[{"number":1}]`, strings.Replace(testPull, `"open"`, `""`, 1), "open PR"},
		{"closed PR", `[{"number":1}]`, strings.Replace(testPull, `"open"`, `"closed"`, 1), "PR #1 is closed or merged"},
		{"stale head", `[{"number":1}]`, strings.Replace(testPull, `"old"`, `"new"`, 1), "newer commits"},
		{"no PR", `[]`, "", "single PR"},
		{"ambiguous commit", `[{"number":1},{"number":2}]`, "", "single PR"},
		{"fork PR", `[{"number":1}]`, strings.Replace(testPull, "acme/repo", "other/repo", 1), "forks"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := repairTestClient(t, func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodGet {
					t.Errorf("readiness wrote: %s", r.Method)
				}
				w.Header().Set("Content-Type", "application/json")
				switch r.URL.Path {
				case "/repos/acme/repo/actions/runs/9":
					w.Write([]byte(`{"head_sha":"old","event":"pull_request","head_repository":{"full_name":"acme/repo"},"pull_requests":[]}`))
				case "/repos/acme/repo/commits/old/pulls":
					if r.URL.Query().Get("per_page") != "2" {
						t.Error("commit lookup must be bounded and detect ambiguity")
					}
					w.Write([]byte(tc.candidates))
				case "/repos/acme/repo/pulls/1":
					w.Write([]byte(tc.pull))
				default:
					t.Errorf("unexpected request %s", r.URL)
					http.NotFound(w, r)
				}
			})
			p, err := c.RepairPullForRun(context.Background(), "acme/repo", 9)
			if tc.message != "" {
				if !errors.Is(err, ErrRepairUnsupported) || !strings.Contains(err.Error(), tc.message) {
					t.Fatalf("pull %+v, error %v; want %q", p, err, tc.message)
				}
			} else if err != nil || p.Number != 1 || p.HeadSHA != "old" {
				t.Fatalf("pull %+v, error %v", p, err)
			}
		})
	}
}

// A repair page names the pull request it is about, and where it is going. The
// base is not the repository's default branch: a pull request can target a
// release branch, and the page must say which.
func TestRepairPullReadsTitleAuthorBranchAndBase(t *testing.T) {
	c := repairTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path != "/repos/acme/repo/pulls/7" {
			http.NotFound(w, r)
			return
		}
		w.Write([]byte(`{"number":7,"state":"open","title":"Retry transient 502s","html_url":"https://github.com/acme/repo/pull/7","user":{"id":9,"login":"dana"},"head":{"ref":"fix/retry","sha":"abc","repo":{"full_name":"acme/repo","default_branch":"main"}},"base":{"ref":"release/1.2"}}`))
	})
	got, err := c.RepairPull(t.Context(), "acme/repo", 7)
	if err != nil {
		t.Fatal(err)
	}
	want := RepairPull{
		Number: 7, AuthorID: 9, Author: "dana", Branch: "fix/retry", Base: "release/1.2",
		HeadSHA: "abc", Title: "Retry transient 502s", URL: "https://github.com/acme/repo/pull/7",
		DefaultBranch: "main", Open: true,
	}
	if got != want {
		t.Fatalf("got  %+v\nwant %+v", got, want)
	}
}

// A push, schedule or manual run on the default branch has no PR of its own,
// but GitHub answers a commit lookup with the PR whose merge made that commit.
// Taking that PR for the run's own settled the failure as "closed" on the next
// tick, so the first red build on main after a merge left the attention list
// without a word, with nobody having seen it. Only a run a PR started may be
// resolved by commit, and the lookup is not even made for any other.
func TestARunNoPRStartedIsNeverTakenForTheMergedPRThatMadeItsCommit(t *testing.T) {
	for _, event := range []string{"push", "schedule", "workflow_dispatch", "merge_group"} {
		t.Run(event, func(t *testing.T) {
			c := repairTestClient(t, func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				switch r.URL.Path {
				case "/repos/acme/repo/actions/runs/9":
					w.Write([]byte(`{"head_sha":"merge","head_branch":"main","event":"` + event + `","head_repository":{"full_name":"acme/repo"},"pull_requests":[]}`))
				case "/repos/acme/repo/commits/merge/pulls":
					t.Error("a run no PR started was looked up by commit")
					w.Write([]byte(`[{"number":1}]`))
				case "/repos/acme/repo/pulls/1":
					w.Write([]byte(`{"number":1,"state":"closed","head":{"ref":"feature","sha":"prtip","repo":{"full_name":"acme/repo","default_branch":"main"}}}`))
				default:
					t.Errorf("unexpected request %s", r.URL)
					http.NotFound(w, r)
				}
			})
			_, err := c.RepairPullForRun(context.Background(), "acme/repo", 9)
			var changed *RepairTargetChanged
			if errors.As(err, &changed) {
				t.Fatalf("a %s run was settled as %q: %s", event, changed.Code, changed.Message)
			}
			if !errors.Is(err, ErrRepairUnsupported) || !strings.Contains(err.Error(), "not started by a PR") {
				t.Fatalf("error %v; want an unsupported target that says no PR started the run", err)
			}
		})
	}
}

// The gate is on the lookup by commit, not on the event: a push to a PR's own
// branch that GitHub links to the PR is that PR's run, and is resolved from
// the link as before, closed PR and all.
func TestAPushRunGitHubLinksToItsPRIsStillResolved(t *testing.T) {
	c := repairTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/repos/acme/repo/actions/runs/9":
			w.Write([]byte(`{"head_sha":"old","head_branch":"feature","event":"push","head_repository":{"full_name":"acme/repo"},"pull_requests":[{"number":1}]}`))
		case "/repos/acme/repo/pulls/1":
			w.Write([]byte(strings.Replace(testPull, `"open"`, `"closed"`, 1)))
		default:
			t.Errorf("unexpected request %s", r.URL)
			http.NotFound(w, r)
		}
	})
	_, err := c.RepairPullForRun(context.Background(), "acme/repo", 9)
	var changed *RepairTargetChanged
	if !errors.As(err, &changed) || changed.Code != "closed" {
		t.Fatalf("error %v; want the PR the run is linked to reported closed", err)
	}
}

func TestRepairChecksKeepConfirmedFailuresWhenAnotherEndpointIsUnavailable(t *testing.T) {
	for _, tc := range []struct{ name, checks, status, runs, want string }{
		{"status denied", `{"total_count":1,"check_runs":[{"status":"completed","conclusion":"failure"}]}`, "", "", "failed"},
		{"checks denied", "", `{"total_count":1,"state":"failure"}`, "", "failed"},
		{"actions fallback", "", "", `{"workflow_runs":[{"workflow_id":1,"head_sha":"sha","status":"completed","conclusion":"failure"}]}`, "failed"},
		{"actions timed out", "", "", `{"workflow_runs":[{"workflow_id":1,"head_sha":"sha","status":"completed","conclusion":"timed_out"}]}`, "failed"},
		{"all endpoints denied", "", "", "", ""},
		{"pending rerun", "", "", `{"workflow_runs":[{"workflow_id":1,"head_sha":"sha","status":"in_progress"},{"workflow_id":1,"head_sha":"sha","status":"completed","conclusion":"failure"}]}`, ""},
		{"different workflows", "", "", `{"workflow_runs":[{"workflow_id":1,"head_sha":"sha","status":"completed","conclusion":"success"},{"workflow_id":2,"head_sha":"sha","status":"completed","conclusion":"failure"}]}`, "failed"},
		{"wrong head", "", "", `{"workflow_runs":[{"workflow_id":1,"head_sha":"old","status":"completed","conclusion":"failure"}]}`, ""},
		{"successful rerun", "", "", `{"workflow_runs":[{"workflow_id":1,"head_sha":"sha","status":"completed","conclusion":"success"},{"workflow_id":1,"head_sha":"sha","status":"completed","conclusion":"failure"}]}`, ""},
		{"incomplete success", `{"total_count":1,"check_runs":[{"status":"completed","conclusion":"success"}]}`, "", `{"workflow_runs":[]}`, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := repairTestClient(t, func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				var body string
				switch {
				case strings.HasSuffix(r.URL.Path, "/check-runs"):
					body = tc.checks
				case strings.HasSuffix(r.URL.Path, "/status"):
					body = tc.status
				case strings.HasSuffix(r.URL.Path, "/actions/runs"):
					if r.URL.Query().Get("head_sha") != "sha" {
						t.Error("Actions lookup was not pinned to the PR head")
					}
					body = tc.runs
				default:
					t.Errorf("unexpected request %s", r.URL)
				}
				if body == "" {
					w.WriteHeader(http.StatusForbidden)
					w.Write([]byte(`{"message":"Resource not accessible by integration"}`))
					return
				}
				w.Write([]byte(body))
			})
			got, err := c.RepairChecks(context.Background(), "acme/repo", "sha")
			if got != tc.want || (err != nil) != (tc.want == "") {
				t.Fatalf("got %q, %v; want %q", got, err, tc.want)
			}
		})
	}
}
