package github

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"

	gh "github.com/google/go-github/v88/github"
)

// A personal account as a target.
//
// GitHub keeps self-hosted runners on an organisation or on a repository and
// nowhere else, so an App installed on a person's account has no account-wide
// place to register one. What "the account's runners" means for a person is
// therefore the runners of every repository the installation covers, and every
// call here works a repository at a time: a runner is registered on the
// repository whose job it is for, found by asking each repository, and deleted
// from the one it is on.

// personalListWorkers bounds how many repositories are asked for their runners
// at once. A sweep of an account is one request for each repository; in turn
// they would take a minute for an account with a few hundred, and all at once
// they are the burst GitHub's secondary limit exists to refuse.
const personalListWorkers = 8

// personalRepo checks the repository a runner is to be registered on and
// returns its name without the owner.
func (c *appClient) personalRepo(full string) (string, error) {
	owner, name, _ := SplitTarget(strings.TrimSpace(full))
	if name == "" {
		return "", fmt.Errorf("%s is a personal account, where GitHub registers a runner on one repository, "+
			"and no repository was named for this one: a runner there is only created for a job that is waiting", c.target)
	}
	if !strings.EqualFold(owner, c.owner) {
		return "", fmt.Errorf("the repository %s does not belong to %s, the account this installation covers", full, c.target)
	}
	return name, nil
}

// errNoAccountToken refuses the tokens config.sh registers and removes with.
// They are minted for an organisation or a repository, and a runner registered
// with one stays on that repository for good, where the other repositories'
// jobs cannot reach it.
func (c *appClient) errNoAccountToken(what string) error {
	return fmt.Errorf("github: %s cannot be created for %s: it is a personal account, where every runner belongs to one repository, "+
		"so its pools must be ephemeral; turn on \"ephemeral\" for the pool, or connect the repository on its own as owner/name",
		what, c.target)
}

// runnerRepoIndex is which repository each runner of a personal account is
// registered on, by GitHub's runner ID.
type runnerRepoIndex struct {
	mu   sync.Mutex
	byID map[int64]string
}

func (c *appClient) rememberRunnerRepo(id int64, repo string) {
	if id == 0 || repo == "" || c.runnerRepos == nil {
		return
	}
	c.runnerRepos.mu.Lock()
	defer c.runnerRepos.mu.Unlock()
	c.runnerRepos.byID[id] = repo
}

func (c *appClient) forgetRunnerRepo(id int64) {
	if c.runnerRepos == nil {
		return
	}
	c.runnerRepos.mu.Lock()
	defer c.runnerRepos.mu.Unlock()
	delete(c.runnerRepos.byID, id)
}

func (c *appClient) rememberedRunnerRepo(id int64) (string, bool) {
	if c.runnerRepos == nil {
		return "", false
	}
	c.runnerRepos.mu.Lock()
	defer c.runnerRepos.mu.Unlock()
	repo, ok := c.runnerRepos.byID[id]
	return repo, ok
}

// runnerRepo finds the repository a runner is registered on. What this client
// registered or has listed is remembered; anything else, a runner from before
// a restart, costs one sweep of the account, which also says when the runner
// is on no repository at all.
func (c *appClient) runnerRepo(ctx context.Context, id int64) (string, bool, error) {
	if repo, ok := c.rememberedRunnerRepo(id); ok {
		return repo, true, nil
	}
	runners, err := c.listPersonalRunners(ctx)
	if err != nil {
		return "", false, err
	}
	for _, r := range runners {
		if r.ID == id {
			return r.Repo, true, nil
		}
	}
	return "", false, nil
}

// installationRepos lists every repository the installation covers, in
// owner/name form. It follows every page: a listing that stopped early would
// make a runner on a repository it did not reach look deregistered.
func (c *appClient) installationRepos(ctx context.Context) ([]string, error) {
	opts := &gh.ListOptions{PerPage: pollPerPage}
	var out []string
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		page, resp, err := c.readClient().Apps.ListRepos(ctx, opts)
		if err != nil {
			return nil, c.fail("list repositories visible to the installation", resp, err)
		}
		for _, r := range page.Repositories {
			if r.GetFullName() != "" {
				out = append(out, r.GetFullName())
			}
		}
		if resp == nil || resp.NextPage == 0 {
			return out, nil
		}
		opts.Page = resp.NextPage
	}
}

// listPersonalRunners lists the runners of every repository the installation
// covers. A repository that answers 404 is skipped, because it was deleted or
// taken out of the installation between the two listings; any other failure
// fails the whole listing, because a partial answer would read as runners
// having gone.
func (c *appClient) listPersonalRunners(ctx context.Context) ([]Runner, error) {
	repos, err := c.installationRepos(ctx)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	var (
		mu       sync.Mutex
		wg       sync.WaitGroup
		out      []Runner
		firstErr error
	)
	work := make(chan string)
	for range min(personalListWorkers, len(repos)) {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for repo := range work {
				found, err := c.listRunnersAt(ctx, "repos/"+repo+"/actions/runners", repo)
				mu.Lock()
				switch {
				case err == nil:
					out = append(out, found...)
				case errors.Is(err, ErrNotFound):
				case firstErr == nil:
					firstErr = err
					cancel()
				}
				mu.Unlock()
			}
		}()
	}
feed:
	for _, repo := range repos {
		select {
		case work <- repo:
		case <-ctx.Done():
			break feed
		}
	}
	close(work)
	wg.Wait()
	if firstErr != nil {
		return nil, firstErr
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	// The repositories answer in whatever order they finish; the callers
	// compare listings, so the order is made the same every time.
	slices.SortFunc(out, func(a, b Runner) int {
		if c := strings.Compare(a.Repo, b.Repo); c != 0 {
			return c
		}
		return int(a.ID - b.ID)
	})
	for _, r := range out {
		c.rememberRunnerRepo(r.ID, r.Repo)
	}
	return out, nil
}

// probePersonalRunners proves the installation can read a repository's
// runners, which is the permission every registration on a personal account
// rests on. One repository is enough to prove it, and an installation that
// covers none has nothing to prove it against yet.
func (c *appClient) probePersonalRunners(ctx context.Context) error {
	page, resp, err := c.readClient().Apps.ListRepos(ctx, &gh.ListOptions{PerPage: 1})
	if err != nil {
		return c.fail("list repositories visible to the installation", resp, err)
	}
	for _, r := range page.Repositories {
		if r.GetFullName() == "" {
			continue
		}
		_, err := c.listRunnersAt(ctx, "repos/"+r.GetFullName()+"/actions/runners", r.GetFullName())
		return err
	}
	return nil
}
