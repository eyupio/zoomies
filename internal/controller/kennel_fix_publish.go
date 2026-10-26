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

// PublishKennelFix opens one draft pull request for an approved retained fix.
// It re-reads the file from the default branch, rechecks the live settings, the
// approval and that the finding is still open and not waived, and refuses any
// change to the file or the edit. The fix is bound to its file's blob, not to
// the branch head, so a branch that moved while the file did not is a base for
// the draft rather than a refusal. It never merges and never force-pushes; a
// retry after an uncertain response reconciles the branch and pull request the
// earlier attempt left behind. It does not close the finding: Kennel Club does,
// when it reads the file on the branch and the job has a timeout.
func (c *Controller) PublishKennelFix(ctx context.Context, actor *store.AuditEvent, installation, repo, id, headSHA, sourceSHA256 string, check OptimisationPolicyCheck) (*RetainedKennelFix, error) {
	if actor == nil || actor.ActorID == "" || installation == "" || repo == "" || id == "" || check == nil || !knownHash(headSHA, 40) || !knownHash(sourceSHA256, 64) {
		return nil, ErrOptimisationPublish
	}
	current, err := c.ReplayKennelFix(ctx, installation, repo, id)
	if err != nil {
		return nil, err
	}
	if current.Preview.HeadSHA != headSHA || current.Preview.SourceSHA256 != sourceSHA256 {
		return nil, ErrKennelFixApproval
	}
	if current.Approval == nil {
		return nil, ErrOptimisationNotApproved
	}
	if current.Approval.HeadSHA != headSHA || current.Approval.SourceSHA256 != sourceSHA256 {
		return nil, ErrKennelFixApproval
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
	req := KennelFixRequest{KennelRepositoryID: current.KennelRepositoryID, BlobSHA: current.Preview.BlobSHA, JobIndex: current.Preview.JobIndex, Minutes: current.Preview.Minutes}
	// The finding must still be open and not waived, and the file must still be
	// the blob the preview was made from: the inspection refuses both.
	fresh, err := c.InspectKennelFix(ctx, req)
	if err != nil {
		if errors.Is(err, github.ErrKennelFixStale) {
			return nil, fmt.Errorf("%w: the workflow file changed since the fix was previewed; preview it again from the finding", ErrOptimisationPublish)
		}
		return nil, err
	}
	afterPolicy, err := check(ctx)
	if err != nil {
		return nil, err
	}
	if !afterPolicy.SourceAllowed || !afterPolicy.WorkflowEditsAllowed || !afterPolicy.PublishAllowed {
		return nil, ErrOptimisationPublishPolicy
	}
	if fresh.Source.WorkflowSHA256 != current.Preview.SourceSHA256 || fresh.Source.Branch != current.Branch || fresh.Preview.AfterSHA256 != current.Preview.AfterSHA256 || fresh.Preview.Edit != current.Preview.Edit || fresh.Preview.File != current.Preview.File || fresh.Preview.Minutes != current.Preview.Minutes || fresh.Preview.JobName != current.Preview.JobName {
		return nil, fmt.Errorf("%w: the workflow file or its branch no longer matches the Kennel fix preview; preview it again", ErrOptimisationPublish)
	}
	// Re-read the approval and retained record after the source read: a preview
	// that expired or was evicted meanwhile must not be published, and another
	// request may already have opened its draft.
	latest, err := c.ReplayKennelFix(ctx, installation, repo, id)
	if err != nil {
		return nil, err
	}
	if latest.Preview.HeadSHA != headSHA || latest.Preview.SourceSHA256 != sourceSHA256 || latest.SHA256 != current.SHA256 {
		return nil, ErrKennelFixApproval
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
	branch := "zoomies/kennel-fix-" + id
	title := "Give " + latest.Preview.JobName + " a timeout in CI"
	body := fmt.Sprintf("This draft was opened from approved Zoomies Kennel fix preview `%s` for the `ci.no_timeout` finding, read at commit `%s`.\n\n"+
		"It gives the job `%s` a `timeout-minutes` of %d. Zoomies did not choose the value: an operator supplied it, so check that a healthy run of this job finishes well inside it. "+
		"The finding closes when Kennel Club next reads this file on the default branch and the job has a timeout, not when this pull request is opened. "+
		"Zoomies will not merge this pull request; review the diff and let the repository's own checks decide.\n\n"+
		"<!-- zoomies-kennel-fix:%s -->\n", id, fresh.Source.HeadSHA, latest.Preview.JobName, latest.Preview.Minutes, id)
	commitMessage := "Give " + latest.Preview.JobName + " a timeout in CI\n\nZoomies Kennel fix preview " + id
	pr, err := publisher.PublishOptimisation(ctx, github.OptimisationPublishRequest{
		Repo: repo, Base: fresh.Source.Branch, BaseCommit: fresh.Source.HeadSHA, Head: branch,
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
	record := &store.KennelFixPublication{
		PreviewID: id, InstallationID: installation, Repository: repo, PublishedBy: actor.ActorID,
		Branch: pr.Branch, BaseBranch: fresh.Source.Branch, BaseSHA: fresh.Source.HeadSHA, HeadSHA: latest.Preview.HeadSHA,
		SourceSHA256: latest.Preview.SourceSHA256, PreviewSHA256: latest.SHA256,
		PullNumber: pr.Number, PullURL: pr.HTMLURL,
	}
	if err := c.st.PutKennelFixPublication(ctx, record, actor); err != nil {
		c.log.Error("recording a Kennel fix publication", "error", err, "preview", id, "pull_request", pr.Number)
		return nil, fmt.Errorf("%w: the draft pull request #%d was opened, but its record could not be saved; press publish again to reconcile it", ErrOptimisationPublish, pr.Number)
	}
	latest.Publication = &RetainedKennelFixPublication{
		PublishedBy: record.PublishedBy, PublishedAt: record.CreatedAt, Branch: record.Branch, BaseBranch: record.BaseBranch,
		BaseSHA: record.BaseSHA, HeadSHA: record.HeadSHA, SourceSHA256: record.SourceSHA256, PreviewSHA256: record.PreviewSHA256,
		PullNumber: record.PullNumber, PullURL: record.PullURL,
	}
	return latest, nil
}
