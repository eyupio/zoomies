package controller

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/eyupio/zoomies/internal/github"
	"github.com/eyupio/zoomies/internal/scheduler"
	"github.com/eyupio/zoomies/internal/store"
)

// rateLimitBackoff is how long the poller stands down after GitHub says the
// installation is out of quota. Polling is the fallback path; spending the
// last of an installation's quota on it would take the webhook path's own API
// calls -- minting JIT configs -- down with it.
const rateLimitBackoff = 15 * time.Minute

// pollLoop is the webhook fallback.
//
// A controller behind NAT, or one whose webhook secret was mistyped, would
// otherwise stop scaling silently, and a fleet that has quietly stopped
// scaling looks exactly like a quiet fleet.
func (c *Controller) pollLoop(ctx context.Context) {
	if !c.cfg().GitHub.PollFallback {
		c.log.Info("the fallback poller is off; scaling depends entirely on webhooks reaching this controller")
		return
	}
	interval := c.pollInterval()
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			c.pollOnce(ctx)
			c.polls.Add(1)
		case <-c.settingsChanged:
			// github.poll_interval is a runtime setting. The timer was built
			// from the old value, so it is rebuilt here rather than left to
			// fire once more on the old interval.
		}
		if d := c.pollInterval(); d != interval {
			interval = d
			ticker.Reset(d)
		}
	}
}

func (c *Controller) pollInterval() time.Duration {
	if d := c.cfg().GitHub.PollInterval; d > 0 {
		return d
	}
	return 30 * time.Second
}

// pollOnce lists queued jobs for every installation and records what it finds.
//
// It skips entirely when a webhook has arrived within the last two poll
// intervals. That single check is what makes the fallback cheap: on a working
// installation the poller costs one query against the local database per
// interval and no GitHub calls at all, so leaving it on by default does not
// spend an organisation's API quota.
func (c *Controller) pollOnce(ctx context.Context) {
	now := c.Now()

	// Accepted deliveries only: one that was rejected recorded a job for
	// nobody, and a run of them is the mistyped-secret case this poller is
	// the safety net for.
	//
	// The fleet-wide answer is only used to say whether a webhook has ever
	// arrived, which the Overview reports about the deployment rather than
	// about any one installation. What to poll is decided per installation
	// below.
	last, err := c.st.LastAcceptedDeliveryAt(ctx)
	if err != nil {
		c.log.Error("could not tell when the last webhook arrived", "error", err)
		return
	}
	c.pollingOnly.Store(last.IsZero())

	fresh, err := c.st.LastAcceptedDeliveryByInstallation(ctx)
	if err != nil {
		c.log.Error("could not tell which installations webhooks are arriving for", "error", err)
		return
	}

	insts, err := c.st.ListInstallations(ctx)
	if err != nil {
		c.log.Error("could not list installations to poll", "error", err)
		return
	}

	found, changed := 0, 0
	for _, inst := range insts {
		if ctx.Err() != nil {
			return
		}
		if IsDemoID(inst.ID) {
			// The demo fixtures are already in the database; there is nothing
			// to poll for, and trying would log a credential failure on every
			// tick of a demo instance.
			continue
		}
		if c.pollHeld(inst.ID, now) {
			continue
		}
		// An installation whose webhooks are arriving costs nothing: this is
		// what makes leaving the poller on by default defensible, and asking
		// it per installation is what stops a working organisation's
		// deliveries from covering for a silent one on the same controller.
		if at, ok := fresh[inst.ID]; ok && now.Sub(at) < 2*c.pollInterval() {
			continue
		}
		client, err := c.clients.get(ctx, inst)
		if err != nil {
			c.log.Warn("skipping an installation while polling", "installation", inst.ID, "error", err)
			continue
		}
		jobs, err := client.ListQueuedJobs(ctx)
		c.observeGitHub(inst.ID, err)
		if err != nil {
			if errors.Is(err, github.ErrRateLimited) {
				// Hold this installation only. Its quota is its own, and the
				// sweep abandoning the rest would let one organisation out of
				// quota stop every other one from scaling.
				c.holdPolling(inst.ID, now.Add(rateLimitBackoff))
				c.log.Warn("GitHub rate-limited the fallback poller for an installation; standing down for it",
					"installation", inst.ID, "backoff", rateLimitBackoff, "error", err)
				continue
			}
			c.log.Warn("could not poll for queued jobs", "installation", inst.ID, "error", err)
			continue
		}
		found += len(jobs)
		n, err := c.ingestQueuedJobs(ctx, inst, jobs)
		if err != nil {
			c.log.Error("could not record polled jobs", "installation", inst.ID, "error", err)
			continue
		}
		changed += n
	}

	if found > 0 {
		c.log.Debug("polled GitHub for queued jobs", "found", found, "new_or_changed", changed)
	}
	if changed > 0 {
		c.Nudge()
	}
}

// ingestQueuedJobs folds polled jobs into the same rows the webhook path
// writes, so both paths feed one scheduler and one history.
//
// A job is attributed to the installation covering its repository, resolved
// the same way the webhook path resolves it, rather than to the installation
// this poll happened to come from. The two are usually the same and are not
// always: an organisation installation and a repository one can both cover a
// repository, and if each path picked its own answer the job's installation
// would flip with every delivery. The polled installation is the fallback,
// because it did report the job.
func (c *Controller) ingestQueuedJobs(ctx context.Context, polled *store.Installation, jobs []github.QueuedJob) (int, error) {
	if len(jobs) == 0 {
		return 0, nil
	}
	pools, err := c.st.ListPools(ctx)
	if err != nil {
		return 0, fmt.Errorf("listing pools to match polled jobs: %w", err)
	}

	changed := 0
	for _, q := range jobs {
		job := &store.Job{
			GitHubJobID:    q.ID,
			GitHubRunID:    q.RunID,
			Repo:           q.Repo,
			Workflow:       q.WorkflowName,
			JobName:        q.JobName,
			Labels:         store.NormalizeLabels(q.Labels),
			State:          store.JobQueued,
			InstallationID: c.owningInstallation(ctx, q.Repo, polled),
			RunnerName:     q.RunnerName,
			HTMLURL:        q.HTMLURL,
			QueuedAt:       q.QueuedAt,
		}
		if job.QueuedAt.IsZero() {
			job.QueuedAt = c.Now()
		}
		if p := scheduler.BestPool(pools, job); p != nil {
			job.PoolID = p.ID
			job.Matched = true
		}
		saved, change, err := c.st.ApplyJob(ctx, job)
		if err != nil {
			c.log.Warn("could not record a polled job", "github_job_id", q.ID, "error", err)
			continue
		}
		c.recordJobChange(ctx, saved, change, sourcePoller, nil)
		if saved.State == store.JobQueued {
			changed++
		}
		c.publishJob(ctx, saved)
	}
	return changed, nil
}

// pollHeld reports whether an installation is inside its rate-limit backoff,
// clearing the hold once it has expired so the map does not grow with the
// installations that have long since recovered.
func (c *Controller) pollHeld(id string, now time.Time) bool {
	c.pollMu.Lock()
	defer c.pollMu.Unlock()
	until, ok := c.pollPaused[id]
	if !ok {
		return false
	}
	if !now.Before(until) {
		delete(c.pollPaused, id)
		return false
	}
	return true
}

// holdPolling stands the poller down for one installation until a moment.
func (c *Controller) holdPolling(id string, until time.Time) {
	c.pollMu.Lock()
	defer c.pollMu.Unlock()
	if c.pollPaused == nil {
		c.pollPaused = map[string]time.Time{}
	}
	c.pollPaused[id] = until
}

// owningInstallation is the installation covering a repository, with a
// fallback for the caller that already knows one. A lookup failure is not
// worth abandoning an ingest over: the poller's own installation is a worse
// answer than the repository's and a much better one than none.
func (c *Controller) owningInstallation(ctx context.Context, repo string, fallback *store.Installation) string {
	if owner, err := c.st.FindInstallationByTarget(ctx, repo); err == nil {
		return owner.ID
	} else if !errors.Is(err, store.ErrNotFound) {
		c.log.Warn("could not resolve the installation covering a polled repository",
			"repo", repo, "error", err)
	}
	return installationID(fallback)
}
