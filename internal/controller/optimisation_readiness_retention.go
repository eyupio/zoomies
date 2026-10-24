package controller

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"time"

	"github.com/eyupio/zoomies/internal/aicontext"
	"github.com/eyupio/zoomies/internal/github"
	"github.com/eyupio/zoomies/internal/optimisation"
	"github.com/eyupio/zoomies/internal/prrepair"
	"github.com/eyupio/zoomies/internal/store"
)

type optimisationReadinessRetainedProvenance struct {
	SelectionRunID   int64  `json:"selection_run_id"`
	SelectionAttempt int    `json:"selection_attempt"`
	GitHubJobID      int64  `json:"github_job_id"`
	HeadBranch       string `json:"head_branch"`
}

type optimisationReadinessRetainedSource struct {
	HeadSHA string `json:"head_sha"`
	Path    string `json:"path"`
	Mode    string `json:"mode"`
	Content string `json:"content"`
}

type optimisationReadinessRetainedEnvelope struct {
	Version        string                                   `json:"version"`
	ID             string                                   `json:"id"`
	InstallationID string                                   `json:"installation_id"`
	Repository     string                                   `json:"repository"`
	ReviewedBy     string                                   `json:"reviewed_by"`
	ModelFunding   string                                   `json:"model_funding"`
	Provenance     *optimisationReadinessRetainedProvenance `json:"provenance,omitempty"`
	Source         optimisationReadinessRetainedSource      `json:"source"`
	Evidence       optimisation.ReadinessEvidence           `json:"evidence"`
	Samples        []optimisation.SetupSample               `json:"samples"`
	Preview        optimisation.ReadinessPreview            `json:"preview"`
}

// RetainedReadinessPreview is a kept bounded readiness preview. The full source
// stays in the private envelope; a transport renders the edit, metadata and
// evidence summary only.
type RetainedReadinessPreview struct {
	ID             string
	InstallationID string
	Repository     string
	ReviewedBy     string
	SHA256         string
	CreatedAt      time.Time
	Currentness    string
	Preview        optimisation.ReadinessPreview
	Provenance     *ReadinessProvenance
	Approval       *RetainedReadinessApproval
}

// RetainedReadinessApproval is one attributed decision about the exact retained
// readiness preview. It is not a publication and expires with the preview.
type RetainedReadinessApproval struct {
	ApprovedBy    string
	ApprovedAt    time.Time
	HeadSHA       string
	SourceSHA256  string
	PreviewSHA256 string
}

// ReadinessProvenance is the selected fleet job and branch a kept preview was
// bound to. Previews retained before this was recorded cannot be published.
type ReadinessProvenance struct {
	Selection   github.SetupCacheSelection
	GitHubJobID int64
	HeadBranch  string
}

// CaptureReadinessPreview re-reads the source and wait history, requires the same
// head, workflow digest and condition the operator reviewed, and keeps an
// immutable private preview for fourteen days.
func (c *Controller) CaptureReadinessPreview(ctx context.Context, actor *store.AuditEvent, installation, repo string, selection github.SetupCacheSelection, githubJobID int64, condition, headSHA, sourceSHA256 string) (*RetainedReadinessPreview, error) {
	if actor == nil || actor.ActorID == "" || !knownHash(headSHA, 40) || !knownHash(sourceSHA256, 64) || condition == "" {
		return nil, ErrOptimisationReadiness
	}
	inspection, err := c.inspectReadinessPreview(ctx, installation, repo, selection, githubJobID, condition)
	if err != nil {
		return nil, err
	}
	if inspection.Source.HeadSHA != headSHA || inspection.Source.WorkflowSHA256 != sourceSHA256 || inspection.Preview.Condition != condition {
		return nil, ErrOptimisationReadiness
	}
	envelope := optimisationReadinessRetainedEnvelope{
		Version: store.OptimisationReadinessPreviewVersion, ID: store.NewID(store.PrefixOptimisationReadinessPreview),
		InstallationID: installation, Repository: repo, ReviewedBy: actor.ActorID, ModelFunding: "no_model_call",
		Provenance: &optimisationReadinessRetainedProvenance{SelectionRunID: selection.RunID, SelectionAttempt: selection.Attempt, GitHubJobID: githubJobID, HeadBranch: inspection.Source.HeadBranch},
		Source:     optimisationReadinessRetainedSource{HeadSHA: inspection.Source.HeadSHA, Path: inspection.Source.Workflow.Path, Mode: inspection.Source.Workflow.Mode, Content: inspection.Source.Workflow.Content},
		Evidence:   inspection.Evidence, Samples: inspection.Samples, Preview: inspection.Preview,
	}
	payload, err := json.Marshal(envelope)
	if err != nil {
		return nil, err
	}
	if len(payload) > store.MaxOptimisationReadinessPreviewBytes {
		return nil, ErrOptimisationReadiness
	}
	row := &store.OptimisationReadinessPreview{ID: envelope.ID, InstallationID: installation, Repository: repo, InitiatingUser: actor.ActorID, Version: envelope.Version, Payload: payload}
	if err := c.st.PutOptimisationReadinessPreview(ctx, row, actor); err != nil {
		return nil, err
	}
	return retainedReadinessView(row, inspection.Preview, envelope.Provenance), nil
}

// ReplayReadinessPreview validates historical calculation, not the current head
// or policy. It never reads replacement source.
func (c *Controller) ReplayReadinessPreview(ctx context.Context, installation, repo, id string) (*RetainedReadinessPreview, error) {
	row, err := c.st.GetOptimisationReadinessPreview(ctx, installation, repo, id)
	if err != nil {
		return nil, err
	}
	var e optimisationReadinessRetainedEnvelope
	dec := json.NewDecoder(bytes.NewReader(row.Payload))
	dec.DisallowUnknownFields()
	if dec.Decode(&e) != nil || dec.Decode(new(any)) != io.EOF || row.Version != store.OptimisationReadinessPreviewVersion || e.Version != row.Version || e.ID != row.ID || e.InstallationID != row.InstallationID || e.Repository != row.Repository || e.ReviewedBy != row.InitiatingUser || e.ModelFunding != "no_model_call" {
		return nil, ErrOptimisationReadiness
	}
	if p := e.Provenance; p != nil && (p.SelectionRunID <= 0 || p.SelectionAttempt <= 0 || p.GitHubJobID <= 0 || !aicontext.ValidBranch(p.HeadBranch)) {
		return nil, ErrOptimisationReadiness
	}
	evidence, err := optimisation.ReadinessEvidenceFromSamples(e.Source.HeadSHA, hashString(e.Source.Content), e.Samples)
	if err != nil {
		return nil, ErrOptimisationReadiness
	}
	evidence.Condition = e.Evidence.Condition
	oldEvidence, _ := json.Marshal(e.Evidence)
	newEvidence, _ := json.Marshal(evidence)
	if !bytes.Equal(oldEvidence, newEvidence) {
		return nil, ErrOptimisationReadiness
	}
	source := optimisation.ReadinessSource{HeadSHA: e.Source.HeadSHA, Workflow: prrepair.File{Path: e.Source.Path, Mode: e.Source.Mode, Content: e.Source.Content}}
	preview, err := optimisation.PreviewReadiness(source, evidence, optimisation.Policy{SourceAllowed: true, WorkflowEditsAllowed: true})
	if err != nil {
		return nil, ErrOptimisationReadiness
	}
	oldPreview, _ := json.Marshal(e.Preview)
	newPreview, _ := json.Marshal(preview)
	if !bytes.Equal(oldPreview, newPreview) {
		return nil, ErrOptimisationReadiness
	}
	return retainedReadinessView(row, preview, e.Provenance), nil
}

func retainedReadinessView(row *store.OptimisationReadinessPreview, preview optimisation.ReadinessPreview, p *optimisationReadinessRetainedProvenance) *RetainedReadinessPreview {
	v := &RetainedReadinessPreview{ID: row.ID, InstallationID: row.InstallationID, Repository: row.Repository, ReviewedBy: row.InitiatingUser, SHA256: row.SHA256, CreatedAt: row.CreatedAt, Currentness: "not_checked", Preview: preview}
	if p != nil {
		v.Provenance = &ReadinessProvenance{Selection: github.SetupCacheSelection{RunID: p.SelectionRunID, Attempt: p.SelectionAttempt}, GitHubJobID: p.GitHubJobID, HeadBranch: p.HeadBranch}
	}
	if a := row.Approval; a != nil {
		v.Approval = &RetainedReadinessApproval{ApprovedBy: a.ApprovedBy, ApprovedAt: a.CreatedAt, HeadSHA: a.HeadSHA, SourceSHA256: a.SourceSHA256, PreviewSHA256: a.PreviewSHA256}
	}
	return v
}

// ApproveReadinessPreview records an operator's explicit approval of the exact
// retained readiness preview they name. It reads no source and calls no GitHub
// reader, so an approval cannot publish, edit a workflow or start a job.
func (c *Controller) ApproveReadinessPreview(ctx context.Context, actor *store.AuditEvent, installation, repo, id, headSHA, sourceSHA256 string) (*RetainedReadinessPreview, error) {
	if actor == nil || actor.ActorID == "" || installation == "" || repo == "" || id == "" || !knownHash(headSHA, 40) || !knownHash(sourceSHA256, 64) {
		return nil, ErrOptimisationReadinessApproval
	}
	current, err := c.ReplayReadinessPreview(ctx, installation, repo, id)
	if err != nil {
		return nil, err
	}
	if current.Preview.HeadSHA != headSHA || current.Preview.SourceSHA256 != sourceSHA256 {
		return nil, ErrOptimisationReadinessApproval
	}
	approval, err := c.st.ApproveOptimisationReadinessPreview(ctx, installation, repo, id, headSHA, sourceSHA256, current.SHA256, actor)
	if err != nil {
		return nil, err
	}
	current.Approval = &RetainedReadinessApproval{ApprovedBy: approval.ApprovedBy, ApprovedAt: approval.CreatedAt, HeadSHA: approval.HeadSHA, SourceSHA256: approval.SourceSHA256, PreviewSHA256: approval.PreviewSHA256}
	return current, nil
}
