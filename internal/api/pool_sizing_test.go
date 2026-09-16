package api

import (
	"net/http"
	"testing"
	"time"

	"github.com/eyupio/zoomies/internal/config"
	"github.com/eyupio/zoomies/internal/controller"
	"github.com/eyupio/zoomies/internal/store"
)

// A pool that names no size is saved with the fleet's, not with no limit.
//
// This is the whole of why sizing is mandatory: a runner with no cgroup limit
// takes every core on the machine it lands on while the fleet charges it one
// slot's share, so the host reads as half committed, its daemon stops
// answering, and the creates queued behind it time out on a machine every page
// calls busy.
func TestAPoolThatNamesNoSizeIsSizedFromTheFleet(t *testing.T) {
	h := newHarness(t)
	inst := h.installation()
	u, _ := h.user("operator", store.RoleOperator)
	cookie := h.session(u)

	res := h.do(request{method: http.MethodPost, path: "/api/v1/pools", cookie: cookie, body: poolBody(inst.ID)})
	res.mustStatus(t, http.StatusCreated, "create")
	var created store.Pool
	res.into(t, &created)

	if created.Resources.CPUs != config.DefaultRunnerCPUs {
		t.Errorf("cpus = %v, want the fleet default %v", created.Resources.CPUs, config.DefaultRunnerCPUs)
	}
	if created.Resources.MemoryMB != config.DefaultRunnerMemoryMB {
		t.Errorf("memory_mb = %d, want the fleet default %d", created.Resources.MemoryMB, config.DefaultRunnerMemoryMB)
	}
}

// The default is the fleet's own answer, so a fleet of bigger machines says so
// once rather than on every pool it creates.
func TestTheFleetsOwnDefaultSizeIsWhatANewPoolGets(t *testing.T) {
	h := newHarness(t, func(c *config.Config) {
		c.Runners.DefaultCPUs = 4
		c.Runners.DefaultMemoryMB = 8192
	})
	inst := h.installation()
	u, _ := h.user("operator", store.RoleOperator)
	cookie := h.session(u)

	defaults := h.do(request{method: http.MethodGet, path: "/api/v1/pools/defaults", cookie: cookie})
	defaults.mustStatus(t, http.StatusOK, "defaults")
	var shown poolDefaultsResponse
	defaults.into(t, &shown)
	if shown.Resources.CPUs != 4 || shown.Resources.MemoryMB != 8192 {
		t.Fatalf("defaults = %+v, want 4 CPU and 8192 MB", shown.Resources)
	}

	res := h.do(request{method: http.MethodPost, path: "/api/v1/pools", cookie: cookie, body: poolBody(inst.ID)})
	res.mustStatus(t, http.StatusCreated, "create")
	var created store.Pool
	res.into(t, &created)
	if created.Resources.CPUs != 4 || created.Resources.MemoryMB != 8192 {
		t.Errorf("a pool created with no size got %+v, want the fleet's 4 CPU and 8 GB", created.Resources)
	}
}

// A size below what a runner needs to be a runner is refused, because the job
// it fails is failed in a way that looks like a broken image rather than like a
// limit somebody typed.
func TestASizeTooSmallToRunAJobIsRefused(t *testing.T) {
	h := newHarness(t)
	inst := h.installation()
	u, _ := h.user("operator", store.RoleOperator)
	cookie := h.session(u)

	for _, tc := range []struct {
		name  string
		size  map[string]any
		field string
	}{
		{"a tenth of a core", map[string]any{"cpus": 0.1, "memory_mb": 4096}, "resources.cpus"},
		{"256 MB", map[string]any{"cpus": 2, "memory_mb": 256}, "resources.memory_mb"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			body := poolBody(inst.ID)
			body["resources"] = tc.size
			res := h.do(request{method: http.MethodPost, path: "/api/v1/pools", cookie: cookie, body: body})
			res.mustStatus(t, http.StatusUnprocessableEntity, "create")
			var env errorEnvelope
			res.into(t, &env)
			named := false
			for _, fe := range env.Errors {
				if fe.Field == tc.field {
					named = true
				}
			}
			if !named {
				t.Errorf("no error on %s: %s", tc.field, res.body)
			}
		})
	}
}

// The room is what a maximum is worth comparing against, and a maximum above it
// is runners the scheduler will never create with nothing else saying so.
func TestTheRoomIsCountedFromTheMachinesAndTheSize(t *testing.T) {
	h := newHarness(t)
	inst := h.installation()
	u, _ := h.user("operator", store.RoleOperator)
	cookie := h.session(u)

	// Ten cores and 20 GB, set to eight slots. Half a core and 512 MB are held
	// back by the floors, so the machine has room for four 2-core, 4 GB
	// runners -- and four of its slots are promises it cannot keep.
	host := &store.Host{
		Name: "big-1", Capacity: 8,
		CPUs: 10, MemoryMB: 20480,
		Backends: store.StringSlice{"docker"}, Labels: store.StringMap{},
		OS: "linux", Arch: "amd64", LastHeartbeat: time.Now(),
	}
	if err := h.st.CreateHost(h.ctx, host); err != nil {
		t.Fatalf("CreateHost: %v", err)
	}

	body := poolBody(inst.ID)
	body["max_runners"] = 8
	res := h.do(request{method: http.MethodPost, path: "/api/v1/pools/validate", cookie: cookie, body: body})
	res.mustStatus(t, http.StatusOK, "validate")
	var verdict validatePoolResponse
	res.into(t, &verdict)

	if verdict.Room.Runners != 4 {
		t.Errorf("room = %d runners, want 4 on ten cores and 20 GB at 2 CPU and 4 GB each", verdict.Room.Runners)
	}
	if len(verdict.Room.Hosts) != 1 {
		t.Fatalf("room lists %d hosts, want 1", len(verdict.Room.Hosts))
	}
	only := verdict.Room.Hosts[0]
	if only.Slots != 8 || only.Fits != 4 || only.Room != 4 {
		t.Errorf("host room = %+v, want 8 slots, 4 fits, 4 room", only)
	}
	if only.LimitedBy != "cpu" {
		t.Errorf("limited_by = %q, want cpu -- the machine runs out before the slots do", only.LimitedBy)
	}
	if !hasWarning(verdict.Warnings, "pool.max_above_room") {
		t.Errorf("a maximum of 8 on a fleet with room for 4 raised no warning: %+v", verdict.Warnings)
	}
	if !hasWarning(verdict.Warnings, "pool.host_overcommitted") {
		t.Errorf("a host promising 8 slots with room for 4 raised no warning: %+v", verdict.Warnings)
	}
}

// A maximum the fleet can actually place says nothing, because a warning an
// operator sees on every correct pool is one they stop reading.
func TestAMaximumTheFleetCanPlaceRaisesNothing(t *testing.T) {
	h := newHarness(t)
	inst := h.installation()
	u, _ := h.user("operator", store.RoleOperator)
	cookie := h.session(u)

	host := &store.Host{
		Name: "big-1", Capacity: 4,
		CPUs: 10, MemoryMB: 20480,
		Backends: store.StringSlice{"docker"}, Labels: store.StringMap{},
		OS: "linux", Arch: "amd64", LastHeartbeat: time.Now(),
	}
	if err := h.st.CreateHost(h.ctx, host); err != nil {
		t.Fatalf("CreateHost: %v", err)
	}

	body := poolBody(inst.ID)
	body["max_runners"] = 4
	res := h.do(request{method: http.MethodPost, path: "/api/v1/pools/validate", cookie: cookie, body: body})
	res.mustStatus(t, http.StatusOK, "validate")
	var verdict validatePoolResponse
	res.into(t, &verdict)
	if len(verdict.Warnings) != 0 {
		t.Errorf("a pool that fits its fleet warned about it: %+v", verdict.Warnings)
	}
	if verdict.Resources.CPUs != config.DefaultRunnerCPUs {
		t.Errorf("the verdict did not carry the size the pool would run at: %+v", verdict.Resources)
	}
}

// A cache that may grow past the disk it lives on is not limited at all: the
// disk fills first, and a host below its disk reserve takes no runner of any
// pool.
func TestACacheLimitAboveTheDiskWarns(t *testing.T) {
	h := newHarness(t)
	inst := h.installation()
	u, _ := h.user("operator", store.RoleOperator)
	cookie := h.session(u)

	host := &store.Host{
		Name: "small-disk", Capacity: 2,
		CPUs: 8, MemoryMB: 16384, DiskTotalMB: 40960, DiskFreeMB: 10240,
		Backends: store.StringSlice{"docker"}, Labels: store.StringMap{},
		OS: "linux", Arch: "amd64", LastHeartbeat: time.Now(),
	}
	if err := h.st.CreateHost(h.ctx, host); err != nil {
		t.Fatalf("CreateHost: %v", err)
	}

	body := poolBody(inst.ID)
	body["max_runners"] = 2
	body["cache"] = map[string]any{
		"enabled":    true,
		"scope":      "pool",
		"source":     "/var/lib/zoomies-cache",
		"size_limit": 64 * 1024 * 1024 * 1024,
	}
	res := h.do(request{method: http.MethodPost, path: "/api/v1/pools/validate", cookie: cookie, body: body})
	res.mustStatus(t, http.StatusOK, "validate")
	var verdict validatePoolResponse
	res.into(t, &verdict)
	if !hasWarning(verdict.Warnings, "pool.cache_above_disk") {
		t.Errorf("a 64 GB cache limit on a host with 10 GB free raised no warning: %+v", verdict.Warnings)
	}
}

// A pool created before sizing was mandatory is sized the first time anything
// about it is saved -- which is the moment an operator is looking at it.
func TestEditingAPoolWithNoSizeSizesIt(t *testing.T) {
	h := newHarness(t)
	inst := h.installation()
	u, _ := h.user("operator", store.RoleOperator)
	cookie := h.session(u)

	legacy := &store.Pool{
		Name: "zoomies-legacy", InstallationID: inst.ID,
		Labels:  store.StringSlice{"self-hosted", "zoomies-legacy"},
		Backend: store.BackendDocker, PullPolicy: store.PullIfNotPresent,
		Image:      "ghcr.io/eyupio/zoomies-runner:test",
		MaxRunners: 2, IdleTimeout: store.Duration(5 * time.Minute),
		Ephemeral: true, DockerMode: store.DockerNone, Enabled: true,
	}
	if err := h.st.CreatePool(h.ctx, legacy); err != nil {
		t.Fatalf("CreatePool: %v", err)
	}

	res := h.do(request{method: http.MethodPatch, path: "/api/v1/pools/" + legacy.ID,
		cookie: cookie, body: map[string]any{"priority": 1}})
	res.mustStatus(t, http.StatusOK, "patch")
	var updated store.Pool
	res.into(t, &updated)
	if updated.Resources.CPUs != config.DefaultRunnerCPUs || updated.Resources.MemoryMB != config.DefaultRunnerMemoryMB {
		t.Errorf("an unsized pool stayed unsized after an edit: %+v", updated.Resources)
	}
}

func hasWarning(warnings []controller.Problem, code string) bool {
	for _, w := range warnings {
		if w.Code == code {
			return true
		}
	}
	return false
}
