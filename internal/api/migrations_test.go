package api

import (
	"fmt"
	"net/http"
	"slices"
	"strings"
	"testing"

	"github.com/eyupio/zoomies/internal/controller"
	"github.com/eyupio/zoomies/internal/migrate"
	"github.com/eyupio/zoomies/internal/store"
)

const (
	ciBefore = `name: CI

on: [push]

jobs:
  build:
    runs-on: ubuntu-latest      # the cheap one
    steps:
      - uses: actions/checkout@v4
  windows:
    runs-on: windows-latest
    steps:
      - run: build.ps1
  matrix:
    runs-on: ${{ matrix.os }}
    steps:
      - run: make test
`
	releaseBefore = "jobs:\n  ship:\n    runs-on: [ubuntu-22.04]\n"
)

// migrationHarness is a fleet with one Linux pool, two repositories with
// workflows and one without.
func migrationHarness(t *testing.T) (*harness, *store.Installation, string) {
	t.Helper()
	h := newHarness(t)
	inst := h.installation()

	pool := h.pool(inst, "zoomies-linux-x64")
	pool.Labels = store.StringSlice(store.BrandLabels([]string{"zoomies-linux-x64", "linux", "x64"}))
	if err := h.st.UpdatePool(h.ctx, pool); err != nil {
		t.Fatalf("UpdatePool: %v", err)
	}

	h.gh.AddWorkflow("acme/widgets", ".github/workflows/ci.yml", ciBefore)
	h.gh.AddWorkflow("acme/widgets", ".github/workflows/release.yml", releaseBefore)
	h.gh.AddWorkflow("acme/api", ".github/workflows/ci.yml", ciBefore)
	h.gh.AddRepo("acme/site")

	_, cookie := h.user("migrator", store.RoleOperator)
	return h, inst, cookie
}

func TestMigrationPlanProposesAMappingAndADiff(t *testing.T) {
	h, inst, cookie := migrationHarness(t)

	resp := h.do(request{method: http.MethodPost, path: "/api/v1/migrations/plan", cookie: cookie,
		body: map[string]any{"installation_id": inst.ID}})
	resp.mustStatus(t, http.StatusOK, "plan")

	var plan migrationPlanResponse
	resp.into(t, &plan)

	// Every repository is reported, including the one with nothing to do:
	// "no workflows" is an answer, not an omission.
	if len(plan.Repositories) != 3 {
		t.Fatalf("repositories = %d, want all three", len(plan.Repositories))
	}

	// The server proposed the only pool for the Ubuntu labels, and nothing at
	// all for Windows, because no pool in this fleet runs it.
	if plan.Mapping["ubuntu-latest"] != "zoomies-linux-x64" {
		t.Errorf("mapping = %v, want ubuntu-latest on the Linux pool", plan.Mapping)
	}
	if plan.Mapping["ubuntu-22.04"] != "zoomies-linux-x64" {
		t.Errorf("mapping = %v, want ubuntu-22.04 on the Linux pool", plan.Mapping)
	}
	if _, ok := plan.Mapping["windows-latest"]; ok {
		t.Errorf("mapping = %v, want no proposal for windows-latest: nothing here runs Windows", plan.Mapping)
	}
	if !slices.Contains(plan.Unmapped, "windows-latest") {
		t.Errorf("unmapped = %v, want windows-latest named so the operator can decide", plan.Unmapped)
	}

	// Two repositories, three files, three Ubuntu jobs.
	if plan.Counts.Repos != 2 || plan.Counts.Workflows != 3 || plan.Counts.Jobs != 3 {
		t.Errorf("counts = %+v, want 2 repos / 3 workflows / 3 jobs", plan.Counts)
	}

	widgets := repoPlan(t, plan, "acme/widgets")
	ci := workflowPlan(t, widgets, ".github/workflows/ci.yml")
	if len(ci.Rewrites) != 1 || ci.Rewrites[0].Job != "build" {
		t.Fatalf("rewrites = %+v, want just the build job", ci.Rewrites)
	}
	if ci.Rewrites[0].To != "zoomies-linux-x64" {
		t.Errorf("to = %q, want the pool's label", ci.Rewrites[0].To)
	}
	// The Windows job and the matrix job are both left alone, each with a
	// reason a person can act on.
	if len(ci.Skips) != 2 {
		t.Fatalf("skips = %+v, want the windows and matrix jobs", ci.Skips)
	}
	if !strings.Contains(ci.Diff, "-    runs-on: ubuntu-latest      # the cheap one") ||
		!strings.Contains(ci.Diff, "+    runs-on: zoomies-linux-x64      # the cheap one") {
		t.Errorf("diff does not show the change with its comment intact:\n%s", ci.Diff)
	}

	if len(plan.Pools) != 1 || plan.Pools[0].RunsOn != "zoomies-linux-x64" {
		t.Errorf("pools = %+v, want the one pool with its runs-on value", plan.Pools)
	}
	// Nothing was written.
	if got, _ := h.gh.FileContent("acme/widgets", ".github/workflows/ci.yml"); got != ciBefore {
		t.Error("the plan endpoint modified a workflow")
	}
}

func TestMigrationPlanHonoursTheOperatorsMapping(t *testing.T) {
	h, inst, cookie := migrationHarness(t)

	resp := h.do(request{method: http.MethodPost, path: "/api/v1/migrations/plan", cookie: cookie,
		body: map[string]any{
			"installation_id": inst.ID,
			"repos":           []string{"acme/widgets"},
			"mapping":         map[string]string{"UBUNTU-LATEST": "zoomies-big", "windows-latest": ""},
		}})
	resp.mustStatus(t, http.StatusOK, "plan")

	var plan migrationPlanResponse
	resp.into(t, &plan)
	if len(plan.Repositories) != 1 {
		t.Fatalf("repositories = %d, want only the one asked for", len(plan.Repositories))
	}
	// The key is lowercased and the empty value is dropped, because that is
	// what the browser sends for a label the operator chose not to map.
	if plan.Mapping["ubuntu-latest"] != "zoomies-big" || len(plan.Mapping) != 1 {
		t.Fatalf("mapping = %v, want just the one the operator gave", plan.Mapping)
	}
	// ubuntu-22.04 is now unmapped, so release.yml is left alone.
	release := workflowPlan(t, repoPlan(t, plan, "acme/widgets"), ".github/workflows/release.yml")
	if release.Diff != "" || len(release.Rewrites) != 0 {
		t.Errorf("release.yml changed under a mapping that does not cover it: %+v", release)
	}
}

func TestMigrationPlanRefusesWithNoPool(t *testing.T) {
	h := newHarness(t)
	inst := h.installation()
	_, cookie := h.user("migrator", store.RoleOperator)

	resp := h.do(request{method: http.MethodPost, path: "/api/v1/migrations/plan", cookie: cookie,
		body: map[string]any{"installation_id": inst.ID}})
	resp.mustStatus(t, http.StatusUnprocessableEntity, "plan with no pool")
	if msg := resp.errorMessage(t); !strings.Contains(msg, "nowhere to migrate to") {
		t.Errorf("message = %q, want it to say there is no pool", msg)
	}
}

func TestMigrationOpensOnePullRequestPerRepository(t *testing.T) {
	h, inst, cookie := migrationHarness(t)

	resp := h.do(request{method: http.MethodPost, path: "/api/v1/migrations/pull-requests", cookie: cookie,
		body: map[string]any{
			"installation_id": inst.ID,
			"repos":           []string{"acme/widgets", "acme/api", "acme/site"},
			"mapping":         map[string]string{"ubuntu-latest": "zoomies-linux-x64", "ubuntu-22.04": "zoomies-linux-x64"},
		}})
	resp.mustStatus(t, http.StatusOK, "pull requests")

	var out migrationApplyResponse
	resp.into(t, &out)
	if out.Opened != 2 || out.Skipped != 1 || out.Failed != 0 {
		t.Fatalf("outcome = %+v, want two opened and the empty repository skipped", out)
	}

	widgets := result(t, out, "acme/widgets")
	if widgets.Status != "opened" || widgets.PullRequestURL == "" {
		t.Fatalf("acme/widgets = %+v", widgets)
	}
	if widgets.Workflows != 2 || widgets.Jobs != 2 {
		t.Errorf("acme/widgets changed %d files and %d jobs, want 2 and 2", widgets.Workflows, widgets.Jobs)
	}
	if !strings.HasPrefix(widgets.Branch, "zoomies/migrate-runners-") {
		t.Errorf("branch = %q, want a branded, dated branch", widgets.Branch)
	}

	site := result(t, out, "acme/site")
	if site.Status != "skipped" || !strings.Contains(site.Reason, "no .github/workflows") {
		t.Errorf("acme/site = %+v, want a skip that says why", site)
	}

	// The file on GitHub is the rewritten one, and only the runs-on changed.
	got, ok := h.gh.FileContent("acme/widgets", ".github/workflows/ci.yml")
	if !ok {
		t.Fatal("the workflow is gone")
	}
	if !strings.Contains(got, "runs-on: zoomies-linux-x64      # the cheap one") {
		t.Errorf("the committed file did not get the new label:\n%s", got)
	}
	if !strings.Contains(got, "runs-on: windows-latest") || !strings.Contains(got, "runs-on: ${{ matrix.os }}") {
		t.Errorf("the committed file lost a job it should not have touched:\n%s", got)
	}
	if !strings.Contains(got, "on: [push]") || !strings.Contains(got, "- uses: actions/checkout@v4") {
		t.Errorf("the committed file was reformatted:\n%s", got)
	}

	// The default branch is untouched; the work is on its own branch.
	branches := h.gh.Branches("acme/widgets")
	if len(branches) != 2 || !slices.Contains(branches, "main") {
		t.Errorf("branches = %v, want main and the migration branch", branches)
	}
}

// An organisation is read a page at a time, and the pages fit together: the
// second one starts where the first stopped, and nothing appears in both. An
// operator who cannot reach the repository at the end of the alphabet cannot
// migrate it.
func TestMigrationPlanPagesThroughTheOrganisation(t *testing.T) {
	h, inst, cookie := migrationHarness(t)
	// Enough repositories that one plan cannot hold them.
	for i := 0; i < controller.MaxPlanRepos; i++ {
		h.gh.AddWorkflow(fmt.Sprintf("acme/z%02d", i), ".github/workflows/ci.yml", ciBefore)
	}

	first := h.do(request{method: http.MethodPost, path: "/api/v1/migrations/plan", cookie: cookie,
		body: map[string]any{"installation_id": inst.ID}})
	first.mustStatus(t, http.StatusOK, "first page")
	var page1 migrationPlanResponse
	first.into(t, &page1)

	if len(page1.Repositories) != controller.MaxPlanRepos {
		t.Fatalf("first page = %d repositories, want %d", len(page1.Repositories), controller.MaxPlanRepos)
	}
	if !page1.Truncated || page1.NextCursor == "" {
		t.Fatalf("first page says it is the whole organisation: truncated=%v cursor=%q", page1.Truncated, page1.NextCursor)
	}
	if want := controller.MaxPlanRepos + 3; page1.TotalRepos != want {
		t.Errorf("total_repos = %d, want %d, so the wizard can say what it is showing", page1.TotalRepos, want)
	}
	if last := page1.Repositories[len(page1.Repositories)-1].Repo; page1.NextCursor != last {
		t.Errorf("next_cursor = %q, want the last repository on the page (%q)", page1.NextCursor, last)
	}

	second := h.do(request{method: http.MethodPost, path: "/api/v1/migrations/plan", cookie: cookie,
		body: map[string]any{"installation_id": inst.ID, "cursor": page1.NextCursor}})
	second.mustStatus(t, http.StatusOK, "second page")
	var page2 migrationPlanResponse
	second.into(t, &page2)

	if page2.Truncated || page2.NextCursor != "" {
		t.Errorf("second page wants a third: truncated=%v cursor=%q", page2.Truncated, page2.NextCursor)
	}
	seen := map[string]bool{}
	for _, r := range page1.Repositories {
		seen[r.Repo] = true
	}
	for _, r := range page2.Repositories {
		if seen[r.Repo] {
			t.Errorf("%s is on both pages", r.Repo)
		}
		seen[r.Repo] = true
	}
	if len(seen) != page1.TotalRepos {
		t.Errorf("the two pages hold %d repositories, want all %d", len(seen), page1.TotalRepos)
	}
}

// A repository with several workflow files is not all-or-nothing: the operator
// ticks the ones that should move, and the pull request touches those.
func TestMigrationOpensOnlyTheWorkflowFilesChosen(t *testing.T) {
	h, inst, cookie := migrationHarness(t)

	resp := h.do(request{method: http.MethodPost, path: "/api/v1/migrations/pull-requests", cookie: cookie,
		body: map[string]any{
			"installation_id": inst.ID,
			"repos":           []string{"acme/widgets"},
			"mapping":         map[string]string{"ubuntu-latest": "zoomies-linux-x64", "ubuntu-22.04": "zoomies-linux-x64"},
			"workflows":       map[string][]string{"acme/widgets": {".github/workflows/ci.yml"}},
		}})
	resp.mustStatus(t, http.StatusOK, "pull requests")

	var out migrationApplyResponse
	resp.into(t, &out)
	widgets := result(t, out, "acme/widgets")
	if widgets.Status != "opened" || widgets.Workflows != 1 || widgets.Jobs != 1 {
		t.Fatalf("acme/widgets = %+v, want one file and one job", widgets)
	}

	ci, _ := h.gh.FileContent("acme/widgets", ".github/workflows/ci.yml")
	if !strings.Contains(ci, "runs-on: zoomies-linux-x64") {
		t.Errorf("the chosen file was not migrated:\n%s", ci)
	}
	release, _ := h.gh.FileContent("acme/widgets", ".github/workflows/release.yml")
	if release != releaseBefore {
		t.Errorf("the file nobody chose was changed:\n%s", release)
	}
}

// A repository whose chosen files have nothing to move is skipped with a reason
// naming the choice, rather than silently opening a pull request on the files
// the operator left alone.
func TestMigrationSkipsARepositoryWhoseChosenFilesDoNotChange(t *testing.T) {
	h, inst, cookie := migrationHarness(t)

	resp := h.do(request{method: http.MethodPost, path: "/api/v1/migrations/pull-requests", cookie: cookie,
		body: map[string]any{
			"installation_id": inst.ID,
			"repos":           []string{"acme/widgets"},
			"mapping":         map[string]string{"ubuntu-latest": "zoomies-linux-x64"},
			"workflows":       map[string][]string{"acme/widgets": {".github/workflows/release.yml"}},
		}})
	resp.mustStatus(t, http.StatusOK, "pull requests")

	var out migrationApplyResponse
	resp.into(t, &out)
	widgets := result(t, out, "acme/widgets")
	if widgets.Status != "skipped" || !strings.Contains(widgets.Reason, "you chose") {
		t.Errorf("acme/widgets = %+v, want a skip that names the choice", widgets)
	}
}

// A path outside .github/workflows is a client bug, and accepting it would mean
// a pull request that quietly changed nothing.
func TestMigrationRefusesAPathThatIsNotAWorkflow(t *testing.T) {
	h, inst, cookie := migrationHarness(t)

	resp := h.do(request{method: http.MethodPost, path: "/api/v1/migrations/pull-requests", cookie: cookie,
		body: map[string]any{
			"installation_id": inst.ID,
			"repos":           []string{"acme/widgets"},
			"mapping":         map[string]string{"ubuntu-latest": "zoomies-linux-x64"},
			"workflows":       map[string][]string{"acme/widgets": {"Makefile"}},
		}})
	resp.mustStatus(t, http.StatusUnprocessableEntity, "not a workflow")
	if msg := resp.errorMessage(t); !strings.Contains(msg, "Makefile") {
		t.Errorf("message = %q, want it to name the path", msg)
	}
}

func TestMigrationRefusesToTouchEverythingByDefault(t *testing.T) {
	h, inst, cookie := migrationHarness(t)

	resp := h.do(request{method: http.MethodPost, path: "/api/v1/migrations/pull-requests", cookie: cookie,
		body: map[string]any{"installation_id": inst.ID, "mapping": map[string]string{"ubuntu-latest": "zoomies-linux-x64"}}})
	resp.mustStatus(t, http.StatusUnprocessableEntity, "no repos")
	if msg := resp.errorMessage(t); !strings.Contains(msg, "name the repositories") {
		t.Errorf("message = %q, want it to insist on an explicit list", msg)
	}
}

func TestMigrationRefusesAnEmptyMapping(t *testing.T) {
	h, inst, cookie := migrationHarness(t)

	resp := h.do(request{method: http.MethodPost, path: "/api/v1/migrations/pull-requests", cookie: cookie,
		body: map[string]any{"installation_id": inst.ID, "repos": []string{"acme/widgets"}, "mapping": map[string]string{}}})
	resp.mustStatus(t, http.StatusUnprocessableEntity, "no mapping")
	if msg := resp.errorMessage(t); !strings.Contains(msg, "nothing would change") {
		t.Errorf("message = %q", msg)
	}
}

// A repository the operator named but the App cannot see is a mistake worth
// reporting, not a row that quietly goes missing from the results.
func TestMigrationNamesARepositoryItCannotSee(t *testing.T) {
	h, inst, cookie := migrationHarness(t)

	resp := h.do(request{method: http.MethodPost, path: "/api/v1/migrations/pull-requests", cookie: cookie,
		body: map[string]any{
			"installation_id": inst.ID,
			"repos":           []string{"acme/ghost"},
			"mapping":         map[string]string{"ubuntu-latest": "zoomies-linux-x64"},
		}})
	resp.mustStatus(t, http.StatusNotFound, "unknown repo")
	if msg := resp.errorMessage(t); !strings.Contains(msg, "acme/ghost") {
		t.Errorf("message = %q, want it to name the repository", msg)
	}
}

// A viewer may not spend the installation's GitHub quota, and certainly may
// not open pull requests in the organisation's repositories.
func TestMigrationIsClosedToViewers(t *testing.T) {
	h, inst, _ := migrationHarness(t)
	_, viewer := h.user("watcher", store.RoleViewer)

	for _, path := range []string{"/api/v1/migrations/plan", "/api/v1/migrations/pull-requests"} {
		resp := h.do(request{method: http.MethodPost, path: path, cookie: viewer,
			body: map[string]any{"installation_id": inst.ID}})
		resp.mustStatus(t, http.StatusForbidden, path)
	}
}

// ---------------------------------------------------------------------------

func repoPlan(t *testing.T, plan migrationPlanResponse, repo string) migrate.RepoPlan {
	t.Helper()
	for _, r := range plan.Repositories {
		if r.Repo == repo {
			return r
		}
	}
	t.Fatalf("%s is not in the plan", repo)
	return migrate.RepoPlan{}
}

func workflowPlan(t *testing.T, repo migrate.RepoPlan, path string) migrate.WorkflowPlan {
	t.Helper()
	for _, w := range repo.Workflows {
		if w.Path == path {
			return w
		}
	}
	t.Fatalf("%s is not in the plan for %s", path, repo.Repo)
	return migrate.WorkflowPlan{}
}

func result(t *testing.T, out migrationApplyResponse, repo string) migrationResult {
	t.Helper()
	for _, r := range out.Results {
		if r.Repo == repo {
			return r
		}
	}
	t.Fatalf("%s is not in the results", repo)
	return migrationResult{}
}
