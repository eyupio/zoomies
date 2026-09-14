package api

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/eyupio/zoomies/internal/store"
)

// throttledHost is a host on the second rung, as the controller would have
// left it after two steps of sustained pressure. The row is written directly:
// the ladder is the scheduler's and is tested there, and what these tests
// are about is what an operator can do to the rung once it exists.
func throttledHost(t *testing.T, h *harness) *store.Host {
	t.Helper()
	host := h.host("vm-1")
	host.CPUs, host.MemoryMB = 8, 16*1024
	if err := h.st.UpdateHost(h.ctx, host); err != nil {
		t.Fatalf("UpdateHost: %v", err)
	}
	since := time.Now().Add(-7 * time.Minute)
	changed := time.Now().Add(-3 * time.Minute)
	throttle := store.HostThrottle{
		Level: 2, Since: &since, ChangedAt: &changed,
		Reason: "the 1-minute load average is 30.0, at least twice the host's 8 CPUs",
	}
	if err := h.st.SetHostThrottle(h.ctx, host.ID, throttle, 0); err != nil {
		t.Fatalf("SetHostThrottle: %v", err)
	}
	host.Throttle = throttle
	return host
}

// lastAudit is the newest audit row for an action, or nil.
func lastAudit(t *testing.T, h *harness, action string) *store.AuditEvent {
	t.Helper()
	rows, _, err := h.st.ListAudit(h.ctx, store.AuditFilter{Actions: []string{action}}, store.Page{Limit: 1})
	if err != nil {
		t.Fatalf("ListAudit(%s): %v", action, err)
	}
	if len(rows) == 0 {
		return nil
	}
	return rows[0]
}

// A throttle lifts itself after five minutes of calm, which is the right
// answer for a host nobody is looking at and the wrong one for a host whose
// operator has just cancelled the runaway job: they know the pressure is gone
// and should not have to wait for the fleet to notice. The route is the
// difference, and the audit row is what says who decided the fleet was wrong.
func TestAnOperatorCanLiftAHostsThrottle(t *testing.T) {
	h := newHarness(t)
	host := throttledHost(t, h)
	operator, _ := h.user("operator", store.RoleOperator)

	before := h.do(request{method: http.MethodGet, path: "/api/v1/hosts/" + host.ID, cookie: h.session(operator)})
	before.mustStatus(t, http.StatusOK, "read the throttled host")
	var was hostResponse
	before.into(t, &was)
	if was.Throttle == nil || was.Throttle.Level != 2 || was.EffectiveCapacity != 2 || was.ThrottleReason == "" {
		t.Fatalf("the view does not show the throttle it was given: %+v", was)
	}

	resp := h.do(request{method: http.MethodPost, path: "/api/v1/hosts/" + host.ID + "/throttle/clear",
		cookie: h.session(operator)})
	resp.mustStatus(t, http.StatusOK, "lift the throttle")
	var view hostResponse
	resp.into(t, &view)
	if view.Throttle != nil || view.ThrottleReason != "" {
		t.Errorf("the response still carries a throttle: %+v", view)
	}
	if view.EffectiveCapacity != view.Capacity || view.Capacity != 4 {
		t.Errorf("effective capacity = %d of %d, want the configured 4 of 4 once the throttle is lifted",
			view.EffectiveCapacity, view.Capacity)
	}

	// It survives the write: the next scheduling pass reads the row, not the
	// response.
	stored, err := h.st.GetHost(h.ctx, host.ID)
	if err != nil {
		t.Fatalf("GetHost: %v", err)
	}
	if stored.Throttle.Active() {
		t.Fatalf("the stored host is still throttled: %+v", stored.Throttle)
	}

	// The audit row is under the operator, not the system: a colleague reading
	// the log should see who lifted it early and what rung it was on.
	row := lastAudit(t, h, "host.throttle_clear")
	if row == nil {
		t.Fatal("no host.throttle_clear audit row was written")
	}
	if row.TargetKind != "host" || row.TargetID != host.ID || row.ActorID != operator.ID {
		t.Errorf("audit row = kind %q target %q actor %q, want host %s by %s",
			row.TargetKind, row.TargetID, row.ActorID, host.ID, operator.ID)
	}
	var detail struct {
		Name              string `json:"name"`
		Level             int    `json:"level"`
		Reason            string `json:"reason"`
		EffectiveCapacity int    `json:"effective_capacity"`
	}
	if err := json.Unmarshal([]byte(row.After), &detail); err != nil {
		t.Fatalf("the audit detail is not JSON: %v: %s", err, row.After)
	}
	if detail.Name != host.Name || detail.Level != 2 || detail.Reason == "" || detail.EffectiveCapacity != 2 {
		t.Errorf("the audit detail does not describe the throttle it cleared: %s", row.After)
	}

	// Lifting a throttle that is not there is answered with the host as it is
	// and writes no second row, exactly as uncordoning an uncordoned host does.
	again := h.do(request{method: http.MethodPost, path: "/api/v1/hosts/" + host.ID + "/throttle/clear",
		cookie: h.session(operator)})
	again.mustStatus(t, http.StatusOK, "lift a throttle that is not there")
	rows, _, err := h.st.ListAudit(h.ctx, store.AuditFilter{Actions: []string{"host.throttle_clear"}}, store.Page{Limit: 10})
	if err != nil {
		t.Fatalf("ListAudit: %v", err)
	}
	if len(rows) != 1 {
		t.Errorf("a no-op clear wrote an audit row: %d rows", len(rows))
	}
}

// Lifting a throttle changes what the fleet places on a host, which is an
// operator's decision and not a viewer's; and a host that does not exist is
// a 404, not a 500, because the id came from somewhere and that somewhere
// is stale.
func TestLiftingAThrottleIsForOperatorsAndNamedHosts(t *testing.T) {
	h := newHarness(t)
	host := throttledHost(t, h)
	viewer, _ := h.user("viewer", store.RoleViewer)
	operator, _ := h.user("operator", store.RoleOperator)

	refused := h.do(request{method: http.MethodPost, path: "/api/v1/hosts/" + host.ID + "/throttle/clear",
		cookie: h.session(viewer)})
	refused.mustStatus(t, http.StatusForbidden, "a viewer lifting a throttle")
	stored, err := h.st.GetHost(h.ctx, host.ID)
	if err != nil {
		t.Fatalf("GetHost: %v", err)
	}
	if !stored.Throttle.Active() {
		t.Fatal("a refused request lifted the throttle anyway")
	}
	if row := lastAudit(t, h, "host.throttle_clear"); row != nil {
		t.Errorf("a refused request wrote an audit row: %+v", row)
	}

	missing := h.do(request{method: http.MethodPost, path: "/api/v1/hosts/host_nope/throttle/clear",
		cookie: h.session(operator)})
	missing.mustStatus(t, http.StatusNotFound, "a host that does not exist")
}

// A change to the capacity or the reserve is the operator answering the
// pressure that throttled the host, so it lifts the rung with it; left
// standing, the new figures would take effect only once the old episode had
// spent five minutes calm. A label says nothing about the machine and lifts
// nothing.
func TestChangingCapacityOrTheReserveLiftsTheThrottle(t *testing.T) {
	cases := []struct {
		name  string
		body  map[string]any
		lifts bool
		// audited is whether the PATCH changed anything at all: a request
		// that repeats the host's own figures writes no host.update row.
		audited bool
	}{
		{"capacity", map[string]any{"capacity": 3}, true, true},
		{"a CPU reserve", map[string]any{"reserve_cpus": 2}, true, true},
		{"a memory reserve", map[string]any{"reserve_memory_mb": 4096}, true, true},
		{"labels alone", map[string]any{"labels": map[string]string{"zone": "eu"}}, false, true},
		// The edit dialog sends every field it shows, so an operator who
		// added a label sends the capacity too; only a figure that moved is
		// an answer to the pressure, and the same figure again is not.
		{"the capacity it already has", map[string]any{"capacity": 4}, false, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := newHarness(t)
			host := throttledHost(t, h)
			operator, _ := h.user("operator", store.RoleOperator)

			resp := h.do(request{method: http.MethodPatch, path: "/api/v1/hosts/" + host.ID,
				cookie: h.session(operator), body: tc.body})
			resp.mustStatus(t, http.StatusOK, "patch the host")
			var view hostResponse
			resp.into(t, &view)

			stored, err := h.st.GetHost(h.ctx, host.ID)
			if err != nil {
				t.Fatalf("GetHost: %v", err)
			}
			if stored.Throttle.Active() == tc.lifts {
				t.Fatalf("stored throttle active = %v after changing %s, want %v", stored.Throttle.Active(), tc.name, !tc.lifts)
			}
			if (view.Throttle == nil) != tc.lifts {
				t.Errorf("the response's throttle does not match the row: %+v", view.Throttle)
			}

			// The PATCH's own audit row says so, rather than leaving a reader
			// to work out that {} against {level: 2} means "lifted".
			row := lastAudit(t, h, "host.update")
			if !tc.audited {
				if row != nil {
					t.Fatalf("a PATCH that changed nothing wrote an audit row: %+v", row)
				}
				return
			}
			if row == nil {
				t.Fatal("no host.update audit row was written")
			}
			var after map[string]any
			if err := json.Unmarshal([]byte(row.After), &after); err != nil {
				t.Fatalf("the audit's after is not JSON: %v: %s", err, row.After)
			}
			if _, ok := after["throttle_cleared"]; ok != tc.lifts {
				t.Errorf("throttle_cleared present = %v in the audit detail, want %v: %s", ok, tc.lifts, row.After)
			}
			if lifted := lastAudit(t, h, "host.throttle_clear"); lifted != nil {
				t.Errorf("a PATCH wrote a host.throttle_clear row of its own; the lift is the PATCH's to record: %+v", lifted)
			}
		})
	}
}
