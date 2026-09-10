package api

import (
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/eyupio/zoomies/internal/cryptox"
	"github.com/eyupio/zoomies/internal/store"
	"github.com/eyupio/zoomies/internal/version"
)

// TestUserLifecycle covers the account management an admin does, including the
// refusal that stops an instance being locked out of itself.
func TestUserLifecycle(t *testing.T) {
	h := newHarness(t)
	admin, _ := h.user("root", store.RoleAdmin)
	cookie := h.session(admin)

	created := h.do(request{method: http.MethodPost, path: "/api/v1/users", cookie: cookie,
		body: map[string]any{"username": "Bob", "password": testPassword, "role": "operator", "email": "bob@example.com"}})
	created.mustStatus(t, http.StatusCreated, "create user")
	var bob userResponse
	created.into(t, &bob)
	if bob.Username != "bob" {
		t.Errorf("username = %q, want it lowercased", bob.Username)
	}
	if !bob.MustChangePassword {
		t.Error("an account created with somebody else's password should have to change it")
	}

	dup := h.do(request{method: http.MethodPost, path: "/api/v1/users", cookie: cookie,
		body: map[string]any{"username": "bob", "password": testPassword, "role": "viewer"}})
	dup.mustStatus(t, http.StatusConflict, "duplicate username")

	short := h.do(request{method: http.MethodPost, path: "/api/v1/users", cookie: cookie,
		body: map[string]any{"username": "carol", "password": "short", "role": "viewer"}})
	short.mustStatus(t, http.StatusUnprocessableEntity, "short password")

	patched := h.do(request{method: http.MethodPatch, path: "/api/v1/users/" + bob.ID, cookie: cookie,
		body: map[string]any{"role": "viewer", "display_name": "Bob"}})
	patched.mustStatus(t, http.StatusOK, "patch user")
	var updated userResponse
	patched.into(t, &updated)
	if updated.Role != store.RoleViewer || updated.DisplayName != "Bob" {
		t.Errorf("updated user = %+v", updated)
	}

	// The last administrator cannot be demoted, disabled or deleted.
	demote := h.do(request{method: http.MethodPatch, path: "/api/v1/users/" + admin.ID, cookie: cookie,
		body: map[string]any{"role": "viewer"}})
	demote.mustStatus(t, http.StatusConflict, "demote the last admin")
	if !strings.Contains(demote.errorMessage(t), "administrator") {
		t.Errorf("the refusal does not explain itself: %q", demote.errorMessage(t))
	}
	remove := h.do(request{method: http.MethodDelete, path: "/api/v1/users/" + admin.ID, cookie: cookie})
	remove.mustStatus(t, http.StatusConflict, "delete the last admin")

	reset := h.do(request{method: http.MethodPost, path: "/api/v1/users/" + bob.ID + "/password", cookie: cookie,
		body: map[string]any{"new_password": "a-brand-new-password"}})
	reset.mustStatus(t, http.StatusNoContent, "reset a password")

	deleted := h.do(request{method: http.MethodDelete, path: "/api/v1/users/" + bob.ID, cookie: cookie})
	deleted.mustStatus(t, http.StatusNoContent, "delete user")
	gone := h.do(request{method: http.MethodGet, path: "/api/v1/users/" + bob.ID, cookie: cookie})
	gone.mustStatus(t, http.StatusNotFound, "get a deleted user")
}

// TestDisablingAUserEndsItsSessionsAtTheAPI proves the teardown that actually
// runs in production. The auth service has SetUserDisabled, which deletes the
// sessions and is what the service-level test covers, but nothing calls it: the
// only way an account is disabled is this PATCH, which saves the row and then
// ends the sessions itself. Deleting that second step would break the property
// and break no test.
//
// A 401 on its own would not prove it, either. Authentication refuses a
// disabled account whatever its sessions look like, so the cookie stops
// working the moment the flag is set. What has to be shown is that the rows
// are gone -- because an account that is re-enabled must not find its old
// cookies waiting, and because a session left behind is a live credential for
// as long as it has not expired.
func TestDisablingAUserEndsItsSessionsAtTheAPI(t *testing.T) {
	h := newHarness(t)
	admin, _ := h.user("root", store.RoleAdmin)
	adminCookie := h.session(admin)
	// Two sessions, because the teardown is per-account and not per-cookie:
	// signing out the laptop is no use if the phone is still signed in.
	bob, laptop := h.user("bob", store.RoleOperator)
	phone := h.session(bob)

	for _, c := range []struct {
		name   string
		cookie string
	}{{"laptop", laptop}, {"phone", phone}} {
		probe := h.do(request{method: http.MethodGet, path: "/api/v1/auth/session", cookie: c.cookie})
		probe.mustStatus(t, http.StatusOK, "bob's "+c.name+" before being disabled")
	}

	disable := h.do(request{method: http.MethodPatch, path: "/api/v1/users/" + bob.ID,
		cookie: adminCookie, body: map[string]any{"disabled": true}})
	disable.mustStatus(t, http.StatusOK, "disable bob")

	// The rows themselves are gone, not merely refused. This is the assertion
	// the 401 cannot make.
	for _, c := range []struct {
		name   string
		cookie string
	}{{"laptop", laptop}, {"phone", phone}} {
		_, _, err := h.st.GetSessionByTokenHash(h.ctx, cryptox.HashToken(c.cookie))
		if !errors.Is(err, store.ErrNotFound) {
			t.Errorf("bob's %s session survived being disabled: %v", c.name, err)
		}
	}

	// And re-enabling the account does not bring them back, which is what an
	// operator disabling somebody for an hour is relying on.
	enable := h.do(request{method: http.MethodPatch, path: "/api/v1/users/" + bob.ID,
		cookie: adminCookie, body: map[string]any{"disabled": false}})
	enable.mustStatus(t, http.StatusOK, "re-enable bob")
	for _, c := range []struct {
		name   string
		cookie string
	}{{"laptop", laptop}, {"phone", phone}} {
		probe := h.do(request{method: http.MethodGet, path: "/api/v1/auth/session", cookie: c.cookie})
		probe.mustStatus(t, http.StatusUnauthorized, "bob's "+c.name+" after he was re-enabled")
	}

	// Nobody else was signed out: the teardown is scoped to the one account.
	stillIn := h.do(request{method: http.MethodGet, path: "/api/v1/auth/session", cookie: adminCookie})
	stillIn.mustStatus(t, http.StatusOK, "the admin who did the disabling")
}

// TestSettings covers what may be changed at runtime and what may not.
func TestSettings(t *testing.T) {
	h := newHarness(t)
	admin, _ := h.user("root", store.RoleAdmin)
	cookie := h.session(admin)

	get := h.do(request{method: http.MethodGet, path: "/api/v1/settings", cookie: cookie})
	get.mustStatus(t, http.StatusOK, "settings")
	var settings settingsResponse
	get.into(t, &settings)
	if settings.Version == "" || settings.DatabasePath == "" {
		t.Errorf("settings = %+v", settings)
	}
	if len(settings.RestartRequiredKeys) == 0 {
		t.Error("no restart-required keys, so the UI cannot tell which fields are read-only")
	}
	// The encryption key is reported as configured, never as a value.
	security, _ := settings.Config["security"].(map[string]any)
	if security["encryption_key_configured"] != true {
		t.Errorf("security = %v", security)
	}
	if _, leaked := security["encryption_key"]; leaked {
		t.Error("the settings response carries the encryption key")
	}

	patched := h.do(request{method: http.MethodPatch, path: "/api/v1/settings", cookie: cookie,
		body: map[string]any{"retention": map[string]any{"jobs": "48h"}, "log.level": "debug"}})
	patched.mustStatus(t, http.StatusOK, "patch settings")
	var after settingsResponse
	patched.into(t, &after)
	retention, _ := after.Config["retention"].(map[string]any)
	if retention["jobs"] != "48h0m0s" {
		t.Errorf("retention.jobs = %v, want 48h0m0s", retention["jobs"])
	}
	if h.ctrl.Config().Retention.Jobs.String() != "48h0m0s" {
		t.Errorf("the running configuration was not changed: %s", h.ctrl.Config().Retention.Jobs)
	}

	// A setting that needs a restart is refused with a message that says so.
	refused := h.do(request{method: http.MethodPatch, path: "/api/v1/settings", cookie: cookie,
		body: map[string]any{"server": map[string]any{"bind": "0.0.0.0:9000"}}})
	refused.mustStatus(t, http.StatusUnprocessableEntity, "patch a restart-only setting")
	var env errorEnvelope
	refused.into(t, &env)
	if len(env.Errors) == 0 || env.Errors[0].Field != "server.bind" {
		t.Fatalf("expected a field error on server.bind: %+v", env)
	}
	if !strings.Contains(env.Errors[0].Message, "restart") {
		t.Errorf("the message does not say a restart is needed: %q", env.Errors[0].Message)
	}
	if h.ctrl.Config().Server.Bind == "0.0.0.0:9000" {
		t.Error("a refused setting was applied anyway")
	}

	// So is an unparseable value.
	bad := h.do(request{method: http.MethodPatch, path: "/api/v1/settings", cookie: cookie,
		body: map[string]any{"retention.audit": "forever"}})
	bad.mustStatus(t, http.StatusUnprocessableEntity, "patch with a bad duration")

	// A request is refused as a whole: a good key sent beside a bad one is not
	// applied behind the operator's back, and nothing is audited.
	countAudit := func() int {
		res := h.do(request{method: http.MethodGet, path: "/api/v1/audit?action=settings.update", cookie: cookie})
		res.mustStatus(t, http.StatusOK, "audit")
		var page struct {
			Total int `json:"total"`
		}
		res.into(t, &page)
		return page.Total
	}
	audited := countAudit()
	mixed := h.do(request{method: http.MethodPatch, path: "/api/v1/settings", cookie: cookie,
		body: map[string]any{"retention.jobs": "1h", "log.level": "bogus"}})
	mixed.mustStatus(t, http.StatusUnprocessableEntity, "patch a good key beside a bad one")
	if h.ctrl.Config().Retention.Jobs.String() != "48h0m0s" {
		t.Errorf("retention.jobs = %s after a refused request, want it left at 48h0m0s", h.ctrl.Config().Retention.Jobs)
	}
	if h.ctrl.Config().Log.Level != "debug" {
		t.Errorf("log.level = %q after a refused request, want it left at debug", h.ctrl.Config().Log.Level)
	}
	if n := countAudit(); n != audited {
		t.Errorf("a refused request wrote %d audit rows", n-audited)
	}
}

// TestJoinTokenLifecycle covers minting the credential a new host enrols with.
func TestJoinTokenLifecycle(t *testing.T) {
	h := newHarness(t)
	admin, _ := h.user("root", store.RoleAdmin)
	cookie := h.session(admin)

	created := h.do(request{method: http.MethodPost, path: "/api/v1/join-tokens", cookie: cookie,
		body: map[string]any{"ttl": "1h", "capacity": 4, "labels": map[string]string{"zone": "eu"}}})
	created.mustStatus(t, http.StatusCreated, "create join token")
	var token createJoinTokenResponse
	created.into(t, &token)
	if !token.Usable || token.Capacity != 4 || token.Labels["zone"] != "eu" {
		t.Errorf("join token = %+v", token)
	}
	if !strings.Contains(token.Command, "--mode agent") {
		t.Errorf("the command is not the one-liner for a new host: %q", token.Command)
	}

	listed := h.do(request{method: http.MethodGet, path: "/api/v1/join-tokens", cookie: cookie})
	listed.mustStatus(t, http.StatusOK, "list join tokens")
	if strings.Contains(string(listed.body), token.Token) {
		t.Fatal("the join token list contains the secret")
	}

	revoked := h.do(request{method: http.MethodDelete, path: "/api/v1/join-tokens/" + token.ID, cookie: cookie})
	revoked.mustStatus(t, http.StatusNoContent, "revoke")
	again := h.do(request{method: http.MethodDelete, path: "/api/v1/join-tokens/" + token.ID, cookie: cookie})
	again.mustStatus(t, http.StatusNotFound, "revoke twice")

	bad := h.do(request{method: http.MethodPost, path: "/api/v1/join-tokens", cookie: cookie,
		body: map[string]any{"ttl": "soon"}})
	bad.mustStatus(t, http.StatusUnprocessableEntity, "create with a bad ttl")
}

// TestJoinTokenCanBeWatchedUntilAHostUsesIt is the contract behind the
// Add-a-host page: it polls one token and learns which host redeemed it, so
// the operator sees the machine arrive without leaving the page.
func TestJoinTokenCanBeWatchedUntilAHostUsesIt(t *testing.T) {
	h := newHarness(t)
	admin, _ := h.user("root", store.RoleAdmin)
	cookie := h.session(admin)

	created := h.do(request{method: http.MethodPost, path: "/api/v1/join-tokens", cookie: cookie,
		body: map[string]any{"ttl": "1h", "capacity": 0}})
	created.mustStatus(t, http.StatusCreated, "create join token")
	var minted createJoinTokenResponse
	created.into(t, &minted)

	before := h.do(request{method: http.MethodGet, path: "/api/v1/join-tokens/" + minted.ID, cookie: cookie})
	before.mustStatus(t, http.StatusOK, "get an unused token")
	var pending joinTokenResponse
	before.into(t, &pending)
	if !pending.Usable || pending.UsedAt != nil || pending.UsedByID != "" {
		t.Fatalf("an unused token reads as %+v", pending)
	}
	if strings.Contains(string(before.body), minted.Token) {
		t.Fatal("reading a join token back returned its secret")
	}

	join := h.do(request{method: http.MethodPost, path: "/api/v1/agent/join", body: map[string]any{
		"protocol_version": 1, "join_token": minted.Token, "name": "build-box-7",
		"capacity": 3, "os": "linux", "arch": "arm64", "version": "test",
		"backends": []map[string]any{{"kind": "docker", "available": true}},
	}})
	join.mustStatus(t, http.StatusOK, "agent join")
	var joined struct {
		HostID string `json:"host_id"`
	}
	join.into(t, &joined)

	after := h.do(request{method: http.MethodGet, path: "/api/v1/join-tokens/" + minted.ID, cookie: cookie})
	after.mustStatus(t, http.StatusOK, "get a spent token")
	var spent joinTokenResponse
	after.into(t, &spent)
	if spent.Usable || spent.UsedAt == nil {
		t.Errorf("a redeemed token still reads as unused: %+v", spent)
	}
	if spent.UsedByID != joined.HostID {
		t.Errorf("used_by_id = %q, want the host that joined, %q", spent.UsedByID, joined.HostID)
	}
	// Capacity 0 on the token means the agent's own number stands, which is
	// what "let the agent decide" has to mean for the page's default.
	host := h.do(request{method: http.MethodGet, path: "/api/v1/hosts/" + joined.HostID, cookie: cookie})
	host.mustStatus(t, http.StatusOK, "get the joined host")
	var view hostResponse
	host.into(t, &view)
	if view.Capacity != 3 {
		t.Errorf("host capacity = %d, want the agent's 3 when the token left it to the agent", view.Capacity)
	}

	missing := h.do(request{method: http.MethodGet, path: "/api/v1/join-tokens/join_nothing", cookie: cookie})
	missing.mustStatus(t, http.StatusNotFound, "get a token that never existed")
}

// TestJoinTokenCommandUsesTheAddressTheCallerGave covers the UI sending the
// address the browser reached the controller on, so the pasted command never
// carries a loopback URL or a placeholder.
func TestJoinTokenCommandUsesTheAddressTheCallerGave(t *testing.T) {
	h := newHarness(t)
	admin, _ := h.user("root", store.RoleAdmin)
	cookie := h.session(admin)

	created := h.do(request{method: http.MethodPost, path: "/api/v1/join-tokens", cookie: cookie,
		body: map[string]any{"controller_url": "https://zoomies.internal:8443/"}})
	created.mustStatus(t, http.StatusCreated, "create with a controller_url")
	var minted createJoinTokenResponse
	created.into(t, &minted)
	if !strings.Contains(minted.Command, "--controller 'https://zoomies.internal:8443' ") {
		t.Errorf("the command does not carry the given address, without its trailing slash: %q", minted.Command)
	}
	if strings.Contains(minted.Command, "<this-controller>") {
		t.Errorf("the command still carries the placeholder: %q", minted.Command)
	}

	for _, bad := range []string{"zoomies.internal", "ftp://zoomies.internal", "https://", "https://user:pw@zoomies.internal"} {
		resp := h.do(request{method: http.MethodPost, path: "/api/v1/join-tokens", cookie: cookie,
			body: map[string]any{"controller_url": bad}})
		resp.mustStatus(t, http.StatusUnprocessableEntity, "create with controller_url "+bad)
		if !strings.Contains(string(resp.body), `"controller_url"`) {
			t.Errorf("the refusal of %q does not name the field: %s", bad, resp.body)
		}
	}
}

// TestHostCordonAndDelete covers taking a machine out of the fleet safely.
func TestHostCordonAndDelete(t *testing.T) {
	h := newHarness(t)
	inst := h.installation()
	pool := h.pool(inst, "linux-x64")
	host := h.host("vm-1")
	h.runner(pool, host, store.RunnerIdle)

	operator, _ := h.user("operator", store.RoleOperator)
	admin, _ := h.user("root", store.RoleAdmin)

	cordon := h.do(request{method: http.MethodPost, path: "/api/v1/hosts/" + host.ID + "/cordon",
		cookie: h.session(operator), body: map[string]any{"cordoned": true}})
	cordon.mustStatus(t, http.StatusOK, "cordon")
	var out hostResponse
	cordon.into(t, &out)
	if !out.Cordoned {
		t.Error("the host is not cordoned")
	}
	if out.ActiveRunners != 1 {
		t.Errorf("cordoning changed the runner count: %+v", out)
	}

	capacity := h.do(request{method: http.MethodPatch, path: "/api/v1/hosts/" + host.ID,
		cookie: h.session(operator), body: map[string]any{"capacity": 8, "labels": map[string]string{"zone": "eu"}}})
	capacity.mustStatus(t, http.StatusOK, "patch host")
	var patched hostResponse
	capacity.into(t, &patched)
	if patched.Capacity != 8 || patched.Labels["zone"] != "eu" {
		t.Errorf("patched host = %+v", patched)
	}

	// A host with live runners is not deleted by accident.
	refused := h.do(request{method: http.MethodDelete, path: "/api/v1/hosts/" + host.ID, cookie: h.session(admin)})
	refused.mustStatus(t, http.StatusConflict, "delete a busy host")
	if !strings.Contains(refused.errorMessage(t), "force") {
		t.Errorf("the refusal does not say how to proceed: %q", refused.errorMessage(t))
	}

	forced := h.do(request{method: http.MethodDelete, path: "/api/v1/hosts/" + host.ID + "?force=true",
		cookie: h.session(admin)})
	forced.mustStatus(t, http.StatusNoContent, "forced delete")
}

// TestJobsListAndFacets covers the job history page.
func TestJobsListAndFacets(t *testing.T) {
	h := newHarness(t)
	inst := h.installation()
	pool := h.pool(inst, "linux-x64")
	h.job(pool, store.JobQueued)
	done := h.job(pool, store.JobCompleted)
	done.Conclusion = "success"
	if _, err := h.st.UpsertJob(h.ctx, done); err != nil {
		t.Fatalf("UpsertJob: %v", err)
	}
	viewer, _ := h.user("viewer", store.RoleViewer)
	cookie := h.session(viewer)

	list := h.do(request{method: http.MethodGet, path: "/api/v1/jobs", cookie: cookie})
	list.mustStatus(t, http.StatusOK, "list jobs")
	var jobs page[jobResponse]
	list.into(t, &jobs)
	if jobs.Total != 2 {
		t.Fatalf("total = %d, want 2", jobs.Total)
	}
	for _, j := range jobs.Items {
		if j.PoolName != pool.Name {
			t.Errorf("job %s does not name its pool: %+v", j.ID, j)
		}
	}

	filtered := h.do(request{method: http.MethodGet, path: "/api/v1/jobs?state=completed", cookie: cookie})
	var completed page[jobResponse]
	filtered.into(t, &completed)
	if completed.Total != 1 {
		t.Errorf("completed total = %d, want 1", completed.Total)
	}

	badState := h.do(request{method: http.MethodGet, path: "/api/v1/jobs?state=nope", cookie: cookie})
	badState.mustStatus(t, http.StatusBadRequest, "unknown job state")

	badSince := h.do(request{method: http.MethodGet, path: "/api/v1/jobs?since=yesterday", cookie: cookie})
	badSince.mustStatus(t, http.StatusBadRequest, "unparseable since")

	facets := h.do(request{method: http.MethodGet, path: "/api/v1/jobs/facets", cookie: cookie})
	facets.mustStatus(t, http.StatusOK, "facets")
	var f jobFacetsResponse
	facets.into(t, &f)
	if len(f.Repos) == 0 || f.Repos[0] != "acme/widgets" {
		t.Errorf("repos = %v", f.Repos)
	}
	if len(f.Conclusions) == 0 {
		t.Errorf("conclusions = %v, want the one that exists", f.Conclusions)
	}
}

// TestWebhookDeliveriesAndTest covers the two endpoints an operator uses when
// scaling has gone quiet.
func TestWebhookDeliveriesAndTest(t *testing.T) {
	h := newHarness(t)
	viewer, _ := h.user("viewer", store.RoleViewer)
	operator, _ := h.user("operator", store.RoleOperator)

	deliveries := h.do(request{method: http.MethodGet, path: "/api/v1/webhook-deliveries", cookie: h.session(viewer)})
	deliveries.mustStatus(t, http.StatusOK, "webhook deliveries")
	var out webhookDeliveriesResponse
	deliveries.into(t, &out)
	if out.LastReceivedAt != nil {
		t.Errorf("last_received_at = %v on an instance that has never had a delivery", out.LastReceivedAt)
	}
	if !strings.Contains(string(deliveries.body), `"last_received_at":null`) {
		t.Error("last_received_at is absent rather than null, so 'quiet' and 'broken' look the same")
	}

	badStatus := h.do(request{method: http.MethodGet, path: "/api/v1/webhook-deliveries?status=maybe",
		cookie: h.session(viewer)})
	badStatus.mustStatus(t, http.StatusBadRequest, "unknown delivery status")

	check := h.do(request{method: http.MethodPost, path: "/api/v1/webhook-test", cookie: h.session(operator)})
	check.mustStatus(t, http.StatusOK, "webhook test")
	var verdict webhookCheckResponse
	check.into(t, &verdict)
	if verdict.Message == "" {
		t.Error("the reachability check said nothing")
	}
	if !verdict.Reachable && verdict.Fix == "" {
		t.Error("the check reported a failure with no remedy")
	}
	if !verdict.PollingAvailable {
		t.Error("polling_available is false although github.poll_fallback is on")
	}
}

// TestAuditListAndFilters covers the page that answers "who did that?".
func TestAuditListAndFilters(t *testing.T) {
	h := newHarness(t)
	inst := h.installation()
	operator, _ := h.user("operator", store.RoleOperator)
	cookie := h.session(operator)

	create := h.do(request{method: http.MethodPost, path: "/api/v1/pools", cookie: cookie, body: poolBody(inst.ID)})
	create.mustStatus(t, http.StatusCreated, "create pool")

	audit := h.do(request{method: http.MethodGet, path: "/api/v1/audit", cookie: cookie})
	audit.mustStatus(t, http.StatusOK, "audit")
	var events page[store.AuditEvent]
	audit.into(t, &events)
	if events.Total == 0 {
		t.Fatal("the audit log is empty after a pool was created")
	}
	found := false
	for _, e := range events.Items {
		if e.Action == "pool.create" {
			found = true
			if e.ActorName != "operator" {
				t.Errorf("the row does not name the actor: %+v", e)
			}
			if e.After == "" {
				t.Error("the row has no after picture")
			}
		}
	}
	if !found {
		t.Fatal("no pool.create row")
	}

	actions := h.do(request{method: http.MethodGet, path: "/api/v1/audit/actions", cookie: cookie})
	actions.mustStatus(t, http.StatusOK, "audit actions")
	var names list[string]
	actions.into(t, &names)
	if len(names.Items) == 0 {
		t.Fatal("no distinct action names")
	}

	filtered := h.do(request{method: http.MethodGet, path: "/api/v1/audit?action=pool.create", cookie: cookie})
	var only page[store.AuditEvent]
	filtered.into(t, &only)
	for _, e := range only.Items {
		if e.Action != "pool.create" {
			t.Errorf("the action filter let %q through", e.Action)
		}
	}
}

// TestStatsCarryTheFleetsOwnFigures is what lets the Overview default to this
// fleet's numbers.
//
// GitHub reports every job in an installed repository, so on an organisation
// that also uses hosted runners the unscoped counts are mostly somebody else's,
// and a queue depth built from them answers "why is my fleet slow?" with a
// number nobody here can act on. Both scopes travel in one payload rather than
// being chosen by a query parameter, because the same numbers arrive over the
// event stream -- one frame for every viewer -- so a per-request scope would be
// right until the next frame overwrote it.
func TestStatsCarryTheFleetsOwnFigures(t *testing.T) {
	h := newHarness(t)
	inst := h.installation()
	pool := h.pool(inst, "linux-x64")
	h.job(pool, store.JobQueued)

	// Somebody else's: reported by GitHub, claimed by no pool, run by no
	// runner here, and already under way so it cannot be counted as the
	// unclaimed-queued case the fleet does own.
	started := time.Now().Add(-time.Minute)
	if _, err := h.st.UpsertJob(h.ctx, &store.Job{
		GitHubJobID: 987654, GitHubRunID: 2, Repo: "acme/widgets", Workflow: "ci",
		JobName: "hosted", State: store.JobInProgress,
		QueuedAt: time.Now().Add(-2 * time.Minute), StartedAt: &started,
	}); err != nil {
		t.Fatalf("seeding a hosted job: %v", err)
	}

	viewer, _ := h.user("viewer", store.RoleViewer)
	resp := h.do(request{method: http.MethodGet, path: "/api/v1/stats", cookie: h.session(viewer)})
	resp.mustStatus(t, http.StatusOK, "stats")
	body := resp.json(t)

	if body["running_jobs"] != float64(1) {
		t.Errorf("running_jobs = %v, want the hosted job counted in the unscoped figure", body["running_jobs"])
	}
	fleet, ok := body["fleet"].(map[string]any)
	if !ok {
		t.Fatalf("the stats payload carries no fleet figures: %s", truncate(resp.body))
	}
	if fleet["running_jobs"] != float64(0) {
		t.Errorf("fleet.running_jobs = %v, want 0: the only running job is somebody else's", fleet["running_jobs"])
	}
	if fleet["queued_jobs"] != float64(1) {
		t.Errorf("fleet.queued_jobs = %v, want the pool's own queued job", fleet["queued_jobs"])
	}
}

// TestOverviewEndpoints covers the four reads the Overview makes.
func TestOverviewEndpoints(t *testing.T) {
	h := newHarness(t)
	inst := h.installation()
	pool := h.pool(inst, "linux-x64")
	host := h.host("vm-1")
	h.runner(pool, host, store.RunnerIdle)
	h.job(pool, store.JobQueued)
	viewer, _ := h.user("viewer", store.RoleViewer)
	cookie := h.session(viewer)

	stats := h.do(request{method: http.MethodGet, path: "/api/v1/stats?window=24h", cookie: cookie})
	stats.mustStatus(t, http.StatusOK, "stats")
	body := stats.json(t)
	if body["window"] != "24h0m0s" {
		t.Errorf("window = %v, want the requested one echoed back", body["window"])
	}
	runners, _ := body["runners"].(map[string]any)
	if runners["idle"] != float64(1) {
		t.Errorf("runners = %v", runners)
	}

	badWindow := h.do(request{method: http.MethodGet, path: "/api/v1/stats?window=soon", cookie: cookie})
	badWindow.mustStatus(t, http.StatusBadRequest, "unparseable window")

	samples := h.do(request{method: http.MethodGet, path: "/api/v1/samples?window=1h", cookie: cookie})
	samples.mustStatus(t, http.StatusOK, "samples")
	if !strings.Contains(string(samples.body), `"items"`) {
		t.Errorf("samples = %s", truncate(samples.body))
	}

	problems := h.do(request{method: http.MethodGet, path: "/api/v1/problems", cookie: cookie})
	problems.mustStatus(t, http.StatusOK, "problems")

	scaling := h.do(request{method: http.MethodGet, path: "/api/v1/scaling-events?limit=5", cookie: cookie})
	scaling.mustStatus(t, http.StatusOK, "scaling events")
}

// The Hosts page and the pool wizard both answer "why is this host not taking
// work?" out of backend_info, so the API has to hand back the agent's whole
// probe -- the backends that failed included -- rather than a list of the ones
// that worked.
func TestHostResponseCarriesTheAgentsProbe(t *testing.T) {
	h := newHarness(t)
	host := h.host("vm-1")
	host.Backends = store.StringSlice{"process"}
	host.BackendInfo = store.HostBackends{
		{Kind: store.BackendDocker, Detail: "cannot connect to /var/run/docker.sock: permission denied"},
		{Kind: store.BackendProcess, Available: true, Version: "1.2.3", Endpoint: "exec", SupportsDinD: false},
	}
	if err := h.st.UpdateHost(h.ctx, host); err != nil {
		t.Fatalf("UpdateHost: %v", err)
	}
	viewer, _ := h.user("viewer", store.RoleViewer)

	resp := h.do(request{method: http.MethodGet, path: "/api/v1/hosts/" + host.ID, cookie: h.session(viewer)})
	resp.mustStatus(t, http.StatusOK, "get host")
	var out hostResponse
	resp.into(t, &out)

	if len(out.BackendInfo) != 2 {
		t.Fatalf("backend_info = %+v, want every backend the agent probed", out.BackendInfo)
	}
	var docker backendInfoResponse
	for _, b := range out.BackendInfo {
		if b.Kind == store.BackendDocker {
			docker = b
		}
	}
	if docker.Available {
		t.Errorf("docker is reported available although the agent could not reach it: %+v", docker)
	}
	if !strings.Contains(docker.Detail, "permission denied") {
		t.Errorf("detail = %q, want the agent's own explanation", docker.Detail)
	}
}

// A host that joined before probes were stored has no probe to show. Its
// available kinds are still rendered, and nothing is invented about the
// backends it never reported on.
func TestHostResponseFallsBackToTheKindsAlone(t *testing.T) {
	h := newHarness(t)
	host := h.host("vm-1")
	viewer, _ := h.user("viewer", store.RoleViewer)

	resp := h.do(request{method: http.MethodGet, path: "/api/v1/hosts/" + host.ID, cookie: h.session(viewer)})
	resp.mustStatus(t, http.StatusOK, "get host")
	var out hostResponse
	resp.into(t, &out)

	if len(out.BackendInfo) != 1 || out.BackendInfo[0].Kind != store.BackendDocker || !out.BackendInfo[0].Available {
		t.Fatalf("backend_info = %+v, want the one kind the host is known to have", out.BackendInfo)
	}
	if out.BackendInfo[0].Detail != "" {
		t.Errorf("detail = %q, want nothing invented", out.BackendInfo[0].Detail)
	}
}

// A viewer must not read admin-only documents out of the audit log.
//
// audit.read is a viewer action; users.read, tokens.read, joins.read and
// settings.read are all admin ones. Returning every row's before/after exactly
// as stored made the audit log a way around that split: a viewer who was
// refused GET /users could read a user's email, display name and OIDC subject
// out of the row that recorded the change to them.
//
// The row itself is not the problem and is not withheld. Who did what, to which
// thing, and when is what a viewer is given the audit log for; it is the
// document describing the thing that is admin-only.
func TestTheAuditLogDoesNotLetAViewerReadAdminOnlyDocuments(t *testing.T) {
	h := newHarness(t)
	_, adminCookie := h.user("root@example.com", store.RoleAdmin)

	created := h.do(request{method: http.MethodPost, path: "/api/v1/users", cookie: adminCookie,
		body: map[string]any{"username": "dana", "password": testPassword, "role": "operator",
			"email": "dana@example.com"}})
	created.mustStatus(t, http.StatusCreated, "create user")

	// The admin who can call GET /users still sees the whole row.
	asAdmin := h.do(request{method: http.MethodGet, path: "/api/v1/audit?target_kind=user", cookie: adminCookie})
	asAdmin.mustStatus(t, http.StatusOK, "audit as admin")
	if !strings.Contains(string(asAdmin.body), "dana@example.com") {
		t.Fatalf("an admin lost the document they are allowed to read:\n%s", string(asAdmin.body))
	}

	_, viewerCookie := h.user("watcher@example.com", store.RoleViewer)
	asViewer := h.do(request{method: http.MethodGet, path: "/api/v1/audit?target_kind=user", cookie: viewerCookie})
	asViewer.mustStatus(t, http.StatusOK, "audit as viewer")
	if strings.Contains(string(asViewer.body), "dana@example.com") {
		t.Errorf("a viewer read a user's email out of the audit log:\n%s", string(asViewer.body))
	}
	// The row is still there, or the viewer has lost the audit trail itself.
	if !strings.Contains(string(asViewer.body), "user.create") {
		t.Errorf("the viewer lost the row as well as the document:\n%s", string(asViewer.body))
	}

	// And the direct route is refused, which is the promise being kept.
	direct := h.do(request{method: http.MethodGet, path: "/api/v1/users", cookie: viewerCookie})
	if direct.status != http.StatusForbidden {
		t.Errorf("GET /users as viewer = %d, want 403", direct.status)
	}
}

// The join command has to install an agent that matches this controller.
//
// Unpinned, the installer resolves "latest", which is the newest published
// release rather than this build. A fleet whose controller had moved past that
// release enrolled every new host on an older agent, and the host arrived
// reading "Different build" for good: re-running the command changed nothing,
// because the version it asked for was the version already there. Where the
// older agent predates the machine measurement code it also arrives permanently
// "Size unknown", which no argument to the command could fix.
//
// Main is a published channel too. CI keeps a rolling dev binary beside the
// dev images, so a main-sha-* controller can enrol a host from the same channel
// instead of silently falling back to the newest release.
func TestJoinCommandPinsTheControllersOwnRelease(t *testing.T) {
	h := newHarness(t)
	admin, _ := h.user("root", store.RoleAdmin)
	cookie := h.session(admin)

	mint := func(t *testing.T) createJoinTokenResponse {
		t.Helper()
		created := h.do(request{method: http.MethodPost, path: "/api/v1/join-tokens", cookie: cookie,
			body: map[string]any{"controller_url": "https://zoomies.internal:8443"}})
		created.mustStatus(t, http.StatusCreated, "create")
		var minted createJoinTokenResponse
		created.into(t, &minted)
		return minted
	}

	was := version.Version
	t.Cleanup(func() { version.Version = was })

	for _, tc := range []struct {
		name    string
		stamped string
		want    string
		tag     string
	}{
		{"a release tag", "1.0.0", "--version v1.0.0", "v1.0.0"},
		{"a tag with a v", "v1.0.0", "--version v1.0.0", "v1.0.0"},
		{"a prerelease", "0.2-beta", "--version v0.2-beta", "v0.2-beta"},
		{"main", "main-sha-117bc18", "--version dev", "dev"},
		{"an unstamped development build", "dev", "--version dev", "dev"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			version.Version = tc.stamped
			minted := mint(t)
			if !strings.Contains(minted.Command, tc.want) {
				t.Errorf("a controller stamped %q handed out a command without %q, so the host it enrols "+
					"will not match it:\n%s", tc.stamped, tc.want, minted.Command)
			}
			if minted.VersionNote != "" {
				t.Errorf("a pinned command still carried a note: %q", minted.VersionNote)
			}
			if minted.InstallTag != tc.tag {
				t.Errorf("install_tag = %q, want %q", minted.InstallTag, tc.tag)
			}
			if !strings.Contains(minted.ControllerVersion, tc.stamped) {
				t.Errorf("controller_version = %q, want it to name %q", minted.ControllerVersion, tc.stamped)
			}
		})
	}

	for _, stamped := range []string{"v0.2-beta-276-g394254b", "some-fork-build"} {
		t.Run("unreleased "+stamped, func(t *testing.T) {
			version.Version = stamped
			minted := mint(t)
			// No pin: there is no asset with this version, and a pin that 404s
			// is worse than an agent that merely differs.
			if strings.Contains(minted.Command, "--version") {
				t.Errorf("a controller stamped %q pinned a version that can never be downloaded:\n%s",
					stamped, minted.Command)
			}
			if minted.InstallTag != "" {
				t.Errorf("an unpublished build advertised install tag %q", minted.InstallTag)
			}
			// But it must not be silent about it.
			if minted.VersionNote == "" {
				t.Errorf("a controller stamped %q gave no reason for handing out an unpinned command; "+
					"the operator finds out when the host reads \"Different build\"", stamped)
			}
			if !strings.Contains(minted.VersionNote, stamped) {
				t.Errorf("the note does not name the build it is about: %q", minted.VersionNote)
			}
		})
	}
}
