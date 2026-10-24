package controller

import (
	"context"
	"errors"
	"strconv"
	"time"

	"github.com/eyupio/zoomies/internal/github"
	"github.com/eyupio/zoomies/internal/optimisation"
	"github.com/eyupio/zoomies/internal/store"
)

var (
	ErrOptimisationCache         = errors.New("cache preview evidence is unavailable, inconsistent or unsupported")
	ErrOptimisationCacheApproval = errors.New("the cache preview's head and source digest do not match the retained preview")
)

// CacheInspection is what an operator is shown before keeping a cache preview.
// It carries the source identity, the retained setup evidence and the computed
// preview, but never the full workflow file in a transport.
type CacheInspection struct {
	Source   github.SetupCacheSource
	Evidence optimisation.SetupEvidence
	Samples  []optimisation.SetupSample
	Preview  optimisation.CachePreview
}

// InspectCachePreview binds one selected fleet job and one pinned GitHub
// attempt to the pure cache preview. The source read is the only GitHub work;
// repeated setup cost comes from the fleet's retained job history. Nothing is
// retained here.
func (c *Controller) InspectCachePreview(ctx context.Context, installation, repo string, selection github.SetupCacheSelection, githubJobID int64) (*optimisation.CachePreview, error) {
	inspection, err := c.inspectCachePreview(ctx, installation, repo, selection, githubJobID)
	if err != nil {
		return nil, err
	}
	return &inspection.Preview, nil
}

func (c *Controller) inspectCachePreview(ctx context.Context, installation, repo string, selection github.SetupCacheSelection, githubJobID int64) (*CacheInspection, error) {
	if installation == "" || repo == "" || githubJobID <= 0 || !github.ValidSetupCacheSelection(selection) {
		return nil, ErrOptimisationCache
	}
	inst, err := c.st.GetInstallation(ctx, installation)
	if err != nil {
		return nil, err
	}
	owner, name, kind := github.SplitTarget(repo)
	if kind != store.TargetRepo || name == "" || !(inst.TargetType == store.TargetRepo && inst.Target == repo || inst.TargetType == store.TargetOrg && inst.Target == owner) {
		return nil, ErrOptimisationCache
	}
	policy, err := c.OptimisationPolicy(ctx)
	if err != nil {
		return nil, err
	}
	if !policy.SourceAllowed || !policy.WorkflowEditsAllowed {
		return nil, ErrOptimisationPolicy
	}
	if c.githubHeld(installation, c.Now()) {
		return nil, github.ErrRateLimited
	}
	client, err := c.ClientFor(ctx, installation)
	if err != nil {
		return nil, err
	}
	reader, ok := client.(github.SetupCacheSourceReader)
	if !ok {
		return nil, ErrOptimisationCache
	}
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	release, err := c.admitPRDelay(installation)
	if err != nil {
		return nil, err
	}
	defer release()
	quota, err := client.RateLimit(ctx)
	if err != nil {
		c.holdIfRateLimited(installation, err, c.Now(), "reading cache preview quota")
		return nil, err
	}
	if quota == nil || quota.Limit <= 0 || quota.Remaining < 0 || quota.Remaining > quota.Limit || quota.Remaining-github.SetupCacheEvidenceMaximumRequests < quota.Limit/2+quota.Limit%2 {
		retry := time.Minute
		if quota != nil && quota.ResetAt.After(c.Now()) {
			retry = quota.ResetAt.Sub(c.Now())
		}
		return nil, &PRDelayAdmissionError{RetryAfter: retry}
	}
	if c.githubHeld(installation, c.Now()) {
		return nil, github.ErrRateLimited
	}
	source, err := reader.ReadSetupCacheSource(ctx, repo, selection)
	if err != nil {
		c.holdIfRateLimited(installation, err, c.Now(), "reading cache preview source")
		return nil, err
	}
	if !github.ValidSetupCacheSource(source, repo, selection) {
		return nil, ErrOptimisationCache
	}
	job, err := c.st.GetJobByGitHubID(ctx, githubJobID)
	if err != nil {
		return nil, err
	}
	if job.Repo != repo || job.InstallationID != installation || job.GitHubRunID != selection.RunID || job.RunAttempt != selection.Attempt || job.HeadSHA != source.HeadSHA || job.Workflow == "" || job.JobName != source.JobName || job.State != store.JobCompleted || job.Conclusion != "success" {
		return nil, ErrOptimisationCache
	}
	var setup *store.JobStep
	for i := range job.Steps {
		step := &job.Steps[i]
		if step.Name != source.StepName {
			continue
		}
		if setup != nil {
			return nil, ErrOptimisationCache
		}
		setup = step
	}
	if setup == nil || setup.Number <= 0 || setup.StartedAt == nil || setup.CompletedAt == nil || !setup.CompletedAt.After(*setup.StartedAt) {
		return nil, ErrOptimisationCache
	}
	samples, err := c.st.RecentSetupStepSamples(ctx, repo, job.Workflow, source.JobName, source.StepName, c.Now().Add(-90*24*time.Hour), 100)
	if err != nil {
		return nil, err
	}
	evidenceSamples := make([]optimisation.SetupSample, 0, len(samples))
	selectedFound := false
	for _, sample := range samples {
		if sample.RunAttempt < 1 || sample.SetupMS <= 0 {
			continue
		}
		if sample.JobID == job.ID {
			selectedFound = true
		}
		evidenceSamples = append(evidenceSamples, optimisation.SetupSample{
			JobID: sample.JobID, RunID: strconv.FormatInt(sample.GitHubRunID, 10), Attempt: sample.RunAttempt,
			StepNumber: sample.StepNumber, SetupMS: sample.SetupMS,
		})
	}
	if !selectedFound {
		return nil, ErrOptimisationCache
	}
	evidence, err := optimisation.SetupEvidenceFromSamples(source.HeadSHA, source.WorkflowSHA256, source.LockfileSHA256, evidenceSamples)
	if err != nil {
		return nil, ErrOptimisationCache
	}
	preview, err := optimisation.PreviewCache(optimisation.CacheSource{HeadSHA: source.HeadSHA, Workflow: source.Workflow, Lockfile: source.Lockfile}, evidence, policy)
	if err != nil {
		return nil, ErrOptimisationCache
	}
	return &CacheInspection{Source: *source, Evidence: evidence, Samples: evidenceSamples, Preview: preview}, nil
}
