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
	ErrOptimisationReadiness         = errors.New("readiness preview evidence is unavailable, inconsistent or unsupported")
	ErrOptimisationReadinessApproval = errors.New("the readiness preview's head and source digest do not match the retained preview")
)

// ReadinessInspection is what an operator is shown before keeping a readiness
// preview. It carries the source identity, the retained wait evidence and the
// computed preview, but never the full workflow file in a transport.
type ReadinessInspection struct {
	Source   github.ReadinessSource
	Evidence optimisation.ReadinessEvidence
	Samples  []optimisation.SetupSample
	Preview  optimisation.ReadinessPreview
}

// InspectReadinessPreview binds one selected fleet job and one pinned GitHub
// attempt to the pure readiness preview. The source read is the only GitHub
// work; repeated wait cost comes from the fleet's retained job history. It
// reads no lockfile, retains nothing and guesses no condition.
func (c *Controller) InspectReadinessPreview(ctx context.Context, installation, repo string, selection github.SetupCacheSelection, githubJobID int64, condition string) (*optimisation.ReadinessPreview, error) {
	inspection, err := c.inspectReadinessPreview(ctx, installation, repo, selection, githubJobID, condition)
	if err != nil {
		return nil, err
	}
	return &inspection.Preview, nil
}

func (c *Controller) inspectReadinessPreview(ctx context.Context, installation, repo string, selection github.SetupCacheSelection, githubJobID int64, condition string) (*ReadinessInspection, error) {
	if installation == "" || repo == "" || githubJobID <= 0 || !github.ValidSetupCacheSelection(selection) || condition == "" {
		return nil, ErrOptimisationReadiness
	}
	inst, err := c.st.GetInstallation(ctx, installation)
	if err != nil {
		return nil, err
	}
	owner, name, kind := github.SplitTarget(repo)
	if kind != store.TargetRepo || name == "" || !(inst.TargetType == store.TargetRepo && inst.Target == repo || inst.TargetType == store.TargetOrg && inst.Target == owner) {
		return nil, ErrOptimisationReadiness
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
	reader, ok := client.(github.ReadinessSourceReader)
	if !ok {
		return nil, ErrOptimisationReadiness
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
		c.holdIfRateLimited(installation, err, c.Now(), "reading readiness preview quota")
		return nil, err
	}
	if quota == nil || quota.Limit <= 0 || quota.Remaining < 0 || quota.Remaining > quota.Limit || quota.Remaining-github.ReadinessEvidenceMaximumRequests < quota.Limit/2+quota.Limit%2 {
		retry := time.Minute
		if quota != nil && quota.ResetAt.After(c.Now()) {
			retry = quota.ResetAt.Sub(c.Now())
		}
		return nil, &PRDelayAdmissionError{RetryAfter: retry}
	}
	if c.githubHeld(installation, c.Now()) {
		return nil, github.ErrRateLimited
	}
	source, err := reader.ReadReadinessSource(ctx, repo, selection)
	if err != nil {
		c.holdIfRateLimited(installation, err, c.Now(), "reading readiness preview source")
		return nil, err
	}
	if !github.ValidReadinessSource(source, repo, selection) {
		return nil, ErrOptimisationReadiness
	}
	job, err := c.st.GetJobByGitHubID(ctx, githubJobID)
	if err != nil {
		return nil, err
	}
	if job.Repo != repo || job.InstallationID != installation || job.GitHubRunID != selection.RunID || job.RunAttempt != selection.Attempt || job.HeadSHA != source.HeadSHA || job.Workflow == "" || job.JobName != source.JobName || job.State != store.JobCompleted || job.Conclusion != "success" {
		return nil, ErrOptimisationReadiness
	}
	var wait *store.JobStep
	for i := range job.Steps {
		step := &job.Steps[i]
		if step.Name != source.StepName {
			continue
		}
		if wait != nil {
			return nil, ErrOptimisationReadiness
		}
		wait = step
	}
	if wait == nil || wait.Number <= 0 || wait.StartedAt == nil || wait.CompletedAt == nil || !wait.CompletedAt.After(*wait.StartedAt) {
		return nil, ErrOptimisationReadiness
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
		return nil, ErrOptimisationReadiness
	}
	evidence, err := optimisation.ReadinessEvidenceFromSamples(source.HeadSHA, source.WorkflowSHA256, evidenceSamples)
	if err != nil {
		return nil, ErrOptimisationReadiness
	}
	evidence.Condition = condition
	preview, err := optimisation.PreviewReadiness(optimisation.ReadinessSource{HeadSHA: source.HeadSHA, Workflow: source.Workflow}, evidence, policy)
	if err != nil {
		return nil, ErrOptimisationReadiness
	}
	return &ReadinessInspection{Source: *source, Evidence: evidence, Samples: evidenceSamples, Preview: preview}, nil
}
