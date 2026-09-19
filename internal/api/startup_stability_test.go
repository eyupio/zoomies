package api

import (
	"github.com/eyupio/zoomies/internal/config"
	"github.com/eyupio/zoomies/internal/store"
	"net/http"
	"strings"
	"testing"
)

func TestAutomaticPoolWizardAndSettingsShareStartupFindings(t *testing.T) {
	h := newHarness(t, func(c *config.Config) { c.Scheduler.DefaultRunnerLimits = false })
	inst := h.installation()
	h.host("vm-1")
	operator, _ := h.user("operator", store.RoleOperator)
	cookie := h.session(operator)
	body := poolBody(inst.ID)
	created := h.do(request{method: http.MethodPost, path: "/api/v1/pools", cookie: cookie, body: body})
	created.mustStatus(t, http.StatusCreated, "create")
	var pool poolResponse
	created.into(t, &pool)
	for _, path := range []string{"/api/v1/pools/validate", "/api/v1/pools/validate?id=" + pool.ID} {
		if path == "/api/v1/pools/validate" {
			body["name"] = "another-pool"
		} else {
			body["name"] = pool.Name
		}
		response := h.do(request{method: http.MethodPost, path: path, cookie: cookie, body: body})
		response.mustStatus(t, http.StatusOK, "wizard")
		var verdict validatePoolResponse
		response.into(t, &verdict)
		found := false
		for _, w := range verdict.Warnings {
			if w.Code == "scheduler.default_runner_limits_off" && w.Setting == "scheduler.default_runner_limits" {
				found = true
			}
		}
		if !found || !verdict.Valid {
			t.Fatalf("wizard omitted advisory or blocked save: %+v", verdict)
		}
	}
	h.do(request{method: http.MethodPatch, path: "/api/v1/settings", cookie: cookie, body: map[string]any{"scheduler.default_runner_limits": true}}).mustStatus(t, http.StatusForbidden, "operator cannot fix fleet settings")
	admin, _ := h.user("admin", store.RoleAdmin)
	adminCookie := h.session(admin)
	response := h.do(request{method: http.MethodGet, path: "/api/v1/settings", cookie: adminCookie})
	response.mustStatus(t, http.StatusOK, "settings")
	if !strings.Contains(string(response.body), "scheduler.default_runner_limits_off") {
		t.Fatal("settings omitted matching signal")
	}
	h.do(request{method: http.MethodPatch, path: "/api/v1/settings", cookie: adminCookie, body: map[string]any{"scheduler.default_runner_limits": true}}).mustStatus(t, http.StatusOK, "admin fix")
}
