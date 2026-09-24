package api

import (
	"net/http"
	"strings"
	"testing"

	"github.com/eyupio/zoomies/internal/auth"
	"github.com/eyupio/zoomies/internal/store"
)

// A provisioner asks one endpoint "can I use this yet". Before the first
// account the answer is still a 200 -- the first-run page is exactly what an
// empty instance should serve, and a load balancer hiding it would strand the
// person who needs it -- but it says nobody can sign in.
func TestReadinessSaysWhetherTheFirstAccountExists(t *testing.T) {
	h := newHarness(t)

	before := h.do(request{method: http.MethodGet, path: "/readyz"})
	before.mustStatus(t, http.StatusOK, "readiness on an empty instance")
	if got, ok := before.json(t)["bootstrap_required"].(bool); !ok || !got {
		t.Fatalf("an instance with no account reports bootstrap_required %v; want true", before.json(t)["bootstrap_required"])
	}

	if _, _, err := h.ctrl.Auth().CreateUnattendedIdentity(h.ctx, auth.Unattended{Username: "ops", Password: testPassword}); err != nil {
		t.Fatalf("CreateUnattendedIdentity: %v", err)
	}

	after := h.do(request{method: http.MethodGet, path: "/readyz"})
	after.mustStatus(t, http.StatusOK, "readiness once an account exists")
	if got, ok := after.json(t)["bootstrap_required"].(bool); !ok || got {
		t.Fatalf("an instance with an account reports bootstrap_required %v; want false", after.json(t)["bootstrap_required"])
	}
}

// The first-run page is the person's path, and its row has to say so, so the
// audit log answers "how was this instance claimed" whichever way it was.
func TestTheSetupTokenPathAuditsItsMethod(t *testing.T) {
	h := newHarness(t)
	resp := h.do(request{method: http.MethodPost, path: "/api/v1/auth/bootstrap", body: map[string]any{
		"username": "root", "password": testPassword, "setup_token": h.setupToken(),
	}})
	resp.mustStatus(t, http.StatusCreated, "bootstrap with the setup token")

	rows, _, err := h.st.ListAudit(h.ctx, store.AuditFilter{Actions: []string{"auth.bootstrap"}}, store.Page{Limit: 10})
	if err != nil {
		t.Fatalf("ListAudit: %v", err)
	}
	if len(rows) != 1 || !strings.Contains(rows[0].After, auth.BootstrapSetupToken) || rows[0].ActorName != "root" {
		t.Fatalf("got %d rows, the first %+v; want one by root naming the setup token", len(rows), rows)
	}
}
