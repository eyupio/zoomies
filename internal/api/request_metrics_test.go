package api

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sort"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/eyupio/zoomies/internal/store"
)

// requestSeries is /metrics' zoomies_api_requests_total as the scraper would
// see it, as "group/class" to count.
func requestSeries(t testing.TB, h *harness) map[string]float64 {
	t.Helper()
	families, err := h.ctrl.Registry().Gather()
	if err != nil {
		t.Fatalf("gathering metrics: %v", err)
	}
	out := map[string]float64{}
	for _, f := range families {
		if f.GetName() != "zoomies_api_requests_total" {
			continue
		}
		for _, m := range f.GetMetric() {
			var group, class string
			for _, l := range m.GetLabel() {
				switch l.GetName() {
				case "group":
					group = l.GetValue()
				case "status":
					class = l.GetValue()
				}
			}
			out[group+"/"+class] = m.GetCounter().GetValue()
		}
	}
	return out
}

// timedGroups is the groups zoomies_api_request_seconds has any observation
// for.
func timedGroups(t testing.TB, h *harness) []string {
	t.Helper()
	families, err := h.ctrl.Registry().Gather()
	if err != nil {
		t.Fatalf("gathering metrics: %v", err)
	}
	var out []string
	for _, f := range families {
		if f.GetName() != "zoomies_api_request_seconds" {
			continue
		}
		for _, m := range f.GetMetric() {
			for _, l := range m.GetLabel() {
				if l.GetName() == "group" {
					out = append(out, l.GetValue())
				}
			}
		}
	}
	sort.Strings(out)
	return out
}

func TestARoutesGroupIsTheFirstPartOfItsPattern(t *testing.T) {
	for _, c := range []struct {
		pattern, path, want string
	}{
		{"/api/v1/hosts/{id}/cordon", "/api/v1/hosts/host_1/cordon", "hosts"},
		{"/api/v1/runners/{id}/logs", "/api/v1/runners/run_1/logs", "runners"},
		{"/api/v1/agent/heartbeat", "/api/v1/agent/heartbeat", "agent"},
		{"/api/v1/provider-setups/{id}/complete", "/api/v1/provider-setups/x/complete", "provider-setups"},
		{"/healthz", "/healthz", "health"},
		{"/readyz", "/readyz", "health"},
		{"/metrics", "/metrics", "metrics"},
		{"/mcp", "/mcp", "mcp"},
		{"/api/openapi.yaml", "/api/openapi.yaml", "api"},
		{"/*", "/anything", "ui"},
		// Nothing matched: the UI asking for one of its files, or an API path
		// that is not there.
		{"", "/assets/app.js", "ui"},
		{"", "/api/v1/not-a-route", "unmatched"},
		// The router reports an API path it has no route for as the pattern of
		// the sub-router that refused it, which ends in a star. That is a miss
		// and not the UI, and read as the group after /api/v1/ it would be one.
		{"/api/v1/*", "/api/v1/not-a-route", "unmatched"},
		// A sub-router's own miss is still that sub-router's group.
		{"/api/v1/agent/*", "/api/v1/agent/nope", "agent"},
		// A pattern is source, but a label that is not tidy is still not a label.
		{"/Odd Name/{id}", "/x", "odd_name"},
	} {
		if got := routeGroup(c.pattern, c.path); got != c.want {
			t.Errorf("routeGroup(%q, %q) = %q, want %q", c.pattern, c.path, got, c.want)
		}
	}
}

// The timings leave a route out by its pattern, and a pattern is a string a
// rename can quietly stop matching. Each one has to be a route the router
// serves, or the stream it names is being timed again without anybody noticing.
func TestEveryLongLivedRouteIsOneTheRouterServes(t *testing.T) {
	h := newHarness(t)
	routes, ok := h.api.handler.(chi.Routes)
	if !ok {
		t.Fatalf("the server's handler is a %T, not a chi router", h.api.handler)
	}
	served := map[string]bool{}
	if err := chi.Walk(routes, func(_, route string, _ http.Handler, _ ...func(http.Handler) http.Handler) error {
		served[route] = true
		return nil
	}); err != nil {
		t.Fatalf("chi.Walk: %v", err)
	}
	for pattern := range longLivedRoutes {
		if !served[pattern] {
			t.Errorf("%s is named as long-lived for the request timings but the router serves no such route", pattern)
		}
	}
}

// The property the label has to have: it grows with the code and not with the
// traffic. A hundred made-up paths of each kind, and a real route with a
// hundred different ids, must leave the same few series behind as one of each.
func TestRequestsAreCountedByTheRouteTheyMatchedNotThePathTheyAsked(t *testing.T) {
	h := newHarness(t)
	token := h.token("viewer", store.RoleViewer)

	h.do(request{method: http.MethodGet, path: "/healthz"}).mustStatus(t, http.StatusOK, "the health check")
	for i := range 100 {
		h.do(request{method: http.MethodGet, path: fmt.Sprintf("/api/v1/made-up-%d", i), token: token})
		h.do(request{method: http.MethodGet, path: fmt.Sprintf("/made-up-page-%d", i)})
		h.do(request{method: http.MethodGet, path: fmt.Sprintf("/api/v1/pools/pool_%d", i), token: token})
	}

	got := requestSeries(t, h)
	if len(got) > 6 {
		t.Errorf("%d series after 300 requests to 201 different paths, want a handful: %v", len(got), got)
	}
	for series, want := range map[string]float64{"health/2xx": 1, "unmatched/4xx": 100, "pools/4xx": 100} {
		if got[series] != want {
			t.Errorf("%s = %v, want %v (all series: %v)", series, got[series], want, got)
		}
	}
	// The UI answers a page it does not have with its own shell, so whichever
	// class it uses, the hundred are one series.
	var ui float64
	for series, n := range got {
		if len(series) > 3 && series[:3] == "ui/" {
			ui += n
		}
	}
	if ui != 100 {
		t.Errorf("%v requests counted for the ui group, want 100 (all series: %v)", ui, got)
	}
}

// How long a stream stayed open is how long its client stayed. One that stays
// forty minutes would put its group's p99 in the top bucket for good, so a
// stream is counted when it ends and never timed.
func TestAStreamIsCountedWhenItEndsAndNeverTimed(t *testing.T) {
	h := newHarness(t)
	token := h.token("viewer", store.RoleViewer)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, h.srv.URL+"/api/v1/events", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("opening the event stream: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("the event stream answered %d, want 200", resp.StatusCode)
	}
	buf := make([]byte, 1)
	if _, err := resp.Body.Read(buf); err != nil {
		t.Fatalf("reading the first byte of the stream: %v", err)
	}
	// Not counted yet: there is no status to count until the handler returns.
	if n := requestSeries(t, h)["events/2xx"]; n != 0 {
		t.Fatalf("a stream still open was counted %v times, want 0", n)
	}

	cancel()
	deadline := time.Now().Add(3 * time.Second)
	for requestSeries(t, h)["events/2xx"] != 1 {
		if time.Now().After(deadline) {
			t.Fatalf("the stream was never counted after it ended: %v", requestSeries(t, h))
		}
		time.Sleep(5 * time.Millisecond)
	}
	for _, g := range timedGroups(t, h) {
		if g == "events" {
			t.Error("a stream was timed, and one that stayed forty minutes would have set the group's p99 for good")
		}
	}
}

// What the middleware costs a request is what an operator pays for the
// numbers, so it is measured. The benchmark first checks that it is serving the
// request its name says and that the middleware saw it, so that a faster
// number can never come from a request that stopped reaching it.
func BenchmarkAHealthCheckThroughTheAccessLog(b *testing.B) {
	h := newHarness(b)
	handler := h.api.Handler()

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	if rec.Code != http.StatusOK {
		b.Fatalf("the health check answered %d, want 200", rec.Code)
	}
	if got := requestSeries(b, h)["health/2xx"]; got != 1 {
		b.Fatalf("the access log counted %v health checks before timing, want 1: the benchmark is not exercising the middleware", got)
	}

	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/healthz", nil))
	}
}

// The cost the metrics add to one request is the group, the two lookups of a
// labelled series and two increments. It is timed on its own, because beside a
// request that reaches the database it is too small to see in the whole.
// Before timing it checks that the call it is timing was counted, so a faster
// number cannot come from a call that stopped reaching the registry.
func BenchmarkRecordingAServedRequest(b *testing.B) {
	h := newHarness(b)
	const pattern, path = "/api/v1/hosts/{id}/cordon", "/api/v1/hosts/host_1/cordon"

	h.ctrl.ObserveRequest(routeGroup(pattern, path), http.StatusOK, 3*time.Millisecond, false)
	if got := requestSeries(b, h)["hosts/2xx"]; got != 1 {
		b.Fatalf("zoomies_api_requests_total{group=hosts,status=2xx} = %v before timing, want 1: the benchmark is not recording what it times", got)
	}

	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		h.ctrl.ObserveRequest(routeGroup(pattern, path), http.StatusOK, 3*time.Millisecond, false)
	}
}
