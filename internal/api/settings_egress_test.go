package api

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/eyupio/zoomies/internal/config"
	"github.com/eyupio/zoomies/internal/store"
)

// fieldRefusal returns the message a 422 carries for one field, or "".
func fieldRefusal(t *testing.T, body []byte, field string) string {
	t.Helper()
	var env struct {
		Errors []fieldError `json:"errors"`
	}
	if err := json.Unmarshal(body, &env); err != nil {
		t.Fatalf("decoding the refusal: %v\n%s", err, body)
	}
	for _, e := range env.Errors {
		if e.Field == field {
			return e.Message
		}
	}
	return ""
}

// The threat the guard exists for is somebody with settings rights, not the
// person who runs the process, aiming the controller at its own
// neighbourhood. So a write through the settings page is refused even though
// the same value in the file would only be warned about -- and the refusal
// names the key and the switch that would admit it.
func TestWritingAPrivateOutboundURLThroughTheSettingsIsRefused(t *testing.T) {
	for _, tc := range []struct {
		field string
		body  map[string]any
	}{
		{"github.api_base_url", map[string]any{"github.api_base_url": "https://10.0.0.9/api/v3"}},
		{"capacity_demand.destination_url", map[string]any{"capacity_demand.destination_url": "http://169.254.169.254/latest"}},
		{"agent.runner_download_url", map[string]any{"agent.runner_download_url": "http://127.0.0.1:8000/runner"}},
		{"oidc.issuer", map[string]any{"oidc.enabled": true, "oidc.issuer": "https://192.168.10.4/realms/fleet", "oidc.client_id": "zoomies"}},
	} {
		t.Run(tc.field, func(t *testing.T) {
			h := newHarness(t)
			admin, _ := h.user("admin", store.RolePlatform)
			resp := h.do(request{method: http.MethodPatch, path: "/api/v1/settings", cookie: h.session(admin), body: tc.body})
			resp.mustStatus(t, http.StatusUnprocessableEntity, "write a private outbound URL")
			msg := fieldRefusal(t, resp.body, tc.field)
			if !strings.Contains(msg, tc.field) || !strings.Contains(msg, "security.allow_private_egress") {
				t.Errorf("the refusal does not name %s and the switch: %q\n%s", tc.field, msg, resp.body)
			}
		})
	}
}

// Turning single sign-on on starts dialling an issuer that is already
// stored, so it is refused as though the issuer had been written.
func TestTurningSingleSignOnOnWithAPrivateIssuerIsRefused(t *testing.T) {
	h := newHarness(t, func(c *config.Config) {
		c.OIDC.Issuer = "https://192.168.10.4/realms/fleet"
		c.OIDC.ClientID = "zoomies"
	})
	admin, _ := h.user("admin", store.RolePlatform)
	resp := h.do(request{method: http.MethodPatch, path: "/api/v1/settings", cookie: h.session(admin),
		body: map[string]any{"oidc.enabled": true}})
	resp.mustStatus(t, http.StatusUnprocessableEntity, "turn on single sign-on at a LAN issuer")
	if msg := fieldRefusal(t, resp.body, "oidc.enabled"); !strings.Contains(msg, "oidc.issuer") {
		t.Errorf("the refusal does not say it is the issuer: %q\n%s", msg, resp.body)
	}
}

// With the platform's switch on, the same write goes through.
func TestAllowingPrivateEgressAdmitsTheSettingsWrite(t *testing.T) {
	h := newHarness(t, allowPrivateEgress)
	admin, _ := h.user("admin", store.RolePlatform)
	resp := h.do(request{method: http.MethodPatch, path: "/api/v1/settings", cookie: h.session(admin),
		body: map[string]any{"github.api_base_url": "https://10.0.0.9/api/v3"}})
	resp.mustStatus(t, http.StatusOK, "write a LAN Enterprise Server once allowed")
	if got := h.ctrl.Config().GitHub.APIBaseURL; !strings.Contains(got, "10.0.0.9") {
		t.Errorf("github.api_base_url = %q after an admitted write", got)
	}
}

// An import is a write through the API too: its preview marks the row, and
// applying it is refused.
func TestAnImportNamingAPrivateOutboundURLIsRefused(t *testing.T) {
	h := newHarness(t)
	admin, _ := h.user("admin", store.RolePlatform)
	cookie := h.session(admin)
	document := "github:\n  api_base_url: https://10.0.0.9/api/v3\n"

	preview := h.do(request{method: http.MethodPost, path: "/api/v1/settings/import", cookie: cookie,
		body: map[string]any{"document": document, "dry_run": true}})
	preview.mustStatus(t, http.StatusOK, "dry run")
	var out importResponse
	preview.into(t, &out)
	var row *importChange
	for i := range out.Changes {
		if out.Changes[i].Key == "github.api_base_url" {
			row = &out.Changes[i]
		}
	}
	if row == nil || row.Action != importRefusedAction || !strings.Contains(row.Reason, "security.allow_private_egress") {
		t.Fatalf("the preview does not refuse the row by the switch: %+v", row)
	}

	applied := h.do(request{method: http.MethodPost, path: "/api/v1/settings/import", cookie: cookie,
		body: map[string]any{"document": document}})
	applied.mustStatus(t, http.StatusUnprocessableEntity, "apply")
	if got := h.ctrl.Config().GitHub.APIBaseURL; strings.Contains(got, "10.0.0.9") {
		t.Errorf("a refused import changed github.api_base_url to %q", got)
	}
}
