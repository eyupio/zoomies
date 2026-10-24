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

// PublishShardingPreview opens one draft pull request for an approved retained
// sharding preview. It re-reads the selected job, its branch, the workflow and
// the history with the stored provenance, rechecks the live settings and the
// approval, and refuses a change to the head, the workflow, the branch or the
// step. It never merges and never force-pushes; a retry after an uncertain
// response reconciles the branch and pull request the earlier attempt left.
//
// The shard count is the one thing allowed to differ on the fresh read, and
// only upwards: the pool's headroom moves with the fleet's load, so insisting on
// the same count would refuse a good change whenever a runner happened to be
// busy. A count the pool or the history no longer supports is refused, and what
// is published is always the approved change, never a recomputed one.
func (c *Controller) PublishShardingPreview(ctx context.Context, actor *store.AuditEvent, installation, repo, id, headSHA, sourceSHA256 string, check OptimisationPolicyCheck) (*RetainedShardingPreview, error) {
	if actor == nil || actor.ActorID == "" || installation == "" || repo == "" || id == "" || check == nil || !knownHash(headSHA, 40) || !knownHash(sourceSHA256, 64) {
		return nil, ErrOptimisationPublish
	}
	current, err := c.ReplayShardingPreview(ctx, installation, repo, id)
	if err != nil {
		return nil, err
	}
	if current.Preview.HeadSHA != headSHA || current.Preview.SourceSHA256 != sourceSHA256 {
		return nil, ErrOptimisationShardingApproval
	}
	if current.Approval == nil {
		return nil, ErrOptimisationNotApproved
	}
	if current.Approval.HeadSHA != headSHA || current.Approval.SourceSHA256 != sourceSHA256 {
		return nil, ErrOptimisationShardingApproval
	}
	if current.Publication != nil {
		return current, nil
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
	fresh, err := c.inspectShardingPreview(ctx, installation, repo, current.Provenance.Selection, current.Provenance.GitHubJobID)
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
		return nil, fmt.Errorf("%w: the run or its branch changed since the sharding preview; preview it again", ErrOptimisationPublish)
	}
	if fresh.Preview.JobName != current.Preview.JobName || fresh.Preview.StepName != current.Preview.StepName || fresh.Preview.Line != current.Preview.Line || fresh.Preview.Edit.Path != current.Preview.Edit.Path || fresh.Preview.File.Mode != current.Preview.File.Mode {
		return nil, fmt.Errorf("%w: the test step no longer matches the sharding preview; preview it again", ErrOptimisationPublish)
	}
	if fresh.Preview.Shards < current.Preview.Shards {
		return nil, fmt.Errorf("%w: the pool or the job's history no longer supports %d shards (it would now choose %d); preview it again", ErrOptimisationPublish, current.Preview.Shards, fresh.Preview.Shards)
	}
	// Re-read the approval and retained record after the source read: a preview
	// that expired or was evicted meanwhile must not be published, and another
	// request may already have opened its draft.
	latest, err := c.ReplayShardingPreview(ctx, installation, repo, id)
	if err != nil {
		return nil, err
	}
	if latest.Preview.HeadSHA != headSHA || latest.Preview.SourceSHA256 != sourceSHA256 || latest.SHA256 != current.SHA256 {
		return nil, ErrOptimisationShardingApproval
	}
	if latest.Approval == nil || latest.Approval.HeadSHA != headSHA || latest.Approval.SourceSHA256 != sourceSHA256 {
		return nil, ErrOptimisationNotApproved
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
	p := latest.Preview
	branch := "zoomies/sharding-" + id
	title := "Shard the " + p.JobName + " Playwright tests in CI"
	body := fmt.Sprintf("This draft was opened from approved Zoomies sharding preview `%s` at commit `%s`.\n\n"+
		"It splits the `%s` step of `%s` into %d Playwright shards using Playwright's own `--shard` flag, so together they run the whole suite. "+
		"A job with the original name stays behind and fails unless every shard passed, so a required check that names the job still resolves.\n\n"+
		"Zoomies chose the count from the job's retained history and the pool's headroom. It costs more runner time, not less: every shard repeats the setup, so a run goes from about %ds to about %ds of runner time, and it needs %d runners at once. "+
		"The wall-clock figure (about %ds to about %ds) is an estimate from medians, verification is inconclusive, and passing CI alone cannot establish an improvement. "+
		"Zoomies will not merge this pull request; review the diff and let the repository's own checks decide.\n\n"+
		"<!-- zoomies-sharding-optimisation:%s -->\n", id, p.HeadSHA, p.StepName, p.JobName, p.Shards,
		p.RunnerMSBefore/1000, p.RunnerMSAfter/1000, p.Shards, p.WallMSBefore/1000, p.WallMSAfter/1000, id)
	commitMessage := "Shard the " + p.JobName + " Playwright tests in CI\n\nZoomies sharding preview " + id
	pr, err := publisher.PublishOptimisation(ctx, github.OptimisationPublishRequest{
		Repo: repo, Base: latest.Provenance.HeadBranch, BaseCommit: p.HeadSHA, Head: branch,
		Title: title, Body: body, CommitMessage: commitMessage, PreviewID: id,
		Path: p.Edit.Path, Mode: p.File.Mode, Content: p.File.Content,
		SourceSHA256: p.SourceSHA256,
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
	record := &store.OptimisationShardingPublication{
		PreviewID: id, InstallationID: installation, Repository: repo, PublishedBy: actor.ActorID,
		Branch: pr.Branch, BaseBranch: latest.Provenance.HeadBranch, BaseSHA: p.HeadSHA, HeadSHA: p.HeadSHA,
		SourceSHA256: p.SourceSHA256, PreviewSHA256: latest.SHA256,
		PullNumber: pr.Number, PullURL: pr.HTMLURL,
	}
	if err := c.st.PutOptimisationShardingPublication(ctx, record, actor); err != nil {
		c.log.Error("recording a sharding publication", "error", err, "preview", id, "pull_request", pr.Number)
		return nil, fmt.Errorf("%w: the draft pull request #%d was opened, but its record could not be saved; press publish again to reconcile it", ErrOptimisationPublish, pr.Number)
	}
	latest.Publication = &RetainedShardingPublication{
		PublishedBy: record.PublishedBy, PublishedAt: record.CreatedAt, Branch: record.Branch, BaseBranch: record.BaseBranch,
		BaseSHA: record.BaseSHA, HeadSHA: record.HeadSHA, SourceSHA256: record.SourceSHA256, PreviewSHA256: record.PreviewSHA256,
		PullNumber: record.PullNumber, PullURL: record.PullURL,
	}
	return latest, nil
}
