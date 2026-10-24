package controller

import (
	"context"
	"errors"
	"strconv"
	"time"

	"github.com/eyupio/zoomies/internal/github"
	"github.com/eyupio/zoomies/internal/kennel/workflow"
	"github.com/eyupio/zoomies/internal/optimisation"
	"github.com/eyupio/zoomies/internal/scheduler"
	"github.com/eyupio/zoomies/internal/store"
)

var ErrOptimisationSharding = errors.New("sharding preview evidence is unavailable, inconsistent or unsupported")

// ShardingInspection is what an operator is shown before keeping a sharding
// preview. It carries the source identity, the retained timing evidence and the
// computed preview, but never the full workflow file in a transport.
type ShardingInspection struct {
	Source   github.ShardSource
	Evidence optimisation.ShardEvidence
	Samples  []optimisation.ShardSample
	Preview  optimisation.ShardPreview
}

// InspectShardingPreview binds one selected fleet job and one pinned GitHub
// attempt to the pure sharding preview. The source read is the only GitHub work;
// the test and setup timings come from the fleet's retained job history and the
// headroom from the fleet's own pools and hosts. It retains nothing.
func (c *Controller) InspectShardingPreview(ctx context.Context, installation, repo string, selection github.SetupCacheSelection, githubJobID int64) (*optimisation.ShardPreview, error) {
	inspection, err := c.inspectShardingPreview(ctx, installation, repo, selection, githubJobID)
	if err != nil {
		return nil, err
	}
	return &inspection.Preview, nil
}

func (c *Controller) inspectShardingPreview(ctx context.Context, installation, repo string, selection github.SetupCacheSelection, githubJobID int64) (*ShardingInspection, error) {
	if installation == "" || repo == "" || githubJobID <= 0 || !github.ValidSetupCacheSelection(selection) {
		return nil, ErrOptimisationSharding
	}
	inst, err := c.st.GetInstallation(ctx, installation)
	if err != nil {
		return nil, err
	}
	owner, name, kind := github.SplitTarget(repo)
	if kind != store.TargetRepo || name == "" || !(inst.TargetType == store.TargetRepo && inst.Target == repo || inst.TargetType == store.TargetOrg && inst.Target == owner) {
		return nil, ErrOptimisationSharding
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
	reader, ok := client.(github.ShardSourceReader)
	if !ok {
		return nil, ErrOptimisationSharding
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
		c.holdIfRateLimited(installation, err, c.Now(), "reading sharding preview quota")
		return nil, err
	}
	if quota == nil || quota.Limit <= 0 || quota.Remaining < 0 || quota.Remaining > quota.Limit || quota.Remaining-github.ShardEvidenceMaximumRequests < quota.Limit/2+quota.Limit%2 {
		retry := time.Minute
		if quota != nil && quota.ResetAt.After(c.Now()) {
			retry = quota.ResetAt.Sub(c.Now())
		}
		return nil, &PRDelayAdmissionError{RetryAfter: retry}
	}
	if c.githubHeld(installation, c.Now()) {
		return nil, github.ErrRateLimited
	}
	source, err := reader.ReadShardSource(ctx, repo, selection)
	if err != nil {
		c.holdIfRateLimited(installation, err, c.Now(), "reading sharding preview source")
		return nil, shardingSourceError(err)
	}
	if !github.ValidShardSource(source, repo, selection) {
		return nil, ErrOptimisationSharding
	}
	job, err := c.st.GetJobByGitHubID(ctx, githubJobID)
	if err != nil {
		return nil, err
	}
	if job.Repo != repo || job.InstallationID != installation || job.GitHubRunID != selection.RunID || job.RunAttempt != selection.Attempt || job.HeadSHA != source.HeadSHA || job.Workflow == "" || job.JobName != source.JobName || job.State != store.JobCompleted || job.Conclusion != "success" {
		return nil, ErrOptimisationSharding
	}
	history, err := c.st.RecentSetupStepSamples(ctx, repo, job.Workflow, source.JobName, source.StepName, c.Now().Add(-90*24*time.Hour), 100)
	if err != nil {
		return nil, err
	}
	samples := make([]optimisation.ShardSample, 0, len(history))
	selectedFound := false
	for _, h := range history {
		// A run whose test step had nothing recorded before it has no setup
		// figure to repeat, so it cannot size the shards.
		if h.RunAttempt < 1 || h.SetupMS <= 0 || h.LeadMS <= 0 {
			continue
		}
		if h.JobID == job.ID {
			selectedFound = true
		}
		samples = append(samples, optimisation.ShardSample{
			JobID: h.JobID, RunID: strconv.FormatInt(h.GitHubRunID, 10), Attempt: h.RunAttempt,
			StepNumber: h.StepNumber, TestMS: h.SetupMS, SetupMS: h.LeadMS,
		})
	}
	if !selectedFound {
		return nil, ErrOptimisationSharding
	}
	headroom, err := c.shardHeadroom(ctx, installation, job.Labels)
	if err != nil {
		return nil, err
	}
	evidence, err := optimisation.ShardEvidenceFromSamples(source.HeadSHA, source.WorkflowSHA256, headroom, samples)
	if err != nil {
		return nil, err
	}
	preview, err := optimisation.PreviewSharding(optimisation.ShardSource{HeadSHA: source.HeadSHA, Workflow: source.Workflow}, evidence, policy)
	if err != nil {
		return nil, err
	}
	return &ShardingInspection{Source: *source, Evidence: evidence, Samples: samples, Preview: preview}, nil
}

// shardHeadroom is how many more runners the best pool for the job's labels
// could start right now: its configured maximum less the runners it already
// has, held down by the free slots on hosts that are healthy and not cordoned.
// It is an upper bound read at one moment, not a reservation, and the preview
// says so; shards that cannot all start at once queue behind each other.
func (c *Controller) shardHeadroom(ctx context.Context, installation string, labels []string) (int, error) {
	pools, err := c.st.ListPools(ctx)
	if err != nil {
		return 0, err
	}
	counts, err := c.st.CountRunnersByPool(ctx)
	if err != nil {
		return 0, err
	}
	best := 0
	for _, p := range pools {
		if !p.Enabled || p.InstallationID != installation || !scheduler.PoolMatches(p, labels) {
			continue
		}
		if room := p.MaxRunners - counts[p.ID].Live(); room > best {
			best = room
		}
	}
	if best == 0 {
		return 0, nil
	}
	hosts, err := c.st.ListHosts(ctx)
	if err != nil {
		return 0, err
	}
	now, free := c.Now(), 0
	for _, h := range hosts {
		if h.Healthy(now) && !h.Cordoned {
			free += h.Free()
		}
	}
	return min(best, free), nil
}

// shardingSourceError turns the workflow's own refusal into the one the
// operator is shown, so they are told what to change in the file instead of that
// a read failed. Every other error is passed on unchanged.
func shardingSourceError(err error) error {
	var refusal *workflow.ShardRefusal
	switch {
	case errors.As(err, &refusal):
		return &optimisation.ShardingRefusal{Reason: refusal.Reason}
	case errors.Is(err, workflow.ErrShardNotFound):
		return &optimisation.ShardingRefusal{Reason: "no single Playwright test step on an Ubuntu runner was found in the workflow"}
	case errors.Is(err, workflow.ErrShardAmbiguous):
		return &optimisation.ShardingRefusal{Reason: "more than one Playwright test step was found, and Zoomies will not guess which to shard"}
	}
	return err
}
