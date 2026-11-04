package controller

import (
	"context"
	"sort"
	"strings"
	"time"

	"github.com/eyupio/zoomies/internal/github"
	"github.com/eyupio/zoomies/internal/store"
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
	watches, err := c.st.EliRepairWatches(ctx)
	if err != nil {
		return
	}
	templates := map[string]*store.EliRepairWatch{}
	for _, watch := range watches {
		if !watch.Enabled || !watch.Checks {
			continue
		}
		key := watch.InstallationID + "\x00" + strings.ToLower(watch.Repo)
		if current := templates[key]; current == nil || watch.CreatedAt.After(current.CreatedAt) {
			templates[key] = watch
		}
	}
	keys := make([]string, 0, len(templates))
	for key := range templates {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for i, key := range keys {
		if i >= maxEliWatchDiscoveryRepositories || ctx.Err() != nil || !c.mayAct() || c.transferDraining.Load() {
			return
		}
		template := templates[key]
		policy, err := c.eliWatchPolicy(ctx, template.Repo, template.InstallationID)
		if err != nil {
			continue
		}
		discoveryCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
		raw, err := c.ClientFor(discoveryCtx, template.InstallationID)
		if err != nil {
			cancel()
			continue
		}
		discovery, ok := raw.(github.RepairDiscoveryClient)
		if !ok {
			cancel()
			continue
		}
		pulls, capped, err := discovery.RepairOpenPulls(discoveryCtx, template.Repo, github.RepairPullDiscoveryLimit)
		cancel()
		if err != nil {
			continue
		}
		if _, err := c.admitOpenPulls(ctx, template.Repo, template.InstallationID, template.OwnerID, policy, template.Checks, template.Conflicts, pulls, capped); err != nil {
			c.log.Warn("could not admit new watches for an Eli repair repository", "repo", template.Repo, "error", err)
		}
	}
}
