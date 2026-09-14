package api

import (
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/eyupio/zoomies/internal/store"
)

// waitForStreamTask polls as an agent would until the controller has queued
// the log relay this request caused, and returns its stream ID.
func waitForStreamTask(t *testing.T, h *harness, agentToken string) string {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		resp := h.do(request{method: http.MethodGet, path: "/api/v1/agent/tasks?wait=1", token: agentToken})
		if resp.status != http.StatusOK {
			t.Fatalf("agent task poll: %d %s", resp.status, resp.body)
		}
		var batch struct {
			Tasks []struct {
				Kind     string `json:"kind"`
				StreamID string `json:"stream_id"`
			} `json:"tasks"`
		}
		resp.into(t, &batch)
		for _, task := range batch.Tasks {
			if task.Kind == "stream_logs" && task.StreamID != "" {
				return task.StreamID
			}
		}
	}
	t.Fatal("no stream_logs task was queued within the deadline")
	return ""
}

// A download has to terminate. It is the same relay as the live tail, read
// until it goes quiet rather than followed -- a runner still producing output
// would otherwise stream forever into a file the browser never finishes.
func TestDownloadingRunnerLogsEndsWhenTheOutputDoes(t *testing.T) {
	h := newHarness(t)
	inst := h.installation()
	pool := h.pool(inst, "linux-x64")

	hostID, agentToken := h.agentToken("vm-1")
	host, err := h.st.GetHost(h.ctx, hostID)
	if err != nil {
		t.Fatalf("GetHost: %v", err)
	}
	run := h.runner(pool, host, store.RunnerBusy)
	u, _ := h.user("viewer", store.RoleViewer)

	done := make(chan *response, 1)
	go func() {
		done <- h.do(request{method: http.MethodGet,
			path: "/api/v1/runners/" + run.ID + "/logs/download", cookie: h.session(u)})
	}()

	streamID := waitForStreamTask(t, h, agentToken)
	const body = "the first line\nthe last line of the build\n"
	post := h.do(request{method: http.MethodPost, path: "/api/v1/agent/logs/" + streamID,
		token: agentToken, headers: map[string]string{"Content-Type": "application/octet-stream"},
		rawBody: body})
	if post.status != http.StatusNoContent && post.status != http.StatusOK {
		t.Fatalf("the agent's log POST answered %d: %s", post.status, post.body)
	}

	resp := <-done
	if resp.status != http.StatusOK {
		t.Fatalf("status = %d: %s", resp.status, resp.body)
	}
	if !strings.Contains(string(resp.body), "the last line of the build") {
		t.Fatalf("the download did not carry the output:\n%s", resp.body)
	}

	// It is a file, not a page: the browser must save it under a name that
	// says which runner it came from.
	if ct := resp.header.Get("Content-Type"); !strings.HasPrefix(ct, "text/plain") {
		t.Errorf("Content-Type = %q, want text/plain", ct)
	}
	cd := resp.header.Get("Content-Disposition")
	if !strings.HasPrefix(cd, "attachment;") || !strings.Contains(cd, ".log") {
		t.Errorf("Content-Disposition = %q, want an attachment named .log", cd)
	}
}

// A runner that never reached a host cannot produce output. That is a fact
// about the runner rather than a fault in the server, so it is a 409 carrying
// the controller's own sentence -- not a 500 with a request ID.
func TestDownloadingLogsForARunnerThatCannotProduceThem(t *testing.T) {
	h := newHarness(t)
	inst := h.installation()
	pool := h.pool(inst, "linux-x64")
	host := h.host("vm-1")
	run := h.runner(pool, host, store.RunnerRemoved)
	u, _ := h.user("viewer", store.RoleViewer)

	resp := h.do(request{method: http.MethodGet,
		path: "/api/v1/runners/" + run.ID + "/logs/download", cookie: h.session(u)})
	if resp.status != http.StatusConflict {
		t.Fatalf("status = %d, want 409: %s", resp.status, resp.body)
	}
	if resp.errorMessage(t) == "" {
		t.Fatalf("the refusal said nothing an operator can act on:\n%s", resp.body)
	}

	missing := h.do(request{method: http.MethodGet,
		path: "/api/v1/runners/run_nope/logs/download", cookie: h.session(u)})
	if missing.status != http.StatusNotFound {
		t.Fatalf("status = %d, want 404: %s", missing.status, missing.body)
	}
}

// The download name goes into a Content-Disposition header and then into a
// filesystem, so it is reduced to characters that survive both -- a runner
// name with a slash or a quote in it must not be able to break either.
func TestLogFilenameSurvivesEveryFilesystemAndQuotingRule(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{"zoomies-abc123", "zoomies-abc123.log"},
		{"zoomies_4vcpu.ubuntu-24.04", "zoomies_4vcpu.ubuntu-24.04.log"},
		{"../../etc/passwd", "..-..-etc-passwd.log"},
		{`a"quote`, "a-quote.log"},
		{"runner name", "runner-name.log"},
		// A name made entirely of characters that cannot survive would leave
		// nothing at all, and ".log" is not a filename.
		{"///", "---.log"},
		{"", "runner.log"},
	} {
		if got := logFilename(tc.in); got != tc.want {
			t.Errorf("logFilename(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}
