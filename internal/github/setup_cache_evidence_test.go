package github

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/eyupio/zoomies/internal/store"
)

type cacheSourceFixture struct {
	workflowPath, workflow    string
	lockfilePath, lockfile    string
	head, branch              string
	runID                     int64
	attempt                   int
	event, status, conclusion string
	treeTruncated             bool
	contentsFail              string
	extraLockfiles            []string
}

func cacheSourceDefault() cacheSourceFixture {
	return cacheSourceFixture{
		workflowPath: ".github/workflows/ci.yml",
		workflow:     "on: push\njobs:\n  build:\n    name: Build\n    runs-on: ubuntu-latest\n    steps:\n      - name: Checkout\n        uses: actions/checkout@" + strings.Repeat("a", 40) + "\n      - name: Node\n        uses: actions/setup-node@" + strings.Repeat("b", 40) + "\n        with:\n          node-version: 20\n",
		lockfilePath: "package-lock.json", lockfile: `{"lockfileVersion":3}`,
		head: strings.Repeat("a", 40), branch: "main", runID: 10, attempt: 2,
		event: "push", status: "completed", conclusion: "success",
	}
}

func (f cacheSourceFixture) blob(content string) string { return contextGitBlobSHA(content) }

func cacheSourceServer(t *testing.T, f cacheSourceFixture, runs ...string) *httptest.Server {
	t.Helper()
	if len(runs) == 0 {
		run := map[string]any{"id": f.runID, "run_attempt": f.attempt, "repository": map[string]any{"full_name": "acme/repo"}, "status": f.status, "conclusion": f.conclusion, "event": f.event, "head_sha": f.head, "head_branch": f.branch, "path": f.workflowPath}
		b, _ := json.Marshal(run)
		runs = []string{string(b)}
	}
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.Contains(r.URL.Path, "/actions/runs/"+strconv.FormatInt(f.runID, 10)+"/attempts/"+strconv.Itoa(f.attempt)):
			i := int(calls.Add(1)) - 1
			if i >= len(runs) {
				i = len(runs) - 1
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(runs[i]))
		case strings.Contains(r.URL.Path, "/git/trees/"):
			entries := []map[string]any{}
			for _, e := range []struct{ path, content string }{{f.workflowPath, f.workflow}, {f.lockfilePath, f.lockfile}} {
				if e.path == "" || e.content == "" {
					continue
				}
				entries = append(entries, map[string]any{"path": e.path, "type": "blob", "mode": "100644", "sha": f.blob(e.content), "size": len(e.content)})
			}
			for _, path := range f.extraLockfiles {
				entries = append(entries, map[string]any{"path": path, "type": "blob", "mode": "100644", "sha": f.blob("{}"), "size": 2})
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{"sha": strings.Repeat("c", 40), "truncated": f.treeTruncated, "tree": entries})
		case strings.HasPrefix(r.URL.Path, "/repos/acme/repo/contents/"):
			path := strings.TrimPrefix(r.URL.Path, "/repos/acme/repo/contents/")
			content, ok := map[string]string{f.workflowPath: f.workflow, f.lockfilePath: f.lockfile}[path]
			if !ok || f.contentsFail == path {
				http.NotFound(w, r)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{"type": "file", "path": path, "encoding": "base64", "size": len(content), "sha": f.blob(content), "content": base64.StdEncoding.EncodeToString([]byte(content))})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func cacheSourceClient(t *testing.T, srv *httptest.Server) *appClient {
	t.Helper()
	c, err := newGitHubClient(srv.Client(), srv.URL+"/", srv.URL+"/")
	if err != nil {
		t.Fatal(err)
	}
	return newAppClient(c, c, "acme/repo", store.TargetRepo, 42, "https://github.com")
}

func TestReadSetupCacheSourceBindsTheWorkflowAndLockfile(t *testing.T) {
	f := cacheSourceDefault()
	srv := cacheSourceServer(t, f)
	src, err := cacheSourceClient(t, srv).ReadSetupCacheSource(context.Background(), "acme/repo", SetupCacheSelection{RunID: f.runID, Attempt: f.attempt})
	if err != nil {
		t.Fatal(err)
	}
	if src.Version != "setup-cache-evidence-v1" || src.Repository != "acme/repo" || src.RunID != "10" || src.Attempt != 2 || src.HeadSHA != f.head || src.HeadBranch != "main" || src.WorkflowPath != f.workflowPath || src.Workflow.Content != f.workflow || src.WorkflowBlobSHA != f.blob(f.workflow) || src.LockfilePath != f.lockfilePath || src.Lockfile.Mode != "100644" || src.Lockfile.Content != f.lockfile || src.JobIndex != 0 || src.StepIndex != 1 || src.JobName != "Build" || src.StepName != "Node" {
		t.Fatalf("source %+v", src)
	}
	if !ValidSetupCacheSource(src, "acme/repo", SetupCacheSelection{RunID: f.runID, Attempt: f.attempt}) {
		t.Fatal("source failed its own shape check")
	}
}

func TestReadSetupCacheSourceRefusesUnsupportedEvidence(t *testing.T) {
	for _, tc := range []struct {
		name string
		edit func(*cacheSourceFixture)
		runs []string
	}{
		{"non-push", func(f *cacheSourceFixture) { f.event = "pull_request" }, nil},
		{"failed", func(f *cacheSourceFixture) { f.conclusion = "failure" }, nil},
		{"truncated tree", func(f *cacheSourceFixture) { f.treeTruncated = true }, nil},
		{"no lockfile", func(f *cacheSourceFixture) { f.lockfile = "" }, nil},
		{"two lockfiles", func(f *cacheSourceFixture) { f.extraLockfiles = []string{"yarn.lock"} }, nil},
		{"unnamed step", func(f *cacheSourceFixture) { f.workflow = strings.Replace(f.workflow, "      - name: Node\n", "", 1) }, nil},
		{"two setup steps", func(f *cacheSourceFixture) {
			f.workflow = strings.Replace(f.workflow, "      - name: Node\n", "      - name: Node\n        uses: actions/setup-node@"+strings.Repeat("c", 40)+"\n        with:\n          node-version: 20\n      - name: Node\n", 1)
		}, nil},
		{"changed run", func(f *cacheSourceFixture) {}, []string{func() string {
			f := cacheSourceDefault()
			return `{"id":10,"run_attempt":2,"repository":{"full_name":"acme/repo"},"status":"completed","conclusion":"success","event":"push","head_sha":"` + f.head + `","head_branch":"main","path":"` + f.workflowPath + `"}`
		}(), `{"id":10,"run_attempt":2,"repository":{"full_name":"acme/repo"},"status":"completed","conclusion":"success","event":"push","head_sha":"` + strings.Repeat("d", 40) + `","head_branch":"main","path":".github/workflows/ci.yml"}`}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := cacheSourceDefault()
			tc.edit(&f)
			srv := cacheSourceServer(t, f, tc.runs...)
			_, err := cacheSourceClient(t, srv).ReadSetupCacheSource(context.Background(), "acme/repo", SetupCacheSelection{RunID: f.runID, Attempt: f.attempt})
			if err == nil {
				t.Fatal("accepted")
			}
		})
	}
}

func TestReadSetupCacheSourceRefusesAContentsReadFailure(t *testing.T) {
	f := cacheSourceDefault()
	f.contentsFail = f.workflowPath
	srv := cacheSourceServer(t, f)
	_, err := cacheSourceClient(t, srv).ReadSetupCacheSource(context.Background(), "acme/repo", SetupCacheSelection{RunID: f.runID, Attempt: f.attempt})
	if err == nil || errors.Is(err, ErrSetupCacheNotFound) || errors.Is(err, ErrSetupCacheAmbiguous) {
		t.Fatalf("err = %v", err)
	}
}

func TestSetupCacheSourceShapeRefusesAMovedFileOrName(t *testing.T) {
	f := cacheSourceDefault()
	srv := cacheSourceServer(t, f)
	src, err := cacheSourceClient(t, srv).ReadSetupCacheSource(context.Background(), "acme/repo", SetupCacheSelection{RunID: f.runID, Attempt: f.attempt})
	if err != nil {
		t.Fatal(err)
	}
	for _, mode := range []string{"repo", "workflow", "lockfile", "job", "step"} {
		c := *src
		switch mode {
		case "repo":
			c.Repository = "other/repo"
		case "workflow":
			c.Workflow.Content += "# changed\n"
		case "lockfile":
			c.Lockfile.Content += " "
		case "job":
			c.JobName = "other"
		case "step":
			c.StepName = "other"
		}
		if ValidSetupCacheSource(&c, "acme/repo", SetupCacheSelection{RunID: f.runID, Attempt: f.attempt}) {
			t.Fatalf("%s accepted", mode)
		}
	}
}

var _ = fmt.Sprintf
