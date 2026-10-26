package controller

import (
	"context"
	"errors"
	"time"

	"github.com/eyupio/zoomies/internal/github"
	"github.com/eyupio/zoomies/internal/kennel/offline"
)

// ErrKennelReview is the one refusal for a pull request that cannot be
// reviewed: Kennel Club must be on, the repository tracked, and the
// installation's App able to read pull requests and repository contents.
var ErrKennelReview = errors.New("this pull request cannot be reviewed: it needs Kennel Club on with its workflow checks, a tracked repository and a pull request number, and optimisation source reads turned on")

// KennelReviewReport is a CI-focused review of one pull request, pinned to the
// two commits it was read at. It is computed when asked and stored nowhere: a
// later push is a different review, and nothing is posted to the pull request.
type KennelReviewReport struct {
	KennelRepositoryID string
	InstallationID     string
	Repository         string
	PullNumber         int
	Open               bool
	BaseBranch         string
	// BaseSHA is the merge base the pull request's diff is against.
	BaseSHA string
	HeadSHA string
	ReadAt  time.Time
	// FilesCut is true when the pull request changes more files than were
	// listed, so the paths below are the first of them.
	FilesCut bool
	Review   offline.ReviewReport
}

// ReviewPullRequestCI reviews what one pull request changes in the repository's
// workflows. The rules are the evaluator's own, built into this binary: nothing
// in the pull request, and no instruction file it changes, can alter them. It
// executes nothing, calls no model and writes nothing.
func (c *Controller) ReviewPullRequestCI(ctx context.Context, kennelRepositoryID string, pull int) (*KennelReviewReport, error) {
	if pull < 1 || kennelRepositoryID == "" || !c.cfg().Kennel.Enabled || !c.cfg().Kennel.WorkflowChecks {
		return nil, ErrKennelReview
	}
	row, err := c.st.GetKennelRepository(ctx, kennelRepositoryID)
	if err != nil {
		return nil, err
	}
	if row.Untracked != nil {
		return nil, ErrKennelReview
	}
	policy, err := c.OptimisationPolicy(ctx)
	if err != nil {
		return nil, err
	}
	if !policy.SourceAllowed {
		return nil, ErrOptimisationPolicy
	}
	installation := row.InstallationID
	if c.githubHeld(installation, c.Now()) {
		return nil, github.ErrRateLimited
	}
	client, err := c.ClientFor(ctx, installation)
	if err != nil {
		return nil, err
	}
	reader, ok := client.(github.KennelReviewSourceReader)
	if !ok {
		return nil, ErrKennelReview
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	release, err := c.admitPRDelay(installation)
	if err != nil {
		return nil, err
	}
	defer release()
	quota, err := client.RateLimit(ctx)
	if err != nil {
		c.holdIfRateLimited(installation, err, c.Now(), "reading Kennel review quota")
		return nil, err
	}
	if quota == nil || quota.Limit <= 0 || quota.Remaining < 0 || quota.Remaining > quota.Limit || quota.Remaining-github.KennelReviewMaximumRequests < quota.Limit/2+quota.Limit%2 {
		retry := time.Minute
		if quota != nil && quota.ResetAt.After(c.Now()) {
			retry = quota.ResetAt.Sub(c.Now())
		}
		return nil, &PRDelayAdmissionError{RetryAfter: retry}
	}
	if c.githubHeld(installation, c.Now()) {
		return nil, github.ErrRateLimited
	}
	src, err := reader.ReadKennelReviewSource(ctx, row.FullName, pull)
	if err != nil {
		c.holdIfRateLimited(installation, err, c.Now(), "reading a pull request for Kennel review")
		return nil, err
	}
	if src == nil || src.Repository != row.FullName || src.Number != pull {
		return nil, ErrKennelReview
	}
	review := offline.Review(offline.ReviewInput{
		Base: src.Base, Head: src.Head, Changed: src.Changed, Unreadable: src.Unreadable,
		Public: row.Visibility == "public", Disabled: kennelDisabled(c.cfg().Kennel),
	})
	if src.FilesCut {
		review.Complete = false
	}
	return &KennelReviewReport{KennelRepositoryID: row.ID, InstallationID: installation, Repository: row.FullName, PullNumber: pull,
		Open: src.Open, BaseBranch: src.BaseBranch, BaseSHA: src.BaseSHA, HeadSHA: src.HeadSHA, ReadAt: c.Now(), FilesCut: src.FilesCut, Review: review}, nil
}
