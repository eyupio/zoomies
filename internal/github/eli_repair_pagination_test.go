package github

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"testing"
)

func TestRepairChecksReadLaterPagesWithoutCertifyingIncompleteCoverage(t *testing.T) {
	for _, tt := range []struct {
		name, first, second, want string
		denySecond                bool
		total, pages              int
	}{
		{"all 101 checks passed", "success", "success", "passed", false, 101, 2},
		{"the last check failed", "success", "failure", "failed", false, 101, 2},
		{"a later page cannot be read", "success", "success", "", true, 101, 2},
		{"a known failure survives a later read error", "failure", "success", "failed", true, 101, 2},
		{"more than the bounded read", "success", "success", "waiting", false, 1100, 10},
	} {
		t.Run(tt.name, func(t *testing.T) {
			pages := 0
			c := repairTestClient(t, func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				if strings.HasSuffix(r.URL.Path, "/status") {
					w.Write([]byte(`{"total_count":0,"state":"pending"}`))
					return
				}
				if strings.HasSuffix(r.URL.Path, "/actions/runs") {
					w.Write([]byte(`{"workflow_runs":[]}`))
					return
				}
				page, _ := strconv.Atoi(r.URL.Query().Get("page"))
				page = max(page, 1)
				pages++
				if tt.denySecond && page == 2 {
					w.WriteHeader(http.StatusForbidden)
					w.Write([]byte(`{"message":"checks denied"}`))
					return
				}
				runs := make([]map[string]string, min(100, tt.total-(page-1)*100))
				for i := range runs {
					conclusion := tt.second
					if page == 1 {
						conclusion = tt.first
					}
					runs[i] = map[string]string{"status": "completed", "conclusion": conclusion}
				}
				json.NewEncoder(w).Encode(map[string]any{"total_count": tt.total, "check_runs": runs})
			})
			state, err := c.RepairChecks(context.Background(), "acme/repo", "head")
			if state != tt.want || pages != tt.pages || (state == "") != (err != nil) {
				t.Fatalf("state=%q, pages=%d, error=%v; want %q after %d pages", state, pages, err, tt.want, tt.pages)
			}
		})
	}
}
