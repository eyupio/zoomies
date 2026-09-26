package controller

import (
	"context"
	"errors"
	"github.com/eyupio/zoomies/internal/github"
	"github.com/eyupio/zoomies/internal/store"
	"sync"
	"time"
)

// Bounded workers keep optional remote lookups out of webhook and heartbeat
// acknowledgements. Durable leases also cover controller restarts and 429s.
func (c *Controller) enrichmentLoop(ctx context.Context) {
	tick := time.NewTicker(time.Second)
	defer tick.Stop()
	for {
		c.enrichOnce(ctx)
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
		}
	}
}
func (c *Controller) enrichOnce(ctx context.Context) {
	if !c.mayAct() {
		return
	}
	items, err := c.st.ClaimEnrichment(ctx, 4)
	if err != nil {
		c.log.Warn("could not claim GitHub enrichment", "error", err)
		return
	}
	var wg sync.WaitGroup
	for _, item := range items {
		wg.Add(1)
		go func(v store.Enrichment) {
			defer wg.Done()
			cctx, cancel := context.WithTimeout(ctx, 5*time.Second)
			defer cancel()
			err := c.enrich(cctx, v)
			if err != nil {
				c.log.Debug("GitHub enrichment will retry", "kind", v.Kind, "target", v.Target, "error", err)
			}
			if e := c.st.FinishEnrichment(ctx, v, err == nil); e != nil {
				c.log.Warn("could not acknowledge enrichment", "error", e)
			}
		}(item)
	}
	wg.Wait()
}
func (c *Controller) enrich(ctx context.Context, v store.Enrichment) error {
	if !c.mayAct() {
		return errors.New("controller authority is paused")
	}
	switch v.Kind {
	case "registration":
		r, err := c.st.GetRunner(ctx, v.Target)
		if errors.Is(err, store.ErrNotFound) {
			return nil
		}
		if err != nil {
			return err
		}
		if r.RegistrationDeletedAt != nil {
			return nil
		}
		c.deleteRegistration(ctx, r, nil)
		r, err = c.st.GetRunner(ctx, v.Target)
		if err != nil {
			return err
		}
		if r.RegistrationDeletedAt == nil {
			return errors.New("registration cleanup not yet confirmed")
		}
		return nil
	case "ready":
		r, err := c.st.GetRunner(ctx, v.Target)
		if errors.Is(err, store.ErrNotFound) {
			return nil
		}
		if err != nil {
			return err
		}
		if r.State != store.RunnerRegistering {
			return nil
		}
		if !c.runnerOnline(ctx, r, make(map[string][]github.Runner)) {
			return errors.New("runner not yet confirmed online")
		}
		return c.applyRunnerState(ctx, r, store.RunnerIdle, "GitHub confirmed the runner online", "")
	case "job":
		j, err := c.st.GetJob(ctx, v.Target)
		if errors.Is(err, store.ErrNotFound) {
			return nil
		}
		if err != nil {
			return err
		}
		if j.InstallationID == "" || j.GitHubRunID == 0 {
			return nil
		}
		cancelled := j.State == store.JobCompleted && j.Conclusion == "cancelled"
		if j.RunNumber != 0 && !cancelled {
			return nil
		}
		if c.githubHeld(j.InstallationID, c.Now()) {
			return errors.New("GitHub rate limit hold")
		}
		client, err := c.ClientFor(ctx, j.InstallationID)
		if err != nil {
			return err
		}
		run, err := client.GetWorkflowRun(ctx, j.Repo, j.GitHubRunID)
		c.observeGitHub(j.InstallationID, err)
		if err != nil {
			if errors.Is(err, github.ErrRateLimited) {
				c.holdRateLimited(j.InstallationID, err, c.Now(), "enriching workflow")
			}
			return err
		}
		if run.RunNumber > 0 {
			if err := c.st.SetRunNumberForRun(ctx, j.Repo, j.GitHubRunID, run.RunNumber); err != nil {
				return err
			}
			j.RunNumber = run.RunNumber
			c.publishJob(ctx, j)
		}
		if cancelled && run.Cancelled() {
			if !c.mayAct() {
				return errors.New("controller authority is paused")
			}
			return c.cancelWorkflowRunLocally(ctx, j.Repo, j.GitHubRunID, true)
		}
	}
	return nil
}
