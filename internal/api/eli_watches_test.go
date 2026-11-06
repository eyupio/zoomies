package api

import (
	"net/http"
	"testing"

	"github.com/eyupio/zoomies/internal/controller"
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

func TestEliWatchStateNamesTheThreeStates(t *testing.T) {
	row := &store.EliRepairWatch{Enabled: true}
	if got := eliWatchState(row); got != "enabled" {
		t.Fatalf("enabled state = %q", got)
	}
	row = &store.EliRepairWatch{StopReason: "not this one"}
	if got := eliWatchState(row); got != "stopped" {
		t.Fatalf("stopped state = %q", got)
	}
	row = &store.EliRepairWatch{PausedReason: "later"}
	if got := eliWatchState(row); got != "paused" {
		t.Fatalf("paused state = %q", got)
	}
}

func TestEliWatchesListFiltersByStateAndPagesPastTheEnd(t *testing.T) {
	h := newHarness(t)
	inst := h.installation()
	own := watchRow(t, h, inst, "acme/one", 1, "usr_owner")
	watchRow(t, h, inst, "acme/two", 2, "usr_somebody")
	if err := h.ctrl.PauseEliRepairWatch(h.ctx, own.ID, "later"); err != nil {
		t.Fatal(err)
	}
	admin := h.token("admin", store.RoleAdmin)

	paused := h.do(request{method: http.MethodGet, path: "/api/v1/assistant/repairs/watches?state=paused", token: admin})
	paused.mustStatus(t, http.StatusOK, "filtering by paused")
	var body struct {
		Items []map[string]any `json:"items"`
		Total int              `json:"total"`
	}
	paused.into(t, &body)
	if body.Total != 1 || body.Items[0]["id"] != own.ID {
		t.Fatalf("paused filter = %+v", body)
	}

	stopped := h.do(request{method: http.MethodGet, path: "/api/v1/assistant/repairs/watches?state=stopped", token: admin})
	stopped.mustStatus(t, http.StatusOK, "filtering by stopped")
	stopped.into(t, &body)
	if body.Total != 0 {
		t.Fatalf("stopped filter = %+v", body)
	}

	past := h.do(request{method: http.MethodGet, path: "/api/v1/assistant/repairs/watches?offset=99", token: admin})
	past.mustStatus(t, http.StatusOK, "paging past the end")
	past.into(t, &body)
	if body.Total != 2 || len(body.Items) != 0 {
		t.Fatalf("past the end = %+v", body)
	}
}

func TestEliWatchAdmissionAndResumeRefuseWhatTheCallerCannotDo(t *testing.T) {
	h := newHarness(t)
	inst := h.installation()
	row := watchRow(t, h, inst, "acme/one", 1, "usr_owner")
	token := h.token("machine", store.RoleAdmin)

	// A token belongs to no person, so it cannot be the owner of unattended work.
	ownerless := h.do(request{method: http.MethodPost, path: "/api/v1/assistant/repairs/watches", token: token,
		body: map[string]any{"repo": "acme/one", "pull_number": 1, "installation_id": inst.ID, "capabilities": []string{"checks"}}})
	ownerless.mustStatus(t, http.StatusConflict, "admitting a watch without a person")

	badBody := h.do(request{method: http.MethodPost, path: "/api/v1/assistant/repairs/watches", token: token,
		body: map[string]any{"pull_number": "one"}})
	badBody.mustStatus(t, http.StatusBadRequest, "admitting a watch with a malformed body")

	missing := h.do(request{method: http.MethodPost, path: "/api/v1/assistant/repairs/watches/watch_nosuch/resume", token: token})
	missing.mustStatus(t, http.StatusNotFound, "resuming a watch that does not exist")

	refused := h.do(request{method: http.MethodPost, path: "/api/v1/assistant/repairs/watches/" + row.ID + "/resume", token: token})
	refused.mustStatus(t, http.StatusUnprocessableEntity, "resuming a watch whose policy is gone")

	malformed := h.do(request{method: http.MethodPost, path: "/api/v1/assistant/repairs/watches/" + row.ID + "/pause", token: token,
		body: map[string]any{"reason": 7}})
	malformed.mustStatus(t, http.StatusBadRequest, "pausing a watch with a malformed body")
}

func TestWatchCapabilitiesKeepsTheChosenOnes(t *testing.T) {
	if got := watchCapabilities(&store.EliRepairWatch{Checks: true}); len(got) != 1 || got[0] != controller.EliWatchChecks {
		t.Fatalf("checks = %v", got)
	}
	if got := watchCapabilities(&store.EliRepairWatch{Conflicts: true}); len(got) != 1 || got[0] != controller.EliWatchConflicts {
		t.Fatalf("conflicts = %v", got)
	}
	if got := watchCapabilities(&store.EliRepairWatch{Checks: true, Conflicts: true}); len(got) != 2 {
		t.Fatalf("both = %v", got)
	}
	if got := watchCapabilities(&store.EliRepairWatch{}); len(got) != 0 {
		t.Fatalf("neither = %v", got)
	}
}

func TestEliRepositoryWatchesFilterAndRefuseWhatACallerCannotDo(t *testing.T) {
	h := newHarness(t)
	inst := h.installation()
	own, err := h.st.SetEliRepairWatchRepository(h.ctx, store.EliRepairWatchRepository{
		InstallationID: inst.ID, Repo: "acme/one", OwnerID: "usr_owner", Checks: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	other, err := h.st.SetEliRepairWatchRepository(h.ctx, store.EliRepairWatchRepository{
		InstallationID: inst.ID, Repo: "acme/two", OwnerID: "usr_somebody", Conflicts: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := h.ctrl.PauseEliRepositoryWatches(h.ctx, other.ID); err != nil {
		t.Fatal(err)
	}
	admin := h.token("admin", store.RoleAdmin)

	bad := h.do(request{method: http.MethodGet, path: "/api/v1/assistant/repairs/watches/repositories?state=stopped", token: admin})
	bad.mustStatus(t, http.StatusUnprocessableEntity, "an unknown repository state")

	enabled := h.do(request{method: http.MethodGet, path: "/api/v1/assistant/repairs/watches/repositories?state=enabled&repo=acme/one", token: admin})
	enabled.mustStatus(t, http.StatusOK, "filtering repository watches")
	var body struct {
		Items []store.EliRepairWatchRepository `json:"items"`
	}
	enabled.into(t, &body)
	if len(body.Items) != 1 || body.Items[0].ID != own.ID {
		t.Fatalf("enabled repository filter = %+v", body)
	}

	paused := h.do(request{method: http.MethodGet, path: "/api/v1/assistant/repairs/watches/repositories?state=paused&repo=acme/two", token: admin})
	paused.mustStatus(t, http.StatusOK, "filtering paused repository watches")
	paused.into(t, &body)
	if len(body.Items) != 1 || body.Items[0].ID != other.ID {
		t.Fatalf("paused repository filter = %+v", body)
	}

	token := h.token("machine", store.RoleAdmin)
	ownerless := h.do(request{method: http.MethodPost, path: "/api/v1/assistant/repairs/watches/repositories", token: token,
		body: map[string]any{"repo": "acme/one", "installation_id": inst.ID, "capabilities": []string{"checks"}}})
	ownerless.mustStatus(t, http.StatusConflict, "admitting a repository without a person")

	malformed := h.do(request{method: http.MethodPost, path: "/api/v1/assistant/repairs/watches/repositories", token: token,
		body: map[string]any{"repo": 3}})
	malformed.mustStatus(t, http.StatusBadRequest, "a malformed repository body")

	missingResume := h.do(request{method: http.MethodPost, path: "/api/v1/assistant/repairs/watches/repositories/dec_nosuch/resume", token: token})
	missingResume.mustStatus(t, http.StatusNotFound, "resuming a repository decision that does not exist")

	missingPause := h.do(request{method: http.MethodPost, path: "/api/v1/assistant/repairs/watches/repositories/dec_nosuch/pause", token: token})
	if missingPause.status != http.StatusNotFound && missingPause.status != http.StatusUnprocessableEntity {
		t.Fatalf("pausing a missing decision = %d", missingPause.status)
	}
}
