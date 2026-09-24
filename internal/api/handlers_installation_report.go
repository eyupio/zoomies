package api

import (
	"net/http"
	"time"

	"github.com/eyupio/zoomies/internal/auth"
	"github.com/eyupio/zoomies/internal/store"
)

// defaultReportWindow is a month, which is the period the report exists for:
// an operator's own record of how each installation was served over one.
const defaultReportWindow = 30 * 24 * time.Hour

// installationReportResponse is GET /installations/{id}/report.
//
// It names no repository. The counts are per installation, and the route is
// gated on usage.read, which a token can hold without jobs.read -- the scope
// that reads repository names. An installation's target can itself be a
// repository, so it is only rendered to a caller who could read it anyway.
type installationReportResponse struct {
	Installation installationRef `json:"installation"`
	Window       string          `json:"window"`
	*store.InstallationReport
	// Unavailable says in words which figures do not cover the whole window,
	// and why, so a client does not have to derive it from the instants.
	Unavailable []string `json:"unavailable"`
}

type installationRef struct {
	ID         string `json:"id"`
	Target     string `json:"target,omitempty"`
	TargetType string `json:"target_type,omitempty"`
}

func (s *Server) handleInstallationReport(w http.ResponseWriter, r *http.Request) {
	window, err := queryDuration(r, "window", defaultReportWindow)
	if err != nil {
		badRequestField(w, "window", err.Error())
		return
	}
	if window > maxUsageRange {
		badRequestField(w, "window", "window cannot exceed 366 days (8784h)")
		return
	}
	inst, err := s.ctrl.Store().GetInstallation(r.Context(), chiURLParam(r, "id"))
	if err != nil {
		s.fail(w, r, "reading the installation", err)
		return
	}
	now := s.ctrl.Now()
	rep, err := s.ctrl.Store().InstallationReport(r.Context(), inst.ID, now.Add(-window), now)
	if err != nil {
		s.internal(w, r, "computing the installation report", err)
		return
	}
	ref := installationRef{ID: inst.ID}
	if targetVisible(Identity(r.Context()), inst) {
		ref.Target, ref.TargetType = inst.Target, string(inst.TargetType)
	}
	writeJSON(w, http.StatusOK, installationReportResponse{
		Installation: ref, Window: window.String(), InstallationReport: rep, Unavailable: reportGaps(rep),
	})
}

// targetVisible says whether a caller may be shown an installation's target.
// An organisation's name is what installations.read reads; a repository's is
// also what jobs.read reads, and a caller with only one of them is shown
// neither.
func targetVisible(id *auth.Identity, inst *store.Installation) bool {
	if id == nil || !auth.Allowed(id, auth.ActionInstallationsRead) {
		return false
	}
	return inst.TargetType != store.TargetRepo || auth.Allowed(id, auth.ActionJobsRead)
}

// reportGaps renders the parts of the window a report could not answer.
func reportGaps(rep *store.InstallationReport) []string {
	out := []string{}
	day := func(t time.Time) string { return t.UTC().Format("2 January 2006 15:04 MST") }
	if rep.CountsFrom.After(rep.From) {
		out = append(out, "Counts before "+day(rep.CountsFrom)+" are unavailable: the rows they are counted from "+
			"were pruned before the daily roll-up began keeping them.")
	}
	if rep.TimingsFrom.After(rep.From) {
		out = append(out, "Timings before "+day(rep.TimingsFrom)+" are unavailable: they are measured from runner "+
			"sessions, which retention.runner_sessions has pruned, and the daily roll-up keeps counts only.")
	}
	return out
}
