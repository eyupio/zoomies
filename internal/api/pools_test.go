package api

import (
	"net/http"
	"slices"
	"strings"
	"testing"

	"github.com/eyupio/zoomies/internal/controller"
	"github.com/eyupio/zoomies/internal/store"
)

// poolBody is a valid pool definition, which each test then breaks in one way.
// poolBody is a create request with a name that does not carry the brand,
// because that is what an operator types and the server is what makes every
// pool name branded.
func poolBody(instID string) map[string]any {
	return map[string]any{
		"name":            "linux-x64",
		"installation_id": instID,
		"labels":          []string{"self-hosted", "linux", "x64", "linux-x64"},
		"backend":         "docker",
		"image":           "ghcr.io/eyupio/zoomies-runner:test",
		"min_runners":     0,
		"max_runners":     4,
		"idle_timeout":    "5m",
		"ephemeral":       true,
		"docker_mode":     "none",
		"enabled":         true,
	}
}

// TestPoolRoundTrip walks the whole pool lifecycle the way the UI does:
// validate, create, read, edit, disable, enable, delete.
func TestPoolRoundTrip(t *testing.T) {
	h := newHarness(t)
	inst := h.installation()
	h.host("vm-1")
	u, _ := h.user("operator", store.RoleOperator)
	cookie := h.session(u)

	// The wizard's review step.
	validate := h.do(request{method: http.MethodPost, path: "/api/v1/pools/validate",
		cookie: cookie, body: poolBody(inst.ID)})
	validate.mustStatus(t, http.StatusOK, "validate")
	var verdict validatePoolResponse
	validate.into(t, &verdict)
	if !verdict.Valid {
		t.Fatalf("a valid pool was rejected: %+v", verdict.Errors)
	}
	if verdict.MatchingHosts != 1 {
		t.Errorf("matching_hosts = %d, want 1", verdict.MatchingHosts)
	}
	if len(verdict.Warnings) != 0 {
		t.Errorf("a safe pool produced warnings: %+v", verdict.Warnings)
	}
	// Nothing was created.
	if pools, err := h.st.ListPools(h.ctx); err != nil || len(pools) != 0 {
		t.Fatalf("validate created a pool: %v %v", pools, err)
	}

	created := h.do(request{method: http.MethodPost, path: "/api/v1/pools", cookie: cookie, body: poolBody(inst.ID)})
	created.mustStatus(t, http.StatusCreated, "create")
	var pool poolResponse
	created.into(t, &pool)
	if pool.ID == "" || pool.Name != "zoomies-linux-x64" {
		t.Fatalf("created pool = %+v, want the name branded on the way in", pool)
	}
	if pool.InstallationTarget != "acme" {
		t.Errorf("installation_target = %q, want acme", pool.InstallationTarget)
	}
	if pool.Counts.Live != 0 || pool.QueuedJobs != 0 {
		t.Errorf("a new pool reports work it does not have: %+v", pool.Counts)
	}

	// The same name twice is a conflict rather than a second pool.
	dup := h.do(request{method: http.MethodPost, path: "/api/v1/pools", cookie: cookie, body: poolBody(inst.ID)})
	dup.mustStatus(t, http.StatusUnprocessableEntity, "duplicate name")

	got := h.do(request{method: http.MethodGet, path: "/api/v1/pools/" + pool.ID, cookie: cookie})
	got.mustStatus(t, http.StatusOK, "get")

	// A patch changes one field and leaves the rest alone.
	patched := h.do(request{method: http.MethodPatch, path: "/api/v1/pools/" + pool.ID, cookie: cookie,
		body: map[string]any{"max_runners": 8}})
	patched.mustStatus(t, http.StatusOK, "patch")
	var updated poolResponse
	patched.into(t, &updated)
	if updated.MaxRunners != 8 {
		t.Errorf("max_runners = %d, want 8", updated.MaxRunners)
	}
	// Four labels as asked for, plus the brand every pool answers to.
	if updated.Name != "zoomies-linux-x64" || len(updated.Labels) != 5 {
		t.Errorf("a partial update lost fields: %+v", updated)
	}
	if !slices.Contains(updated.Labels, store.BrandLabel) {
		t.Errorf("labels = %v, want the %q label on every pool", updated.Labels, store.BrandLabel)
	}

	disabled := h.do(request{method: http.MethodPost, path: "/api/v1/pools/" + pool.ID + "/disable", cookie: cookie})
	disabled.mustStatus(t, http.StatusOK, "disable")
	var afterDisable poolResponse
	disabled.into(t, &afterDisable)
	if afterDisable.Enabled {
		t.Error("the pool is still enabled after being disabled")
	}

	enabled := h.do(request{method: http.MethodPost, path: "/api/v1/pools/" + pool.ID + "/enable", cookie: cookie})
	enabled.mustStatus(t, http.StatusOK, "enable")

	deleted := h.do(request{method: http.MethodDelete, path: "/api/v1/pools/" + pool.ID, cookie: cookie})
	deleted.mustStatus(t, http.StatusOK, "delete")
	var deletion deletePoolResponse
	deleted.into(t, &deletion)
	if deletion.RunnersAffected != 0 {
		t.Errorf("runners_affected = %d, want 0", deletion.RunnersAffected)
	}

	gone := h.do(request{method: http.MethodGet, path: "/api/v1/pools/" + pool.ID, cookie: cookie})
	gone.mustStatus(t, http.StatusNotFound, "get after delete")

	// Every one of those mutations left an audit row.
	audit := h.do(request{method: http.MethodGet, path: "/api/v1/audit?target_kind=pool", cookie: cookie})
	audit.mustStatus(t, http.StatusOK, "audit")
	var events page[store.AuditEvent]
	audit.into(t, &events)
	seen := map[string]bool{}
	for _, e := range events.Items {
		seen[e.Action] = true
	}
	for _, want := range []string{"pool.create", "pool.update", "pool.enable", "pool.disable", "pool.delete"} {
		if !seen[want] {
			t.Errorf("no %s row in the audit log (saw %v)", want, seen)
		}
	}
}

// TestPoolValidationNamesTheField checks that a refusal is something an
// operator can act on rather than "invalid request".
func TestPoolValidationNamesTheField(t *testing.T) {
	h := newHarness(t)
	inst := h.installation()
	u, _ := h.user("operator", store.RoleOperator)
	cookie := h.session(u)

	cases := []struct {
		name   string
		mutate func(map[string]any)
		field  string
		says   string
	}{
		{"no name", func(b map[string]any) { delete(b, "name") }, "name", "name"},
		{"no labels", func(b map[string]any) { b["labels"] = []string{} }, "labels", "at least one label"},
		{"only implicit labels", func(b map[string]any) { b["labels"] = []string{"self-hosted", "linux"} }, "labels", "at least one label"},
		{"unknown backend", func(b map[string]any) { b["backend"] = "kubernetes" }, "backend", "docker, podman or process"},
		{"max below min", func(b map[string]any) { b["min_runners"] = 5; b["max_runners"] = 2 }, "min_runners", "maximum"},
		{"zero max", func(b map[string]any) { b["max_runners"] = 0 }, "max_runners", "at least 1"},
		{"bad duration", func(b map[string]any) { b["idle_timeout"] = "5 munutes" }, "idle_timeout", "5m"},
		{"unknown installation", func(b map[string]any) { b["installation_id"] = "ins_nope" }, "installation_id", "no installation"},
		{"docker on the process backend", func(b map[string]any) { b["backend"] = "process"; b["docker_mode"] = "dind" }, "docker_mode", "process backend"},
		{"repository cache without a repository", func(b map[string]any) {
			b["cache"] = map[string]any{"enabled": true, "scope": "repository"}
		}, "cache.repository", "acme/name"},
		{"repository cache under another owner", func(b map[string]any) {
			b["cache"] = map[string]any{"enabled": true, "scope": "repository", "repository": "other/widgets"}
		}, "cache.repository", "under that owner"},
		{"repository cache that is not a repository", func(b map[string]any) {
			b["cache"] = map[string]any{"enabled": true, "scope": "repository", "repository": "acme"}
		}, "cache.repository", "owner/name"},
		// A limit the fleet cannot keep is refused rather than accepted and
		// forgotten: there is no directory to measure inside a named volume.
		{"size limit on a named volume", func(b map[string]any) {
			b["cache"] = map[string]any{"enabled": true, "scope": "pool", "size_limit": 1 << 30, "source": "zoomies-cache"}
		}, "cache.size_limit", "absolute host path"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			body := poolBody(inst.ID)
			tc.mutate(body)

			create := h.do(request{method: http.MethodPost, path: "/api/v1/pools", cookie: cookie, body: body})
			create.mustStatus(t, http.StatusUnprocessableEntity, "create")
			var env errorEnvelope
			create.into(t, &env)
			if env.Error.Code != codeUnprocessable {
				t.Fatalf("code = %q, want %q", env.Error.Code, codeUnprocessable)
			}
			found := ""
			for _, fe := range env.Errors {
				if fe.Field == tc.field {
					found = fe.Message
				}
			}
			if found == "" {
				t.Fatalf("no error on %q; got %+v", tc.field, env.Errors)
			}
			if !strings.Contains(found, tc.says) {
				t.Errorf("the message on %s does not say %q: %q", tc.field, tc.says, found)
			}

			// The dry run reports exactly the same thing without creating
			// anything, which is the property the wizard depends on.
			validate := h.do(request{method: http.MethodPost, path: "/api/v1/pools/validate", cookie: cookie, body: body})
			validate.mustStatus(t, http.StatusOK, "validate")
			var verdict validatePoolResponse
			validate.into(t, &verdict)
			if verdict.Valid {
				t.Fatal("validate accepted a pool that create refused")
			}
			if len(verdict.Errors) != len(env.Errors) {
				t.Errorf("validate reported %d errors, create reported %d", len(verdict.Errors), len(env.Errors))
			}
		})
	}
}

// The review step is where an operator decides, so the dry run has to carry
// the warning that a repository cache under an organisation installation is
// only as private as the pool's labels -- the installation is the harness's,
// which targets an organisation.
func TestPoolValidateWarnsThatARepositoryCacheIsOnlyAsPrivateAsItsLabels(t *testing.T) {
	h := newHarness(t)
	inst := h.installation()
	h.host("vm-1")
	u, _ := h.user("operator", store.RoleOperator)

	body := poolBody(inst.ID)
	body["cache"] = map[string]any{"enabled": true, "scope": "repository", "repository": "acme/widgets"}

	resp := h.do(request{method: http.MethodPost, path: "/api/v1/pools/validate", cookie: h.session(u), body: body})
	resp.mustStatus(t, http.StatusOK, "validate")
	var verdict validatePoolResponse
	resp.into(t, &verdict)
	if !verdict.Valid {
		t.Fatalf("a repository cache under an organisation installation is allowed: %+v", verdict.Errors)
	}
	found := false
	for _, w := range verdict.Warnings {
		if w.Code == "pool.cache_shared" {
			found = true
			if !strings.Contains(w.Detail, "acme") || w.Fix == "" {
				t.Errorf("the warning should name the organisation and say what to do: %+v", w)
			}
		}
	}
	if !found {
		t.Fatalf("no shared-cache warning in %+v", verdict.Warnings)
	}
}

// An organisation-wide installation is the ordinary deployment -- one app, one
// fleet -- and it must not cost every repository its own cache. Naming the
// repository is what the installation cannot do for itself.
func TestARepositoryCacheIsAllowedUnderAnOrganisationInstallation(t *testing.T) {
	h := newHarness(t)
	inst := h.installation()
	h.host("vm-1")
	u, _ := h.user("operator", store.RoleOperator)
	cookie := h.session(u)

	body := poolBody(inst.ID)
	body["cache"] = map[string]any{
		"enabled": true, "scope": "repository", "repository": "acme/widgets",
		"source": "/var/lib/zoomies/cache", "size_limit": 1 << 30,
	}
	created := h.do(request{method: http.MethodPost, path: "/api/v1/pools", cookie: cookie, body: body})
	created.mustStatus(t, http.StatusCreated, "create")
	var pool poolResponse
	created.into(t, &pool)

	stored, err := h.st.GetPool(h.ctx, pool.ID)
	if err != nil {
		t.Fatalf("GetPool: %v", err)
	}
	if stored.Cache.Repository != "acme/widgets" || stored.Cache.Scope != store.CacheScopeRepository {
		t.Fatalf("cache = %+v, want a repository cache for acme/widgets", stored.Cache)
	}
}

// TestPoolValidateWarnsAboutDangerousSettings is the other half of the review
// step: the settings that are allowed but weaken isolation are named before the
// pool is created, not after.
func TestPoolValidateWarnsAboutDangerousSettings(t *testing.T) {
	h := newHarness(t)
	inst := h.installation()
	h.host("vm-1")
	u, _ := h.user("operator", store.RoleOperator)

	body := poolBody(inst.ID)
	body["docker_mode"] = "host-socket"
	body["run_as_root"] = true
	body["ephemeral"] = false

	resp := h.do(request{method: http.MethodPost, path: "/api/v1/pools/validate", cookie: h.session(u), body: body})
	resp.mustStatus(t, http.StatusOK, "validate")
	var verdict validatePoolResponse
	resp.into(t, &verdict)
	if !verdict.Valid {
		t.Fatalf("a dangerous pool is still a valid one: %+v", verdict.Errors)
	}
	if len(verdict.Warnings) < 3 {
		t.Fatalf("expected a warning for each dangerous setting, got %d: %+v", len(verdict.Warnings), verdict.Warnings)
	}
	joined := ""
	for _, w := range verdict.Warnings {
		joined += w.Title + "|"
		if w.Fix == "" {
			t.Errorf("warning %q has no fix", w.Title)
		}
	}
	for _, want := range []string{"root on the host", "as root", "persistent"} {
		if !strings.Contains(joined, want) {
			t.Errorf("no warning mentions %q; got %s", want, joined)
		}
	}
}

func TestPoolValidateWarnsWhenNoHostMatches(t *testing.T) {
	h := newHarness(t)
	inst := h.installation()
	u, _ := h.user("operator", store.RoleOperator)

	resp := h.do(request{method: http.MethodPost, path: "/api/v1/pools/validate",
		cookie: h.session(u), body: poolBody(inst.ID)})
	resp.mustStatus(t, http.StatusOK, "validate")

	var verdict validatePoolResponse
	resp.into(t, &verdict)
	if verdict.MatchingHosts != 0 {
		t.Fatalf("matching_hosts = %d with no hosts registered", verdict.MatchingHosts)
	}
	found := false
	for _, w := range verdict.Warnings {
		if w.Code == "pool.no_matching_hosts" {
			found = true
		}
	}
	if !found {
		t.Errorf("a pool no host can run produced no warning: %+v", verdict.Warnings)
	}
}

// The usual reason no host matches is not a missing machine: it is a host that
// is right there with a daemon its agent could not reach. The agent's own
// explanation is the shortest route to a working pool, so the dry run has to
// carry it rather than telling the operator to go and add a host.
func TestPoolValidateRepeatsWhatTheHostSaidAboutItsBackend(t *testing.T) {
	h := newHarness(t)
	inst := h.installation()
	u, _ := h.user("operator", store.RoleOperator)

	host := h.host("vm-1")
	host.Backends = store.StringSlice{"process"}
	host.BackendInfo = store.HostBackends{{
		Kind:   store.BackendDocker,
		Detail: "/var/run/docker.sock is not readable by this agent",
	}}
	if err := h.st.UpdateHost(h.ctx, host); err != nil {
		t.Fatalf("UpdateHost: %v", err)
	}

	resp := h.do(request{method: http.MethodPost, path: "/api/v1/pools/validate",
		cookie: h.session(u), body: poolBody(inst.ID)})
	resp.mustStatus(t, http.StatusOK, "validate")

	var verdict validatePoolResponse
	resp.into(t, &verdict)
	if verdict.MatchingHosts != 0 {
		t.Fatalf("matching_hosts = %d, want none: the only host has no docker", verdict.MatchingHosts)
	}
	var warning *controller.Problem
	for i, w := range verdict.Warnings {
		if w.Code == "pool.no_matching_hosts" {
			warning = &verdict.Warnings[i]
		}
	}
	if warning == nil {
		t.Fatalf("warnings = %+v, want one about no host matching", verdict.Warnings)
	}
	if !strings.Contains(warning.Detail, "vm-1 reports:") ||
		!strings.Contains(warning.Detail, "not readable by this agent") {
		t.Errorf("detail = %q, want the host's own explanation", warning.Detail)
	}
	if !strings.Contains(warning.Fix, "docker") {
		t.Errorf("fix = %q, want it to point at the backend rather than at buying a machine", warning.Fix)
	}
	// The host runs the process backend, so the wizard can offer that instead
	// of leaving "a backend they already offer" as an exercise.
	if !slices.Contains(warning.Alternatives, "process") {
		t.Errorf("alternatives = %v, want the backend this host does offer", warning.Alternatives)
	}
	if !strings.Contains(warning.Fix, "point this pool at process") {
		t.Errorf("fix = %q, want the alternative named", warning.Fix)
	}
}

// TestDeletePoolDrainsItsRunners checks the default: deleting a pool finishes
// the work in flight rather than interrupting it.
func TestDeletePoolDrainsItsRunners(t *testing.T) {
	h := newHarness(t)
	inst := h.installation()
	pool := h.pool(inst, "linux-x64")
	host := h.host("vm-1")
	h.runner(pool, host, store.RunnerIdle)
	h.runner(pool, host, store.RunnerIdle)
	u, _ := h.user("operator", store.RoleOperator)

	resp := h.do(request{method: http.MethodDelete, path: "/api/v1/pools/" + pool.ID, cookie: h.session(u)})
	resp.mustStatus(t, http.StatusOK, "delete")
	var out deletePoolResponse
	resp.into(t, &out)
	if out.RunnersAffected != 2 {
		t.Fatalf("runners_affected = %d, want 2", out.RunnersAffected)
	}
}

// The pool page is where an operator lands when a workflow is not running, so
// the reason its runners cannot be placed belongs there and not only on the
// Overview.
func TestPoolResponseCarriesWhyItCannotScale(t *testing.T) {
	h := newHarness(t)
	inst := h.installation()
	pool := h.pool(inst, "linux-x64")
	h.job(pool, store.JobQueued)
	u, _ := h.user("viewer", store.RoleViewer)

	// No host at all, so the scheduler wants a runner and has nowhere to put it.
	if err := h.ctrl.Reconcile(h.ctx); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}

	resp := h.do(request{method: http.MethodGet, path: "/api/v1/pools/" + pool.ID, cookie: h.session(u)})
	resp.mustStatus(t, http.StatusOK, "get pool")
	var out poolResponse
	resp.into(t, &out)

	found := false
	for _, w := range out.Warnings {
		if w.Code == "pool.no_capacity" {
			found = true
			if w.Detail == "" || w.Fix == "" {
				t.Errorf("warning = %+v, want it to say what is true and what to change", w)
			}
		}
	}
	if !found {
		t.Errorf("warnings = %+v, want one saying the pool has nowhere to run", out.Warnings)
	}
}

// A pool's name is in every runner it registers and in the label a workflow
// asks for, so renaming one to something that says nothing about this fleet is
// not something the API lets an operator do by accident: the name is branded on
// the way in, and what comes back is the name that was stored.
func TestARenameCannotDropTheBrand(t *testing.T) {
	h := newHarness(t)
	inst := h.installation()
	h.host("vm-1")
	u, _ := h.user("operator", store.RoleOperator)
	cookie := h.session(u)

	created := h.do(request{method: http.MethodPost, path: "/api/v1/pools", cookie: cookie, body: poolBody(inst.ID)})
	created.mustStatus(t, http.StatusCreated, "create")
	var pool poolResponse
	created.into(t, &pool)

	renamed := h.do(request{method: http.MethodPatch, path: "/api/v1/pools/" + pool.ID, cookie: cookie,
		body: map[string]any{"name": "gpu"}})
	renamed.mustStatus(t, http.StatusOK, "rename")
	var updated poolResponse
	renamed.into(t, &updated)
	if updated.Name != "zoomies-gpu" {
		t.Fatalf("name = %q, want the rename branded", updated.Name)
	}

	stored, err := h.st.GetPool(h.ctx, pool.ID)
	if err != nil {
		t.Fatalf("GetPool: %v", err)
	}
	if stored.Name != "zoomies-gpu" {
		t.Fatalf("stored name = %q, want the response and the database to agree", stored.Name)
	}
}

// The two fields the API accepts for a pool and used to forget: a PATCH that
// set them answered 200 with a body that did not show them, and nothing ever
// read them back -- not GET, not the event stream, not the CLI.
func TestPoolResponsesCarryTheRepositoryLimitAndTheCostRate(t *testing.T) {
	h := newHarness(t)
	inst := h.installation()
	u, _ := h.user("operator", store.RoleOperator)

	body := poolBody(inst.ID)
	body["repository_scale_up_limit"] = 2
	body["cost_per_runner_hour"] = 0.25
	created := h.do(request{method: http.MethodPost, path: "/api/v1/pools", cookie: h.session(u), body: body})
	created.mustStatus(t, http.StatusCreated, "create pool")
	got := created.json(t)
	if got["repository_scale_up_limit"] != float64(2) || got["cost_per_runner_hour"] != 0.25 {
		t.Fatalf("create response: repository_scale_up_limit=%v cost_per_runner_hour=%v, want 2 and 0.25",
			got["repository_scale_up_limit"], got["cost_per_runner_hour"])
	}

	fetched := h.do(request{method: http.MethodGet, path: "/api/v1/pools/" + got["id"].(string), cookie: h.session(u)})
	fetched.mustStatus(t, http.StatusOK, "get pool")
	again := fetched.json(t)
	if again["repository_scale_up_limit"] != float64(2) || again["cost_per_runner_hour"] != 0.25 {
		t.Fatalf("GET: repository_scale_up_limit=%v cost_per_runner_hour=%v, want 2 and 0.25",
			again["repository_scale_up_limit"], again["cost_per_runner_hour"])
	}
}

// Prewarming pulls an image onto every host that matches the pool: minutes of
// somebody else's network and disk, started by one request. It was the only
// mutating operator route that wrote no audit row, against the security page's
// promise that every one of them does.
func TestPrewarmingAPoolIsAudited(t *testing.T) {
	h := newHarness(t)
	inst := h.installation()
	pool := h.pool(inst, "linux-x64")
	h.host("vm-1")
	token := h.token("ops", store.RoleOperator)

	resp := h.do(request{method: http.MethodPost, path: "/api/v1/pools/" + pool.ID + "/prewarm", token: token})
	resp.mustStatus(t, http.StatusAccepted, "prewarm")

	rows, _, err := h.st.ListAudit(h.ctx, store.AuditFilter{Actions: []string{"pool.prewarm"}}, store.Page{Limit: 10})
	if err != nil {
		t.Fatalf("ListAudit: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("audit rows = %d, want the prewarm recorded once", len(rows))
	}
	if rows[0].TargetID != pool.ID || rows[0].TargetKind != "pool" {
		t.Fatalf("audit row = %+v, want it to name the pool", rows[0])
	}
	// The image is what was pulled, and the row is the only record of which
	// one, since the pool's image can change afterwards.
	if !strings.Contains(rows[0].After, pool.Image) {
		t.Fatalf("audit detail = %q, want the image %q", rows[0].After, pool.Image)
	}
}

// A pool's own name is not a name that is taken.
//
// The wizard's review step is the same form whether it is creating a pool or
// editing one, and it calls /pools/validate either way. Without saying which
// pool it is editing, the name check compared the pool against every pool
// including itself: opening a pool, changing its image and pressing on was
// refused with "a pool called linux-x64 already exists" -- about itself -- and
// the only way to save any edit was to rename the pool as well.
func TestValidatingAnEditDoesNotClashWithThePoolBeingEdited(t *testing.T) {
	h := newHarness(t)
	inst := h.installation()
	h.host("vm-1")
	existing := h.pool(inst, "linux-x64")
	other := h.pool(inst, "arm64")
	u, _ := h.user("operator", store.RoleOperator)
	cookie := h.session(u)

	body := poolBody(inst.ID)
	body["image"] = "ghcr.io/eyupio/zoomies-runner:next"

	for _, tc := range []struct {
		name      string
		query     string
		wantValid bool
		wantErr   string
	}{
		{"editing itself", "?id=" + existing.ID, true, ""},
		{"creating another with the same name", "", false, "already exists"},
		{"editing a different pool into a taken name", "?id=" + other.ID, false, "already exists"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			res := h.do(request{method: http.MethodPost,
				path: "/api/v1/pools/validate" + tc.query, cookie: cookie, body: body})
			res.mustStatus(t, http.StatusOK, "validate")
			var verdict validatePoolResponse
			res.into(t, &verdict)
			if verdict.Valid != tc.wantValid {
				t.Fatalf("valid = %v, want %v (errors: %+v)", verdict.Valid, tc.wantValid, verdict.Errors)
			}
			if tc.wantErr == "" {
				return
			}
			var found bool
			for _, e := range verdict.Errors {
				if e.Field == "name" && strings.Contains(e.Message, tc.wantErr) {
					found = true
				}
			}
			if !found {
				t.Fatalf("no name error mentioning %q: %+v", tc.wantErr, verdict.Errors)
			}
		})
	}
}

// A pool that gives its jobs a daemon needs an image with a Docker client, and
// the stock runner image has none. Asking the operator to remember both halves
// was the mistake everybody made -- the daemon came up, the job died at its
// first docker step -- so the server makes the second half itself: the stock
// image becomes its Docker variant as the pool is saved, and the response
// shows it. What it must not do is undo an operator's own choice, or reverse
// itself when the daemon goes away again.
func TestAPoolThatGivesJobsADaemonIsSavedOnTheDockerImage(t *testing.T) {
	h := newHarness(t)
	inst := h.installation()
	h.host("vm-1")
	u, _ := h.user("operator", store.RoleOperator)
	cookie := h.session(u)

	// The stock image with a daemon: swapped, and the tag survives.
	body := poolBody(inst.ID)
	body["image"] = "ghcr.io/eyupio/zoomies-runner:latest"
	body["docker_mode"] = "dind"
	created := h.do(request{method: http.MethodPost, path: "/api/v1/pools", cookie: cookie, body: body})
	created.mustStatus(t, http.StatusCreated, "create")
	var pool poolResponse
	created.into(t, &pool)
	if pool.Image != "ghcr.io/eyupio/zoomies-runner-docker:latest" {
		t.Fatalf("image = %q, want the stock image's Docker variant under the same tag", pool.Image)
	}

	// Taking the daemon away does not take the client away: the swap is not
	// reversed, because the image still runs everything the stock one does,
	// and may be in use against a DOCKER_HOST of the pool's own.
	patched := h.do(request{method: http.MethodPatch, path: "/api/v1/pools/" + pool.ID, cookie: cookie,
		body: map[string]any{"docker_mode": "none"}})
	patched.mustStatus(t, http.StatusOK, "patch to none")
	patched.into(t, &pool)
	if pool.Image != "ghcr.io/eyupio/zoomies-runner-docker:latest" {
		t.Fatalf("image = %q after dropping the daemon, want it left alone", pool.Image)
	}

	// Putting the stock image back on a pool without a daemon stores it as
	// typed; asking for a daemon again swaps it again.
	patched = h.do(request{method: http.MethodPatch, path: "/api/v1/pools/" + pool.ID, cookie: cookie,
		body: map[string]any{"image": "ghcr.io/eyupio/zoomies-runner:latest"}})
	patched.mustStatus(t, http.StatusOK, "patch the stock image back")
	patched.into(t, &pool)
	if pool.Image != "ghcr.io/eyupio/zoomies-runner:latest" {
		t.Fatalf("image = %q on a pool with no daemon, want the stock image as typed", pool.Image)
	}
	patched = h.do(request{method: http.MethodPatch, path: "/api/v1/pools/" + pool.ID, cookie: cookie,
		body: map[string]any{"docker_mode": "host-socket"}})
	patched.mustStatus(t, http.StatusOK, "patch to host-socket")
	patched.into(t, &pool)
	if pool.Image != "ghcr.io/eyupio/zoomies-runner-docker:latest" {
		t.Fatalf("image = %q after asking for the host socket, want the Docker variant", pool.Image)
	}

	// "Correcting" the image back to the stock one on the edit page, with
	// the daemon still on, is the same request with the same answer.
	patched = h.do(request{method: http.MethodPatch, path: "/api/v1/pools/" + pool.ID, cookie: cookie,
		body: map[string]any{"image": "ghcr.io/eyupio/zoomies-runner:latest"}})
	patched.mustStatus(t, http.StatusOK, "patch the stock image onto a daemon pool")
	patched.into(t, &pool)
	if pool.Image != "ghcr.io/eyupio/zoomies-runner-docker:latest" {
		t.Fatalf("image = %q after typing the stock image onto a daemon pool, want the Docker variant", pool.Image)
	}

	// A pinned tag is a deliberate choice of one build, and the variant may
	// not exist for it, so it is kept exactly as typed.
	patched = h.do(request{method: http.MethodPatch, path: "/api/v1/pools/" + pool.ID, cookie: cookie,
		body: map[string]any{"image": "ghcr.io/eyupio/zoomies-runner:sha-b966fb6"}})
	patched.mustStatus(t, http.StatusOK, "patch a pinned stock tag")
	patched.into(t, &pool)
	if pool.Image != "ghcr.io/eyupio/zoomies-runner:sha-b966fb6" {
		t.Fatalf("image = %q, want a pinned tag left as typed", pool.Image)
	}

	// An image of the operator's own is theirs, daemon or not.
	patched = h.do(request{method: http.MethodPatch, path: "/api/v1/pools/" + pool.ID, cookie: cookie,
		body: map[string]any{"image": "registry.example.com/ci/runner:latest"}})
	patched.mustStatus(t, http.StatusOK, "patch an own image")
	patched.into(t, &pool)
	if pool.Image != "registry.example.com/ci/runner:latest" {
		t.Fatalf("image = %q, want an operator's own image left alone", pool.Image)
	}

	// The wizard's dry run is the same code path, and says which image the
	// pool would run, so the review step can show the pool the server will
	// make rather than the one that was typed.
	body = poolBody(inst.ID)
	body["name"] = "review"
	body["image"] = "ghcr.io/eyupio/zoomies-runner:latest"
	body["docker_mode"] = "dind"
	validate := h.do(request{method: http.MethodPost, path: "/api/v1/pools/validate", cookie: cookie, body: body})
	validate.mustStatus(t, http.StatusOK, "validate")
	var verdict validatePoolResponse
	validate.into(t, &verdict)
	if !verdict.Valid || verdict.Image != "ghcr.io/eyupio/zoomies-runner-docker:latest" {
		t.Fatalf("verdict = %+v, want a valid pool on the Docker variant", verdict)
	}
	if pools, err := h.st.ListPools(h.ctx); err != nil || len(pools) != 1 {
		t.Fatalf("the dry run created a pool: %v %v", pools, err)
	}

	// Every swap the server made is in the audit trail as an image change,
	// where an operator looking for why the pool's image is not what they
	// typed will find it.
	audit := h.do(request{method: http.MethodGet, path: "/api/v1/audit?target_kind=pool", cookie: cookie})
	audit.mustStatus(t, http.StatusOK, "audit")
	var events page[store.AuditEvent]
	audit.into(t, &events)
	found := false
	for _, e := range events.Items {
		if e.Action == "pool.create" && strings.Contains(e.After, "zoomies-runner-docker:latest") {
			found = true
		}
	}
	if !found {
		t.Fatalf("no pool.create audit row records the Docker variant; rows: %d", len(events.Items))
	}
}

// A pool that names a platform gets the matching runner image without anyone
// having to keep the two in step, and the API says which image that is.
func TestAPoolsPlatformPicksItsImage(t *testing.T) {
	h := newHarness(t)
	inst := h.installation()
	u, _ := h.user("operator", store.RoleOperator)
	cookie := h.session(u)

	body := poolBody(inst.ID)
	body["name"] = "zoomies-4vcpu-debian-12"
	body["labels"] = []string{"zoomies-4vcpu-debian-12"}
	delete(body, "image")
	body["platform"] = map[string]any{"os": "debian", "os_version": "12", "arch": "amd64"}

	res := h.do(request{method: http.MethodPost, path: "/api/v1/pools", cookie: cookie, body: body})
	res.mustStatus(t, http.StatusCreated, "create")
	var pool poolResponse
	res.into(t, &pool)

	if pool.Image != "" {
		t.Errorf("image = %q; a pool that names none should stay unpinned", pool.Image)
	}
	if want := "ghcr.io/eyupio/zoomies-runner:debian-12"; pool.EffectiveImage != want {
		t.Errorf("effective_image = %q, want %q", pool.EffectiveImage, want)
	}
	if pool.Platform.OS != "debian" || pool.Platform.OSVersion != "12" || pool.Platform.Arch != "amd64" {
		t.Errorf("platform = %+v", pool.Platform)
	}
}

// A pool with no platform and no image of its own falls back to the instance
// default, which is what every pool created before platforms existed does.
func TestAPoolWithNoPlatformFallsBackToTheInstanceDefault(t *testing.T) {
	h := newHarness(t)
	inst := h.installation()
	u, _ := h.user("operator", store.RoleOperator)
	cookie := h.session(u)

	body := poolBody(inst.ID)
	delete(body, "image")
	res := h.do(request{method: http.MethodPost, path: "/api/v1/pools", cookie: cookie, body: body})
	res.mustStatus(t, http.StatusCreated, "create")
	var pool poolResponse
	res.into(t, &pool)
	if pool.EffectiveImage != h.cfg.GitHub.RunnerImage {
		t.Errorf("effective_image = %q, want the instance default %q",
			pool.EffectiveImage, h.cfg.GitHub.RunnerImage)
	}
}

// A platform nothing is published for is refused at creation, where it can
// still be fixed, rather than at every create, where it cannot.
func TestAPoolCannotAskForAPlatformNothingPublishes(t *testing.T) {
	h := newHarness(t)
	inst := h.installation()
	u, _ := h.user("operator", store.RoleOperator)
	cookie := h.session(u)

	cases := []struct {
		name     string
		platform map[string]any
		field    string
		contains string
	}{
		{
			name:     "an operating system Zoomies does not know",
			platform: map[string]any{"os": "plan9"},
			field:    "platform.os",
			contains: "Ubuntu 24.04",
		},
		{
			name:     "a release nothing is published for",
			platform: map[string]any{"os": "ubuntu", "os_version": "20.04"},
			field:    "platform.os_version",
			contains: "no runner image is published",
		},
		{
			name:     "an architecture Zoomies does not run on",
			platform: map[string]any{"os": "ubuntu", "arch": "riscv64"},
			field:    "platform.arch",
			contains: "amd64 or arm64",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			req := poolBody(inst.ID)
			delete(req, "image")
			req["platform"] = c.platform
			res := h.do(request{method: http.MethodPost, path: "/api/v1/pools", cookie: cookie, body: req})
			res.mustStatus(t, http.StatusUnprocessableEntity, "create")
			body := string(res.body)
			if !strings.Contains(body, c.field) || !strings.Contains(body, c.contains) {
				t.Errorf("the error does not name %s or explain the fix: %s", c.field, body)
			}
		})
	}
}

// Naming an image is an explicit override: an operator who has built their own
// is not second-guessed about which platform it is.
func TestAnExplicitImageOverridesThePlatform(t *testing.T) {
	h := newHarness(t)
	inst := h.installation()
	u, _ := h.user("operator", store.RoleOperator)
	cookie := h.session(u)

	body := poolBody(inst.ID)
	body["image"] = "ghcr.io/acme/our-own-runner:v3"
	body["platform"] = map[string]any{"os": "ubuntu", "os_version": "20.04"}

	res := h.do(request{method: http.MethodPost, path: "/api/v1/pools", cookie: cookie, body: body})
	res.mustStatus(t, http.StatusCreated, "create")
	var pool poolResponse
	res.into(t, &pool)
	if pool.EffectiveImage != "ghcr.io/acme/our-own-runner:v3" {
		t.Errorf("effective_image = %q, want the image the operator named", pool.EffectiveImage)
	}
}

// The Hosts page has to be able to say what a machine is, not just what it is
// called, and to offer the name it would be given today.
func TestHostsReportWhatMachineTheyAre(t *testing.T) {
	h := newHarness(t)
	u, _ := h.user("viewer", store.RoleViewer)
	cookie := h.session(u)

	host := h.host("build01")
	host.Distro, host.OSVersion, host.CPUs, host.MemoryMB = "ubuntu", "24.04", 16, 32768
	if err := h.st.UpdateHost(h.ctx, host); err != nil {
		t.Fatalf("UpdateHost: %v", err)
	}

	res := h.do(request{method: http.MethodGet, path: "/api/v1/hosts/" + host.ID, cookie: cookie})
	res.mustStatus(t, http.StatusOK, "get host")
	var got hostResponse
	res.into(t, &got)

	if got.PlatformLabel != "Ubuntu 24.04, amd64" {
		t.Errorf("platform_label = %q", got.PlatformLabel)
	}
	if got.CanonicalName != "zoomies-16vcpu-32gb-ubuntu-2404-build01" {
		t.Errorf("canonical_name = %q", got.CanonicalName)
	}
	if got.CPUs != 16 || got.MemoryMB != 32768 {
		t.Errorf("size = %d vCPU / %d MB", got.CPUs, got.MemoryMB)
	}
}

// The wizard's review step must not promise hosts the scheduler will then
// refuse to place on: both go through controller.HostFit, which applies the
// scheduler's own placement rule, platform included.
func TestTheHostCountRespectsThePoolsPlatform(t *testing.T) {
	h := newHarness(t)
	inst := h.installation()
	u, _ := h.user("operator", store.RoleOperator)
	cookie := h.session(u)

	// One Ubuntu 24.04 amd64 host, which is what h.host builds plus a distro.
	host := h.host("build01")
	host.Distro, host.OSVersion = "ubuntu", "24.04"
	if err := h.st.UpdateHost(h.ctx, host); err != nil {
		t.Fatalf("UpdateHost: %v", err)
	}

	body := poolBody(inst.ID)
	delete(body, "image")

	// A pool that asks for nothing sees the host.
	res := h.do(request{method: http.MethodPost, path: "/api/v1/pools/validate", cookie: cookie, body: body})
	res.mustStatus(t, http.StatusOK, "validate")
	var verdict validatePoolResponse
	res.into(t, &verdict)
	if verdict.MatchingHosts != 1 {
		t.Fatalf("matching_hosts = %d, want 1", verdict.MatchingHosts)
	}

	// A pool that asks for Debian does not, and the warning says what to add.
	body["platform"] = map[string]any{"os": "debian", "os_version": "12"}
	res = h.do(request{method: http.MethodPost, path: "/api/v1/pools/validate", cookie: cookie, body: body})
	res.mustStatus(t, http.StatusOK, "validate")
	res.into(t, &verdict)
	if verdict.MatchingHosts != 0 {
		t.Errorf("matching_hosts = %d; the only host is Ubuntu", verdict.MatchingHosts)
	}
	found := false
	for _, w := range verdict.Warnings {
		if w.Code == "pool.no_matching_hosts" {
			found = true
			if !strings.Contains(w.Fix, "Debian 12") {
				t.Errorf("the fix does not name the machine to add: %q", w.Fix)
			}
		}
	}
	if !found {
		t.Errorf("no warning about there being no host: %+v", verdict.Warnings)
	}
}
