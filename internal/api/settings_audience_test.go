package api

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/eyupio/zoomies/internal/store"
)

// settingsBody is the part of the settings page these tests read.
type settingsBody struct {
	Settings []struct {
		Key      string `json:"key"`
		Scope    string `json:"scope"`
		Editable bool   `json:"editable"`
		Reason   string `json:"reason"`
	} `json:"settings"`
	ConfigPath          string   `json:"config_path"`
	DatabasePath        string   `json:"database_path"`
	EventSubscribers    int      `json:"event_subscribers"`
	RestartRequiredKeys []string `json:"restart_required_keys"`
}

func settingsFor(t *testing.T, h *harness, role store.Role) settingsBody {
	t.Helper()
	u, _ := h.user(string(role)+"-caller", role)
	resp := h.do(request{method: http.MethodGet, path: "/api/v1/settings", cookie: h.session(u)})
	if resp.status != http.StatusOK {
		t.Fatalf("GET /settings as %s = %d: %s", role, resp.status, resp.body)
	}
	var out settingsBody
	if err := json.Unmarshal(resp.body, &out); err != nil {
		t.Fatalf("decoding the settings page: %v", err)
	}
	return out
}

// An instance one team operates while another uses the fleet is the shape the
// platform role exists for. There, what the process binds, trusts, stores and
// logs on its own machine is not the fleet's business -- so an administrator
// is not shown those keys at all, rather than shown them locked. A locked
// field still says where the backups go.
func TestAFleetAdministratorIsNotShownThePlatformsSettings(t *testing.T) {
	h := newHarness(t)

	asAdmin := settingsFor(t, h, store.RoleAdmin)
	for _, s := range asAdmin.Settings {
		if s.Scope == "platform" {
			t.Errorf("an administrator was shown %q, which is the platform's", s.Key)
		}
	}
	// And the keys it does get are real ones, so a filter that returned
	// nothing at all would not pass this.
	var sawFleet bool
	for _, s := range asAdmin.Settings {
		if s.Key == "scheduler.interval" {
			sawFleet = true
		}
	}
	if !sawFleet {
		t.Error("an administrator was shown no scheduler keys; the fleet's own settings are theirs")
	}

	// The platform sees both halves: on a single-team instance this is what
	// an administrator saw before the role existed.
	asPlatform := settingsFor(t, h, store.RolePlatform)
	var platformKeys, fleetKeys int
	for _, s := range asPlatform.Settings {
		switch s.Scope {
		case "platform":
			platformKeys++
		case "instance":
			fleetKeys++
		}
	}
	if platformKeys == 0 || fleetKeys == 0 {
		t.Errorf("the platform saw %d platform and %d fleet keys; it should see both", platformKeys, fleetKeys)
	}
	if len(asPlatform.Settings) <= len(asAdmin.Settings) {
		t.Errorf("the platform saw %d settings and an administrator %d; the platform sees more",
			len(asPlatform.Settings), len(asAdmin.Settings))
	}
}

// The paths and the subscriber count describe the machine the process is on,
// which on an operated instance belongs to whoever runs it. A fleet's
// administrator has no use for a path on somebody else's host.
func TestAFleetAdministratorIsNotToldWhereTheProcessKeepsItsFiles(t *testing.T) {
	h := newHarness(t)

	asAdmin := settingsFor(t, h, store.RoleAdmin)
	if asAdmin.ConfigPath != "" {
		t.Errorf("an administrator was told the configuration file is at %q", asAdmin.ConfigPath)
	}
	if asAdmin.DatabasePath != "" {
		t.Errorf("an administrator was told the database is at %q", asAdmin.DatabasePath)
	}
	if asAdmin.EventSubscribers != 0 {
		t.Errorf("an administrator was told there are %d event subscribers", asAdmin.EventSubscribers)
	}

	// A key the caller was never shown must not survive in a list of key
	// names either: naming one there reads as a bug in the page.
	for _, k := range asAdmin.RestartRequiredKeys {
		if strings.HasPrefix(k, "server.") || strings.HasPrefix(k, "backup.") || strings.HasPrefix(k, "retention.") {
			t.Errorf("restart_required_keys named %q, a setting the caller was not shown", k)
		}
	}

	asPlatform := settingsFor(t, h, store.RolePlatform)
	if asPlatform.DatabasePath == "" {
		t.Error("the platform was not told where its own database is")
	}
}

// The bootstrap keys are not platform-scoped -- they are not stored anywhere
// a scope could apply to -- but their values are the database file and the
// key file. Blanking database_path while database.path carried the same
// string in the same response is the leak the browser-level audience test
// found, so this pins it where it is cheapest to run.
func TestAFleetAdministratorIsNotShownTheDatabaseOrKeyFile(t *testing.T) {
	h := newHarness(t)
	admin, _ := h.user("fleet-admin", store.RoleAdmin)

	for _, path := range []string{"/api/v1/settings", "/api/v1/diagnostics/bundle"} {
		resp := h.do(request{method: http.MethodGet, path: path, cookie: h.session(admin)})
		if resp.status != http.StatusOK {
			t.Fatalf("GET %s as admin = %d: %s", path, resp.status, resp.body)
		}
		for _, leak := range []string{"database.path", `"database":`, "encryption_key_file", h.ctrl.Store().Path()} {
			if leak != "" && strings.Contains(string(resp.body), leak) {
				t.Errorf("GET %s told an administrator %q", path, leak)
			}
		}
	}

	asPlatform := settingsFor(t, h, store.RolePlatform)
	var sawDatabase bool
	for _, s := range asPlatform.Settings {
		sawDatabase = sawDatabase || s.Key == "database.path"
	}
	if !sawDatabase {
		t.Error("the platform was not shown database.path; it is theirs to see")
	}
}

// The refusal is the half an operator acts on: a settings page that says
// "not editable" with no reason is what sends somebody to the source.
func TestAnAdministratorChangingAPlatformSettingIsToldWhoseItIs(t *testing.T) {
	h := newHarness(t)
	admin, _ := h.user("fleet-admin", store.RoleAdmin)

	resp := h.do(request{
		method: http.MethodPatch, path: "/api/v1/settings", cookie: h.session(admin),
		body: map[string]any{"server.bind": "0.0.0.0:9999"},
	})
	if resp.status != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422: %s", resp.status, resp.body)
	}
	if !strings.Contains(string(resp.body), "whoever runs this controller") {
		t.Errorf("the refusal does not say whose the setting is: %s", resp.body)
	}
	// And it did not take effect.
	if got := h.ctrl.Config().Server.Bind; got == "0.0.0.0:9999" {
		t.Errorf("the bind address was changed to %q by somebody who may not change it", got)
	}

	// The same change from the platform is ordinary work.
	plat, _ := h.user("operator-of-record", store.RolePlatform)
	ok := h.do(request{
		method: http.MethodPatch, path: "/api/v1/settings", cookie: h.session(plat),
		body: map[string]any{"server.bind": "127.0.0.1:9191"},
	})
	if ok.status != http.StatusOK {
		t.Fatalf("the platform could not change its own listener: %d %s", ok.status, ok.body)
	}
}

// The audience split on the page is only worth as much as the narrowest way
// out of it. An export is one click from the settings page and carries every
// key somebody has set, so an administrator who cannot read the instance's
// bind address on the page must not be able to read it out of the download.
func TestAnAdministratorsExportCarriesOnlyTheFleetsSettings(t *testing.T) {
	h := newHarness(t)

	// Something set at each scope, so neither half is absent by accident.
	plat, _ := h.user("operator-of-record", store.RolePlatform)
	set := h.do(request{
		method: http.MethodPatch, path: "/api/v1/settings", cookie: h.session(plat),
		body: map[string]any{"backup.directory": "/var/lib/zoomies/backups", "scheduler.interval": "12s"},
	})
	if set.status != http.StatusOK {
		t.Fatalf("seeding the settings: %d %s", set.status, set.body)
	}

	admin, _ := h.user("fleet-admin", store.RoleAdmin)
	resp := h.do(request{method: http.MethodGet, path: "/api/v1/settings/export", cookie: h.session(admin)})
	if resp.status != http.StatusOK {
		t.Fatalf("export as admin = %d: %s", resp.status, resp.body)
	}
	body := string(resp.body)
	if strings.Contains(body, "/var/lib/zoomies/backups") {
		t.Errorf("an administrator's export carries the platform's backup directory:\n%s", body)
	}
	if !strings.Contains(body, "12s") {
		t.Errorf("an administrator's export lost the fleet's own scheduler interval:\n%s", body)
	}

	// The platform's own export is whole: it is the instance's configuration,
	// and it is what a restore is started from.
	whole := h.do(request{method: http.MethodGet, path: "/api/v1/settings/export", cookie: h.session(plat)})
	if whole.status != http.StatusOK {
		t.Fatalf("export as platform = %d: %s", whole.status, whole.body)
	}
	if !strings.Contains(string(whole.body), "/var/lib/zoomies/backups") {
		t.Error("the platform's export is missing its own backup directory")
	}
}

// The support bundle renders the same configuration tree the settings page
// does, so it answers the same way. A bundle is the third way out of the
// audience split, after the page and the export, and an operator collecting
// one for a bug report should not be the way an instance's bind address
// reaches whoever asked for it.
func TestAnAdministratorsSupportBundleLeavesOutThePlatformsConfiguration(t *testing.T) {
	h := newHarness(t)

	plat, _ := h.user("operator-of-record", store.RolePlatform)
	set := h.do(request{
		method: http.MethodPatch, path: "/api/v1/settings", cookie: h.session(plat),
		body: map[string]any{"backup.directory": "/var/lib/zoomies/backups"},
	})
	if set.status != http.StatusOK {
		t.Fatalf("seeding: %d %s", set.status, set.body)
	}

	admin, _ := h.user("fleet-admin", store.RoleAdmin)
	resp := h.do(request{method: http.MethodGet, path: "/api/v1/diagnostics/bundle", cookie: h.session(admin)})
	if resp.status != http.StatusOK {
		t.Fatalf("bundle as admin = %d: %s", resp.status, resp.body)
	}
	if strings.Contains(string(resp.body), "/var/lib/zoomies/backups") {
		t.Error("an administrator's support bundle carries the platform's backup directory")
	}

	var doc struct {
		Instance struct {
			ConfigPath   string `json:"config_path"`
			DatabasePath string `json:"database_path"`
		} `json:"instance"`
	}
	if err := json.Unmarshal(resp.body, &doc); err != nil {
		t.Fatalf("decoding the bundle: %v", err)
	}
	if doc.Instance.DatabasePath != "" {
		t.Errorf("the bundle told an administrator the database is at %q", doc.Instance.DatabasePath)
	}

	// The platform's own bundle is the whole instance, which is what makes it
	// worth collecting.
	whole := h.do(request{method: http.MethodGet, path: "/api/v1/diagnostics/bundle", cookie: h.session(plat)})
	if !strings.Contains(string(whole.body), "/var/lib/zoomies/backups") {
		t.Error("the platform's own bundle is missing its backup directory")
	}
}
