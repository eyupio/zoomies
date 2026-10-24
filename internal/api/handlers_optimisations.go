package api

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/eyupio/zoomies/internal/config"
	"github.com/eyupio/zoomies/internal/controller"
	"github.com/eyupio/zoomies/internal/github"
	"github.com/eyupio/zoomies/internal/optimisation"
	"github.com/eyupio/zoomies/internal/store"
)

// CheckoutSelectionBody names one exact run, attempt, GitHub job and the job and
// step positions in the workflow file. The two GitHub identities travel as
// decimal strings so a browser cannot round them.
type CheckoutSelectionBody struct {
	RunID       string `json:"run_id"`
	Attempt     int    `json:"attempt"`
	GitHubJobID string `json:"github_job_id"`
	JobIndex    *int   `json:"job_index,omitempty"`
	StepIndex   *int   `json:"step_index,omitempty"`
}

type CheckoutInspectionRequest struct {
	InstallationID string                `json:"installation_id"`
	Repository     string                `json:"repository"`
	Selection      CheckoutSelectionBody `json:"selection"`
}

// CheckoutPreviewRequest carries the operator's declaration. The scope of that
// declaration is fixed by the server; the request supplies only the head and
// digest of the source the operator was shown, so a file that moved since is
// refused rather than approved by accident.
type CheckoutPreviewRequest struct {
	InstallationID  string                `json:"installation_id"`
	Repository      string                `json:"repository"`
	Selection       CheckoutSelectionBody `json:"selection"`
	HeadSHA         string                `json:"head_sha"`
	SourceSHA256    string                `json:"source_sha256"`
	HistoryReviewed bool                  `json:"history_reviewed"`
}

type CheckoutInspectionView struct {
	InstallationID string                `json:"installation_id"`
	Repository     string                `json:"repository"`
	Selection      CheckoutSelectionBody `json:"selection"`
	HeadSHA        string                `json:"head_sha"`
	Path           string                `json:"path"`
	Mode           string                `json:"mode"`
	Content        string                `json:"content"`
	SourceSHA256   string                `json:"source_sha256"`
	StepNumber     int64                 `json:"step_number"`
	JobName        string                `json:"job_name"`
	StepName       string                `json:"step_name"`
	CheckoutMS     int64                 `json:"checkout_ms"`
	EvidenceID     string                `json:"evidence_id"`
	Basis          string                `json:"basis"`
	InspectedAt    time.Time             `json:"inspected_at"`
}

type CheckoutCollectionView struct {
	Version       string `json:"version"`
	Basis         string `json:"basis"`
	StepNumber    int64  `json:"step_number"`
	BlobSHA       string `json:"blob_sha"`
	SourceFunding string `json:"source_funding"`
}

// CheckoutApprovalRequest names the exact retained preview and the head and
// source digest the caller means to approve. The server checks them against the
// immutable preview rather than trusting the request as evidence.
type CheckoutApprovalRequest struct {
	InstallationID string `json:"installation_id"`
	Repository     string `json:"repository"`
	HeadSHA        string `json:"head_sha"`
	SourceSHA256   string `json:"source_sha256"`
}

type CheckoutApprovalView struct {
	ApprovedBy    string    `json:"approved_by"`
	ApprovedAt    time.Time `json:"approved_at"`
	HeadSHA       string    `json:"head_sha"`
	SourceSHA256  string    `json:"source_sha256"`
	PreviewSHA256 string    `json:"preview_sha256"`
}

type CheckoutPublicationRequest struct {
	InstallationID string `json:"installation_id"`
	Repository     string `json:"repository"`
	HeadSHA        string `json:"head_sha"`
	SourceSHA256   string `json:"source_sha256"`
}

type CheckoutPublicationView struct {
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

type CheckoutVerificationRequest struct {
	InstallationID string `json:"installation_id"`
	Repository     string `json:"repository"`
	ComparisonID   string `json:"comparison_id"`
}

type CheckoutVerificationView struct {
	ComparisonID string    `json:"comparison_id"`
	LinkedBy     string    `json:"linked_by"`
	LinkedAt     time.Time `json:"linked_at"`
}

type CheckoutPreviewDetail struct {
	Version      string `json:"version"`
	Kind         string `json:"kind"`
	State        string `json:"state"`
	HeadSHA      string `json:"head_sha"`
	SourceSHA256 string `json:"source_sha256"`
	AfterSHA256  string `json:"after_sha256"`
	EvidenceID   string `json:"evidence_id"`
	RunID        string `json:"run_id"`
	Attempt      int    `json:"attempt"`
	JobIndex     int    `json:"job_index"`
	StepIndex    int    `json:"step_index"`
	Line         int    `json:"line"`
	CheckoutMS   int64  `json:"checkout_ms"`
	Path         string `json:"path"`
	Before       string `json:"before"`
	After        string `json:"after"`
	Verification string `json:"verification"`
	Explanation  string `json:"explanation"`
}

// CheckoutPreviewView leaves the retained file out on purpose: the workflow
// source is shown once, at inspection, and the retained record answers with the
// one line it would change.
type CheckoutPreviewView struct {
	ID             string                    `json:"id"`
	InstallationID string                    `json:"installation_id"`
	Repository     string                    `json:"repository"`
	ReviewedBy     string                    `json:"reviewed_by"`
	SHA256         string                    `json:"sha256"`
	CreatedAt      time.Time                 `json:"created_at"`
	Currentness    string                    `json:"currentness"`
	Collection     *CheckoutCollectionView   `json:"collection"`
	Approval       *CheckoutApprovalView     `json:"approval"`
	Publication    *CheckoutPublicationView  `json:"publication"`
	Verification   *CheckoutVerificationView `json:"verification"`
	Preview        CheckoutPreviewDetail     `json:"preview"`
}

// parse reads a selection. Both positions omitted means "find the checkout step
// in this job for me"; naming one without the other is a mistake, not a guess.
func (b CheckoutSelectionBody) parse() (github.CheckoutSelection, bool) {
	run, err1 := strconv.ParseInt(b.RunID, 10, 64)
	job, err2 := strconv.ParseInt(b.GitHubJobID, 10, 64)
	if (b.JobIndex == nil) != (b.StepIndex == nil) {
		return github.CheckoutSelection{}, false
	}
	sel := github.CheckoutSelection{RunID: run, Attempt: b.Attempt, GitHubJobID: job, Locate: b.JobIndex == nil}
	if b.JobIndex != nil {
		sel.JobIndex, sel.StepIndex = *b.JobIndex, *b.StepIndex
	}
	return sel, err1 == nil && err2 == nil && strconv.FormatInt(run, 10) == b.RunID && strconv.FormatInt(job, 10) == b.GitHubJobID && github.ValidCheckoutSelection(sel)
}

func selectionBody(s github.CheckoutSelection) CheckoutSelectionBody {
	return CheckoutSelectionBody{RunID: strconv.FormatInt(s.RunID, 10), Attempt: s.Attempt, GitHubJobID: strconv.FormatInt(s.GitHubJobID, 10), JobIndex: &s.JobIndex, StepIndex: &s.StepIndex}
}

func checkoutPreviewView(p *controller.RetainedOptimisationPreview) CheckoutPreviewView {
	v := CheckoutPreviewView{
		ID: p.ID, InstallationID: p.InstallationID, Repository: p.Repository, ReviewedBy: p.ReviewedBy, SHA256: p.SHA256,
		CreatedAt: p.CreatedAt, Currentness: p.Currentness,
		Preview: CheckoutPreviewDetail{
			Version: p.Preview.Version, Kind: p.Preview.Kind, State: p.Preview.State, HeadSHA: p.Preview.HeadSHA,
			SourceSHA256: p.Preview.SourceSHA256, AfterSHA256: p.Preview.AfterSHA256, EvidenceID: p.Preview.EvidenceID,
			RunID: p.Preview.RunID, Attempt: p.Preview.Attempt, JobIndex: p.Preview.JobIndex, StepIndex: p.Preview.StepIndex,
			Line: p.Preview.Line, CheckoutMS: p.Preview.CheckoutMS, Path: p.Preview.Edit.Path, Before: p.Preview.Edit.Before,
			After: p.Preview.Edit.After, Verification: p.Preview.Verification, Explanation: p.Preview.Explanation,
		},
	}
	if c := p.Collection; c != nil {
		v.Collection = &CheckoutCollectionView{Version: c.Version, Basis: c.Basis, StepNumber: c.StepNumber, BlobSHA: c.BlobSHA, SourceFunding: c.SourceFunding}
	}
	if a := p.Approval; a != nil {
		v.Approval = &CheckoutApprovalView{ApprovedBy: a.ApprovedBy, ApprovedAt: a.ApprovedAt, HeadSHA: a.HeadSHA, SourceSHA256: a.SourceSHA256, PreviewSHA256: a.PreviewSHA256}
	}
	if p := p.Publication; p != nil {
		v.Publication = &CheckoutPublicationView{PublishedBy: p.PublishedBy, PublishedAt: p.PublishedAt, Branch: p.Branch, BaseBranch: p.BaseBranch, BaseSHA: p.BaseSHA, HeadSHA: p.HeadSHA, SourceSHA256: p.SourceSHA256, PreviewSHA256: p.PreviewSHA256, PullNumber: p.PullNumber, PullURL: p.PullURL}
	}
	if x := p.Verification; x != nil {
		v.Verification = &CheckoutVerificationView{ComparisonID: x.ComparisonID, LinkedBy: x.LinkedBy, LinkedAt: x.LinkedAt}
	}
	return v
}

func decodeOptimisationBody(w http.ResponseWriter, r *http.Request, into any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, 8<<10)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(into); err != nil {
		badRequest(w, "Supply one request within 8 KiB using only documented fields.")
		return false
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		badRequest(w, "Supply one request.")
		return false
	}
	return true
}

func validScope(installation, repo string) bool {
	return strings.TrimSpace(installation) != "" && strings.TrimSpace(repo) != "" && len(installation) <= 128 && len(repo) <= 1024
}

func (s *Server) handleInspectCheckout(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	id := Identity(r.Context())
	if id == nil || id.ID == "" {
		unauthorized(w, "An identified caller is required to read workflow source.")
		return
	}
	var in CheckoutInspectionRequest
	if !decodeOptimisationBody(w, r, &in) {
		return
	}
	sel, ok := in.Selection.parse()
	if !validScope(in.InstallationID, in.Repository) || !ok {
		badRequest(w, "Supply an installation, a repository and a positive decimal run ID, attempt, GitHub job ID, job index and step index.")
		return
	}
	out, err := s.ctrl.InspectCheckoutEvidence(r.Context(), in.InstallationID, in.Repository, sel)
	if err != nil {
		s.optimisationError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, CheckoutInspectionView{
		InstallationID: in.InstallationID, Repository: out.Repository, Selection: selectionBody(out.Selection), HeadSHA: out.HeadSHA,
		Path: out.Path, Mode: out.Mode, Content: out.Content, SourceSHA256: out.SourceSHA256, StepNumber: out.StepNumber, JobName: out.JobName, StepName: out.StepName,
		CheckoutMS: out.CheckoutMS, EvidenceID: out.EvidenceID, Basis: out.Basis, InspectedAt: out.InspectedAt,
	})
}

func (s *Server) handleCaptureCheckoutPreview(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	id := Identity(r.Context())
	if id == nil || id.ID == "" {
		unauthorized(w, "An identified caller is required to retain a preview.")
		return
	}
	var in CheckoutPreviewRequest
	if !decodeOptimisationBody(w, r, &in) {
		return
	}
	sel, ok := in.Selection.parse()
	if !validScope(in.InstallationID, in.Repository) || !ok {
		badRequest(w, "Supply an installation, a repository and a positive decimal run ID, attempt, GitHub job ID, job index and step index.")
		return
	}
	if !in.HistoryReviewed {
		badRequest(w, "Confirm that you reviewed the source and that neither the repository's tooling nor its downstream consumers need full history.")
		return
	}
	actor := &store.AuditEvent{ActorID: id.ID, ActorName: id.Name, ActorKind: id.Kind, IP: id.IP}
	review := controller.OptimisationHistoryReview(in.HeadSHA, in.SourceSHA256)
	out, err := s.ctrl.CaptureOptimisationPreview(r.Context(), actor, in.InstallationID, in.Repository, sel, review, s.ctrl.OptimisationPolicy)
	if err != nil {
		s.optimisationError(w, r, err)
		return
	}
	writeCreated(w, r, out.ID, checkoutPreviewView(out))
}

func (s *Server) handleGetCheckoutPreview(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	q, parseErr := url.ParseQuery(r.URL.RawQuery)
	if parseErr != nil || len(q) != 2 || len(q["installation_id"]) != 1 || len(q["repo"]) != 1 || !validScope(q.Get("installation_id"), q.Get("repo")) {
		badRequest(w, "Supply exactly one installation_id and repo, without other query arguments.")
		return
	}
	id := chiURLParam(r, "id")
	if !validPreviewID(id) {
		badRequest(w, "Supply a preview ID.")
		return
	}
	out, err := s.ctrl.ReplayOptimisationPreview(r.Context(), q.Get("installation_id"), q.Get("repo"), id)
	if err != nil {
		s.optimisationError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, checkoutPreviewView(out))
}

// handleApproveCheckoutPreview records an operator's explicit approval of one
// retained preview. It reads no source, so the two preview settings are not
// consulted: this is a decision about evidence already kept, not new access.
// Nothing is published, and the approval expires with the preview.
func (s *Server) handleApproveCheckoutPreview(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	id := Identity(r.Context())
	if id == nil || id.ID == "" {
		unauthorized(w, "An identified caller is required to approve a preview.")
		return
	}
	previewID := chiURLParam(r, "id")
	var in CheckoutApprovalRequest
	if !decodeOptimisationBody(w, r, &in) {
		return
	}
	if !validPreviewID(previewID) || !validScope(in.InstallationID, in.Repository) || !validDigest(in.HeadSHA, 40) || !validDigest(in.SourceSHA256, 64) {
		badRequest(w, "Supply a preview ID, an installation, a repository and the commit and source digest the preview names.")
		return
	}
	actor := &store.AuditEvent{ActorID: id.ID, ActorName: id.Name, ActorKind: id.Kind, IP: id.IP}
	out, err := s.ctrl.ApproveOptimisationPreview(r.Context(), actor, in.InstallationID, in.Repository, previewID, in.HeadSHA, in.SourceSHA256)
	if err != nil {
		s.optimisationError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, checkoutPreviewView(out))
}

// handlePublishCheckoutPreview opens one draft pull request for an approved
// preview. The server re-reads the source, policy and approval before writing;
// the request names only the preview binding, never GitHub objects.
func (s *Server) handlePublishCheckoutPreview(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	id := Identity(r.Context())
	if id == nil || id.ID == "" {
		unauthorized(w, "An identified caller is required to publish a preview.")
		return
	}
	previewID := chiURLParam(r, "id")
	var in CheckoutPublicationRequest
	if !decodeOptimisationBody(w, r, &in) {
		return
	}
	if !validPreviewID(previewID) || !validScope(in.InstallationID, in.Repository) || !validDigest(in.HeadSHA, 40) || !validDigest(in.SourceSHA256, 64) {
		badRequest(w, "Supply a preview ID, an installation, a repository and the commit and source digest the preview names.")
		return
	}
	actor := &store.AuditEvent{ActorID: id.ID, ActorName: id.Name, ActorKind: id.Kind, IP: id.IP}
	out, err := s.ctrl.PublishOptimisationPreview(r.Context(), actor, in.InstallationID, in.Repository, previewID, in.HeadSHA, in.SourceSHA256, s.ctrl.OptimisationPolicy)
	if err != nil {
		s.optimisationError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, checkoutPreviewView(out))
}

// handleLinkCheckoutVerification attaches one retained comparison to a
// published preview. The controller checks that the comparison is a workflow
// comparison naming this preview; the link is evidence to read, not a saving.
func (s *Server) handleLinkCheckoutVerification(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	id := Identity(r.Context())
	if id == nil || id.ID == "" {
		unauthorized(w, "An identified caller is required to link a comparison.")
		return
	}
	previewID := chiURLParam(r, "id")
	var in CheckoutVerificationRequest
	if !decodeOptimisationBody(w, r, &in) {
		return
	}
	if !validPreviewID(previewID) || !validScope(in.InstallationID, in.Repository) || !validComparisonID(in.ComparisonID) {
		badRequest(w, "Supply a preview ID, an installation, a repository and a comparison ID starting cmp_.")
		return
	}
	actor := &store.AuditEvent{ActorID: id.ID, ActorName: id.Name, ActorKind: id.Kind, IP: id.IP}
	out, err := s.ctrl.LinkOptimisationVerification(r.Context(), actor, in.InstallationID, in.Repository, previewID, in.ComparisonID)
	if err != nil {
		s.optimisationError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, checkoutPreviewView(out))
}

func validComparisonID(id string) bool {
	return strings.HasPrefix(id, store.PrefixComparison+"_") && len(id) <= 128 && !strings.ContainsAny(id, " /\r\n\x00")
}

func validPreviewID(id string) bool {
	return strings.HasPrefix(id, store.PrefixOptimisationPreview+"_") && len(id) <= 128 && !strings.ContainsAny(id, " /\r\n\x00")
}

func validDigest(s string, n int) bool {
	if len(s) != n || s != strings.ToLower(s) {
		return false
	}
	_, err := hex.DecodeString(s)
	return err == nil
}

func (s *Server) optimisationError(w http.ResponseWriter, r *http.Request, err error) {
	var admission *controller.PRDelayAdmissionError
	var shardingRefusal *optimisation.ShardingRefusal
	switch {
	case errors.Is(err, controller.ErrOptimisationPolicy):
		forbidden(w, "Reviewed workflow optimisations are off. An administrator turns on optimisation.source_reads and optimisation.workflow_edits in Settings.")
	case errors.Is(err, controller.ErrOptimisationPublishPermission):
		forbidden(w, "The installation cannot open a draft pull request. "+github.OptimisationPublishPermissionHint)
	case errors.Is(err, controller.ErrOptimisationPublishPolicy):
		forbidden(w, "Publishing an optimisation draft is off. An administrator turns on optimisation.source_reads, optimisation.workflow_edits and optimisation.publish in Settings.")
	case errors.Is(err, controller.ErrOptimisationNotApproved):
		conflict(w, "Approve this preview before publishing it.")
	case errors.Is(err, controller.ErrOptimisationApproval):
		conflict(w, "The commit and source digest do not match this retained preview. Open it again and approve exactly what it shows.")
	case errors.Is(err, controller.ErrOptimisationReadinessApproval):
		conflict(w, "The commit and source digest do not match this retained readiness preview. Open it again and approve exactly what it shows.")
	case errors.Is(err, controller.ErrOptimisationCacheApproval):
		conflict(w, "The commit and source digest do not match this retained cache preview. Open it again and approve exactly what it shows.")
	case errors.Is(err, github.ErrSetupCacheNotFound):
		conflict(w, "No supported root lockfile was found at the selected commit. Cache previews need package-lock.json, npm-shrinkwrap.json, yarn.lock or pnpm-lock.yaml at the repository root.")
	case errors.Is(err, github.ErrSetupCacheAmbiguous):
		conflict(w, "More than one supported root lockfile was found at the selected commit. Remove or rename the extra lockfile before previewing a cache change.")
	case errors.Is(err, github.ErrSetupCacheChanged):
		conflict(w, "The attempt changed while its source was read. Try the preview again.")
	case errors.Is(err, controller.ErrOptimisationCache):
		conflict(w, "This cache preview cannot be computed from the selected job. It needs a successful push job the fleet ran, one pinned actions/setup-node step with literal job and step names, one root lockfile, and at least two matching setup timings from the last ninety days.")
	case errors.As(err, &shardingRefusal):
		conflict(w, "This job cannot be sharded: "+shardingRefusal.Reason+".")
	case errors.Is(err, optimisation.ErrShardingUnsupported), errors.Is(err, controller.ErrOptimisationSharding):
		conflict(w, "This sharding preview cannot be computed from the selected job. It needs a successful push job the fleet ran, one Playwright test step on an Ubuntu runner, at least ten matching successful runs from the last ninety days, and a pool that could start at least two runners at once.")
	case errors.Is(err, controller.ErrOptimisationReadiness):
		conflict(w, "This readiness preview cannot be computed from the selected job. It needs a successful push job the fleet ran, one literal sleep step on an Ubuntu runner, an operator-supplied readiness command, and at least two matching wait timings from the last ninety days.")
	case errors.Is(err, controller.ErrOptimisationPublish):
		conflict(w, err.Error())
	case errors.Is(err, controller.ErrOptimisationVerification):
		conflict(w, err.Error())
	case errors.Is(err, store.ErrConflict):
		conflict(w, "This preview already carries a different approval or no longer matches its retained evidence. Open it again before approving.")
	case errors.As(err, &admission):
		rateLimited(w, err.Error(), admission.RetryAfter.Truncate(time.Second)+time.Second)
	case errors.Is(err, github.ErrRateLimited):
		rateLimited(w, "The GitHub installation is holding diagnostic reads. Try again later.", time.Minute)
	case errors.Is(err, github.ErrForbidden):
		forbidden(w, "The installation cannot read this repository's contents or Actions metadata; check the App's Contents and Actions read permissions.")
	case errors.Is(err, store.ErrNotFound):
		notFound(w, "The preview or installation is unavailable; a preview is kept for fourteen days and the oldest are evicted first.")
	case errors.Is(err, github.ErrCheckoutNotFound):
		conflict(w, "That job has no checkout step that can be previewed. It needs a commit-pinned actions/checkout with a literal fetch-depth: 0, in a job with a literal name, and the same line must not appear twice in the file.")
	case errors.Is(err, github.ErrCheckoutAmbiguous):
		conflict(w, "That job has more than one checkout step that could be previewed, and Zoomies will not guess which. Choose a job with a single full-history checkout.")
	case errors.Is(err, controller.ErrOptimisationPreview):
		conflict(w, "This step cannot be previewed. It needs a successful run on a push commit, a root workflow file with literal job and step names, a commit-pinned actions/checkout with fetch-depth: 0, and a source that has not changed since you reviewed it.")
	case errors.Is(err, context.DeadlineExceeded):
		conflict(w, "Reading the workflow exceeded its time limit. Try again later.")
	default:
		s.fail(w, r, "processing a checkout preview", err)
	}
}

// OptimisationEnablement is whether this instance lets Zoomies read workflow
// source and keep a proposed edit, and whether the caller can change that. The
// two settings are separate grants in the registry; the page asks for them
// together because one is no use without the other, and says so in words.
type OptimisationEnablement struct {
	SourceReads    bool   `json:"source_reads"`
	WorkflowEdits  bool   `json:"workflow_edits"`
	Publish        bool   `json:"publish"`
	Enabled        bool   `json:"enabled"`
	PublishEnabled bool   `json:"publish_enabled"`
	CanChange      bool   `json:"can_change"`
	CanPublish     bool   `json:"can_publish"`
	Reason         string `json:"reason,omitempty"`
	PublishReason  string `json:"publish_reason,omitempty"`
}

type OptimisationEnablementRequest struct {
	Enabled *bool `json:"enabled"`
	Publish *bool `json:"publish"`
}

const optimisationSourceKey, optimisationEditsKey, optimisationPublishKey = "optimisation.source_reads", "optimisation.workflow_edits", "optimisation.publish"

func (s *Server) optimisationEnablement(r *http.Request) OptimisationEnablement {
	cfg := s.cfg()
	out := OptimisationEnablement{SourceReads: cfg.Optimisation.SourceReads, WorkflowEdits: cfg.Optimisation.WorkflowEdits, Publish: cfg.Optimisation.Publish, CanChange: true, CanPublish: true}
	out.Enabled = out.SourceReads && out.WorkflowEdits
	out.PublishEnabled = out.Enabled && out.Publish
	role := callerRole(r)
	if !role.AtLeast(store.RoleAdmin) {
		out.CanChange, out.CanPublish = false, false
		out.Reason = "Only an administrator can turn this on. Ask one to press the button on this page, or to switch on optimisation.source_reads and optimisation.workflow_edits in Settings."
		out.PublishReason = "Only an administrator can turn this on. Ask one to press the button on this page, or to switch on optimisation.source_reads, optimisation.workflow_edits and optimisation.publish in Settings."
		return out
	}
	for _, key := range []string{optimisationSourceKey, optimisationEditsKey} {
		st, ok := config.LookupSetting(key)
		if !ok {
			continue
		}
		if editable, reason := s.editable(st, cfg, role); !editable {
			out.CanChange, out.Reason = false, reason
			break
		}
	}
	for _, key := range []string{optimisationSourceKey, optimisationEditsKey, optimisationPublishKey} {
		st, ok := config.LookupSetting(key)
		if !ok {
			continue
		}
		if editable, reason := s.editable(st, cfg, role); !editable {
			out.CanPublish, out.PublishReason = false, reason
			break
		}
	}
	return out
}

func (s *Server) handleGetOptimisationEnablement(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, s.optimisationEnablement(r))
}

// handleSetOptimisationEnablement is the settings write, narrowed to the one
// question this page asks. It plans and applies through the same path as
// PATCH /settings, so the environment lock, validation and audit row are the
// settings page's own, and a change is live for the very next request.
func (s *Server) handleSetOptimisationEnablement(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	var in OptimisationEnablementRequest
	if !decodeOptimisationBody(w, r, &in) {
		return
	}
	if in.Enabled == nil && in.Publish == nil {
		badRequest(w, "Say whether to turn workflow previews or publishing on or off with enabled or publish: true or false.")
		return
	}
	current := s.cfg()
	flat := map[string]any{}
	if in.Enabled != nil {
		flat[optimisationSourceKey], flat[optimisationEditsKey] = *in.Enabled, *in.Enabled
	}
	if in.Publish != nil {
		flat[optimisationPublishKey] = *in.Publish
		// Publication is no use without the two preview grants, so its press
		// turns them on too. Turning it off leaves them where they are.
		if *in.Publish {
			flat[optimisationSourceKey], flat[optimisationEditsKey] = true, true
		}
	}
	candidate, staged, fields := s.planSettings(current, flat, callerRole(r))
	if len(fields) > 0 {
		unprocessable(w, "these settings could not be changed", fields)
		return
	}
	if len(staged) > 0 {
		if fields := validatePlan(current, candidate, staged); len(fields) > 0 {
			unprocessable(w, "that would leave a controller that will not start", fields)
			return
		}
		if err := s.applyPlan(r.Context(), Identity(r.Context()), staged); err != nil {
			s.internal(w, r, "storing the optimisation settings", err)
			return
		}
	}
	writeJSON(w, http.StatusOK, s.optimisationEnablement(r))
}
