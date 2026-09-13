package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// Without --follow this is a snapshot, and the API has a route that ends by
// itself. Reading the live stream and guessing when to stop would give a
// truncated file with no way to tell.
func TestRunnersLogsWithoutFollowReadsTheRouteThatEnds(t *testing.T) {
	var path string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path = r.URL.Path
		w.Header().Set("Content-Type", "text/plain")
		_, _ = w.Write([]byte("the first line\nthe last line\n"))
	}))
	defer srv.Close()

	out, _ := runCLI(t, "runners", "logs", "run_1", "--url", srv.URL)

	if !strings.HasSuffix(path, "/runners/run_1/logs/download") {
		t.Errorf("a snapshot read %q, want the download route", path)
	}
	if out != "the first line\nthe last line\n" {
		t.Errorf("the output was not printed as it arrived: %q", out)
	}
}

// Following stops at the relay's "end" event, which for an ephemeral runner is
// the job ending. Without that this command would never terminate.
func TestRunnersLogsFollowStopsAtTheEndEvent(t *testing.T) {
	var query string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		query = r.URL.RawQuery
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		// A frame this client cannot read is not a reason to drop the rest of
		// the job's output, so a broken one sits in the middle.
		_, _ = w.Write([]byte("event: log\ndata: \"building\\n\"\n\n" +
			"event: log\ndata: not json at all\n\n" +
			"event: log\ndata: \"done\\n\"\n\n" +
			"event: end\ndata: {}\n\n"))
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
	}))
	defer srv.Close()

	out, _ := runCLI(t, "runners", "logs", "run_1", "--url", srv.URL, "--follow", "--tail", "50")

	if out != "building\ndone\n" {
		t.Errorf("output = %q, want both readable chunks and nothing else", out)
	}
	if !strings.Contains(query, "follow=true") || !strings.Contains(query, "tail=50") {
		t.Errorf("query = %q, want the follow and tail it was asked for", query)
	}
}

// A negative tail is an operator's typo, not a request for a negative number
// of lines, so it is sent as none rather than passed on for the server to
// refuse.
func TestRunnersLogsClampsANegativeTail(t *testing.T) {
	var query string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		query = r.URL.RawQuery
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("event: end\ndata: {}\n\n"))
	}))
	defer srv.Close()

	runCLI(t, "runners", "logs", "run_1", "--url", srv.URL, "--follow", "--tail", "-5")

	if !strings.Contains(query, "tail=0") {
		t.Errorf("query = %q, want a clamped tail", query)
	}
}

// A 404 here has two causes and an operator cannot tell them apart from the
// status code, so the message names both rather than leaving them to guess
// whether they typed the ID wrong or their host is down.
func TestRunnersLogsExplainsWhatA404Means(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"error":{"code":"not_found","message":"no such runner"}}`))
	}))
	defer srv.Close()

	e, _, errOut := newTestEnv(t)
	if code := dispatch(context.Background(), e, []string{"runners", "logs", "run_1", "--url", srv.URL}); code == exitOK {
		t.Fatal("a 404 was reported as success")
	}
	body := errOut.String()
	if !strings.Contains(body, "run_1") {
		t.Errorf("the complaint must name the runner:\n%s", body)
	}
	if !strings.Contains(body, "no runner with that ID") || !strings.Contains(body, "not reachable") {
		t.Errorf("both causes must be named:\n%s", body)
	}
}

// Anything that is not a 404 is passed through as it came: a 409 explained as
// a missing runner would send an operator to look at the wrong thing.
func TestRunnersLogsPassesThroughWhatItCannotImprove(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusConflict)
		_, _ = w.Write([]byte(`{"error":{"code":"conflict","message":"this runner was removed before it produced any output"}}`))
	}))
	defer srv.Close()

	e, _, errOut := newTestEnv(t)
	if code := dispatch(context.Background(), e, []string{"runners", "logs", "run_1", "--url", srv.URL}); code == exitOK {
		t.Fatal("a 409 was reported as success")
	}
	body := errOut.String()
	if !strings.Contains(body, "removed before it produced any output") {
		t.Errorf("the server's own sentence must survive:\n%s", body)
	}
	if strings.Contains(body, "no runner with that ID") {
		t.Errorf("a 409 was explained as a 404:\n%s", body)
	}
}
