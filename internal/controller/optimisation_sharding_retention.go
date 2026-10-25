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

type optimisationShardingRetainedProvenance struct {
	SelectionRunID   int64  `json:"selection_run_id"`
	SelectionAttempt int    `json:"selection_attempt"`
	GitHubJobID      int64  `json:"github_job_id"`
	HeadBranch       string `json:"head_branch"`
}

type optimisationShardingRetainedSource struct {
	HeadSHA string `json:"head_sha"`
	Path    string `json:"path"`
	Mode    string `json:"mode"`
	Content string `json:"content"`
}

type optimisationShardingRetainedEnvelope struct {
	Version        string                                 `json:"version"`
	ID             string                                 `json:"id"`
	InstallationID string                                 `json:"installation_id"`
	Repository     string                                 `json:"repository"`
	ReviewedBy     string                                 `json:"reviewed_by"`
	ModelFunding   string                                 `json:"model_funding"`
	Provenance     optimisationShardingRetainedProvenance `json:"provenance"`
	Source         optimisationShardingRetainedSource     `json:"source"`
	Evidence       optimisation.ShardEvidence             `json:"evidence"`
	Samples        []optimisation.ShardSample             `json:"samples"`
	Preview        optimisation.ShardPreview              `json:"preview"`
}

// RetainedShardingPreview is a kept sharding preview. The full source stays in
// the private envelope; a transport renders the edit, metadata and evidence
// summary only.
type RetainedShardingPreview struct {
	ID             string
	InstallationID string
	Repository     string
	ReviewedBy     string
	SHA256         string
	CreatedAt      time.Time
	Currentness    string
	Preview        optimisation.ShardPreview
	Provenance     ShardingProvenance
	Approval       *RetainedShardingApproval
}

// RetainedShardingApproval is one attributed decision about the exact retained
// sharding preview. It is not a publication and expires with the preview.
type RetainedShardingApproval struct {
	ApprovedBy    string
	ApprovedAt    time.Time
	HeadSHA       string
	SourceSHA256  string
	PreviewSHA256 string
}

// ShardingProvenance is the selected fleet job and branch a kept preview was
// bound to. Every sharding preview records it, so every one can be rechecked
// before anything is published.
type ShardingProvenance struct {
	Selection   github.SetupCacheSelection
	GitHubJobID int64
	HeadBranch  string
}

// CaptureShardingPreview re-reads the source and history, requires the same
// head, workflow digest and shard count the operator reviewed, and keeps an
// immutable private preview for fourteen days. The count is part of what was
// reviewed: the pool's headroom can move between the inspection and the keep,
// and a different number of shards is a different change.
func (c *Controller) CaptureShardingPreview(ctx context.Context, actor *store.AuditEvent, installation, repo string, selection github.SetupCacheSelection, githubJobID int64, headSHA, sourceSHA256 string, shards int) (*RetainedShardingPreview, error) {
	if actor == nil || actor.ActorID == "" || !knownHash(headSHA, 40) || !knownHash(sourceSHA256, 64) || shards < optimisation.MinShards || shards > optimisation.MaxShards {
		return nil, ErrOptimisationSharding
	}
	inspection, err := c.inspectShardingPreview(ctx, installation, repo, selection, githubJobID)
	if err != nil {
		return nil, err
	}
	if inspection.Source.HeadSHA != headSHA || inspection.Source.WorkflowSHA256 != sourceSHA256 || inspection.Preview.Shards != shards {
		return nil, ErrOptimisationSharding
	}
	envelope := optimisationShardingRetainedEnvelope{
		Version: store.OptimisationShardingPreviewVersion, ID: store.NewID(store.PrefixOptimisationShardingPreview),
		InstallationID: installation, Repository: repo, ReviewedBy: actor.ActorID, ModelFunding: "no_model_call",
		Provenance: optimisationShardingRetainedProvenance{SelectionRunID: selection.RunID, SelectionAttempt: selection.Attempt, GitHubJobID: githubJobID, HeadBranch: inspection.Source.HeadBranch},
		Source:     optimisationShardingRetainedSource{HeadSHA: inspection.Source.HeadSHA, Path: inspection.Source.Workflow.Path, Mode: inspection.Source.Workflow.Mode, Content: inspection.Source.Workflow.Content},
		Evidence:   inspection.Evidence, Samples: inspection.Samples, Preview: inspection.Preview,
	}
	payload, err := json.Marshal(envelope)
	if err != nil {
		return nil, err
	}
	if len(payload) > store.MaxOptimisationShardingPreviewBytes {
		return nil, ErrOptimisationSharding
	}
	row := &store.OptimisationShardingPreview{ID: envelope.ID, InstallationID: installation, Repository: repo, InitiatingUser: actor.ActorID, Version: envelope.Version, Payload: payload}
	if err := c.st.PutOptimisationShardingPreview(ctx, row, actor); err != nil {
		return nil, err
	}
	return retainedShardingView(row, inspection.Preview, envelope.Provenance), nil
}

// ReplayShardingPreview validates historical calculation, not the current head
// or policy. It never reads replacement source. The pool's headroom cannot be
// derived from the samples, so the stored figure is taken as given and the rest
// of the evidence, and the whole preview, are recomputed from it and compared.
func (c *Controller) ReplayShardingPreview(ctx context.Context, installation, repo, id string) (*RetainedShardingPreview, error) {
	row, err := c.st.GetOptimisationShardingPreview(ctx, installation, repo, id)
	if err != nil {
		return nil, err
	}
	var e optimisationShardingRetainedEnvelope
	dec := json.NewDecoder(bytes.NewReader(row.Payload))
	dec.DisallowUnknownFields()
	if dec.Decode(&e) != nil || dec.Decode(new(any)) != io.EOF || row.Version != store.OptimisationShardingPreviewVersion || e.Version != row.Version || e.ID != row.ID || e.InstallationID != row.InstallationID || e.Repository != row.Repository || e.ReviewedBy != row.InitiatingUser || e.ModelFunding != "no_model_call" {
		return nil, ErrOptimisationSharding
	}
	if p := e.Provenance; p.SelectionRunID <= 0 || p.SelectionAttempt <= 0 || p.GitHubJobID <= 0 || !aicontext.ValidBranch(p.HeadBranch) {
		return nil, ErrOptimisationSharding
	}
	evidence, err := optimisation.ShardEvidenceFromSamples(e.Source.HeadSHA, hashString(e.Source.Content), e.Evidence.Headroom, e.Samples)
	if err != nil {
		return nil, ErrOptimisationSharding
	}
	oldEvidence, _ := json.Marshal(e.Evidence)
	newEvidence, _ := json.Marshal(evidence)
	if !bytes.Equal(oldEvidence, newEvidence) {
		return nil, ErrOptimisationSharding
	}
	source := optimisation.ShardSource{HeadSHA: e.Source.HeadSHA, Workflow: prrepair.File{Path: e.Source.Path, Mode: e.Source.Mode, Content: e.Source.Content}}
	preview, err := optimisation.PreviewSharding(source, evidence, optimisation.Policy{SourceAllowed: true, WorkflowEditsAllowed: true})
	if err != nil {
		return nil, ErrOptimisationSharding
	}
	oldPreview, _ := json.Marshal(e.Preview)
	newPreview, _ := json.Marshal(preview)
	if !bytes.Equal(oldPreview, newPreview) {
		return nil, ErrOptimisationSharding
	}
	return retainedShardingView(row, preview, e.Provenance), nil
}

func retainedShardingView(row *store.OptimisationShardingPreview, preview optimisation.ShardPreview, p optimisationShardingRetainedProvenance) *RetainedShardingPreview {
	v := &RetainedShardingPreview{
		ID: row.ID, InstallationID: row.InstallationID, Repository: row.Repository, ReviewedBy: row.InitiatingUser, SHA256: row.SHA256, CreatedAt: row.CreatedAt, Currentness: "not_checked", Preview: preview,
		Provenance: ShardingProvenance{Selection: github.SetupCacheSelection{RunID: p.SelectionRunID, Attempt: p.SelectionAttempt}, GitHubJobID: p.GitHubJobID, HeadBranch: p.HeadBranch},
	}
	if a := row.Approval; a != nil {
		v.Approval = &RetainedShardingApproval{ApprovedBy: a.ApprovedBy, ApprovedAt: a.CreatedAt, HeadSHA: a.HeadSHA, SourceSHA256: a.SourceSHA256, PreviewSHA256: a.PreviewSHA256}
	}
	return v
}

// ApproveShardingPreview records an operator's explicit approval of the exact
// retained sharding preview they name. It reads no source and calls no GitHub
// reader, so an approval cannot publish, edit a workflow or start a job.
func (c *Controller) ApproveShardingPreview(ctx context.Context, actor *store.AuditEvent, installation, repo, id, headSHA, sourceSHA256 string) (*RetainedShardingPreview, error) {
	if actor == nil || actor.ActorID == "" || installation == "" || repo == "" || id == "" || !knownHash(headSHA, 40) || !knownHash(sourceSHA256, 64) {
		return nil, ErrOptimisationShardingApproval
	}
	current, err := c.ReplayShardingPreview(ctx, installation, repo, id)
	if err != nil {
		return nil, err
	}
	if current.Preview.HeadSHA != headSHA || current.Preview.SourceSHA256 != sourceSHA256 {
		return nil, ErrOptimisationShardingApproval
	}
	approval, err := c.st.ApproveOptimisationShardingPreview(ctx, installation, repo, id, headSHA, sourceSHA256, current.SHA256, actor)
	if err != nil {
		return nil, err
	}
	current.Approval = &RetainedShardingApproval{ApprovedBy: approval.ApprovedBy, ApprovedAt: approval.CreatedAt, HeadSHA: approval.HeadSHA, SourceSHA256: approval.SourceSHA256, PreviewSHA256: approval.PreviewSHA256}
	return current, nil
}
