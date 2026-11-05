package controller

import (
	"context"
	"time"

	"github.com/eyupio/zoomies/internal/github"
)

const (
	// eliWatchDiscoveryInterval is how often an enabled repository is checked
	// for pull requests opened since the last pass. Webhook wakeups are a later
	// slice; this bound keeps a missed delivery from going unnoticed for ever.
	eliWatchDiscoveryInterval = 5 * time.Minute
	// maxEliWatchDiscoveryRepositories bounds one pass. Each repository can
	// still return up to RepairPullDiscoveryLimit open pull requests.
	maxEliWatchDiscoveryRepositories = 20
)

func (c *Controller) repairWatchDiscoveryLoop(ctx context.Context) {
	ticker := time.NewTicker(eliWatchDiscoveryInterval)
	defer ticker.Stop()
	for {
		if ctx.Err() != nil {
			return
		}
		c.discoverEliRepairWatches(ctx)
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

// discoverEliRepairWatches finds pull requests opened after a repository was
// admitted. A repository counts as watched while at least one of its watches is
// enabled; the newest enabled watch supplies the owner and capabilities. A
// paused or stopped pull request is left alone by create-only admission.
func (c *Controller) discoverEliRepairWatches(ctx context.Context) {
	if ctx.Err() != nil || !c.mayAct() || c.transferDraining.Load() {
		return
	}
	subscriptions, err := c.st.DueEliRepairWatchRepositories(ctx, maxEliWatchDiscoveryRepositories)
	if err != nil {
		return
	}
	for _, subscription := range subscriptions {
		if ctx.Err() != nil || !c.mayAct() || c.transferDraining.Load() {
			return
		}
		func() {
			defer func() {
				_ = c.st.MarkEliRepairWatchRepositoryDiscovered(ctx, subscription.ID, c.Now())
			}()
			policy, err := c.eliWatchPolicy(ctx, subscription.Repo, subscription.InstallationID)
			if err != nil {
				return
			}
			discoveryCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
			raw, err := c.ClientFor(discoveryCtx, subscription.InstallationID)
			if err != nil {
				cancel()
				return
			}
			discovery, ok := raw.(github.RepairDiscoveryClient)
			if !ok {
				cancel()
				return
			}
			pulls, capped, err := discovery.RepairOpenPulls(discoveryCtx, subscription.Repo, github.RepairPullDiscoveryLimit)
			cancel()
			if err != nil {
				return
			}
			if _, err := c.admitOpenPulls(ctx, subscription.Repo, subscription.InstallationID, subscription.OwnerID, policy, subscription.Checks, subscription.Conflicts, pulls, capped); err != nil {
				c.log.Warn("could not admit new watches for an Eli repair repository", "repo", subscription.Repo, "error", err)
			}
		}()
	}
}
