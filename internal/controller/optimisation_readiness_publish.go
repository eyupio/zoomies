package controller

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/eyupio/zoomies/internal/github"
	"github.com/eyupio/zoomies/internal/store"
)

// PublishReadinessPreview opens one draft pull request for an approved retained
// readiness preview. It re-reads the selected job, its branch and the workflow
// with the stored provenance, rechecks the live settings and the approval, and
// refuses any change to the head, workflow, condition, bound or edit. It never
// merges and never force-pushes; a retry after an uncertain response reconciles
// the branch and pull request the earlier attempt left behind.
func (c *Controller) PublishReadinessPreview(ctx context.Context, actor *store.AuditEvent, installation, repo, id, headSHA, sourceSHA256 string, check OptimisationPolicyCheck) (*RetainedReadinessPreview, error) {
	if actor == nil || actor.ActorID == "" || installation == "" || repo == "" || id == "" || check == nil || !knownHash(headSHA, 40) || !knownHash(sourceSHA256, 64) {
		return nil, ErrOptimisationPublish
	}
	current, err := c.ReplayReadinessPreview(ctx, installation, repo, id)
	if err != nil {
		return nil, err
	}
	if current.Preview.HeadSHA != headSHA || current.Preview.SourceSHA256 != sourceSHA256 {
		return nil, ErrOptimisationReadinessApproval
	}
	if current.Approval == nil {
		return nil, ErrOptimisationNotApproved
	}
	if current.Approval.HeadSHA != headSHA || current.Approval.SourceSHA256 != sourceSHA256 {
		return nil, ErrOptimisationReadinessApproval
	}
	if current.Publication != nil {
		return current, nil
	}
	if current.Provenance == nil {
		return nil, errReadinessNoProvenance
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
	fresh, err := c.inspectReadinessPreview(ctx, installation, repo, current.Provenance.Selection, current.Provenance.GitHubJobID, current.Preview.Condition)
	if err != nil {
		return nil, err
	}
	afterPolicy, err := check(ctx)
	if err != nil {
		return nil, err
	}
	if !afterPolicy.SourceAllowed || !afterPolicy.WorkflowEditsAllowed || !afterPolicy.PublishAllowed {
		return nil, ErrOptimisationPublishPolicy
	}
	if fresh.Source.HeadSHA != current.Preview.HeadSHA || fresh.Source.WorkflowSHA256 != current.Preview.SourceSHA256 || fresh.Source.HeadBranch != current.Provenance.HeadBranch {
		return nil, fmt.Errorf("%w: the run or its branch changed since the readiness preview; preview it again", ErrOptimisationPublish)
	}
	if fresh.Preview.Condition != current.Preview.Condition || fresh.Preview.WaitSeconds != current.Preview.WaitSeconds || fresh.Preview.Edit.Path != current.Preview.Edit.Path || fresh.Preview.File.Mode != current.Preview.File.Mode || fresh.Preview.File.Content != current.Preview.File.Content || fresh.Preview.AfterSHA256 != current.Preview.AfterSHA256 || fresh.Preview.Edit.After != current.Preview.Edit.After || fresh.Preview.JobName != current.Preview.JobName || fresh.Preview.StepName != current.Preview.StepName || fresh.Preview.Line != current.Preview.Line {
		return nil, fmt.Errorf("%w: the wait step no longer matches the readiness preview; preview it again", ErrOptimisationPublish)
	}
	// Re-read the approval and retained record after the source read: a preview
	// that expired or was evicted meanwhile must not be published, and another
	// request may already have opened its draft.
	latest, err := c.ReplayReadinessPreview(ctx, installation, repo, id)
	if err != nil {
		return nil, err
	}
	if latest.Preview.HeadSHA != headSHA || latest.Preview.SourceSHA256 != sourceSHA256 || latest.SHA256 != current.SHA256 {
		return nil, ErrOptimisationReadinessApproval
	}
	if latest.Approval == nil || latest.Approval.HeadSHA != headSHA || latest.Approval.SourceSHA256 != sourceSHA256 {
		return nil, ErrOptimisationNotApproved
	}
	if latest.Publication != nil {
		return latest, nil
	}
	if latest.Provenance == nil || latest.Provenance.HeadBranch == "" {
		return nil, errReadinessNoProvenance
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
	branch := "zoomies/readiness-" + id
	title := "Poll for readiness instead of sleeping in CI"
	body := fmt.Sprintf("This draft was opened from approved Zoomies readiness preview `%s` at commit `%s`.\n\n"+
		"It replaces one fixed `sleep %d` with a poll of a readiness command an operator supplied, bounded by `timeout %d`: the old duration is the limit, and a timeout fails the step. "+
		"Zoomies did not choose or infer the command; check that it really means the service is ready. "+
		"Zoomies measured no saving: the wait time is descriptive, verification is inconclusive, and passing CI alone cannot establish improvement. "+
		"Zoomies will not merge this pull request; review the diff and let the repository's own checks decide.\n\n"+
		"<!-- zoomies-readiness-optimisation:%s -->\n", id, latest.Preview.HeadSHA, latest.Preview.WaitSeconds, latest.Preview.WaitSeconds, id)
	commitMessage := "Poll for readiness instead of sleeping in CI\n\nZoomies readiness preview " + id
	pr, err := publisher.PublishOptimisation(ctx, github.OptimisationPublishRequest{
		Repo: repo, Base: latest.Provenance.HeadBranch, BaseCommit: latest.Preview.HeadSHA, Head: branch,
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
	record := &store.OptimisationReadinessPublication{
		PreviewID: id, InstallationID: installation, Repository: repo, PublishedBy: actor.ActorID,
		Branch: pr.Branch, BaseBranch: latest.Provenance.HeadBranch, BaseSHA: latest.Preview.HeadSHA, HeadSHA: latest.Preview.HeadSHA,
		SourceSHA256: latest.Preview.SourceSHA256, PreviewSHA256: latest.SHA256,
		PullNumber: pr.Number, PullURL: pr.HTMLURL,
	}
	if err := c.st.PutOptimisationReadinessPublication(ctx, record, actor); err != nil {
		c.log.Error("recording a readiness publication", "error", err, "preview", id, "pull_request", pr.Number)
		return nil, fmt.Errorf("%w: the draft pull request #%d was opened, but its record could not be saved; press publish again to reconcile it", ErrOptimisationPublish, pr.Number)
	}
	latest.Publication = &RetainedReadinessPublication{
		PublishedBy: record.PublishedBy, PublishedAt: record.CreatedAt, Branch: record.Branch, BaseBranch: record.BaseBranch,
		BaseSHA: record.BaseSHA, HeadSHA: record.HeadSHA, SourceSHA256: record.SourceSHA256, PreviewSHA256: record.PreviewSHA256,
		PullNumber: record.PullNumber, PullURL: record.PullURL,
	}
	return latest, nil
}

// errReadinessNoProvenance refuses a preview kept before the selected job and
// branch were recorded: its branch cannot be rechecked, so it cannot be published.
var errReadinessNoProvenance = fmt.Errorf("%w: this preview predates branch recording; make a new readiness preview before publishing", ErrOptimisationPublish)
