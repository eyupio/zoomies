package api

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/eyupio/zoomies/internal/auth"
	"github.com/eyupio/zoomies/internal/controller"
	"github.com/eyupio/zoomies/internal/github"
	"github.com/eyupio/zoomies/internal/optimisation"
	"github.com/eyupio/zoomies/internal/store"
)

var errOptimisationAccess = errors.New("optimisation access withdrawn")

type OptimisationRequest struct {
	InstallationID       string `json:"installation_id"`
	Repository           string `json:"repository"`
	RunID                string `json:"run_id"`
	Attempt              int    `json:"attempt"`
	GitHubJobID          string `json:"github_job_id"`
	JobIndex             *int   `json:"job_index"`
	StepIndex            *int   `json:"step_index"`
	ReviewedHeadSHA      string `json:"reviewed_head_sha,omitempty"`
	ReviewedSourceSHA256 string `json:"reviewed_source_sha256,omitempty"`
	HistoryNotRequired   *bool  `json:"history_not_required,omitempty"`
}

// This is an explicit source-bearing whitelist, never the private envelope.
type OptimisationView struct {
	AfterSHA256    *string    `json:"after_sha256"`
	BindingBasis   string     `json:"binding_basis"`
	Version        string     `json:"version"`
	InstallationID string     `json:"installation_id"`
	Repository     string     `json:"repository"`
	ID             string     `json:"id"`
	CreatedAt      *time.Time `json:"created_at"`
	ReviewedBy     *string    `json:"reviewed_by"`
	HeadSHA        string     `json:"head_sha"`
	SourceSHA256   string     `json:"source_sha256"`
	Path           string     `json:"path"`
	Mode           string     `json:"mode"`
	Content        string     `json:"content"`
	RunID          string     `json:"run_id"`
	Attempt        int        `json:"attempt"`
	GitHubJobID    *string    `json:"github_job_id"`
	StepNumber     *int64     `json:"step_number"`
	JobIndex       int        `json:"job_index"`
	StepIndex      int        `json:"step_index"`
	CheckoutMS     int64      `json:"checkout_ms"`
	Before         *string    `json:"before"`
	After          *string    `json:"after"`
	Verification   string     `json:"verification"`
	Currentness    string     `json:"currentness"`
	SourceFunding  string     `json:"source_funding"`
}

func (s *Server) handleOptimisationPolicy(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	a := s.cfg().Assistant
	writeJSON(w, 200, map[string]bool{"source_enabled": a.OptimisationSourceEnabled, "preview_enabled": a.OptimisationPreviewEnabled, "history_enabled": a.OptimisationHistoryEnabled})
}
func (s *Server) optimisationPolicy(r *http.Request) controller.OptimisationPolicyCheck {
	original := Identity(r.Context())
	return func(ctx context.Context) (optimisation.Policy, error) {
		current, err := s.resolveIdentity(r.WithContext(ctx))
		if err != nil {
			return optimisation.Policy{}, errOptimisationAccess
		}
		if original == nil || current == nil || current.ID != original.ID || current.ID == "" || !current.Can(auth.ActionOptimisationsInspect) {
			return optimisation.Policy{}, errOptimisationAccess
		}
		a := s.cfg().Assistant
		return optimisation.Policy{SourceAllowed: a.OptimisationSourceEnabled && current.Can(auth.ActionOptimisationsInspect), WorkflowEditsAllowed: a.OptimisationPreviewEnabled && current.Can(auth.ActionOptimisationsWrite)}, nil
	}
}
func optimisationDecimal(s string) (int64, bool) {
	n, e := strconv.ParseInt(s, 10, 64)
	return n, e == nil && n > 0 && strconv.FormatInt(n, 10) == s
}
func (s *Server) optimisationInput(w http.ResponseWriter, r *http.Request, capture bool) (OptimisationRequest, github.CheckoutSelection, bool) {
	var in OptimisationRequest
	r.Body = http.MaxBytesReader(w, r.Body, 4096)
	d := json.NewDecoder(r.Body)
	d.DisallowUnknownFields()
	if d.Decode(&in) != nil || d.Decode(new(any)) != io.EOF {
		badRequest(w, "Supply one bounded optimisation request using documented fields.")
		return in, github.CheckoutSelection{}, false
	}
	run, ok := optimisationDecimal(in.RunID)
	job, okJob := optimisationDecimal(in.GitHubJobID)
	if !ok || !okJob || in.Attempt < 1 || in.Attempt > 2147483647 || in.JobIndex == nil || in.StepIndex == nil || in.InstallationID == "" || len(in.InstallationID) > 128 || len(in.Repository) > 240 || in.Repository == "" {
		badRequest(w, "Supply exact positive decimal run/job IDs, an attempt, source indices, installation and repository.")
		return in, github.CheckoutSelection{}, false
	}
	sel := github.CheckoutSelection{RunID: run, Attempt: in.Attempt, GitHubJobID: job, JobIndex: *in.JobIndex, StepIndex: *in.StepIndex}
	if !github.ValidCheckoutSelection(sel) || !capture && (in.HistoryNotRequired != nil || in.ReviewedHeadSHA != "" || in.ReviewedSourceSHA256 != "") {
		badRequest(w, "Use supported source positions and keep history review fields on the capture request only.")
		return in, sel, false
	}
	return in, sel, true
}
func (s *Server) handleInspectOptimisation(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	in, sel, ok := s.optimisationInput(w, r, false)
	if !ok {
		return
	}
	e, err := s.ctrl.InspectOptimisationSource(r.Context(), in.InstallationID, in.Repository, sel, s.optimisationPolicy(r))
	if err != nil {
		s.optimisationError(w, r, err)
		return
	}
	job := strconv.FormatInt(sel.GitHubJobID, 10)
	number := e.StepNumber
	writeJSON(w, 200, OptimisationView{Version: "checkout-web-v1", BindingBasis: e.Basis, InstallationID: in.InstallationID, Repository: in.Repository, HeadSHA: e.Source.HeadSHA, SourceSHA256: e.Timing.SourceSHA256, Path: e.Source.File.Path, Mode: e.Source.File.Mode, Content: e.Source.File.Content, RunID: in.RunID, Attempt: sel.Attempt, GitHubJobID: &job, StepNumber: &number, JobIndex: sel.JobIndex, StepIndex: sel.StepIndex, CheckoutMS: *e.Timing.CheckoutMS, Verification: "inconclusive", Currentness: "not_checked", SourceFunding: "installation_github_read"})
}
func (s *Server) handleCaptureOptimisation(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	in, sel, ok := s.optimisationInput(w, r, true)
	if !ok {
		return
	}
	if in.HistoryNotRequired == nil || !*in.HistoryNotRequired {
		badRequest(w, "Review repository tooling and downstream history/tag needs before declaring full history unnecessary.")
		return
	}
	id := Identity(r.Context())
	if id == nil || id.ID == "" {
		unauthorized(w, "An identified operator is required.")
		return
	}
	needs := false
	review := optimisation.HistoryReview{HeadSHA: in.ReviewedHeadSHA, SourceSHA256: in.ReviewedSourceSHA256, Scope: "repository_and_downstream_consumers", NeedsHistory: &needs}
	saved, err := s.ctrl.CaptureOptimisationPreview(r.Context(), &store.AuditEvent{ActorID: id.ID, ActorKind: id.Kind, ActorName: id.Name, IP: id.IP}, in.InstallationID, in.Repository, sel, review, s.optimisationPolicy(r))
	if err != nil {
		s.optimisationError(w, r, err)
		return
	}
	writeJSON(w, 201, optimisationWebView(saved))
}
func optimisationWebView(saved *controller.RetainedOptimisationPreview) OptimisationView {
	p := saved.Preview
	out := OptimisationView{Version: "checkout-web-v1", BindingBasis: "unknown", AfterSHA256: &p.AfterSHA256, ID: saved.ID, CreatedAt: &saved.CreatedAt, ReviewedBy: &saved.ReviewedBy, InstallationID: saved.InstallationID, Repository: saved.Repository, HeadSHA: p.HeadSHA, SourceSHA256: p.SourceSHA256, Path: p.File.Path, Mode: p.File.Mode, Content: p.File.Content, RunID: p.RunID, Attempt: p.Attempt, JobIndex: p.JobIndex, StepIndex: p.StepIndex, CheckoutMS: p.CheckoutMS, Before: &p.Edit.Before, After: &p.Edit.After, Verification: p.Verification, Currentness: saved.Currentness, SourceFunding: "unknown"}
	if saved.Collection != nil {
		job := strconv.FormatInt(saved.Collection.Selection.GitHubJobID, 10)
		number := saved.Collection.StepNumber
		out.GitHubJobID = &job
		out.StepNumber = &number
		out.SourceFunding = saved.Collection.SourceFunding
		out.BindingBasis = saved.Collection.Basis
	}
	return out
}
func (s *Server) handleGetOptimisation(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if !s.cfg().Assistant.OptimisationHistoryEnabled {
		conflict(w, "Optimisation source-bearing history is disabled in Settings.")
		return
	}
	q, e := url.ParseQuery(r.URL.RawQuery)
	id := chiURLParam(r, "id")
	if e != nil || len(q) != 2 || len(q["installation_id"]) != 1 || len(q["repo"]) != 1 || q.Get("installation_id") == "" || len(q.Get("installation_id")) > 128 || q.Get("repo") == "" || len(q.Get("repo")) > 240 || !strings.HasPrefix(id, "opv_") || len(id) > 128 {
		badRequest(w, "Supply one preview ID, installation_id and repo.")
		return
	}
	saved, err := s.ctrl.ReplayOptimisationPreview(r.Context(), q.Get("installation_id"), q.Get("repo"), id)
	if err != nil {
		s.optimisationError(w, r, err)
		return
	}
	fresh, accessErr := s.resolveIdentity(r)
	if accessErr != nil || fresh == nil || !fresh.Can(auth.ActionOptimisationsRead) || !s.cfg().Assistant.OptimisationHistoryEnabled {
		forbidden(w, "Optimisation history access was withdrawn.")
		return
	}
	writeJSON(w, 200, optimisationWebView(saved))
}
func (s *Server) optimisationError(w http.ResponseWriter, r *http.Request, err error) {
	var limited *controller.PRDelayAdmissionError
	switch {
	case errors.As(err, &limited):
		rateLimited(w, "Optimisation diagnostic reads are temporarily limited.", limited.RetryAfter.Truncate(time.Second)+time.Second)
	case errors.Is(err, github.ErrRateLimited):
		rateLimited(w, "The installation is holding GitHub reads. Retry manually later.", time.Minute)
	case errors.Is(err, errOptimisationAccess), errors.Is(err, github.ErrForbidden):
		forbidden(w, "Source access was refused. Check operator scopes, source/preview settings and App Contents/Actions permissions.")
	case errors.Is(err, store.ErrNotFound):
		notFound(w, "Preview or installation unavailable; retained history may have expired or been evicted.")
	case errors.Is(err, controller.ErrOptimisationPreview):
		conflict(w, "Preview unavailable. Check source/preview settings, unique literal names, successful push timing and the reviewed source digest/head.")
	case errors.Is(err, github.ErrExperimentAttemptChanged):
		conflict(w, "Checkout evidence changed during inspection. Inspect again manually.")
	case errors.Is(err, store.ErrInvalidOptimisationPreview), errors.Is(err, context.DeadlineExceeded):
		conflict(w, "Preview cannot be retained or verified within its bounds.")
	default:
		s.fail(w, r, "processing optimisation preview", err)
	}
}
