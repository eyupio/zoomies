package controller

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"time"

	"github.com/eyupio/zoomies/internal/github"
	"github.com/eyupio/zoomies/internal/optimisation"
	"github.com/eyupio/zoomies/internal/prrepair"
	"github.com/eyupio/zoomies/internal/store"
)

var (
	ErrOptimisationPreview  = errors.New("optimisation preview evidence is unavailable, inconsistent or unsupported")
	ErrOptimisationApproval = errors.New("the approval does not name the retained preview")
)

type optimisationRetainedSource struct {
	HeadSHA string `json:"head_sha"`
	Path    string `json:"path"`
	Mode    string `json:"mode"`
	Content string `json:"content"`
}

func (s optimisationRetainedSource) source() optimisation.Source {
	return optimisation.Source{HeadSHA: s.HeadSHA, File: prrepair.File{Path: s.Path, Mode: s.Mode, Content: s.Content}}
}

// OptimisationCheckoutProvenance contains no raw API names, URLs or logs.
// Nil remains unknown for previews retained before evidence admission.
type OptimisationCheckoutProvenance struct {
	Version        string                   `json:"version"`
	Basis          string                   `json:"basis"`
	Selection      github.CheckoutSelection `json:"selection"`
	StepNumber     int64                    `json:"step_number"`
	BlobSHA        string                   `json:"blob_sha"`
	JobNameSHA256  string                   `json:"job_name_sha256"`
	StepNameSHA256 string                   `json:"step_name_sha256"`
	SourceFunding  string                   `json:"source_funding"`
	// HeadBranch is the branch the push ran on. A preview retained before this
	// was recorded has none, stays readable and is not publishable.
	HeadBranch string `json:"head_branch,omitempty"`
}

func (p *OptimisationCheckoutProvenance) evidence(repo string, source optimisation.Source, timing optimisation.Timing) *github.CheckoutEvidence {
	return &github.CheckoutEvidence{Version: p.Version, Basis: p.Basis, Repository: repo, Selection: p.Selection, StepNumber: p.StepNumber, BlobSHA: p.BlobSHA, JobNameSHA256: p.JobNameSHA256, StepNameSHA256: p.StepNameSHA256, Source: source, Timing: timing, HeadBranch: p.HeadBranch}
}

type optimisationRetainedEnvelope struct {
	Collection     *OptimisationCheckoutProvenance `json:"collection,omitempty"`
	Version        string                          `json:"version"`
	ID             string                          `json:"id"`
	InstallationID string                          `json:"installation_id"`
	Repository     string                          `json:"repository"`
	ReviewedBy     string                          `json:"reviewed_by"`
	ModelFunding   string                          `json:"model_funding"`
	Source         optimisationRetainedSource      `json:"source"`
	Timing         optimisation.Timing             `json:"timing"`
	History        optimisation.HistoryReview      `json:"history"`
	Preview        optimisation.Preview            `json:"preview"`
}

// RetainedOptimisationApproval is a separate, attributed act from keeping the
// preview. It names the exact preview, head and source digest that were
// approved, and it grants no publication authority by itself.
type RetainedOptimisationApproval struct {
	ApprovedBy    string
	ApprovedAt    time.Time
	HeadSHA       string
	SourceSHA256  string
	PreviewSHA256 string
}

// RetainedOptimisationPublication is the one draft pull request opened for an
// approved preview. It is a record of the external write, not a merge and not
// a claim that the change saves time.
type RetainedOptimisationPublication struct {
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

type RetainedOptimisationPreview struct {
	Collection     *OptimisationCheckoutProvenance
	Approval       *RetainedOptimisationApproval
	Publication    *RetainedOptimisationPublication
	ID             string
	InstallationID string
	Repository     string
	ReviewedBy     string
	SHA256         string
	CreatedAt      time.Time
	Currentness    string
	Preview        optimisation.Preview
}

// RetainOptimisationPreview is an internal adapter for already admitted source,
// timing and policy. It is not a collector and has no route. A future transport
// must independently authorise source-bearing history and obtain evidence; request
// booleans or client-submitted timing must never stand in for server admission.
func (c *Controller) RetainOptimisationPreview(ctx context.Context, actor *store.AuditEvent, installation, repo string, source optimisation.Source, timing optimisation.Timing, history optimisation.HistoryReview, policy optimisation.Policy) (*RetainedOptimisationPreview, error) {
	return c.retainOptimisationPreview(ctx, actor, installation, repo, source, timing, history, policy, nil)
}
func (c *Controller) retainOptimisationPreview(ctx context.Context, actor *store.AuditEvent, installation, repo string, source optimisation.Source, timing optimisation.Timing, history optimisation.HistoryReview, policy optimisation.Policy, collection *OptimisationCheckoutProvenance) (*RetainedOptimisationPreview, error) {
	if collection != nil && (collection.SourceFunding != "installation_github_read" || !github.ValidCheckoutEvidence(collection.evidence(repo, source, timing), repo, collection.Selection)) {
		return nil, ErrOptimisationPreview
	}
	if actor == nil || actor.ActorID == "" || source.File.Partial || source.File.StartLine != 0 {
		return nil, ErrOptimisationPreview
	}
	preview, err := optimisation.PreviewCheckout(source, timing, history, policy)
	if err != nil {
		return nil, ErrOptimisationPreview
	}
	envelope := optimisationRetainedEnvelope{
		Collection: collection,
		Version:    store.OptimisationPreviewVersion, ID: store.NewID(store.PrefixOptimisationPreview),
		InstallationID: installation, Repository: repo, ReviewedBy: actor.ActorID,
		ModelFunding: "no_model_call",
		Source:       optimisationRetainedSource{HeadSHA: source.HeadSHA, Path: source.File.Path, Mode: source.File.Mode, Content: source.File.Content},
		Timing:       timing, History: history, Preview: preview,
	}
	payload, err := json.Marshal(envelope)
	if err != nil {
		return nil, err
	}
	row := &store.OptimisationPreview{ID: envelope.ID, InstallationID: installation, Repository: repo, InitiatingUser: actor.ActorID, Version: envelope.Version, Payload: payload}
	if err := c.st.PutOptimisationPreview(ctx, row, actor); err != nil {
		return nil, err
	}
	return retainedOptimisationView(row, preview, collection), nil
}

// ReplayOptimisationPreview validates historical calculation, not current head
// or current publication policy. It never reads replacement source. Its private
// source-bearing result requires separate authorisation before any future route.
func (c *Controller) ReplayOptimisationPreview(ctx context.Context, installation, repo, id string) (*RetainedOptimisationPreview, error) {
	row, err := c.st.GetOptimisationPreview(ctx, installation, repo, id)
	if err != nil {
		return nil, err
	}
	var e optimisationRetainedEnvelope
	dec := json.NewDecoder(bytes.NewReader(row.Payload))
	dec.DisallowUnknownFields()
	if dec.Decode(&e) != nil || dec.Decode(new(any)) != io.EOF || row.Version != store.OptimisationPreviewVersion || e.Version != row.Version || e.ID != row.ID || e.InstallationID != row.InstallationID || e.Repository != row.Repository || e.ReviewedBy != row.InitiatingUser || e.ModelFunding != "no_model_call" {
		return nil, ErrOptimisationPreview
	}
	if e.Collection != nil && (e.Collection.SourceFunding != "installation_github_read" || !github.ValidCheckoutEvidence(e.Collection.evidence(row.Repository, e.Source.source(), e.Timing), row.Repository, e.Collection.Selection)) {
		return nil, ErrOptimisationPreview
	}
	// Policy here permits recomputation only. Replay neither grants future edit
	// authority nor claims the historical preview remains current.
	preview, err := optimisation.PreviewCheckout(e.Source.source(), e.Timing, e.History, optimisation.Policy{SourceAllowed: true, WorkflowEditsAllowed: true})
	if err != nil {
		return nil, ErrOptimisationPreview
	}
	oldJSON, err := json.Marshal(e.Preview)
	if err != nil {
		return nil, err
	}
	newJSON, err := json.Marshal(preview)
	if err != nil {
		return nil, err
	}
	if !bytes.Equal(oldJSON, newJSON) {
		return nil, ErrOptimisationPreview
	}
	return retainedOptimisationView(row, preview, e.Collection), nil
}

// ApproveOptimisationPreview records an explicit, attributed approval of the
// exact retained preview the caller names. It reads no source and calls no
// GitHub reader, so an approval cannot publish, edit a workflow or start a job.
// The head and source digest must be the ones the retained preview already
// binds; a mismatch is refused rather than approved by accident.
func (c *Controller) ApproveOptimisationPreview(ctx context.Context, actor *store.AuditEvent, installation, repo, id, headSHA, sourceSHA256 string) (*RetainedOptimisationPreview, error) {
	if actor == nil || actor.ActorID == "" || id == "" || installation == "" || repo == "" || !knownHash(headSHA, 40) || !knownHash(sourceSHA256, 64) {
		return nil, ErrOptimisationApproval
	}
	current, err := c.ReplayOptimisationPreview(ctx, installation, repo, id)
	if err != nil {
		return nil, err
	}
	if current.Preview.HeadSHA != headSHA || current.Preview.SourceSHA256 != sourceSHA256 {
		return nil, ErrOptimisationApproval
	}
	approval, err := c.st.ApproveOptimisationPreview(ctx, installation, repo, id, headSHA, sourceSHA256, current.SHA256, actor)
	if err != nil {
		return nil, err
	}
	current.Approval = &RetainedOptimisationApproval{
		ApprovedBy: approval.ApprovedBy, ApprovedAt: approval.CreatedAt,
		HeadSHA: approval.HeadSHA, SourceSHA256: approval.SourceSHA256, PreviewSHA256: approval.PreviewSHA256,
	}
	return current, nil
}

func retainedOptimisationView(row *store.OptimisationPreview, preview optimisation.Preview, collection *OptimisationCheckoutProvenance) *RetainedOptimisationPreview {
	v := &RetainedOptimisationPreview{Collection: collection, ID: row.ID, InstallationID: row.InstallationID, Repository: row.Repository, ReviewedBy: row.InitiatingUser, SHA256: row.SHA256, CreatedAt: row.CreatedAt, Currentness: "not_checked", Preview: preview}
	if a := row.Approval; a != nil {
		v.Approval = &RetainedOptimisationApproval{ApprovedBy: a.ApprovedBy, ApprovedAt: a.CreatedAt, HeadSHA: a.HeadSHA, SourceSHA256: a.SourceSHA256, PreviewSHA256: a.PreviewSHA256}
	}
	if p := row.Publication; p != nil {
		v.Publication = &RetainedOptimisationPublication{PublishedBy: p.PublishedBy, PublishedAt: p.CreatedAt, Branch: p.Branch, BaseBranch: p.BaseBranch, BaseSHA: p.BaseSHA, HeadSHA: p.HeadSHA, SourceSHA256: p.SourceSHA256, PreviewSHA256: p.PreviewSHA256, PullNumber: p.PullNumber, PullURL: p.PullURL}
	}
	return v
}
