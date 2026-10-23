package api

import (
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/eyupio/zoomies/internal/controller"
	"github.com/eyupio/zoomies/internal/github"
	"github.com/eyupio/zoomies/internal/store"
)

type CacheSelectionBody struct {
	RunID       string `json:"run_id"`
	Attempt     int    `json:"attempt"`
	GitHubJobID string `json:"github_job_id"`
}

type CacheInspectionRequest struct {
	InstallationID string             `json:"installation_id"`
	Repository     string             `json:"repository"`
	Selection      CacheSelectionBody `json:"selection"`
}

type CacheCaptureRequest struct {
	InstallationID string             `json:"installation_id"`
	Repository     string             `json:"repository"`
	Selection      CacheSelectionBody `json:"selection"`
	HeadSHA        string             `json:"head_sha"`
	SourceSHA256   string             `json:"source_sha256"`
	LockfileSHA256 string             `json:"lockfile_sha256"`
}

type CacheInspectionView struct {
	InstallationID string             `json:"installation_id"`
	Repository     string             `json:"repository"`
	Selection      CacheSelectionBody `json:"selection"`
	HeadSHA        string             `json:"head_sha"`
	WorkflowPath   string             `json:"workflow_path"`
	LockfilePath   string             `json:"lockfile_path"`
	LockfileSHA256 string             `json:"lockfile_sha256"`
	SourceSHA256   string             `json:"source_sha256"`
	PackageManager string             `json:"package_manager"`
	JobName        string             `json:"job_name"`
	StepName       string             `json:"step_name"`
	Line           int                `json:"line"`
	Runs           int                `json:"runs"`
	SetupMS        int64              `json:"setup_ms"`
	Before         string             `json:"before"`
	After          string             `json:"after"`
	Verification   string             `json:"verification"`
	Explanation    string             `json:"explanation"`
	InspectedAt    time.Time          `json:"inspected_at"`
}

type CachePreviewView struct {
	ID             string    `json:"id"`
	InstallationID string    `json:"installation_id"`
	Repository     string    `json:"repository"`
	ReviewedBy     string    `json:"reviewed_by"`
	SHA256         string    `json:"sha256"`
	CreatedAt      time.Time `json:"created_at"`
	Currentness    string    `json:"currentness"`
	HeadSHA        string    `json:"head_sha"`
	WorkflowPath   string    `json:"workflow_path"`
	LockfilePath   string    `json:"lockfile_path"`
	LockfileSHA256 string    `json:"lockfile_sha256"`
	SourceSHA256   string    `json:"source_sha256"`
	AfterSHA256    string    `json:"after_sha256"`
	PackageManager string    `json:"package_manager"`
	JobName        string    `json:"job_name"`
	StepName       string    `json:"step_name"`
	Line           int       `json:"line"`
	Runs           int       `json:"runs"`
	SetupMS        int64     `json:"setup_ms"`
	Before         string    `json:"before"`
	After          string    `json:"after"`
	Verification   string    `json:"verification"`
	Explanation    string    `json:"explanation"`
}

func (b CacheSelectionBody) parse() (github.SetupCacheSelection, int64, bool) {
	run, err1 := strconv.ParseInt(b.RunID, 10, 64)
	job, err2 := strconv.ParseInt(b.GitHubJobID, 10, 64)
	sel := github.SetupCacheSelection{RunID: run, Attempt: b.Attempt}
	return sel, job, err1 == nil && err2 == nil && job > 0 && strconv.FormatInt(run, 10) == b.RunID && strconv.FormatInt(job, 10) == b.GitHubJobID && github.ValidSetupCacheSelection(sel)
}

func cacheInspectionView(i *controller.CacheInspection) CacheInspectionView {
	p := i.Preview
	return CacheInspectionView{
		HeadSHA: p.HeadSHA, WorkflowPath: p.Edit.Path, LockfilePath: p.LockfilePath, LockfileSHA256: p.LockfileSHA256,
		SourceSHA256: p.SourceSHA256, PackageManager: p.PackageManager, JobName: p.JobName, StepName: p.StepName,
		Line: p.Line, Runs: p.Runs, SetupMS: p.SetupMS, Before: p.Edit.Before, After: p.Edit.After,
		Verification: p.Verification, Explanation: p.Explanation, InspectedAt: time.Now().UTC(),
	}
}

func cachePreviewView(p *controller.RetainedCachePreview) CachePreviewView {
	d := p.Preview
	return CachePreviewView{
		ID: p.ID, InstallationID: p.InstallationID, Repository: p.Repository, ReviewedBy: p.ReviewedBy, SHA256: p.SHA256,
		CreatedAt: p.CreatedAt, Currentness: p.Currentness, HeadSHA: d.HeadSHA, WorkflowPath: d.Edit.Path,
		LockfilePath: d.LockfilePath, LockfileSHA256: d.LockfileSHA256, SourceSHA256: d.SourceSHA256, AfterSHA256: d.AfterSHA256,
		PackageManager: d.PackageManager, JobName: d.JobName, StepName: d.StepName, Line: d.Line, Runs: d.Runs,
		SetupMS: d.SetupMS, Before: d.Edit.Before, After: d.Edit.After, Verification: d.Verification, Explanation: d.Explanation,
	}
}

func (s *Server) handleInspectCache(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	id := Identity(r.Context())
	if id == nil || id.ID == "" {
		unauthorized(w, "An identified caller is required to inspect a cache preview.")
		return
	}
	var in CacheInspectionRequest
	if !decodeOptimisationBody(w, r, &in) {
		return
	}
	sel, job, ok := in.Selection.parse()
	if !validScope(in.InstallationID, in.Repository) || !ok {
		badRequest(w, "Supply an installation, a repository, a positive decimal run ID and GitHub job ID, and a positive attempt.")
		return
	}
	out, err := s.ctrl.InspectCachePreview(r.Context(), in.InstallationID, in.Repository, sel, job)
	if err != nil {
		s.optimisationError(w, r, err)
		return
	}
	view := cacheInspectionView(&controller.CacheInspection{Preview: *out})
	view.InstallationID, view.Repository, view.Selection = in.InstallationID, in.Repository, in.Selection
	writeJSON(w, http.StatusOK, view)
}

func (s *Server) handleCaptureCache(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	id := Identity(r.Context())
	if id == nil || id.ID == "" {
		unauthorized(w, "An identified caller is required to retain a cache preview.")
		return
	}
	var in CacheCaptureRequest
	if !decodeOptimisationBody(w, r, &in) {
		return
	}
	sel, job, ok := in.Selection.parse()
	if !validScope(in.InstallationID, in.Repository) || !ok || !validDigest(in.HeadSHA, 40) || !validDigest(in.SourceSHA256, 64) || !validDigest(in.LockfileSHA256, 64) {
		badRequest(w, "Supply an installation, a repository, a positive decimal run ID and GitHub job ID, a positive attempt, and the commit, source digest and lockfile digest that were reviewed.")
		return
	}
	actor := &store.AuditEvent{ActorID: id.ID, ActorName: id.Name, ActorKind: id.Kind, IP: id.IP}
	out, err := s.ctrl.CaptureCachePreview(r.Context(), actor, in.InstallationID, in.Repository, sel, job, in.HeadSHA, in.SourceSHA256, in.LockfileSHA256)
	if err != nil {
		s.optimisationError(w, r, err)
		return
	}
	writeCreated(w, r, out.ID, cachePreviewView(out))
}

func (s *Server) handleGetCachePreview(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	q, parseErr := url.ParseQuery(r.URL.RawQuery)
	if parseErr != nil || len(q) != 2 || len(q["installation_id"]) != 1 || len(q["repo"]) != 1 || !validScope(q.Get("installation_id"), q.Get("repo")) {
		badRequest(w, "Supply exactly one installation_id and repo, without other query arguments.")
		return
	}
	id := chiURLParam(r, "id")
	if !validCachePreviewID(id) {
		badRequest(w, "Supply a cache preview ID.")
		return
	}
	out, err := s.ctrl.ReplayCachePreview(r.Context(), q.Get("installation_id"), q.Get("repo"), id)
	if err != nil {
		s.optimisationError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, cachePreviewView(out))
}

func validCachePreviewID(id string) bool {
	return strings.HasPrefix(id, store.PrefixOptimisationCachePreview+"_") && len(id) <= 128 && !strings.ContainsAny(id, " /\r\n\x00")
}
