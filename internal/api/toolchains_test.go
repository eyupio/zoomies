package api

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/eyupio/zoomies/internal/store"
)

// Before any scan the answer is an empty reading, not a 404 or a null: a
// client iterating pools and unmatched must not have to special-case "never
// scanned".
func TestToolchainsIsAnEmptyReadingBeforeTheFirstScan(t *testing.T) {
	h := newHarness(t)
	viewer, _ := h.user("viewer", store.RoleViewer)

	resp := h.do(request{method: http.MethodGet, path: "/api/v1/toolchains", cookie: h.session(viewer)})
	if resp.status != http.StatusOK {
		t.Fatalf("status = %d: %s", resp.status, resp.body)
	}
	var got map[string]json.RawMessage
	if err := json.Unmarshal([]byte(resp.body), &got); err != nil {
		t.Fatalf("decoding: %v", err)
	}
	for key, want := range map[string]string{"running": "false", "pools": "{}", "unmatched": "[]", "installations": "[]"} {
		if string(got[key]) != want {
			t.Errorf("%s = %s, want %s", key, got[key], want)
		}
	}
}

// Starting a scan answers at once and the scan is then visible as finished;
// the request does not wait on minutes of GitHub calls.
func TestStartingAToolchainScanAnswersAtOnce(t *testing.T) {
	h := newHarness(t)
	admin, _ := h.user("admin", store.RoleAdmin)

	resp := h.do(request{method: http.MethodPost, path: "/api/v1/toolchains/scan", cookie: h.session(admin)})
	if resp.status != http.StatusAccepted {
		t.Fatalf("status = %d, want 202: %s", resp.status, resp.body)
	}
	deadline := time.Now().Add(10 * time.Second)
	for {
		get := h.do(request{method: http.MethodGet, path: "/api/v1/toolchains", cookie: h.session(admin)})
		var scan struct {
			Running    bool    `json:"running"`
			FinishedAt *string `json:"finished_at"`
		}
		if err := json.Unmarshal([]byte(get.body), &scan); err != nil {
			t.Fatalf("decoding: %v", err)
		}
		if !scan.Running && scan.FinishedAt != nil {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("the scan never finished: %s", get.body)
		}
		time.Sleep(10 * time.Millisecond)
	}
}
