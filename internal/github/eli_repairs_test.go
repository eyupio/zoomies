package github

import (
	"context"
	"encoding/base64"
	"encoding/json"
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
	big := strings.Repeat("x", prrepair.MaxFileBytes+1)
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
	if len(src.Snapshot.Skipped) != 1 || !strings.HasPrefix(src.Snapshot.Skipped[0], "skills/reference.md: ") || !strings.Contains(src.Snapshot.Skipped[0], "32 KiB") {
		t.Errorf("skipped = %v", src.Snapshot.Skipped)
	}
}

func itoa(n int) string { return strconv.Itoa(n) }
