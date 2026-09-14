package api

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// The capacity map reloads its samples on every window change, and a browser
// that moves on cancels the request it left behind. That is the client's
// doing, not a fault in the controller: logging each one as a failed request
// filled the log with errors nobody could act on, which is how the errors
// that matter get scrolled past.
func TestAnAbandonedRequestIsNotLoggedAsAFailure(t *testing.T) {
	h := newHarness(t)

	ctx, cancel := context.WithCancel(h.ctx)
	cancel()
	r := httptest.NewRequest(http.MethodGet, "/api/v1/hosts/samples?window=1h", nil).WithContext(ctx)
	w := httptest.NewRecorder()
	h.api.internal(w, r, "reading host samples", context.Canceled)

	if w.Code != statusClientClosed {
		t.Errorf("status = %d, want %d for a client that hung up", w.Code, statusClientClosed)
	}
	if body := strings.TrimSpace(w.Body.String()); body != "" {
		t.Errorf("body = %q, want nothing: there is no one left to read it", body)
	}
	if logged := h.logs.text(); strings.Contains(logged, "ERROR request failed") {
		t.Errorf("logged an error for an abandoned request:\n%s", logged)
	}
}

// A cancellation that is not the client's -- the store giving up on its own
// while the request is still live -- is a real failure, and an operator needs
// the log line and the request ID to chase it.
func TestACancellationTheClientDidNotCauseIsStillAFailure(t *testing.T) {
	h := newHarness(t)

	r := httptest.NewRequest(http.MethodGet, "/api/v1/hosts/samples?window=1h", nil)
	w := httptest.NewRecorder()
	h.api.internal(w, r, "reading host samples", errors.New("context canceled"))

	if w.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500", w.Code)
	}
	if logged := h.logs.text(); !strings.Contains(logged, "request failed") {
		t.Errorf("did not log the failure:\n%s", logged)
	}
}
