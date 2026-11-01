package api

import (
	"encoding/json"
	"github.com/eyupio/zoomies/internal/controller"
	"github.com/eyupio/zoomies/internal/store"
	"testing"
)

func TestEliManualPolicyCanBeSavedWithoutAnInstallationProvider(t *testing.T) {
	h := newHarness(t)
	inst := h.installation()
	_, admin := h.user("repair-admin", store.RoleAdmin)
	body := map[string]any{"repo": inst.Target + "/repo", "installation_id": inst.ID, "provider_id": "", "enabled": true, "automatic": false, "ask_first": true, "allow_workflows": false}
	h.do(request{method: "PUT", path: "/api/v1/assistant/repairs/policy", cookie: admin, body: body}).mustStatus(t, 200, "manual-only setup needs no installation payer")
	body["automatic"] = true
	body["ask_first"] = false
	h.do(request{method: "PUT", path: "/api/v1/assistant/repairs/policy", cookie: admin, body: body}).mustStatus(t, 422, "automatic repairs need an installation payer")
	policies, err := h.st.EliRepairPolicies(h.ctx)
	if err != nil || len(policies) != 1 || !policies[0].AskFirst {
		t.Fatalf("policy %+v %v", policies, err)
	}
}
func TestEliReadinessExplainsMissingSetupWithoutChargingAnAttempt(t *testing.T) {
	h := newHarness(t)
	_, viewer := h.user("repair-viewer", store.RoleViewer)
	response := h.do(request{method: "GET", path: "/api/v1/assistant/repairs/readiness?repo=acme/repo&pull_number=1", cookie: viewer})
	response.mustStatus(t, 200, "readiness is a diagnosis")
	var result struct {
		Ready    bool `json:"ready"`
		Blockers []struct {
			Code string `json:"code"`
		} `json:"blockers"`
	}
	if err := json.Unmarshal(response.body, &result); err != nil {
		t.Fatal(err)
	}
	if result.Ready || len(result.Blockers) < 2 {
		t.Fatalf("readiness %+v", result)
	}
	rows, _, err := h.st.ListEliRepairsPage(h.ctx, store.EliRepairFilter{Limit: store.MaxEliRepairPage})
	if err != nil || len(rows) != 0 {
		t.Fatal("readiness spent an attempt")
	}
}

// The readiness answer is read by the UI through a client generated from the
// OpenAPI document, so a check or a pull request field the document does not
// describe is one the page cannot see. The checks are always the same eight, in
// the order they are made, whatever stopped the run.
func TestEliReadinessAlwaysReturnsEightChecksInTheSpecsShape(t *testing.T) {
	h := newHarness(t)
	_, viewer := h.user("repair-viewer", store.RoleViewer)
	response := h.do(request{method: "GET", path: "/api/v1/assistant/repairs/readiness?repo=acme/repo&pull_number=1", cookie: viewer})
	response.mustStatus(t, 200, "readiness is a diagnosis")
	doc := loadSpec(t)
	assertShape(t, doc, "EliReadiness", json.RawMessage(response.body))
	var answer struct {
		Pull   *json.RawMessage  `json:"pull"`
		Checks []json.RawMessage `json:"checks"`
	}
	if err := json.Unmarshal(response.body, &answer); err != nil {
		t.Fatal(err)
	}
	if answer.Pull != nil {
		t.Fatal("a pull request was returned to someone whose GitHub account was never verified")
	}
	want := []string{"identity", "provider", "installation", "access", "policy", "pull", "checks", "permissions"}
	if len(answer.Checks) != len(want) {
		t.Fatalf("%d checks, want %d: %s", len(answer.Checks), len(want), response.body)
	}
	for i, raw := range answer.Checks {
		assertShape(t, doc, "EliReadinessCheck", raw)
		var check struct{ Code, Status, Blocker string }
		if err := json.Unmarshal(raw, &check); err != nil {
			t.Fatal(err)
		}
		if check.Code != want[i] || check.Status == "" {
			t.Errorf("check %d is %+v, want code %s", i, check, want[i])
		}
		// A blocked check says which blocker stopped it, and nothing else does, so
		// the page can tell GitHub not answering from the check it was making.
		if (check.Status == "blocked") != (check.Blocker != "") {
			t.Errorf("check %d is %s and names blocker %q", i, check.Status, check.Blocker)
		}
	}
	pull, err := json.Marshal(controller.EliReadinessPull{Number: 1, Title: "t", Author: "a", Branch: "b", Base: "main", URL: "https://github.com/acme/repo/pull/1", Open: true})
	if err != nil {
		t.Fatal(err)
	}
	assertShape(t, doc, "EliReadinessPull", pull)
	// The mode is only present once readiness reaches the policy, which an
	// unlinked caller never does, so the document is checked against an answer
	// that carries it.
	mode, err := json.Marshal(controller.EliReadiness{Repo: "acme/repo", PolicyMode: "automatic", Blockers: []controller.EliBlocker{}, Checks: []controller.EliCheck{}})
	if err != nil {
		t.Fatal(err)
	}
	assertShape(t, doc, "EliReadiness", mode)
}

// The page that starts a repair knows which run it is looking at and says so.
// The request is decoded strictly, so a field that is not part of it is refused
// before anything else is read: the run id has to be accepted, and then the
// request meets the same readiness gate as any other.
func TestRequestingARepairAcceptsARunID(t *testing.T) {
	h := newHarness(t)
	_, viewer := h.user("repair-viewer", store.RoleViewer)
	body := map[string]any{"repo": "acme/repo", "pull_number": 1, "instruction": "", "head_sha": "abc123", "run_id": 900}
	response := h.do(request{method: "POST", path: "/api/v1/assistant/repairs", cookie: viewer, body: body})
	response.mustStatus(t, 409, "a request that names its run is read like any other and stopped by readiness")
	rows, _, err := h.st.ListEliRepairsPage(h.ctx, store.EliRepairFilter{Limit: store.MaxEliRepairPage})
	if err != nil || len(rows) != 0 {
		t.Fatalf("a refused request left %d repairs: %v", len(rows), err)
	}
}

// lift is one field of a JSON object, as the JSON it holds, so a test can check
// the shape of each level of a response against its own schema.
func lift(t *testing.T, object json.RawMessage, field string) json.RawMessage {
	t.Helper()
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(object, &fields); err != nil {
		t.Fatalf("not an object: %v: %s", err, object)
	}
	value, ok := fields[field]
	if !ok {
		t.Fatalf("no %q in %s", field, object)
	}
	return value
}

// The page that follows a repair reads its case through a client generated from
// the OpenAPI document, so every field of the case, and of each job in it, must
// be one the document describes. assertShape looks one object deep, so each
// level is lifted out and checked on its own.
func TestARepairsCaseMatchesTheSpecShapes(t *testing.T) {
	h := newHarness(t)
	inst := h.installation()
	_, admin := h.user("repair-admin", store.RoleAdmin)
	row := &store.EliRepair{
		DedupKey: "case-shape", InstallationID: inst.ID, Repo: inst.Target + "/repo", PullNumber: 1, RunID: 900, Trigger: "mention",
		Details: store.EliRepairDetails{Case: &store.EliRepairCase{
			PullTitle: "Retry transient 502s", PullURL: "https://github.com/acme/repo/pull/1", Author: "dana", Branch: "feature",
			RunURL: "https://github.com/acme/repo/actions/runs/900", Workflow: "CI", RunNumber: 1042,
			Jobs: []store.EliRepairCaseJob{{Name: "unit", Step: "Run go test ./...", URL: "https://github.com/acme/repo/actions/runs/900/job/11", DurationMS: 95000}},
		}},
	}
	if added, err := h.st.EnqueueEliRepair(h.ctx, row); err != nil || !added {
		t.Fatalf("seeding the repair: added=%v, %v", added, err)
	}
	response := h.do(request{method: "GET", path: "/api/v1/assistant/repairs", cookie: admin})
	response.mustStatus(t, 200, "reading the history")
	var page []json.RawMessage
	if err := json.Unmarshal(lift(t, response.body, "items"), &page); err != nil || len(page) != 1 {
		t.Fatalf("history: %d items, %v", len(page), err)
	}
	details := lift(t, page[0], "details")
	repairCase := lift(t, details, "case")
	var jobs []json.RawMessage
	if err := json.Unmarshal(lift(t, repairCase, "jobs"), &jobs); err != nil || len(jobs) != 1 {
		t.Fatalf("the case has %d jobs, %v", len(jobs), err)
	}
	doc := loadSpec(t)
	assertShape(t, doc, "EliRepair", page[0])
	assertShape(t, doc, "EliRepairDetails", details)
	assertShape(t, doc, "EliRepairCase", repairCase)
	assertShape(t, doc, "EliRepairCaseJob", jobs[0])
}
