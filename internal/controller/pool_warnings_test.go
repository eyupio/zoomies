package controller

import (
	"strings"
	"testing"
	"time"

	"github.com/eyupio/zoomies/internal/config"
	"github.com/eyupio/zoomies/internal/store"
)

// A repository cache under an organisation installation is only as private
// as the pool's labels, because GitHub can hand an organisation-registered
// runner any repository's job that asks for them. That is a fact about the
// installation, which the pool cannot see in itself, so the warning has to
// come from the pairing -- and must not fire where the installation already
// confines the runner to one repository.
func TestPoolWarningsSayWhenARepositoryCacheIsOnlyAsPrivateAsItsLabels(t *testing.T) {
	cache := store.CacheConfig{Enabled: true, Scope: store.CacheScopeRepository, Repository: "acme/widgets"}
	org := &store.Installation{Target: "acme", TargetType: store.TargetOrg}
	repo := &store.Installation{Target: "acme/widgets", TargetType: store.TargetRepo}

	cases := []struct {
		name string
		pool store.Pool
		inst *store.Installation
		want bool
	}{
		{"repository cache under an organisation", store.Pool{Name: "widgets", Cache: cache}, org, true},
		{"the same cache under a repository installation", store.Pool{Name: "widgets", Cache: cache}, repo, false},
		{"pool scope is a different choice, documented as such", store.Pool{Name: "shared", Cache: store.CacheConfig{Enabled: true, Scope: store.CacheScopePool}}, org, false},
		{"a disabled cache", store.Pool{Name: "plain", Cache: store.CacheConfig{Scope: store.CacheScopeRepository, Repository: "acme/widgets"}}, org, false},
		{"an unknown installation, which validation reports itself", store.Pool{Name: "orphan", Cache: cache}, nil, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var found *Problem
			for _, w := range PoolWarnings(&tc.pool, tc.inst, config.Default()) {
				if w.Code == "pool.cache_shared" {
					w := w
					found = &w
				}
			}
			if (found != nil) != tc.want {
				t.Fatalf("warned = %v, want %v; warnings: %+v", found != nil, tc.want, PoolWarnings(&tc.pool, tc.inst, config.Default()))
			}
			if found == nil {
				return
			}
			// The sentence has to carry the mechanism, the repository at
			// stake and the one thing the operator can do about it.
			for _, want := range []string{"acme/widgets", "labels"} {
				if !strings.Contains(found.Title+found.Detail+found.Fix, want) {
					t.Errorf("the warning does not mention %q: %+v", want, *found)
				}
			}
			if found.Fix == "" || found.TargetID != tc.pool.ID {
				t.Errorf("the warning needs a fix and a target: %+v", *found)
			}
		})
	}
}

// A daemon with no client is not a slower pool: it is a pool where every job
// that touches Docker dies at its first step, with an error naming a missing
// binary and not the reason. The swap moves a pool off the client-less image
// wherever the variant is known to exist, so what reaches this warning is what
// it could not move -- and an operator who pinned that image has to be told,
// because nothing else they can see says why the jobs are failing.
func TestPoolWarningsSayWhenAPoolHasADaemonAndNoClient(t *testing.T) {
	const digest = "@sha256:8c2f1a9e5b3d4c6a7e8f0b1d2c3a4e5f60718293a4b5c6d7e8f90a1b2c3d4e5f"
	cases := []struct {
		name string
		pool store.Pool
		want string // the reference the fix should name, or "" for no warning
	}{
		{"a digest of the stock image", store.Pool{Name: "builders", DockerMode: store.DockerDinD,
			Image: "ghcr.io/eyupio/zoomies-runner" + digest},
			"ghcr.io/eyupio/zoomies-runner-docker" + digest},
		{"a commit pin from another build", store.Pool{Name: "builders", DockerMode: store.DockerDinD,
			Image: "ghcr.io/eyupio/zoomies-runner:sha-b966fb6"},
			"ghcr.io/eyupio/zoomies-runner-docker:sha-b966fb6"},
		{"the host socket needs a client just as much", store.Pool{Name: "builders", DockerMode: store.DockerHostSocket,
			Image: "ghcr.io/eyupio/zoomies-runner:sha-b966fb6"},
			"ghcr.io/eyupio/zoomies-runner-docker:sha-b966fb6"},
		// Everything the swap handles, which is the common case and must stay
		// quiet: a warning that fires on a working pool is a warning nobody
		// reads on the pool where it matters.
		{"a tag the swap moves", store.Pool{Name: "builders", DockerMode: store.DockerDinD,
			Image: "ghcr.io/eyupio/zoomies-runner:latest"}, ""},
		{"a pool that names no image at all", store.Pool{Name: "builders", DockerMode: store.DockerDinD}, ""},
		{"a pool that names an operating system", store.Pool{Name: "builders", DockerMode: store.DockerDinD,
			Platform: store.Platform{OS: "debian", OSVersion: "12"}}, ""},
		{"an image of the operator's own, which may carry one", store.Pool{Name: "builders", DockerMode: store.DockerDinD,
			Image: "registry.example.com/ci/runner:latest"}, ""},
		{"no daemon, nothing to reach", store.Pool{Name: "plain", DockerMode: store.DockerNone,
			Image: "ghcr.io/eyupio/zoomies-runner:sha-b966fb6"}, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var found *Problem
			for _, w := range PoolWarnings(&tc.pool, nil, config.Default()) {
				if w.Code == "pool.docker_client_missing" {
					w := w
					found = &w
				}
			}
			if (found == nil) != (tc.want == "") {
				t.Fatalf("warned = %v, want %v", found != nil, tc.want != "")
			}
			if found == nil {
				return
			}
			if found.Severity != config.SeverityError {
				t.Errorf("severity = %q; every Docker job on this pool fails, which is work not happening", found.Severity)
			}
			// The operator needs the reference to paste, not a repository to
			// go and search for, and the error the jobs are dying with so the
			// two can be connected.
			for _, want := range []string{tc.want, "Unable to locate executable file: docker", tc.pool.Name} {
				if !strings.Contains(found.Title+found.Detail+found.Fix, want) {
					t.Errorf("the warning does not mention %q: %+v", want, *found)
				}
			}
		})
	}
}

// A pool that named no image is running the fleet's default, and no edit to the
// pool corrects a default that cannot be swapped -- so the sentence has to name
// the setting that can be. Telling that operator to "clear the pool's image"
// would be advice they have already taken.
func TestTheDockerClientFixNamesWhicheverSettingCanBeChanged(t *testing.T) {
	const digest = "@sha256:8c2f1a9e5b3d4c6a7e8f0b1d2c3a4e5f60718293a4b5c6d7e8f90a1b2c3d4e5f"
	cfg := config.Default()
	cfg.GitHub.RunnerImage = "ghcr.io/eyupio/zoomies-runner" + digest

	following := store.Pool{Name: "builders", DockerMode: store.DockerDinD}
	own := store.Pool{Name: "builders", DockerMode: store.DockerDinD,
		Image: "ghcr.io/eyupio/zoomies-runner" + digest}

	fix := func(p store.Pool, c *config.Config) string {
		w, ok := dockerClientWarning(&p, c)
		if !ok {
			t.Fatalf("no warning for %+v", p)
		}
		return w.Fix
	}
	if got := fix(following, cfg); !strings.Contains(got, "github.runner_image") {
		t.Errorf("a pool on the fleet's default is told: %q", got)
	}
	if got := fix(own, config.Default()); !strings.Contains(got, "image of pool builders") {
		t.Errorf("a pool with an image of its own is told: %q", got)
	}
}

// A fleet that exists to build container images decides about the privileged
// sidecar once. Repeating it for every such pool on every pass is what teaches
// an operator to skim the problems list, and the list is only worth having
// while it is read -- so the fleet may ask not to be told that one again, and
// only that one.
func TestAFleetCanSayDockerInDockerIsExpected(t *testing.T) {
	quiet := config.Default()
	quiet.Security.DockerInDockerExpected = true

	dangers := func(p store.Pool, cfg *config.Config) []string {
		var out []string
		for _, w := range PoolWarnings(&p, nil, cfg) {
			if w.Code == "pool.dangerous" {
				out = append(out, w.Title)
			}
		}
		return out
	}
	dind := store.Pool{Name: "builders", DockerMode: store.DockerDinD, Ephemeral: true}
	if got := dangers(dind, config.Default()); len(got) != 1 {
		t.Fatalf("a dind pool warns %v by default, want the one sentence", got)
	}
	if got := dangers(dind, quiet); len(got) != 0 {
		t.Errorf("a dind pool still warns once the fleet said it knows: %v", got)
	}

	// What the setting must not silence: the socket hands any job on the pool
	// root on the host, and that is a different decision from wanting Docker.
	socket := store.Pool{Name: "builders", DockerMode: store.DockerHostSocket, Ephemeral: true}
	if got := dangers(socket, quiet); len(got) != 1 {
		t.Errorf("the host socket warns %v, want it said whatever the fleet asked about dind", got)
	}
	persistent := store.Pool{Name: "builders", DockerMode: store.DockerDinD}
	if got := dangers(persistent, quiet); len(got) != 1 {
		t.Errorf("persistent runners warn %v, want the sentence that is not about Docker kept", got)
	}
}

func TestAutomaticPoolWarningsIncludeFleetStartupRecommendations(t *testing.T) {
	cfg := config.Default()
	cfg.Agent.Embedded = true
	cfg.Scheduler.DefaultRunnerLimits = false
	cfg.Scheduler.HostThrottling = false
	cfg.Agent.BootstrapCPUGrace = 0
	cfg.Runners.DockerWait = time.Minute
	cfg.Scheduler.ProvisionTimeout = time.Minute
	p := &store.Pool{Backend: store.BackendDocker, DockerMode: store.DockerDinD}
	wanted := map[string]bool{"scheduler.default_runner_limits": false, "scheduler.host_throttling": false, "agent.bootstrap_cpu_grace": false, "runners.docker_wait": false, "scheduler.provision_timeout": false}
	for _, w := range PoolWarnings(p, nil, cfg) {
		if _, ok := wanted[w.Setting]; ok {
			wanted[w.Setting] = true
		}
	}
	for key, found := range wanted {
		if !found {
			t.Errorf("automatic pool did not explain %s", key)
		}
	}
	p.Resources = store.Resources{CPUs: 2, MemoryMB: 4096}
	for _, w := range PoolWarnings(p, nil, cfg) {
		if w.Code == "scheduler.default_runner_limits_off" {
			t.Fatal("fixed pool received auto sizing remedy")
		}
	}
}
