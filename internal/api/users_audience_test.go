package api

import (
	"net/http"
	"testing"

	"github.com/eyupio/zoomies/internal/store"
)

// The platform role is only a line if an administrator cannot step over it.
// Before this was enforced an administrator could create a platform account,
// promote their own, or reset the platform's password and sign in as it --
// each one a single request, and each one the whole of what the role guards.
func TestAnAdministratorCannotGrantOrTakeOverThePlatformRole(t *testing.T) {
	h := newHarness(t)
	admin, _ := h.user("fleet-admin", store.RoleAdmin)
	plat, _ := h.user("operator-of-record", store.RolePlatform)
	cookie := h.session(admin)

	refused := []request{
		{method: http.MethodPost, path: "/api/v1/users", cookie: cookie,
			body: map[string]any{"username": "shadow", "password": "correct horse battery staple", "role": "platform"}},
		{method: http.MethodPatch, path: "/api/v1/users/" + admin.ID, cookie: cookie,
			body: map[string]any{"role": "platform"}},
		{method: http.MethodPost, path: "/api/v1/users/" + plat.ID + "/password", cookie: cookie,
			body: map[string]any{"new_password": "correct horse battery staple"}},
		{method: http.MethodPatch, path: "/api/v1/users/" + plat.ID, cookie: cookie,
			body: map[string]any{"disabled": true}},
		{method: http.MethodDelete, path: "/api/v1/users/" + plat.ID, cookie: cookie},
	}
	for _, req := range refused {
		resp := h.do(req)
		if resp.status != http.StatusForbidden {
			t.Errorf("%s %s as admin = %d, want 403: %s", req.method, req.path, resp.status, resp.body)
		}
	}
	if u, err := h.st.GetUser(h.ctx, admin.ID); err != nil || u.Role != store.RoleAdmin {
		t.Errorf("the administrator's role is now %v (err %v); it should still be admin", u.Role, err)
	}

	// The fleet's own accounts are still the administrator's to manage, so
	// the refusal is about the role and not about accounts in general.
	ok := h.do(request{method: http.MethodPost, path: "/api/v1/users", cookie: cookie,
		body: map[string]any{"username": "new-operator", "password": "correct horse battery staple", "role": "operator"}})
	if ok.status != http.StatusCreated {
		t.Errorf("an administrator could not create an operator: %d %s", ok.status, ok.body)
	}

	// And the platform may grant its own role.
	granted := h.do(request{method: http.MethodPost, path: "/api/v1/users", cookie: h.session(plat),
		body: map[string]any{"username": "second-platform", "password": "correct horse battery staple", "role": "platform"}})
	if granted.status != http.StatusCreated {
		t.Errorf("the platform could not create a platform account: %d %s", granted.status, granted.body)
	}
}
