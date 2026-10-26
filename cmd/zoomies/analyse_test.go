package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/eyupio/zoomies/internal/analysis"
	"github.com/eyupio/zoomies/internal/controller"
)

var (
	analyseSince = time.Date(2026, 10, 12, 8, 0, 0, 0, time.UTC)
	analyseUntil = time.Date(2026, 10, 13, 8, 0, 0, 0, time.UTC)
)

// analyseBody is a finding as the route sends it.
func analyseBody(t *testing.T, f any) string {
	t.Helper()
	b, err := json.Marshal(f)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func fixtureQueueWait(jobName string) analysis.QueueWaitFinding {
	return analysis.QueueWaitFinding{
		Analysis: "queue_wait", Version: analysis.QueueWaitVersion, Since: analyseSince, Until: analyseUntil,
		Summary:  "Jobs waited 3h 10m in all, most of it for runners to start (60%).",
		Dominant: string(analysis.CauseRunnerStartup), Confidence: analysis.Medium,
		ConfidenceReason: "the job read was cut at the newest 5000",
		NextSteps: []analysis.NextStep{
			{Text: "Keep warm runners so jobs do not wait for one to start.", Setting: "pool.min_runners", Code: "pool.queue_wait_high"},
			{Text: "Read why runners are slow to start on this pool.", Code: "runner.startup_slow", Link: "https://zoomies.sh/problem-codes/#runnerstartup_slow"},
		},
		Codes: []string{"pool.queue_wait_high", "runner.startup_slow"},
		Evidence: analysis.QueueWaitEvidence{
			Fleet: []analysis.CauseShare{{Cause: analysis.CauseRunnerStartup, Seconds: 6840, Share: 0.6}, {Cause: analysis.CauseGitHubDelay, Seconds: 4560, Share: 0.4}},
			Jobs:  40, Waited: 31, WaitSeconds: 11400, RunnerKept: 2, JobsTruncated: true,
			Pools: []analysis.PoolWait{{
				PoolID: "pool_linux", PoolName: "linux-x64", Jobs: 31, WaitSeconds: 11400,
				Causes:   []analysis.CauseShare{{Cause: analysis.CauseRunnerStartup, Seconds: 6840, Share: 0.6}},
				Dominant: analysis.CauseRunnerStartup,
				Longest: []analysis.LongWait{
					{JobID: "job_slow", Repo: "acme/widgets", Workflow: "ci", JobName: jobName, WaitSeconds: 1260, Cause: analysis.CauseRunnerStartup},
				},
			}},
		},
	}
}

func fixtureCompare() analysis.CompareFinding {
	p := func(v int64) *int64 { return &v }
	fl := func(v float64) *float64 { return &v }
	side := func(label string, since, until time.Time, jobs int) analysis.CompareSide {
		return analysis.CompareSide{Label: label, Since: since, Until: until, AskedUntil: until, Figures: analysis.JobFigures{
			Jobs: jobs, Succeeded: jobs - 2, Failed: 2,
			Duration:  analysis.Spread{Samples: jobs, P50MS: p(180_000), P95MS: p(300_000)},
			QueueWait: analysis.Spread{Samples: jobs, P50MS: p(4_000), P95MS: p(20_000)},
			Startup:   analysis.Spread{Samples: jobs},
		}}
	}
	a := side("v1.3.2", time.Date(2026, 10, 10, 20, 0, 0, 0, time.UTC), time.Date(2026, 10, 12, 6, 0, 0, 0, time.UTC), 120)
	// The side read only as far as its last job; the person asked for later.
	a.AskedUntil = time.Date(2026, 10, 12, 7, 30, 0, 0, time.UTC)
	return analysis.CompareFinding{
		Analysis: "compare", Version: analysis.CompareVersion, Since: analyseSince, Until: analyseUntil,
		Summary:  "Under v1.3.3 duration p95 went from 5m to 9m.",
		Dominant: "duration_p95", Confidence: analysis.High,
		NextSteps: []analysis.NextStep{{Text: "Jobs ran longer under v1.3.3 (duration p95 5m to 9m).", Code: "jobs.duration_regressed"}},
		Codes:     []string{"jobs.duration_regressed"},
		Evidence: analysis.CompareEvidence{
			By: "version",
			A:  a,
			B:  side("v1.3.3", time.Date(2026, 10, 12, 9, 15, 0, 0, time.UTC), time.Date(2026, 10, 13, 7, 45, 0, 0, time.UTC), 130),
			Changes: []analysis.Change{
				{Measure: "duration_p95", Unit: "ms", Direction: analysis.Regressed, From: fl(300_000), To: fl(540_000), Ratio: fl(1.8)},
				{Measure: "failure_rate", Unit: "rate", Direction: analysis.Unchanged, From: fl(0.0167), To: fl(0.0154), Ratio: fl(0.03)},
			},
			Excluded: []string{"cancelled", "skipped"},
		},
	}
}

func fixtureHostPressure(jobName string) analysis.HostPressureFinding {
	at := analyseSince.Add(3 * time.Hour)
	return analysis.HostPressureFinding{
		Analysis: "host_pressure", Version: analysis.HostPressureVersion, Since: analyseSince, Until: analyseUntil,
		Summary:  "6 of build-1's 7 failures came in the minutes it was pressured.",
		Dominant: analysis.HostPressured, Confidence: analysis.High,
		NextSteps: []analysis.NextStep{{Text: "Give build-1 fewer slots.", Setting: "host.capacity", Code: "host.throttled"}},
		Codes:     []string{"host.throttled"},
		Evidence: analysis.HostPressureEvidence{
			HostID: "host_b", HostName: "build-1", Pressure: analysis.PressureThresholds{LoadPerCPU: 2, MemoryReserveMB: 512},
			Minutes: 120, PressuredMinutes: 20, ExpectedMinutes: 120,
			Events: []analysis.BadEvent{
				{JobID: "job_1", Repo: "acme/app", Workflow: "ci", JobName: jobName, Kind: analysis.EventFailure, At: at, Measured: true, Pressured: true},
				{JobID: "job_2", Repo: "acme/app", Workflow: "ci", JobName: "test", Kind: analysis.EventOOM, At: at.Add(-time.Minute), Measured: true},
			},
			MeasuredEvents: 2, PressuredEvents: 1,
			Threshold: &analysis.PressureThreshold{Measure: analysis.MeasureLoadPerCPU, Value: 2.5, EventShare: 0.86, MinuteShare: 0.17},
		},
	}
}

// fixtureWithheld is host pressure as the route sends it to a token that
// cannot read jobs: the names blanked and the controller's own step added.
func fixtureWithheld() analysis.HostPressureFinding {
	f := fixtureHostPressure("build")
	for i := range f.Evidence.Events {
		f.Evidence.Events[i].Repo, f.Evidence.Events[i].Workflow, f.Evidence.Events[i].JobName = "", "", ""
	}
	f.Evidence.JobNamesWithheld = true
	f.NextSteps = append(f.NextSteps, analysis.NextStep{Text: analysis.JobNamesWithheldText})
	return f
}

func fixtureStartup(message string) analysis.StartupFailuresFinding {
	recovered := analyseSince.Add(5 * time.Hour)
	return analysis.StartupFailuresFinding{
		Analysis: "startup_failures", Version: analysis.StartupFailuresVersion, Since: analyseSince, Until: analyseUntil,
		Summary:  "Runners on build-2 failed to start 9 times since 09:12, all before their container started.",
		Dominant: "host_c", Confidence: analysis.High,
		NextSteps: []analysis.NextStep{{Text: "Check the image build-2 pulls.", Setting: "pool.image", Code: "runner.image_pull_failed"}},
		Codes:     []string{"runner.image_pull_failed"},
		Evidence: analysis.StartupEvidence{
			Starts: 30, Failures: 11, Unknown: 1,
			Groups: []analysis.StartupGroup{
				{Scope: analysis.ScopeHost, ID: "host_c", Name: "build-2", Starts: 12, Failures: 9, FailingSince: analyseSince.Add(time.Hour + 12*time.Minute),
					LastAt: analyseSince.Add(4 * time.Hour), Stage: analysis.StageBeforeContainer, FaultKind: "image", Explanation: analysis.ExplainImage, Explained: 9,
					LastRunnerID: "run_9", LastMessage: message},
				{Scope: analysis.ScopePool, ID: "pool_arm", Name: "arm64", Starts: 6, Failures: 2, FailingSince: analyseSince.Add(2 * time.Hour),
					RecoveredAt: &recovered, LastAt: analyseSince.Add(3 * time.Hour), Stage: analysis.StageAfterRegistration, LastRunnerID: "run_4", LastMessage: "runner exited 1"},
			},
		},
	}
}

func fixtureRecurring(job string) analysis.RecurringFailuresFinding {
	return analysis.RecurringFailuresFinding{
		Analysis: "recurring_failures", Version: analysis.RecurringFailuresVersion, Since: analyseSince, Until: analyseUntil,
		Summary:  "The job that fails most failed 4 of its 9 runs (job job_9).",
		Dominant: "job_9", Confidence: analysis.High,
		NextSteps: []analysis.NextStep{{Text: "Read the failed step's log from one failed run and one rerun of the same revision."}},
		Codes:     []string{},
		Evidence: analysis.RecurringEvidence{
			Jobs: 40, Failures: 6,
			Groups: []analysis.RecurringGroup{{Repository: "acme/app", Workflow: "ci.yml", Job: job, Runs: 9, Failures: 4, Branches: 2,
				FirstFailureAt: analyseSince.Add(time.Hour), LastFailureAt: analyseSince.Add(3 * time.Hour), FailedStep: "Run tests",
				StepFailures: 3, RetryPasses: 3, Class: analysis.ClassFlaky, LastJobID: "job_9"}},
		},
	}
}

func TestAnalyseRecurringFailuresPrintsEachJobAndWhatItLooksLike(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/analysis/recurring-failures" {
			t.Errorf("unexpected request %s", r.URL)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(analyseBody(t, fixtureRecurring("test"))))
	}))
	t.Cleanup(srv.Close)
	out, _ := runCLI(t, "analyse", "recurring-failures", "--url", srv.URL)
	for _, want := range []string{"failed 4 of its 9 runs", "acme/app", "ci.yml", "4 of 9", "Run tests", "passed since", "flaky",
		"1. Read the failed step's log"} {
		if !strings.Contains(out, want) {
			t.Errorf("recurring-failures does not say %q:\n%s", want, out)
		}
	}
}

func TestAnalyseQueueWaitPrintsTheSummaryTheStepsAndThePools(t *testing.T) {
	var calls atomic.Int32
	body := analyseBody(t, fixtureQueueWait("build"))
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		q := r.URL.Query()
		if r.Method != http.MethodGet || r.URL.Path != "/api/v1/analysis/queue-wait" || q.Get("pool_id") != "pool_linux" {
			t.Errorf("unexpected request %s %s", r.Method, r.URL)
		}
		// A duration ago reaches the route as the instant it names, which is
		// the only form the route reads.
		if _, err := time.Parse(time.RFC3339, q.Get("since")); err != nil {
			t.Errorf("since %q is not an RFC 3339 instant", q.Get("since"))
		}
		if q.Get("until") != "2026-10-13T08:00:00Z" {
			t.Errorf("until = %q", q.Get("until"))
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)

	out, _ := runCLI(t, "analyse", "queue-wait", "--pool", "pool_linux", "--since", "6h", "--until", "2026-10-13T08:00:00Z", "--url", srv.URL)
	for _, want := range []string{
		"Jobs waited 3h 10m in all, most of it for runners to start (60%).",
		"medium: the job read was cut at the newest 5000",
		"2026-10-12T08:00:00Z to 2026-10-13T08:00:00Z",
		"runner_startup 1h 54m (60%)",
		"pool_linux", "linux-x64", "job_slow", "acme/widgets", "21m",
		"1. Keep warm runners so jobs do not wait for one to start. (setting pool.min_runners, code pool.queue_wait_high)",
		"2. Read why runners are slow to start on this pool. (code runner.startup_slow)",
		"https://zoomies.sh/problem-codes/#runnerstartup_slow",
		"2 jobs waited past eligibility on a runner its pool keeps between jobs",
		"The job read was cut at the newest 5000",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("queue-wait does not say %q:\n%s", want, out)
		}
	}
	if calls.Load() != 1 {
		t.Errorf("spent %d reads, want one", calls.Load())
	}
}

func TestAnalyseHostPressureNeedsAHost(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls.Add(1) }))
	t.Cleanup(srv.Close)
	_, errOut, code := runCLIFailing(t, "analyse", "host-pressure", "--url", srv.URL)
	if code != exitUsage {
		t.Fatalf("exit %d, want %d", code, exitUsage)
	}
	if !strings.Contains(errOut, "--host") {
		t.Errorf("the usage error does not name --host:\n%s", errOut)
	}
	if calls.Load() != 0 {
		t.Errorf("a request was spent on a command that could not be answered")
	}
}

func TestAnalyseCompareRefusesAByItDoesNotKnow(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls.Add(1) }))
	t.Cleanup(srv.Close)
	_, errOut, code := runCLIFailing(t, "analyse", "compare", "--by", "month", "--url", srv.URL)
	if code != exitUsage {
		t.Fatalf("exit %d, want %d", code, exitUsage)
	}
	if !strings.Contains(errOut, `"month"`) || !strings.Contains(errOut, "version or period") {
		t.Errorf("the usage error does not say what --by takes:\n%s", errOut)
	}
	if calls.Load() != 0 {
		t.Errorf("a request was spent on a --by the route would refuse")
	}
}

// The route refuses a release named with a comparison by period, since the
// answer would read as one about that release; the CLI says so before it
// spends a request, in the route's words with the flags' names.
func TestAnalyseCompareByPeriodRefusesARelease(t *testing.T) {
	for _, flag := range []string{"--a", "--b"} {
		var calls atomic.Int32
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls.Add(1) }))
		t.Cleanup(srv.Close)
		_, errOut, code := runCLIFailing(t, "analyse", "compare", "--by", "period", flag, "v1.3.2", "--url", srv.URL)
		if code != exitUsage {
			t.Fatalf("%s: exit %d, want %d", flag, code, exitUsage)
		}
		if !strings.Contains(errOut, "--a and --b name releases, which a comparison by period does not read") {
			t.Errorf("%s: the usage error does not say why:\n%s", flag, errOut)
		}
		if calls.Load() != 0 {
			t.Errorf("%s: a request was spent", flag)
		}
	}
}

// A refusal is the route's own sentence (which release, what to change) with
// its stable code beside it, so a script and a person read the same refusal. A
// sentence of the CLI's own would be a guess at what the controller meant.
func TestAnalyseCompareShowsTheRoutesRefusal(t *testing.T) {
	sentence := controller.ErrTooFewReleases.Error()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := json.Marshal(map[string]any{"error": map[string]string{"code": "analysis.too_few_releases", "message": sentence}})
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusConflict)
		_, _ = w.Write(body)
	}))
	t.Cleanup(srv.Close)
	_, errOut, code := runCLIFailing(t, "analyse", "compare", "--url", srv.URL)
	if code != exitError {
		t.Fatalf("exit %d, want %d", code, exitError)
	}
	if !strings.Contains(errOut, sentence) {
		t.Errorf("the route's sentence did not reach the terminal:\n%s", errOut)
	}
	if !strings.Contains(errOut, "(analysis.too_few_releases)") {
		t.Errorf("the refusal's code did not reach the terminal:\n%s", errOut)
	}
}

// With both ends of the period unreadable, the error names the same one every
// time: an answer that changes between two runs of one command reads as two
// different mistakes.
func TestAnalyseNamesTheSameBadFlagEveryTime(t *testing.T) {
	for range 50 {
		_, errOut, code := runCLIFailing(t, "analyse", "queue-wait", "--since", "soon", "--until", "later", "--url", "http://127.0.0.1:1")
		if code != exitUsage {
			t.Fatalf("exit %d, want %d", code, exitUsage)
		}
		if !strings.Contains(errOut, `--since "soon"`) || strings.Contains(errOut, "--until") {
			t.Fatalf("the error does not name --since alone:\n%s", errOut)
		}
	}
}

// A comparison's sides each cover their own span: a release side runs from
// its first job to its last, and a side read from the roll-up covers whole
// days. The period asked for is neither, so it is not what the sides show.
func TestAnalyseCompareShowsEachSidesOwnDates(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if r.URL.Path != "/api/v1/analysis/compare" || q.Get("by") != "version" || q.Get("a") != "v1.3.2" || q.Get("b") != "v1.3.3" {
			t.Errorf("unexpected request %s", r.URL)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(analyseBody(t, fixtureCompare())))
	}))
	t.Cleanup(srv.Close)
	out, _ := runCLI(t, "analyse", "compare", "--a", "v1.3.2", "--b", "v1.3.3", "--url", srv.URL)
	for _, want := range []string{
		"Under v1.3.3 duration p95 went from 5m to 9m.",
		"2026-10-10T20:00:00Z", "2026-10-12T06:00:00Z", "asked until 2026-10-12T07:30:00Z",
		"2026-10-12T09:15:00Z", "2026-10-13T07:45:00Z",
		"duration_p95", "regressed", "5m", "9m", "1.80",
		"cancelled and skipped jobs, from every figure",
		"1. Jobs ran longer under v1.3.3 (duration p95 5m to 9m). (code jobs.duration_regressed)",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("compare does not say %q:\n%s", want, out)
		}
	}
	// b read to the end it was asked for, so it carries no asked-until; and
	// the request's own period is shown nowhere.
	if strings.Count(out, "asked until") != 1 {
		t.Errorf("asked until shown %d times, want once (side a only):\n%s", strings.Count(out, "asked until"), out)
	}
	if strings.Contains(out, "2026-10-12T08:00:00Z") || strings.Contains(out, "2026-10-13T08:00:00Z") {
		t.Errorf("compare shows the request's period rather than each side's:\n%s", out)
	}
}

// A token that cannot read jobs gets host pressure without the names. The
// table must not print an empty name as if it were one, and the person is told
// once, by the controller's own step, which names neither a setting nor a code.
func TestAnalyseHostPressureSaysOnceThatJobNamesWereWithheld(t *testing.T) {
	srv := jsonRoutes(t, map[string]string{"/api/v1/analysis/host-pressure": analyseBody(t, fixtureWithheld())})
	out, _ := runCLI(t, "analyse", "host-pressure", "--host", "host_b", "--url", srv.URL)
	if n := strings.Count(out, analysis.JobNamesWithheldText); n != 1 {
		t.Errorf("the withheld names are said %d times, want once:\n%s", n, out)
	}
	for _, line := range strings.Split(out, "\n") {
		if strings.Contains(line, analysis.JobNamesWithheldText) && strings.Contains(line, "(") {
			t.Errorf("the withheld step names a setting or a code it does not have:\n%s", line)
		}
	}
	for _, bad := range []string{`""`, "REPO", "WORKFLOW", "JOB NAME"} {
		if strings.Contains(out, bad) {
			t.Errorf("host pressure printed %q for names it was not given:\n%s", bad, out)
		}
	}
	for _, want := range []string{"job_1", "job_2", "under pressure", "not under pressure", "build-1", "load_per_cpu"} {
		if !strings.Contains(out, want) {
			t.Errorf("host pressure does not say %q:\n%s", want, out)
		}
	}
}

func TestAnalyseStartupFailuresPrintsEachGroupAndItsLastFailure(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if r.URL.Path != "/api/v1/analysis/startup-failures" || q.Get("pool_id") != "pool_arm" || q.Get("host_id") != "host_c" {
			t.Errorf("unexpected request %s", r.URL)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(analyseBody(t, fixtureStartup("pull access denied for ghcr.io/acme/runner"))))
	}))
	t.Cleanup(srv.Close)
	out, _ := runCLI(t, "analyse", "startup-failures", "--pool", "pool_arm", "--host", "host_c", "--url", srv.URL)
	for _, want := range []string{
		"Runners on build-2 failed to start 9 times",
		"host_c", "build-2", "9 of 12", "before_container", "still failing",
		"pool_arm", "arm64", "recovered 2026-10-12T13:00:00Z",
		"run_9", "pull access denied for ghcr.io/acme/runner",
		"1. Check the image build-2 pulls. (setting pool.image, code runner.image_pull_failed)",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("startup-failures does not say %q:\n%s", want, out)
		}
	}
}

// Job names, workflows and repositories are written by whoever can open a pull
// request, and a runner's failure message can quote one. Printed raw, an escape
// sequence in one could clear the screen or forge a line the controller never
// wrote. Each analysis must print a hostile name in as many lines as a plain
// one, with no control character, and with the text still there to be read.
func TestAnalyseNeverPrintsAControlCharacterFromAName(t *testing.T) {
	var hostile string
	if err := json.Unmarshal([]byte(`"`+hostileJSON+`"`), &hostile); err != nil {
		t.Fatal(err)
	}
	hostile = "x\x1b[2J" + hostile
	cases := []struct {
		name  string
		route string
		args  []string
		body  func(string) any
	}{
		{"queue-wait", "/api/v1/analysis/queue-wait", []string{"queue-wait"}, func(n string) any { return fixtureQueueWait(n) }},
		{"host-pressure", "/api/v1/analysis/host-pressure", []string{"host-pressure", "--host", "host_b"}, func(n string) any { return fixtureHostPressure(n) }},
		{"startup-failures", "/api/v1/analysis/startup-failures", []string{"startup-failures"}, func(n string) any { return fixtureStartup(n) }},
		{"recurring-failures", "/api/v1/analysis/recurring-failures", []string{"recurring-failures"}, func(n string) any { return fixtureRecurring(n) }},
	}
	for _, c := range cases {
		run := func(name string) string {
			srv := jsonRoutes(t, map[string]string{c.route: analyseBody(t, c.body(name))})
			out, _ := runCLI(t, append(append([]string{"analyse"}, c.args...), "--url", srv.URL)...)
			return out
		}
		lines := strings.Count(run("build"), "\n")
		out := run(hostile)
		assertNoTerminalControl(t, c.name, out, lines)
		if !strings.Contains(out, "pwned") {
			t.Errorf("%s dropped the hostile name instead of printing it escaped:\n%q", c.name, out)
		}
	}
}

func TestAnalyseJSONIsTheRoutesBodyUnchanged(t *testing.T) {
	cases := []struct {
		route string
		args  []string
		body  string
	}{
		{"/api/v1/analysis/queue-wait", []string{"queue-wait"}, analyseBody(t, fixtureQueueWait("build"))},
		{"/api/v1/analysis/compare", []string{"compare"}, analyseBody(t, fixtureCompare())},
		{"/api/v1/analysis/host-pressure", []string{"host-pressure", "--host", "host_b"}, analyseBody(t, fixtureWithheld())},
		{"/api/v1/analysis/startup-failures", []string{"startup-failures"}, analyseBody(t, fixtureStartup("exit 1"))},
	}
	for _, c := range cases {
		// A field this CLI does not know survives, because the bytes are the
		// route's and not a re-marshalling of the CLI's own types.
		body := strings.TrimSuffix(c.body, "}") + `,"added_later":{"n":1}}`
		srv := jsonRoutes(t, map[string]string{c.route: body})
		out, _ := runCLI(t, append(append([]string{"analyse"}, c.args...), "--url", srv.URL, "--output", "json")...)
		var got, want any
		if err := json.Unmarshal([]byte(out), &got); err != nil {
			t.Fatalf("%s: not JSON: %v\n%s", c.route, err, out)
		}
		if err := json.Unmarshal([]byte(body), &want); err != nil {
			t.Fatal(err)
		}
		g, _ := json.Marshal(got)
		w, _ := json.Marshal(want)
		if string(g) != string(w) {
			t.Errorf("%s: --output json changed the body:\n got %s\nwant %s", c.route, g, w)
		}
	}
}

// An answer of another analysis or another version is refused, not read as
// this one: a field that means something else under v2 would be printed as
// what it meant under v1. The sentence is the MCP tools' own (P30), pinned in
// both places.
func TestAnalyseRefusesAFindingOfAnotherAnalysisOrVersion(t *testing.T) {
	const sentence = "the controller's analysis is not in the shape this tool expects; upgrade the controller and the CLI together"
	newer := fixtureQueueWait("build")
	newer.Version = "queue-wait-v2"
	for name, body := range map[string]string{
		"a newer version":  analyseBody(t, newer),
		"another analysis": analyseBody(t, fixtureCompare()),
	} {
		for _, output := range []string{"table", "json"} {
			srv := jsonRoutes(t, map[string]string{"/api/v1/analysis/queue-wait": body})
			out, errOut, code := runCLIFailing(t, "analyse", "queue-wait", "--url", srv.URL, "--output", output)
			if code != exitError {
				t.Errorf("%s, %s: exit %d, want %d", name, output, code, exitError)
			}
			if !strings.Contains(errOut, sentence) {
				t.Errorf("%s, %s: the refusal is not the upgrade sentence:\n%s", name, output, errOut)
			}
			if out != "" {
				t.Errorf("%s, %s: printed an answer it refused:\n%s", name, output, out)
			}
		}
	}
}
