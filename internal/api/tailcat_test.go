package api

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/eyupio/zoomies/internal/agent"
	"github.com/eyupio/zoomies/internal/config"
	"github.com/eyupio/zoomies/internal/store"
	"github.com/tailscale/tailcat"
	"tailscale.com/tstest/integration"
)

func TestPrivateSurfaceRejectsOperatorRoutesAndUnauthenticatedAgents(t *testing.T) {
	h := newHarness(t)
	for _, path := range []string{"/", "/api/v1/settings", "/api/v1/hosts", "/api/v1/join-tokens", "/api/v1/meta", "/metrics", "/healthz"} {
		w := httptest.NewRecorder()
		h.api.privateAgentHandler().ServeHTTP(w, httptest.NewRequest("GET", path, nil))
		if w.Code != http.StatusNotFound {
			t.Errorf("%s returned %d", path, w.Code)
		}
	}
	w := httptest.NewRecorder()
	h.api.privateAgentHandler().ServeHTTP(w, httptest.NewRequest("GET", agent.PathTasks, nil))
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated task request returned %d", w.Code)
	}
}

func TestPrivateEnrolmentRequiresAdminAndEnabledAuthentication(t *testing.T) {
	h := newHarness(t, func(c *config.Config) { c.Server.TailcatEnabled = false })
	viewer, _ := h.user("private-viewer", store.RoleViewer)
	h.do(request{method: "POST", path: "/api/v1/join-tokens", cookie: h.session(viewer), body: map[string]any{"connection": "tailcat"}}).mustStatus(t, 403, "viewer cannot create a tunnel")
	admin, _ := h.user("private-admin", store.RoleAdmin)
	h.do(request{method: "POST", path: "/api/v1/join-tokens", cookie: h.session(admin), body: map[string]any{"connection": "tailcat"}}).mustStatus(t, 422, "disabled private connections")
	tokens, err := h.st.ListJoinTokens(h.ctx)
	if err != nil || len(tokens) != 0 {
		t.Fatal("failed tunnel setup minted an unusable token")
	}
}

func TestPrivateEnrolmentCommandsAreShownOnceAndObservedConnectionIsTrusted(t *testing.T) {
	h := newHarness(t)
	identity := tailcat.NewPrivateKey()
	identity.Public.RegionID = 1
	address := string(identity.Public.Addr())
	// Model an already-running listener. Transport encryption is exercised
	// separately by the local-DERP integration test.
	h.api.private.node = &tailcat.Server{}
	h.api.private.address = address
	admin, _ := h.user("private-admin", store.RoleAdmin)
	minted := h.do(request{method: "POST", path: "/api/v1/join-tokens", cookie: h.session(admin), body: map[string]any{"connection": "tailcat", "capacity": 3}})
	minted.mustStatus(t, 201, "private enrolment")
	var result createJoinTokenResponse
	minted.into(t, &result)
	if !strings.Contains(result.Command, "tailcat://"+address) || !strings.Contains(result.JoinCommand, "tailcat://"+address) {
		t.Fatal("commands do not use the private connection")
	}
	for _, path := range []string{"/api/v1/join-tokens", "/api/v1/join-tokens/" + result.ID, "/api/v1/meta", "/api/v1/settings"} {
		response := h.do(request{method: "GET", path: path, cookie: h.session(admin)})
		var body map[string]any
		response.into(t, &body)
		encoded, _ := json.Marshal(body)
		if strings.Contains(string(encoded), address) || strings.Contains(string(encoded), result.Token) {
			t.Fatalf("%s leaked an enrolment credential", path)
		}
	}
	privateHTTP := httptest.NewServer(h.api.privateAgentHandler())
	defer privateHTTP.Close()
	tr, err := agent.NewHTTPTransport(agent.HTTPOptions{ControllerURL: privateHTTP.URL})
	if err != nil {
		t.Fatal(err)
	}
	joined, err := tr.Join(h.ctx, agent.JoinRequest{ProtocolVersion: agent.ProtocolVersion, JoinToken: result.Token, Name: "lab-through-private-surface", Connection: "direct"})
	if err != nil {
		t.Fatal(err)
	}
	host, err := h.st.GetHost(h.ctx, joined.HostID)
	if err != nil || host.Connection != "tailcat" || host.Capacity != 3 {
		t.Fatal("enrolment lost verified connection or operator capacity")
	}
	if _, err := tr.Join(h.ctx, agent.JoinRequest{ProtocolVersion: agent.ProtocolVersion, JoinToken: result.Token, Name: "replay"}); err == nil {
		t.Fatal("enrolment token could be replayed")
	}
	if strings.Contains(h.logs.text(), address) || strings.Contains(h.logs.text(), result.Token) {
		t.Fatal("enrolment credentials leaked to logs")
	}
}

// A local DERP relay exercises the real WireGuard/Tailcat data path without
// depending on Tailscale's hosted infrastructure or a real GitHub account.
func TestPrivateHostJoinsHeartbeatsAndReconnectsAfterControllerRestart(t *testing.T) {
	t.Setenv("IN_TS_TEST", "true")
	h := newHarness(t)
	dm := integration.RunDERPAndSTUN(t, func(string, ...any) {}, "127.0.0.1")
	identity := tailcat.NewPrivateKey()
	identity.Public.Region = append(identity.Public.Region, dm.Regions[1])
	raw, err := json.Marshal(identity)
	if err != nil {
		t.Fatal(err)
	}
	sealed, err := h.key.Seal(raw)
	if err != nil {
		t.Fatal(err)
	}
	if err := h.st.SetSetting(h.ctx, tailcatIdentitySetting, base64.StdEncoding.EncodeToString(sealed), true); err != nil {
		t.Fatal(err)
	}
	address, err := h.api.ensureTailcat(h.ctx)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(h.api.closeTailcat)
	_, token, err := h.ctrl.Auth().CreateJoinToken(h.ctx, time.Minute, nil, 2, "test")
	if err != nil {
		t.Fatal(err)
	}
	tr, err := agent.NewHTTPTransport(agent.HTTPOptions{ControllerURL: "tailcat://" + address})
	if err != nil {
		t.Fatal(err)
	}
	defer tr.Close()
	ctx, cancel := context.WithTimeout(h.ctx, 30*time.Second)
	defer cancel()
	joined, err := tr.Join(ctx, agent.JoinRequest{ProtocolVersion: agent.ProtocolVersion, JoinToken: token, Name: "home-lab", Capacity: 2, OS: "linux", Arch: "arm64"})
	if err != nil {
		t.Fatal(err)
	}
	tr.SetCredentials(joined.HostID, joined.AgentToken)
	if _, err := tr.Heartbeat(ctx, agent.HeartbeatRequest{ProtocolVersion: agent.ProtocolVersion, Capacity: 2}); err != nil {
		t.Fatal(err)
	}
	host, err := h.st.GetHost(h.ctx, joined.HostID)
	if err != nil || host.Connection != "tailcat" {
		t.Fatalf("host did not record the observed tunnel: %v", err)
	}
	settings, _ := h.st.ListSettings(h.ctx)
	for _, setting := range settings {
		if strings.Contains(setting.Value, address) {
			t.Fatal("settings leaked capability")
		}
	}
	h.api.closeTailcat()
	restarted, err := New(Options{Controller: h.ctrl})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(restarted.closeTailcat)
	next, err := restarted.ensureTailcat(h.ctx)
	if err != nil {
		t.Fatal(err)
	}
	if next != address {
		t.Fatal("controller restart changed the enrolment address")
	}
	// A new agent process uses the same persisted address and agent token.
	tr.Close()
	tr, err = agent.NewHTTPTransport(agent.HTTPOptions{ControllerURL: agent.TailcatController, TailcatAddress: address})
	if err != nil {
		t.Fatal(err)
	}
	defer tr.Close()
	tr.SetCredentials(joined.HostID, joined.AgentToken)
	if _, err := tr.Heartbeat(ctx, agent.HeartbeatRequest{ProtocolVersion: agent.ProtocolVersion, Capacity: 2}); err != nil {
		t.Fatal(err)
	}
	// The very same credential over HTTP is observed as direct; a forged JSON
	// connection field cannot keep the private badge.
	direct, err := agent.NewHTTPTransport(agent.HTTPOptions{ControllerURL: h.srv.URL})
	if err != nil {
		t.Fatal(err)
	}
	direct.SetCredentials(joined.HostID, joined.AgentToken)
	if _, err := direct.Heartbeat(ctx, agent.HeartbeatRequest{ProtocolVersion: agent.ProtocolVersion, Capacity: 2}); err != nil {
		t.Fatal(err)
	}
	host, _ = h.st.GetHost(h.ctx, joined.HostID)
	if host.Connection != "direct" {
		t.Fatal("connection badge did not follow the observed transport")
	}
	// A saved address is not an enrolment token; replay remains refused.
	if _, err := tr.Join(ctx, agent.JoinRequest{ProtocolVersion: agent.ProtocolVersion, JoinToken: token, Name: "second-host"}); err == nil {
		t.Fatal("spent join token was accepted")
	}
}
