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

// The Overview is where an operator looks after clearing the queue, and the
// queue depth it draws has to have moved. It used to count every row GitHub
// still called queued, so a fleet whose queue had just been emptied went on
// reporting the number it had before -- the removal looked like a button that
// did nothing. The pool's own count is the same claim in a second place.
func TestRemovingQueuedItemsEmptiesTheOverviewsQueueDepth(t *testing.T) {
	h := newHarness(t)
	inst := h.installation()
	pool := h.pool(inst, "linux")
	first, second := h.job(pool, store.JobQueued), h.job(pool, store.JobQueued)
	_, operator := h.user("operator", store.RoleOperator)

	stats := func() (queued, fleetQueued, poolQueued int) {
		t.Helper()
		resp := h.do(request{method: "GET", path: "/api/v1/stats", cookie: operator})
		resp.mustStatus(t, http.StatusOK, "stats")
		var out struct {
			QueuedJobs int `json:"queued_jobs"`
			Fleet      struct {
				QueuedJobs int `json:"queued_jobs"`
			} `json:"fleet"`
			Pools []struct {
				PoolID string `json:"pool_id"`
				Queued int    `json:"queued"`
			} `json:"pools"`
		}
		if err := json.Unmarshal(resp.body, &out); err != nil {
			t.Fatal(err)
		}
		for _, p := range out.Pools {
			if p.PoolID == pool.ID {
				poolQueued = p.Queued
			}
		}
		return out.QueuedJobs, out.Fleet.QueuedJobs, poolQueued
	}

	if q, f, p := stats(); q != 2 || f != 2 || p != 2 {
		t.Fatalf("before removing anything: queued=%d fleet=%d pool=%d, want 2 each", q, f, p)
	}
	body := map[string]any{"ids": []string{first.ID, second.ID}, "action": "delete"}
	h.do(request{method: "POST", path: "/api/v1/provisioning/bulk", body: body, cookie: operator}).
		mustStatus(t, http.StatusOK, "delete")
	if q, f, p := stats(); q != 0 || f != 0 || p != 0 {
		t.Fatalf("after emptying the queue: queued=%d fleet=%d pool=%d, want 0 each", q, f, p)
	}

	// Restoring one brings it back, so the tile is reporting the queue rather
	// than remembering a decision.
	body["ids"], body["action"] = []string{first.ID}, "resume"
	h.do(request{method: "POST", path: "/api/v1/provisioning/bulk", body: body, cookie: operator}).
		mustStatus(t, http.StatusOK, "resume")
	if q, f, p := stats(); q != 1 || f != 1 || p != 1 {
		t.Fatalf("after restoring one: queued=%d fleet=%d pool=%d, want 1 each", q, f, p)
	}
}
