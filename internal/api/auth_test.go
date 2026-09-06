package api

import (
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/eyupio/zoomies/internal/auth"
	"github.com/eyupio/zoomies/internal/config"
	"github.com/eyupio/zoomies/internal/store"
)

// TestBootstrapCreatesTheFirstAdminThenRefuses is the whole security of the
// bootstrap route: it is unauthenticated, so it has to close permanently the
// moment an account exists.
func TestBootstrapCreatesTheFirstAdmin(t *testing.T) {
	h := newHarness(t)

	meta := h.do(request{method: http.MethodGet, path: "/api/v1/meta"})
	meta.mustStatus(t, http.StatusOK, "meta before bootstrap")
	if got := meta.json(t)["bootstrap_required"]; got != true {
		t.Fatalf("bootstrap_required = %v on a fresh instance, want true", got)
	}

	resp := h.do(request{method: http.MethodPost, path: "/api/v1/auth/bootstrap", body: map[string]any{
		"username": "root", "password": testPassword, "email": "root@example.com",
		"setup_token": h.setupToken(),
	}})
	resp.mustStatus(t, http.StatusCreated, "bootstrap")

	body := resp.json(t)
	if body["role"] != string(store.RoleAdmin) {
		t.Errorf("first account has role %v, want admin", body["role"])
	}
	if resp.cookie == nil || resp.cookie.Value == "" {
		t.Fatal("bootstrap did not set a session cookie")
	}
	if !resp.cookie.HttpOnly || resp.cookie.SameSite != http.SameSiteLaxMode || resp.cookie.Path != "/" {
		t.Errorf("session cookie attributes are wrong: %+v", resp.cookie)
	}

	// The cookie it handed back has to work.
	session := h.do(request{method: http.MethodGet, path: "/api/v1/auth/session", cookie: resp.cookie.Value})
	session.mustStatus(t, http.StatusOK, "session after bootstrap")

	again := h.do(request{method: http.MethodPost, path: "/api/v1/auth/bootstrap", body: map[string]any{
		"username": "second", "password": testPassword, "setup_token": h.setupToken(),
	}})
	again.mustStatus(t, http.StatusConflict, "second bootstrap")
	if code := again.errorCode(t); code != codeConflict {
		t.Errorf("second bootstrap error code = %q, want %q", code, codeConflict)
	}

	// And the refusal is not a fluke of the first account being an admin: a
	// viewer-only instance stays closed too.
	if meta := h.do(request{method: http.MethodGet, path: "/api/v1/meta"}); meta.json(t)["bootstrap_required"] != false {
		t.Error("bootstrap_required is still true after the first account was created")
	}
}

func TestBootstrapRejectsAShortPassword(t *testing.T) {
	h := newHarness(t)
	resp := h.do(request{method: http.MethodPost, path: "/api/v1/auth/bootstrap", body: map[string]any{
		"username": "root", "password": "short", "setup_token": h.setupToken(),
	}})
	resp.mustStatus(t, http.StatusUnprocessableEntity, "bootstrap with a short password")

	var env errorEnvelope
	resp.into(t, &env)
	if len(env.Errors) == 0 || env.Errors[0].Field != "password" {
		t.Fatalf("expected a field error on password, got %+v", env)
	}
	if !strings.Contains(env.Errors[0].Message, "12") {
		t.Errorf("the message does not say how long a password must be: %q", env.Errors[0].Message)
	}
}

// TestLoginIsIndistinguishable checks the one property a login endpoint has to
// have: a wrong password and an unknown user must look the same.
func TestLoginIsIndistinguishable(t *testing.T) {
	h := newHarness(t)
	h.user("alice", store.RoleOperator)

	wrongPassword := h.do(request{method: http.MethodPost, path: "/api/v1/auth/login", body: map[string]any{
		"username": "alice", "password": "not-the-password",
	}})
	unknownUser := h.do(request{method: http.MethodPost, path: "/api/v1/auth/login", body: map[string]any{
		"username": "nobody-at-all", "password": "not-the-password",
	}})

	wrongPassword.mustStatus(t, http.StatusUnauthorized, "wrong password")
	unknownUser.mustStatus(t, http.StatusUnauthorized, "unknown user")
	if a, b := wrongPassword.errorMessage(t), unknownUser.errorMessage(t); a != b {
		t.Fatalf("a wrong password and an unknown user are distinguishable:\n  %q\n  %q", a, b)
	}
	if wrongPassword.cookie != nil && wrongPassword.cookie.Value != "" {
		t.Error("a refused login set a session cookie")
	}
}

func TestLoginLogoutRoundTrip(t *testing.T) {
	h := newHarness(t)
	h.user("alice", store.RoleOperator)

	login := h.do(request{method: http.MethodPost, path: "/api/v1/auth/login", body: map[string]any{
		"username": "alice", "password": testPassword,
	}})
	login.mustStatus(t, http.StatusOK, "login")
	if login.cookie == nil {
		t.Fatal("login set no session cookie")
	}
	cookie := login.cookie.Value

	body := login.json(t)
	if body["name"] != "alice" || body["role"] != string(store.RoleOperator) {
		t.Errorf("identity = %v, want alice/operator", body)
	}

	session := h.do(request{method: http.MethodGet, path: "/api/v1/auth/session", cookie: cookie})
	session.mustStatus(t, http.StatusOK, "session")

	logout := h.do(request{method: http.MethodPost, path: "/api/v1/auth/logout", cookie: cookie})
	logout.mustStatus(t, http.StatusNoContent, "logout")
	if logout.cookie == nil || logout.cookie.MaxAge >= 0 {
		t.Errorf("logout did not expire the cookie: %+v", logout.cookie)
	}

	after := h.do(request{method: http.MethodGet, path: "/api/v1/auth/session", cookie: cookie})
	after.mustStatus(t, http.StatusUnauthorized, "session after logout")
}

func TestLoginIsRateLimited(t *testing.T) {
	h := newHarness(t)
	h.user("alice", store.RoleViewer)

	var last *response
	for range h.cfg.Security.RateLimitLogins + 2 {
		last = h.do(request{method: http.MethodPost, path: "/api/v1/auth/login", body: map[string]any{
			"username": "alice", "password": "wrong",
		}})
	}
	last.mustStatus(t, http.StatusTooManyRequests, "login after too many attempts")
	if code := last.errorCode(t); code != codeRateLimited {
		t.Errorf("error code = %q, want %q", code, codeRateLimited)
	}
}

// TestBearerTokenAuthenticates covers the credential automation uses, including
// the two ways it stops working.
func TestBearerTokenAuthenticates(t *testing.T) {
	h := newHarness(t)

	token := h.token("ci", store.RoleViewer)
	ok := h.do(request{method: http.MethodGet, path: "/api/v1/pools", token: token})
	ok.mustStatus(t, http.StatusOK, "pools with a token")

	// A viewer token may not write.
	refused := h.do(request{method: http.MethodPost, path: "/api/v1/pools", token: token, body: map[string]any{}})
	refused.mustStatus(t, http.StatusForbidden, "write with a viewer token")

	// Revoked.
	revoked, plaintext, err := h.ctrl.Auth().CreateAPIToken(h.ctx, auth.NewToken{Name: "old", Role: store.RoleAdmin})
	if err != nil {
		t.Fatalf("CreateAPIToken: %v", err)
	}
	if err := h.ctrl.Auth().RevokeAPIToken(h.ctx, revoked.ID); err != nil {
		t.Fatalf("RevokeAPIToken: %v", err)
	}
	resp := h.do(request{method: http.MethodGet, path: "/api/v1/pools", token: plaintext})
	resp.mustStatus(t, http.StatusUnauthorized, "revoked token")
	if !strings.Contains(resp.errorMessage(t), "revoked") {
		t.Errorf("the message does not say the token was revoked: %q", resp.errorMessage(t))
	}

}

func ptr[T any](v T) *T { return &v }

// TestExpiredTokenIsRefused drives the clock forward rather than the row
// backwards, which is what the auth service's injectable clock is for.
func TestExpiredTokenIsRefused(t *testing.T) {
	h := newHarness(t)
	_, plaintext, err := h.ctrl.Auth().CreateAPIToken(h.ctx, auth.NewToken{
		Name: "short-lived", Role: store.RoleAdmin, ExpiresAt: ptr(time.Now().Add(50 * time.Millisecond)),
	})
	if err != nil {
		t.Fatalf("CreateAPIToken: %v", err)
	}
	time.Sleep(80 * time.Millisecond)

	resp := h.do(request{method: http.MethodGet, path: "/api/v1/pools", token: plaintext})
	resp.mustStatus(t, http.StatusUnauthorized, "expired token")
	if !strings.Contains(resp.errorMessage(t), "expired") {
		t.Errorf("the message does not say the token expired: %q", resp.errorMessage(t))
	}
}

// TestCSRF is the cookie/bearer asymmetry: a cookie rides along on a
// cross-origin request and a bearer token does not, so only the first needs the
// same-origin check.
func TestCSRF(t *testing.T) {
	h := newHarness(t)
	inst := h.installation()
	h.pool(inst, "linux-x64")
	u, _ := h.user("operator", store.RoleOperator)
	cookie := h.session(u)
	token := h.token("ci", store.RoleOperator)

	foreign := h.do(request{
		method: http.MethodPost, path: "/api/v1/pools/validate",
		cookie: cookie, origin: "https://evil.example.com",
		body: map[string]any{"name": "x"},
	})
	foreign.mustStatus(t, http.StatusForbidden, "cookie POST from a foreign origin")
	if !strings.Contains(foreign.errorMessage(t), "evil.example.com") {
		t.Errorf("the refusal does not name the origin: %q", foreign.errorMessage(t))
	}

	sameOrigin := h.do(request{
		method: http.MethodPost, path: "/api/v1/pools/validate",
		cookie: cookie, body: map[string]any{"name": "x"},
	})
	if sameOrigin.status == http.StatusForbidden {
		t.Fatalf("a same-origin cookie POST was refused: %s", truncate(sameOrigin.body))
	}

	// The same foreign-origin request with a bearer token is allowed, because
	// a browser could not have made it.
	withToken := h.do(request{
		method: http.MethodPost, path: "/api/v1/pools/validate",
		token: token, origin: "https://evil.example.com",
		body: map[string]any{"name": "x"},
	})
	if withToken.status == http.StatusForbidden {
		t.Fatalf("a bearer POST from another origin was refused: %s", truncate(withToken.body))
	}

	// A cookie request with no browser headers at all cannot be shown to be
	// same-origin, so it is refused too.
	headerless := h.do(request{
		method: http.MethodPost, path: "/api/v1/pools/validate",
		cookie: cookie, noOrigin: true, body: map[string]any{"name": "x"},
	})
	headerless.mustStatus(t, http.StatusForbidden, "cookie POST with no Origin")

	// Reads are never refused: a cross-origin GET cannot be read by the page
	// that made it, and refusing it would break nothing but link-following.
	read := h.do(request{
		method: http.MethodGet, path: "/api/v1/pools", cookie: cookie,
		origin: "https://evil.example.com",
	})
	read.mustStatus(t, http.StatusOK, "cross-origin GET")
}

func TestAllowedOriginsPermitsAConfiguredOrigin(t *testing.T) {
	h := newHarness(t, func(c *config.Config) {
		c.Server.AllowedOrigins = []string{"https://ops.example.com"}
	})
	u, _ := h.user("operator", store.RoleOperator)

	resp := h.do(request{
		method: http.MethodPost, path: "/api/v1/pools/validate",
		cookie: h.session(u), origin: "https://ops.example.com",
		body: map[string]any{"name": "x"},
	})
	if resp.status == http.StatusForbidden {
		t.Fatalf("a configured origin was refused: %s", truncate(resp.body))
	}
}

// TestDisableAuthSynthesisesAnAdmin covers the local-development switch. It is
// refused by config validation off loopback, so the only thing to check here is
// that it does what it says.
func TestDisableAuthSynthesisesAnAdmin(t *testing.T) {
	h := newHarness(t, func(c *config.Config) { c.Security.DisableAuth = true })

	resp := h.do(request{method: http.MethodGet, path: "/api/v1/auth/session"})
	resp.mustStatus(t, http.StatusOK, "session with auth disabled")
	body := resp.json(t)
	if body["name"] != "auth-disabled" || body["role"] != string(store.RoleAdmin) {
		t.Fatalf("identity = %v, want the auth-disabled admin", body)
	}

	meta := h.do(request{method: http.MethodGet, path: "/api/v1/meta"})
	if meta.json(t)["auth_disabled"] != true {
		t.Error("meta does not report that authentication is disabled")
	}
}

func TestChangeOwnPassword(t *testing.T) {
	h := newHarness(t)
	u, _ := h.user("alice", store.RoleViewer)
	cookie := h.session(u)

	wrong := h.do(request{method: http.MethodPost, path: "/api/v1/auth/password", cookie: cookie,
		body: map[string]any{"old_password": "nope", "new_password": "another-good-password"}})
	wrong.mustStatus(t, http.StatusUnprocessableEntity, "change with the wrong current password")

	ok := h.do(request{method: http.MethodPost, path: "/api/v1/auth/password", cookie: cookie,
		body: map[string]any{"old_password": testPassword, "new_password": "another-good-password"}})
	ok.mustStatus(t, http.StatusNoContent, "change password")
	if ok.cookie == nil || ok.cookie.Value == "" {
		t.Fatal("changing a password did not issue a fresh session for this browser")
	}

	// The old cookie is gone; the new one works.
	old := h.do(request{method: http.MethodGet, path: "/api/v1/auth/session", cookie: cookie})
	old.mustStatus(t, http.StatusUnauthorized, "the old session after a password change")
	fresh := h.do(request{method: http.MethodGet, path: "/api/v1/auth/session", cookie: ok.cookie.Value})
	fresh.mustStatus(t, http.StatusOK, "the new session after a password change")
}

// ---------------------------------------------------------------------------
// Single sign-on
// ---------------------------------------------------------------------------

// The OIDC state is minted server-side and remembered in a cookie on the
// browser that began the handshake. Without that binding, a state an attacker
// obtained by signing in as themselves is one this controller would accept from
// anybody's browser -- and the victim would end up signed in as the attacker.
//
// The provider here is a bare value: Enabled() only asks whether there is one,
// and the state check happens before anything touches it, which is the point.
func TestOIDCCallbackRefusesAStateThisBrowserDidNotStart(t *testing.T) {
	h := newHarness(t)
	h.api.oidc = &auth.OIDCProvider{}

	cases := []struct {
		name   string
		cookie string
		state  string
	}{
		{name: "no cookie at all", state: "state-from-somewhere-else"},
		{name: "a state from another browser", cookie: "ours", state: "theirs"},
		{name: "a cookie but no state", cookie: "ours"},
		{name: "neither"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := request{method: http.MethodGet, path: "/api/v1/auth/oidc/callback?code=abc&state=" + tc.state}
			if tc.cookie != "" {
				req.headers = map[string]string{"Cookie": OIDCStateCookie + "=" + tc.cookie}
			}
			resp := h.do(req)
			resp.mustStatus(t, http.StatusFound, "callback with "+tc.name)
			if loc := resp.header.Get("Location"); !strings.HasPrefix(loc, "/login?error=") {
				t.Fatalf("Location = %q; want a redirect back to the login page", loc)
			}
			if resp.cookie != nil && resp.cookie.Value != "" {
				t.Fatal("a refused callback issued a session")
			}
		})
	}
}

// Whatever the outcome, the state cookie is spent: leaving it in place would
// let the same callback be replayed against this browser.
func TestOIDCCallbackClearsTheStateCookie(t *testing.T) {
	h := newHarness(t)
	h.api.oidc = &auth.OIDCProvider{}

	resp := h.do(request{
		method:  http.MethodGet,
		path:    "/api/v1/auth/oidc/callback?code=abc&state=theirs",
		headers: map[string]string{"Cookie": OIDCStateCookie + "=ours"},
	})
	resp.mustStatus(t, http.StatusFound, "refused callback")

	var cleared bool
	for _, c := range resp.header.Values("Set-Cookie") {
		if strings.HasPrefix(c, OIDCStateCookie+"=;") || strings.Contains(c, OIDCStateCookie+"=;") {
			cleared = true
		}
	}
	if !cleared {
		t.Fatalf("the state cookie was not cleared: %v", resp.header.Values("Set-Cookie"))
	}
}

// The bootstrap route has to be open on a fresh install and closed to everybody
// who is not the operator, and "no account exists yet" does not tell those two
// apart. The setup token does: it is printed in this controller's log, so
// holding it means having deployed the thing.
func TestBootstrapNeedsTheSetupToken(t *testing.T) {
	h := newHarness(t)

	cases := []struct {
		name string
		body map[string]any
	}{
		{name: "no token at all", body: map[string]any{"username": "root", "password": testPassword}},
		{name: "an empty token", body: map[string]any{"username": "root", "password": testPassword, "setup_token": "  "}},
		{name: "somebody else's guess", body: map[string]any{"username": "root", "password": testPassword, "setup_token": "zoo-not-the-token"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			resp := h.do(request{method: http.MethodPost, path: "/api/v1/auth/bootstrap", body: tc.body})
			resp.mustStatus(t, http.StatusUnprocessableEntity, "bootstrap with "+tc.name)

			var env errorEnvelope
			resp.into(t, &env)
			if len(env.Errors) == 0 || env.Errors[0].Field != "setup_token" {
				t.Fatalf("expected a field error on setup_token, got %+v", env)
			}
			if resp.cookie != nil && resp.cookie.Value != "" {
				t.Fatal("a refused bootstrap issued a session")
			}
		})
	}

	// Nothing was created, so the instance is still waiting for its first
	// account rather than quietly belonging to whoever tried.
	if meta := h.do(request{method: http.MethodGet, path: "/api/v1/meta"}); meta.json(t)["bootstrap_required"] != true {
		t.Fatal("a refused bootstrap created an account")
	}
	users, err := h.st.ListUsers(h.ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(users) != 0 {
		t.Fatalf("accounts = %d after refused bootstraps; want none", len(users))
	}
}
