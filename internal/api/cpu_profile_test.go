package api

import (
	"bytes"
	"compress/gzip"
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/eyupio/zoomies/internal/store"
)

// profileRequest is the request for a CPU profile as a client that can give up
// would make it, for the tests that need to.
func (h *harness) profileRequest(ctx context.Context, seconds, token string) (*http.Response, error) {
	h.t.Helper()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, h.srv.URL+"/api/v1/diagnostics/cpu-profile?seconds="+seconds, nil)
	if err != nil {
		h.t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	return http.DefaultClient.Do(req)
}

func TestAnAdministratorCanTakeACPUProfile(t *testing.T) {
	h := newHarness(t)
	admin := h.token("admin", store.RoleAdmin)

	resp := h.do(request{method: http.MethodGet, path: "/api/v1/diagnostics/cpu-profile?seconds=1", token: admin})
	resp.mustStatus(t, http.StatusOK, "an administrator taking a one second profile")

	if got := resp.header.Get("Content-Type"); got != "application/octet-stream" {
		t.Errorf("Content-Type = %q, want application/octet-stream", got)
	}
	if got := resp.header.Get("Content-Disposition"); !strings.HasPrefix(got, `attachment; filename="zoomies-cpu-`) || !strings.HasSuffix(got, `.pprof"`) {
		t.Errorf("Content-Disposition = %q, want an attachment named zoomies-cpu-<time>.pprof", got)
	}
	if got := resp.header.Get("Cache-Control"); got != "no-store" {
		t.Errorf("Cache-Control = %q, want no-store", got)
	}
	// pprof writes a gzip stream, which is what `go tool pprof` expects to open.
	zr, err := gzip.NewReader(bytes.NewReader(resp.body))
	if err != nil {
		t.Fatalf("the body is not a gzip stream: %v", err)
	}
	inner, err := io.ReadAll(zr)
	if err != nil || len(inner) == 0 {
		t.Fatalf("the profile inside the gzip stream is empty or unreadable (%d bytes, %v)", len(inner), err)
	}

	events, _, err := h.st.ListAudit(h.ctx, store.AuditFilter{Actions: []string{"diagnostics.cpu_profile"}}, store.Page{Limit: 10})
	if err != nil {
		t.Fatalf("reading the audit log: %v", err)
	}
	if len(events) != 1 {
		t.Errorf("%d audit events for taking a profile, want 1: it reads the process's internals and says who did", len(events))
	}
}

func TestAnOperatorCannotTakeACPUProfile(t *testing.T) {
	h := newHarness(t)

	resp := h.do(request{method: http.MethodGet, path: "/api/v1/diagnostics/cpu-profile?seconds=1", token: h.token("operator", store.RoleOperator)})
	resp.mustStatus(t, http.StatusForbidden, "an operator taking a profile")
	if msg := resp.errorMessage(t); !strings.Contains(msg, string(store.RoleAdmin)) {
		t.Errorf("the refusal does not name the role that is missing: %q", msg)
	}
	if h.ctrl.CPUProfileInProgress() {
		t.Error("a refused request left the profiler running")
	}
}

// How long a profile may run is bounded because the request holds the
// connection and the profiler for it, and a number that is not a number of
// seconds is a mistake to say so about, not to guess at.
func TestTheLengthOfAProfileIsHeldToItsBounds(t *testing.T) {
	h := newHarness(t)
	admin := h.token("admin", store.RoleAdmin)

	for _, seconds := range []string{"0", "31", "-1", "abc", "1.5", "99999999999999999999"} {
		resp := h.do(request{method: http.MethodGet, path: "/api/v1/diagnostics/cpu-profile?seconds=" + seconds, token: admin})
		resp.mustStatus(t, http.StatusBadRequest, "seconds="+seconds)
		if msg := resp.errorMessage(t); !strings.Contains(msg, "from 1 to 30") {
			t.Errorf("seconds=%s: the refusal does not say what is allowed: %q", seconds, msg)
		}
	}
	if h.ctrl.CPUProfileInProgress() {
		t.Error("a refused length left the profiler running")
	}
}

// The runtime has one profiler. A second request while one is being taken has
// to be refused and left alone, not allowed to cut the first one short, and
// the first one has to finish as asked.
func TestASecondProfileIsRefusedWhileOneIsBeingTaken(t *testing.T) {
	h := newHarness(t)
	admin := h.token("admin", store.RoleAdmin)

	first := make(chan *response, 1)
	go func() {
		first <- h.do(request{method: http.MethodGet, path: "/api/v1/diagnostics/cpu-profile?seconds=2", token: admin})
	}()
	waitUntil(t, "the first profile to start", h.ctrl.CPUProfileInProgress)

	second := h.do(request{method: http.MethodGet, path: "/api/v1/diagnostics/cpu-profile?seconds=1", token: admin})
	second.mustStatus(t, http.StatusConflict, "a second profile while one is being taken")

	select {
	case r := <-first:
		r.mustStatus(t, http.StatusOK, "the first profile, after a second was refused")
	case <-time.After(10 * time.Second):
		t.Fatal("the first profile never finished")
	}

	h.do(request{method: http.MethodGet, path: "/api/v1/diagnostics/cpu-profile?seconds=1", token: admin}).
		mustStatus(t, http.StatusOK, "a profile after the earlier ones have finished")
}

// Hanging up has to end the sample. Otherwise asking for thirty seconds and
// leaving would hold the profiler, and every later request would be refused
// for the rest of them.
func TestAProfileEndsWhenTheClientHangsUp(t *testing.T) {
	h := newHarness(t)
	admin := h.token("admin", store.RoleAdmin)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		if resp, err := h.profileRequest(ctx, "30", admin); err == nil {
			resp.Body.Close()
		}
	}()
	waitUntil(t, "the profile to start", h.ctrl.CPUProfileInProgress)

	cancel()
	<-done
	waitUntil(t, "the profiler to be released after the client left", func() bool { return !h.ctrl.CPUProfileInProgress() })

	h.do(request{method: http.MethodGet, path: "/api/v1/diagnostics/cpu-profile?seconds=1", token: admin}).
		mustStatus(t, http.StatusOK, "a profile straight after a client hung up on one")
}

// waitUntil polls for a condition on another goroutine.
func waitUntil(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(5 * time.Millisecond)
	}
}
