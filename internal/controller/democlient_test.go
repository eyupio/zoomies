package controller

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/eyupio/zoomies/internal/github"
	"github.com/eyupio/zoomies/internal/migrate"
	"github.com/eyupio/zoomies/internal/store"
)

func demoFixtureClient() github.Client {
	return newDemoClient(&store.Installation{Target: "acme", TargetType: store.TargetOrg})
}

// Every call that would have to reach GitHub to be true refuses, and says so
// in a sentence an operator can act on. A demo that answered these would leave
// runners stuck in provisioning with nothing to explain why.
func TestEveryCallThatWouldNeedARealAppRefusesAndSaysWhy(t *testing.T) {
	ctx := context.Background()
	c := demoFixtureClient()

	cases := []struct {
		what string
		call func() error
	}{
		{"a just-in-time configuration", func() error {
			_, err := c.CreateJITConfig(ctx, github.JITRequest{Name: "demo-runner"})
			return err
		}},
		{"a registration token", func() error {
			_, err := c.CreateRegistrationToken(ctx)
			return err
		}},
		{"a removal token", func() error {
			_, err := c.CreateRemoveToken(ctx)
			return err
		}},
		{"a pull request", func() error {
			_, err := c.OpenPullRequest(ctx, github.PullRequestRequest{Repo: "acme/api"})
			return err
		}},
		{"one job's detail", func() error {
			_, err := c.GetWorkflowJob(ctx, "acme/api", 1)
			return err
		}},
		{"cancelling a run", func() error { return c.CancelWorkflowRun(ctx, "acme/api", 1, false) }},
	}
	for _, tc := range cases {
		t.Run(tc.what, func(t *testing.T) {
			err := tc.call()
			if err == nil {
				t.Fatalf("asking the demo fixture for %s succeeded", tc.what)
			}
			if !errors.Is(err, ErrDemoFixture) {
				t.Errorf("error = %v, want it to match ErrDemoFixture so a caller can tell a fixture from an outage", err)
			}
		})
	}
}

// The reads answer plausibly instead of erroring, because a demo whose
// Installations page 500s reads as broken software rather than as a fleet.
func TestTheReadsADemoPageMakesAllAnswer(t *testing.T) {
	ctx := context.Background()
	c := demoFixtureClient()

	app, err := c.Probe(ctx)
	if err != nil {
		t.Fatalf("Probe: %v", err)
	}
	if app.Owner != "acme" || app.Permissions["organization_self_hosted_runners"] != "write" {
		t.Errorf("app = %+v, want the fixture's own organisation with the permission a fleet needs", app)
	}
	// "selected" is one of the two setup mistakes the verify dialog names, and
	// the fixture is a fleet with nothing wrong.
	if app.RepositorySelection != "all" {
		t.Errorf("repository selection = %q, want a fixture that reads as correctly installed", app.RepositorySelection)
	}

	limit, err := c.RateLimit(ctx)
	if err != nil {
		t.Fatalf("RateLimit: %v", err)
	}
	if limit.Remaining <= 0 || limit.Remaining > limit.Limit || limit.ResetAt.IsZero() {
		t.Errorf("rate limit = %+v, want quota left and a reset in the future", limit)
	}

	groups, err := c.ListRunnerGroups(ctx)
	if err != nil {
		t.Fatalf("ListRunnerGroups: %v", err)
	}
	if len(groups) == 0 {
		t.Error("the fixture offers no runner group, so a pool cannot name one")
	}

	// The two lists are deliberately empty: the demo's runners and queued jobs
	// are rows in the database already, and inventing more on every poll would
	// have the fixture drift under the screenshots taken of it.
	if runners, err := c.ListRunners(ctx); err != nil || len(runners) != 0 {
		t.Errorf("ListRunners = %v, %v; want nothing for the reaper to reap", runners, err)
	}
	if jobs, err := c.ListQueuedJobs(ctx); err != nil || len(jobs) != 0 {
		t.Errorf("ListQueuedJobs = %v, %v; want the fixture's own rows left alone", jobs, err)
	}

	// Removing a registration that was never there is the end state the
	// caller wanted, so it is not an error.
	if err := c.DeleteRunner(ctx, 42); err != nil {
		t.Errorf("DeleteRunner: %v, want deleting a registration nobody made to succeed", err)
	}

	target, kind := c.Target()
	if target != "acme" || kind != store.TargetOrg {
		t.Errorf("Target() = %q, %q; want the installation's own organisation", target, kind)
	}
	if c.WebURL() != "https://github.com/acme" {
		t.Errorf("WebURL() = %q, want a link to the organisation", c.WebURL())
	}
}

// The migration wizard's demo needs one of each kind of repository, because
// each is a different thing for it to say: something to move, nothing to move,
// already moved, and one it must refuse to open a pull request against.
func TestTheFixtureOffersEveryKindOfRepositoryTheWizardHasToExplain(t *testing.T) {
	ctx := context.Background()
	c := demoFixtureClient()

	repos, err := c.ListRepositories(ctx, 0)
	if err != nil {
		t.Fatalf("ListRepositories: %v", err)
	}
	byName := map[string]github.Repository{}
	for _, r := range repos {
		byName[r.FullName] = r
	}
	for _, want := range slices.Concat(demoRepos, demoQuietRepos, demoMigratedRepos, demoArchivedRepos) {
		r, ok := byName[want]
		if !ok {
			t.Fatalf("%s is a fixture repository but is not listed", want)
		}
		if r.DefaultBranch == "" || r.HTMLURL == "" {
			t.Errorf("%s = %+v, want a branch to open against and a link to follow", want, r)
		}
	}
	for _, archived := range demoArchivedRepos {
		if !byName[archived].Archived {
			t.Errorf("%s is the fixture's archived repository and must say so, or the wizard offers to migrate it", archived)
		}
	}

	// A repository with no workflows is its own answer, not an empty list:
	// the wizard's "hide repositories with nothing to move" filter is what it
	// exists for.
	for _, quiet := range demoQuietRepos {
		if _, err := c.ListWorkflows(ctx, quiet); !errors.Is(err, github.ErrNoWorkflows) {
			t.Errorf("ListWorkflows(%q) = %v, want ErrNoWorkflows", quiet, err)
		}
	}

	// One repository carries a second workflow, so the wizard's per-file
	// choice has something to choose between.
	files, err := c.ListWorkflows(ctx, demoRepos[0])
	if err != nil {
		t.Fatalf("ListWorkflows(%q): %v", demoRepos[0], err)
	}
	if len(files) < 2 {
		t.Errorf("%s has %d workflow files, want more than one to choose between", demoRepos[0], len(files))
	}
	seen := map[string]bool{}
	for _, f := range files {
		if f.SHA == "" {
			t.Errorf("%s has no SHA, so a migration could not be written back safely", f.Path)
		}
		if seen[f.Path] {
			t.Errorf("%s is listed twice", f.Path)
		}
		seen[f.Path] = true
	}
}

// The fixture's two kinds of workflow have to read differently to the code
// that plans a migration: one has a job to move, the other is already done.
func TestTheFixtureHasAWorkflowToMigrateAndOneAlreadyMigrated(t *testing.T) {
	ctx := context.Background()
	c := demoFixtureClient()

	files, err := c.ListWorkflows(ctx, demoRepos[0])
	if err != nil {
		t.Fatalf("ListWorkflows: %v", err)
	}
	if !strings.Contains(files[0].Content, "ubuntu-latest") {
		t.Errorf("%s has nothing on GitHub's runners, so the wizard has nothing to offer", files[0].Path)
	}

	for _, done := range demoMigratedRepos {
		files, err := c.ListWorkflows(ctx, done)
		if err != nil {
			t.Fatalf("ListWorkflows(%q): %v", done, err)
		}
		for _, f := range files {
			if strings.Contains(f.Content, "runs-on: ubuntu-latest") {
				t.Errorf("%s still runs a job on GitHub's runners, so it does not read as migrated", done)
			}
		}
	}
}

// The migration planner is the reader these fixtures are written for, so the
// one that is meant to have work finds work, and one job it must leave alone.
func TestThePlannerSeesBothHalvesOfTheFixtureWorkflow(t *testing.T) {
	ctx := context.Background()
	c := demoFixtureClient()

	files, err := c.ListWorkflows(ctx, demoRepos[0])
	if err != nil {
		t.Fatalf("ListWorkflows: %v", err)
	}
	workflows := make([]migrate.Workflow, 0, len(files))
	for _, f := range files {
		workflows = append(workflows, migrate.Workflow{Path: f.Path, SHA: f.SHA, Content: f.Content})
	}

	plan := migrate.PlanRepo(demoRepos[0], "main", workflows,
		migrate.Mapping{Labels: map[string]string{"ubuntu-latest": "zoomies-demo-linux-x64"}})
	if !plan.Changed() {
		t.Fatal("the fixture's repository gives the wizard nothing to migrate")
	}

	var rewrites, skips int
	for _, w := range plan.Workflows {
		rewrites += len(w.Rewrites)
		skips += len(w.Skips)
	}
	if rewrites == 0 || skips == 0 {
		t.Errorf("plan has %d rewrites and %d skips; the fixture exists to show both a job that moves and one the wizard must refuse to touch",
			rewrites, skips)
	}

	// A repository somebody has already moved reads as "nothing to do", which
	// is the other thing the wizard has to be able to say.
	for _, done := range demoMigratedRepos {
		files, err := c.ListWorkflows(ctx, done)
		if err != nil {
			t.Fatalf("ListWorkflows(%q): %v", done, err)
		}
		migrated := make([]migrate.Workflow, 0, len(files))
		for _, f := range files {
			migrated = append(migrated, migrate.Workflow{Path: f.Path, SHA: f.SHA, Content: f.Content})
		}
		if migrate.PlanRepo(done, "main", migrated,
			migrate.Mapping{Labels: map[string]string{"ubuntu-latest": "zoomies-demo-linux-x64"}}).Changed() {
			t.Errorf("%s is the fixture's already-migrated repository and still has something to change", done)
		}
	}
}
