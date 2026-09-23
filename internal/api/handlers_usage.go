package api

import (
	"encoding/csv"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/eyupio/zoomies/internal/store"
)

const maxUsageRange = 366 * 24 * time.Hour

// maxHourlyRange bounds a request for hourly buckets. A fortnight is 336
// buckets a row, which is a punch card of a week with room to spare; a year
// of hours would be 8,784 a row, multiplied by every repository.
const maxHourlyRange = 14 * 24 * time.Hour

// usageQuery is what the two usage routes agree a request means.
type usageQuery struct {
	from, to time.Time
	group    store.UsageGroup
	interval store.UsageInterval
}

func usageParams(r *http.Request) (usageQuery, error) {
	var q usageQuery
	from, to := r.URL.Query().Get("from"), r.URL.Query().Get("to")
	f, err := time.Parse(time.RFC3339, from)
	if err != nil {
		return q, fmt.Errorf("from is required and must be RFC 3339")
	}
	t, err := time.Parse(time.RFC3339, to)
	if err != nil {
		return q, fmt.Errorf("to is required and must be RFC 3339")
	}
	if !f.Before(t) {
		return q, fmt.Errorf("from must be before to")
	}
	if t.Sub(f) > maxUsageRange {
		return q, fmt.Errorf("date range cannot exceed 366 days")
	}
	g := store.UsageGroup(r.URL.Query().Get("group_by"))
	if g == "" {
		g = store.UsageByPool
	}
	switch g {
	case store.UsageByHost, store.UsageByPool, store.UsageByInstallation, store.UsageByRepository, store.UsageByWorkflow:
	default:
		return q, fmt.Errorf("group_by must be installation, repository, workflow, host, or pool")
	}
	interval := store.UsageInterval(r.URL.Query().Get("interval"))
	switch interval {
	case store.UsageAutoInterval, store.UsageDaily:
	case store.UsageHourly:
		if t.Sub(f) > maxHourlyRange {
			return q, fmt.Errorf("hourly buckets cover at most 14 days; ask for daily buckets, or a shorter range")
		}
	default:
		return q, fmt.Errorf("interval must be hour or day")
	}
	return usageQuery{from: f, to: t, group: g, interval: interval}, nil
}

func (s *Server) handleUsage(w http.ResponseWriter, r *http.Request) {
	q, err := usageParams(r)
	if err != nil {
		badRequest(w, err.Error())
		return
	}
	f, t, g := q.from, q.to, q.group
	rows, err := s.ctrl.Store().UsageWithInterval(r.Context(), f, t, g, q.interval)
	if err != nil {
		s.internal(w, r, "querying usage", err)
		return
	}
	rows = filterUsageRows(rows, r)
	history, err := s.usageHistoryFrom(r)
	if err != nil {
		s.internal(w, r, "reading where usage history begins", err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"from": f, "to": t, "group_by": g, "items": rows,
		"costs_are_estimates": true,
		// Told to the client even when items is empty, so the page can explain
		// an absent runner-hours column rather than leaving it blank.
		"allocation_attributable": store.UsageAllocationAttributable(g),
		"history_from":            history,
	})
}

// usageHistoryFrom is the earliest instant each side of the usage aggregate
// can still be computed from, given what the prune loop has already deleted.
//
// Job figures are read from job rows, which are kept thirty days by default.
// Runner allocation is read from the usage ledger -- the daily roll-up and the
// sessions behind it -- as well as the runner rows, so it begins where the
// ledger begins however short retention.runners is: on a database upgraded
// into the ledger, that is the oldest runner row the upgrade found. A report
// over a longer range is not wrong for the days it has history for, but it is
// silently short for the days it has not, so the response says where each
// history begins and the page says so beside the figure. A window of zero
// keeps everything and is reported as null.
func (s *Server) usageHistoryFrom(r *http.Request) (map[string]*time.Time, error) {
	now := s.ctrl.Now()
	since := func(window time.Duration) *time.Time {
		if window <= 0 {
			return nil
		}
		t := now.Add(-window)
		return &t
	}
	ret := s.cfg().Retention
	runners := since(ret.Runners)
	if runners != nil {
		ledger, err := s.ctrl.Store().UsageLedgerFrom(r.Context())
		if err != nil {
			return nil, err
		}
		if ledger != nil && ledger.Before(*runners) {
			runners = ledger
		}
	}
	return map[string]*time.Time{"jobs": since(ret.Jobs), "runners": runners}, nil
}

func (s *Server) handleUsageCSV(w http.ResponseWriter, r *http.Request) {
	q, err := usageParams(r)
	if err != nil {
		badRequest(w, err.Error())
		return
	}
	rows, err := s.ctrl.Store().UsageWithInterval(r.Context(), q.from, q.to, q.group, q.interval)
	if err != nil {
		s.internal(w, r, "querying usage", err)
		return
	}
	rows = filterUsageRows(rows, r)
	history, err := s.usageHistoryFrom(r)
	if err != nil {
		s.internal(w, r, "reading where usage history begins", err)
		return
	}
	// The export leaves the page behind, so it carries the page's caveat with
	// it: a spreadsheet reconciled against a bill should say where its runner
	// history begins as plainly as the page does. It is a column on every row
	// rather than a preamble, because a preamble breaks every CSV reader.
	instant := func(t *time.Time) string {
		if t == nil {
			return ""
		}
		return t.UTC().Format(time.RFC3339)
	}
	jobsFrom, runnersFrom := instant(history["jobs"]), instant(history["runners"])
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="zoomies-usage.csv"`)
	c := csv.NewWriter(w)
	_ = c.Write([]string{"group", "job_execution_seconds", "allocated_runner_seconds", "jobs_queued", "jobs_started", "jobs_completed", "average_queue_wait_seconds", "peak_concurrency", "estimated_cost", "history_from_jobs", "history_from_runners"})
	for _, x := range rows {
		cost := ""
		if x.EstimatedCost != nil {
			cost = strconv.FormatFloat(*x.EstimatedCost, 'f', 2, 64)
		}
		// A blank cell is the honest rendering of "not calculated for this
		// grouping"; a spreadsheet would sum a zero.
		_ = c.Write([]string{csvText(x.Key), fmt.Sprint(x.JobExecutionSeconds), optionalFloat(x.AllocatedRunnerSeconds),
			strconv.Itoa(x.Jobs), strconv.Itoa(x.JobsStarted), strconv.Itoa(x.JobsCompleted),
			optionalFloat(x.AverageQueueWaitSeconds), strconv.Itoa(x.PeakConcurrency), cost, jobsFrom, runnersFrom})
	}
	c.Flush()
}

func optionalFloat(v *float64) string {
	if v == nil {
		return ""
	}
	return fmt.Sprint(*v)
}

// csvText makes a free-text cell safe to open in a spreadsheet.
//
// Excel and Sheets evaluate a cell that begins with =, +, -, @, a tab or a
// carriage return, and CSV quoting does not stop them: the quotes are gone by
// the time the cell is read. The group column carries names that GitHub
// payloads supplied -- a workflow can be called anything, by anyone who can
// push to a repository the App can see -- so a leading formula character is
// somebody else's to choose. A leading apostrophe is the spreadsheet
// convention for "this is text", and it is not shown in the cell.
func csvText(s string) string {
	if s == "" {
		return s
	}
	switch s[0] {
	case '=', '+', '-', '@', '\t', '\r':
		return "'" + s
	}
	return s
}

func filterUsageRows(rows []store.UsageRow, r *http.Request) []store.UsageRow {
	if keys, ok := r.URL.Query()["key"]; ok && len(keys) > 0 {
		out := make([]store.UsageRow, 0)
		for _, row := range rows {
			if row.Key == keys[0] {
				out = append(out, row)
			}
		}
		return out
	}
	return rows
}
