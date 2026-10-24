package controller

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"time"

	"github.com/eyupio/zoomies/internal/aicontext"
	"github.com/eyupio/zoomies/internal/github"
	"github.com/eyupio/zoomies/internal/optimisation"
	"github.com/eyupio/zoomies/internal/prrepair"
	"github.com/eyupio/zoomies/internal/store"
)

type optimisationCacheRetainedSource struct {
	HeadSHA string `json:"head_sha"`
	Path    string `json:"path"`
	Mode    string `json:"mode"`
	Content string `json:"content"`
}

type optimisationCacheRetainedLockfile struct {
	Path   string `json:"path"`
	Mode   string `json:"mode"`
	SHA256 string `json:"sha256"`
}

type optimisationCacheRetainedEnvelope struct {
	Version        string                               `json:"version"`
	ID             string                               `json:"id"`
	InstallationID string                               `json:"installation_id"`
	Repository     string                               `json:"repository"`
	ReviewedBy     string                               `json:"reviewed_by"`
	ModelFunding   string                               `json:"model_funding"`
	Provenance     *optimisationCacheRetainedProvenance `json:"provenance,omitempty"`
	Source         optimisationCacheRetainedSource      `json:"source"`
	Lockfile       optimisationCacheRetainedLockfile    `json:"lockfile"`
	Evidence       optimisation.SetupEvidence           `json:"evidence"`
	Samples        []optimisation.SetupSample           `json:"samples"`
	Preview        optimisation.CachePreview            `json:"preview"`
}

type optimisationCacheRetainedProvenance struct {
	SelectionRunID   int64  `json:"selection_run_id"`
	SelectionAttempt int    `json:"selection_attempt"`
	GitHubJobID      int64  `json:"github_job_id"`
	HeadBranch       string `json:"head_branch"`
}

// RetainedCachePreview is a kept cache preview. The full after source stays in
// the private envelope; a transport renders the edit, metadata and evidence
// summary only.
type RetainedCachePreview struct {
	ID             string
	InstallationID string
	Repository     string
	ReviewedBy     string
	SHA256         string
	CreatedAt      time.Time
	Currentness    string
	Preview        optimisation.CachePreview
	Approval       *RetainedCacheApproval
	Publication    *RetainedCachePublication
	Provenance     *CacheProvenance
}

// CacheProvenance is the selected fleet job and branch a cache preview was
// bound to. Previews retained before this was recorded have no provenance and
// cannot be published, because their branch cannot be rechecked.
type CacheProvenance struct {
	Selection   github.SetupCacheSelection
	GitHubJobID int64
	HeadBranch  string
}

// RetainedCacheApproval is one attributed decision about the exact retained
// cache preview. It is not a publication and expires with the preview.
// RetainedCachePublication is the one draft pull request opened for an
// approved cache preview. It is not a merge and does not claim a saving.
type RetainedCachePublication struct {
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

type RetainedCacheApproval struct {
	ApprovedBy    string
	ApprovedAt    time.Time
	HeadSHA       string
	SourceSHA256  string
	PreviewSHA256 string
}

// CaptureCachePreview re-reads the source and setup history, requires the same
// head, workflow digest and lockfile digest the operator reviewed, and keeps an
// immutable private preview for fourteen days.
func (c *Controller) CaptureCachePreview(ctx context.Context, actor *store.AuditEvent, installation, repo string, selection github.SetupCacheSelection, githubJobID int64, headSHA, sourceSHA256, lockfileSHA256 string) (*RetainedCachePreview, error) {
	if actor == nil || actor.ActorID == "" || !knownHash(headSHA, 40) || !knownHash(sourceSHA256, 64) || !knownHash(lockfileSHA256, 64) {
		return nil, ErrOptimisationCache
	}
	inspection, err := c.inspectCachePreview(ctx, installation, repo, selection, githubJobID)
	if err != nil {
		return nil, err
	}
	if inspection.Source.HeadSHA != headSHA || inspection.Source.WorkflowSHA256 != sourceSHA256 || inspection.Source.LockfileSHA256 != lockfileSHA256 {
		return nil, ErrOptimisationCache
	}
	envelope := optimisationCacheRetainedEnvelope{
		Version: store.OptimisationCachePreviewVersion, ID: store.NewID(store.PrefixOptimisationCachePreview),
		InstallationID: installation, Repository: repo, ReviewedBy: actor.ActorID, ModelFunding: "no_model_call",
		Provenance: &optimisationCacheRetainedProvenance{SelectionRunID: selection.RunID, SelectionAttempt: selection.Attempt, GitHubJobID: githubJobID, HeadBranch: inspection.Source.HeadBranch},
		Source:     optimisationCacheRetainedSource{HeadSHA: inspection.Source.HeadSHA, Path: inspection.Source.Workflow.Path, Mode: inspection.Source.Workflow.Mode, Content: inspection.Source.Workflow.Content},
		Lockfile:   optimisationCacheRetainedLockfile{Path: inspection.Source.Lockfile.Path, Mode: inspection.Source.Lockfile.Mode, SHA256: inspection.Source.LockfileSHA256},
		Evidence:   inspection.Evidence, Samples: inspection.Samples, Preview: inspection.Preview,
	}
	payload, err := json.Marshal(envelope)
	if err != nil {
		return nil, err
	}
	if len(payload) > store.MaxOptimisationCachePreviewBytes {
		return nil, ErrOptimisationCache
	}
	row := &store.OptimisationCachePreview{ID: envelope.ID, InstallationID: installation, Repository: repo, InitiatingUser: actor.ActorID, Version: envelope.Version, Payload: payload}
	if err := c.st.PutOptimisationCachePreview(ctx, row, actor); err != nil {
		return nil, err
	}
	return retainedCacheView(row, inspection.Preview), nil
}

// ReplayCachePreview validates historical calculation, not the current head or
// policy. It never reads replacement source.
func (c *Controller) ReplayCachePreview(ctx context.Context, installation, repo, id string) (*RetainedCachePreview, error) {
	row, err := c.st.GetOptimisationCachePreview(ctx, installation, repo, id)
	if err != nil {
		return nil, err
	}
	var e optimisationCacheRetainedEnvelope
	dec := json.NewDecoder(bytes.NewReader(row.Payload))
	dec.DisallowUnknownFields()
	if dec.Decode(&e) != nil || dec.Decode(new(any)) != io.EOF || row.Version != store.OptimisationCachePreviewVersion || e.Version != row.Version || e.ID != row.ID || e.InstallationID != row.InstallationID || e.Repository != row.Repository || e.ReviewedBy != row.InitiatingUser || e.ModelFunding != "no_model_call" {
		return nil, ErrOptimisationCache
	}
	if p := e.Provenance; p != nil && (p.SelectionRunID <= 0 || p.SelectionAttempt <= 0 || p.GitHubJobID <= 0 || !aicontext.ValidBranch(p.HeadBranch)) {
		return nil, ErrOptimisationCache
	}
	evidence, err := optimisation.SetupEvidenceFromSamples(e.Source.HeadSHA, hashString(e.Source.Content), e.Lockfile.SHA256, e.Samples)
	if err != nil {
		return nil, ErrOptimisationCache
	}
	oldEvidence, _ := json.Marshal(e.Evidence)
	newEvidence, _ := json.Marshal(evidence)
	if !bytes.Equal(oldEvidence, newEvidence) {
		return nil, ErrOptimisationCache
	}
	source := optimisation.CacheSource{
		HeadSHA:        e.Source.HeadSHA,
		Workflow:       prrepair.File{Path: e.Source.Path, Mode: e.Source.Mode, Content: e.Source.Content},
		Lockfile:       prrepair.File{Path: e.Lockfile.Path, Mode: e.Lockfile.Mode},
		LockfileSHA256: e.Lockfile.SHA256,
	}
	preview, err := optimisation.PreviewCache(source, evidence, optimisation.Policy{SourceAllowed: true, WorkflowEditsAllowed: true})
	if err != nil {
		return nil, ErrOptimisationCache
	}
	oldPreview, _ := json.Marshal(e.Preview)
	newPreview, _ := json.Marshal(preview)
	if !bytes.Equal(oldPreview, newPreview) {
		return nil, ErrOptimisationCache
	}
	view := retainedCacheView(row, preview)
	if p := e.Provenance; p != nil {
		view.Provenance = &CacheProvenance{Selection: github.SetupCacheSelection{RunID: p.SelectionRunID, Attempt: p.SelectionAttempt}, GitHubJobID: p.GitHubJobID, HeadBranch: p.HeadBranch}
	}
	return view, nil
}

func retainedCacheView(row *store.OptimisationCachePreview, preview optimisation.CachePreview) *RetainedCachePreview {
	v := &RetainedCachePreview{ID: row.ID, InstallationID: row.InstallationID, Repository: row.Repository, ReviewedBy: row.InitiatingUser, SHA256: row.SHA256, CreatedAt: row.CreatedAt, Currentness: "not_checked", Preview: preview}
	if a := row.Approval; a != nil {
		v.Approval = &RetainedCacheApproval{ApprovedBy: a.ApprovedBy, ApprovedAt: a.CreatedAt, HeadSHA: a.HeadSHA, SourceSHA256: a.SourceSHA256, PreviewSHA256: a.PreviewSHA256}
	}
	if p := row.Publication; p != nil {
		v.Publication = &RetainedCachePublication{PublishedBy: p.PublishedBy, PublishedAt: p.CreatedAt, Branch: p.Branch, BaseBranch: p.BaseBranch, BaseSHA: p.BaseSHA, HeadSHA: p.HeadSHA, SourceSHA256: p.SourceSHA256, PreviewSHA256: p.PreviewSHA256, PullNumber: p.PullNumber, PullURL: p.PullURL}
	}
	return v
}

// ApproveCachePreview records an operator's explicit approval of the exact
// retained cache preview they name. It reads no source and calls no GitHub
// reader, so an approval cannot publish, edit a workflow or start a job.
func (c *Controller) ApproveCachePreview(ctx context.Context, actor *store.AuditEvent, installation, repo, id, headSHA, sourceSHA256 string) (*RetainedCachePreview, error) {
	if actor == nil || actor.ActorID == "" || installation == "" || repo == "" || id == "" || !knownHash(headSHA, 40) || !knownHash(sourceSHA256, 64) {
		return nil, ErrOptimisationCacheApproval
	}
	current, err := c.ReplayCachePreview(ctx, installation, repo, id)
	if err != nil {
		return nil, err
	}
	if current.Preview.HeadSHA != headSHA || current.Preview.SourceSHA256 != sourceSHA256 {
		return nil, ErrOptimisationCacheApproval
	}
	approval, err := c.st.ApproveOptimisationCachePreview(ctx, installation, repo, id, headSHA, sourceSHA256, current.SHA256, actor)
	if err != nil {
		return nil, err
	}
	current.Approval = &RetainedCacheApproval{ApprovedBy: approval.ApprovedBy, ApprovedAt: approval.CreatedAt, HeadSHA: approval.HeadSHA, SourceSHA256: approval.SourceSHA256, PreviewSHA256: approval.PreviewSHA256}
	return current, nil
}

func hashString(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}
