package controller

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"time"

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
	Version        string                            `json:"version"`
	ID             string                            `json:"id"`
	InstallationID string                            `json:"installation_id"`
	Repository     string                            `json:"repository"`
	ReviewedBy     string                            `json:"reviewed_by"`
	ModelFunding   string                            `json:"model_funding"`
	Source         optimisationCacheRetainedSource   `json:"source"`
	Lockfile       optimisationCacheRetainedLockfile `json:"lockfile"`
	Evidence       optimisation.SetupEvidence        `json:"evidence"`
	Samples        []optimisation.SetupSample        `json:"samples"`
	Preview        optimisation.CachePreview         `json:"preview"`
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
		Source:   optimisationCacheRetainedSource{HeadSHA: inspection.Source.HeadSHA, Path: inspection.Source.Workflow.Path, Mode: inspection.Source.Workflow.Mode, Content: inspection.Source.Workflow.Content},
		Lockfile: optimisationCacheRetainedLockfile{Path: inspection.Source.Lockfile.Path, Mode: inspection.Source.Lockfile.Mode, SHA256: inspection.Source.LockfileSHA256},
		Evidence: inspection.Evidence, Samples: inspection.Samples, Preview: inspection.Preview,
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
	return retainedCacheView(row, preview), nil
}

func retainedCacheView(row *store.OptimisationCachePreview, preview optimisation.CachePreview) *RetainedCachePreview {
	return &RetainedCachePreview{ID: row.ID, InstallationID: row.InstallationID, Repository: row.Repository, ReviewedBy: row.InitiatingUser, SHA256: row.SHA256, CreatedAt: row.CreatedAt, Currentness: "not_checked", Preview: preview}
}

func hashString(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}
