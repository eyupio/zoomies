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
