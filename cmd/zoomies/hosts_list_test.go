package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// A throttled host has fewer slots than its operator configured, and the
// list has to show the slots it is actually taking: 2/8 on a host throttled
// to four reads as six slots free, and the scheduler will refuse all six.
func TestHostsListShowsAThrottledHostsEffectiveSlots(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"items":[
		  {"id":"hst_a","name":"calm","capacity":4,"active_runners":1,"free":3,"effective_capacity":4,
		   "backends":["docker"],"healthy":true,"cpus":8,"memory_mb":16384,"last_heartbeat":"2026-01-01T00:00:00Z"},
		  {"id":"hst_b","name":"pressed","capacity":4,"active_runners":1,"free":1,"effective_capacity":2,
		   "throttle_reason":"throttled to 2 of 4 slots (step 2 of 3) after sustained pressure: the 1-minute load average is 30.0, at least twice the host's 8 CPUs; running jobs continue, and the throttle lifts one step after 5m of calm",
		   "backends":["docker"],"healthy":true,"cpus":8,"memory_mb":16384,"last_heartbeat":"2026-01-01T00:00:00Z"},
		  {"id":"hst_c","name":"old-controller","capacity":4,"active_runners":2,"free":2,
		   "backends":["docker"],"healthy":true,"cpus":8,"memory_mb":16384,"last_heartbeat":"2026-01-01T00:00:00Z"}
		],"total":3}`))
	}))
	defer srv.Close()

	e, out, errOut := newTestEnv(t)
	if code := dispatch(context.Background(), e, []string{"hosts", "list", "--url", srv.URL}); code != exitOK {
		t.Fatalf("exit code = %d\n%s", code, errOut)
	}
	got := out.String()
	for _, want := range []string{
		"1/4", "1/2 (of 4)", "throttled",
		"pressed is throttled to 2 of 4 slots",
		// A controller older than effective_capacity sends none; that is
		// not a host with no slots.
		"2/4",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("output does not contain %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "2/0") || strings.Contains(got, "1/4 (of") {
		t.Errorf("a host with no throttle was shown as throttled:\n%s", got)
	}
}
