package api

import (
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/eyupio/zoomies/internal/analysis"
	"github.com/eyupio/zoomies/internal/auth"
	"github.com/eyupio/zoomies/internal/controller"
	"github.com/eyupio/zoomies/internal/store"
)

// maxReleaseName bounds a and b. Neither internal/version nor the jobs'
// controller_version column bounds a release's name, so this is the bound the
// installer holds a binary's reported version to, which is a few characters
// for any real build. A longer name names no release this fleet stamped, and
// would be carried into every sentence and label of the answer.
const maxReleaseName = 128

// defaultAnalysisSpan is the period an analysis reads when the caller names
// none: a day is what "why was it slow today" means, and short enough that the
// per-minute reads behind a finding stay small.
const defaultAnalysisSpan = 24 * time.Hour

// statsWindowFits refuses a period the job statistics and the analyses cannot
// read: one that does not end after it starts, and one longer than
// limits.job_stats_window, which bounds both. what is the reader and its verb
// ("job statistics cover"), so each says the one sentence in its own words.
func (s *Server) statsWindowFits(w http.ResponseWriter, since, until time.Time, what string) bool {
	if !since.Before(until) {
		badRequestField(w, "since", "since must be before until")
		return false
	}
	if span, limit := until.Sub(since), s.cfg().Limits.JobStatsSpan(); span > limit {
		badRequestField(w, "since", controller.SpanRefusal(span, limit, what))
		return false
	}
	return true
}

// analysisWindow is the period an analysis route reads: until defaults to now
// and since to a day before it, never further back than the limit allows, so a
// request that named nothing is never refused for a period it did not ask for.
//
// An until past now ends now here as it does in the controller, and before the
// default and the span are worked out from it: otherwise an until two days out
// would default since into the future and be refused on a field the caller
// never sent, and a since at the limit would be refused as too long for hours
// nobody reads.
func (s *Server) analysisWindow(w http.ResponseWriter, r *http.Request) (controller.AnalysisWindow, bool) {
	since, err := queryTime(r, "since")
	if err != nil {
		badRequestField(w, "since", err.Error())
		return controller.AnalysisWindow{}, false
	}
	until, err := queryTime(r, "until")
	if err != nil {
		badRequestField(w, "until", err.Error())
		return controller.AnalysisWindow{}, false
	}
	now := s.ctrl.Now()
	out := controller.AnalysisWindow{Until: now}
	if until != nil && until.Before(now) {
		out.Until = *until
	}
	out.Since = out.Until.Add(-min(defaultAnalysisSpan, s.cfg().Limits.JobStatsSpan()))
	if since != nil {
		out.Since = *since
		if !out.Since.Before(now) {
			badRequestField(w, "since", controller.ErrSinceNotPast.Error())
			return controller.AnalysisWindow{}, false
		}
	}
	if !s.statsWindowFits(w, out.Since, out.Until, "an analysis covers") {
		return controller.AnalysisWindow{}, false
	}
	return out, true
}

// answerAnalysis renders a finding, or what stopped it.
//
// The three comparison refusals carry the controller's own sentence, which
// says what to change, under a code of their own. A pair that is one release
// is a 400: no data makes it answerable. A fleet without two releases to
// choose, or a period past retention.jobs with no midnight to end on, is a
// 409: the request is well formed and the fleet's records refuse it, as
// update.nothing_newer is a 409 for a fleet with nothing newer to take. A 422
// here means a form's fields are wrong, and these name no field to fix.
func (s *Server) answerAnalysis(w http.ResponseWriter, r *http.Request, doing string, finding any, err error) {
	var missing controller.NotFoundError
	switch {
	case err == nil:
		writeJSON(w, http.StatusOK, finding)
	case errors.As(err, &missing):
		notFound(w, fmt.Sprintf("there is no %s %s in this fleet; it may have been removed, or the id is wrong", missing.Kind, missing.ID))
	case errors.Is(err, store.ErrNotFound):
		notFound(w, err.Error())
	case errors.Is(err, controller.ErrSinceNotPast):
		badRequestField(w, "since", err.Error())
	case errors.Is(err, controller.ErrSameRelease):
		writeError(w, http.StatusBadRequest, errorEnvelope{Error: errorBody{
			Code: codeAnalysisSameRelease, Message: err.Error(), Field: "a"}})
	case errors.Is(err, controller.ErrTooFewReleases):
		writeError(w, http.StatusConflict, errorEnvelope{Error: errorBody{
			Code: codeAnalysisTooFewReleases, Message: err.Error()}})
	case errors.Is(err, controller.ErrWholeDaysOnly):
		writeError(w, http.StatusConflict, errorEnvelope{Error: errorBody{
			Code: codeAnalysisWholeDaysOnly, Message: err.Error(), Field: "since"}})
	default:
		s.internal(w, r, doing, err)
	}
}

// handleAnalyseQueueWait answers GET /api/v1/analysis/queue-wait.
func (s *Server) handleAnalyseQueueWait(w http.ResponseWriter, r *http.Request) {
	win, ok := s.analysisWindow(w, r)
	if !ok {
		return
	}
	f, err := s.ctrl.AnalyseQueueWait(r.Context(), win, r.URL.Query().Get("pool_id"))
	s.answerAnalysis(w, r, "analysing queue wait", f, err)
}

// handleAnalyseCompare answers GET /api/v1/analysis/compare.
func (s *Server) handleAnalyseCompare(w http.ResponseWriter, r *http.Request) {
	win, ok := s.analysisWindow(w, r)
	if !ok {
		return
	}
	q := r.URL.Query()
	req := controller.CompareRequest{By: q.Get("by"), A: q.Get("a"), B: q.Get("b"), Since: win.Since, Until: win.Until}
	switch req.By {
	case "", "version":
	case "period":
		// A release named to a comparison by period would be quietly
		// ignored, and the answer read as one about that release.
		for _, name := range []string{"a", "b"} {
			if q.Get(name) != "" {
				badRequestField(w, name, "a and b name releases, which a comparison by period does not read; leave them out, or compare by=version")
				return
			}
		}
	default:
		badRequestField(w, "by", fmt.Sprintf("a comparison is by version or period, not %q", req.By))
		return
	}
	for _, name := range []string{"a", "b"} {
		if len(q.Get(name)) > maxReleaseName {
			badRequestField(w, name, fmt.Sprintf("%s is longer than a release's name can be (%d bytes at most); name a release such as v1.3.2", name, maxReleaseName))
			return
		}
	}
	f, err := s.ctrl.AnalyseCompare(r.Context(), req)
	s.answerAnalysis(w, r, "comparing", f, err)
}

// handleAnalyseHostPressure answers GET /api/v1/analysis/host-pressure.
func (s *Server) handleAnalyseHostPressure(w http.ResponseWriter, r *http.Request) {
	hostID := r.URL.Query().Get("host_id")
	if hostID == "" {
		badRequestField(w, "host_id", "name the host to read with host_id; the pressure analysis is about one host")
		return
	}
	win, ok := s.analysisWindow(w, r)
	if !ok {
		return
	}
	f, err := s.ctrl.AnalyseHostPressure(r.Context(), win, hostID)
	// The route is gated on hosts.read, and a host's view names no job: a
	// token narrowed to hosts:read is told when things went wrong and how,
	// never which jobs they were, which only jobs.read may read.
	if err == nil && !auth.Allowed(Identity(r.Context()), auth.ActionJobsRead) {
		f = analysis.WithholdJobNames(f)
	}
	s.answerAnalysis(w, r, "analysing host pressure", f, err)
}

// handleAnalyseStartupFailures answers GET /api/v1/analysis/startup-failures.
func (s *Server) handleAnalyseStartupFailures(w http.ResponseWriter, r *http.Request) {
	win, ok := s.analysisWindow(w, r)
	if !ok {
		return
	}
	q := r.URL.Query()
	f, err := s.ctrl.AnalyseStartupFailures(r.Context(), win, q.Get("pool_id"), q.Get("host_id"))
	s.answerAnalysis(w, r, "analysing startup failures", f, err)
}

// handleAnalyseRecurringFailures answers GET /api/v1/analysis/recurring-failures.
func (s *Server) handleAnalyseRecurringFailures(w http.ResponseWriter, r *http.Request) {
	win, ok := s.analysisWindow(w, r)
	if !ok {
		return
	}
	f, err := s.ctrl.AnalyseRecurringFailures(r.Context(), win)
	s.answerAnalysis(w, r, "analysing recurring failures", f, err)
}
