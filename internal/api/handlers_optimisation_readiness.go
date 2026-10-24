package api

import (
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/eyupio/zoomies/internal/controller"
	"github.com/eyupio/zoomies/internal/optimisation"
	"github.com/eyupio/zoomies/internal/store"
)

type ReadinessInspectionRequest struct {
	InstallationID string             `json:"installation_id"`
	Repository     string             `json:"repository"`
	Selection      CacheSelectionBody `json:"selection"`
	Condition      string             `json:"condition"`
}

type ReadinessInspectionView struct {
	InstallationID string             `json:"installation_id"`
	Repository     string             `json:"repository"`
	Selection      CacheSelectionBody `json:"selection"`
	Condition      string             `json:"condition"`
	HeadSHA        string             `json:"head_sha"`
	SourceSHA256   string             `json:"source_sha256"`
	WorkflowPath   string             `json:"workflow_path"`
	JobName        string             `json:"job_name"`
	StepName       string             `json:"step_name"`
	Line           int                `json:"line"`
	WaitSeconds    int                `json:"wait_seconds"`
	Runs           int                `json:"runs"`
	WaitMS         int64              `json:"wait_ms"`
	Before         string             `json:"before"`
	After          string             `json:"after"`
	Verification   string             `json:"verification"`
	Explanation    string             `json:"explanation"`
	InspectedAt    time.Time          `json:"inspected_at"`
}

func readinessInspectionView(p *optimisation.ReadinessPreview, installation, repo string, selection CacheSelectionBody) ReadinessInspectionView {
	return ReadinessInspectionView{
		InstallationID: installation, Repository: repo, Selection: selection, Condition: p.Condition,
		HeadSHA: p.HeadSHA, SourceSHA256: p.SourceSHA256, WorkflowPath: p.Edit.Path, JobName: p.JobName, StepName: p.StepName, Line: p.Line,
		WaitSeconds: p.WaitSeconds, Runs: p.Runs, WaitMS: p.WaitMS,
		Before: p.Edit.Before, After: p.Edit.After, Verification: p.Verification, Explanation: p.Explanation,
		InspectedAt: time.Now().UTC(),
	}
}

func (s *Server) handleInspectReadiness(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	id := Identity(r.Context())
	if id == nil || id.ID == "" {
		unauthorized(w, "An identified caller is required to inspect a readiness preview.")
		return
	}
	var in ReadinessInspectionRequest
	if !decodeOptimisationBody(w, r, &in) {
		return
	}
	selection, githubJobID, ok := in.Selection.parse()
	if !ok || !validScope(in.InstallationID, in.Repository) || strings.TrimSpace(in.Condition) == "" || len(in.Condition) > 1024 || strings.ContainsAny(in.Condition, "\r\n\x00") {
		badRequest(w, "Supply an installation, a repository, a completed push attempt and the readiness command to poll.")
		return
	}
	out, err := s.ctrl.InspectReadinessPreview(r.Context(), in.InstallationID, in.Repository, selection, githubJobID, in.Condition)
	if err != nil {
		s.optimisationError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, readinessInspectionView(out, in.InstallationID, in.Repository, in.Selection))
}

type ReadinessCaptureRequest struct {
	InstallationID string             `json:"installation_id"`
	Repository     string             `json:"repository"`
	Selection      CacheSelectionBody `json:"selection"`
	Condition      string             `json:"condition"`
	HeadSHA        string             `json:"head_sha"`
	SourceSHA256   string             `json:"source_sha256"`
}

type ReadinessApprovalRequest struct {
	InstallationID string `json:"installation_id"`
	Repository     string `json:"repository"`
	HeadSHA        string `json:"head_sha"`
	SourceSHA256   string `json:"source_sha256"`
}

type ReadinessApprovalView struct {
	ApprovedBy    string    `json:"approved_by"`
	ApprovedAt    time.Time `json:"approved_at"`
	HeadSHA       string    `json:"head_sha"`
	SourceSHA256  string    `json:"source_sha256"`
	PreviewSHA256 string    `json:"preview_sha256"`
}

type ReadinessPublicationRequest struct {
	InstallationID string `json:"installation_id"`
	Repository     string `json:"repository"`
	HeadSHA        string `json:"head_sha"`
	SourceSHA256   string `json:"source_sha256"`
}

type ReadinessPublicationView struct {
	PublishedBy   string    `json:"published_by"`
	PublishedAt   time.Time `json:"published_at"`
	Branch        string    `json:"branch"`
	BaseBranch    string    `json:"base_branch"`
	BaseSHA       string    `json:"base_sha"`
	HeadSHA       string    `json:"head_sha"`
	SourceSHA256  string    `json:"source_sha256"`
	PreviewSHA256 string    `json:"preview_sha256"`
	PullNumber    int       `json:"pull_number"`
	PullURL       string    `json:"pull_url"`
}

type ReadinessPreviewView struct {
	ID             string                    `json:"id"`
	InstallationID string                    `json:"installation_id"`
	Repository     string                    `json:"repository"`
	ReviewedBy     string                    `json:"reviewed_by"`
	SHA256         string                    `json:"sha256"`
	CreatedAt      time.Time                 `json:"created_at"`
	Currentness    string                    `json:"currentness"`
	Selection      CacheSelectionBody        `json:"selection"`
	Condition      string                    `json:"condition"`
	HeadSHA        string                    `json:"head_sha"`
	SourceSHA256   string                    `json:"source_sha256"`
	WorkflowPath   string                    `json:"workflow_path"`
	JobName        string                    `json:"job_name"`
	StepName       string                    `json:"step_name"`
	Line           int                       `json:"line"`
	WaitSeconds    int                       `json:"wait_seconds"`
	Runs           int                       `json:"runs"`
	WaitMS         int64                     `json:"wait_ms"`
	Before         string                    `json:"before"`
	After          string                    `json:"after"`
	Verification   string                    `json:"verification"`
	Explanation    string                    `json:"explanation"`
	Approval       *ReadinessApprovalView    `json:"approval"`
	Publication    *ReadinessPublicationView `json:"publication"`
}

func readinessPreviewView(p *controller.RetainedReadinessPreview) ReadinessPreviewView {
	d := p.Preview
	selection := CacheSelectionBody{}
	if p.Provenance != nil {
		selection = CacheSelectionBody{RunID: strconv.FormatInt(p.Provenance.Selection.RunID, 10), Attempt: p.Provenance.Selection.Attempt, GitHubJobID: strconv.FormatInt(p.Provenance.GitHubJobID, 10)}
	}
	v := ReadinessPreviewView{
		ID: p.ID, InstallationID: p.InstallationID, Repository: p.Repository, ReviewedBy: p.ReviewedBy, SHA256: p.SHA256,
		CreatedAt: p.CreatedAt, Currentness: p.Currentness, Selection: selection, Condition: d.Condition,
		HeadSHA: d.HeadSHA, SourceSHA256: d.SourceSHA256, WorkflowPath: d.Edit.Path, JobName: d.JobName, StepName: d.StepName, Line: d.Line,
		WaitSeconds: d.WaitSeconds, Runs: d.Runs, WaitMS: d.WaitMS, Before: d.Edit.Before, After: d.Edit.After,
		Verification: d.Verification, Explanation: d.Explanation,
	}
	if a := p.Approval; a != nil {
		v.Approval = &ReadinessApprovalView{ApprovedBy: a.ApprovedBy, ApprovedAt: a.ApprovedAt, HeadSHA: a.HeadSHA, SourceSHA256: a.SourceSHA256, PreviewSHA256: a.PreviewSHA256}
	}
	if x := p.Publication; x != nil {
		v.Publication = &ReadinessPublicationView{PublishedBy: x.PublishedBy, PublishedAt: x.PublishedAt, Branch: x.Branch, BaseBranch: x.BaseBranch, BaseSHA: x.BaseSHA, HeadSHA: x.HeadSHA, SourceSHA256: x.SourceSHA256, PreviewSHA256: x.PreviewSHA256, PullNumber: x.PullNumber, PullURL: x.PullURL}
	}
	return v
}

func validReadinessPreviewID(id string) bool {
	return strings.HasPrefix(id, store.PrefixOptimisationReadinessPreview+"_") && len(id) <= 128 && !strings.ContainsAny(id, " /\r\n\x00")
}

func (s *Server) handleCaptureReadiness(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	id := Identity(r.Context())
	if id == nil || id.ID == "" {
		unauthorized(w, "An identified caller is required to keep a readiness preview.")
		return
	}
	var in ReadinessCaptureRequest
	if !decodeOptimisationBody(w, r, &in) {
		return
	}
	selection, githubJobID, ok := in.Selection.parse()
	if !ok || !validScope(in.InstallationID, in.Repository) || strings.TrimSpace(in.Condition) == "" || len(in.Condition) > 1024 || strings.ContainsAny(in.Condition, "\r\n\x00") || !validDigest(in.HeadSHA, 40) || !validDigest(in.SourceSHA256, 64) {
		badRequest(w, "Supply an installation, a repository, the selected run and job, the readiness command, and the commit and source digest you reviewed.")
		return
	}
	actor := &store.AuditEvent{ActorID: id.ID, ActorName: id.Name, ActorKind: id.Kind, IP: id.IP}
	out, err := s.ctrl.CaptureReadinessPreview(r.Context(), actor, in.InstallationID, in.Repository, selection, githubJobID, in.Condition, in.HeadSHA, in.SourceSHA256)
	if err != nil {
		s.optimisationError(w, r, err)
		return
	}
	writeCreated(w, r, out.ID, readinessPreviewView(out))
}

func (s *Server) handleGetReadiness(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	q, parseErr := url.ParseQuery(r.URL.RawQuery)
	if parseErr != nil || len(q) != 2 || len(q["installation_id"]) != 1 || len(q["repo"]) != 1 || !validScope(q.Get("installation_id"), q.Get("repo")) {
		badRequest(w, "Supply exactly one installation_id and repo, without other query arguments.")
		return
	}
	id := chiURLParam(r, "id")
	if !validReadinessPreviewID(id) {
		badRequest(w, "Supply a readiness preview ID.")
		return
	}
	out, err := s.ctrl.ReplayReadinessPreview(r.Context(), q.Get("installation_id"), q.Get("repo"), id)
	if err != nil {
		s.optimisationError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, readinessPreviewView(out))
}

// handleApproveReadiness records an operator's explicit approval of one
// retained readiness preview. It reads no source, so the preview settings are
// not consulted: this is a decision about evidence already kept, not new
// access. It publishes nothing and expires with the preview.
func (s *Server) handleApproveReadiness(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	id := Identity(r.Context())
	if id == nil || id.ID == "" {
		unauthorized(w, "An identified caller is required to approve a readiness preview.")
		return
	}
	previewID := chiURLParam(r, "id")
	var in ReadinessApprovalRequest
	if !decodeOptimisationBody(w, r, &in) {
		return
	}
	if !validReadinessPreviewID(previewID) || !validScope(in.InstallationID, in.Repository) || !validDigest(in.HeadSHA, 40) || !validDigest(in.SourceSHA256, 64) {
		badRequest(w, "Supply a readiness preview ID, an installation, a repository and the commit and source digest the preview names.")
		return
	}
	actor := &store.AuditEvent{ActorID: id.ID, ActorName: id.Name, ActorKind: id.Kind, IP: id.IP}
	out, err := s.ctrl.ApproveReadinessPreview(r.Context(), actor, in.InstallationID, in.Repository, previewID, in.HeadSHA, in.SourceSHA256)
	if err != nil {
		s.optimisationError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, readinessPreviewView(out))
}

// handlePublishReadiness opens one draft pull request for an approved readiness
// preview. The server re-reads the source, policy and approval before writing;
// the request names only the preview binding, never GitHub objects.
func (s *Server) handlePublishReadiness(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	id := Identity(r.Context())
	if id == nil || id.ID == "" {
		unauthorized(w, "An identified caller is required to publish a readiness preview.")
		return
	}
	previewID := chiURLParam(r, "id")
	var in ReadinessPublicationRequest
	if !decodeOptimisationBody(w, r, &in) {
		return
	}
	if !validReadinessPreviewID(previewID) || !validScope(in.InstallationID, in.Repository) || !validDigest(in.HeadSHA, 40) || !validDigest(in.SourceSHA256, 64) {
		badRequest(w, "Supply a readiness preview ID, an installation, a repository and the commit and source digest the preview names.")
		return
	}
	actor := &store.AuditEvent{ActorID: id.ID, ActorName: id.Name, ActorKind: id.Kind, IP: id.IP}
	out, err := s.ctrl.PublishReadinessPreview(r.Context(), actor, in.InstallationID, in.Repository, previewID, in.HeadSHA, in.SourceSHA256, s.ctrl.OptimisationPolicy)
	if err != nil {
		s.optimisationError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, readinessPreviewView(out))
}
