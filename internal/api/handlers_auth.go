package api

import (
	"errors"
	"net/http"
	"strings"

	"github.com/eyupio/zoomies/internal/auth"
	"github.com/eyupio/zoomies/internal/cryptox"
	"github.com/eyupio/zoomies/internal/store"
)

// identityResponse is the caller, as the UI's session store holds it.
type identityResponse struct {
	Kind               string     `json:"kind"`
	ID                 string     `json:"id"`
	Name               string     `json:"name"`
	Role               store.Role `json:"role"`
	Scopes             []string   `json:"scopes"`
	MustChangePassword bool       `json:"must_change_password"`
}

func newIdentityResponse(id *auth.Identity, mustChange bool) identityResponse {
	scopes := id.Scopes
	if scopes == nil {
		scopes = []string{}
	}
	return identityResponse{
		Kind: id.Kind, ID: id.ID, Name: id.Name, Role: id.Role,
		Scopes: scopes, MustChangePassword: mustChange,
	}
}

// ---------------------------------------------------------------------------
// Bootstrap
// ---------------------------------------------------------------------------

type bootstrapRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
	Email    string `json:"email"`
	// SetupToken is printed in the controller's log while no account exists.
	SetupToken string `json:"setup_token"`
}

// handleBootstrap creates the first administrator.
//
// It is unauthenticated because on a fresh install there is nobody to
// authenticate as, and it closes permanently once any account exists -- the
// auth service checks that under a mutex.
//
// "No account exists yet" is not by itself a safe condition, though: it is one
// a stranger can satisfy too, and on a controller published to the internet the
// first person to load this page would otherwise become its administrator. So
// the request also has to carry the setup token this process printed at
// startup, which proves the caller can read the controller's log. Whoever
// deployed it can; a passer-by cannot.
func (s *Server) handleBootstrap(w http.ResponseWriter, r *http.Request) {
	var req bootstrapRequest
	if !decode(w, r, &req) {
		return
	}

	var fields []fieldError
	if strings.TrimSpace(req.Username) == "" {
		fields = append(fields, fieldError{"username", "the first administrator needs a name to sign in with"})
	}
	if err := auth.CheckPassword(req.Password); err != nil {
		fields = append(fields, fieldError{"password", err.Error()})
	}
	if strings.TrimSpace(req.SetupToken) == "" {
		fields = append(fields, fieldError{"setup_token", "paste the setup token from the controller's log; it is on a line beginning \"setup token\""})
	}
	if len(fields) > 0 {
		unprocessable(w, "the first administrator could not be created", fields)
		return
	}

	u, err := s.auth.CreateFirstAdminWithSetupToken(r.Context(), req.Username, req.Password, req.SetupToken)
	if err != nil {
		switch {
		case errors.Is(err, auth.ErrAlreadyBootstrapped):
			conflict(w, err.Error())
		case errors.Is(err, auth.ErrBadSetupToken):
			s.logger(r).Warn("a bootstrap attempt carried the wrong setup token", "ip", ClientIP(r.Context()))
			unprocessable(w, err.Error(), []fieldError{{"setup_token", err.Error()}})
		default:
			unprocessable(w, err.Error(), nil)
		}
		return
	}

	// The address is stored separately from the account itself so that the
	// admin can be created without an email and add one later.
	if email := strings.TrimSpace(req.Email); email != "" {
		u.Email = email
		if err := s.auth.UpdateUser(r.Context(), u); err != nil {
			s.logger(r).Warn("could not save the first administrator's email address", "error", err)
		}
	}

	id := &auth.Identity{Kind: auth.KindUser, ID: u.ID, Name: u.Username, Role: u.Role, IP: ClientIP(r.Context())}
	s.auth.Auditor().Auth(r.Context(), id, "auth.bootstrap", map[string]any{
		"username": u.Username, "role": u.Role,
	})

	token, err := s.auth.NewSession(r.Context(), u, ClientIP(r.Context()), r.UserAgent())
	if err != nil {
		// The account exists, which is the important half. Say so rather than
		// leaving the operator wondering whether to try again.
		s.logger(r).Error("created the first administrator but could not start a session", "error", err)
		writeJSON(w, http.StatusCreated, newIdentityResponse(id, u.MustChangePassword))
		return
	}
	s.setSessionCookie(w, token)
	writeJSON(w, http.StatusCreated, newIdentityResponse(id, u.MustChangePassword))
}

// ---------------------------------------------------------------------------
// Password login
// ---------------------------------------------------------------------------

type loginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

// handleLogin signs a person in with a password.
//
// A wrong password and an unknown username are the same 401 with the same
// message and, thanks to the auth service's dummy verification, very nearly the
// same timing: telling an attacker which usernames exist is a gift for nothing
// in return.
func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	var req loginRequest
	if !decode(w, r, &req) {
		return
	}
	ip := ClientIP(r.Context())

	u, token, err := s.auth.Login(r.Context(), req.Username, req.Password, ip, r.UserAgent())
	if err != nil {
		// The auth service writes the audit row, because what may be recorded
		// about a username nobody recognises is its decision, not this layer's.
		s.auth.AuditLoginFailure(r.Context(), req.Username, ip, err)
		switch {
		case errors.Is(err, auth.ErrRateLimited):
			rateLimited(w, err.Error(), 0)
		case errors.Is(err, auth.ErrInvalidCredentials),
			errors.Is(err, auth.ErrAccountDisabled),
			errors.Is(err, auth.ErrSSOOnly):
			unauthorized(w, err.Error())
		default:
			s.internal(w, r, "signing in", err)
		}
		return
	}

	s.setSessionCookie(w, token)
	id := &auth.Identity{Kind: auth.KindUser, ID: u.ID, Name: u.Username, Role: u.Role, IP: ip}
	s.auth.Auditor().Auth(r.Context(), id, "auth.login", map[string]any{"role": u.Role})
	writeJSON(w, http.StatusOK, newIdentityResponse(id, u.MustChangePassword))
}

// handleLogout ends the session behind the cookie and clears it.
func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie(SessionCookie); err == nil {
		if err := s.auth.Logout(r.Context(), c.Value); err != nil {
			s.logger(r).Warn("could not delete a session on sign-out", "error", err)
		}
	}
	s.clearSessionCookie(w)
	s.auth.Auditor().Auth(r.Context(), Identity(r.Context()), "auth.logout", nil)
	noContent(w)
}

// handleSession returns who the caller is. The UI calls it on boot to decide
// between the app and the login page.
func (s *Server) handleSession(w http.ResponseWriter, r *http.Request) {
	id := Identity(r.Context())
	must := false
	if id.Kind == auth.KindUser && id.ID != "" {
		if u, err := s.ctrl.Store().GetUser(r.Context(), id.ID); err == nil {
			must = u.MustChangePassword
		}
	}
	writeJSON(w, http.StatusOK, newIdentityResponse(id, must))
}

type changePasswordRequest struct {
	OldPassword string `json:"old_password"`
	NewPassword string `json:"new_password"`
}

// handleChangePassword changes the caller's own password.
//
// Every other session for the account is ended by the auth service, so a stolen
// cookie does not survive the change. This browser gets a fresh one, because
// asking someone to sign in again immediately after choosing a new password
// reads as a failure.
func (s *Server) handleChangePassword(w http.ResponseWriter, r *http.Request) {
	id := Identity(r.Context())
	if id.Kind != auth.KindUser || id.ID == "" {
		forbidden(w, "only a signed-in account can change its own password; an API token has none to change")
		return
	}

	var req changePasswordRequest
	if !decode(w, r, &req) {
		return
	}
	if err := auth.CheckPassword(req.NewPassword); err != nil {
		unprocessable(w, "that password cannot be used", []fieldError{{"new_password", err.Error()}})
		return
	}

	if err := s.auth.ChangePassword(r.Context(), id.ID, req.OldPassword, req.NewPassword); err != nil {
		switch {
		case errors.Is(err, store.ErrNotFound):
			notFound(w, "this account no longer exists")
		case errors.Is(err, auth.ErrSSOOnly):
			unprocessable(w, err.Error(), []fieldError{{"new_password", err.Error()}})
		default:
			// A wrong current password is the common case and is the caller's
			// to fix, not an internal failure.
			unprocessable(w, err.Error(), []fieldError{{"old_password", err.Error()}})
		}
		return
	}

	s.auth.Auditor().Auth(r.Context(), id, "auth.password_changed", nil)

	u, err := s.ctrl.Store().GetUser(r.Context(), id.ID)
	if err == nil {
		if token, terr := s.auth.NewSession(r.Context(), u, ClientIP(r.Context()), r.UserAgent()); terr == nil {
			s.setSessionCookie(w, token)
		} else {
			s.logger(r).Warn("could not issue a new session after a password change", "error", terr)
		}
	}
	noContent(w)
}

// ---------------------------------------------------------------------------
// Single sign-on
// ---------------------------------------------------------------------------

// handleOIDCStart sends the browser to the identity provider.
//
// The state is remembered twice: once by the provider, which holds the nonce
// that goes with it, and once in a cookie on this browser. The cookie is what
// ties the handshake to the person who began it -- without it, a state minted
// by an attacker's own sign-in is one this controller would happily accept from
// anybody's browser, which is a login CSRF: the victim ends up signed in as the
// attacker, and everything they then do happens in the attacker's account.
func (s *Server) handleOIDCStart(w http.ResponseWriter, r *http.Request) {
	if !s.oidc.Enabled() {
		s.ssoUnavailable(w)
		return
	}
	authURL, state, err := s.oidc.Start()
	if err != nil {
		s.internal(w, r, "starting the single sign-on handshake", err)
		return
	}
	s.setOIDCStateCookie(w, state)
	http.Redirect(w, r, authURL, http.StatusFound)
}

// handleOIDCCallback finishes the handshake and drops the browser back into the
// app.
//
// Failures redirect to the login page with a message rather than rendering an
// error document: whoever is looking at this is a person in a browser who was
// trying to sign in, and the login page is where they can try again.
func (s *Server) handleOIDCCallback(w http.ResponseWriter, r *http.Request) {
	if !s.oidc.Enabled() {
		s.ssoUnavailable(w)
		return
	}
	q := r.URL.Query()
	if e := q.Get("error"); e != "" {
		desc := q.Get("error_description")
		if desc == "" {
			desc = e
		}
		s.clearOIDCStateCookie(w)
		s.redirectToLogin(w, r, "the identity provider refused the sign-in: "+desc)
		return
	}

	// The state has to be the one this browser was sent away with. Checking it
	// before anything else means a callback aimed at somebody else's browser is
	// refused without ever spending the state or exchanging the code.
	state := q.Get("state")
	expected := oidcStateCookie(r)
	s.clearOIDCStateCookie(w)
	if expected == "" || state == "" || !cryptox.ConstantTimeEqual(expected, state) {
		s.logger(r).Warn("a single sign-on callback did not match the browser that started it",
			"has_cookie", expected != "", "has_state", state != "")
		s.redirectToLogin(w, r, "this sign-in did not start in this browser, so it cannot be completed here; start again from the login page")
		return
	}

	claims, err := s.oidc.Complete(r.Context(), state, q.Get("code"))
	if err != nil {
		s.logger(r).Warn("a single sign-on callback could not be completed", "error", err)
		s.redirectToLogin(w, r, err.Error())
		return
	}

	u, err := s.oidc.EnsureUser(r.Context(), s.ctrl.Store(), claims, s.oidc.AllowSignup())
	if err != nil {
		s.logger(r).Warn("a single sign-on login matched no account", "subject", claims.Subject, "error", err)
		s.redirectToLogin(w, r, err.Error())
		return
	}
	if u.Disabled {
		s.redirectToLogin(w, r, auth.ErrAccountDisabled.Error())
		return
	}

	ip := ClientIP(r.Context())
	token, err := s.auth.NewSession(r.Context(), u, ip, r.UserAgent())
	if err != nil {
		s.internal(w, r, "starting a session after single sign-on", err)
		return
	}
	s.setSessionCookie(w, token)
	if err := s.ctrl.Store().TouchLogin(r.Context(), u.ID, s.auth.Now()); err != nil {
		s.logger(r).Warn("could not record a single sign-on login", "user", u.Username, "error", err)
	}
	s.auth.Auditor().Auth(r.Context(),
		&auth.Identity{Kind: auth.KindUser, ID: u.ID, Name: u.Username, Role: u.Role, IP: ip},
		"auth.login", map[string]any{"method": "oidc", "role": u.Role})

	http.Redirect(w, r, "/", http.StatusFound)
}

func (s *Server) ssoUnavailable(w http.ResponseWriter) {
	msg := "single sign-on is not configured on this controller; sign in with a username and password"
	if s.oidcErr != nil {
		msg = "single sign-on is configured but could not be set up: " + s.oidcErr.Error()
	}
	writeError(w, http.StatusServiceUnavailable, errorEnvelope{Error: errorBody{
		Code: codeInternal, Message: msg,
	}})
}

// redirectToLogin sends a failed sign-in back to the login page with the reason
// in the query string, which is where the UI renders it.
func (s *Server) redirectToLogin(w http.ResponseWriter, r *http.Request, reason string) {
	http.Redirect(w, r, "/login?error="+urlQueryEscape(reason), http.StatusFound)
}
