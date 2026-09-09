package api

import (
	"net/http"
	"strings"
	"testing"

	"github.com/eyupio/zoomies/internal/store"
)

// fence puts the harness's fleet behind the fence, the way a restore does.
func (h *harness) fence(t *testing.T, reason string) {
	t.Helper()
	if err := h.st.SetRecoveryFence(h.ctx, true, reason); err != nil {
		t.Fatalf("SetRecoveryFence: %v", err)
	}
	if err := h.ctrl.LoadFence(h.ctx); err != nil {
		t.Fatalf("LoadFence: %v", err)
	}
}

// A fenced instance is serving, is deciding, and is doing none of it -- which
// from outside looks exactly like a healthy fleet with nothing queued. A load
// balancer taking it out of rotation is the correct outcome, and so is a
// deployment that will not go green until somebody has looked.
func TestReadinessFailsWhileTheFleetIsFenced(t *testing.T) {
	h := newHarness(t)

	ready := h.do(request{method: http.MethodGet, path: "/readyz"})
	ready.mustStatus(t, http.StatusOK, "readiness before the fence")

	h.fence(t, "restored from a backup taken on Tuesday")

	fenced := h.do(request{method: http.MethodGet, path: "/readyz"})
	fenced.mustStatus(t, http.StatusServiceUnavailable, "readiness while fenced")
	body := string(fenced.body)
	if !strings.Contains(body, "backup taken on Tuesday") {
		t.Errorf("the probe does not say why it is not ready:\n%s", body)
	}

	// Liveness is deliberately unaffected. The container's health check is
	// /healthz, so a fenced controller must not be restarted by its runtime:
	// that would achieve nothing and lose the operator's session.
	alive := h.do(request{method: http.MethodGet, path: "/healthz"})
	alive.mustStatus(t, http.StatusOK, "liveness while fenced")
}

// Lifting the fence is a person saying a recovered fleet has been checked and
// may act on the world again, which is why it is audited under its own action:
// the audit log is where somebody later asks who decided that, and when.
func TestLiftingTheFenceIsOneAuditedAct(t *testing.T) {
	h := newHarness(t)
	u, _ := h.user("admin", store.RoleAdmin)
	cookie := h.session(u)
	h.fence(t, "restored from /var/backups/zoomies/zoomies-20260908-181718")

	state := h.do(request{method: http.MethodGet, path: "/api/v1/recovery", cookie: cookie})
	state.mustStatus(t, http.StatusOK, "reading the fence")
	var got recoveryResponse
	state.into(t, &got)
	if !got.Fenced || got.Reason == "" {
		t.Fatalf("the fence does not read as on: %+v", got)
	}

	lifted := h.do(request{method: http.MethodPost, path: "/api/v1/recovery/unfence", cookie: cookie})
	lifted.mustStatus(t, http.StatusOK, "lifting the fence")
	lifted.into(t, &got)
	if got.Fenced {
		t.Error("the fence is still on after being lifted")
	}
	if h.ctrl.Fenced().Fenced {
		t.Error("the controller still believes it is fenced")
	}

	rows, _, err := h.st.ListAudit(h.ctx, store.AuditFilter{Actions: []string{"recovery.unfence"}}, store.Page{Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 {
		t.Fatalf("lifting the fence wrote %d audit rows, want 1", len(rows))
	}
	if rows[0].ActorID != u.ID {
		t.Errorf("the audit row does not name who lifted it: %+v", rows[0])
	}

	// Two operators recovering one fleet will both press it, and the second
	// must not be told something went wrong.
	again := h.do(request{method: http.MethodPost, path: "/api/v1/recovery/unfence", cookie: cookie})
	again.mustStatus(t, http.StatusOK, "lifting an unfenced instance")
}
