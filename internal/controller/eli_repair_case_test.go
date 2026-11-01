package controller

import (
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/eyupio/zoomies/internal/github"
	"github.com/eyupio/zoomies/internal/store"
)

// seedJob stores a finished job of the setup's repository: a repair's case is
// read from the jobs the controller already holds, so the tests give it some.
func (s *commandSetup) seedJob(t *testing.T, j store.Job) {
	t.Helper()
	if j.Repo == "" {
		j.Repo = s.repo
	}
	if j.State == "" {
		j.State = store.JobCompleted
	}
	if _, err := s.h.st.UpsertJob(s.h.ctx, &j); err != nil {
		t.Fatal(err)
	}
}

// failingAt is the steps of a job that stopped at one of them, the way GitHub
// reports it: the steps before it passed and the ones after it never ran.
func failingAt(step string) store.JobSteps {
	return store.JobSteps{
		{Number: 1, Name: "Set up job", Status: "completed", Conclusion: "success"},
		{Number: 2, Name: step, Status: "completed", Conclusion: "failure"},
		{Number: 3, Name: "Complete job", Status: "completed", Conclusion: "skipped"},
	}
}

// pullFor is the pull request a case is about, as the GitHub client reads it.
func pullFor(head string) github.RepairPull {
	return github.RepairPull{Number: 1, Title: "Retry transient 502s", Author: "dana", Branch: "feature", URL: "https://github.com/acme/repo/pull/1", HeadSHA: head, Open: true}
}

// A person deciding whether to let Eli try needs to see what failed, in the
// words GitHub shows: the jobs, the step each stopped at, and where to look.
func TestARepairCaseNamesTheFailedJobsAndTheirSteps(t *testing.T) {
	s := newCommandSetup(t)
	base := s.h.st.Now().Add(-time.Hour)
	started, finished := base.Add(5*time.Second), base.Add(100*time.Second)
	run := "https://github.com/acme/repo/actions/runs/900"
	s.seedJob(t, store.Job{GitHubJobID: 11, GitHubRunID: 900, Workflow: "CI", RunNumber: 1042, JobName: "unit", Conclusion: "failure", HTMLURL: run + "/job/11", Steps: failingAt("Run go test ./..."), QueuedAt: base, StartedAt: &started, CompletedAt: &finished})
	s.seedJob(t, store.Job{GitHubJobID: 12, GitHubRunID: 900, Workflow: "CI", RunNumber: 1042, JobName: "integration", Conclusion: "timed_out", HTMLURL: run + "/job/12", Steps: failingAt("Run integration suite"), QueuedAt: base.Add(time.Second)})
	s.seedJob(t, store.Job{GitHubJobID: 13, GitHubRunID: 900, Workflow: "CI", RunNumber: 1042, JobName: "lint", Conclusion: "success", HTMLURL: run + "/job/13", QueuedAt: base.Add(2 * time.Second)})
	// Another run's failure is not what this repair was started for.
	s.seedJob(t, store.Job{GitHubJobID: 14, GitHubRunID: 901, Workflow: "Docs", RunNumber: 7, JobName: "site", Conclusion: "failure", QueuedAt: base.Add(3 * time.Second)})

	got := s.h.c.repairCase(s.h.ctx, s.fake, s.repo, pullFor("old"), 900)
	want := &store.EliRepairCase{
		PullTitle: "Retry transient 502s", PullURL: "https://github.com/acme/repo/pull/1", Author: "dana", Branch: "feature",
		RunURL: run, Workflow: "CI", RunNumber: 1042,
		Jobs: []store.EliRepairCaseJob{
			{Name: "unit", Step: "Run go test ./...", URL: run + "/job/11", DurationMS: 95000},
			{Name: "integration", Step: "Run integration suite", URL: run + "/job/12"},
		},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("case = %+v\nwant   %+v", got, want)
	}
}

// A repair asked for from a pull request has no run to name. The commit its
// checks failed on is what ties the jobs to it, and a job of an earlier commit,
// a passing one or another repository's must not be read as its failure.
func TestARepairCaseFindsJobsByHeadWhenThereIsNoRun(t *testing.T) {
	s := newCommandSetup(t)
	base := s.h.st.Now().Add(-time.Hour)
	s.seedJob(t, store.Job{GitHubJobID: 21, GitHubRunID: 910, HeadSHA: "old", Workflow: "CI", RunNumber: 12, JobName: "unit", Conclusion: "failure", Steps: failingAt("Run tests"), QueuedAt: base})
	s.seedJob(t, store.Job{GitHubJobID: 22, GitHubRunID: 909, HeadSHA: "older", Workflow: "CI", RunNumber: 11, JobName: "unit", Conclusion: "failure", QueuedAt: base.Add(time.Second)})
	s.seedJob(t, store.Job{GitHubJobID: 23, GitHubRunID: 910, HeadSHA: "old", Workflow: "CI", RunNumber: 12, JobName: "lint", Conclusion: "success", QueuedAt: base.Add(2 * time.Second)})
	s.seedJob(t, store.Job{GitHubJobID: 24, GitHubRunID: 777, Repo: "acme/other", HeadSHA: "old", Workflow: "CI", RunNumber: 3, JobName: "unit", Conclusion: "failure", QueuedAt: base.Add(3 * time.Second)})

	got := s.h.c.repairCase(s.h.ctx, s.fake, s.repo, pullFor("old"), 0)
	if got == nil || len(got.Jobs) != 1 || got.Jobs[0].Name != "unit" || got.Jobs[0].Step != "Run tests" {
		t.Fatalf("case = %+v, want the one failed job of the head commit", got)
	}
	if got.Workflow != "CI" || got.RunNumber != 12 {
		t.Errorf("workflow %q run %d, want CI #12", got.Workflow, got.RunNumber)
	}
}

// Retention removes old jobs, and an offer can outlive them. The repair can
// still be started, and what is known about the pull request is still worth
// showing; an empty list of jobs is a case with less in it, not an error.
func TestARepairCaseSurvivesAPrunedRun(t *testing.T) {
	s := newCommandSetup(t)
	got := s.h.c.repairCase(s.h.ctx, s.fake, s.repo, pullFor("old"), 900)
	want := &store.EliRepairCase{PullTitle: "Retry transient 502s", PullURL: "https://github.com/acme/repo/pull/1", Author: "dana", Branch: "feature"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("case = %+v, want the pull request alone: %+v", got, want)
	}
}

// Everything in a case is written by someone who can open a pull request, and
// it is stored and drawn as links. A scheme that runs script is never a link,
// an address too long to be GitHub's is dropped rather than cut into one that
// goes somewhere else, and a title the size of a document is clipped on a
// character, not in the middle of one.
func TestARepairCaseClipsAndRefusesUnsafeAddresses(t *testing.T) {
	s := newCommandSetup(t)
	base := s.h.st.Now().Add(-time.Hour)
	long := strings.Repeat("é", 5000)
	tooLong := "https://github.com/acme/repo/actions/runs/900/job/32?" + strings.Repeat("a", 600)
	s.seedJob(t, store.Job{GitHubJobID: 31, GitHubRunID: 900, Workflow: long, RunNumber: 5, JobName: long, Conclusion: "failure", HTMLURL: "javascript:alert(document.cookie)", Steps: failingAt(long), QueuedAt: base})
	s.seedJob(t, store.Job{GitHubJobID: 32, GitHubRunID: 900, Workflow: long, RunNumber: 5, JobName: "second", Conclusion: "failure", HTMLURL: tooLong, Steps: failingAt("Run tests"), QueuedAt: base.Add(time.Second)})
	// A scheme with a host parses like a web address, so the host alone does not
	// keep it out: only http and https are links.
	s.seedJob(t, store.Job{GitHubJobID: 33, GitHubRunID: 900, Workflow: long, RunNumber: 5, JobName: "third", Conclusion: "failure", HTMLURL: "ftp://github.com/acme/repo/actions/runs/900/job/33", Steps: failingAt("Run tests"), QueuedAt: base.Add(2 * time.Second)})
	pull := github.RepairPull{Number: 1, Title: long, Author: long, Branch: long, URL: "javascript://github.com/acme/repo/pull/1%0Aalert(document.cookie)", HeadSHA: "old", Open: true}

	got := s.h.c.repairCase(s.h.ctx, s.fake, s.repo, pull, 900)
	if got == nil || len(got.Jobs) != 3 {
		t.Fatalf("case = %+v", got)
	}
	for name, text := range map[string]string{"title": got.PullTitle, "author": got.Author, "branch": got.Branch, "workflow": got.Workflow, "job": got.Jobs[0].Name, "step": got.Jobs[0].Step} {
		if n := utf8.RuneCountInString(text); n != 200 || !utf8.ValidString(text) {
			t.Errorf("%s is %d runes (valid %v), want 200 whole characters", name, n, utf8.ValidString(text))
		}
	}
	if got.PullURL != "" || got.RunURL != "" || got.Jobs[0].URL != "" || got.Jobs[1].URL != "" || got.Jobs[2].URL != "" {
		t.Errorf("an address that is not a safe link was kept: pull %q run %q jobs %q %q %q", got.PullURL, got.RunURL, got.Jobs[0].URL, got.Jobs[1].URL, got.Jobs[2].URL)
	}
}

// A failure this fleet never ran is on record nowhere in Zoomies: an older
// agent's run, a pruned one, or a job GitHub's own runners ran. The case still
// names what failed, read from GitHub's report of the run.
func TestARepairCaseReadsTheFailedJobsFromGitHubWhenTheFleetHasNoRecord(t *testing.T) {
	s := newCommandSetup(t)
	base := s.h.st.Now().Add(-time.Hour)
	started, finished := base, base.Add(95*time.Second)
	s.fake.failedJobs = &github.RepairFailureRun{
		RunID: 900, RunURL: "https://github.com/acme/repo/actions/runs/900", Workflow: "Publish", RunNumber: 1042,
		Jobs: []github.RepairFailedJob{
			{ID: 11, Name: "build-and-push", Conclusion: "failure", URL: "https://github.com/acme/repo/actions/runs/900/job/11", Step: "Install dependencies and build", StepNumber: 8, StartedAt: started, CompletedAt: finished},
			{ID: 12, Name: "notify", Conclusion: "timed_out", URL: "https://github.com/acme/repo/actions/runs/900/job/12"},
		},
	}

	got := s.h.c.repairCase(s.h.ctx, s.fake, s.repo, pullFor("old"), 900)
	want := &store.EliRepairCase{
		PullTitle: "Retry transient 502s", PullURL: "https://github.com/acme/repo/pull/1", Author: "dana", Branch: "feature",
		RunURL: "https://github.com/acme/repo/actions/runs/900", Workflow: "Publish", RunNumber: 1042,
		Jobs: []store.EliRepairCaseJob{
			{Name: "build-and-push", Step: "Install dependencies and build", URL: "https://github.com/acme/repo/actions/runs/900/job/11", DurationMS: 95000},
			{Name: "notify", Step: "", URL: "https://github.com/acme/repo/actions/runs/900/job/12"},
		},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("case = %+v\nwant   %+v", got, want)
	}
}

// The fleet's own record is preferred where it exists: it is what the person
// was looking at, and it costs no GitHub call.
func TestARepairCasePrefersTheFleetsRecordOverGitHubs(t *testing.T) {
	s := newCommandSetup(t)
	base := s.h.st.Now().Add(-time.Hour)
	s.seedJob(t, store.Job{GitHubJobID: 11, GitHubRunID: 900, Workflow: "CI", RunNumber: 1042, JobName: "unit", Conclusion: "failure", HTMLURL: "https://github.com/acme/repo/actions/runs/900/job/11", Steps: failingAt("Run go test ./..."), QueuedAt: base})
	s.fake.failedJobs = &github.RepairFailureRun{
		RunID: 900, RunURL: "https://github.com/acme/repo/actions/runs/900", Workflow: "Publish", RunNumber: 1042,
		Jobs: []github.RepairFailedJob{{ID: 99, Name: "someone-elses-job", Conclusion: "failure"}},
	}

	got := s.h.c.repairCase(s.h.ctx, s.fake, s.repo, pullFor("old"), 900)
	if len(got.Jobs) != 1 || got.Jobs[0].Name != "unit" || got.Workflow != "CI" {
		t.Fatalf("case = %+v, want the fleet's own record of the failure", got)
	}
	if len(s.fake.failedJobsAsked) != 0 {
		t.Errorf("GitHub was asked for jobs the fleet has on record: %v", s.fake.failedJobsAsked)
	}
}

// A GitHub that cannot answer leaves the case as the pull request alone: a
// case with less in it, not an error.
func TestARepairCaseSurvivesGitHubNotAnswering(t *testing.T) {
	s := newCommandSetup(t)
	s.fake.failedJobsErr = errors.New("github: read failed jobs: not found")
	got := s.h.c.repairCase(s.h.ctx, s.fake, s.repo, pullFor("old"), 900)
	want := &store.EliRepairCase{PullTitle: "Retry transient 502s", PullURL: "https://github.com/acme/repo/pull/1", Author: "dana", Branch: "feature"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("case = %+v, want the pull request alone: %+v", got, want)
	}
}

// The run's page is the job's page without the job, and only when the address
// really is a job's.
func TestRunURLFromJobURL(t *testing.T) {
	for _, tc := range []struct{ job, want string }{
		{"https://github.com/acme/repo/actions/runs/900/job/11", "https://github.com/acme/repo/actions/runs/900"},
		{"https://ghe.example.com/acme/repo/actions/runs/900/job/11", "https://ghe.example.com/acme/repo/actions/runs/900"},
		{"https://github.com/acme/repo/actions/runs/900/job/11/", "https://github.com/acme/repo/actions/runs/900"},
		{"https://github.com/acme/repo/actions/runs/900", "https://github.com/acme/repo/actions/runs/900"},
		{"https://github.com/acme/repo/actions/runs/900/job/abc", ""},
		{"https://github.com/acme/repo/actions/runs/900/jobs", ""},
		{"javascript:alert(1)/job/11", ""},
		{"javascript://github.com/acme/repo/actions/runs/900/job/11", ""},
		{"ftp://github.com/acme/repo/actions/runs/900/job/11", ""},
		{"", ""},
	} {
		if got := runURLFromJobURL(tc.job); got != tc.want {
			t.Errorf("runURLFromJobURL(%q) = %q, want %q", tc.job, got, tc.want)
		}
	}
}

// What the person asked Eli to fix is kept as it was when they asked. The page
// they land on reads it back from the row, not from GitHub, which moves on.
func TestAUIRepairRecordsItsRunAndCase(t *testing.T) {
	s := readinessSetup(t)
	s.fake.title, s.fake.author, s.fake.url = "Retry transient 502s", "dana", "https://github.com/acme/repo/pull/1"
	run := "https://github.com/acme/repo/actions/runs/900"
	s.seedJob(t, store.Job{GitHubJobID: 11, GitHubRunID: 900, HeadSHA: "old", Workflow: "CI", RunNumber: 1042, JobName: "unit", Conclusion: "failure", HTMLURL: run + "/job/11", Steps: failingAt("Run go test ./..."), QueuedAt: s.h.st.Now().Add(-time.Hour)})

	returned, err := s.h.c.RequestPinnedEliRepair(s.h.ctx, s.user.ID, s.repo, 1, "", "old", 900, false)
	if err != nil {
		t.Fatal(err)
	}
	rows := s.rows(t)
	if len(rows) != 1 {
		t.Fatalf("%d repairs, want one", len(rows))
	}
	got := rows[0]
	if got.RunID != 900 || returned.RunID != 900 {
		t.Errorf("run id: stored %d, returned %d, want 900", got.RunID, returned.RunID)
	}
	if got.Details.Case == nil || got.Details.Case.PullTitle != "Retry transient 502s" || got.Details.Case.RunURL != run ||
		len(got.Details.Case.Jobs) != 1 || got.Details.Case.Jobs[0].Step != "Run go test ./..." {
		t.Errorf("case = %+v, want the pull request, the run and its failed job", got.Details.Case)
	}
	if returned.Details.Case == nil {
		t.Error("the row handed back to the caller has no case")
	}
}

// An offer remembers the run whose failure raised it. Accepting it is the same
// request as any other, and the repair carries that run.
func TestAcceptingAnOfferRecordsTheOffersRun(t *testing.T) {
	s := readinessSetup(t)
	if err := s.h.st.SetEliRepairPolicy(s.h.ctx, store.EliRepairPolicy{Repo: s.repo, InstallationID: s.inst.ID, Enabled: true, AskFirst: true}); err != nil {
		t.Fatal(err)
	}
	s.seedJob(t, store.Job{GitHubJobID: 1, GitHubRunID: 9, HeadSHA: "old", Workflow: "CI", RunNumber: 31, JobName: "unit", Conclusion: "failure", Steps: failingAt("Run tests"), QueuedAt: s.h.st.Now().Add(-time.Hour)})
	body, _ := json.Marshal(map[string]any{"action": "completed", "repository": map[string]any{"full_name": s.repo}, "workflow_job": map[string]any{"id": 1, "run_id": 9, "head_sha": "old", "conclusion": "failure", "status": "completed"}})
	if err := s.h.c.enqueueRepairWebhook(s.h.ctx, s.inst, "workflow_job", body); err != nil {
		t.Fatal(err)
	}
	offers, err := s.h.c.EliOpportunities(s.h.ctx, s.user.ID)
	if err != nil || len(offers) != 1 || offers[0].RunID != 9 {
		t.Fatalf("offers %+v %v, want one for run 9", offers, err)
	}
	returned, err := s.h.c.ActEliOpportunity(s.h.ctx, s.user.ID, offers[0].ID, "accept", false)
	if err != nil {
		t.Fatal(err)
	}
	rows := s.rows(t)
	if len(rows) != 1 || rows[0].RunID != 9 || returned.RunID != 9 {
		t.Fatalf("stored %+v, returned run %d, want the offer's run 9", rows, returned.RunID)
	}
	if c := rows[0].Details.Case; c == nil || c.Workflow != "CI" || c.RunNumber != 31 || len(c.Jobs) != 1 {
		t.Errorf("case = %+v, want the run's failed job", c)
	}
}

// A repair asked for in a pull request comment has no page-time snapshot: the
// webhook only knows the pull request. The worker is the first to read it, so
// it records the case then, and the attempt page has the same facts as one
// started from the UI.
func TestTheWorkerFillsAMissingCase(t *testing.T) {
	s := newCommandSetup(t)
	s.link(t, true)
	// Settled checks make the worker stop once it has read the pull request,
	// which is all this test needs of it.
	s.fake.check = "passed"
	s.fake.title, s.fake.author, s.fake.url = "Retry transient 502s", "dana", "https://github.com/acme/repo/pull/1"
	s.seedJob(t, store.Job{GitHubJobID: 21, GitHubRunID: 910, HeadSHA: "old", Workflow: "CI", RunNumber: 12, JobName: "unit", Conclusion: "failure", Steps: failingAt("Run tests"), QueuedAt: s.h.st.Now().Add(-time.Hour)})
	row := &store.EliRepair{DedupKey: "comment:100", Repo: s.repo, InstallationID: s.inst.ID, PullNumber: 1, HeadSHA: "old", Trigger: "mention", UserID: s.user.ID, GitHubUserID: 42, GitHubLogin: "octo"}
	if _, err := s.h.st.EnqueueEliRepair(s.h.ctx, row); err != nil {
		t.Fatal(err)
	}
	s.h.c.runEliRepair(s.h.ctx, row)
	rows := s.rows(t)
	if len(rows) != 1 {
		t.Fatalf("%d repairs, want one", len(rows))
	}
	c := rows[0].Details.Case
	if c == nil || c.PullTitle != "Retry transient 502s" || len(c.Jobs) != 1 || c.Jobs[0].Name != "unit" {
		t.Errorf("case = %+v, want the pull request and its failed job", c)
	}
}

// The case a request recorded is the failure the person saw. The worker runs
// later, when the pull request may read differently, and must not rewrite it.
func TestTheWorkerKeepsTheCaseARequestRecorded(t *testing.T) {
	s := newCommandSetup(t)
	s.link(t, true)
	s.fake.check = "passed"
	s.fake.title = "Retitled since"
	row := &store.EliRepair{DedupKey: "ui:recorded", Repo: s.repo, InstallationID: s.inst.ID, PullNumber: 1, HeadSHA: "old", Trigger: "mention", UserID: s.user.ID, GitHubUserID: 42, GitHubLogin: "octo",
		Details: store.EliRepairDetails{Case: &store.EliRepairCase{PullTitle: "As requested"}}}
	if _, err := s.h.st.EnqueueEliRepair(s.h.ctx, row); err != nil {
		t.Fatal(err)
	}
	s.h.c.runEliRepair(s.h.ctx, row)
	rows := s.rows(t)
	if len(rows) != 1 || rows[0].Details.Case == nil || rows[0].Details.Case.PullTitle != "As requested" {
		t.Fatalf("case = %+v, want the one the request recorded", rows[0].Details.Case)
	}
}
