package controller

import (
	"context"
	"errors"
	"time"

	"github.com/eyupio/zoomies/internal/github"
)

// Failure triage runs without personal Eli setup. Closing a PR or passing a
// rerun must clear the fleet's inbox even when nobody has configured repairs.
func (c *Controller) jobAttentionLoop(ctx context.Context) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	for {
		if c.mayAct() && !c.transferDraining.Load() {
			done := c.stepTimer(loopJobAttention, stepRefresh)
			c.refreshJobAttention(ctx)
			done()
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (c *Controller) refreshJobAttention(parent context.Context) {
	ctx, cancel := context.WithTimeout(parent, 20*time.Second)
	defer cancel()
	runs, err := c.st.JobAttentionRunsToCheck(ctx, 5)
	if err != nil {
		c.log.Warn("could not list the failed runs to check on GitHub", "error", err)
		return
	}
	for _, run := range runs {
		if ctx.Err() != nil {
			return
		}
		checked := c.st.Now()
		reason, known, deferred := c.jobAttentionOnGitHub(ctx, run.Repo, run.RunID)
		if deferred {
			// GitHub is refusing this installation for quota, which says
			// nothing about the run: it is not an attempt, so it stays first
			// in line for when the hold ends. Runs of other installations
			// in the batch are still checked.
			continue
		}
		if err := c.st.RecordJobAttentionRun(ctx, run.Repo, run.RunID, reason, checked, known); err != nil {
			c.log.Warn("could not record what GitHub said about a failed run", "repo", run.Repo, "run", run.RunID, "error", err)
			return
		}
	}
	if err := c.st.PruneJobAttentionRuns(ctx); err != nil {
		c.log.Warn("could not prune the records of runs checked on GitHub", "error", err)
	}
}

// jobAttentionOnGitHub says what GitHub now thinks of a failed run, and whether
// it said anything. deferred is set when it was not asked, or refused, because
// the installation is out of quota: the same quota runner registration is
// minted from, so the installation is stood down for as long as GitHub said
// (as every other background sweep stands down from one) and the run is left
// as it was.
func (c *Controller) jobAttentionOnGitHub(ctx context.Context, repo string, run int64) (reason string, known, deferred bool) {
	inst, err := c.st.FindInstallationByTarget(ctx, repo)
	if err != nil {
		return "", false, false
	}
	if c.githubHeld(inst.ID, c.Now()) {
		return "", false, true
	}
	// limited stands the installation down when err is GitHub refusing for
	// quota, and reports whether it was. Any other failure is an unknown, which
	// the caller records so that the run waits its interval.
	limited := func(err error) bool {
		if !errors.Is(err, github.ErrRateLimited) {
			return false
		}
		c.holdRateLimited(inst.ID, err, c.Now(), "re-checking failed runs")
		return true
	}
	raw, err := c.ClientFor(ctx, inst.ID)
	if err != nil {
		return "", false, limited(err)
	}
	state, err := raw.GetWorkflowRun(ctx, repo, run)
	if err != nil {
		return "", false, limited(err)
	}
	if state != nil && state.Event != "" {
		// The failure's trigger decides whether Eli is offered it at all, and
		// this is the one place every failed run is read from GitHub.
		if err := c.st.SetRunEventForRun(ctx, repo, run, state.Event); err != nil {
			c.log.Warn("could not record what started a failed run", "repo", repo, "run", run, "error", err)
		}
	}
	if state != nil && state.Status == "completed" && state.Conclusion == "success" {
		return "fixed", true, false
	}
	client, ok := raw.(github.RepairClient)
	if !ok {
		return "", false, false
	}
	pr, err := client.RepairPullForRun(ctx, repo, run)
	var changed *github.RepairTargetChanged
	if errors.As(err, &changed) {
		return changed.Code, true, false
	}
	if err != nil {
		// A push run without a PR can still have a real failure to fix.
		// Neither an unsupported repair nor a read failure proves it settled.
		return "", false, limited(err)
	}
	checks, err := client.RepairChecks(ctx, repo, pr.HeadSHA)
	if err != nil {
		return "", false, limited(err)
	}
	if checks == "passed" {
		return "fixed", true, false
	}
	// Pending or cancelled checks neither settle an unknown failure nor undo
	// an earlier observed success. A fresh failed job is visible by its time.
	return "", checks == "failed", false
}
