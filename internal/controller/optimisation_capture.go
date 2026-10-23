package controller

import (
	"context"
	"encoding/hex"
	"strings"
	"time"

	"github.com/eyupio/zoomies/internal/github"
	"github.com/eyupio/zoomies/internal/optimisation"
	"github.com/eyupio/zoomies/internal/store"
)

// OptimisationPolicyCheck must read current server authorisation/settings. It is
// called before source reads and again before retention; request flags cannot
// authorise source access or workflow previews. No transport exposes this yet.
type OptimisationPolicyCheck func(context.Context) (optimisation.Policy, error)

// knownHash holds a commit or source digest to the shape the rest of the
// optimisation code expects: lower-case hexadecimal of exactly one length.
func knownHash(s string, n int) bool {
	_, err := hex.DecodeString(s)
	return len(s) == n && s == strings.ToLower(s) && err == nil
}

func (c *Controller) CaptureOptimisationPreview(ctx context.Context, actor *store.AuditEvent, installation, repo string, selection github.CheckoutSelection, review optimisation.HistoryReview, check OptimisationPolicyCheck) (*RetainedOptimisationPreview, error) {
	if actor == nil || actor.ActorID == "" || installation == "" || check == nil || !github.ValidCheckoutSelection(selection) || !knownHash(review.HeadSHA, 40) || !knownHash(review.SourceSHA256, 64) || review.NeedsHistory == nil || *review.NeedsHistory || review.Scope != "repository_and_downstream_consumers" {
		return nil, ErrOptimisationPreview
	}
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	evidence, policy, err := c.readCheckoutEvidence(ctx, installation, repo, selection, check)
	if err != nil {
		return nil, err
	}
	collection := &OptimisationCheckoutProvenance{Version: evidence.Version, Basis: evidence.Basis, Selection: evidence.Selection, StepNumber: evidence.StepNumber, BlobSHA: evidence.BlobSHA, JobNameSHA256: evidence.JobNameSHA256, StepNameSHA256: evidence.StepNameSHA256, SourceFunding: "installation_github_read", HeadBranch: evidence.HeadBranch}
	return c.retainOptimisationPreview(ctx, actor, installation, repo, evidence.Source, evidence.Timing, review, policy, collection)
}

// readCheckoutEvidence is the admitted read shared by inspection and capture:
// server policy before any source read, the installation's hold, the shared
// diagnostic flight and quota reserve, the bounded read, and server policy again
// before the caller keeps or shows anything. It never retains.
func (c *Controller) readCheckoutEvidence(ctx context.Context, installation, repo string, selection github.CheckoutSelection, check OptimisationPolicyCheck) (*github.CheckoutEvidence, optimisation.Policy, error) {
	none := optimisation.Policy{}
	policy, err := check(ctx)
	if err != nil {
		return nil, none, err
	}
	if !policy.SourceAllowed || !policy.WorkflowEditsAllowed {
		return nil, none, ErrOptimisationPolicy
	}
	inst, err := c.st.GetInstallation(ctx, installation)
	if err != nil {
		return nil, none, err
	}
	owner, name, kind := github.SplitTarget(repo)
	if kind != store.TargetRepo || name == "" || !(inst.TargetType == store.TargetRepo && inst.Target == repo || inst.TargetType == store.TargetOrg && inst.Target == owner) {
		return nil, none, ErrOptimisationPreview
	}
	if c.githubHeld(installation, c.Now()) {
		return nil, none, github.ErrRateLimited
	}
	client, err := c.ClientFor(ctx, installation)
	if err != nil {
		return nil, none, err
	}
	reader, ok := client.(github.CheckoutEvidenceReader)
	if !ok {
		return nil, none, ErrOptimisationPreview
	}
	release, err := c.admitPRDelay(installation)
	if err != nil {
		return nil, none, err
	}
	defer release()
	quota, err := client.RateLimit(ctx)
	if err != nil {
		c.holdIfRateLimited(installation, err, c.Now(), "reading checkout preview quota")
		return nil, none, err
	}
	if quota == nil || quota.Limit <= 0 || quota.Remaining < 0 || quota.Remaining > quota.Limit || quota.Remaining-github.CheckoutEvidenceMaximumRequests < quota.Limit/2+quota.Limit%2 {
		retry := time.Minute
		if quota != nil && quota.ResetAt.After(c.Now()) {
			retry = quota.ResetAt.Sub(c.Now())
		}
		return nil, none, &PRDelayAdmissionError{RetryAfter: retry}
	}
	if c.githubHeld(installation, c.Now()) {
		return nil, none, github.ErrRateLimited
	}
	evidence, err := reader.ReadCheckoutEvidence(ctx, repo, selection)
	if err != nil {
		c.holdIfRateLimited(installation, err, c.Now(), "reading checkout preview evidence")
		return nil, none, err
	}
	// A Locate read returns the positions it found; everything else the caller
	// named must come back exactly as named.
	if evidence == nil {
		return nil, none, ErrOptimisationPreview
	}
	resolved := evidence.Selection
	if resolved.RunID != selection.RunID || resolved.Attempt != selection.Attempt || resolved.GitHubJobID != selection.GitHubJobID || resolved.Locate || !selection.Locate && (resolved.JobIndex != selection.JobIndex || resolved.StepIndex != selection.StepIndex) || !github.ValidCheckoutEvidence(evidence, repo, resolved) {
		return nil, none, ErrOptimisationPreview
	}
	policy, err = check(ctx)
	if err != nil {
		return nil, none, err
	}
	if !policy.SourceAllowed || !policy.WorkflowEditsAllowed {
		return nil, none, ErrOptimisationPolicy
	}
	return evidence, policy, nil
}
