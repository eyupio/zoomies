package agent

import (
	"context"
	"errors"
	"testing"

	"github.com/eyupio/zoomies/internal/backend"
)

type cacheBackend struct {
	*fakeBackend
	calls   int
	keep    int64
	err     error
	bounded bool
}

func (b *cacheBackend) PruneBuildCache(ctx context.Context, keep int64) (int64, error) {
	b.calls++
	b.keep = keep
	_, b.bounded = ctx.Deadline()
	return 10, b.err
}

func TestAgentCacheCleanupIsBoundedOptionalAndRetryable(t *testing.T) {
	a, _, be, _ := newAgent(t, 2)
	pruner := &cacheBackend{fakeBackend: be, err: errors.New("daemon unavailable")}
	registry := backend.NewRegistry(pruner)
	a.opts.Backends = registry
	a.pruneBuildCache(context.Background())
	if pruner.calls != 0 {
		t.Fatal("disabled cleanup ran")
	}
	a.opts.DockerBuildCacheMB = 5120
	a.pruneBuildCache(context.Background())
	pruner.err = nil
	a.pruneBuildCache(context.Background())
	if pruner.calls != 2 || pruner.keep != 5<<30 || !pruner.bounded {
		t.Fatalf("cleanup calls=%d budget=%d bounded=%v", pruner.calls, pruner.keep, pruner.bounded)
	}
}

type sweepBackend struct {
	*fakeBackend
	calls   int
	bounded bool
}

func (b *sweepBackend) SweepExternals(ctx context.Context) (int, error) {
	b.calls++
	_, b.bounded = ctx.Deadline()
	return 1, nil
}

// The externals sweep runs whatever the builder-cache target, which is 0 on a
// shared daemon: the volumes it removes are only ever the agent's own.
func TestTheExternalsSweepRunsWithoutABuilderCacheTarget(t *testing.T) {
	a, _, be, _ := newAgent(t, 2)
	sweeper := &sweepBackend{fakeBackend: be}
	a.opts.Backends = backend.NewRegistry(sweeper)
	a.opts.DockerBuildCacheMB = 0
	a.sweepExternals(context.Background())
	if sweeper.calls != 1 || !sweeper.bounded {
		t.Fatalf("sweep calls=%d bounded=%v", sweeper.calls, sweeper.bounded)
	}
}

type imageSweepBackend struct {
	*fakeBackend
	calls   int
	bounded bool
	err     error
}

func (b *imageSweepBackend) SweepImages(ctx context.Context) (int, error) {
	b.calls++
	_, b.bounded = ctx.Deadline()
	return 2, b.err
}

// The image sweep, like the externals one, runs whatever the builder-cache
// target: the images it removes are only the ones this agent pulled and a tag
// has since moved off, and a failure is retried by the next pass, not fatal.
func TestTheImageSweepRunsWithoutABuilderCacheTargetAndSurvivesAnError(t *testing.T) {
	a, _, be, _ := newAgent(t, 2)
	sweeper := &imageSweepBackend{fakeBackend: be, err: errors.New("daemon unavailable")}
	a.opts.Backends = backend.NewRegistry(sweeper)
	a.opts.DockerBuildCacheMB = 0
	a.sweepImages(context.Background())
	sweeper.err = nil
	a.sweepImages(context.Background())
	if sweeper.calls != 2 || !sweeper.bounded {
		t.Fatalf("image sweep calls=%d bounded=%v; want one per pass, each with a deadline", sweeper.calls, sweeper.bounded)
	}
}
