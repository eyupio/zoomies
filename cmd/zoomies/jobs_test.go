package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// `jobs get` is the page somebody opens when a job did not do what they
// expected, so it has to carry the whole story: what the job was, where it got
// to, why it is there, and what happened along the way.
func TestJobsGetCarriesTheWholeStory(t *testing.T) {
	srv := jsonRoutes(t, map[string]string{
		"/api/v1/jobs/job_1": `{
			"id":"job_1","repo":"acme/widgets","workflow":"CI","job_name":"build",
			"head_branch":"main","run_attempt":2,"state":"completed","conclusion":"failure",
			"labels":["zoomies-4vcpu"],"matched":true,"pool_name":"zoomies-4vcpu",
			"runner_name":"zoomies-abc","queued_at":"2025-01-01T00:00:00Z",
			"started_at":"2025-01-01T00:00:10Z","completed_at":"2025-01-01T00:05:00Z",
			"queue_wait_ms":10000,"duration_ms":290000,
			"failed_step":{"number":4,"name":"go test ./..."},
			"html_url":"https://github.com/acme/widgets/actions/runs/1",
			"steps":[
				{"number":1,"name":"Set up job","status":"completed","conclusion":"success",
				 "started_at":"2025-01-01T00:00:10Z","completed_at":"2025-01-01T00:00:12Z"},
				{"number":4,"name":"go test ./...","status":"completed","conclusion":"failure"}]}`,
		"/api/v1/jobs/job_1/events": `{"items":[
			{"at":"2025-01-01T00:00:00Z","source":"webhook","message":"queued"},
			{"at":"2025-01-01T00:00:10Z","source":"runner","message":"picked up by zoomies-abc"}],"total":2}`,
		"/api/v1/jobs/job_1/explanation": `{"summary":"This job failed in its own steps.",
			"detail":"The runner was healthy throughout.","fix":"Look at step 4."}`,
	})

	out, _ := runCLI(t, "jobs", "get", "job_1", "--url", srv.URL)

	for _, want := range []string{"acme/widgets", "CI", "build", "main", "zoomies-abc"} {
		if !strings.Contains(out, want) {
			t.Errorf("jobs get must show %q:\n%s", want, out)
		}
	}
	if !strings.Contains(out, "step 4, go test ./...") {
		t.Errorf("the step it failed at must be shown:\n%s", out)
	}
	if !strings.Contains(out, "https://github.com/acme/widgets/actions/runs/1") {
		t.Errorf("the link back to GitHub must be shown:\n%s", out)
	}
	// The reason comes from the controller, which can see the scheduler and the
	// host; the CLI reasoning from the job row alone used to get this wrong.
	for _, want := range []string{"This job failed in its own steps.", "The runner was healthy", "Fix:"} {
		if !strings.Contains(out, want) {
			t.Errorf("the explanation must be shown, including %q:\n%s", want, out)
		}
	}
	if !strings.Contains(out, "Steps") || !strings.Contains(out, "Set up job") {
		t.Errorf("the steps must be shown:\n%s", out)
	}
	if !strings.Contains(out, "Timeline") || !strings.Contains(out, "picked up by zoomies-abc") {
		t.Errorf("the timeline must be shown:\n%s", out)
	}
}

// A job whose runner stopped under it did not fail in its own steps, and
// saying so is the difference between an operator debugging their workflow and
// an operator looking at their fleet.
func TestJobsGetMarksARunnerThatWasLost(t *testing.T) {
	srv := jsonRoutes(t, map[string]string{
		"/api/v1/jobs/job_1": `{"id":"job_1","repo":"acme/widgets","workflow":"CI",
			"job_name":"build","state":"completed","conclusion":"failure",
			"runner_fault":"the host stopped answering while this job was running",
			"queued_at":"2025-01-01T00:00:00Z"}`,
		"/api/v1/jobs/job_1/events":      `{"items":[],"total":0}`,
		"/api/v1/jobs/job_1/explanation": `{}`,
	})

	out, _ := runCLI(t, "jobs", "get", "job_1", "--url", srv.URL)

	if !strings.Contains(out, "runner lost") {
		t.Errorf("a lost runner must be labelled:\n%s", out)
	}
	if !strings.Contains(out, "the host stopped answering") {
		t.Errorf("the fault itself must be shown:\n%s", out)
	}
	// Nothing to say is said by saying nothing.
	if strings.Contains(out, "Steps") || strings.Contains(out, "Timeline") {
		t.Errorf("empty sections were printed:\n%s", out)
	}
}

// The list has room for one phrase about a job that went wrong, and it has to
// be the one that says where to look.
func TestFailureWhyPicksTheOnePhraseThatFits(t *testing.T) {
	lost := jobItem{RunnerFault: "the host stopped answering",
		FailedStep: &jobStep{Number: 4, Name: "go test ./..."}}
	// A job whose runner vanished mid-step has both, and the runner is the
	// bigger fact: the step did not fail, it was interrupted.
	if got := failureWhy(lost); got != "runner lost" {
		t.Errorf("failureWhy = %q, want runner lost", got)
	}

	failed := jobItem{FailedStep: &jobStep{Number: 4, Name: "go test ./..."}}
	if got := failureWhy(failed); got != "go test ./..." {
		t.Errorf("failureWhy = %q, want the step's name", got)
	}

	// A job that went fine has nothing to say in that column.
	if got := failureWhy(jobItem{}); got != "" {
		t.Errorf("failureWhy = %q, want nothing", got)
	}
}

// An attempt number is only interesting once it exists; a zero would read as
// "attempt zero" rather than "GitHub did not say".
func TestAttemptShowsADashUntilThereIsOne(t *testing.T) {
	for in, want := range map[int]string{0: "--", -1: "--", 1: "1", 3: "3"} {
		if got := attempt(in); got != want {
			t.Errorf("attempt(%d) = %q, want %q", in, got, want)
		}
	}
}

// The two filters an operator reaches for when somebody says CI is flaky, and
// the category that turns a column of "runner lost" into something to act on.
// They are sent as the API's own keys, so the terminal and the address bar ask
// for the same thing.
func TestJobsListSendsTheFaultFiltersTheAPIUnderstands(t *testing.T) {
	var asked []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		asked = append(asked, r.URL.RawQuery)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"items":[{"id":"job_1","repo":"acme/widgets","workflow":"CI",
			"job_name":"build","state":"completed","conclusion":"failure","matched":true,
			"fault_kind":"out_of_memory","fault_domain":"fleet",
			"runner_fault":"runner zoomies-a stopped: out of memory",
			"queued_at":"2025-01-01T00:00:00Z"}],"total":1}`))
	}))
	t.Cleanup(srv.Close)

	out, _ := runCLI(t, "jobs", "list", "--ours", "--url", srv.URL)
	if len(asked) != 1 || !strings.Contains(asked[0], "faulted=true") {
		t.Fatalf("--ours sent %v, want faulted=true", asked)
	}
	// The category rather than "runner lost" in the why column: a column read
	// downwards wants the word that differs between rows.
	if !strings.Contains(out, "out of memory") {
		t.Errorf("the why column does not carry the category:\n%s", out)
	}

	asked = nil
	if _, _ = runCLI(t, "jobs", "list", "--theirs", "--url", srv.URL); len(asked) != 1 ||
		!strings.Contains(asked[0], "workflow_failed=true") {
		t.Fatalf("--theirs sent %v, want workflow_failed=true", asked)
	}

	asked = nil
	if _, _ = runCLI(t, "jobs", "list", "--fault", "out_of_memory", "--url", srv.URL); len(asked) != 1 ||
		!strings.Contains(asked[0], "fault=out_of_memory") {
		t.Fatalf("--fault sent %v, want fault=out_of_memory", asked)
	}
}

// --ours and --theirs ask for opposite halves of one list, so asking for both
// is a mistake worth catching here rather than sending a request the server
// will refuse.
func TestJobsListRefusesBothHalvesOfTheFailedList(t *testing.T) {
	e, _, errOut := newTestEnv(t)
	if code := dispatch(context.Background(), e,
		[]string{"jobs", "list", "--ours", "--theirs", "--url", "http://127.0.0.1:1"}); code == exitOK {
		t.Fatal("asking for both halves was accepted")
	}
	if !strings.Contains(errOut.String(), "opposite halves") {
		t.Errorf("the refusal does not say why:\n%s", errOut.String())
	}
}

// The remedy for a job the fleet broke, at the terminal. GitHub has no
// job-level rerun, so the one thing the output must not do is let somebody
// think only this job is going again.
func TestJobsRerunAsksGitHubAndSaysWhatElseGoesWithIt(t *testing.T) {
	var method, path string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		method, path = r.Method, r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"accepted":true,"run_id":4301,"fault_domain":"fleet"}`))
	}))
	t.Cleanup(srv.Close)

	out, _ := runCLI(t, "jobs", "rerun", "job_1", "--url", srv.URL)
	if method != http.MethodPost || path != "/api/v1/jobs/job_1/rerun" {
		t.Fatalf("asked %s %s, want POST the rerun route", method, path)
	}
	if !strings.Contains(out, "4301") {
		t.Errorf("the output does not name the run being re-run:\n%s", out)
	}
	// Whose failure it was, because it decides whether anybody here has
	// something to fix before the re-run is worth spending.
	if !strings.Contains(out, "fleet's rather than the workflow's") {
		t.Errorf("the output does not say whose failure it was:\n%s", out)
	}
}
