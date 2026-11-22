package auth

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/eyupio/zoomies/internal/config"
	"github.com/eyupio/zoomies/internal/cryptox"
	"github.com/eyupio/zoomies/internal/events"
	"github.com/eyupio/zoomies/internal/store"
)

var testPolicy = OAuthPolicy{Issuer: "https://zoomies.test", OpenRegistration: true}

const verifier = "a-verifier-that-is-long-enough-to-be-accepted-by-rfc-7636-0123456789"

// A native client listens wherever it could get a port, so a loopback
// redirect matches whatever the port; everything else is exact, because
// "close enough" is how an authorisation code ends up somewhere it should not.
func TestRedirectMatching(t *testing.T) {
	for _, tc := range []struct {
		registered, requested string
		want                  bool
	}{
		{"https://claude.ai/api/mcp/auth_callback", "https://claude.ai/api/mcp/auth_callback", true},
		{"https://claude.ai/api/mcp/auth_callback", "https://claude.ai/api/mcp/auth_callback/", false},
		{"https://claude.ai/api/mcp/auth_callback", "https://claude.ai:8443/api/mcp/auth_callback", false},
		{"https://claude.ai/api/mcp/auth_callback", "https://evil.example/api/mcp/auth_callback", false},
		{"http://localhost/callback", "http://localhost:3118/callback", true},
		{"http://127.0.0.1/callback", "http://127.0.0.1:50000/callback", true},
		{"http://[::1]/callback", "http://[::1]:50000/callback", true},
		{"http://localhost/callback", "http://127.0.0.1:3118/callback", false},
		{"http://localhost/callback", "http://localhost:3118/other", false},
		{"http://localhost/callback", "http://localhost:3118/callback?x=1", false},
		{"http://localhost/callback", "https://localhost:3118/callback", false},
	} {
		if got := redirectMatches(tc.registered, tc.requested); got != tc.want {
			t.Errorf("redirectMatches(%q, %q) = %v, want %v", tc.registered, tc.requested, got, tc.want)
		}
	}
}

func TestRedirectURIsMustBeHTTPSOrLoopback(t *testing.T) {
	for _, tc := range []struct {
		uri string
		ok  bool
	}{
		{"https://claude.ai/api/mcp/auth_callback", true},
		{"http://localhost:8080/callback", true},
		{"http://127.0.0.1/callback", true},
		{"http://zoomies.example/callback", false},
		{"claude://callback", false},
		{"https://claude.ai/cb#frag", false},
		{"https://user@claude.ai/cb", false},
		{"/relative", false},
	} {
		_, err := checkRedirectURIs([]string{tc.uri})
		if (err == nil) != tc.ok {
			t.Errorf("%q: err=%v, want ok=%v", tc.uri, err, tc.ok)
		}
	}
}

func TestPKCEIsS256AndChecksTheVerifierShape(t *testing.T) {
	challenge := PKCEChallenge(verifier)
	if !validChallenge(challenge) || !verifyPKCE(verifier, challenge) {
		t.Fatal("a verifier must match its own S256 challenge")
	}
	for _, v := range []string{verifier + "x", "short", strings.Repeat("a", 129), strings.Repeat("a", 42) + " "} {
		if verifyPKCE(v, challenge) {
			t.Errorf("%q must not verify", v)
		}
	}
}

// An absent resource means the MCP endpoint; the case of the scheme and host
// and a trailing slash are forgiven, as the specification asks; anything
// else is a different audience.
func TestResourceIndicator(t *testing.T) {
	for _, tc := range []struct {
		in string
		ok bool
	}{
		{"", true},
		{"https://zoomies.test/mcp", true},
		{"HTTPS://Zoomies.Test/mcp/", true},
		{"https://zoomies.test", false},
		{"https://zoomies.test/mcp#x", false},
		{"https://other.test/mcp", false},
	} {
		got, err := CheckResource(testPolicy, tc.in)
		if (err == nil) != tc.ok {
			t.Errorf("%q: err=%v, want ok=%v", tc.in, err, tc.ok)
		}
		if err == nil && got != "https://zoomies.test/mcp" {
			t.Errorf("%q must canonicalise to the endpoint, got %q", tc.in, got)
		}
	}
}

// Claude Code names itself by the URL of a document it publishes. The
// document is fetched, must name itself, and becomes a client that can be
// listed and revoked; one that claims another's URL is refused.
func TestClientIDMetadataDocuments(t *testing.T) {
	const id = "https://claude.ai/oauth/claude-code-client-metadata"
	docs := map[string]string{
		id:                             `{"client_id":"` + id + `","client_name":"Claude Code","redirect_uris":["http://localhost/callback","http://127.0.0.1/callback"],"token_endpoint_auth_method":"none"}`,
		"https://impostor.example/doc": `{"client_id":"` + id + `","client_name":"Claude Code","redirect_uris":["https://impostor.example/cb"]}`,
		"https://secret.example/doc":   `{"client_id":"https://secret.example/doc","redirect_uris":["https://secret.example/cb"],"token_endpoint_auth_method":"private_key_jwt"}`,
	}
	fetches := 0
	cfg := config.Default()
	c := newClock()
	st, err := store.Open(t.Context(), store.Options{Path: ":memory:", Now: c.Now})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	s := New(st, cfg, events.New(), WithClock(c.Now), WithClientMetadataFetcher(func(_ context.Context, u string) ([]byte, error) {
		fetches++
		if d, ok := docs[u]; ok {
			return []byte(d), nil
		}
		return nil, errors.New("it answered 404 Not Found")
	}))
	ctx := t.Context()

	begin := func(clientID, redirect string) (*store.OAuthRequest, error) {
		return s.BeginAuthorization(ctx, testPolicy, AuthorizeParams{
			ClientID: clientID, RedirectURI: redirect, ResponseType: "code",
			CodeChallenge: PKCEChallenge(verifier), CodeChallengeMethod: "S256",
		})
	}
	if _, err := begin(id, "http://localhost:3118/callback"); err != nil {
		t.Fatalf("Claude Code's document must be accepted: %v", err)
	}
	cl, err := st.GetOAuthClientByClientID(ctx, id)
	if err != nil || cl.Kind != store.OAuthClientMetadata || cl.Name != "Claude Code" {
		t.Fatalf("the document must become a listed client, got %+v %v", cl, err)
	}
	if _, err := begin(id, "http://127.0.0.1:4000/callback"); err != nil || fetches != 1 {
		t.Errorf("a second sign-in within the hour must use what was fetched, fetches=%d err=%v", fetches, err)
	}
	for _, bad := range []string{"https://impostor.example/doc", "https://secret.example/doc", "https://missing.example/doc"} {
		if _, err := begin(bad, "https://impostor.example/cb"); err == nil {
			t.Errorf("%s must be refused", bad)
		}
	}
	if _, err := s.store.RevokeOAuthClient(ctx, cl.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := begin(id, "http://localhost:3118/callback"); err == nil {
		t.Error("a revoked metadata client must stay refused")
	}
}

func TestMetadataClientIDsAreHTTPSURLsWithAPath(t *testing.T) {
	for id, want := range map[string]bool{
		"https://claude.ai/oauth/claude-code-client-metadata": true,
		"https://claude.ai":                    false,
		"https://claude.ai/":                   false,
		"http://claude.ai/doc":                 false,
		"https://claude.ai/doc#x":              false,
		"https://user@claude.ai/doc":           false,
		"oac_abcdefghijklm":                    false,
		"https://claude.ai/a/../metadata.json": false,
	} {
		if got := IsMetadataClientID(id); got != want {
			t.Errorf("IsMetadataClientID(%q) = %v, want %v", id, got, want)
		}
	}
}

// The fetch is the one request this process makes because an unauthenticated
// caller asked; it must not reach this machine or its network.
func TestMetadataFetchRefusesPrivateAddresses(t *testing.T) {
	for addr, public := range map[string]bool{
		"127.0.0.1": false, "10.1.2.3": false, "192.168.1.1": false, "169.254.169.254": false,
		"100.64.0.1": false, "::1": false, "fd00::1": false, "0.0.0.0": false,
		// The ranges a shorter list once let through: the rest of "this
		// network", benchmarking, reserved, and IPv6 addresses that carry a
		// private IPv4 destination -- the metadata service behind NAT64 is
		// the metadata service.
		"0.1.2.3": false, "198.18.0.1": false, "240.0.0.1": false, "255.255.255.255": false,
		"64:ff9b::a9fe:a9fe": false, "2002:7f00:1::": false, "::ffff:10.0.0.1": false, "fec0::1": false,
		"1.1.1.1": true, "160.79.104.1": true, "2606:4700::1111": true, "64:ff9b::101:101": true,
	} {
		err := dialTarget(addr)
		if (err == nil) != public {
			t.Errorf("dialTarget(%s) = %v, want public=%v", addr, err, public)
		}
	}
	if _, err := fetchPublic(t.Context(), "https://127.0.0.1/doc", false); err == nil || !strings.Contains(err.Error(), "not a public address") {
		t.Errorf("a loopback document must be refused before it is dialled, got %v", err)
	}
}

// Codes and access tokens expire; an access token for a disabled account
// stops working without anybody revoking it.
func TestMCPCredentialsExpire(t *testing.T) {
	s, st, c := newService(t)
	ctx := t.Context()
	u := addUser(t, st, "olive", store.RoleOperator, nil)
	id := &Identity{Kind: KindUser, ID: u.ID, Name: u.Username, Role: u.Role}
	cl, err := s.RegisterClient(ctx, testPolicy, ClientRegistration{RedirectURIs: []string{ClaudeCallback}}, "203.0.113.1")
	if err != nil {
		t.Fatal(err)
	}
	approve := func() string {
		r, err := s.BeginAuthorization(ctx, testPolicy, AuthorizeParams{ClientID: cl.ClientID, RedirectURI: ClaudeCallback,
			ResponseType: "code", CodeChallenge: PKCEChallenge(verifier), CodeChallengeMethod: "S256"})
		if err != nil {
			t.Fatal(err)
		}
		d, err := s.ApproveAuthorization(ctx, testPolicy, id, r.ID, store.RoleOperator)
		if err != nil {
			t.Fatal(err)
		}
		back, _ := url.Parse(d.RedirectTo)
		return back.Query().Get("code")
	}
	exchange := func(code string) (*TokenResponse, error) {
		return s.Token(ctx, testPolicy, ClientCredentials{ClientID: cl.ClientID}, TokenRequest{
			GrantType: "authorization_code", Code: code, CodeVerifier: verifier, RedirectURI: ClaudeCallback}, "203.0.113.1")
	}

	stale := approve()
	c.Advance(mcpCodeTTL + time.Second)
	if _, err := exchange(stale); err == nil {
		t.Error("an expired code must be refused")
	}

	pair, err := exchange(approve())
	if err != nil {
		t.Fatal(err)
	}
	if got, err := s.AuthenticateMCP(ctx, testPolicy, pair.AccessToken, ""); err != nil || got.Kind != KindConnection || got.Role != store.RoleOperator || got.UserID != u.ID {
		t.Fatalf("a fresh access token resolves to the connection, got %+v %v", got, err)
	}
	c.Advance(mcpAccessTTL + time.Second)
	if _, err := s.AuthenticateMCP(ctx, testPolicy, pair.AccessToken, ""); !errors.Is(err, ErrMCPTokenExpired) {
		t.Errorf("an expired access token must say so, got %v", err)
	}
	next, err := s.Token(ctx, testPolicy, ClientCredentials{ClientID: cl.ClientID}, TokenRequest{GrantType: "refresh_token", RefreshToken: pair.RefreshToken}, "")
	if err != nil {
		t.Fatalf("the refresh token outlives the access token: %v", err)
	}
	if err := s.SetUserDisabled(ctx, u.ID, true); err != nil {
		t.Fatal(err)
	}
	if _, err := s.AuthenticateMCP(ctx, testPolicy, next.AccessToken, ""); !errors.Is(err, ErrAccountDisabled) {
		t.Errorf("a disabled account's connection must stop, got %v", err)
	}
	if _, err := s.Token(ctx, testPolicy, ClientCredentials{ClientID: cl.ClientID}, TokenRequest{GrantType: "refresh_token", RefreshToken: next.RefreshToken}, ""); err == nil {
		t.Error("a disabled account's connection must not be refreshed")
	}

	// The retention pass clears what has expired and the self-registered
	// clients that never finished signing in.
	orphan, _ := s.RegisterClient(ctx, testPolicy, ClientRegistration{ClientName: "an orphan", RedirectURIs: []string{ClaudeCallback}}, "203.0.113.2")
	c.Advance(mcpRefreshTTL + 48*time.Hour)
	if _, err := st.PruneOAuth(ctx, c.Now()); err != nil {
		t.Fatal(err)
	}
	if _, err := st.GetOAuthClient(ctx, orphan.ID); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("a client that never completed a sign-in must be pruned, got %v", err)
	}
	if _, err := st.GetOAuthClient(ctx, cl.ID); err != nil {
		t.Errorf("a client that was used must be kept: %v", err)
	}
}

// TestPruningDropsAbandonedMetadataClientsButNeverARevokedOne is about the
// clients an anonymous caller can make.
//
// An unauthenticated GET /oauth/authorize with any https client_id whose
// document echoes itself back inserts a metadata client, and the retention pass
// only ever removed self-registered ones, so those rows and the administrator's
// client list grew without bound. A pruned metadata client costs nothing -- it
// is fetched and created again on its next sign-in -- with one exception: a
// revoked one is remembered only by its row, so pruning it would let the next
// authorise fetch the URL and create a fresh, unrevoked client, quietly undoing
// an administrator's decision.
func TestPruningDropsAbandonedMetadataClientsButNeverARevokedOne(t *testing.T) {
	s, st, c := newService(t)
	ctx := t.Context()

	add := func(clientID string) *store.OAuthClient {
		t.Helper()
		cl := &store.OAuthClient{ClientID: clientID, Kind: store.OAuthClientMetadata, Name: "app",
			RedirectURIs: []string{"https://app.example/callback"}}
		if err := st.CreateOAuthClient(ctx, cl); err != nil {
			t.Fatalf("CreateOAuthClient(%s): %v", clientID, err)
		}
		return cl
	}
	abandoned := add("https://abandoned.example/client")
	revoked := add("https://revoked.example/client")
	used := add("https://used.example/client")
	if _, err := st.RevokeOAuthClient(ctx, revoked.ID); err != nil {
		t.Fatal(err)
	}
	if err := st.TouchOAuthClient(ctx, used.ID); err != nil {
		t.Fatal(err)
	}

	c.Advance(25 * time.Hour)
	young := add("https://young.example/client")
	if _, err := st.PruneOAuth(ctx, c.Now()); err != nil {
		t.Fatal(err)
	}

	if _, err := st.GetOAuthClient(ctx, abandoned.ID); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("a metadata client that never signed in and is over a day old must be pruned, got %v", err)
	}
	for name, cl := range map[string]*store.OAuthClient{"used": used, "young": young, "revoked": revoked} {
		if _, err := st.GetOAuthClient(ctx, cl.ID); err != nil {
			t.Errorf("the %s metadata client must be kept: %v", name, err)
		}
	}

	// And it is still refused as revoked, rather than fetched afresh.
	_, err := s.BeginAuthorization(ctx, testPolicy, AuthorizeParams{ClientID: revoked.ClientID,
		RedirectURI: "https://app.example/callback", ResponseType: "code",
		CodeChallenge: PKCEChallenge(verifier), CodeChallengeMethod: "S256"})
	if err == nil || !strings.Contains(err.Error(), "revoked") {
		t.Errorf("a revoked metadata client after the retention pass = %v, want it refused as revoked", err)
	}
}

// A consent is a person's, in a browser: a token cannot approve one.
func TestConsentNeedsAPerson(t *testing.T) {
	s, _, _ := newService(t)
	for _, id := range []*Identity{nil, {Kind: KindToken, ID: "tok_x", Role: store.RoleAdmin}, DevIdentity("")} {
		if _, err := s.ApproveAuthorization(t.Context(), testPolicy, id, "oar_x", store.RoleViewer); !errors.Is(err, ErrInvalidInput) {
			t.Errorf("%v must be refused as not a person, got %v", id, err)
		}
	}
}

// connectForTest registers a client, approves it for u and exchanges the code.
func connectForTest(t *testing.T, s *Service, u *store.User) (*store.OAuthClient, *TokenResponse) {
	t.Helper()
	ctx := t.Context()
	cl, err := s.RegisterClient(ctx, testPolicy, ClientRegistration{RedirectURIs: []string{ClaudeCallback}}, "203.0.113.9")
	if err != nil {
		t.Fatal(err)
	}
	r, err := s.BeginAuthorization(ctx, testPolicy, AuthorizeParams{ClientID: cl.ClientID, RedirectURI: ClaudeCallback,
		ResponseType: "code", CodeChallenge: PKCEChallenge(verifier), CodeChallengeMethod: "S256"})
	if err != nil {
		t.Fatal(err)
	}
	id := &Identity{Kind: KindUser, ID: u.ID, Name: u.Username, Role: u.Role}
	d, err := s.ApproveAuthorization(ctx, testPolicy, id, r.ID, store.RoleViewer)
	if err != nil {
		t.Fatal(err)
	}
	back, _ := url.Parse(d.RedirectTo)
	pair, err := s.Token(ctx, testPolicy, ClientCredentials{ClientID: cl.ClientID}, TokenRequest{
		GrantType: "authorization_code", Code: back.Query().Get("code"), CodeVerifier: verifier, RedirectURI: ClaudeCallback}, "")
	if err != nil {
		t.Fatal(err)
	}
	return cl, pair
}

// Closing open registration is meant for the requests already waiting too.
// Approving one anyway would hand the person a code the token endpoint then
// refuses, so the consent screen is where they are told.
func TestApprovalRechecksOpenRegistration(t *testing.T) {
	s, st, _ := newService(t)
	ctx := t.Context()
	u := addUser(t, st, "olive", store.RoleOperator, nil)
	cl, err := s.RegisterClient(ctx, testPolicy, ClientRegistration{RedirectURIs: []string{ClaudeCallback}}, "203.0.113.1")
	if err != nil {
		t.Fatal(err)
	}
	r, err := s.BeginAuthorization(ctx, testPolicy, AuthorizeParams{ClientID: cl.ClientID, RedirectURI: ClaudeCallback,
		ResponseType: "code", CodeChallenge: PKCEChallenge(verifier), CodeChallengeMethod: "S256"})
	if err != nil {
		t.Fatal(err)
	}
	closed := testPolicy
	closed.OpenRegistration = false
	id := &Identity{Kind: KindUser, ID: u.ID, Name: u.Username, Role: u.Role}
	_, err = s.ApproveAuthorization(ctx, closed, id, r.ID, store.RoleViewer)
	if !errors.Is(err, ErrInvalidInput) || !strings.Contains(err.Error(), "security.mcp_open_registration") {
		t.Fatalf("an approval after registration closed must be refused with the setting named, got %v", err)
	}
	if gs, _ := st.ListOAuthGrants(ctx, ""); len(gs) != 0 {
		t.Errorf("a refused approval must grant nothing, got %d connections", len(gs))
	}
}

// A replay revokes the connection once. A later presentation of the same
// credential finds it already ended and adds nothing to the audit log, which
// whoever holds the credential could otherwise fill.
func TestAReplayIsAuditedOnce(t *testing.T) {
	s, st, _ := newService(t)
	ctx := t.Context()
	u := addUser(t, st, "olive", store.RoleOperator, nil)
	_, pair := connectForTest(t, s, u)
	a, err := st.ResolveOAuthAccess(ctx, cryptox.HashToken(pair.AccessToken))
	if err != nil {
		t.Fatal(err)
	}
	g := &a.Grant
	for range 3 {
		s.revokeReplayed(ctx, g, "refresh token used twice", "203.0.113.5")
	}
	_, total, err := st.ListAudit(ctx, store.AuditFilter{Actions: []string{"mcp_connection.replay"}}, store.Page{})
	if err != nil {
		t.Fatal(err)
	}
	if total != 1 {
		t.Errorf("three replays of an ended connection must leave one audit row, got %d", total)
	}
}

// The last-used throttle holds an entry per connection that has been used;
// ending a connection, by any of the three routes, must take its entry with it.
func TestEndingAConnectionForgetsItsLastUse(t *testing.T) {
	s, st, _ := newService(t)
	ctx := t.Context()
	u := addUser(t, st, "olive", store.RoleOperator, nil)
	admin := &Identity{Kind: KindUser, ID: u.ID, Name: u.Username, Role: store.RoleAdmin}
	used := func(pair *TokenResponse) string {
		t.Helper()
		got, err := s.AuthenticateMCP(ctx, testPolicy, pair.AccessToken, "")
		if err != nil {
			t.Fatal(err)
		}
		if _, ok := s.touched.Load(got.ID); !ok {
			t.Fatal("a used connection must be in the throttle")
		}
		return got.ID
	}

	_, pair := connectForTest(t, s, u)
	g := used(pair)
	if err := s.RevokeMCPConnection(ctx, admin, g, ""); err != nil {
		t.Fatal(err)
	}
	if _, ok := s.touched.Load(g); ok {
		t.Error("revoking a connection must forget its last use")
	}

	cl, pair := connectForTest(t, s, u)
	g = used(pair)
	if err := s.RevokeMCPClient(ctx, admin, cl.ID); err != nil {
		t.Fatal(err)
	}
	if _, ok := s.touched.Load(g); ok {
		t.Error("revoking a client must forget its connections' last use")
	}

	cl, pair = connectForTest(t, s, u)
	g = used(pair)
	if err := s.RevokeToken(ctx, testPolicy, ClientCredentials{ClientID: cl.ClientID}, pair.RefreshToken, ""); err != nil {
		t.Fatal(err)
	}
	if _, ok := s.touched.Load(g); ok {
		t.Error("a client revoking its refresh token must forget the connection's last use")
	}
}

// A client that registers on every start is the same client each time, and is
// handed the registration it already has rather than a new row per start. One
// that says anything different -- another name, another redirect -- is new.
func TestAClientThatRegistersAgainGetsItsExistingRegistration(t *testing.T) {
	s, st, _ := newService(t)
	ctx := t.Context()
	claude := ClientRegistration{ClientName: "Claude", RedirectURIs: []string{ClaudeCallback, "http://localhost/callback"}}
	first, err := s.RegisterClient(ctx, testPolicy, claude, "203.0.113.1")
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name string
		in   ClientRegistration
		same bool
	}{
		{"the same metadata", claude, true},
		{"the same redirects in another order", ClientRegistration{ClientName: "Claude", RedirectURIs: []string{"http://localhost/callback", ClaudeCallback}}, true},
		{"another name", ClientRegistration{ClientName: "Claude Code", RedirectURIs: claude.RedirectURIs}, false},
		{"another redirect", ClientRegistration{ClientName: "Claude", RedirectURIs: []string{ClaudeCallback}}, false},
		{"a client URI", ClientRegistration{ClientName: "Claude", ClientURI: "https://claude.ai", RedirectURIs: claude.RedirectURIs}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := s.RegisterClient(ctx, testPolicy, tc.in, "198.51.100.7")
			if err != nil {
				t.Fatal(err)
			}
			if (got.ClientID == first.ClientID) != tc.same {
				t.Errorf("client ID %s against the first %s; want same=%v", got.ClientID, first.ClientID, tc.same)
			}
		})
	}

	// A revoked registration is not handed back: an administrator turned it
	// away, and the client registering again is a new decision to make.
	if err := s.RevokeMCPClient(ctx, &Identity{Kind: KindUser, ID: "usr_admin", Name: "admin", Role: store.RoleAdmin}, first.ID); err != nil {
		t.Fatal(err)
	}
	again, err := s.RegisterClient(ctx, testPolicy, claude, "203.0.113.1")
	if err != nil {
		t.Fatal(err)
	}
	if again.ClientID == first.ClientID {
		t.Error("a revoked registration must not be handed back")
	}
	if _, err := st.GetOAuthClient(ctx, again.ID); err != nil {
		t.Errorf("the fresh registration must be stored: %v", err)
	}
}

// A hundred new clients an hour from one address are allowed, and the one
// after is not; a client re-registering is not counted at all.
func TestRegistrationIsLimitedToAHundredNewClientsAnHourPerAddress(t *testing.T) {
	s, _, _ := newService(t)
	ctx := t.Context()
	ip := "203.0.113.50"
	for i := range mcpRegistrationsPerHour {
		if _, err := s.RegisterClient(ctx, testPolicy, ClientRegistration{
			ClientName: fmt.Sprintf("client %d", i), RedirectURIs: []string{ClaudeCallback}}, ip); err != nil {
			t.Fatalf("registration %d: %v", i+1, err)
		}
	}
	if mcpRegistrationsPerHour != 100 {
		t.Fatalf("the limit is %d; the owner set it at 100 an hour", mcpRegistrationsPerHour)
	}
	var oe *OAuthError
	if _, err := s.RegisterClient(ctx, testPolicy, ClientRegistration{ClientName: "one too many", RedirectURIs: []string{ClaudeCallback}}, ip); !errors.As(err, &oe) || oe.Status != 429 {
		t.Fatalf("the 101st new client must be a 429, got %v", err)
	}
	if _, err := s.RegisterClient(ctx, testPolicy, ClientRegistration{ClientName: "client 7", RedirectURIs: []string{ClaudeCallback}}, ip); err != nil {
		t.Errorf("a client re-registering must not be refused by the limit: %v", err)
	}
}

// Every request that gets as far as being kept writes a row, on an endpoint
// nobody signs in to reach and through the database's one writer, so a script
// that loops on it makes every other write wait behind it. Registration and the
// token endpoint were limited and this one was not. The refusal goes back to the
// client's own checked redirect, as the specification asks for an overloaded
// authorisation server.
func TestAuthorizationRequestsAreLimitedPerAddress(t *testing.T) {
	s, _, c := newService(t)
	ctx := t.Context()
	client, err := s.RegisterClient(ctx, testPolicy, ClientRegistration{ClientName: "scripted", RedirectURIs: []string{ClaudeCallback}}, "198.51.100.1")
	if err != nil {
		t.Fatal(err)
	}
	begin := func(ip string) error {
		_, err := s.BeginAuthorization(ctx, testPolicy, AuthorizeParams{
			ClientID: client.ClientID, RedirectURI: ClaudeCallback, ResponseType: "code",
			CodeChallenge: PKCEChallenge(verifier), CodeChallengeMethod: "S256", IP: ip,
		})
		return err
	}
	const ip = "203.0.113.60"
	for i := range mcpAuthorizationsPerMinute {
		if err := begin(ip); err != nil {
			t.Fatalf("request %d of %d: %v", i+1, mcpAuthorizationsPerMinute, err)
		}
	}

	var refusal *AuthorizeRefusal
	if err := begin(ip); !errors.As(err, &refusal) {
		t.Fatalf("the request after the limit = %v; want a refusal", err)
	}
	if refusal.Code != "temporarily_unavailable" || refusal.RedirectURI != ClaudeCallback {
		t.Errorf("refusal = %q to %q; want temporarily_unavailable sent back to the client's own redirect", refusal.Code, refusal.RedirectURI)
	}
	if err := begin("203.0.113.61"); err != nil {
		t.Errorf("another address must not share the limit: %v", err)
	}
	c.Advance(2 * time.Minute)
	if err := begin(ip); err != nil {
		t.Errorf("the limit must lift once the minute is over: %v", err)
	}
}

// A new password, and signing out everywhere else, end the account's MCP
// connections along with its sessions: a connection is a credential that
// outlives any session, and the person doing either suspects one is loose.
func TestChangingThePasswordOrSigningOutElsewhereEndsMCPConnections(t *testing.T) {
	for _, tc := range []struct {
		name   string
		reason string
		act    func(t *testing.T, s *Service, u *store.User)
	}{
		{"a password change", "password changed", func(t *testing.T, s *Service, u *store.User) {
			if err := s.ChangePassword(t.Context(), u.ID, testPassword, "a different password entirely", "10.0.0.1"); err != nil {
				t.Fatal(err)
			}
		}},
		{"an administrator's reset", "password reset by an administrator", func(t *testing.T, s *Service, u *store.User) {
			if err := s.ResetPassword(t.Context(), u.ID, "a different password entirely"); err != nil {
				t.Fatal(err)
			}
		}},
		{"signing out other sessions", "signed out of other sessions", func(t *testing.T, s *Service, u *store.User) {
			id := &Identity{Kind: KindUser, ID: u.ID, UserID: u.ID, Name: u.Username, Role: u.Role}
			n, err := s.LogoutOthers(t.Context(), id, "")
			if err != nil || n != 2 {
				t.Fatalf("LogoutOthers = %d, %v; want both connections ended", n, err)
			}
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, st, _ := newService(t)
			ctx := t.Context()
			u := addUser(t, st, "olive", store.RoleOperator, nil)
			other := addUser(t, st, "pat", store.RoleOperator, nil)
			cl, mine := connectForTest(t, s, u)
			_, alsoMine := connectForTest(t, s, u)
			_, theirs := connectForTest(t, s, other)

			tc.act(t, s, u)

			for _, pair := range []*TokenResponse{mine, alsoMine} {
				if _, err := s.AuthenticateMCP(ctx, testPolicy, pair.AccessToken, ""); err == nil {
					t.Error("the account's MCP access token must stop working")
				}
				if _, err := s.Token(ctx, testPolicy, ClientCredentials{ClientID: cl.ClientID}, TokenRequest{GrantType: "refresh_token", RefreshToken: pair.RefreshToken}, ""); err == nil {
					t.Error("the account's MCP refresh token must stop working")
				}
			}
			if _, err := s.AuthenticateMCP(ctx, testPolicy, theirs.AccessToken, ""); err != nil {
				t.Errorf("somebody else's connection must be left alone: %v", err)
			}
			rows, _, err := st.ListAudit(ctx, store.AuditFilter{Actions: []string{"mcp_connection.revoke_all"}}, store.Page{})
			if err != nil {
				t.Fatal(err)
			}
			if len(rows) != 1 || rows[0].TargetID != u.ID || !strings.Contains(rows[0].After, tc.reason) {
				t.Errorf("want one mcp_connection.revoke_all row for %s saying %q, got %+v", u.ID, tc.reason, rows)
			}
		})
	}
}

// An edit rescues an app configured with a client_id whose registered redirect
// does not match what the client sends, without a delete and a recreate that
// would leave the app pointing at an ID the portal no longer knows. Only what
// an administrator created is editable, and the live client after the edit is
// what the token endpoint will see on that app's next sign-in.
func TestEditingAnMCPClientChangesItsNameAndRedirects(t *testing.T) {
	s, st, _ := newService(t)
	ctx := t.Context()
	admin := &Identity{Kind: KindUser, ID: "usr_admin", Name: "admin", Role: store.RoleAdmin}
	c, secret, err := s.CreateMCPClient(ctx, admin, NewMCPClient{
		Name: "Claude", RedirectURIs: []string{ClaudeCallback}, Confidential: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if secret == "" {
		t.Fatal("a confidential client must come with a secret")
	}

	after, err := s.UpdateMCPClient(ctx, admin, c.ID, NewMCPClient{
		Name: "Claude Code", RedirectURIs: []string{"http://localhost:3118/callback"},
	})
	if err != nil {
		t.Fatalf("editing an admin client: %v", err)
	}
	if after.Name != "Claude Code" {
		t.Errorf("name after the edit: %q", after.Name)
	}
	if len(after.RedirectURIs) != 1 || after.RedirectURIs[0] != "http://localhost:3118/callback" {
		t.Errorf("redirects after the edit: %v", after.RedirectURIs)
	}
	// The identity the app holds is not disturbed by an edit: only a rotate
	// invalidates the secret it was given.
	if !after.Confidential() || after.SecretPrefix != c.SecretPrefix {
		t.Error("an edit must leave the client's secret alone")
	}

	// The audit carries both ends, so an app that starts failing can have its
	// old redirection read back from the log.
	rows, _, err := st.ListAudit(ctx, store.AuditFilter{Actions: []string{"mcp_client.update"}}, store.Page{})
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || !strings.Contains(rows[0].After, "localhost:3118") || !strings.Contains(rows[0].After, ClaudeCallback) {
		t.Errorf("want one mcp_client.update row naming both redirections, got %+v", rows)
	}

	// Only a client an administrator created is editable: a dynamic one owns
	// its registration, and a revoked one has nothing left to grant.
	if err := s.RevokeMCPClient(ctx, admin, c.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.UpdateMCPClient(ctx, admin, c.ID, NewMCPClient{Name: "Again"}); !errors.Is(err, ErrInvalidInput) {
		t.Errorf("editing a revoked client: %v", err)
	}
	registered, err := s.RegisterClient(ctx, testPolicy, ClientRegistration{
		ClientName: "Self", RedirectURIs: []string{ClaudeCallback},
	}, "203.0.113.9")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.UpdateMCPClient(ctx, admin, registered.ID, NewMCPClient{Name: "Again"}); !errors.Is(err, ErrInvalidInput) {
		t.Errorf("editing a dynamic client: %v", err)
	}
}

// An edit that removes a redirect URI is meant for the sign-ins already under
// way as much as for the ones that start afterwards. A fresh request for the
// address is refused at once, but a request begun in the last fifteen minutes
// and a code issued in the last five carry the address with them, and unless
// they are looked at again they deliver an authorisation code to the very place
// the administrator took away. What the edit still does not do is end a
// connection already made: its refresh token names no redirect, and the API
// promises that an edit leaves connections alone.
func TestRemovingARedirectRefusesTheSignInsAlreadyUnderWayForIt(t *testing.T) {
	s, st, _ := newService(t)
	ctx := t.Context()
	u := addUser(t, st, "olive", store.RoleOperator, nil)
	person := &Identity{Kind: KindUser, ID: u.ID, Name: u.Username, Role: u.Role}
	admin := &Identity{Kind: KindUser, ID: "usr_admin", Name: "admin", Role: store.RoleAdmin}
	const loopback = "http://localhost:3118/callback"

	client, _, err := s.CreateMCPClient(ctx, admin, NewMCPClient{Name: "Claude", RedirectURIs: []string{ClaudeCallback, loopback}})
	if err != nil {
		t.Fatal(err)
	}
	begin := func(redirect string) (*store.OAuthRequest, error) {
		return s.BeginAuthorization(ctx, testPolicy, AuthorizeParams{ClientID: client.ClientID, RedirectURI: redirect,
			ResponseType: "code", CodeChallenge: PKCEChallenge(verifier), CodeChallengeMethod: "S256"})
	}
	waitingFor := func(redirect string) *store.OAuthRequest {
		t.Helper()
		r, err := begin(redirect)
		if err != nil {
			t.Fatalf("starting a sign-in for %s: %v", redirect, err)
		}
		return r
	}
	codeFor := func(redirect string) string {
		t.Helper()
		d, err := s.ApproveAuthorization(ctx, testPolicy, person, waitingFor(redirect).ID, store.RoleViewer)
		if err != nil {
			t.Fatalf("approving a sign-in for %s: %v", redirect, err)
		}
		back, _ := url.Parse(d.RedirectTo)
		return back.Query().Get("code")
	}
	exchange := func(code, redirect string) (*TokenResponse, error) {
		return s.Token(ctx, testPolicy, ClientCredentials{ClientID: client.ClientID}, TokenRequest{
			GrantType: "authorization_code", Code: code, CodeVerifier: verifier, RedirectURI: redirect}, "203.0.113.9")
	}

	// Everything in flight when the edit lands: a connection already made
	// through the address that is about to go, a request waiting for consent
	// and a code not yet exchanged for it, and the same two for the address that
	// stays.
	connected, err := exchange(codeFor(loopback), loopback)
	if err != nil {
		t.Fatal(err)
	}
	waiting := waitingFor(loopback)
	issued := codeFor(loopback)
	stayingRequest := waitingFor(ClaudeCallback)
	stayingCode := codeFor(ClaudeCallback)

	if _, err := s.UpdateMCPClient(ctx, admin, client.ID, NewMCPClient{Name: "Claude", RedirectURIs: []string{ClaudeCallback}}); err != nil {
		t.Fatalf("removing the loopback redirect: %v", err)
	}

	// The front door was always shut: a new sign-in for the address is refused.
	var refusal *AuthorizeRefusal
	if _, err := begin(loopback); !errors.As(err, &refusal) {
		t.Fatalf("a new sign-in for a removed redirect must be refused, got %v", err)
	}

	// The consent screen for the waiting request says where approving would send
	// the person, which is the address the request holds and not whatever the
	// client lists today.
	view, err := s.DescribeAuthorization(ctx, person, waiting.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !view.LoopbackOnly || view.RedirectHost != "localhost:3118" {
		t.Errorf("the screen for a request to localhost:3118 says loopback=%v host=%q, want true and localhost:3118", view.LoopbackOnly, view.RedirectHost)
	}

	// Approving it would hand a code to the removed address. It is refused, in
	// words that say what happened and what to do, and nothing is written.
	d, err := s.ApproveAuthorization(ctx, testPolicy, person, waiting.ID, store.RoleViewer)
	if d != nil || !errors.Is(err, ErrInvalidInput) || !strings.Contains(err.Error(), "redirect URIs") || !strings.Contains(err.Error(), "again") {
		t.Errorf("approving a request for a removed redirect = %+v, %v; want it refused as invalid input saying to start again", d, err)
	}
	if _, err := st.GetOAuthRequest(ctx, waiting.ID); err != nil {
		t.Errorf("a refused approval must leave the request as it was: %v", err)
	}

	// The code issued before the edit is refused at the token endpoint, whether
	// the exchange names the address or leaves it out, because the code's own
	// address is what is checked.
	for _, named := range []string{loopback, ""} {
		_, err = exchange(issued, named)
		var oe *OAuthError
		if !errors.As(err, &oe) || oe.Code != "invalid_grant" || oe.Status != 400 || !strings.Contains(oe.Description, "sign in again") {
			t.Errorf("exchanging a code for a removed redirect (redirect_uri %q) = %v; want 400 invalid_grant saying to sign in again", named, err)
		}
	}

	// What the edit leaves alone. The address that stayed is approved and
	// exchanged as before, and the connection made before the edit is still a
	// connection: it works, and it refreshes.
	if _, err := s.ApproveAuthorization(ctx, testPolicy, person, stayingRequest.ID, store.RoleViewer); err != nil {
		t.Errorf("a request for an address that is still registered must still be approved: %v", err)
	}
	if _, err := exchange(stayingCode, ClaudeCallback); err != nil {
		t.Errorf("a code for an address that is still registered must still be exchanged: %v", err)
	}
	if _, err := s.AuthenticateMCP(ctx, testPolicy, connected.AccessToken, ""); err != nil {
		t.Errorf("an edit must not end a connection already made: %v", err)
	}
	if _, err := s.Token(ctx, testPolicy, ClientCredentials{ClientID: client.ClientID},
		TokenRequest{GrantType: "refresh_token", RefreshToken: connected.RefreshToken}, ""); err != nil {
		t.Errorf("an edit must not stop a connection already made from refreshing: %v", err)
	}
}

// The consent screen warns that the code is going to the person's own machine,
// where any program can listen. That is a fact about the address this request
// sends them to: a client that also registered an https address is not made
// safe on its loopback one by it, and is not made unsafe on the https one.
func TestTheConsentScreenWarnsAboutTheAddressThisRequestSendsThePersonTo(t *testing.T) {
	s, st, _ := newService(t)
	ctx := t.Context()
	u := addUser(t, st, "olive", store.RoleOperator, nil)
	person := &Identity{Kind: KindUser, ID: u.ID, Name: u.Username, Role: u.Role}
	admin := &Identity{Kind: KindUser, ID: "usr_admin", Name: "admin", Role: store.RoleAdmin}

	client, _, err := s.CreateMCPClient(ctx, admin, NewMCPClient{
		Name: "Claude", RedirectURIs: []string{ClaudeCallback, "http://localhost/callback", "http://127.0.0.1/callback"}})
	if err != nil {
		t.Fatal(err)
	}
	for redirect, want := range map[string]bool{
		ClaudeCallback:                    false,
		"http://localhost:3118/callback":  true,
		"http://127.0.0.1:50000/callback": true,
	} {
		r, err := s.BeginAuthorization(ctx, testPolicy, AuthorizeParams{ClientID: client.ClientID, RedirectURI: redirect,
			ResponseType: "code", CodeChallenge: PKCEChallenge(verifier), CodeChallengeMethod: "S256"})
		if err != nil {
			t.Fatalf("starting a sign-in for %s: %v", redirect, err)
		}
		v, err := s.DescribeAuthorization(ctx, person, r.ID)
		if err != nil {
			t.Fatal(err)
		}
		if v.LoopbackOnly != want {
			t.Errorf("the screen for a request to %s says loopback=%v, want %v", redirect, v.LoopbackOnly, want)
		}
	}
}
