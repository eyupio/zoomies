package api

import (
	"net/http"
	"testing"

	"github.com/eyupio/zoomies/internal/store"
)

func watchRow(t *testing.T, h *harness, inst *store.Installation, repo string, pull int, owner string) *store.EliRepairWatch {
	t.Helper()
	row, err := h.st.AdmitEliRepairWatch(h.ctx, store.EliRepairWatch{
		InstallationID: inst.ID, Repo: repo, PullNumber: pull, OwnerID: owner,
		PolicyProviderID: "prv_team", Checks: true, Conflicts: true,
		HeadSHA: "head", BaseRef: "main", BaseSHA: "base",
	})
	if err != nil {
		t.Fatalf("AdmitEliRepairWatch: %v", err)
	}
	return row
}

func TestEliWatchesListFiltersAndNeverLeaksTheLease(t *testing.T) {
	h := newHarness(t)
	inst := h.installation()
	one := watchRow(t, h, inst, "acme/one", 1, "usr_owner")
	watchRow(t, h, inst, "acme/two", 2, "usr_owner")
	admin := h.token("admin", store.RoleAdmin)

	list := h.do(request{method: http.MethodGet, path: "/api/v1/assistant/repairs/watches", token: admin})
	list.mustStatus(t, http.StatusOK, "listing watches")
	var body struct {
		Items []map[string]any `json:"items"`
		Total int              `json:"total"`
	}
	list.into(t, &body)
	if body.Total != 2 || len(body.Items) != 2 {
		t.Fatalf("list = %+v", body)
	}
	if _, leaked := body.Items[0]["lease_token"]; leaked {
		t.Error("the watch list carries an observation lease")
	}

	filtered := h.do(request{method: http.MethodGet, path: "/api/v1/assistant/repairs/watches?repo=acme/one", token: admin})
	filtered.mustStatus(t, http.StatusOK, "filtering watches")
	filtered.into(t, &body)
	if body.Total != 1 || len(body.Items) != 1 || body.Items[0]["id"] != one.ID {
		t.Fatalf("filtered list = %+v", body)
	}

	bad := h.do(request{method: http.MethodGet, path: "/api/v1/assistant/repairs/watches?state=asleep", token: admin})
	bad.mustStatus(t, http.StatusUnprocessableEntity, "an unknown watch state")
}

func TestEliWatchPauseAndStopAreRecorded(t *testing.T) {
	h := newHarness(t)
	inst := h.installation()
	row := watchRow(t, h, inst, "acme/one", 1, "usr_owner")
	admin := h.token("admin", store.RoleAdmin)

	paused := h.do(request{method: http.MethodPost, path: "/api/v1/assistant/repairs/watches/" + row.ID + "/pause", token: admin,
		body: map[string]any{"reason": "waiting for a release"}})
	paused.mustStatus(t, http.StatusOK, "pausing a watch")
	var got store.EliRepairWatch
	paused.into(t, &got)
	if got.Enabled || got.PausedReason != "waiting for a release" {
		t.Fatalf("paused watch = %+v", got)
	}

	stopped := h.do(request{method: http.MethodPost, path: "/api/v1/assistant/repairs/watches/" + row.ID + "/stop", token: admin,
		body: map[string]any{"reason": "not this PR"}})
	stopped.mustStatus(t, http.StatusOK, "stopping a watch")
	stopped.into(t, &got)
	if got.Enabled || got.StopReason != "not this PR" {
		t.Fatalf("stopped watch = %+v", got)
	}

	missing := h.do(request{method: http.MethodPost, path: "/api/v1/assistant/repairs/watches/watch_nosuch/pause", token: admin,
		body: map[string]any{}})
	missing.mustStatus(t, http.StatusNotFound, "pausing a watch that does not exist")
}

func TestEliRepositoryWatchesListAndPauseWithoutStoppingAdmittedPRs(t *testing.T) {
	h := newHarness(t)
	inst := h.installation()
	decision, err := h.st.SetEliRepairWatchRepository(h.ctx, store.EliRepairWatchRepository{
		InstallationID: inst.ID, Repo: "acme/one", OwnerID: "usr_owner", Checks: true, Conflicts: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	admin := h.token("admin", store.RoleAdmin)

	list := h.do(request{method: http.MethodGet, path: "/api/v1/assistant/repairs/watches/repositories?state=enabled", token: admin})
	list.mustStatus(t, http.StatusOK, "listing repository watches")
	var body struct {
		Items []store.EliRepairWatchRepository `json:"items"`
	}
	list.into(t, &body)
	if len(body.Items) != 1 || body.Items[0].ID != decision.ID {
		t.Fatalf("repository watches = %+v", body)
	}

	paused := h.do(request{method: http.MethodPost, path: "/api/v1/assistant/repairs/watches/repositories/" + decision.ID + "/pause", token: admin})
	paused.mustStatus(t, http.StatusOK, "pausing a repository watch")
	var got store.EliRepairWatchRepository
	paused.into(t, &got)
	if got.Enabled || got.ID != decision.ID {
		t.Fatalf("paused repository watch = %+v", got)
	}
}
