package api

import (
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/eyupio/zoomies/internal/store"
)

// The activity charts count a job in the hour it finished, so the list behind
// one of their squares asks when jobs finished. A job still in hand has not
// finished in any window and is in none.
func TestJobsListTakesAWindowOnWhenJobsFinished(t *testing.T) {
	h := newHarness(t)
	pool := h.pool(h.installation(), "linux-x64")
	_, cookie := h.user("viewer", store.RoleViewer)
	now := time.Now().UTC().Truncate(time.Second)
	at := func(d time.Duration) string { return url.QueryEscape(now.Add(-d).Format(time.RFC3339)) }

	h.job(pool, store.JobQueued)
	finish := func(ago time.Duration) {
		j := h.job(pool, store.JobCompleted)
		done := now.Add(-ago)
		j.Conclusion, j.CompletedAt = "success", &done
		if _, err := h.st.UpsertJob(h.ctx, j); err != nil {
			t.Fatalf("UpsertJob: %v", err)
		}
	}
	finish(3 * time.Hour)    // before the window
	finish(90 * time.Minute) // inside it
	finish(time.Hour)        // exactly its end, which is not included

	var jobs page[jobResponse]
	resp := h.do(request{method: http.MethodGet, cookie: cookie,
		path: "/api/v1/jobs?completed_since=" + at(2*time.Hour) + "&completed_until=" + at(time.Hour)})
	resp.mustStatus(t, http.StatusOK, "a completion window")
	resp.into(t, &jobs)
	if jobs.Total != 1 {
		t.Fatalf("finished in [now-2h, now-1h) total = %d, want the one job inside it", jobs.Total)
	}

	// The Workflows page shares the filters, so it takes the window too.
	h.do(request{method: http.MethodGet, cookie: cookie,
		path: "/api/v1/workflow-runs?completed_since=" + at(2*time.Hour)}).
		mustStatus(t, http.StatusOK, "the runs list takes the same window")
}

func TestJobsListRefusesACompletionWindowItCannotRead(t *testing.T) {
	h := newHarness(t)
	_, cookie := h.user("viewer", store.RoleViewer)
	now := time.Now().UTC()
	at := func(d time.Duration) string { return url.QueryEscape(now.Add(-d).Format(time.RFC3339)) }
	for name, tc := range map[string]struct{ query, want string }{
		"an unreadable start":     {"completed_since=yesterday", "completed_since"},
		"an unreadable end":       {"completed_until=soon", "completed_until"},
		"an end before the start": {"completed_since=" + at(time.Hour) + "&completed_until=" + at(2*time.Hour), "start must be before end"},
	} {
		resp := h.do(request{method: http.MethodGet, path: "/api/v1/jobs?" + tc.query, cookie: cookie})
		resp.mustStatus(t, http.StatusBadRequest, name)
		if !strings.Contains(string(resp.body), tc.want) {
			t.Errorf("%s: %s, want it to mention %q", name, resp.body, tc.want)
		}
	}
}
