package backend

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strconv"

	"github.com/eyupio/zoomies/internal/store"
)

// BuildCachePruner is optional: process and Podman backends do not expose
// Docker's builder cache API. This never prunes containers, images or volumes.
type BuildCachePruner interface {
	PruneBuildCache(context.Context, int64) (int64, error)
}

func (b *DockerBackend) PruneBuildCache(ctx context.Context, keepBytes int64) (int64, error) {
	if b.Kind() != store.BackendDocker || keepBytes <= 0 {
		return 0, nil
	}
	var response struct{ SpaceReclaimed int64 }
	err := b.api.do(ctx, http.MethodPost, "/build/prune", url.Values{
		"all": {"true"}, "keep-storage": {strconv.FormatInt(keepBytes, 10)},
	}, nil, &response)
	if err != nil {
		return 0, fmt.Errorf("pruning unused Docker builder cache: %w", err)
	}
	return response.SpaceReclaimed, nil
}

// json-file is readable by the existing log API. Rotation bounds logs even
// when a cancelled runner or its sidecar is waiting for a cleanup retry.
func runnerLogConfig(fl flavor) *LogConfig {
	if fl.kind != store.BackendDocker {
		return nil
	}
	return &LogConfig{Type: "json-file", Config: map[string]string{"max-size": "10m", "max-file": "3"}}
}
