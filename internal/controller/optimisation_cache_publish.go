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

// PublishCachePreview opens one draft pull request for an approved retained
// cache preview. It re-reads the selected job, its branch, the workflow and the
// lockfile, rechecks the live settings and the approval, and refuses any change.
// It never merges and never force-pushes; a retry after an uncertain response
// reconciles the branch and pull request the earlier attempt left behind.
func (c *Controller) PublishCachePreview(ctx context.Context, actor *store.AuditEvent, installation, repo, id, headSHA, sourceSHA256 string, check OptimisationPolicyCheck) (*RetainedCachePreview, error) {
	if actor == nil || actor.ActorID == "" || installation == "" || repo == "" || id == "" || check == nil || !knownHash(headSHA, 40) || !knownHash(sourceSHA256, 64) {
		return nil, ErrOptimisationPublish
	}
	current, err := c.ReplayCachePreview(ctx, installation, repo, id)
	if err != nil {
		return nil, err
	}
	if current.Preview.HeadSHA != headSHA || current.Preview.SourceSHA256 != sourceSHA256 {
		return nil, ErrOptimisationCacheApproval
	}
	if current.Approval == nil {
		return nil, ErrOptimisationNotApproved
	}
	if current.Approval.HeadSHA != headSHA || current.Approval.SourceSHA256 != sourceSHA256 {
		return nil, ErrOptimisationCacheApproval
	}
	if current.Publication != nil {
		return current, nil
	}
	if current.Provenance == nil {
		return nil, fmt.Errorf("%w: this preview predates branch recording; make a new cache preview before publishing", ErrOptimisationPublish)
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
	fresh, err := c.inspectCachePreview(ctx, installation, repo, current.Provenance.Selection, current.Provenance.GitHubJobID)
	if err != nil {
		return nil, err
	}
	afterPolicy, err := check(ctx)
	if err != nil {
		return nil, err
	}
	if !afterPolicy.PublishAllowed {
		return nil, ErrOptimisationPublishPolicy
	}
	if fresh.Source.HeadSHA != current.Preview.HeadSHA || fresh.Source.WorkflowSHA256 != current.Preview.SourceSHA256 || fresh.Source.LockfileSHA256 != current.Preview.LockfileSHA256 || fresh.Source.HeadBranch != current.Provenance.HeadBranch {
		return nil, fmt.Errorf("%w: the run or its branch changed since the cache preview; preview it again", ErrOptimisationPublish)
	}
	if fresh.Preview.Edit.Path != current.Preview.Edit.Path || fresh.Preview.File.Mode != current.Preview.File.Mode || fresh.Preview.File.Content != current.Preview.File.Content || fresh.Preview.AfterSHA256 != current.Preview.AfterSHA256 || fresh.Preview.PackageManager != current.Preview.PackageManager || fresh.Preview.JobName != current.Preview.JobName || fresh.Preview.StepName != current.Preview.StepName || fresh.Preview.Line != current.Preview.Line {
		return nil, fmt.Errorf("%w: the setup block no longer matches the cache preview; preview it again", ErrOptimisationPublish)
	}
	// Re-read the approval and retained record after the source read: a preview
	// that expired or was evicted meanwhile must not be published, and another
	// request may already have opened its draft.
	latest, err := c.ReplayCachePreview(ctx, installation, repo, id)
	if err != nil {
		return nil, err
	}
	if latest.Preview.HeadSHA != headSHA || latest.Preview.SourceSHA256 != sourceSHA256 || latest.SHA256 != current.SHA256 {
		return nil, ErrOptimisationCacheApproval
	}
	if latest.Approval == nil || latest.Approval.HeadSHA != headSHA || latest.Approval.SourceSHA256 != sourceSHA256 {
		return nil, ErrOptimisationNotApproved
	}
	if latest.Publication != nil {
		return latest, nil
	}
	if latest.Provenance == nil || latest.Provenance.HeadBranch == "" {
		return nil, fmt.Errorf("%w: this preview predates branch recording; make a new cache preview before publishing", ErrOptimisationPublish)
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
	branch := "zoomies/cache-" + id
	title := "Cache Node dependencies in CI"
	body := fmt.Sprintf("This draft was opened from approved Zoomies cache preview `%s` at commit `%s`.\n\n"+
		"It adds the setup action's own package cache to one `actions/setup-node` step, using the root lockfile the preview named. "+
		"Zoomies measured no saving: the setup time is descriptive, verification is inconclusive, and passing CI alone cannot establish improvement. "+
		"The cache is shared with whoever can write this repository's cache. Zoomies will not merge this pull request; review the diff and let the repository's own checks decide.\n\n"+
		"<!-- zoomies-cache-optimisation:%s -->\n", id, latest.Preview.HeadSHA, id)
	commitMessage := "Cache Node dependencies in CI\n\nZoomies cache preview " + id
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
	record := &store.OptimisationCachePublication{
		PreviewID: id, InstallationID: installation, Repository: repo, PublishedBy: actor.ActorID,
		Branch: pr.Branch, BaseBranch: latest.Provenance.HeadBranch, BaseSHA: latest.Preview.HeadSHA, HeadSHA: latest.Preview.HeadSHA,
		SourceSHA256: latest.Preview.SourceSHA256, PreviewSHA256: latest.SHA256,
		PullNumber: pr.Number, PullURL: pr.HTMLURL,
	}
	if err := c.st.PutOptimisationCachePublication(ctx, record, actor); err != nil {
		c.log.Error("recording a cache publication", "error", err, "preview", id, "pull_request", pr.Number)
		return nil, fmt.Errorf("%w: the draft pull request #%d was opened, but its record could not be saved; press publish again to reconcile it", ErrOptimisationPublish, pr.Number)
	}
	latest.Publication = &RetainedCachePublication{
		PublishedBy: record.PublishedBy, PublishedAt: record.CreatedAt, Branch: record.Branch, BaseBranch: record.BaseBranch,
		BaseSHA: record.BaseSHA, HeadSHA: record.HeadSHA, SourceSHA256: record.SourceSHA256, PreviewSHA256: record.PreviewSHA256,
		PullNumber: record.PullNumber, PullURL: record.PullURL,
	}
	return latest, nil
}
