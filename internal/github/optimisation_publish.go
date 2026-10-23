package github

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"unicode/utf8"

	"github.com/eyupio/zoomies/internal/aicontext"
	"github.com/eyupio/zoomies/internal/optimisation"
	gh "github.com/google/go-github/v88/github"
)

// ErrOptimisationPublishConflict means the repository no longer matches the
// reviewed preview, or a branch left by an earlier attempt differs from it.
// Nothing was overwritten and the operator can look before trying again.
var ErrOptimisationPublishConflict = errors.New("optimisation draft pull request does not match the reviewed preview")

// OptimisationPublisher opens one draft pull request for an approved preview.
// It is separate from the Client interface, like the repair and context setup
// surfaces, because only one feature makes these writes.
type OptimisationPublisher interface {
	PublishOptimisation(context.Context, OptimisationPublishRequest) (*PullRequest, error)
}

var _ OptimisationPublisher = (*appClient)(nil)

// OptimisationPublishRequest is one reviewed file change on one new branch.
// The base is pinned to BaseCommit; a branch that has moved is a refusal.
type OptimisationPublishRequest struct {
	Repo          string
	Base          string
	BaseCommit    string
	Head          string
	Title         string
	Body          string
	CommitMessage string
	PreviewID     string
	Path          string
	Mode          string
	Content       string
	SourceSHA256  string
}

// OptimisationPublishPermissionHint names what an App needs before it can open
// an optimisation draft pull request. Workflows write is separate because
// GitHub requires it specifically for a change under .github/workflows.
const OptimisationPublishPermissionHint = `opening an optimisation draft pull request needs three repository permissions: ` +
	`"Contents" (contents) write, "Pull requests" (pull_requests) write, and "Workflows" (workflows) write, ` +
	`which GitHub requires specifically to change files under .github/workflows. ` +
	`An App created for runner management may not hold them: add them under the App's Permissions & events, ` +
	`then accept the change on the installation`

func validOptimisationPublishSHA(s string, n int) bool {
	if len(s) != n {
		return false
	}
	_, err := hex.DecodeString(s)
	return err == nil
}

// publishRequestValid holds every bound before any GitHub object is made.
func publishRequestValid(req OptimisationPublishRequest) bool {
	if !aicontext.ValidBranch(req.Base) || !aicontext.ValidBranch(req.Head) || req.Base == req.Head || !validDelaySHA(req.BaseCommit) || !validOptimisationPublishSHA(req.SourceSHA256, 64) {
		return false
	}
	if req.PreviewID == "" || len(req.PreviewID) > 128 || strings.ContainsAny(req.PreviewID, " /\r\n\x00") {
		return false
	}
	if req.Title == "" || len(req.Title) > 256 || req.Body == "" || len(req.Body) > 65536 || req.CommitMessage == "" || len(req.CommitMessage) > 4096 {
		return false
	}
	if req.Mode != "100644" && req.Mode != "100755" || req.Content == "" || len(req.Content) > optimisation.MaxCheckoutSourceBytes || !utf8.ValidString(req.Content) || strings.ContainsRune(req.Content, 0) {
		return false
	}
	return experimentSourcePath(&ExperimentAttemptEvidence{Event: "push", HeadSHA: req.BaseCommit, WorkflowPath: req.Path}) == req.Path
}

// PublishOptimisation commits the reviewed file on a new branch and opens a
// draft pull request. It never force-pushes and never merges. If an earlier
// attempt left the branch or the pull request, the same preview reconciles
// them; anything that differs is preserved and refused rather than replaced.
func (c *appClient) PublishOptimisation(ctx context.Context, req OptimisationPublishRequest) (*PullRequest, error) {
	owner, name, err := splitRepo(req.Repo)
	if err != nil {
		return nil, err
	}
	if !publishRequestValid(req) {
		return nil, fmt.Errorf("%w: the reviewed change is not safe to publish", ErrOptimisationPublishConflict)
	}
	ref, resp, err := c.asInstallation.Git.GetRef(ctx, owner, name, "heads/"+req.Base)
	if err != nil {
		return nil, c.fail("verify optimisation base branch", resp, err)
	}
	if ref.GetObject().GetSHA() != req.BaseCommit {
		return nil, fmt.Errorf("%w: %s has moved since the preview; preview it again", ErrOptimisationPublishConflict, req.Base)
	}
	existing, existingResp, existingErr := c.asInstallation.Git.GetRef(ctx, owner, name, "heads/"+req.Head)
	if existingErr != nil && !errors.Is(classify(existingResp, existingErr), ErrNotFound) {
		return nil, c.fail("reconcile optimisation branch", existingResp, existingErr)
	}
	if existingErr != nil {
		// A branch that appeared between the two reads is as much a change as a
		// base that moved.
		ref, resp, err = c.asInstallation.Git.GetRef(ctx, owner, name, "heads/"+req.Base)
		if err != nil {
			return nil, c.fail("recheck optimisation base branch", resp, err)
		}
		if ref.GetObject().GetSHA() != req.BaseCommit {
			return nil, fmt.Errorf("%w: %s has moved since the preview; preview it again", ErrOptimisationPublishConflict, req.Base)
		}
	}
	base, resp, err := c.asInstallation.Git.GetCommit(ctx, owner, name, req.BaseCommit)
	if err != nil {
		return nil, c.fail("read optimisation base commit", resp, err)
	}
	if base.GetTree().GetSHA() == "" {
		return nil, fmt.Errorf("%w: the base commit has no tree", ErrOptimisationPublishConflict)
	}
	tree, resp, err := c.asInstallation.Git.CreateTree(ctx, owner, name, base.GetTree().GetSHA(), []*gh.TreeEntry{{
		Path: gh.Ptr(req.Path), Mode: gh.Ptr(req.Mode), Type: gh.Ptr("blob"), Content: gh.Ptr(req.Content),
	}})
	if err != nil {
		return nil, c.optimisationPublishError("prepare optimisation file", classify(resp, err))
	}
	message := strings.TrimSpace(req.CommitMessage)
	if existingErr == nil {
		previous, resp, err := c.asInstallation.Git.GetCommit(ctx, owner, name, existing.GetObject().GetSHA())
		if err != nil {
			return nil, c.fail("verify existing optimisation branch", resp, err)
		}
		if previous.GetTree().GetSHA() != tree.GetSHA() || previous.GetMessage() != message || len(previous.Parents) != 1 || previous.Parents[0].GetSHA() != req.BaseCommit {
			return nil, fmt.Errorf("%w: an existing branch differs from this preview or its base; it has been preserved", ErrOptimisationPublishConflict)
		}
	} else {
		commit, resp, err := c.asInstallation.Git.CreateCommit(ctx, owner, name, gh.Commit{
			Message: gh.Ptr(message),
			Tree:    tree,
			Parents: []*gh.Commit{{SHA: gh.Ptr(req.BaseCommit)}},
		}, nil)
		if err != nil {
			return nil, c.optimisationPublishError("prepare optimisation commit", classify(resp, err))
		}
		commitSHA := commit.GetSHA()
		ref, resp, err = c.asInstallation.Git.GetRef(ctx, owner, name, "heads/"+req.Base)
		if err != nil {
			return nil, c.fail("recheck optimisation base branch", resp, err)
		}
		if ref.GetObject().GetSHA() != req.BaseCommit {
			return nil, fmt.Errorf("%w: %s has moved since the preview; preview it again", ErrOptimisationPublishConflict, req.Base)
		}
		_, resp, err = c.asInstallation.Git.CreateRef(ctx, owner, name, gh.CreateRef{Ref: "refs/heads/" + req.Head, SHA: commitSHA})
		if err != nil {
			// A concurrent attempt may have created the same deterministic
			// branch. It is ours only if it carries the same commit.
			race, raceResp, raceErr := c.asInstallation.Git.GetRef(ctx, owner, name, "heads/"+req.Head)
			if raceErr == nil {
				prior, priorResp, priorErr := c.asInstallation.Git.GetCommit(ctx, owner, name, race.GetObject().GetSHA())
				if priorErr != nil {
					return nil, c.fail("verify concurrent optimisation branch", priorResp, priorErr)
				}
				if prior.GetTree().GetSHA() != tree.GetSHA() || prior.GetMessage() != message || len(prior.Parents) != 1 || prior.Parents[0].GetSHA() != req.BaseCommit {
					return nil, fmt.Errorf("%w: the branch %s already exists with different content; it has been preserved", ErrOptimisationPublishConflict, req.Head)
				}
			} else if !errors.Is(classify(raceResp, raceErr), ErrNotFound) {
				return nil, c.fail("reconcile concurrent optimisation branch", raceResp, raceErr)
			} else {
				return nil, c.optimisationPublishError("publish optimisation branch", classify(resp, err))
			}
		}
	}
	// Closed PRs are included: a retry must not reopen a deliberately declined
	// proposal or duplicate one whose successful response was lost.
	pulls, resp, err := c.asInstallation.PullRequests.List(ctx, owner, name, &gh.PullRequestListOptions{State: "all", Head: owner + ":" + req.Head, Base: req.Base, ListOptions: gh.ListOptions{PerPage: 100}})
	if err != nil {
		return nil, c.fail("reconcile optimisation pull request", resp, err)
	}
	if resp != nil && resp.NextPage != 0 {
		return nil, fmt.Errorf("%w: too many pull requests to reconcile safely", ErrOptimisationPublishConflict)
	}
	if len(pulls) > 0 {
		if len(pulls) != 1 || pulls[0].GetTitle() != req.Title || pulls[0].GetBody() != req.Body {
			return nil, fmt.Errorf("%w: an existing pull request differs from this preview; it has been preserved", ErrOptimisationPublishConflict)
		}
		if pulls[0].GetState() != "open" {
			return nil, fmt.Errorf("%w: the previous draft pull request is closed; check its outcome before publishing again", ErrOptimisationPublishConflict)
		}
		return &PullRequest{Number: pulls[0].GetNumber(), HTMLURL: pulls[0].GetHTMLURL(), Branch: req.Head}, nil
	}
	// The last read before opening a new pull request: a base that moved while
	// the branch was being prepared would make the draft include somebody
	// else's change. The branch is ours and has no pull request, so it goes.
	ref, resp, err = c.asInstallation.Git.GetRef(ctx, owner, name, "heads/"+req.Base)
	if err != nil {
		return nil, c.fail("recheck optimisation base branch", resp, err)
	}
	if ref.GetObject().GetSHA() != req.BaseCommit {
		return nil, c.discardBranch(ctx, owner, name, req.Head, fmt.Errorf("%w: %s has moved since the preview; the draft was not opened", ErrOptimisationPublishConflict, req.Base))
	}
	pr, resp, err := c.asInstallation.PullRequests.Create(ctx, owner, name, &gh.NewPullRequest{
		Title: gh.Ptr(req.Title), Body: gh.Ptr(req.Body), Head: gh.Ptr(req.Head), Base: gh.Ptr(req.Base), Draft: gh.Ptr(true),
	})
	if err != nil {
		cause := c.optimisationPublishError("open optimisation draft pull request", classify(resp, err))
		// A concurrent attempt may have opened the matching pull request. A 422
		// can also mean one already exists, so reconcile before deciding the
		// branch is debris.
		after, _, afterErr := c.asInstallation.PullRequests.List(ctx, owner, name, &gh.PullRequestListOptions{State: "all", Head: owner + ":" + req.Head, Base: req.Base, ListOptions: gh.ListOptions{PerPage: 100}})
		if afterErr == nil {
			if len(after) == 1 && after[0].GetTitle() == req.Title && after[0].GetBody() == req.Body && after[0].GetState() == "open" {
				return &PullRequest{Number: after[0].GetNumber(), HTMLURL: after[0].GetHTMLURL(), Branch: req.Head}, nil
			}
			if len(after) > 0 {
				return nil, fmt.Errorf("%w: an existing pull request differs from this preview; it has been preserved", ErrOptimisationPublishConflict)
			}
		}
		// Only a definite refusal with no matching pull request proves there is
		// nothing to keep. A lost response, a 5xx or a 422 may have opened one.
		if afterErr != nil || resp == nil || resp.StatusCode >= 500 || resp.StatusCode == http.StatusRequestTimeout || resp.StatusCode == http.StatusUnprocessableEntity {
			return nil, fmt.Errorf("%w; the branch %s was kept because the pull request may have been opened from it; check the repository's pull requests before deleting it", cause, req.Head)
		}
		return nil, c.discardBranch(ctx, owner, name, req.Head, cause)
	}
	return &PullRequest{Number: pr.GetNumber(), HTMLURL: pr.GetHTMLURL(), Branch: req.Head}, nil
}

func (c *appClient) optimisationPublishError(op string, e error) error {
	if e == nil {
		return nil
	}
	if errors.Is(e, ErrForbidden) {
		return fmt.Errorf("github: %s: %w; %s", op, e, OptimisationPublishPermissionHint)
	}
	return errorf(op, e)
}
