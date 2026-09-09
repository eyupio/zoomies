package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

// Emptying a host is two calls in one order, and the order is the whole point:
// draining an uncordoned host means the scheduler puts fresh runners on it
// while the old ones are still finishing, and an operator watching the count go
// down and back up concludes the drain failed.
func TestDrainingAHostCordonsItFirst(t *testing.T) {
	var mu sync.Mutex
	var calls []string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		calls = append(calls, r.Method+" "+r.URL.Path)
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.HasSuffix(r.URL.Path, "/cordon"):
			_, _ = w.Write([]byte(`{"id":"hst_a","name":"vm-1","cordoned":true}`))
		case r.URL.Path == "/api/v1/runners":
			if got := r.URL.Query().Get("host_id"); got != "hst_a" {
				t.Errorf("the runner list was not scoped to the host: %q", got)
			}
			_, _ = w.Write([]byte(`{"items":[{"id":"run_1"},{"id":"run_2"}],"total":2}`))
		case r.URL.Path == "/api/v1/runners/bulk":
			var body struct {
				Action string   `json:"action"`
				IDs    []string `json:"ids"`
				Force  bool     `json:"force"`
			}
			_ = json.NewDecoder(r.Body).Decode(&body)
			if body.Action != "drain" {
				t.Errorf("bulk action = %q, want drain", body.Action)
			}
			// Never forces: a drained runner finishes the job it is on, and an
			// operator who wants the machine now types something else.
			if body.Force {
				t.Error("draining a host forced its runners, interrupting live jobs")
			}
			_, _ = w.Write([]byte(`{"results":[{"id":"run_1","ok":true},{"id":"run_2","ok":true}]}`))
		default:
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
		}
	}))
	defer srv.Close()

	e, out, errOut := newTestEnv(t)
	if code := dispatch(context.Background(), e, []string{"hosts", "drain", "hst_a", "--url", srv.URL}); code != exitOK {
		t.Fatalf("exit code = %d\n%s", code, errOut)
	}

	mu.Lock()
	defer mu.Unlock()
	if len(calls) < 3 {
		t.Fatalf("calls = %v", calls)
	}
	if !strings.HasSuffix(calls[0], "/cordon") {
		t.Errorf("the first call was %q; the host has to be cordoned before it is drained", calls[0])
	}
	if !strings.Contains(out.String(), "Cordoned") || !strings.Contains(out.String(), "2 runners") {
		t.Errorf("the summary does not say what it did:\n%s", out)
	}
}

// A host with nothing on it is already empty, and saying so beats a bulk call
// with no ids -- which the API refuses, so the command would report a failure
// for having nothing to do.
func TestDrainingAnEmptyHostSaysSoRatherThanFailing(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.HasSuffix(r.URL.Path, "/cordon"):
			_, _ = w.Write([]byte(`{"id":"hst_a","name":"vm-1","cordoned":true}`))
		case r.URL.Path == "/api/v1/runners":
			_, _ = w.Write([]byte(`{"items":[],"total":0}`))
		default:
			t.Errorf("an empty host still called %s %s", r.Method, r.URL.Path)
		}
	}))
	defer srv.Close()

	e, out, errOut := newTestEnv(t)
	if code := dispatch(context.Background(), e, []string{"hosts", "drain", "hst_a", "--url", srv.URL}); code != exitOK {
		t.Fatalf("exit code = %d\n%s", code, errOut)
	}
	if !strings.Contains(out.String(), "already empty") {
		t.Errorf("the summary does not say the host was empty:\n%s", out)
	}
}
