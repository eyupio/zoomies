package controller

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"time"

	"github.com/eyupio/zoomies/internal/store"
)

// StuckSeedEnvVar names the environment variable that adds the diagnostics
// fixture on top of the demo fleet.
//
// It is separate from ZOOMIES_SEED_DEMO on purpose, and it is the opposite of
// what a demo wants. The demo deliberately keeps its two starting runners young
// (see freshenDemoRunners) so an instance left open never reports a fault in a
// fleet that has no agent to have one. Every diagnostic this project has for a
// fleet in trouble -- the problems drawer, the stuck-runner shapes, the blocked
// pool, the held job -- is therefore unreachable from the demo, which is why
// they went untested until this existed.
//
// Nothing here is for an operator. It exists so a test can open the pages an
// operator opens on their worst day.
const StuckSeedEnvVar = "ZOOMIES_SEED_STUCK"

// The diagnostics fixture's own identifiers. They begin "demo" after the
// prefix, so IsDemoID covers them and every guard that keeps a fixture out of a
// real fleet applies to them unchanged.
const (
	stuckPoolID    = "pool_demostuckblocked"
	stuckPoolName  = "zoomies-demo-stuck-blocked"
	stuckHeldJobID = "job_demostuckheld"
)

// stuckSeedRequested reports whether the diagnostics fixture was asked for.
func stuckSeedRequested() bool {
	v, ok := os.LookupEnv(StuckSeedEnvVar)
	if !ok {
		return false
	}
	b, err := strconv.ParseBool(v)
	return err == nil && b
}

// SeedStuck ages the demo's starting runners into the two stuck shapes and adds
// the two fixtures the demo has no room for: a pool nothing can place, and a
// job GitHub is holding.
//
// It runs after SeedDemo and depends on it, because a fleet in trouble is only
// legible against one that is working: the point of the problems drawer is that
// it names the three things wrong among the twenty that are right.
func (c *Controller) SeedStuck(ctx context.Context) error {
	now := c.Now()

	// The demo's two starting runners are the two shapes exactly: one with no
	// container yet, one whose container came up and never registered. They are
	// already the right shape and only ever the wrong age, so this back-dates
	// them rather than writing more rows -- a fixture that duplicated them
	// would drift from the demo's the first time either was edited.
	starting, _, err := c.st.ListRunners(ctx, store.RunnerFilter{
		States: []store.RunnerState{store.RunnerProvisioning, store.RunnerRegistering},
	}, store.Page{Limit: 100})
	if err != nil {
		return fmt.Errorf("listing the runners to age: %w", err)
	}
	// Three quarters of the provision timeout: past half, which is when
	// runners.not_progressing starts saying so, and short of the whole, which
	// is when the reconcile loop gives up and fails the runner. Ageing them to
	// the full timeout produced a fixture with one stuck runner rather than
	// two, because the next pass failed the one with no container -- which is
	// the correct behaviour and the wrong fixture.
	stuckSince := now.Add(-c.cfg().Scheduler.ProvisionTimeout * 3 / 4)
	aged := 0
	for _, r := range starting {
		if !IsDemoID(r.ID) {
			continue
		}
		if r.ContainerStartedAt != nil {
			err = c.st.SetRunnerStartup(ctx, r.ID, r.ImagePullDuration, &stuckSince)
		} else {
			err = c.st.SetRunnerCreatedAt(ctx, r.ID, stuckSince)
		}
		if err != nil {
			return fmt.Errorf("ageing runner %s into a stuck one: %w", r.ID, err)
		}
		aged++
	}
	if aged == 0 {
		return fmt.Errorf("no demo runner was starting up, so there is nothing to make stuck; is %s set too?", SeedEnvVar)
	}

	if err := c.seedBlockedPool(ctx); err != nil {
		return err
	}
	if err := c.seedHeldJob(ctx, now); err != nil {
		return err
	}
	c.log.Info("seeded the diagnostics fixture", "env", StuckSeedEnvVar, "stuck_runners", aged)
	return nil
}

// seedBlockedPool writes a pool no host can take, so the scheduler's own
// sentence for that -- and the pool warning the drawer renders from it -- has
// something to say.
//
// The selector is what makes it unplaceable, rather than a disabled pool or a
// ceiling of zero: those are choices an operator made, and the page says so
// differently. A selector nothing answers is the misconfiguration, and it is
// the one the reason string exists to explain.
func (c *Controller) seedBlockedPool(ctx context.Context) error {
	if _, err := c.st.GetPool(ctx, stuckPoolID); err == nil {
		return nil
	}
	p := &store.Pool{
		ID:             stuckPoolID,
		Name:           stuckPoolName,
		InstallationID: demoInstallationID,
		Labels:         store.StringSlice(store.BrandLabels([]string{"linux", "x64", stuckPoolName})),
		Backend:        store.BackendDocker,
		MinRunners:     1,
		MaxRunners:     2,
		IdleTimeout:    store.Duration(5 * time.Minute),
		Ephemeral:      true,
		DockerMode:     store.DockerNone,
		// No host answers this, and none is meant to.
		HostSelector: store.StringMap{"zone": "nowhere"},
		Enabled:      true,
	}
	if err := c.st.CreatePool(ctx, p); err != nil {
		return fmt.Errorf("seeding the blocked pool: %w", err)
	}
	return nil
}

// seedHeldJob writes a job GitHub is holding for a deployment review.
//
// It is the one job state the demo has never carried, and the one that reads
// as a fleet fault when it is not: every panel that explains a wait keys on
// `queued`, so a held job used to sit in the drawer with nothing said about it.
func (c *Controller) seedHeldJob(ctx context.Context, now time.Time) error {
	held := &store.Job{
		ID:             stuckHeldJobID,
		GitHubJobID:    80099,
		GitHubRunID:    40099,
		Repo:           demoRepos[0],
		Workflow:       "Deploy",
		JobName:        "deploy-production",
		Labels:         store.StringSlice(store.BrandLabels([]string{"linux", "x64", "zoomies-demo-linux-x64"})),
		InstallationID: demoInstallationID,
		State:          store.JobWaiting,
		QueuedAt:       now.Add(-18 * time.Minute),
		HTMLURL:        fmt.Sprintf("https://github.com/%s/actions/runs/%d", demoRepos[0], 40099),
		HeadBranch:     "main",
		HeadSHA:        fmt.Sprintf("%040x", 0xDEC1DE),
		RunAttempt:     1,
	}
	saved, change, err := c.st.ApplyJob(ctx, held)
	if err != nil {
		return fmt.Errorf("seeding the held job: %w", err)
	}
	return c.seedJobTimeline(ctx, saved, change)
}
