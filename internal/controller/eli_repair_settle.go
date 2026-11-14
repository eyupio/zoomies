package controller

import (
	"cmp"
	"context"
	"errors"
	"time"

	"github.com/eyupio/zoomies/internal/github"
	"github.com/eyupio/zoomies/internal/store"
)

// eliSettleBatch is how many finished repairs one pass asks GitHub about. Each
// costs a read of the pull request and, when it is still open at the same
// commit, of its checks, from the quota runner registration shares; a pass runs
// every five minutes, so a list longer than this is walked a batch at a time.
const eliSettleBatch = 25

// retireSettledEliRepairs stops a finished repair asking for a person once
// there is nothing left for them to do: its pull request closed or merged,
// moved to a newer commit than the one the repair was about, or now passes its
// checks.
//
// A repair that ended without a fix was never looked at again. The page's
// Needs you list is every such repair that is the newest on its pull request,
// with no window, so one whose pull request had been closed for days went on
// heading it. Offers have been retired this way since they were added
// (retireStaleEliOffers); this is the same for the attempts.
//
// Nothing is posted on the pull request: this is the list catching up with
// GitHub, not Eli doing something.
func (c *Controller) retireSettledEliRepairs(parent context.Context) {
	ctx, cancel := context.WithTimeout(parent, 30*time.Second)
	defer cancel()
	rows, total, err := c.st.ListEliRepairsPage(ctx, store.EliRepairFilter{Group: "needs_action", Limit: eliSettleBatch, Offset: c.eliSettleOffset})
	if err != nil {
		c.log.Warn("could not list the Eli repairs that ask for a person", "error", err)
		return
	}
	// Only the repair loop reads or writes the offset. A repair that leaves the
	// list moves the ones after it up a place, so a batch can skip a few; they
	// are reached when the walk comes round again.
	if c.eliSettleOffset += len(rows); len(rows) == 0 || c.eliSettleOffset >= total {
		c.eliSettleOffset = 0
	}
	for _, r := range rows {
		if ctx.Err() != nil {
			return
		}
		if r.PullNumber <= 0 || c.githubHeld(r.InstallationID, c.Now()) {
			continue
		}
		raw, err := c.ClientFor(ctx, r.InstallationID)
		if err != nil {
			continue
		}
		client, ok := raw.(github.RepairClient)
		if !ok {
			continue
		}
		code, reason, err := eliRepairSettledOnGitHub(ctx, client, r)
		if errors.Is(err, github.ErrRateLimited) {
			c.holdRateLimited(r.InstallationID, err, c.Now(), "re-checking finished Eli repairs")
			continue
		}
		if err != nil || code == "" {
			// GitHub not answering settles nothing, and neither does a pull
			// request that still fails where the repair left it.
			continue
		}
		// What Eli found is still the record of the attempt, so it follows the
		// reason instead of being replaced by it.
		message := reason
		if r.Message != "" {
			message += " Before that: " + r.Message
		}
		if _, err := c.st.SettleEliRepair(ctx, r, code, message); err != nil {
			c.log.Warn("could not settle a finished Eli repair", "repair", r.ID, "error", err)
			return
		}
	}
}

// eliRepairSettledOnGitHub says why a finished repair no longer needs anyone,
// or nothing when it still does. The codes are the ones the attention list and
// the repair page already use for a failure that went away.
func eliRepairSettledOnGitHub(ctx context.Context, client github.RepairClient, r *store.EliRepair) (code, reason string, err error) {
	pr, err := client.RepairPull(ctx, r.Repo, r.PullNumber)
	var changed *github.RepairTargetChanged
	if errors.As(err, &changed) && changed.Code == "closed" {
		return "closed", "The pull request was closed or merged, so this repair no longer needs anyone.", nil
	}
	// A draft or a fork is not something Eli can repair, but GitHub still said
	// where its head is, which is all that is asked here.
	if errors.Is(err, github.ErrRepairUnsupported) && pr.HeadSHA != "" {
		err = nil
	}
	if err != nil {
		return "", "", err
	}
	// The commit the repair speaks for is Eli's own when it published one, and
	// otherwise the head it was asked about.
	if about := cmp.Or(r.CommitSHA, r.HeadSHA); about != "" && pr.HeadSHA != about {
		return "superseded", "The pull request has a newer commit than the one this repair was about, so it no longer needs anyone. The current commit's checks say whether another repair is wanted.", nil
	}
	state, err := client.RepairChecks(ctx, r.Repo, pr.HeadSHA)
	if err != nil {
		return "", "", err
	}
	if state == "passed" {
		return "fixed", "The pull request's checks now pass, so this repair no longer needs anyone.", nil
	}
	return "", "", nil
}
