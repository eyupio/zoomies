package controller

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"

	"github.com/eyupio/zoomies/internal/github"
	"github.com/eyupio/zoomies/internal/migrate"
	"github.com/eyupio/zoomies/internal/store"
)

// The migration service: what moving a repository's workflows onto this fleet
// would change, and then doing it.
//
// It is two halves of one thing, deliberately shaped like /pools/validate and
// /pools: a plan is computed and shown, and only a second, explicit call
// writes anything. Nothing about a migration should ever be a surprise -- it
// opens pull requests in other people's repositories -- so planning creates
// nothing at all, and applying takes the plan's own inputs back rather than a
// token that could stand for a plan the operator never saw.
//
// Neither half stores anything. A migration is a thing an operator does once
// per repository; a table of half-finished migrations would be a schema to
// maintain, a thing to garbage-collect, and another place for a stale plan to
// hide.
//
// This lives in the controller rather than the API because it is fleet logic
// -- which pools a job could go to, what a repository's workflows say, what
// GitHub is asked to do about it -- and the API has no opinions about the
// fleet. The handlers decode a request, call here, and render the answer.

// Both halves fan out into GitHub's API on a quota the scheduler shares, so
// each bounds what one request will do.
const (
	// MaxPlanRepos is the most repositories one plan will read workflows from.
	// The wizard scans in pages; an operator with more repositories than this
	// migrates them in batches, which is how anyone would want to review them
	// anyway.
	MaxPlanRepos = 50
	// MaxApplyRepos is the most pull requests one call will open. It is lower
	// than the plan's limit on purpose: fifty pull requests landing at once in
	// somebody's review queue is not a migration, it is an incident.
	MaxApplyRepos = 25
	// planConcurrency is how many repositories are read at once. GitHub's
	// secondary rate limits punish bursts, and a scan is not urgent.
	planConcurrency = 4
)

var (
	// ErrNoMigrationPool means the installation has no enabled pool, so there
	// is nowhere to send a job and nothing to plan.
	ErrNoMigrationPool = errors.New("there is nowhere to migrate to: this installation has no enabled pool")
	// ErrNothingMapped means the apply request maps no hosted label to a pool
	// and points no single job at one either, so no workflow would change.
	ErrNothingMapped = errors.New("nothing would change: no hosted label is mapped to a pool, and no job is pointed at one by name")
	// ErrNotAWorkflow means the apply request named a file GitHub would never
	// run, which would be a pull request that quietly changed nothing.
	ErrNotAWorkflow = errors.New("only files directly under .github/workflows can be migrated")
	// ErrBadOverride means an override named something other than one job in
	// one workflow file in one repository, so there is no single place for it
	// to apply to.
	ErrBadOverride = errors.New("an override names one job in one workflow file in one repository")
)

// ---------------------------------------------------------------------------
// Requests
// ---------------------------------------------------------------------------

// MigrationPlanRequest asks what a migration would change.
type MigrationPlanRequest struct {
	InstallationID string `json:"installation_id"`
	// Repos limits the plan to these repositories. Empty means every
	// repository the installation can see, up to the limit.
	Repos []string `json:"repos"`
	// Mapping is hosted label -> the runs-on value that replaces it. Empty asks
	// the server to propose one from the pools that exist.
	Mapping map[string]string `json:"mapping"`
	// Overrides are the exceptions to Mapping, each naming one job in one file
	// in one repository. Empty is the common case: most fleets want one answer
	// per hosted-runner label everywhere.
	Overrides []migrate.Override `json:"overrides"`
	// Cursor asks for the page of repositories after this one. It is the full
	// name of the last repository the previous page returned, because the
	// listing is sorted by name: a name is stable when the App gains or loses
	// a repository between two pages, and an offset is not.
	Cursor string `json:"cursor"`
}

// MigrationApplyRequest opens the pull requests a plan described.
type MigrationApplyRequest struct {
	InstallationID string            `json:"installation_id"`
	Repos          []string          `json:"repos"`
	Mapping        map[string]string `json:"mapping"`
	// Overrides are the exceptions to Mapping. They sit alongside Workflows
	// rather than inside it: Workflows says which files move at all, and an
	// override says where one job in one of them lands.
	Overrides []migrate.Override `json:"overrides"`
	// Workflows narrows a repository to the workflow files named here, keyed by
	// repository. A repository absent from the map gets every file the mapping
	// would change, which is what a client that does not know about the field
	// asks for. It exists because a repository with a dozen workflow files is
	// rarely one where all dozen should move at once.
	Workflows map[string][]string `json:"workflows"`
	// Title, Body and CommitMessage override the defaults. They are here
	// because the pull request lands in somebody else's repository, and an
	// organisation with a pull-request template or a commit convention should
	// not have to accept ours.
	Title         string `json:"title"`
	Body          string `json:"body"`
	CommitMessage string `json:"commit_message"`
}

// ---------------------------------------------------------------------------
// Results
// ---------------------------------------------------------------------------

// MigrationPoolOption is one pool the wizard can map a hosted label to.
type MigrationPoolOption struct {
	ID     string   `json:"id"`
	Name   string   `json:"name"`
	Labels []string `json:"labels"`
	// RunsOn is what a workflow writes to reach this pool: the value the
	// mapping should hold.
	RunsOn  string `json:"runs_on"`
	Enabled bool   `json:"enabled"`
}

// MigrationPlan is the whole review step.
type MigrationPlan struct {
	InstallationID string `json:"installation_id"`
	Target         string `json:"target"`
	// Repositories is every repository looked at, whether or not it changed.
	// A repository with nothing to do is information: it means its workflows
	// are already somewhere deliberate.
	Repositories []migrate.RepoPlan `json:"repositories"`
	// HostedLabels is every hosted-runner label found across them -- GitHub's
	// own and the vendors' -- which is what the mapping step lists.
	HostedLabels []string `json:"hosted_labels"`
	// Mapping is what was applied -- the request's, or the proposal the server
	// made from the pools that exist.
	Mapping map[string]string `json:"mapping"`
	// Overrides is what was applied on top of it, echoed back so that a plan
	// describes itself: every `to` under Repositories came either from Mapping
	// or from one of these.
	Overrides []migrate.Override `json:"overrides"`
	// Unmapped are the hosted labels no pool was proposed for. They are the
	// operator's decision, and the reason a plan can be empty.
	Unmapped []string `json:"unmapped"`
	// Pools is what the mapping step chooses between.
	Pools  []MigrationPoolOption `json:"pools"`
	Counts migrate.Counts        `json:"counts"`
	// Truncated says there are more repositories after this page. It is kept
	// alongside NextCursor because it is what a client reads to know the
	// listing is incomplete, whether or not it can page.
	Truncated bool `json:"truncated"`
	// NextCursor is what to send as Cursor to read the next page. Empty means
	// this was the last one.
	NextCursor string `json:"next_cursor,omitempty"`
	// TotalRepos is how many repositories the installation can see, so a wizard
	// showing a page can say what fraction of the whole it is.
	TotalRepos int `json:"total_repos"`
	// MissingPermissions is what the App still needs before the apply step can
	// work. It is reported here, in the step before, because discovering it
	// halfway through opening pull requests leaves half of them open.
	MissingPermissions []string `json:"missing_permissions"`
	// PermissionHint is the sentence that fixes MissingPermissions.
	PermissionHint string `json:"permission_hint,omitempty"`
	// SettingsURL is where to go and fix it.
	SettingsURL string `json:"settings_url,omitempty"`
}

// MigrationResult is what happened to one repository.
type MigrationResult struct {
	Repo string `json:"repo"`
	// Status is "opened", "skipped" or "failed".
	Status string `json:"status"`
	// PullRequestURL and PullRequestNumber are set when Status is "opened".
	PullRequestURL    string `json:"pull_request_url,omitempty"`
	PullRequestNumber int    `json:"pull_request_number,omitempty"`
	Branch            string `json:"branch,omitempty"`
	// Workflows is how many files the pull request changed.
	Workflows int `json:"workflows"`
	// Jobs is how many runs-on lines it rewrote.
	Jobs int `json:"jobs"`
	// Reason explains a skip or a failure.
	Reason string `json:"reason,omitempty"`
}

// MigrationOutcome is what applying a migration did.
type MigrationOutcome struct {
	Results []MigrationResult `json:"results"`
	Opened  int               `json:"opened"`
	Skipped int               `json:"skipped"`
	Failed  int               `json:"failed"`
	// Branch is the branch every pull request was opened from, for the audit
	// row; each result carries its own for the wire.
	Branch string `json:"-"`
}

// ---------------------------------------------------------------------------
// Plan
// ---------------------------------------------------------------------------

// PlanMigration works out what a migration would change. It creates nothing.
//
// It answers store.ErrNotFound for an installation that does not exist,
// ErrNoMigrationPool for one with nowhere to migrate to, and GitHub's own
// errors when the App cannot see or read the repositories.
func (c *Controller) PlanMigration(ctx context.Context, req MigrationPlanRequest) (*MigrationPlan, error) {
	inst, client, err := c.migrationClient(ctx, req.InstallationID)
	if err != nil {
		return nil, err
	}
	pools, err := c.migrationPools(ctx, inst.ID)
	if err != nil {
		return nil, fmt.Errorf("listing the pools this installation could migrate to: %w", err)
	}
	if len(pools) == 0 {
		return nil, ErrNoMigrationPool
	}
	overrides, err := normaliseOverrides(req.Overrides)
	if err != nil {
		return nil, err
	}

	repos, next, total, err := migrationRepos(ctx, client, req.Repos, req.Cursor, MaxPlanRepos)
	if err != nil {
		return nil, err
	}

	// Two passes. The first reads every repository and works out which hosted
	// labels are in play; the second applies a mapping to them. They are
	// separate because the mapping the server proposes depends on what the
	// first pass found, and reading each repository twice would double the
	// cost of the most expensive call Zoomies makes.
	sources := readWorkflows(ctx, client, repos)

	// Asking GitHub what this installation may do costs one call, and it is
	// asked before the plans are built rather than after because the answer
	// changes what "no workflows" means. GitHub returns 404 for contents an App
	// may not read, exactly as it does for a repository that has no
	// .github/workflows, so without this an App that was never granted Contents
	// reports every repository in the organisation as having nothing to
	// migrate -- and the operator is told a fact about their repositories
	// instead of about their App.
	var appInfo *github.AppInfo
	if info, err := client.Probe(ctx); err == nil {
		appInfo = info
	}
	blind := appInfo != nil && !appInfo.CanReadContents()

	hosted := hostedLabelsAcross(sources)
	mapping := normaliseMapping(req.Mapping)
	if len(mapping) == 0 {
		mapping = migrate.Suggest(pools, hosted)
	}
	m := migrate.Mapping{Labels: mapping, Overrides: overrides}

	// The labels that mean "this fleet", so a repository somebody already
	// migrated is reported as finished rather than as having nothing in it.
	fleet := migrate.FleetLabels(pools)

	plans := make([]migrate.RepoPlan, 0, len(sources))
	for _, src := range sources {
		failed := src.err
		// An archived repository was never read, and is greyed out for its own
		// reason; every other empty-looking one is only empty as far as an App
		// that cannot see inside it can tell.
		if failed == "" && blind && len(src.workflows) == 0 && !src.repo.Archived {
			failed = "the App may not read this repository's contents, so its workflows were never looked at"
		}
		if failed != "" {
			plans = append(plans, migrate.RepoPlan{
				Repo:          src.repo.FullName,
				DefaultBranch: src.repo.DefaultBranch,
				Error:         failed,
				Archived:      src.repo.Archived,
			})
			continue
		}
		plan := migrate.PlanRepo(src.repo.FullName, src.repo.DefaultBranch, src.workflows, m)
		plan.Archived = src.repo.Archived
		for _, wf := range src.workflows {
			if migrate.UsesAnyLabel(wf.Content, fleet) {
				plan.OnZoomies = true
				break
			}
		}
		plans = append(plans, plan)
	}

	var unmapped []string
	for _, l := range hosted {
		if _, ok := mapping[l]; !ok {
			unmapped = append(unmapped, l)
		}
	}

	out := &MigrationPlan{
		InstallationID:     inst.ID,
		Target:             inst.Target,
		Repositories:       plans,
		HostedLabels:       hosted,
		Mapping:            mapping,
		Overrides:          emptySlice(overrides),
		Unmapped:           emptySlice(unmapped),
		Pools:              poolOptions(pools),
		Counts:             migrate.Count(plans),
		Truncated:          next != "",
		NextCursor:         next,
		TotalRepos:         total,
		MissingPermissions: []string{},
	}
	// What the probe above found, turned into something the operator can act
	// on: "403 halfway through" becomes a sentence in the step before.
	if appInfo != nil {
		if missing := appInfo.MissingForMigration(); len(missing) > 0 {
			out.MissingPermissions = missing
			out.PermissionHint = github.MigrationPermissionHint
			settingsOrg := ""
			if inst.TargetType == store.TargetOrg {
				settingsOrg = inst.Target
			}
			out.SettingsURL = github.SettingsURL(inst.APIBaseURL, appInfo.Slug, settingsOrg)
		}
	}
	return out, nil
}

// ---------------------------------------------------------------------------
// Apply
// ---------------------------------------------------------------------------

// ApplyMigration opens the pull requests a plan described, one repository at a
// time.
//
// It re-reads and re-plans rather than trusting a plan the browser sends back.
// The workflows may have changed since the review step, and the alternative --
// committing content the browser supplied -- would make this a way to write
// arbitrary files into any repository the App can reach. The caller has
// already bounded req.Repos to MaxApplyRepos; an empty mapping is
// ErrNothingMapped.
func (c *Controller) ApplyMigration(ctx context.Context, req MigrationApplyRequest) (*MigrationOutcome, error) {
	mapping := normaliseMapping(req.Mapping)
	overrides, err := normaliseOverrides(req.Overrides)
	if err != nil {
		return nil, err
	}
	// An operator who mapped no label at all but pointed three jobs at a pool
	// by hand has asked for something real, so the question is "would anything
	// move", not "is there a mapping".
	if len(mapping) == 0 && !anyOverrideWrites(overrides) {
		return nil, ErrNothingMapped
	}
	m := migrate.Mapping{Labels: mapping, Overrides: overrides}
	only, err := workflowSelection(req.Workflows)
	if err != nil {
		return nil, err
	}
	_, client, err := c.migrationClient(ctx, req.InstallationID)
	if err != nil {
		return nil, err
	}
	repos, _, _, err := migrationRepos(ctx, client, req.Repos, "", MaxApplyRepos)
	if err != nil {
		return nil, err
	}

	branch := github.BranchName(c.Now())
	title := firstNonEmpty(strings.TrimSpace(req.Title), "Run CI on Zoomies runners")
	commit := firstNonEmpty(strings.TrimSpace(req.CommitMessage), title)

	out := &MigrationOutcome{Results: make([]MigrationResult, 0, len(repos)), Branch: branch}
	// One repository at a time. Opening pull requests is a write against a
	// shared quota, and a burst of them is exactly what GitHub's secondary
	// rate limits exist to stop.
	for _, repo := range repos {
		if err := ctx.Err(); err != nil {
			break
		}
		out.Results = append(out.Results, migrateRepo(ctx, client, repo, m, only[strings.ToLower(repo.FullName)], branch, title, req.Body, commit))
	}
	for _, res := range out.Results {
		switch res.Status {
		case "opened":
			out.Opened++
		case "failed":
			out.Failed++
		default:
			out.Skipped++
		}
	}
	return out, nil
}

// migrateRepo plans and opens the pull request for one repository.
//
// only, when not nil, is the set of workflow paths the operator chose in this
// repository; every other file is left where it is.
func migrateRepo(ctx context.Context, client github.Client, repo github.Repository,
	m migrate.Mapping, only map[string]bool, branch, title, body, commit string) MigrationResult {

	res := MigrationResult{Repo: repo.FullName, Status: "skipped"}
	if repo.Archived {
		res.Reason = "the repository is archived, so it accepts no pull requests"
		return res
	}

	workflows, err := client.ListWorkflows(ctx, repo.FullName)
	if err != nil {
		if errors.Is(err, github.ErrNoWorkflows) {
			res.Reason = "the repository has no .github/workflows"
			return res
		}
		res.Status, res.Reason = "failed", err.Error()
		return res
	}

	plan := migrate.PlanRepo(repo.FullName, repo.DefaultBranch, asMigrateWorkflows(workflows), m)
	if only != nil {
		plan = selectWorkflows(plan, only)
	}
	var files []github.FileChange
	for _, wf := range plan.Workflows {
		if !wf.Changed() {
			continue
		}
		files = append(files, github.FileChange{Path: wf.Path, Content: wf.After, SHA: wf.SHA})
		res.Workflows++
		res.Jobs += len(wf.Rewrites)
	}
	if len(files) == 0 {
		res.Reason = "no job in this repository is on a mapped GitHub-hosted label"
		if only != nil {
			res.Reason = "no job in the workflow files you chose here is on a mapped GitHub-hosted label"
		}
		return res
	}

	pr, err := client.OpenPullRequest(ctx, github.PullRequestRequest{
		Repo:          repo.FullName,
		Base:          repo.DefaultBranch,
		Head:          branch,
		Title:         title,
		Body:          firstNonEmpty(body, pullRequestBody(plan)),
		CommitMessage: commit,
		Files:         files,
	})
	if err != nil {
		res.Status, res.Reason = "failed", err.Error()
		return res
	}
	res.Status = "opened"
	res.PullRequestURL, res.PullRequestNumber, res.Branch = pr.HTMLURL, pr.Number, pr.Branch
	return res
}

// pullRequestBody is what somebody reviewing the change reads first.
//
// It says what moved and what did not, because the skips are the part a
// reviewer has to act on: a job left on `${{ matrix.os }}` is still running on
// GitHub's runners after this merges, and nobody should have to work that out
// from the diff.
func pullRequestBody(plan migrate.RepoPlan) string {
	var b strings.Builder
	b.WriteString("Moves this repository's CI onto self-hosted runners managed by [Zoomies](https://zoomies.sh).\n\n")

	changed := 0
	for _, wf := range plan.Workflows {
		if !wf.Changed() {
			continue
		}
		changed++
		fmt.Fprintf(&b, "### `%s`\n\n", wf.Path)
		for _, rw := range wf.Rewrites {
			if rw.Job != "" {
				fmt.Fprintf(&b, "- `%s`: `%s` → `%s`\n", rw.Job, rw.From, rw.To)
			} else {
				fmt.Fprintf(&b, "- line %d: `%s` → `%s`\n", rw.Line, rw.From, rw.To)
			}
		}
		b.WriteString("\n")
	}

	var skips []migrate.Skip
	for _, wf := range plan.Workflows {
		skips = append(skips, wf.Skips...)
	}
	if len(skips) > 0 {
		b.WriteString("### Left alone\n\n")
		for _, sk := range skips {
			where := sk.Job
			if where == "" {
				where = fmt.Sprintf("line %d", sk.Line)
			}
			fmt.Fprintf(&b, "- `%s` (`%s`): %s\n", where, sk.Value, sk.Reason)
		}
		b.WriteString("\nThose jobs still run on GitHub's runners.\n")
	}
	return b.String()
}

// ---------------------------------------------------------------------------
// Shared plumbing
// ---------------------------------------------------------------------------

// migrationClient resolves the installation and its GitHub client.
func (c *Controller) migrationClient(ctx context.Context, id string) (*store.Installation, github.Client, error) {
	inst, err := c.st.GetInstallation(ctx, strings.TrimSpace(id))
	if err != nil {
		return nil, nil, err
	}
	client, err := c.ClientFor(ctx, inst.ID)
	if err != nil {
		return nil, nil, fmt.Errorf("authenticating as the GitHub App: %w", err)
	}
	return inst, client, nil
}

// migrationPools returns the enabled pools belonging to an installation, which
// are the only places a job could actually be sent.
func (c *Controller) migrationPools(ctx context.Context, installationID string) ([]*store.Pool, error) {
	all, err := c.st.ListPools(ctx)
	if err != nil {
		return nil, err
	}
	var out []*store.Pool
	for _, p := range all {
		if p.InstallationID == installationID && p.Enabled {
			out = append(out, p)
		}
	}
	return out, nil
}

func poolOptions(pools []*store.Pool) []MigrationPoolOption {
	out := make([]MigrationPoolOption, 0, len(pools))
	for _, p := range pools {
		out = append(out, MigrationPoolOption{
			ID:      p.ID,
			Name:    p.Name,
			Labels:  emptySlice(p.Labels),
			RunsOn:  store.RunsOn(p.Labels),
			Enabled: p.Enabled,
		})
	}
	return out
}

// migrationRepos resolves the repositories a request names, or one page of
// what the installation can see when it names none.
//
// It returns the page, the cursor for the page after it -- empty when there is
// none -- and how many repositories the installation can see in total, so the
// wizard can say which slice of an organisation is on screen and go and get
// the next one. Paging rather than one big scan is deliberate: reading every
// workflow file in a thousand repositories is the most expensive thing Zoomies
// asks GitHub for, and it spends the quota the scheduler shares.
func migrationRepos(ctx context.Context, client github.Client, named []string, cursor string, limit int) ([]github.Repository, string, int, error) {
	all, err := client.ListRepositories(ctx, 0)
	if err != nil {
		return nil, "", 0, err
	}
	byName := make(map[string]github.Repository, len(all))
	for _, r := range all {
		byName[strings.ToLower(r.FullName)] = r
	}

	if len(named) > 0 {
		out := make([]github.Repository, 0, len(named))
		for _, want := range named {
			want = strings.TrimSpace(want)
			if want == "" {
				continue
			}
			repo, ok := byName[strings.ToLower(want)]
			if !ok {
				// A repository the App cannot see is named, not silently
				// dropped: the operator picked it, and it disappearing from the
				// results with no explanation is worse than a failure.
				return nil, "", 0, fmt.Errorf("%w: this installation cannot see %s; check the App is installed on it", github.ErrNotFound, want)
			}
			out = append(out, repo)
		}
		return out, "", len(out), nil
	}

	sort.Slice(all, func(i, j int) bool { return all[i].FullName < all[j].FullName })
	total := len(all)

	// The cursor is a name rather than an offset, so a repository added or
	// removed between two pages shifts nothing: the next page is whatever
	// sorts after the last name the operator has already seen.
	if after := strings.ToLower(strings.TrimSpace(cursor)); after != "" {
		i := sort.Search(len(all), func(i int) bool { return strings.ToLower(all[i].FullName) > after })
		all = all[i:]
	}
	if len(all) > limit {
		return all[:limit], all[limit-1].FullName, total, nil
	}
	return all, "", total, nil
}

// workflowSelection turns the apply request's per-repository workflow paths
// into a lookup, refusing anything that is not a file GitHub would run.
//
// A path that is not a workflow is a client bug, and accepting it silently
// would mean a repository whose pull request quietly changes nothing.
func workflowSelection(in map[string][]string) (map[string]map[string]bool, error) {
	if len(in) == 0 {
		return nil, nil
	}
	out := make(map[string]map[string]bool, len(in))
	for repo, paths := range in {
		repo = strings.ToLower(strings.TrimSpace(repo))
		if repo == "" {
			continue
		}
		set := make(map[string]bool, len(paths))
		for _, p := range paths {
			p = strings.TrimSpace(p)
			if p == "" {
				continue
			}
			if !migrate.IsWorkflowPath(p) {
				return nil, fmt.Errorf("%w: %q is not one", ErrNotAWorkflow, p)
			}
			set[p] = true
		}
		if len(set) == 0 {
			return nil, fmt.Errorf("%w: %s named none, so leave it out to migrate every file the mapping covers", ErrNotAWorkflow, repo)
		}
		out[repo] = set
	}
	if len(out) == 0 {
		return nil, nil
	}
	return out, nil
}

// selectWorkflows keeps only the files the operator chose, so the pull request
// body still reports the jobs left behind in the files it did touch.
func selectWorkflows(plan migrate.RepoPlan, only map[string]bool) migrate.RepoPlan {
	out := plan
	out.Workflows = make([]migrate.WorkflowPlan, 0, len(plan.Workflows))
	for _, wf := range plan.Workflows {
		if only[wf.Path] {
			out.Workflows = append(out.Workflows, wf)
		}
	}
	return out
}

// workflowSource is one repository's workflows, or why they could not be read.
type workflowSource struct {
	repo      github.Repository
	workflows []migrate.Workflow
	err       string
}

// readWorkflows reads every repository's workflows, a few at a time.
//
// One repository failing is recorded against that repository rather than
// failing the scan: in an organisation of any size there is always one the App
// was removed from, and one with a .github/workflows that is a file rather
// than a directory.
//
// An archived repository is not read at all. Nothing could be opened against
// it whatever its workflows say, and a scan of an organisation that has been
// tidying up for years would otherwise spend most of its GitHub quota on
// repositories the wizard is about to grey out.
func readWorkflows(ctx context.Context, client github.Client, repos []github.Repository) []workflowSource {
	out := make([]workflowSource, len(repos))
	sem := make(chan struct{}, planConcurrency)
	var wg sync.WaitGroup

	for i, repo := range repos {
		wg.Add(1)
		go func() {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			out[i] = workflowSource{repo: repo}
			if repo.Archived {
				return
			}
			if ctx.Err() != nil {
				out[i].err = "the request was cancelled before this repository was read"
				return
			}
			files, err := client.ListWorkflows(ctx, repo.FullName)
			switch {
			case errors.Is(err, github.ErrNoWorkflows):
				// Not an error: most repositories have no workflows.
			case err != nil:
				out[i].err = err.Error()
			default:
				out[i].workflows = asMigrateWorkflows(files)
			}
		}()
	}
	wg.Wait()
	return out
}

func asMigrateWorkflows(files []github.WorkflowFile) []migrate.Workflow {
	out := make([]migrate.Workflow, 0, len(files))
	for _, f := range files {
		if !migrate.IsWorkflowPath(f.Path) {
			continue
		}
		out = append(out, migrate.Workflow{Path: f.Path, SHA: f.SHA, Content: f.Content})
	}
	return out
}

// hostedLabelsAcross collects every GitHub-hosted label the scan found, sorted
// so the mapping step is in the same order every time it is opened.
func hostedLabelsAcross(sources []workflowSource) []string {
	seen := map[string]bool{}
	var out []string
	for _, src := range sources {
		for _, wf := range src.workflows {
			for _, l := range migrate.HostedLabelsIn(wf.Content) {
				if !seen[l] {
					seen[l] = true
					out = append(out, l)
				}
			}
		}
	}
	sort.Strings(out)
	return out
}

// normaliseOverrides trims the overrides a request carries and refuses the ones
// that name nothing.
//
// A malformed override is refused rather than dropped. The operator picked that
// job on the review screen, and a migration that quietly ignored the exception
// they asked for -- sending the job to the consolidated pool instead -- is the
// kind of surprise every other decision in this file is shaped to avoid.
func normaliseOverrides(in []migrate.Override) ([]migrate.Override, error) {
	out := make([]migrate.Override, 0, len(in))
	seen := make(map[string]bool, len(in))
	for i, o := range in {
		o = migrate.Override{
			Repo: strings.TrimSpace(o.Repo),
			Path: strings.TrimSpace(o.Path),
			Job:  strings.TrimSpace(o.Job),
			To:   strings.TrimSpace(o.To),
		}
		switch {
		case o.Repo == "":
			return nil, fmt.Errorf("%w, and override %d names no repository", ErrBadOverride, i+1)
		case o.Job == "":
			// Not every runs-on can be attributed to a job, and one that cannot
			// has no name that survives the file being read again at apply time.
			return nil, fmt.Errorf("%w, and override %d names no job; a runs-on the plan could not attribute to one cannot be overridden", ErrBadOverride, i+1)
		case !migrate.IsWorkflowPath(o.Path):
			return nil, fmt.Errorf("%w, and override %d names %q, which is not a workflow file GitHub runs", ErrBadOverride, i+1, o.Path)
		}
		key := strings.ToLower(o.Repo) + "\x00" + o.Path + "\x00" + o.Job
		if seen[key] {
			// Two answers for one job is not a preference, it is a bug in
			// whatever built the request, and picking one of them silently
			// would hide it.
			return nil, fmt.Errorf("%w, and %s in %s is overridden twice", ErrBadOverride, o.Job, o.Path)
		}
		seen[key] = true
		out = append(out, o)
	}
	return out, nil
}

// anyOverrideWrites reports whether any override would actually move a job. One
// that says "stay where you are" is a decision, but it is not a reason to open
// a pull request.
func anyOverrideWrites(in []migrate.Override) bool {
	for _, o := range in {
		if o.To != "" {
			return true
		}
	}
	return false
}

// normaliseMapping lowercases the keys and drops the entries the browser sends
// for a label the operator chose not to map, which arrive as empty strings.
func normaliseMapping(in map[string]string) map[string]string {
	out := make(map[string]string, len(in))
	for k, v := range in {
		k, v = strings.ToLower(strings.TrimSpace(k)), strings.TrimSpace(v)
		if k == "" || v == "" {
			continue
		}
		out[k] = v
	}
	return out
}
