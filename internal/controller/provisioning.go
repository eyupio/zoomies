package controller

import (
	"context"
	"errors"

	"github.com/eyupio/zoomies/internal/store"
)

func (c *Controller) ControlProvisioning(ctx context.Context, ids []string, action string) ([]store.ProvisioningResult, error) {
	// Serialize with snapshot AND apply, so an old plan cannot create runners
	// after a pause has been acknowledged. Already-issued tasks are unaffected.
	c.reconcileMu.Lock()
	defer c.reconcileMu.Unlock()
	results, err := c.st.ControlProvisioning(ctx, ids, action)
	if err != nil {
		return nil, err
	}
	for _, result := range results {
		if result.OK {
			if j, err := c.st.GetJob(ctx, result.ID); err == nil {
				c.publishJob(ctx, j)
			}
		}
	}
	c.Nudge()
	return results, nil
}

// ErrRunHasNoQueuedJobs is a run-level provisioning action on a run none of
// whose jobs is waiting for a runner here: every job has started or finished,
// or the ones still queued were removed from the queue and are restored from
// the Queue page rather than swept back in with their siblings.
var ErrRunHasNoQueuedJobs = errors.New("no job of this workflow run is queued for this fleet")

// runProvisioningStates is what a run-level action reaches: the run's queued
// jobs in every provisioning state but removed. See RunJobCounts.Controllable.
var runProvisioningStates = []string{"ready", "expedited", store.ProvisioningPaused}

// ControlWorkflowRunProvisioning is ControlProvisioning for every queued job
// of one run: what the Workflows page's run rows call, since a run has no ID
// of its own to select by, only repo + GitHub's run ID.
//
// store.ErrNotFound for a run this fleet has never heard of, and
// ErrRunHasNoQueuedJobs when it knows the run but nothing in it is waiting
// for a runner here -- the two are different answers for an operator, and a
// 200 with no results would read as a button that did nothing.
func (c *Controller) ControlWorkflowRunProvisioning(ctx context.Context, repo string, runID int64, action string) ([]store.ProvisioningResult, error) {
	jobs, err := c.st.ListJobsForRun(ctx, repo, runID)
	if err != nil {
		return nil, err
	}
	if len(jobs) == 0 {
		return nil, store.ErrNotFound
	}
	ids, err := c.st.ProvisioningSelection(ctx, store.JobFilter{
		Repos:        []string{repo},
		RunIDs:       []int64{runID},
		Provisioning: runProvisioningStates,
	})
	if err != nil {
		return nil, err
	}
	if len(ids) == 0 {
		return nil, ErrRunHasNoQueuedJobs
	}
	return c.ControlProvisioning(ctx, ids, action)
}
