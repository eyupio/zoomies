package controller

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/eyupio/zoomies/internal/github"
	"github.com/eyupio/zoomies/internal/optimisation"
	"github.com/eyupio/zoomies/internal/store"
)

var (
	ErrOptimisationPublish           = errors.New("optimisation publication is unavailable, inconsistent or unsupported")
	ErrOptimisationNotApproved       = errors.New("this optimisation preview has not been approved for publication")
	ErrOptimisationPublishPermission = fmt.Errorf("%w: the installation cannot open a draft pull request", ErrOptimisationPublish)
)

// PublishOptimisationPreview opens one draft pull request for an approved
// retained preview. It re-reads the run, its branch and the source, rechecks the
// live settings and the approval, and refuses any change. It never merges and
// never force-pushes; a retry after an uncertain response reconciles the branch
// and pull request the earlier attempt left behind.
func (c *Controller) PublishOptimisationPreview(ctx context.Context, actor *store.AuditEvent, installation, repo, id, headSHA, sourceSHA256 string, check OptimisationPolicyCheck) (*RetainedOptimisationPreview, error) {
	if actor == nil || actor.ActorID == "" || installation == "" || repo == "" || id == "" || check == nil || !knownHash(headSHA, 40) || !knownHash(sourceSHA256, 64) {
		return nil, ErrOptimisationPublish
	}
	current, err := c.ReplayOptimisationPreview(ctx, installation, repo, id)
	if err != nil {
		return nil, err
	}
	if current.Preview.HeadSHA != headSHA || current.Preview.SourceSHA256 != sourceSHA256 {
		return nil, ErrOptimisationApproval
	}
	if current.Approval == nil {
		return nil, ErrOptimisationNotApproved
	}
	if current.Approval.HeadSHA != headSHA || current.Approval.SourceSHA256 != sourceSHA256 {
		return nil, ErrOptimisationApproval
	}
	if current.Publication != nil {
		return current, nil
	}
	if current.Collection == nil || current.Collection.HeadBranch == "" {
		return nil, fmt.Errorf("%w: this preview predates branch recording; make a new preview before publishing", ErrOptimisationPublish)
	}
	policy, err := check(ctx)
	if err != nil {
		return nil, err
	}
	if !policy.SourceAllowed || !policy.WorkflowEditsAllowed || !policy.PublishAllowed {
		return nil, ErrOptimisationPublishPolicy
	}
	ctx, cancel := context.WithTimeout(ctx, time.Minute)
	defer cancel()
	evidence, afterPolicy, err := c.readCheckoutEvidence(ctx, installation, repo, current.Collection.Selection, check)
	if err != nil {
		return nil, err
	}
	if !afterPolicy.PublishAllowed {
		return nil, ErrOptimisationPublishPolicy
	}
	if evidence.Source.HeadSHA != current.Preview.HeadSHA || evidence.Timing.SourceSHA256 != current.Preview.SourceSHA256 || evidence.HeadBranch != current.Collection.HeadBranch {
		return nil, fmt.Errorf("%w: the run or its branch changed since the preview; preview it again", ErrOptimisationPublish)
	}
	if err := optimisation.CheckCurrent(current.Preview, evidence.Source, afterPolicy); err != nil {
		return nil, fmt.Errorf("%w: the source no longer matches the preview; preview it again", ErrOptimisationPublish)
	}
	// Re-read the approval and retained record after the source read: a preview
	// that expired or was evicted meanwhile must not be published, and another
	// request may already have opened its draft.
	latest, err := c.ReplayOptimisationPreview(ctx, installation, repo, id)
	if err != nil {
		return nil, err
	}
	if latest.Preview.HeadSHA != headSHA || latest.Preview.SourceSHA256 != sourceSHA256 || latest.SHA256 != current.SHA256 {
		return nil, ErrOptimisationApproval
	}
	if latest.Approval == nil {
		return nil, ErrOptimisationNotApproved
	}
	if latest.Approval.HeadSHA != headSHA || latest.Approval.SourceSHA256 != sourceSHA256 {
		return nil, ErrOptimisationApproval
	}
	if latest.Collection == nil || latest.Collection.HeadBranch == "" {
		return nil, fmt.Errorf("%w: this preview predates branch recording; make a new preview before publishing", ErrOptimisationPublish)
	}
	if latest.Publication != nil {
		return latest, nil
	}
	client, err := c.ClientFor(ctx, installation)
	if err != nil {
		return nil, err
	}
	publisher, ok := client.(github.OptimisationPublisher)
	if !ok {
		return nil, fmt.Errorf("%w: this GitHub client cannot open draft pull requests", ErrOptimisationPublish)
	}
	info, err := client.Probe(ctx)
	if err != nil {
		if errors.Is(err, github.ErrForbidden) {
			return nil, fmt.Errorf("%w: %s", ErrOptimisationPublishPermission, github.OptimisationPublishPermissionHint)
		}
		return nil, fmt.Errorf("%w: %v", ErrOptimisationPublish, err)
	}
	if info == nil {
		return nil, fmt.Errorf("%w: GitHub did not report the App's permissions", ErrOptimisationPublish)
	}
	if missing := info.MissingForMigration(); len(missing) > 0 {
		return nil, fmt.Errorf("%w: %s; %s", ErrOptimisationPublishPermission, github.OptimisationPublishPermissionHint, strings.Join(missing, ", "))
	}
	branch := "zoomies/checkout-" + id
	title := "Read one commit in a workflow"
	body := fmt.Sprintf("This draft was opened from approved Zoomies preview `%s` at commit `%s`.\n\n"+
		"It changes one `actions/checkout` step from fetching full history to fetching one commit. "+
		"Zoomies measured no saving: verification is inconclusive, and passing CI alone cannot establish improvement. "+
		"Zoomies will not merge this pull request; review the diff and let the repository's own checks decide.\n\n"+
		"<!-- zoomies-optimisation:%s -->\n", id, latest.Preview.HeadSHA, id)
	commitMessage := "Read one commit in a workflow\n\nZoomies optimisation preview " + id
	pr, err := publisher.PublishOptimisation(ctx, github.OptimisationPublishRequest{
		Repo: repo, Base: latest.Collection.HeadBranch, BaseCommit: latest.Preview.HeadSHA, Head: branch,
		Title: title, Body: body, CommitMessage: commitMessage, PreviewID: id,
		Path: latest.Preview.Edit.Path, Mode: latest.Preview.File.Mode, Content: latest.Preview.File.Content,
		SourceSHA256: latest.Preview.SourceSHA256,
	})
	if err != nil {
		if errors.Is(err, github.ErrForbidden) {
			return nil, fmt.Errorf("%w: %s", ErrOptimisationPublishPermission, github.OptimisationPublishPermissionHint)
		}
		return nil, fmt.Errorf("%w: %w", ErrOptimisationPublish, err)
	}
	if pr == nil || pr.Number <= 0 || pr.HTMLURL == "" || pr.Branch != branch {
		return nil, fmt.Errorf("%w: GitHub did not return a usable draft pull request", ErrOptimisationPublish)
	}
	record := &store.OptimisationPublication{
		PreviewID: id, InstallationID: installation, Repository: repo, PublishedBy: actor.ActorID,
		Branch: pr.Branch, BaseBranch: latest.Collection.HeadBranch, BaseSHA: latest.Preview.HeadSHA, HeadSHA: latest.Preview.HeadSHA,
		SourceSHA256: latest.Preview.SourceSHA256, PreviewSHA256: latest.SHA256,
		PullNumber: pr.Number, PullURL: pr.HTMLURL,
	}
	if err := c.st.PutOptimisationPublication(ctx, record, actor); err != nil {
		c.log.Error("recording an optimisation publication", "error", err, "preview", id, "pull_request", pr.Number)
		return nil, fmt.Errorf("%w: the draft pull request #%d was opened, but its record could not be saved; press publish again to reconcile it", ErrOptimisationPublish, pr.Number)
	}
	latest.Publication = &RetainedOptimisationPublication{
		PublishedBy: record.PublishedBy, PublishedAt: record.CreatedAt, Branch: record.Branch, BaseBranch: record.BaseBranch,
		BaseSHA: record.BaseSHA, HeadSHA: record.HeadSHA, SourceSHA256: record.SourceSHA256, PreviewSHA256: record.PreviewSHA256,
		PullNumber: record.PullNumber, PullURL: record.PullURL,
	}
	return latest, nil
}
