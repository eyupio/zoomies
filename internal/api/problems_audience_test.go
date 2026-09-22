package api

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/eyupio/zoomies/internal/store"
)

type problemsBody struct {
	OK    bool `json:"ok"`
	Items []struct {
		Code     string `json:"code"`
		Audience string `json:"audience"`
	} `json:"items"`
}

func problemsSeenBy(t *testing.T, h *harness, role store.Role) problemsBody {
	t.Helper()
	u, _ := h.user(string(role)+"-reader", role)
	resp := h.do(request{method: http.MethodGet, path: "/api/v1/problems", cookie: h.session(u)})
	if resp.status != http.StatusOK {
		t.Fatalf("GET /problems as %s = %d: %s", role, resp.status, resp.body)
	}
	var out problemsBody
	if err := json.Unmarshal(resp.body, &out); err != nil {
		t.Fatalf("decoding the problems list: %v", err)
	}
	return out
}

// The problems drawer is the page an operator opens when something is wrong,
// and on an instance one team operates for another there are two things
// wrong and two people to tell. A fleet told the controller's lease was lost
// can do nothing about it; it also learns how the process is deployed.
func TestTheProblemsDrawerIsTwoListsNotOne(t *testing.T) {
	h := newHarness(t)

	asAdmin := problemsSeenBy(t, h, store.RoleAdmin)
	for _, p := range asAdmin.Items {
		if p.Audience == "platform" {
			t.Errorf("an administrator was shown %q, which is the platform's", p.Code)
		}
		if p.Audience == "" {
			t.Errorf("%q reached the fleet with no audience decided for it", p.Code)
		}
	}

	// A validator finding is the clearest case: every one names a setting,
	// and the dangerous ones name what the process binds and trusts. The
	// harness runs with authentication off, so auth.disabled is raised.
	asPlatform := problemsSeenBy(t, h, store.RolePlatform)
	var platformSaw bool
	for _, p := range asPlatform.Items {
		if p.Audience == "platform" {
			platformSaw = true
		}
	}
	if !platformSaw {
		t.Fatal("the platform's own list carries nothing, so this proves nothing about the split")
	}
	if len(asPlatform.Items) <= len(asAdmin.Items) {
		t.Errorf("the platform saw %d problems and the fleet %d; the platform sees its own as well as theirs is not the claim, but it must see more here",
			len(asPlatform.Items), len(asAdmin.Items))
	}
}

// OK is what the drawer renders "nothing needs your attention" from. Computed
// before the filter it would say the opposite of what the list shows, and
// send somebody looking for a problem they are not allowed to see.
func TestAFleetWithNothingWrongIsToldSoEvenWhileTheProcessHasTroubles(t *testing.T) {
	h := newHarness(t)

	asAdmin := problemsSeenBy(t, h, store.RoleAdmin)
	if asAdmin.OK != (len(asAdmin.Items) == 0) {
		t.Errorf("ok = %v with %d items: the flag and the list disagree", asAdmin.OK, len(asAdmin.Items))
	}

	// And the platform's own view is computed the same way.
	asPlatform := problemsSeenBy(t, h, store.RolePlatform)
	if asPlatform.OK != (len(asPlatform.Items) == 0) {
		t.Errorf("ok = %v with %d items for the platform", asPlatform.OK, len(asPlatform.Items))
	}
}

// The support bundle stays the fleet's to collect -- "send me a bundle" is
// the platform's first support question -- but what it carries about the
// machine the process runs on is reduced to the build it all ran on.
func TestTheFleetsBundleIdentifiesTheBuildAndNotTheMachine(t *testing.T) {
	h := newHarness(t)
	admin, _ := h.user("fleet-admin", store.RoleAdmin)

	resp := h.do(request{method: http.MethodGet, path: "/api/v1/diagnostics/bundle", cookie: h.session(admin)})
	if resp.status != http.StatusOK {
		t.Fatalf("the fleet cannot collect a bundle: %d %s", resp.status, resp.body)
	}
	var doc struct {
		Instance struct {
			Version          string `json:"version"`
			Go               string `json:"go"`
			OS               string `json:"os"`
			CPUs             int    `json:"cpus"`
			DatabasePath     string `json:"database_path"`
			EventSubscribers int    `json:"event_subscribers"`
		} `json:"instance"`
	}
	if err := json.Unmarshal(resp.body, &doc); err != nil {
		t.Fatalf("decoding the bundle: %v", err)
	}
	if doc.Instance.Version == "" || doc.Instance.Go == "" {
		t.Error("the fleet's bundle does not identify the build, which is the point of sending one")
	}
	for name, got := range map[string]any{
		"os": doc.Instance.OS, "cpus": doc.Instance.CPUs,
		"database_path": doc.Instance.DatabasePath, "event_subscribers": doc.Instance.EventSubscribers,
	} {
		switch v := got.(type) {
		case string:
			if v != "" {
				t.Errorf("the fleet's bundle describes the machine: %s = %q", name, v)
			}
		case int:
			if v != 0 {
				t.Errorf("the fleet's bundle describes the machine: %s = %d", name, v)
			}
		}
	}

	// The platform's bundle is whole, which is what makes it worth asking for.
	plat, _ := h.user("operator-of-record", store.RolePlatform)
	whole := h.do(request{method: http.MethodGet, path: "/api/v1/diagnostics/bundle", cookie: h.session(plat)})
	if !strings.Contains(string(whole.body), `"cpus"`) {
		t.Error("the platform's own bundle lost the machine it is about")
	}
}
