package api

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/eyupio/zoomies/internal/store"
)

type tokenList struct {
	Items []struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	} `json:"items"`
}

func mintToken(t *testing.T, h *harness, cookie, name string, role store.Role) string {
	t.Helper()
	resp := h.do(request{
		method: http.MethodPost, path: "/api/v1/tokens", cookie: cookie,
		body: map[string]any{"name": name, "role": string(role)},
	})
	if resp.status != http.StatusCreated {
		t.Fatalf("minting %q: %d %s", name, resp.status, resp.body)
	}
	var out struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(resp.body, &out); err != nil {
		t.Fatalf("decoding the new token: %v", err)
	}
	return out.ID
}

func listTokens(t *testing.T, h *harness, cookie string) tokenList {
	t.Helper()
	resp := h.do(request{method: http.MethodGet, path: "/api/v1/tokens", cookie: cookie})
	if resp.status != http.StatusOK {
		t.Fatalf("listing tokens: %d %s", resp.status, resp.body)
	}
	var out tokenList
	if err := json.Unmarshal(resp.body, &out); err != nil {
		t.Fatalf("decoding the token list: %v", err)
	}
	return out
}

// A platform account operating an instance for another team needs automation
// against it -- a metrics scraper, a backup verifier -- and those credentials
// sat on the same page the fleet's administrators use, revocable by any of
// them. A fleet that can switch off the monitoring of a process it does not
// run can do so by accident.
func TestAPlatformsOwnTokensAreNotTheFleetsToSeeOrRevoke(t *testing.T) {
	h := newHarness(t)
	plat, _ := h.user("operator-of-record", store.RolePlatform)
	platCookie := h.session(plat)
	admin, _ := h.user("fleet-admin", store.RoleAdmin)
	adminCookie := h.session(admin)

	scraper := mintToken(t, h, platCookie, "metrics-scraper", store.RoleViewer)
	fleets := mintToken(t, h, adminCookie, "deploy-bot", store.RoleOperator)

	// The fleet sees its own and not the platform's.
	asAdmin := listTokens(t, h, adminCookie)
	for _, tok := range asAdmin.Items {
		if tok.ID == scraper {
			t.Errorf("an administrator was shown the platform's %q", tok.Name)
		}
	}
	var sawOwn bool
	for _, tok := range asAdmin.Items {
		if tok.ID == fleets {
			sawOwn = true
		}
	}
	if !sawOwn {
		t.Error("an administrator cannot see the fleet's own token; the filter took too much")
	}

	// And cannot revoke what it cannot see. A 404 rather than a 403: refusing
	// would confirm the credential exists, which is the fact being withheld.
	resp := h.do(request{method: http.MethodDelete, path: "/api/v1/tokens/" + scraper, cookie: adminCookie})
	if resp.status != http.StatusNotFound {
		t.Errorf("revoking the platform's token as an administrator = %d, want 404: %s", resp.status, resp.body)
	}

	// The platform sees both, and its own still works.
	asPlatform := listTokens(t, h, platCookie)
	var sawScraper, sawFleets bool
	for _, tok := range asPlatform.Items {
		switch tok.ID {
		case scraper:
			sawScraper = true
		case fleets:
			sawFleets = true
		}
	}
	if !sawScraper || !sawFleets {
		t.Errorf("the platform saw its own=%v and the fleet's=%v; it should see both", sawScraper, sawFleets)
	}
	if got := h.do(request{method: http.MethodDelete, path: "/api/v1/tokens/" + scraper, cookie: platCookie}); got.status != http.StatusNoContent {
		t.Errorf("the platform could not revoke its own token: %d %s", got.status, got.body)
	}
}

// A token minted before the column existed has no owner recorded. Guessing one
// from the token's role would hide the fleet's own automation from it, because
// migration 0045 promoted every administrator's token to the platform role for
// an unrelated reason -- to keep what it could already do.
func TestATokenFromBeforeTheColumnStaysVisible(t *testing.T) {
	h := newHarness(t)
	admin, _ := h.user("fleet-admin", store.RoleAdmin)

	legacy := &store.APIToken{
		Name: "nightly-backup", Role: store.RolePlatform, OwnerRole: "",
		TokenHash: "hash-legacy", Prefix: "zoo_legacy",
	}
	if err := h.st.CreateAPIToken(h.ctx, legacy); err != nil {
		t.Fatalf("seeding a token from before the column: %v", err)
	}

	var found bool
	for _, tok := range listTokens(t, h, h.session(admin)).Items {
		if tok.ID == legacy.ID {
			found = true
		}
	}
	if !found {
		t.Error("a token from before the column vanished from the administrator who could see it yesterday")
	}
}

// An audit row's `ip` says where a colleague was working from. That is a fact
// about a person rather than about the fleet, and an operator who can read
// that their administrator signed in from a hotel has learned something the
// audit log exists to record rather than to publish. Administrators keep it,
// because chasing a suspicious sign-in is why the column is there.
func TestTheAuditAddressIsForAdministrators(t *testing.T) {
	h := newHarness(t)
	admin, _ := h.user("fleet-admin", store.RoleAdmin)
	adminCookie := h.session(admin)

	// Something to audit, from a known address.
	mintToken(t, h, adminCookie, "audited-token", store.RoleViewer)

	read := func(cookie string) []struct {
		Action string `json:"action"`
		IP     string `json:"ip"`
	} {
		t.Helper()
		resp := h.do(request{method: http.MethodGet, path: "/api/v1/audit", cookie: cookie})
		if resp.status != http.StatusOK {
			t.Fatalf("reading the audit log: %d %s", resp.status, resp.body)
		}
		var out struct {
			Items []struct {
				Action string `json:"action"`
				IP     string `json:"ip"`
			} `json:"items"`
		}
		if err := json.Unmarshal(resp.body, &out); err != nil {
			t.Fatalf("decoding the audit log: %v", err)
		}
		return out.Items
	}

	asAdmin := read(adminCookie)
	var adminSawAnAddress bool
	for _, e := range asAdmin {
		if e.IP != "" {
			adminSawAnAddress = true
		}
	}
	if !adminSawAnAddress {
		t.Fatal("no audit row carried an address for an administrator, so this proves nothing")
	}

	operator, _ := h.user("fleet-operator", store.RoleOperator)
	for _, e := range read(h.session(operator)) {
		if e.IP != "" {
			t.Errorf("an operator was shown the address %q on a %q row", e.IP, e.Action)
		}
	}
}
