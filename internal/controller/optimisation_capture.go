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

func (c *Controller) CaptureOptimisationPreview(ctx context.Context, actor *store.AuditEvent, installation, repo string, selection github.CheckoutSelection, review optimisation.HistoryReview, check OptimisationPolicyCheck) (*RetainedOptimisationPreview, error) {
	knownHash := func(s string, n int) bool {
		_, err := hex.DecodeString(s)
		return len(s) == n && s == strings.ToLower(s) && err == nil
	}
	if actor == nil || actor.ActorID == "" || installation == "" || check == nil || !github.ValidCheckoutSelection(selection) || !knownHash(review.HeadSHA, 40) || !knownHash(review.SourceSHA256, 64) || review.NeedsHistory == nil || *review.NeedsHistory || review.Scope != "repository_and_downstream_consumers" {
		return nil, ErrOptimisationPreview
	}
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	evidence, err := c.collectCheckoutEvidence(ctx, installation, repo, selection, check, true)
	if err != nil {
		return nil, err
	}
	policy, err := check(ctx)
	if err != nil {
		return nil, err
	}
	if !policy.SourceAllowed || !policy.WorkflowEditsAllowed {
		return nil, ErrOptimisationPreview
	}
	collection := &OptimisationCheckoutProvenance{Version: evidence.Version, Basis: evidence.Basis, Selection: selection, StepNumber: evidence.StepNumber, BlobSHA: evidence.BlobSHA, JobNameSHA256: evidence.JobNameSHA256, StepNameSHA256: evidence.StepNameSHA256, SourceFunding: "installation_github_read"}
	return c.retainOptimisationPreview(ctx, actor, installation, repo, evidence.Source, evidence.Timing, review, policy, collection)
}

// InspectOptimisationSource returns bounded source-bearing evidence to an already
// authorised caller. It shares capture admission but needs no preview permission.
func (c *Controller) InspectOptimisationSource(ctx context.Context, installation, repo string, selection github.CheckoutSelection, check OptimisationPolicyCheck) (*github.CheckoutEvidence, error) {
	if check == nil || !github.ValidCheckoutSelection(selection) {
		return nil, ErrOptimisationPreview
	}
	return c.collectCheckoutEvidence(ctx, installation, repo, selection, check, false)
}
func (c *Controller) collectCheckoutEvidence(ctx context.Context, installation, repo string, selection github.CheckoutSelection, check OptimisationPolicyCheck, previewRequired bool) (*github.CheckoutEvidence, error) {
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	policy, err := check(ctx)
	if err != nil {
		return nil, err
	}
	if !policy.SourceAllowed || (previewRequired && !policy.WorkflowEditsAllowed) {
		return nil, ErrOptimisationPreview
	}
	inst, err := c.st.GetInstallation(ctx, installation)
	if err != nil {
		return nil, err
	}
	owner, name, kind := github.SplitTarget(repo)
	if kind != store.TargetRepo || name == "" || !(inst.TargetType == store.TargetRepo && inst.Target == repo || inst.TargetType == store.TargetOrg && inst.Target == owner) {
		return nil, ErrOptimisationPreview
	}
	if c.githubHeld(installation, c.Now()) {
		return nil, github.ErrRateLimited
	}
	client, err := c.ClientFor(ctx, installation)
	if err != nil {
		return nil, err
	}
	reader, ok := client.(github.CheckoutEvidenceReader)
	if !ok {
		return nil, ErrOptimisationPreview
	}
	release, err := c.admitPRDelay(installation)
	if err != nil {
		return nil, err
	}
	defer release()
	quota, err := client.RateLimit(ctx)
	if err != nil {
		c.holdIfRateLimited(installation, err, c.Now(), "reading checkout preview quota")
		return nil, err
	}
	if quota == nil || quota.Limit <= 0 || quota.Remaining < 0 || quota.Remaining > quota.Limit || quota.Remaining-github.CheckoutEvidenceMaximumRequests < quota.Limit/2+quota.Limit%2 {
		retry := time.Minute
		if quota != nil && quota.ResetAt.After(c.Now()) {
			retry = quota.ResetAt.Sub(c.Now())
		}
		return nil, &PRDelayAdmissionError{RetryAfter: retry}
	}
	if c.githubHeld(installation, c.Now()) {
		return nil, github.ErrRateLimited
	}
	evidence, err := reader.ReadCheckoutEvidence(ctx, repo, selection)
	if err != nil {
		c.holdIfRateLimited(installation, err, c.Now(), "reading checkout preview evidence")
		return nil, err
	}
	if !github.ValidCheckoutEvidence(evidence, repo, selection) {
		return nil, ErrOptimisationPreview
	}
	policy, err = check(ctx)
	if err != nil {
		return nil, err
	}
	if !policy.SourceAllowed || (previewRequired && !policy.WorkflowEditsAllowed) {
		return nil, ErrOptimisationPreview
	}
	return evidence, nil
}
