package api

import (
	"encoding/json"
	"github.com/eyupio/zoomies/internal/store"
	"net/http"
	"strings"
	"testing"
)

func TestProvisioningAPIControlsOnlyDemand(t *testing.T) {
	h := newHarness(t)
	inst := h.installation()
	pool := h.pool(inst, "linux")
	job := h.job(pool, store.JobQueued)
	_, operator := h.user("operator", store.RoleOperator)
	_, viewer := h.user("viewer", store.RoleViewer)
	body := map[string]any{"ids": []string{job.ID}, "action": "pause"}
	h.do(request{method: "POST", path: "/api/v1/provisioning/bulk", body: body, cookie: viewer}).mustStatus(t, http.StatusForbidden, "viewer control")
	before := len(h.gh.Requests())
	for _, action := range []string{"pause", "run_now", "delete", "resume"} {
		body["action"] = action
		resp := h.do(request{method: "POST", path: "/api/v1/provisioning/bulk", body: body, cookie: operator})
		resp.mustStatus(t, http.StatusOK, action)
		var result struct {
			Results []store.ProvisioningResult `json:"results"`
		}
		if err := json.Unmarshal(resp.body, &result); err != nil {
			t.Fatal(err)
		}
		if len(result.Results) != 1 || !result.Results[0].OK {
			t.Fatalf("%s: %s", action, resp.body)
		}
		got, err := h.st.GetJob(h.ctx, job.ID)
		if err != nil || got.State != store.JobQueued {
			t.Fatalf("GitHub state changed: %+v %v", got, err)
		}
		view := h.do(request{method: "GET", path: "/api/v1/jobs/" + job.ID, cookie: viewer})
		var wire map[string]any
		if err := json.Unmarshal(view.body, &wire); err != nil {
			t.Fatal(err)
		}
		if wire["provisioning"] != got.Provisioning || wire["provision_now"] != got.ProvisionNow {
			t.Fatalf("operator state missing from API: %s", view.body)
		}
		if action == "pause" {
			explain := h.do(request{method: "GET", path: "/api/v1/jobs/" + job.ID + "/explanation", cookie: viewer})
			if !strings.Contains(string(explain.body), "Provisioning is paused") {
				t.Fatalf("explanation: %s", explain.body)
			}
		}
	}
	if len(h.gh.Requests()) != before {
		t.Fatal("provisioning controls called GitHub")
	}
	for _, query := range []string{"?state=completed&managed=false", "?provisioning=ready"} {
		resp := h.do(request{method: "GET", path: "/api/v1/provisioning" + query, cookie: viewer})
		resp.mustStatus(t, 200, "queue")
		var page struct {
			Items  []store.Job    `json:"items"`
			Counts map[string]int `json:"counts"`
		}
		if err := json.Unmarshal(resp.body, &page); err != nil {
			t.Fatal(err)
		}
		if len(page.Items) != 1 || page.Items[0].ID != job.ID || page.Counts["ready"] != 1 {
			t.Fatalf("queue scope: %s", resp.body)
		}
	}
	h.do(request{method: "GET", path: "/api/v1/provisioning?provisioning=bogus", cookie: viewer}).mustStatus(t, 400, "invalid provisioning state")
	h.do(request{method: "GET", path: "/api/v1/provisioning/selection?repo=other/repo", cookie: viewer}).mustStatus(t, 200, "empty selection")
}
