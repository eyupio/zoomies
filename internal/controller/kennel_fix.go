package controller

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"path"
	"strings"
	"time"

	"github.com/eyupio/zoomies/internal/github"
	"github.com/eyupio/zoomies/internal/kennel"
	"github.com/eyupio/zoomies/internal/optimisation"
	"github.com/eyupio/zoomies/internal/prrepair"
	"github.com/eyupio/zoomies/internal/store"
)

var (
	// ErrKennelFix is the one refusal for a fix that cannot be previewed. It
	// names what the fix needs, so the operator knows what to change.
	ErrKennelFix = errors.New("a fix for this finding cannot be previewed: it needs Kennel Club on, a tracked repository, an open ci.no_timeout finding that names this workflow file and job, the minutes, and optimisation source and edit policy")
	// ErrKennelFixWaived refuses a fix for a finding somebody has decided to
	// accept. The waiver stays visible; it is removed on the finding, not here.
	ErrKennelFixWaived = errors.New("this finding is waived, so no fix is offered; remove the waiver on the finding first")
	// ErrKennelFixApproval means the commit and digests named do not match the
	// retained preview.
	ErrKennelFixApproval = errors.New("the commit and source digest do not match this retained Kennel fix preview")
)

// KennelFixRequest names the finding a fix is for and the operator's value. The
// blob and job index are the ones the finding's evidence carries.
type KennelFixRequest struct {
	KennelRepositoryID string
	BlobSHA            string
	JobIndex           int
	Minutes            int
}

// KennelFixInspection is what an operator is shown before keeping a fix: the
// finding, the source identity and the computed preview. It carries the full
// workflow file in memory only; a transport renders the edit and metadata.
type KennelFixInspection struct {
	InstallationID string
	Repository     string
	Finding        kennel.Finding
	Source         github.KennelFixSource
	Preview        optimisation.TimeoutPreview
}

func validKennelFixRequest(r KennelFixRequest) bool {
	return strings.HasPrefix(r.KennelRepositoryID, "kcr_") && len(r.KennelRepositoryID) <= 128 && !strings.ContainsAny(r.KennelRepositoryID, " /\r\n\x00") &&
		knownHash(r.BlobSHA, 40) && r.BlobSHA == strings.ToLower(r.BlobSHA) && r.JobIndex >= 0 && r.JobIndex < 10000 && r.Minutes >= 1 && r.Minutes <= 1440
}

// kennelFixTarget finds the open finding a fix is for and the workflow path it
// is in. It reads only what Kennel Club already stored.
func (c *Controller) kennelFixTarget(ctx context.Context, req KennelFixRequest) (*store.KennelRepository, kennel.Finding, string, error) {
	var none kennel.Finding
	if !validKennelFixRequest(req) || !c.cfg().Kennel.Enabled {
		return nil, none, "", ErrKennelFix
	}
	row, err := c.st.GetKennelRepository(ctx, req.KennelRepositoryID)
	if err != nil {
		return nil, none, "", err
	}
	if row.Untracked != nil || row.EvaluatedAt == nil {
		return nil, none, "", ErrKennelFix
	}
	var ev kennel.Evaluation
	if json.Unmarshal(row.Evaluation, &ev) != nil {
		return nil, none, "", ErrKennelFix
	}
	names := func(f kennel.Finding) bool {
		if f.Code != kennel.CodeNoTimeout {
			return false
		}
		for _, e := range f.Evidence {
			if e.Kind == kennel.EvidenceFile && e.Ref == req.BlobSHA && e.JobIndex != nil && *e.JobIndex == req.JobIndex {
				return true
			}
		}
		return false
	}
	var found *kennel.Finding
	for i := range ev.Findings {
		if names(ev.Findings[i]) {
			found = &ev.Findings[i]
			break
		}
	}
	if found == nil {
		for _, w := range ev.Waived {
			if names(w.Finding) {
				return nil, none, "", ErrKennelFixWaived
			}
		}
		return nil, none, "", ErrKennelFix
	}
	// The path was gated when it was kept and is gated again: a fix reads the file
	// at a path, and only the root of the workflows folder is a path to read.
	wm := parseKennelWatermark(row.Watermark)
	for _, f := range wm.WorkflowFiles {
		if f.SHA != req.BlobSHA {
			continue
		}
		p := gatedPath(f.Path)
		if p == "" || path.Dir(p) != ".github/workflows" || !strings.HasSuffix(p, ".yml") && !strings.HasSuffix(p, ".yaml") {
			return nil, none, "", ErrKennelFix
		}
		return row, *found, p, nil
	}
	return nil, none, "", ErrKennelFix
}

// InspectKennelFix binds one open ci.no_timeout finding to the pure fix. The
// source read is the only GitHub work. It retains nothing and guesses no value.
func (c *Controller) InspectKennelFix(ctx context.Context, req KennelFixRequest) (*KennelFixInspection, error) {
	row, finding, workflowPath, err := c.kennelFixTarget(ctx, req)
	if err != nil {
		return nil, err
	}
	policy, err := c.OptimisationPolicy(ctx)
	if err != nil {
		return nil, err
	}
	if !policy.SourceAllowed || !policy.WorkflowEditsAllowed {
		return nil, ErrOptimisationPolicy
	}
	installation := row.InstallationID
	if c.githubHeld(installation, c.Now()) {
		return nil, github.ErrRateLimited
	}
	client, err := c.ClientFor(ctx, installation)
	if err != nil {
		return nil, err
	}
	reader, ok := client.(github.KennelFixSourceReader)
	if !ok {
		return nil, ErrKennelFix
	}
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	release, err := c.admitPRDelay(installation)
	if err != nil {
		return nil, err
	}
	defer release()
	quota, err := client.RateLimit(ctx)
	if err != nil {
		c.holdIfRateLimited(installation, err, c.Now(), "reading Kennel fix quota")
		return nil, err
	}
	if quota == nil || quota.Limit <= 0 || quota.Remaining < 0 || quota.Remaining > quota.Limit || quota.Remaining-github.KennelFixMaximumRequests < quota.Limit/2+quota.Limit%2 {
		retry := time.Minute
		if quota != nil && quota.ResetAt.After(c.Now()) {
			retry = quota.ResetAt.Sub(c.Now())
		}
		return nil, &PRDelayAdmissionError{RetryAfter: retry}
	}
	if c.githubHeld(installation, c.Now()) {
		return nil, github.ErrRateLimited
	}
	source, err := reader.ReadKennelFixSource(ctx, row.FullName, workflowPath, req.BlobSHA)
	if err != nil {
		c.holdIfRateLimited(installation, err, c.Now(), "reading Kennel fix source")
		return nil, err
	}
	if !github.ValidKennelFixSource(source, row.FullName, workflowPath, req.BlobSHA) {
		return nil, ErrKennelFix
	}
	preview, err := optimisation.PreviewTimeout(
		optimisation.TimeoutSource{HeadSHA: source.HeadSHA, Workflow: source.Workflow},
		optimisation.TimeoutRequest{BlobSHA: req.BlobSHA, JobIndex: req.JobIndex, Minutes: req.Minutes}, policy)
	if err != nil {
		return nil, ErrKennelFix
	}
	return &KennelFixInspection{InstallationID: installation, Repository: row.FullName, Finding: finding, Source: *source, Preview: preview}, nil
}

type kennelFixRetainedSource struct {
	HeadSHA string `json:"head_sha"`
	Branch  string `json:"branch"`
	Path    string `json:"path"`
	Mode    string `json:"mode"`
	Content string `json:"content"`
}

type kennelFixRetainedEnvelope struct {
	Version            string                      `json:"version"`
	ID                 string                      `json:"id"`
	InstallationID     string                      `json:"installation_id"`
	Repository         string                      `json:"repository"`
	KennelRepositoryID string                      `json:"kennel_repository_id"`
	ReviewedBy         string                      `json:"reviewed_by"`
	ModelFunding       string                      `json:"model_funding"`
	BlobSHA            string                      `json:"blob_sha"`
	JobIndex           int                         `json:"job_index"`
	Minutes            int                         `json:"minutes"`
	Source             kennelFixRetainedSource     `json:"source"`
	Preview            optimisation.TimeoutPreview `json:"preview"`
}

// RetainedKennelFix is a kept Kennel fix preview. The full source stays in the
// private envelope; a transport renders the edit, metadata and finding only.
type RetainedKennelFix struct {
	ID                 string
	InstallationID     string
	Repository         string
	KennelRepositoryID string
	ReviewedBy         string
	SHA256             string
	CreatedAt          time.Time
	Currentness        string
	Branch             string
	Preview            optimisation.TimeoutPreview
	Approval           *RetainedKennelFixApproval
	Publication        *RetainedKennelFixPublication
}

// RetainedKennelFixApproval is one attributed decision about the exact retained
// preview. It is not a publication and expires with the preview.
type RetainedKennelFixApproval struct {
	ApprovedBy    string
	ApprovedAt    time.Time
	HeadSHA       string
	SourceSHA256  string
	PreviewSHA256 string
}

// RetainedKennelFixPublication is the one draft pull request opened for an
// approved fix. It is not a merge, and it does not close the finding.
type RetainedKennelFixPublication struct {
	PublishedBy   string
	PublishedAt   time.Time
	Branch        string
	BaseBranch    string
	BaseSHA       string
	HeadSHA       string
	SourceSHA256  string
	PreviewSHA256 string
	PullNumber    int
	PullURL       string
}

// CaptureKennelFix re-reads the source, requires the same head and workflow
// digest the operator reviewed, and keeps an immutable private preview for
// fourteen days.
func (c *Controller) CaptureKennelFix(ctx context.Context, actor *store.AuditEvent, req KennelFixRequest, headSHA, sourceSHA256 string) (*RetainedKennelFix, error) {
	if actor == nil || actor.ActorID == "" || !knownHash(headSHA, 40) || !knownHash(sourceSHA256, 64) {
		return nil, ErrKennelFix
	}
	inspection, err := c.InspectKennelFix(ctx, req)
	if err != nil {
		return nil, err
	}
	if inspection.Source.HeadSHA != headSHA || inspection.Source.WorkflowSHA256 != sourceSHA256 {
		return nil, ErrKennelFix
	}
	envelope := kennelFixRetainedEnvelope{
		Version: store.KennelFixPreviewVersion, ID: store.NewID(store.PrefixKennelFixPreview),
		InstallationID: inspection.InstallationID, Repository: inspection.Repository, KennelRepositoryID: req.KennelRepositoryID,
		ReviewedBy: actor.ActorID, ModelFunding: "no_model_call", BlobSHA: req.BlobSHA, JobIndex: req.JobIndex, Minutes: req.Minutes,
		Source:  kennelFixRetainedSource{HeadSHA: inspection.Source.HeadSHA, Branch: inspection.Source.Branch, Path: inspection.Source.Workflow.Path, Mode: inspection.Source.Workflow.Mode, Content: inspection.Source.Workflow.Content},
		Preview: inspection.Preview,
	}
	payload, err := json.Marshal(envelope)
	if err != nil {
		return nil, err
	}
	if len(payload) > store.MaxKennelFixPreviewBytes {
		return nil, ErrKennelFix
	}
	row := &store.KennelFixPreview{ID: envelope.ID, InstallationID: envelope.InstallationID, Repository: envelope.Repository, InitiatingUser: actor.ActorID, Version: envelope.Version, Payload: payload}
	if err := c.st.PutKennelFixPreview(ctx, row, actor); err != nil {
		return nil, err
	}
	return retainedKennelFixView(row, envelope), nil
}

// ReplayKennelFix validates historical calculation, not the current head or
// policy. It never reads replacement source.
func (c *Controller) ReplayKennelFix(ctx context.Context, installation, repo, id string) (*RetainedKennelFix, error) {
	row, err := c.st.GetKennelFixPreview(ctx, installation, repo, id)
	if err != nil {
		return nil, err
	}
	var e kennelFixRetainedEnvelope
	dec := json.NewDecoder(bytes.NewReader(row.Payload))
	dec.DisallowUnknownFields()
	if dec.Decode(&e) != nil || dec.Decode(new(any)) != io.EOF || row.Version != store.KennelFixPreviewVersion || e.Version != row.Version || e.ID != row.ID || e.InstallationID != row.InstallationID || e.Repository != row.Repository || e.ReviewedBy != row.InitiatingUser || e.ModelFunding != "no_model_call" {
		return nil, ErrKennelFix
	}
	source := optimisation.TimeoutSource{HeadSHA: e.Source.HeadSHA, Workflow: prrepair.File{Path: e.Source.Path, Mode: e.Source.Mode, Content: e.Source.Content}}
	preview, err := optimisation.PreviewTimeout(source, optimisation.TimeoutRequest{BlobSHA: e.BlobSHA, JobIndex: e.JobIndex, Minutes: e.Minutes}, optimisation.Policy{SourceAllowed: true, WorkflowEditsAllowed: true})
	if err != nil {
		return nil, ErrKennelFix
	}
	old, _ := json.Marshal(e.Preview)
	now, _ := json.Marshal(preview)
	if !bytes.Equal(old, now) {
		return nil, ErrKennelFix
	}
	e.Preview = preview
	return retainedKennelFixView(row, e), nil
}

func retainedKennelFixView(row *store.KennelFixPreview, e kennelFixRetainedEnvelope) *RetainedKennelFix {
	v := &RetainedKennelFix{
		ID: row.ID, InstallationID: row.InstallationID, Repository: row.Repository, KennelRepositoryID: e.KennelRepositoryID,
		ReviewedBy: row.InitiatingUser, SHA256: row.SHA256, CreatedAt: row.CreatedAt, Currentness: "not_checked", Branch: e.Source.Branch, Preview: e.Preview,
	}
	if a := row.Approval; a != nil {
		v.Approval = &RetainedKennelFixApproval{ApprovedBy: a.ApprovedBy, ApprovedAt: a.CreatedAt, HeadSHA: a.HeadSHA, SourceSHA256: a.SourceSHA256, PreviewSHA256: a.PreviewSHA256}
	}
	if p := row.Publication; p != nil {
		v.Publication = &RetainedKennelFixPublication{PublishedBy: p.PublishedBy, PublishedAt: p.CreatedAt, Branch: p.Branch, BaseBranch: p.BaseBranch, BaseSHA: p.BaseSHA, HeadSHA: p.HeadSHA, SourceSHA256: p.SourceSHA256, PreviewSHA256: p.PreviewSHA256, PullNumber: p.PullNumber, PullURL: p.PullURL}
	}
	return v
}

// ApproveKennelFix records an operator's explicit approval of the exact retained
// preview they name. It reads no source and calls no GitHub reader, so an
// approval cannot publish, edit a workflow or start a job.
func (c *Controller) ApproveKennelFix(ctx context.Context, actor *store.AuditEvent, installation, repo, id, headSHA, sourceSHA256 string) (*RetainedKennelFix, error) {
	if actor == nil || actor.ActorID == "" || installation == "" || repo == "" || id == "" || !knownHash(headSHA, 40) || !knownHash(sourceSHA256, 64) {
		return nil, ErrKennelFixApproval
	}
	current, err := c.ReplayKennelFix(ctx, installation, repo, id)
	if err != nil {
		return nil, err
	}
	if current.Preview.HeadSHA != headSHA || current.Preview.SourceSHA256 != sourceSHA256 {
		return nil, ErrKennelFixApproval
	}
	approval, err := c.st.ApproveKennelFixPreview(ctx, installation, repo, id, headSHA, sourceSHA256, current.SHA256, actor)
	if err != nil {
		return nil, err
	}
	current.Approval = &RetainedKennelFixApproval{ApprovedBy: approval.ApprovedBy, ApprovedAt: approval.CreatedAt, HeadSHA: approval.HeadSHA, SourceSHA256: approval.SourceSHA256, PreviewSHA256: approval.PreviewSHA256}
	return current, nil
}
