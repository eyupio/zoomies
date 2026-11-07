package controller

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"sort"
	"strings"
	"time"

	"github.com/eyupio/zoomies/internal/github"
	"github.com/eyupio/zoomies/internal/store"
)

// A watched pull request is read on its own schedule, never inside the model
// worker's loop. The intervals keep a green pull request cheap while a failing
// or pending one is noticed promptly.
const (
	eliWatchLease   = 2 * time.Minute
	eliWatchPending = 30 * time.Second
	eliWatchPassed  = 5 * time.Minute
	eliWatchRetry   = 2 * time.Minute
	// maxEliWatchRepairs is the whole chain, including the first attempt. The
	// count lives on the watch and survives restarts and redelivered checks.
	maxEliWatchRepairs = 3
)

func (c *Controller) repairWatchLoop(ctx context.Context) {
	ticker := time.NewTicker(3 * time.Second)
	defer ticker.Stop()
	for {
		if ctx.Err() != nil {
			return
		}
		// A watch that was observed has moved its next check on (or been paused or
		// stopped), so asking again at once finds the next due one, or none. Waiting
		// out the tick after every watch would cap the fleet at one observation
		// every three seconds, however many fell due together.
		if c.observeDueEliRepairWatch(ctx) {
			continue
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

// observeDueEliRepairWatch claims one due watch and records one observation.
// The lease is what keeps two passes, or a restarted controller, from reading
// the same pull request at once. A watch whose repository policy stopped being
// automatic is paused rather than observed, so a person's explicit choice never
// outlives the policy that authorised it.
func (c *Controller) observeDueEliRepairWatch(ctx context.Context) bool {
	if ctx.Err() != nil || !c.mayAct() || c.transferDraining.Load() {
		return false
	}
	now := c.Now()
	watch, err := c.st.ClaimDueEliRepairWatch(ctx, now, eliWatchLease)
	if err != nil {
		return false
	}
	policy, policyErr := c.repairPolicy(ctx, watch.Repo, watch.InstallationID)
	switch {
	case errors.Is(policyErr, store.ErrNotFound):
		_ = c.st.PauseEliRepairWatchLease(ctx, watch.ID, watch.LeaseToken, "The repository's repair policy is no longer enabled. Admit this watch again after enabling repairs.")
		return true
	case policyErr != nil:
		c.releaseEliRepairWatch(ctx, watch, watch.HeadSHA, watch.BaseRef, watch.BaseSHA, "unknown", "[]", now.Add(eliWatchRetry))
		return true
	case !policy.Automatic:
		_ = c.st.PauseEliRepairWatchLease(ctx, watch.ID, watch.LeaseToken, "The repository no longer repairs failed checks automatically. Admit this watch again after enabling automatic repairs.")
		return true
	}
	if _, err := c.st.GetInstallation(ctx, watch.InstallationID); errors.Is(err, store.ErrNotFound) {
		_ = c.st.PauseEliRepairWatchLease(ctx, watch.ID, watch.LeaseToken, "The GitHub installation for this watch is no longer available. Admit the watch again after reconnecting it.")
		return true
	}

	checkCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	raw, clientErr := c.ClientFor(checkCtx, watch.InstallationID)
	if clientErr != nil {
		cancel()
		c.releaseEliRepairWatch(ctx, watch, watch.HeadSHA, watch.BaseRef, watch.BaseSHA, "unknown", "[]", eliWatchRetryAt(clientErr, now))
		return true
	}
	client, ok := raw.(github.RepairClient)
	if !ok {
		cancel()
		c.releaseEliRepairWatch(ctx, watch, watch.HeadSHA, watch.BaseRef, watch.BaseSHA, "unknown", "[]", now.Add(eliWatchRetry))
		return true
	}
	pull, err := client.RepairPull(checkCtx, watch.Repo, watch.PullNumber)
	if err != nil {
		cancel()
		var changed *github.RepairTargetChanged
		if errors.As(err, &changed) && changed.Code == "closed" {
			_ = c.st.StopEliRepairWatchLease(ctx, watch.ID, watch.LeaseToken, "The pull request was closed or merged, so watching stopped.")
			return true
		}
		if errors.Is(err, github.ErrRepairUnsupported) {
			_ = c.st.StopEliRepairWatchLease(ctx, watch.ID, watch.LeaseToken, "The pull request is no longer open, writable and on a branch Eli may update, so watching stopped.")
			return true
		}
		c.releaseEliRepairWatch(ctx, watch, watch.HeadSHA, watch.BaseRef, watch.BaseSHA, "unknown", "[]", eliWatchRetryAt(err, now))
		return true
	}

	// conflictEvidence stays empty when the read failed, so that a transient
	// error leaves the last good reading in place instead of overwriting it with
	// "nothing is known".
	conflictEvidence := ""
	conflictRead := false
	var conflict github.RepairConflict
	if conflictClient, ok := client.(github.RepairConflictClient); ok {
		read, conflictErr := conflictClient.RepairConflictEvidence(checkCtx, watch.Repo, watch.PullNumber)
		if conflictErr == nil {
			conflict = read
			conflictRead = true
			encoded, _ := json.Marshal(conflict)
			conflictEvidence = string(encoded)
		}
	}
	var checkState string
	var checkEvidence string
	var next time.Time
	if watch.Checks {
		if evidenceClient, ok := client.(github.RepairEvidenceClient); ok {
			evidence, evidenceErr := evidenceClient.RepairCheckEvidence(checkCtx, watch.Repo, pull.HeadSHA)
			if evidenceErr != nil {
				checkState, checkEvidence, next = "unknown", "[]", eliWatchRetryAt(evidenceErr, now)
			} else {
				encoded, _ := json.Marshal(evidence)
				checkState, checkEvidence = evidence.State, string(encoded)
				switch checkState {
				case "passed":
					next = now.Add(eliWatchPassed)
				case "failed", "waiting":
					next = now.Add(eliWatchPending)
				default:
					checkState, checkEvidence, next = "unknown", "[]", now.Add(eliWatchRetry)
				}
			}
		} else {
			checkState, err = client.RepairChecks(checkCtx, watch.Repo, pull.HeadSHA)
			checkEvidence = "[]"
			if err != nil {
				checkState, next = "unknown", eliWatchRetryAt(err, now)
			} else {
				switch checkState {
				case "passed":
					next = now.Add(eliWatchPassed)
				case "failed", "waiting":
					next = now.Add(eliWatchPending)
				default:
					checkState, next = "unknown", now.Add(eliWatchRetry)
				}
			}
		}
	} else {
		// A watch that follows only conflicts has no checks to wait on, so it
		// is read at the pace of a pull request whose checks passed. Mergeability
		// that GitHub is still working out, or a read that failed, is looked at
		// sooner.
		checkState, checkEvidence = "unknown", "[]"
		switch {
		case !conflictRead:
			next = now.Add(eliWatchRetry)
		case !conflict.MergeableKnown:
			next = now.Add(eliWatchPending)
		default:
			next = now.Add(eliWatchPassed)
		}
	}
	// The limit is the store's to apply, not this gate's: it is the store that
	// records why the chain stopped, and it can tell the last attempt still working
	// on this head from a new failing head that no attempt is left for.
	if watch.Checks && checkState == "failed" && watch.ChainStopped == "" {
		run, runErr := client.RepairFailedJobs(checkCtx, watch.Repo, 0, pull.HeadSHA)
		if runErr == nil {
			repair := &store.EliRepair{
				InstallationID: watch.InstallationID,
				Repo:           watch.Repo,
				PullNumber:     watch.PullNumber,
				HeadSHA:        pull.HeadSHA,
				ProviderID:     policy.ProviderID,
				Trigger:        "automatic",
				Instruction:    "Diagnose the failed checks on this watched pull request and fix the underlying code issue.",
				DedupKey:       "watch:" + watch.ID + ":" + pull.HeadSHA,
			}
			added, attempts, enqueueErr := c.st.EnqueueEliWatchRepair(ctx, watch.ID, repair, eliWatchFailureSignature(run), maxEliWatchRepairs,
				"Eli has made three repair attempts for this watched pull request. A person needs to look at the remaining failure.")
			switch {
			case enqueueErr != nil && !errors.Is(enqueueErr, store.ErrConflict):
				c.log.Warn("could not start a repair for a watched pull request", "watch", watch.ID, "error", enqueueErr)
			case added:
				c.log.Info("started a repair for a watched pull request", "watch", watch.ID, "repair", repair.ID, "attempt", attempts)
			}
		}
	}
	cancel()

	if ctx.Err() != nil || !c.mayAct() || c.transferDraining.Load() {
		// The lease expires on its own, and a restart makes the watch due
		// again. Do not record a reading taken before a gate closed.
		return true
	}
	if checkState == "passed" && (watch.ChainAttempts > 0 || watch.ChainStopped != "") {
		if err := c.st.ResetEliWatchRepairChain(ctx, watch.ID); err != nil {
			c.log.Warn("could not reset an Eli repair watch chain", "watch", watch.ID, "error", err)
		}
	}
	if conflictRead {
		if err := c.st.SetEliRepairWatchConflict(ctx, watch.ID, watch.LeaseToken, conflictEvidence); err != nil && !errors.Is(err, store.ErrConflict) {
			c.log.Warn("could not record an Eli conflict check", "watch", watch.ID, "error", err)
		}
	}
	conflictDue := watch.Conflicts && conflict.MergeableKnown && !conflict.Mergeable && conflict.MergeBaseSHA != "" &&
		conflict.HeadSHA == pull.HeadSHA && conflict.BaseSHA == pull.BaseSHA
	if conflictDue {
		key := conflictKey(watch.ID, conflict.HeadSHA, conflict.BaseSHA, conflict.MergeBaseSHA)
		if c.claimConflict(key, now.Add(conflictClaimWindow)) {
			c.goTracked(ctx, "conflict-start", func(runCtx context.Context) {
				c.startConflictRepair(runCtx, key, raw, watch, pull, conflict)
			})
			next = laterOf(next, now.Add(2*time.Minute))
		} else if c.conflictClaimed(key) {
			// A worker or the model is already carrying this incident. Keep the
			// watch checking, but do not start a second one.
			next = laterOf(next, now.Add(2*time.Minute))
		}
	}
	c.releaseEliRepairWatch(ctx, watch, pull.HeadSHA, pull.Base, pull.BaseSHA, checkState, checkEvidence, next)
	return true
}

func laterOf(a, b time.Time) time.Time {
	if a.After(b) {
		return a
	}
	return b
}

func (c *Controller) repairWatchEnabled(ctx context.Context, r *store.EliRepair) bool {
	if r.WatchID == "" {
		return true
	}
	watch, err := c.st.EliRepairWatch(ctx, r.WatchID)
	return err == nil && watch.Enabled
}

func eliWatchFailureSignature(run *github.RepairFailureRun) string {
	if run == nil || len(run.Jobs) == 0 {
		return ""
	}
	parts := make([]string, 0, len(run.Jobs))
	for _, job := range run.Jobs {
		parts = append(parts, job.Name+"="+job.Conclusion)
	}
	sort.Strings(parts)
	sum := sha256.Sum256([]byte(strings.Join(parts, "\n")))
	return hex.EncodeToString(sum[:])
}

func (c *Controller) releaseEliRepairWatch(ctx context.Context, watch *store.EliRepairWatch, headSHA, baseRef, baseSHA, checkState, checkEvidence string, next time.Time) {
	if err := c.st.ReleaseEliRepairWatch(ctx, watch.ID, watch.LeaseToken, headSHA, baseRef, baseSHA, checkState, checkEvidence, next); err != nil && !errors.Is(err, store.ErrConflict) {
		c.log.Warn("could not record an Eli repair watch observation", "watch", watch.ID, "error", err)
	}
}

func eliWatchRetryAt(err error, now time.Time) time.Time {
	if at, ok := github.RetryAfterRateLimit(err, now); ok {
		return at
	}
	return now.Add(eliWatchRetry)
}
