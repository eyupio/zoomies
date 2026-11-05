package api

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/eyupio/zoomies/internal/store"
)

// The view is figures the metrics endpoint already publishes, with the machine
// reading beside them, so it is read by whoever runs the process and by nobody
// else, which a test further down holds. It is
// answered as lists and not nulls so that a client can loop over it, and it counts
// the requests made to it like any others, which is how a busy route shows up.
func TestTheControllersLoadIsServedAsListsAndCountsItsOwnRequests(t *testing.T) {
	h := newHarness(t)
	viewer := h.token("platform", store.RolePlatform)
	path := "/api/v1/controller/load"

	// The first request is counted once it has finished, so the second is the one
	// that can see it.
	h.do(request{method: http.MethodGet, path: path, token: viewer}).mustStatus(t, http.StatusOK, "platform")
	res := h.do(request{method: http.MethodGet, path: path, token: viewer})
	res.mustStatus(t, http.StatusOK, "platform, second request")

	var got struct {
		StartedAt       string            `json:"started_at"`
		UptimeSeconds   *float64          `json:"uptime_seconds"`
		Goroutines      int               `json:"goroutines"`
		ReconcilePasses map[string]uint64 `json:"reconcile_passes"`
		Steps           []map[string]any  `json:"steps"`
		API             []struct {
			Group    string `json:"group"`
			Requests uint64 `json:"requests"`
		} `json:"api"`
		APIGroupsNotShown *int           `json:"api_groups_not_shown"`
		Machine           map[string]any `json:"machine"`
	}
	if err := json.Unmarshal(res.body, &got); err != nil {
		t.Fatalf("%v: %s", err, truncate(res.body))
	}
	if got.StartedAt == "" || got.UptimeSeconds == nil || got.Goroutines < 1 || got.APIGroupsNotShown == nil ||
		got.Steps == nil || got.ReconcilePasses == nil || got.Machine == nil {
		t.Fatalf("answer = %s, want every field present and the lists not null", truncate(res.body))
	}
	var counted uint64
	for _, g := range got.API {
		if g.Group == "controller" {
			counted = g.Requests
		}
	}
	if counted < 1 {
		t.Fatalf("api = %+v, want the first request to this route counted under the controller group", got.API)
	}

	h.do(request{method: http.MethodGet, path: path}).mustStatus(t, http.StatusUnauthorized, "no credentials")
}

// The machine's figures are the operator's capacity, not the fleet's, so a fleet's
// own administrator is refused them.
func TestTheControllersLoadIsRefusedToAnyoneBelowThePlatformRole(t *testing.T) {
	h := newHarness(t)
	for _, role := range []store.Role{store.RoleViewer, store.RoleOperator, store.RoleAdmin} {
		h.do(request{method: http.MethodGet, path: "/api/v1/controller/load", token: h.token(string(role), role)}).
			mustStatus(t, http.StatusForbidden, string(role))
	}
}
