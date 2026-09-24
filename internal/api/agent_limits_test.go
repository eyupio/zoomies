package api

import (
	"net/http"
	"strconv"
	"testing"

	"github.com/eyupio/zoomies/internal/agent"
)

// A host over its budget is told so with a status its transport already
// retries, a wait it can honour, and a message and code a person can act on --
// on each of the three routes the budget covers, and before the body is read.
func TestAHostOverItsCallBudgetIsAnswered429WithRetryAfter(t *testing.T) {
	routes := []struct {
		name string
		path string
		body any
	}{
		{"heartbeat", agent.PathHeartbeat, agent.HeartbeatRequest{ProtocolVersion: 1, Capacity: 2, Version: "test"}},
		{"results", agent.PathResults, agent.TaskResult{TaskID: "tsk_nosuchtask"}},
		{"report", agent.PathReport, []agent.RunnerReport{}},
	}
	for _, rt := range routes {
		t.Run(rt.name, func(t *testing.T) {
			h := newHarness(t)
			hostID, token := h.agentToken("vm-noisy")
			for i := 0; ; i++ {
				if ok, _ := h.ctrl.AllowAgentCall(hostID); !ok {
					break
				}
				if i > 100_000 {
					t.Fatal("a host was never refused however many calls it made")
				}
			}
			resp := h.do(request{method: http.MethodPost, path: rt.path, token: token, body: rt.body})
			resp.mustStatus(t, http.StatusTooManyRequests, rt.name+" over budget")
			if code := resp.errorCode(t); code != codeRateLimited {
				t.Errorf("error code = %q, want %q", code, codeRateLimited)
			}
			if secs, err := strconv.Atoi(resp.header.Get("Retry-After")); err != nil || secs < 1 {
				t.Errorf("Retry-After = %q, want a whole number of seconds", resp.header.Get("Retry-After"))
			}
			if resp.errorMessage(t) == "" {
				t.Error("the refusal carried no message for the operator")
			}

			// Another host is not held to this one's budget.
			_, other := h.agentToken("vm-quiet")
			h.do(request{method: http.MethodPost, path: rt.path, token: other, body: rt.body}).
				mustStatus(t, statusFor(rt.name), rt.name+" from another host")
		})
	}
}

// statusFor is what each budgeted route answers the smallest body it accepts.
func statusFor(route string) int {
	if route == "heartbeat" {
		return http.StatusOK
	}
	return http.StatusNoContent
}

// The runner cap reaches the agent as a 413, the status the body limit already
// uses, rather than a 500 that reads as the controller's own failure.
func TestAReportCarryingMoreRunnersThanTheCapIsAnswered413(t *testing.T) {
	h := newHarness(t)
	_, token := h.agentToken("vm-1")
	tooMany := make([]agent.RunnerReport, agent.MaxRunnersPerReport+1)

	for _, c := range []struct {
		name string
		path string
		body any
	}{
		{"report", agent.PathReport, tooMany},
		{"heartbeat", agent.PathHeartbeat, agent.HeartbeatRequest{ProtocolVersion: 1, Capacity: 2, Runners: tooMany}},
	} {
		resp := h.do(request{method: http.MethodPost, path: c.path, token: token, body: c.body})
		resp.mustStatus(t, http.StatusRequestEntityTooLarge, c.name)
		if code := resp.errorCode(t); code != codeTooLarge {
			t.Errorf("%s: error code = %q, want %q", c.name, code, codeTooLarge)
		}
	}
}
