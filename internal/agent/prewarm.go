package agent

import (
	"context"
	"fmt"
	"runtime"
	"time"

	"github.com/eyupio/zoomies/internal/backend"
	"github.com/eyupio/zoomies/internal/store"
)

type warmKey struct {
	platform string
	kind     store.BackendKind
	image    string
	policy   store.PullPolicy
	dind     bool
}

type warmResult struct {
	digest       string
	at           time.Time
	refreshAfter time.Duration
}

// Pools sharing an image often refresh together. Reuse successful preparation
// for one minute, including its dependencies, rather than extracting it again.
// This only coalesces background prewarming; creates still apply their policy.
func (a *Agent) prewarm(ctx context.Context, b backend.Backend, p backend.ImagePrewarmer, task Task) (digest string, err error) {
	key := warmKey{platform: runtime.GOOS + "/" + runtime.GOARCH, kind: b.Kind(), image: task.Image, policy: task.PullPolicy,
		dind: task.Spec != nil && task.Spec.DockerMode == store.DockerDinD}
	a.mu.Lock()
	cached, found := a.warmed[key]
	a.mu.Unlock()
	if found && a.now().Sub(cached.at) >= 0 && a.now().Sub(cached.at) < cached.refreshAfter {
		a.log.Info("background image preparation", "outcome", "cache_hit", "backend", b.Kind(), "digest", cached.digest)
		return cached.digest, nil
	}
	started := a.now()
	defer func() {
		a.log.Info("background image preparation", "outcome", "refresh", "backend", b.Kind(), "duration", a.now().Sub(started), "ok", err == nil, "digest", digest)
	}()
	digest, err = p.PrewarmImage(ctx, task.Image, task.PullPolicy)
	if err != nil {
		return "", err
	}
	if key.dind {
		dependencies, ok := b.(interface {
			PrewarmDinD(context.Context) error
		})
		if !ok {
			return "", fmt.Errorf("the %s backend cannot prewarm the required Docker sidecar", b.Kind())
		}
		if err := dependencies.PrewarmDinD(ctx); err != nil {
			return "", err
		}
	}
	a.mu.Lock()
	if a.warmed == nil || len(a.warmed) >= 128 {
		a.warmed = make(map[warmKey]warmResult)
	}
	a.warmed[key] = warmResult{digest: digest, at: a.now(), refreshAfter: time.Minute - time.Duration(a.randomFraction()*float64(15*time.Second))}
	a.mu.Unlock()
	return digest, nil
}
