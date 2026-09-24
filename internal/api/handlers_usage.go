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

// maxSubDailyBuckets bounds a request for buckets narrower than a day. A
// fortnight of hours is 336 buckets a row, which is a punch card of a week
// with room to spare; a year of hours would be 8,784 a row, multiplied by
// every repository. The bound is on the count rather than on the range, so
// a wider bucket reaches further back for the same cost: 56 days of four
// hours, 112 of eight.
const maxSubDailyBuckets = 14 * 24

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
	if !interval.Known() {
		return q, fmt.Errorf("interval must be hour, 2h, 3h, 4h, 6h, 8h, 12h or day")
	}
	// Left to the rule, a window is hourly only up to two days, well inside.
	if width := interval.Width(f, t); width < 24*time.Hour {
		if reach := maxSubDailyBuckets * width; t.Sub(f) > reach {
			name := "hourly"
			if width > time.Hour {
				name = string(interval)
			}
			return q, fmt.Errorf("%s buckets cover at most %d days; ask for wider buckets, or a shorter range",
				name, int(reach/(24*time.Hour)))
		}
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
	// Grouped by installation, each row also carries the installation
	// report's counts over the same range, and where they are complete from.
	// Any other grouping leaves them blank: the counts are defined per
	// installation and a share of them per repository would be invented.
	var counts map[string]store.InstallationCounts
	countsFrom := ""
	if q.group == store.UsageByInstallation {
		var from time.Time
		counts, from, _, err = s.ctrl.Store().InstallationCountsBetween(r.Context(), q.from, q.to)
		if err != nil {
			s.internal(w, r, "counting the installation report", err)
			return
		}
		countsFrom = from.UTC().Format(time.RFC3339)
	}
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="zoomies-usage.csv"`)
	c := csv.NewWriter(w)
	_ = c.Write([]string{"group", "job_execution_seconds", "allocated_runner_seconds", "jobs_queued", "jobs_started", "jobs_completed", "average_queue_wait_seconds", "peak_concurrency", "estimated_cost", "history_from_jobs", "history_from_runners",
		"jobs_observed", "jobs_eligible", "jobs_created_for", "jobs_ran_here", "jobs_ran_elsewhere", "jobs_fleet_fault",
		"cleanup_pending", "cleanup_converged", "counts_from"})
	for _, x := range rows {
		cost := ""
		if x.EstimatedCost != nil {
			cost = strconv.FormatFloat(*x.EstimatedCost, 'f', 2, 64)
		}
		// A blank cell is the honest rendering of "not calculated for this
		// grouping"; a spreadsheet would sum a zero.
		report := make([]string, 9)
		if counts != nil {
			n := counts[x.Key]
			for i, v := range []int{n.Observed, n.Eligible, n.CreatedFor, n.RanHere, n.RanElsewhere, n.FleetFault,
				n.CleanupPending, n.CleanupConverged} {
				report[i] = strconv.Itoa(v)
			}
			report[8] = countsFrom
		}
		_ = c.Write(append([]string{csvText(x.Key), fmt.Sprint(x.JobExecutionSeconds), optionalFloat(x.AllocatedRunnerSeconds),
			strconv.Itoa(x.Jobs), strconv.Itoa(x.JobsStarted), strconv.Itoa(x.JobsCompleted),
			optionalFloat(x.AverageQueueWaitSeconds), strconv.Itoa(x.PeakConcurrency), cost, jobsFrom, runnersFrom}, report...))
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
