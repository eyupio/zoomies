package controller

import (
	"context"

	"github.com/eyupio/zoomies/internal/store"
)

func (c *Controller) confirmCleanup(ctx context.Context, id string, host bool) error {
	completed, err := c.st.ConfirmRunnerCleanup(ctx, id, host)
	if err != nil {
		c.log.Warn("could not record confirmed cleanup", "runner", id, "host_confirmation", host, "error", err)
		return err
	}
	r, err := c.st.GetRunner(ctx, id)
	if err != nil {
		return err
	}
	if completed && r.FinishedAt != nil && r.CleanedUpAt != nil {
		if p, err := c.st.GetPool(ctx, r.PoolID); err == nil && !IsDemoID(p.InstallationID) {
			observeDuration(c.metrics.cleanupDuration, p.Name, string(p.Backend), *r.FinishedAt, *r.CleanedUpAt)
		}
	}
	c.publishRunnerByID(ctx, id)
	return nil
}

// Observe the job's actual runner once the association is known, not the
// first queued job found when any runner in its pool was created. Prewarmed
// runners and jobs whose eligibility was never observed have no sample.
func (c *Controller) observeScheduling(ctx context.Context, j *store.Job, change store.JobChange, r *store.Runner) {
	firstStart := (j.State == store.JobInProgress || j.State == store.JobCompleted) &&
		(change.RunnerLinked || ((change.Created || change.StateChanged) &&
			change.PreviousState != store.JobInProgress && change.PreviousState != store.JobCompleted))
	if !firstStart || j.EligibleAt == nil || j.RunnerID == "" || IsDemoID(j.InstallationID) {
		return
	}
	if r == nil {
		var err error
		r, err = c.st.GetRunner(ctx, j.RunnerID)
		if err != nil {
			return
		}
	}
	if r.CreateTaskIssuedAt == nil {
		return
	}
	if p, err := c.st.GetPool(ctx, r.PoolID); err == nil && p.InstallationID == j.InstallationID {
		observeDuration(c.metrics.schedulingLatency, p.Name, string(p.Backend), *j.EligibleAt, *r.CreateTaskIssuedAt)
	}
}
