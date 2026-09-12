package agent

import (
	"context"
	"time"

	"github.com/eyupio/zoomies/internal/backend"
)

// Cache collection has a separate loop so a slow daemon cannot delay terminal
// reports, container removal or heartbeats. Docker protects active build cache.
func (a *Agent) buildCacheLoop(ctx context.Context) error {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()
	for {
		a.pruneBuildCache(ctx)
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
		}
	}
}

func (a *Agent) pruneBuildCache(ctx context.Context) {
	if a.opts.DockerBuildCacheMB <= 0 {
		return
	}
	for _, kind := range a.opts.Backends.Kinds() {
		b, err := a.opts.Backends.Get(kind)
		if err != nil {
			continue
		}
		pruner, ok := b.(backend.BuildCachePruner)
		if !ok {
			continue
		}
		cleanupCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
		freed, err := pruner.PruneBuildCache(cleanupCtx, int64(a.opts.DockerBuildCacheMB)<<20)
		cancel()
		if err != nil {
			a.log.Warn("could not clean unused Docker build cache; will retry", "backend", kind, "error", err)
		} else if freed > 0 {
			a.log.Info("cleaned unused Docker build cache", "backend", kind, "reclaimed_bytes", freed,
				"cache_target_mb", a.opts.DockerBuildCacheMB)
		}
	}
}
