package api

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/eyupio/zoomies/internal/agent"
	"github.com/eyupio/zoomies/internal/store"
)

// agentRoute is one of the five routes behind s.agentAuth. The bodies are the
// smallest thing each handler will accept, because what is under test is the
// middleware in front of them and not the handler's own validation.
type agentRoute struct {
	name   string
	method string
	path   string
	body   any
	// raw is the log relay's body, which is a byte stream rather than JSON.
	raw string
}

// agentRoutes is written out by hand rather than read off the router for the
// same reason routeTable is: a list derived from the thing it checks would
// agree with a mistake. Its length is asserted against the router below.
func agentRoutesUnderTest() []agentRoute {
	return []agentRoute{
		{name: "heartbeat", method: http.MethodPost, path: "/api/v1/agent/heartbeat",
			body: agent.HeartbeatRequest{ProtocolVersion: 1, Capacity: 2, Version: "test"}},
		// The long poll is given the shortest wait it will honour, so a
		// refused call costs nothing and an accepted one returns promptly.
		{name: "tasks", method: http.MethodGet, path: "/api/v1/agent/tasks?wait=1"},
		{name: "results", method: http.MethodPost, path: "/api/v1/agent/results",
			body: agent.TaskResult{TaskID: "tsk_nosuchtask"}},
		{name: "report", method: http.MethodPost, path: "/api/v1/agent/report",
			body: []agent.RunnerReport{}},
		{name: "logs", method: http.MethodPost, path: "/api/v1/agent/logs/log_nosuchstream",
			raw: "chunk"},
	}
}

// TestAgentRoutesRefuseAUserCredential is the separation the whole agent
// surface rests on: an agent token reaches /api/v1/agent/* and nothing else,
// and a user token does not reach the agent routes at all.
//
// It walks all five authenticated agent routes. The earlier version of this
// test walked three, which left the task poll and the log relay -- the two
// that carry a runner's work and a runner's output -- with no negative test
// at all.
func TestAgentRoutesRefuseAUserCredential(t *testing.T) {
	h := newHarness(t)
	adminToken := h.token("admin", store.RoleAdmin)
	admin, _ := h.user("root", store.RoleAdmin)
	adminCookie := h.session(admin)
	_, agentToken := h.agentToken("vm-1")

	callers := []struct {
		name  string
		apply func(*request)
	}{
		{"anonymous", func(*request) {}},
		{"an admin API token", func(r *request) { r.token = adminToken }},
		{"an admin browser session", func(r *request) { r.cookie = adminCookie }},
		{"a made-up bearer", func(r *request) { r.token = "zag_notarealtokenatall" }},
	}

	for _, rt := range agentRoutesUnderTest() {
		for _, caller := range callers {
			t.Run(rt.name+"/"+caller.name, func(t *testing.T) {
				req := request{method: rt.method, path: rt.path, body: rt.body, rawBody: rt.raw}
				caller.apply(&req)
				resp := h.do(req)
				resp.mustStatus(t, http.StatusUnauthorized, caller.name+" on "+rt.path)
			})
		}

		// The positive half of the same walk: the host's own token is not
		// refused anywhere, which is what makes the four refusals above about
		// the credential rather than about the route being broken.
		t.Run(rt.name+"/its own agent token", func(t *testing.T) {
			resp := h.do(request{method: rt.method, path: rt.path, body: rt.body,
				rawBody: rt.raw, token: agentToken})
			if resp.status == http.StatusUnauthorized {
				t.Fatalf("the host's own agent token was refused on %s: %s", rt.path, truncate(resp.body))
			}
		})
	}

	// And the agent's own credential is refused on the user API.
	onUserAPI := h.do(request{method: http.MethodGet, path: "/api/v1/pools", token: agentToken})
	onUserAPI.mustStatus(t, http.StatusUnauthorized, "agent token on the user API")
	if !strings.Contains(onUserAPI.errorMessage(t), "agent token") {
		t.Errorf("the refusal does not say what kind of credential it was: %q", onUserAPI.errorMessage(t))
	}
}

// TestAgentRoutesUnderTestCoverTheRouter keeps the walk above honest: a sixth
// authenticated agent route added without a row here would otherwise ship with
// no negative test, which is exactly how tasks and logs came to be missing.
func TestAgentRoutesUnderTestCoverTheRouter(t *testing.T) {
	// PathJoin is deliberately absent: it is the one anonymous agent route,
	// and it is walked by TestAgentJoinRefusesASpentToken instead.
	want := map[string]bool{
		agent.PathHeartbeat: false,
		agent.PathTasks:     false,
		agent.PathResults:   false,
		agent.PathReport:    false,
		agent.PathLogs:      false,
	}
	for _, rt := range agentRoutesUnderTest() {
		path, _, _ := strings.Cut(rt.path, "?")
		matched := false
		for known := range want {
			if path == known || strings.HasPrefix(path, known+"/") {
				want[known] = true
				matched = true
				break
			}
		}
		if !matched {
			t.Errorf("%s is not one of the agent paths the transport declares", rt.path)
		}
	}
	for path, covered := range want {
		if !covered {
			t.Errorf("%s has no row in agentRoutesUnderTest, so nothing proves it refuses a user credential", path)
		}
	}
}

// TestADeletedHostsAgentTokenIsRefused covers revocation on the agent side.
//
// A host's token hash lives on the host row, so removing the host is how an
// operator revokes a machine's access to the fleet. Every agent route has to
// honour that, not only the heartbeat that happens to notice first.
func TestADeletedHostsAgentTokenIsRefused(t *testing.T) {
	h := newHarness(t)
	hostID, agentToken := h.agentToken("vm-doomed")

	if _, err := h.st.DeleteHost(h.ctx, hostID); err != nil {
		t.Fatalf("DeleteHost: %v", err)
	}

	for _, rt := range agentRoutesUnderTest() {
		t.Run(rt.name, func(t *testing.T) {
			resp := h.do(request{method: rt.method, path: rt.path, body: rt.body,
				rawBody: rt.raw, token: agentToken})
			resp.mustStatus(t, http.StatusUnauthorized, "a deleted host's token on "+rt.path)
			// The message has to name the fix, because the agent operator is
			// the only person who can carry it out.
			if !strings.Contains(resp.errorMessage(t), "re-join") {
				t.Errorf("the refusal does not tell the operator to re-join: %q", resp.errorMessage(t))
			}
		})
	}
}

// TestAgentJoinAndHeartbeat covers enrolment and the liveness beat, including
// the 404 an agent's transport turns into "re-join me".
func TestAgentJoinAndHeartbeat(t *testing.T) {
	h := newHarness(t)
	hostID, token := h.agentToken("vm-1")

	host, err := h.st.GetHost(h.ctx, hostID)
	if err != nil {
		t.Fatalf("GetHost: %v", err)
	}
	if host.Name != "vm-1" || host.Capacity != 2 {
		t.Fatalf("the join did not record the host as described: %+v", host)
	}
	if len(host.Backends) != 1 || host.Backends[0] != "docker" {
		t.Errorf("backends = %v, want [docker]", host.Backends)
	}

	beat := h.do(request{method: http.MethodPost, path: "/api/v1/agent/heartbeat", token: token,
		body: agent.HeartbeatRequest{ProtocolVersion: 1, Capacity: 2, Version: "test"}})
	beat.mustStatus(t, http.StatusOK, "heartbeat")
	var resp agent.HeartbeatResponse
	beat.into(t, &resp)
	if !resp.OK {
		t.Errorf("heartbeat response = %+v", resp)
	}

	// A host deleted under a running agent must answer 404, not 500: the
	// agent's transport reads that specific status as "you no longer exist".
	if _, err := h.st.DeleteHost(h.ctx, hostID); err != nil {
		t.Fatalf("DeleteHost: %v", err)
	}
	gone := h.do(request{method: http.MethodPost, path: "/api/v1/agent/heartbeat", token: token,
		body: agent.HeartbeatRequest{ProtocolVersion: 1, Capacity: 2}})
	// The token went with the row, so this is refused before the handler runs;
	// either answer tells the agent to stop, and 401 is the stronger of the two.
	if gone.status != http.StatusUnauthorized && gone.status != http.StatusNotFound {
		t.Fatalf("heartbeat after the host was deleted answered %d, want 401 or 404", gone.status)
	}
}

func TestAgentJoinRefusesASpentToken(t *testing.T) {
	h := newHarness(t)
	_, plaintext, err := h.ctrl.Auth().CreateJoinToken(h.ctx, time.Hour, nil, 2, "test")
	if err != nil {
		t.Fatalf("CreateJoinToken: %v", err)
	}
	body := map[string]any{
		"protocol_version": 1, "join_token": plaintext, "name": "vm-1",
		"capacity": 2, "os": "linux", "arch": "amd64", "version": "test",
	}

	first := h.do(request{method: http.MethodPost, path: "/api/v1/agent/join", body: body})
	first.mustStatus(t, http.StatusOK, "first join")

	body["name"] = "vm-2"
	second := h.do(request{method: http.MethodPost, path: "/api/v1/agent/join", body: body})
	second.mustStatus(t, http.StatusUnprocessableEntity, "second join with the same token")
	if !strings.Contains(second.errorMessage(t), "join token") {
		t.Errorf("the refusal does not name the problem: %q", second.errorMessage(t))
	}
}

// TestAgentTaskPollAndResult walks a task from the queue to its outcome.
func TestAgentTaskPollAndResult(t *testing.T) {
	h := newHarness(t)
	inst := h.installation()
	pool := h.pool(inst, "linux-x64")
	hostID, token := h.agentToken("vm-1")
	host, err := h.st.GetHost(h.ctx, hostID)
	if err != nil {
		t.Fatalf("GetHost: %v", err)
	}
	run := h.runner(pool, host, store.RunnerIdle)

	// An empty long poll returns quickly when asked to wait a second, which is
	// the idle case; nothing is queued yet.
	idle := h.do(request{method: http.MethodGet, path: "/api/v1/agent/tasks?wait=1", token: token})
	idle.mustStatus(t, http.StatusOK, "idle poll")
	var empty agent.TaskBatch
	idle.into(t, &empty)
	if len(empty.Tasks) != 0 {
		t.Fatalf("an idle poll returned %d tasks", len(empty.Tasks))
	}

	// Draining the runner queues a stop task for its host.
	u, _ := h.user("operator", store.RoleOperator)
	drain := h.do(request{method: http.MethodPost, path: "/api/v1/runners/" + run.ID + "/drain", cookie: h.session(u)})
	drain.mustStatus(t, http.StatusAccepted, "drain")

	poll := h.do(request{method: http.MethodGet, path: "/api/v1/agent/tasks?wait=2", token: token})
	poll.mustStatus(t, http.StatusOK, "task poll")
	var batch agent.TaskBatch
	poll.into(t, &batch)
	if len(batch.Tasks) == 0 {
		t.Fatal("the drain queued no task for the agent")
	}
	task := batch.Tasks[0]
	if task.Kind != agent.TaskStopRunner || task.RunnerID != run.ID {
		t.Fatalf("task = %+v, want a stop for %s", task, run.ID)
	}

	result := h.do(request{method: http.MethodPost, path: "/api/v1/agent/results", token: token,
		body: agent.TaskResult{TaskID: task.ID, RunnerID: run.ID, OK: true, CompletedAt: time.Now()}})
	result.mustStatus(t, http.StatusNoContent, "task result")
}

// TestAgentReportTakesABareArray keeps the two halves of the protocol agreeing
// about the wire format: the agent sends []RunnerReport, not an object.
func TestAgentReportTakesABareArray(t *testing.T) {
	h := newHarness(t)
	inst := h.installation()
	pool := h.pool(inst, "linux-x64")
	hostID, token := h.agentToken("vm-1")
	host, err := h.st.GetHost(h.ctx, hostID)
	if err != nil {
		t.Fatalf("GetHost: %v", err)
	}
	run := h.runner(pool, host, store.RunnerProvisioning)

	reports := []agent.RunnerReport{{
		RunnerID: run.ID, State: store.RunnerRegistering, ObservedAt: time.Now(),
	}}
	raw, err := json.Marshal(reports)
	if err != nil {
		t.Fatalf("marshalling reports: %v", err)
	}
	resp := h.do(request{method: http.MethodPost, path: "/api/v1/agent/report", token: token,
		rawBody: string(raw), headers: map[string]string{"Content-Type": "application/json"}})
	resp.mustStatus(t, http.StatusNoContent, "runner report")

	after, err := h.st.GetRunner(h.ctx, run.ID)
	if err != nil {
		t.Fatalf("GetRunner: %v", err)
	}
	if after.State != store.RunnerRegistering {
		t.Errorf("state = %q after the report, want registering", after.State)
	}
}

// TestAgentLogPostForAnUnknownStream is the ordinary case of a viewer who
// closed the tab: the relay is gone and the agent is told to stop.
func TestAgentLogPostForAnUnknownStream(t *testing.T) {
	h := newHarness(t)
	_, token := h.agentToken("vm-1")

	resp := h.do(request{method: http.MethodPost, path: "/api/v1/agent/logs/log_nobody", token: token,
		rawBody: "some output", headers: map[string]string{"Content-Type": "application/octet-stream"}})
	resp.mustStatus(t, http.StatusNotFound, "log post for an unknown stream")
}

// The two halves of the agent protocol are the same binary at different
// versions while an upgrade rolls across a fleet. A newer agent that adds one
// optional field to its heartbeat used to be answered 400 by an older
// controller, and stopped heartbeating -- every host unhealthy at once, for a
// change ProtocolVersion was never meant to cover.
func TestAgentRoutesTolerateFieldsTheyDoNotKnow(t *testing.T) {
	h := newHarness(t)
	_, token := h.agentToken("vm-1")

	beat := h.do(request{method: http.MethodPost, path: "/api/v1/agent/heartbeat", token: token,
		body: map[string]any{"protocol_version": 1, "capacity": 2, "version": "test", "load_average": 0.42}})
	beat.mustStatus(t, http.StatusOK, "a heartbeat carrying a field this controller does not know")

	// The user API keeps its strictness: there the unknown field is a typo
	// that would otherwise be silently ignored.
	admin, _ := h.user("root", store.RoleAdmin)
	typo := h.do(request{method: http.MethodPost, path: "/api/v1/users", cookie: h.session(admin),
		body: map[string]any{"username": "sam", "password": "correct-horse-battery", "rolle": "viewer"}})
	typo.mustStatus(t, http.StatusBadRequest, "a typo in a user API field")
}
