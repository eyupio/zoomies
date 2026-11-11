package agent

import (
	"context"
	"time"

	"github.com/eyupio/zoomies/internal/backend"
)

// Cache collection has a separate loop so a slow daemon cannot delay terminal
// reports, container removal or heartbeats. Docker protects active build cache,
// and refuses to remove a volume a container uses.
func (a *Agent) buildCacheLoop(ctx context.Context) error {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()
	for {
		a.pruneBuildCache(ctx)
		a.sweepExternals(ctx)
		a.sweepImages(ctx)
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

// sweepExternals removes the externals volumes of runner images a pool has
// moved away from. It runs whatever the builder-cache target: the volumes are
// the agent's own, and each image update would otherwise leave one behind.
func (a *Agent) sweepExternals(ctx context.Context) {
	for _, kind := range a.opts.Backends.Kinds() {
		b, err := a.opts.Backends.Get(kind)
		if err != nil {
			continue
		}
		sweeper, ok := b.(backend.ExternalsSweeper)
		if !ok {
			continue
		}
		cleanupCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
		removed, err := sweeper.SweepExternals(cleanupCtx)
		cancel()
		if err != nil {
			a.log.Warn("could not sweep superseded externals volumes; will retry", "backend", kind, "error", err)
		} else if removed > 0 {
			a.log.Info("removed superseded externals volumes", "backend", kind, "volumes", removed)
		}
	}
}

// sweepImages removes the runner images a pool's tag has moved away from. Like
// the externals sweep it runs whatever the builder-cache target, because the
// images are ones this agent pulled and each image update would otherwise leave
// one behind; the backend says what it will and will not touch.
func (a *Agent) sweepImages(ctx context.Context) {
	for _, kind := range a.opts.Backends.Kinds() {
		b, err := a.opts.Backends.Get(kind)
		if err != nil {
			continue
		}
		sweeper, ok := b.(backend.ImageSweeper)
		if !ok {
			continue
		}
		cleanupCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
		removed, err := sweeper.SweepImages(cleanupCtx)
		cancel()
		if err != nil {
			a.log.Warn("could not sweep superseded runner images; will retry", "backend", kind, "error", err)
		} else if removed > 0 {
			a.log.Info("removed superseded runner images", "backend", kind, "images", removed)
		}
	}
}
