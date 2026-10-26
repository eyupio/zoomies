package api

import (
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/eyupio/zoomies/internal/controller"
	"github.com/eyupio/zoomies/internal/store"
)

// KennelFixInspectionRequest names the finding a timeout fix is for. The blob
// and job index are the ones the finding's file evidence carries; the minutes
// are the operator's, never inferred.
type KennelFixInspectionRequest struct {
	KennelRepositoryID string `json:"kennel_repository_id"`
	BlobSHA            string `json:"blob_sha"`
	JobIndex           int    `json:"job_index"`
	Minutes            int    `json:"minutes"`
}

// KennelFixCaptureRequest is the inspection's request with the commit and
// source digest the operator reviewed.
type KennelFixCaptureRequest struct {
	KennelRepositoryID string `json:"kennel_repository_id"`
	BlobSHA            string `json:"blob_sha"`
	JobIndex           int    `json:"job_index"`
	Minutes            int    `json:"minutes"`
	HeadSHA            string `json:"head_sha"`
	SourceSHA256       string `json:"source_sha256"`
}

type KennelFixBindingRequest struct {
	InstallationID string `json:"installation_id"`
	Repository     string `json:"repository"`
	HeadSHA        string `json:"head_sha"`
	SourceSHA256   string `json:"source_sha256"`
}

type KennelFixInspectionView struct {
	KennelRepositoryID string    `json:"kennel_repository_id"`
	InstallationID     string    `json:"installation_id"`
	Repository         string    `json:"repository"`
	CheckCode          string    `json:"check_code"`
	BlobSHA            string    `json:"blob_sha"`
	JobIndex           int       `json:"job_index"`
	JobName            string    `json:"job_name"`
	Minutes            int       `json:"minutes"`
	HeadSHA            string    `json:"head_sha"`
	Branch             string    `json:"branch"`
	SourceSHA256       string    `json:"source_sha256"`
	WorkflowPath       string    `json:"workflow_path"`
	Line               int       `json:"line"`
	Before             string    `json:"before"`
	After              string    `json:"after"`
	Verification       string    `json:"verification"`
	Explanation        string    `json:"explanation"`
	InspectedAt        time.Time `json:"inspected_at"`
}

type KennelFixApprovalView struct {
	ApprovedBy    string    `json:"approved_by"`
	ApprovedAt    time.Time `json:"approved_at"`
	HeadSHA       string    `json:"head_sha"`
	SourceSHA256  string    `json:"source_sha256"`
	PreviewSHA256 string    `json:"preview_sha256"`
}

type KennelFixPublicationView struct {
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

type KennelFixPreviewView struct {
	ID                 string                    `json:"id"`
	InstallationID     string                    `json:"installation_id"`
	Repository         string                    `json:"repository"`
	KennelRepositoryID string                    `json:"kennel_repository_id"`
	ReviewedBy         string                    `json:"reviewed_by"`
	SHA256             string                    `json:"sha256"`
	CreatedAt          time.Time                 `json:"created_at"`
	Currentness        string                    `json:"currentness"`
	CheckCode          string                    `json:"check_code"`
	BlobSHA            string                    `json:"blob_sha"`
	JobIndex           int                       `json:"job_index"`
	JobName            string                    `json:"job_name"`
	Minutes            int                       `json:"minutes"`
	HeadSHA            string                    `json:"head_sha"`
	Branch             string                    `json:"branch"`
	SourceSHA256       string                    `json:"source_sha256"`
	WorkflowPath       string                    `json:"workflow_path"`
	Line               int                       `json:"line"`
	Before             string                    `json:"before"`
	After              string                    `json:"after"`
	Verification       string                    `json:"verification"`
	Explanation        string                    `json:"explanation"`
	Approval           *KennelFixApprovalView    `json:"approval"`
	Publication        *KennelFixPublicationView `json:"publication"`
}

func kennelFixInspectionView(in *controller.KennelFixInspection, req controller.KennelFixRequest) KennelFixInspectionView {
	p := in.Preview
	return KennelFixInspectionView{
		KennelRepositoryID: req.KennelRepositoryID, InstallationID: in.InstallationID, Repository: in.Repository, CheckCode: p.CheckCode,
		BlobSHA: p.BlobSHA, JobIndex: p.JobIndex, JobName: p.JobName, Minutes: p.Minutes, HeadSHA: p.HeadSHA, Branch: in.Source.Branch,
		SourceSHA256: p.SourceSHA256, WorkflowPath: p.Edit.Path, Line: p.Line, Before: p.Edit.Before, After: p.Edit.After,
		Verification: p.Verification, Explanation: p.Explanation, InspectedAt: time.Now().UTC(),
	}
}

func kennelFixPreviewView(p *controller.RetainedKennelFix) KennelFixPreviewView {
	d := p.Preview
	v := KennelFixPreviewView{
		ID: p.ID, InstallationID: p.InstallationID, Repository: p.Repository, KennelRepositoryID: p.KennelRepositoryID,
		ReviewedBy: p.ReviewedBy, SHA256: p.SHA256, CreatedAt: p.CreatedAt, Currentness: p.Currentness, CheckCode: d.CheckCode,
		BlobSHA: d.BlobSHA, JobIndex: d.JobIndex, JobName: d.JobName, Minutes: d.Minutes, HeadSHA: d.HeadSHA, Branch: p.Branch,
		SourceSHA256: d.SourceSHA256, WorkflowPath: d.Edit.Path, Line: d.Line, Before: d.Edit.Before, After: d.Edit.After,
		Verification: d.Verification, Explanation: d.Explanation,
	}
	if a := p.Approval; a != nil {
		v.Approval = &KennelFixApprovalView{ApprovedBy: a.ApprovedBy, ApprovedAt: a.ApprovedAt, HeadSHA: a.HeadSHA, SourceSHA256: a.SourceSHA256, PreviewSHA256: a.PreviewSHA256}
	}
	if x := p.Publication; x != nil {
		v.Publication = &KennelFixPublicationView{PublishedBy: x.PublishedBy, PublishedAt: x.PublishedAt, Branch: x.Branch, BaseBranch: x.BaseBranch, BaseSHA: x.BaseSHA, HeadSHA: x.HeadSHA, SourceSHA256: x.SourceSHA256, PreviewSHA256: x.PreviewSHA256, PullNumber: x.PullNumber, PullURL: x.PullURL}
	}
	return v
}

func validKennelFixPreviewID(id string) bool {
	return strings.HasPrefix(id, store.PrefixKennelFixPreview+"_") && len(id) <= 128 && !strings.ContainsAny(id, " /\r\n\x00")
}

func validKennelFixFinding(repoID, blob string, job, minutes int) bool {
	return strings.HasPrefix(repoID, "kcr_") && len(repoID) <= 128 && !strings.ContainsAny(repoID, " /\r\n\x00") && validDigest(blob, 40) && job >= 0 && job < 10000 && minutes >= 1 && minutes <= 1440
}

func (s *Server) handleInspectKennelFix(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	id := Identity(r.Context())
	if id == nil || id.ID == "" {
		unauthorized(w, "An identified caller is required to inspect a Kennel fix.")
		return
	}
	var in KennelFixInspectionRequest
	if !decodeOptimisationBody(w, r, &in) {
		return
	}
	if !validKennelFixFinding(in.KennelRepositoryID, in.BlobSHA, in.JobIndex, in.Minutes) {
		badRequest(w, "Supply a Kennel repository, the workflow file and job the finding names, and a timeout of 1 to 1440 minutes.")
		return
	}
	req := controller.KennelFixRequest{KennelRepositoryID: in.KennelRepositoryID, BlobSHA: in.BlobSHA, JobIndex: in.JobIndex, Minutes: in.Minutes}
	out, err := s.ctrl.InspectKennelFix(r.Context(), req)
	if err != nil {
		s.optimisationError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, kennelFixInspectionView(out, req))
}

func (s *Server) handleCaptureKennelFix(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	id := Identity(r.Context())
	if id == nil || id.ID == "" {
		unauthorized(w, "An identified caller is required to keep a Kennel fix.")
		return
	}
	var in KennelFixCaptureRequest
	if !decodeOptimisationBody(w, r, &in) {
		return
	}
	if !validKennelFixFinding(in.KennelRepositoryID, in.BlobSHA, in.JobIndex, in.Minutes) || !validDigest(in.HeadSHA, 40) || !validDigest(in.SourceSHA256, 64) {
		badRequest(w, "Supply the finding, the minutes, and the commit and source digest you reviewed.")
		return
	}
	actor := &store.AuditEvent{ActorID: id.ID, ActorName: id.Name, ActorKind: id.Kind, IP: id.IP}
	req := controller.KennelFixRequest{KennelRepositoryID: in.KennelRepositoryID, BlobSHA: in.BlobSHA, JobIndex: in.JobIndex, Minutes: in.Minutes}
	out, err := s.ctrl.CaptureKennelFix(r.Context(), actor, req, in.HeadSHA, in.SourceSHA256)
	if err != nil {
		s.optimisationError(w, r, err)
		return
	}
	writeCreated(w, r, out.ID, kennelFixPreviewView(out))
}

func (s *Server) handleGetKennelFix(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	q, parseErr := url.ParseQuery(r.URL.RawQuery)
	if parseErr != nil || len(q) != 2 || len(q["installation_id"]) != 1 || len(q["repo"]) != 1 || !validScope(q.Get("installation_id"), q.Get("repo")) {
		badRequest(w, "Supply exactly one installation_id and repo, without other query arguments.")
		return
	}
	id := chiURLParam(r, "id")
	if !validKennelFixPreviewID(id) {
		badRequest(w, "Supply a Kennel fix preview ID.")
		return
	}
	out, err := s.ctrl.ReplayKennelFix(r.Context(), q.Get("installation_id"), q.Get("repo"), id)
	if err != nil {
		s.optimisationError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, kennelFixPreviewView(out))
}

// handleApproveKennelFix records an operator's explicit approval of one retained
// Kennel fix. It reads no source, so the settings are not consulted: this is a
// decision about evidence already kept. It publishes nothing and expires with
// the preview.
func (s *Server) handleApproveKennelFix(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	id := Identity(r.Context())
	if id == nil || id.ID == "" {
		unauthorized(w, "An identified caller is required to approve a Kennel fix.")
		return
	}
	previewID := chiURLParam(r, "id")
	var in KennelFixBindingRequest
	if !decodeOptimisationBody(w, r, &in) {
		return
	}
	if !validKennelFixPreviewID(previewID) || !validScope(in.InstallationID, in.Repository) || !validDigest(in.HeadSHA, 40) || !validDigest(in.SourceSHA256, 64) {
		badRequest(w, "Supply a Kennel fix preview ID, an installation, a repository and the commit and source digest the preview names.")
		return
	}
	actor := &store.AuditEvent{ActorID: id.ID, ActorName: id.Name, ActorKind: id.Kind, IP: id.IP}
	out, err := s.ctrl.ApproveKennelFix(r.Context(), actor, in.InstallationID, in.Repository, previewID, in.HeadSHA, in.SourceSHA256)
	if err != nil {
		s.optimisationError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, kennelFixPreviewView(out))
}

// handlePublishKennelFix opens one draft pull request for an approved fix. The
// server re-reads the source, policy and approval before writing; the request
// names only the preview binding, never GitHub objects.
func (s *Server) handlePublishKennelFix(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	id := Identity(r.Context())
	if id == nil || id.ID == "" {
		unauthorized(w, "An identified caller is required to publish a Kennel fix.")
		return
	}
	previewID := chiURLParam(r, "id")
	var in KennelFixBindingRequest
	if !decodeOptimisationBody(w, r, &in) {
		return
	}
	if !validKennelFixPreviewID(previewID) || !validScope(in.InstallationID, in.Repository) || !validDigest(in.HeadSHA, 40) || !validDigest(in.SourceSHA256, 64) {
		badRequest(w, "Supply a Kennel fix preview ID, an installation, a repository and the commit and source digest the preview names.")
		return
	}
	actor := &store.AuditEvent{ActorID: id.ID, ActorName: id.Name, ActorKind: id.Kind, IP: id.IP}
	out, err := s.ctrl.PublishKennelFix(r.Context(), actor, in.InstallationID, in.Repository, previewID, in.HeadSHA, in.SourceSHA256, s.ctrl.OptimisationPolicy)
	if err != nil {
		s.optimisationError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, kennelFixPreviewView(out))
}
