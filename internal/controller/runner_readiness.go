package controller

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/eyupio/zoomies/internal/github"
	"github.com/eyupio/zoomies/internal/store"
)

// Share one registration listing per installation in this report batch.
// Failed checks leave the runner registering; the next report retries.
func (c *Controller) runnerOnline(ctx context.Context, r *store.Runner, cache map[string][]github.Runner) bool {
	pool, err := c.st.GetPool(ctx, r.PoolID)
	if err != nil || c.githubHeld(pool.InstallationID, c.Now()) {
		return false
	}
	remote, checked := cache[pool.InstallationID]
	if !checked {
		cache[pool.InstallationID] = nil
		inst, err := c.st.GetInstallation(ctx, pool.InstallationID)
		if err != nil {
			return false
		}
		client, err := c.clients.get(ctx, inst)
		if err != nil {
			return false
		}
		checkCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()
		remote, err = client.ListRunners(checkCtx)
		c.observeGitHub(inst.ID, err)
		if err != nil {
			if errors.Is(err, github.ErrRateLimited) {
				c.holdRateLimited(inst.ID, err, c.Now(), "confirming runner readiness")
			}
			c.log.Warn("could not confirm runner readiness with GitHub", "runner", r.ID, "error", err)
			return false
		}
		cache[pool.InstallationID] = remote
	}
	for _, gr := range remote {
		if gr.Name == r.Name && (r.GitHubRunnerID == 0 || gr.ID == r.GitHubRunnerID) && strings.EqualFold(gr.Status, "online") {
			if r.GitHubRunnerID == 0 {
				_ = c.st.SetRunnerGitHubID(ctx, r.ID, gr.ID)
			}
			return true
		}
	}
	return false
}
