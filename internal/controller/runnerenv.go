package controller

import (
	"strconv"
	"time"

	"github.com/eyupio/zoomies/internal/config"
	"github.com/eyupio/zoomies/internal/store"
)

// EnvDockerWait is the variable the runner image reads for how long to wait
// for its pool's Docker daemon, in whole seconds. The fleet's runners.docker_wait
// is rendered into it; a pool's own env may name it to say otherwise.
const EnvDockerWait = "ZOOMIES_DOCKER_WAIT"

// runnerEnv is the environment a runner of this pool starts with: the fleet's
// runners.env, then the Docker wait for a pool that provides a daemon, then
// the pool's own env over the top. The pool wins every collision, because it
// is the more specific thing an operator said, and the fleet-wide layer is
// what lets one setting on the Settings page reach every pool at once rather
// than being pasted into each.
//
// Nothing here is a credential. The runner's own identity and JIT
// configuration are written by the backend from the spec's Credentials, and
// the validator refuses a runners.env that names them.
func runnerEnv(r config.Runners, pool *store.Pool) map[string]string {
	if len(r.Env) == 0 && r.DockerWait <= 0 && len(pool.Env) == 0 {
		return nil
	}
	out := make(map[string]string, len(r.Env)+len(pool.Env)+1)
	for k, v := range r.Env {
		out[k] = v
	}
	// Only a pool with a daemon needs to know how long to wait for one; the
	// image ignores the variable otherwise, but a runner's environment should
	// say what applies to it and nothing else.
	if r.DockerWait > 0 && pool.DockerMode != "" && pool.DockerMode != store.DockerNone {
		// The image takes whole seconds and refuses zero, so a wait under a
		// second rounds up to one rather than down to a refusal.
		seconds := max(1, int64((r.DockerWait+time.Second-1)/time.Second))
		out[EnvDockerWait] = strconv.FormatInt(seconds, 10)
	}
	for k, v := range pool.Env {
		out[k] = v
	}
	return out
}
