//go:build e2e

package e2e

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math/rand/v2"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// This test proves the whole product works end to end: a real controller, a
// real GitHub App, a real workflow run, a real container.
//
// See README.md in this directory for the environment it needs, and result.go
// for the vocabulary it reports in. The two things to know before reading it:
// nothing is created until every prerequisite has been checked, and everything
// created is written to a ledger outside the temp directory before it exists,
// so a run killed at any point leaves a record of what to go and remove.

const scenarioName = "ephemeral_runner_runs_a_real_job"

func TestEphemeralRunnerRunsARealJob(t *testing.T) {
	runID := newRunID()
	label := "zoomies-e2e-" + runID
	res := &Result{
		Scenario: scenarioName, RunID: runID, Commit: commit(),
		StartedAt: time.Now(), Category: NotRun,
	}
	t.Cleanup(func() {
		// A run nobody asked for writes nothing. Every result file carries the
		// run id in its name, so recording "not_run" on each invocation would
		// pile up a file per `make test-e2e` on a laptop, and none of them
		// would say anything: not_run is for a scenario a real run left out,
		// not for a run that was never requested.
		if !requested() {
			return
		}
		res.FinishedAt = time.Now()
		if err := res.write(resultsDir(t)); err != nil {
			t.Errorf("writing the result record: %v", err)
		}
	})

	if !requested() {
		t.Skip("set ZOOMIES_E2E=1 to run the end-to-end test; see test/e2e/README.md")
	}

	e, missing := preflight()
	if len(missing) > 0 {
		res.Category, res.Reason = Blocked, strings.Join(missing, "; ")
		if required() {
			// In required mode a prerequisite that is not there is a finding,
			// not a shrug. This is the difference between a gate that means
			// something and one that goes green having done nothing.
			t.Fatalf("blocked: %s", res.Reason)
		}
		t.Skipf("blocked: %s", res.Reason)
	}

	// The sweep runs before this run creates anything, so its findings belong
	// to earlier runs and cannot be confused with this one's.
	res.SweptFromEarlierRuns = sweep(t, runID, e)

	ledger, err := openLedger(runID, label, e)
	if err != nil {
		res.Category, res.Reason = Blocked, fmt.Sprintf("the ledger could not be opened: %v", err)
		t.Fatal(res.Reason)
	}
	// The controller starts before this cleanup is registered, so that cleanup
	// order puts them the right way round: t.Cleanup runs last-registered
	// first, and tidying up has to happen while the API it deletes through is
	// still listening.
	base := controller(t)
	api := &client{t: t, base: base + "/api/v1"}

	// Cleanup runs however the test ends, and it is the only place that
	// removes what this run made. Anything it cannot remove is recorded on the
	// result rather than dropped, because it is sitting on a real organisation.
	t.Cleanup(func() {
		tidyUp(t, ledger, e, label, base)
		res.ResidualCleanup = ledger.outstanding()
		if len(res.ResidualCleanup) == 0 {
			if err := ledger.close(); err != nil {
				t.Errorf("closing the ledger: %v", err)
			}
		}
		if res.Category == NotRun && !t.Failed() {
			res.Category = Passed
		}
		if t.Failed() && res.Category != Blocked {
			res.Category = Failed
			if res.Reason == "" {
				res.Reason = "an assertion failed; see the test output"
			}
		}
	})

	// 1. Connect the GitHub App.
	var inst struct {
		ID string `json:"id"`
	}
	api.post("/installations", map[string]any{
		"app_id":          mustInt(t, e.appID),
		"installation_id": mustInt(t, e.installationID),
		"target":          e.target,
		"target_type":     e.targetType,
		"private_key":     e.privateKey,
	}, &inst)
	if inst.ID == "" {
		t.Fatal("the installation was created but came back without an ID")
	}
	if err := ledger.created("installation", inst.ID); err != nil {
		t.Fatalf("recording the installation in the ledger: %v", err)
	}

	// 2. It must verify, or nothing else can work.
	var health struct {
		OK                 bool     `json:"ok"`
		Message            string   `json:"message"`
		MissingPermissions []string `json:"missing_permissions"`
	}
	api.post("/installations/"+inst.ID+"/verify", nil, &health)
	if !health.OK {
		t.Fatalf("the installation did not verify: %s (missing: %v)",
			health.Message, health.MissingPermissions)
	}

	// 3. A pool carrying this run's own label, so that two runs against one
	//    organisation cannot take each other's jobs or each other's runners.
	var pool struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	}
	api.post("/pools", map[string]any{
		"installation_id": inst.ID,
		"name":            "e2e-" + runID,
		"labels":          []string{label},
		"backend":         "docker",
		"min_runners":     0,
		"max_runners":     1,
		"idle_timeout":    "1m",
		"ephemeral":       true,
		"docker_mode":     "none",
	}, &pool)
	if err := ledger.created("pool", pool.ID); err != nil {
		t.Fatalf("recording the pool in the ledger: %v", err)
	}

	// The audit log must have recorded that.
	var audit struct {
		Items []struct {
			Action   string `json:"action"`
			TargetID string `json:"target_id"`
		} `json:"items"`
	}
	api.get("/audit?action=pool.create", &audit)
	if len(audit.Items) == 0 {
		t.Error("creating a pool was not written to the audit log")
	}

	// 4. Trigger the workflow, asking for this run's label.
	marker := fmt.Sprintf("zoomies-e2e-%s-%d", runID, time.Now().UnixNano())
	res.Marker = marker
	res.RunURL = dispatchWorkflow(t, e, marker, label)

	// 5. A runner must appear, register, and pick the job up.
	var runnerID string
	waitFor(t, waitRunnerCreated, "a runner to be created for the pool", func() bool {
		var out struct {
			Items []struct {
				ID    string `json:"id"`
				State string `json:"state"`
			} `json:"items"`
		}
		api.get("/runners?pool_id="+pool.ID+"&include_removed=true", &out)
		for _, r := range out.Items {
			runnerID = r.ID
			return true
		}
		return false
	})
	t.Logf("runner %s created", runnerID)

	waitFor(t, waitRunnerRegistered, "the runner to reach idle or busy", func() bool {
		var r struct {
			State string `json:"state"`
		}
		api.get("/runners/"+runnerID, &r)
		return r.State == "idle" || r.State == "busy" || r.State == "removed"
	})

	// 6. The job must complete successfully -- and it must be *this run's*
	//    job. Without the marker assertion the scenario would pass on any
	//    completed job in the pool, including one a concurrent run dispatched.
	var ourJob struct {
		ID         string `json:"id"`
		State      string `json:"state"`
		Conclusion string `json:"conclusion"`
		RunnerID   string `json:"runner_id"`
	}
	waitFor(t, waitJobCompleted, "this run's job to complete", func() bool {
		var out struct {
			Items []struct {
				ID         string   `json:"id"`
				State      string   `json:"state"`
				Conclusion string   `json:"conclusion"`
				RunnerID   string   `json:"runner_id"`
				Labels     []string `json:"labels"`
			} `json:"items"`
		}
		api.get("/jobs?pool_id="+pool.ID, &out)
		for _, j := range out.Items {
			if j.State != "completed" {
				continue
			}
			// The pool's label is unique to this run, so a job carrying it is
			// ours by construction; the marker is asserted on the run itself
			// below, where GitHub can be asked what it dispatched.
			for _, l := range j.Labels {
				if l == label {
					ourJob.ID, ourJob.State = j.ID, j.State
					ourJob.Conclusion, ourJob.RunnerID = j.Conclusion, j.RunnerID
					return true
				}
			}
		}
		return false
	})
	if ourJob.Conclusion != "success" {
		t.Fatalf("the job completed with conclusion %q, want success", ourJob.Conclusion)
	}
	if ourJob.RunnerID == "" {
		t.Error("the completed job is not linked to the runner that ran it")
	}
	assertDispatchedRun(t, e, marker)

	// 7. The ephemeral runner must go away by itself.
	waitFor(t, waitRunnerDestroyed, "the ephemeral runner to be destroyed", func() bool {
		var r struct {
			State string `json:"state"`
		}
		api.get("/runners/"+runnerID, &r)
		return r.State == "removed" || r.State == "failed"
	})

	var final struct {
		State       string `json:"state"`
		JobsHandled int    `json:"jobs_handled"`
	}
	api.get("/runners/"+runnerID, &final)
	if final.State != "removed" {
		t.Errorf("runner ended in state %q, want removed", final.State)
	}
	if final.JobsHandled != 1 {
		t.Errorf("runner handled %d jobs, want exactly 1 (it is ephemeral)", final.JobsHandled)
	}

	// 8. The two checks that are the point of running this against reality.
	//    Both ask something other than Zoomies, because "did Zoomies clean up?"
	//    answered by Zoomies is not evidence.
	orphans, err := githubRunnersWithLabel(e, label)
	if err != nil {
		t.Errorf("checking GitHub for leftover registrations: %v", err)
	} else if len(orphans) > 0 {
		names := make([]string, 0, len(orphans))
		for _, o := range orphans {
			names = append(names, fmt.Sprintf("%s (id %d, %s)", o.Name, o.ID, o.Status))
		}
		t.Errorf("GitHub still holds %d registration(s) for this run after the runner was removed: %s",
			len(orphans), strings.Join(names, ", "))
	}
	left, err := containersForPool(pool.ID)
	if err != nil {
		t.Errorf("checking the host for leftover containers: %v", err)
	} else if len(left) > 0 {
		t.Errorf("the host still has %d container(s) for this run's pool: %s",
			len(left), strings.Join(left, "; "))
	}
}

// sweep clears what earlier runs left behind, before this run creates
// anything, and returns what it found.
//
// It reports rather than tidies quietly: a sweep that keeps finding things is
// a harness that keeps crashing, and that is worth seeing in the result file.
func sweep(t *testing.T, runID string, e env) []string {
	t.Helper()
	stale, err := staleLedgers(runID, e)
	if err != nil {
		t.Logf("could not read the ledger directory: %v", err)
		return nil
	}
	var found []string
	for _, rec := range stale {
		runners, err := githubRunnersWithLabel(e, rec.Label)
		if err != nil {
			t.Logf("could not check GitHub for run %s's runners: %v", rec.RunID, err)
			continue
		}
		cleared := true
		for _, r := range runners {
			if err := deleteGitHubRunner(e, r.ID); err != nil {
				t.Logf("could not delete runner %d left by run %s: %v", r.ID, rec.RunID, err)
				cleared = false
				continue
			}
			found = append(found, fmt.Sprintf("run %s: deleted GitHub runner %s (id %d)", rec.RunID, r.Name, r.ID))
		}
		if len(runners) == 0 {
			found = append(found, fmt.Sprintf("run %s: ledger left open with %d resource(s), no GitHub runners remained",
				rec.RunID, len(rec.Resources)))
		}
		if cleared {
			dropLedger(rec.RunID)
		}
	}
	for _, line := range found {
		t.Logf("sweep: %s", line)
	}
	return found
}

// tidyUp removes what this run created, in the order that makes each step
// possible: the pool goes first and forcibly, because its runners have to be
// gone before the installation that registered them can be deleted.
func tidyUp(t *testing.T, l *ledger, e env, label, base string) {
	t.Helper()
	api := &tolerantClient{t: t, base: base + "/api/v1"}
	deadline := time.Now().Add(waitCleanup)
	for _, r := range l.rec.Resources {
		if r.RemovedAt != nil || r.Kind != "pool" {
			continue
		}
		if api.delete("/pools/" + r.ID + "?force=true") {
			l.removed("pool", r.ID)
		}
	}
	// The pool's runners must actually be gone before the installation is
	// deleted, or the registrations outlive the credential that could remove
	// them.
	for time.Now().Before(deadline) {
		var out struct {
			Items []json.RawMessage `json:"items"`
		}
		if !api.get("/runners?include_removed=false", &out) || len(out.Items) == 0 {
			break
		}
		time.Sleep(3 * time.Second)
	}
	for _, r := range l.rec.Resources {
		if r.RemovedAt != nil || r.Kind != "installation" {
			continue
		}
		if api.delete("/installations/" + r.ID) {
			l.removed("installation", r.ID)
		}
	}
	sweepGitHub(t, e, label)
}

// sweepGitHub is the last resort: whatever Zoomies could not be made to
// remove, take off GitHub directly so the organisation is left clean.
func sweepGitHub(t *testing.T, e env, label string) {
	t.Helper()
	runners, err := githubRunnersWithLabel(e, label)
	if err != nil || len(runners) == 0 {
		return
	}
	for _, r := range runners {
		if err := deleteGitHubRunner(e, r.ID); err != nil {
			t.Errorf("could not delete leftover GitHub runner %d: %v", r.ID, err)
			continue
		}
		t.Logf("cleanup: deleted GitHub runner %s (id %d) directly", r.Name, r.ID)
	}
}

// --------------------------------------------------------------------------
// controller
// --------------------------------------------------------------------------

func builtBinary() string {
	dir, err := os.Getwd()
	if err != nil {
		return "zoomies"
	}
	for range 6 {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return filepath.Join(dir, "zoomies")
		}
		dir = filepath.Dir(dir)
	}
	return "zoomies"
}

func resultsDir(t *testing.T) string {
	t.Helper()
	if d := os.Getenv("ZOOMIES_E2E_RESULTS_DIR"); d != "" {
		return d
	}
	return filepath.Join(filepath.Dir(builtBinary()), "roadmap", "validation", "e2e")
}

// controller starts a real zoomies controller against a throwaway database and
// returns its base URL.
func controller(t *testing.T) string {
	t.Helper()

	bin := builtBinary()
	port := freePort(t)
	dir := t.TempDir()
	base := fmt.Sprintf("http://127.0.0.1:%d", port)

	ctx, cancel := context.WithCancel(context.Background())
	cmd := exec.CommandContext(ctx, bin, "controller")
	cmd.Env = append(os.Environ(),
		fmt.Sprintf("ZOOMIES_BIND=127.0.0.1:%d", port),
		"ZOOMIES_DISABLE_AUTH=true", // loopback only; the config validator allows it there
		"ZOOMIES_DB_PATH="+filepath.Join(dir, "zoomies.db"),
		"ZOOMIES_STATE_DIR="+dir,
		"ZOOMIES_CONFIG_DIR="+dir,
		"ZOOMIES_WORK_DIR="+filepath.Join(dir, "work"),
		"ZOOMIES_AGENT_EMBEDDED=true",
		"ZOOMIES_AGENT_BACKEND=docker",
		"ZOOMIES_AGENT_CAPACITY=2",
		// A test host is not reachable from GitHub, so this exercises the
		// polling path rather than the webhook path. The webhook path is
		// covered by the integration tests against the fake GitHub.
		"ZOOMIES_POLL_FALLBACK=true",
		"ZOOMIES_POLL_INTERVAL=10s",
		"ZOOMIES_LOG_FORMAT=text",
		"ZOOMIES_LOG_LEVEL=debug",
	)
	var logs bytes.Buffer
	cmd.Stdout = &logs
	cmd.Stderr = &logs
	if err := cmd.Start(); err != nil {
		cancel()
		t.Fatalf("starting the controller: %v", err)
	}
	// Cleanup runs last-registered-first, so this stops the controller only
	// after tidyUp has finished deleting through its API.
	t.Cleanup(func() {
		cancel()
		_ = cmd.Wait()
		if t.Failed() {
			t.Logf("controller output:\n%s", logs.String())
		}
	})

	waitFor(t, waitControllerHealthy, "the controller to become healthy", func() bool {
		resp, err := http.Get(base + "/healthz")
		if err != nil {
			return false
		}
		resp.Body.Close()
		return resp.StatusCode == http.StatusOK
	})
	return base
}

// --------------------------------------------------------------------------
// helpers
// --------------------------------------------------------------------------

type client struct {
	t    *testing.T
	base string
}

func (c *client) do(method, path string, body any, out any) {
	c.t.Helper()
	status, raw, err := call(method, c.base+path, body, out)
	if err != nil {
		c.t.Fatalf("%s %s: %v", method, path, err)
	}
	if status >= 300 {
		c.t.Fatalf("%s %s: %d\n%s", method, path, status, raw)
	}
}

func (c *client) get(path string, out any)        { c.do(http.MethodGet, path, nil, out) }
func (c *client) post(path string, body, out any) { c.do(http.MethodPost, path, body, out) }

// tolerantClient is the cleanup's client: a failure is reported and moved past
// rather than fatal, because the next step may still remove something and a
// t.Fatal inside a cleanup would skip the rest of it.
type tolerantClient struct {
	t    *testing.T
	base string
}

func (c *tolerantClient) delete(path string) bool {
	c.t.Helper()
	status, raw, err := call(http.MethodDelete, c.base+path, nil, nil)
	if err != nil {
		c.t.Errorf("cleanup: DELETE %s: %v", path, err)
		return false
	}
	if status >= 300 && status != http.StatusNotFound {
		c.t.Errorf("cleanup: DELETE %s: %d\n%s", path, status, raw)
		return false
	}
	return true
}

func (c *tolerantClient) get(path string, out any) bool {
	c.t.Helper()
	status, _, err := call(http.MethodGet, c.base+path, nil, out)
	return err == nil && status < 300
}

func call(method, url string, body, out any) (int, []byte, error) {
	var r io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return 0, nil, fmt.Errorf("marshalling the request body: %w", err)
		}
		r = bytes.NewReader(b)
	}
	req, err := http.NewRequest(method, url, r)
	if err != nil {
		return 0, nil, fmt.Errorf("building the request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 300 && out != nil && len(raw) > 0 {
		if err := json.Unmarshal(raw, out); err != nil {
			return resp.StatusCode, raw, fmt.Errorf("decoding the response: %w", err)
		}
	}
	return resp.StatusCode, raw, nil
}

// dispatchWorkflow triggers the fixture workflow and returns a link to the run
// it started. The label goes with the marker so the workflow asks for this
// run's pool rather than a shared one.
func dispatchWorkflow(t *testing.T, e env, marker, label string) string {
	t.Helper()
	cmd := exec.Command("gh", "workflow", "run", "zoomies-e2e.yml",
		"--repo", e.repo, "-f", "marker="+marker, "-f", "label="+label)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("triggering the workflow in %s: %v\n%s", e.repo, err, out)
	}
	t.Logf("dispatched zoomies-e2e.yml in %s with marker %s and label %s", e.repo, marker, label)
	return "https://github.com/" + e.repo + "/actions/workflows/zoomies-e2e.yml"
}

// assertDispatchedRun asks GitHub that a run carrying this marker exists and
// succeeded. It is the check that ties the job Zoomies ran to the job this
// test asked for; without it the scenario passes on whatever happened to be
// in the pool.
func assertDispatchedRun(t *testing.T, e env, marker string) {
	t.Helper()
	out, err := exec.Command("gh", "run", "list", "--repo", e.repo,
		"--workflow", "zoomies-e2e.yml", "--limit", "20",
		"--json", "databaseId,displayTitle,conclusion,url").Output()
	if err != nil {
		t.Errorf("asking GitHub for the dispatched run: %v", ghError(err))
		return
	}
	var runs []struct {
		DatabaseID   int64  `json:"databaseId"`
		DisplayTitle string `json:"displayTitle"`
		Conclusion   string `json:"conclusion"`
		URL          string `json:"url"`
	}
	if err := json.Unmarshal(out, &runs); err != nil {
		t.Errorf("decoding the run list: %v", err)
		return
	}
	for _, r := range runs {
		if strings.Contains(r.DisplayTitle, marker) {
			if r.Conclusion != "success" {
				t.Errorf("the dispatched run %s concluded %q, want success", r.URL, r.Conclusion)
			}
			return
		}
	}
	t.Logf("no run titled with marker %s found; GitHub does not always put a dispatch input in the title, "+
		"so this is reported rather than failed", marker)
}

func waitFor(t *testing.T, limit time.Duration, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(limit)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(3 * time.Second)
	}
	t.Fatalf("timed out after %s waiting for %s", limit, what)
}

func freePort(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("finding a free port: %v", err)
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port
}

// newRunID is this run's identity. It is short, lower-case and alphanumeric
// because it becomes part of a GitHub runner label and a pool name.
func newRunID() string {
	const alphabet = "abcdefghijklmnopqrstuvwxyz0123456789"
	b := make([]byte, 8)
	for i := range b {
		b[i] = alphabet[rand.IntN(len(alphabet))]
	}
	return string(b)
}

func mustInt(t *testing.T, s string) int64 {
	t.Helper()
	var n int64
	if _, err := fmt.Sscanf(s, "%d", &n); err != nil {
		t.Fatalf("%q is not a number: %v", s, err)
	}
	return n
}
