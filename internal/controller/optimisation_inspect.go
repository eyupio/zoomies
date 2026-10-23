package controller

import (
	"context"
	"errors"
	"strconv"
	"time"

	"github.com/eyupio/zoomies/internal/github"
	"github.com/eyupio/zoomies/internal/kennel/workflow"
	"github.com/eyupio/zoomies/internal/optimisation"
)

// ErrOptimisationPolicy means the server's own settings do not permit reading
// workflow source or previewing a workflow edit. It is separate from
// ErrOptimisationPreview so a transport can name the setting to change.
var (
	ErrOptimisationPolicy        = errors.New("optimisation previews are not enabled: turn on optimisation.source_reads and optimisation.workflow_edits")
	ErrOptimisationPublishPolicy = errors.New("optimisation publication is not enabled: turn on optimisation.source_reads, optimisation.workflow_edits and optimisation.publish")
)

// OptimisationPolicy reads the effective settings on every call. Nothing a
// request carries can stand in for it, and a setting turned off between the
// inspection and the capture takes effect at once.
func (c *Controller) OptimisationPolicy(context.Context) (optimisation.Policy, error) {
	o := c.cfg().Optimisation
	return optimisation.Policy{SourceAllowed: o.SourceReads, WorkflowEditsAllowed: o.WorkflowEdits, PublishAllowed: o.Publish}, nil
}

// CheckoutInspection is what an operator is shown before declaring that the
// repository and its downstream consumers do not need full history. It is
// transient: nothing is retained, so a closed page leaves no source behind.
type CheckoutInspection struct {
	Repository     string
	Selection      github.CheckoutSelection
	HeadSHA        string
	Path           string
	Mode           string
	Content        string
	SourceSHA256   string
	StepNumber     int64
	CheckoutMS     int64
	EvidenceID     string
	Basis          string
	InspectedAt    time.Time
	ReviewDeclared string
	// JobName and StepName are the literal names the source gives the job and
	// the checkout step that was found, so a person sees "Build, Checkout"
	// instead of two positions in a file.
	JobName  string
	StepName string
}

// InspectCheckoutEvidence performs the same admitted read as a capture, under
// the same quota, flight and policy rules, and returns the source for review.
func (c *Controller) InspectCheckoutEvidence(ctx context.Context, installation, repo string, selection github.CheckoutSelection) (*CheckoutInspection, error) {
	if installation == "" || !github.ValidCheckoutSelection(selection) {
		return nil, ErrOptimisationPreview
	}
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	e, _, err := c.readCheckoutEvidence(ctx, installation, repo, selection, c.OptimisationPolicy)
	if err != nil {
		return nil, err
	}
	if e.Timing.CheckoutMS == nil {
		return nil, ErrOptimisationPreview
	}
	jobName, stepName, err := workflow.CheckoutNames([]byte(e.Source.File.Content), e.Selection.JobIndex, e.Selection.StepIndex)
	if err != nil {
		return nil, ErrOptimisationPreview
	}
	return &CheckoutInspection{
		JobName: jobName, StepName: stepName,
		Repository: repo, Selection: e.Selection, HeadSHA: e.Source.HeadSHA, Path: e.Source.File.Path, Mode: e.Source.File.Mode,
		Content: e.Source.File.Content, SourceSHA256: e.Timing.SourceSHA256, StepNumber: e.StepNumber,
		CheckoutMS: *e.Timing.CheckoutMS, EvidenceID: e.Timing.EvidenceID, Basis: e.Basis, InspectedAt: c.Now().UTC(),
		ReviewDeclared: "repository_and_downstream_consumers",
	}, nil
}

// OptimisationHistoryReview builds the operator's declaration from what they
// were shown. The scope is fixed here; a request supplies only the digest and
// head it reviewed, and the preview refuses any that no longer match.
func OptimisationHistoryReview(headSHA, sourceSHA256 string) optimisation.HistoryReview {
	needs := false
	return optimisation.HistoryReview{HeadSHA: headSHA, SourceSHA256: sourceSHA256, Scope: "repository_and_downstream_consumers", NeedsHistory: &needs}
}

func DecimalID(n int64) string { return strconv.FormatInt(n, 10) }
