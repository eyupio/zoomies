package controller

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/eyupio/zoomies/internal/github"
	"github.com/eyupio/zoomies/internal/store"
)

// admitWatchFromPullRequestWebhook wakes repository discovery when GitHub says
// a PR was opened or reopened. It only acts where a person already made the
// explicit repository-wide decision; a per-PR watch never creates one.
func (c *Controller) admitWatchFromPullRequestWebhook(ctx context.Context, inst *store.Installation, body []byte) error {
	if inst == nil {
		return nil
	}
	var p struct {
		Action string `json:"action"`
		Number int    `json:"number"`
		Repo   struct {
			FullName string `json:"full_name"`
		} `json:"repository"`
	}
	if err := json.Unmarshal(body, &p); err != nil {
		return errMalformedDelivery
	}
	if p.Action != "opened" && p.Action != "reopened" {
		return nil
	}
	repo := p.Repo.FullName
	if repo == "" || p.Number <= 0 {
		return nil
	}
	subscription, err := c.st.EliRepairWatchRepositoryFor(ctx, repo, inst.ID)
	if errors.Is(err, store.ErrNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	policy, err := c.eliWatchPolicy(ctx, repo, inst.ID)
	if err != nil {
		return nil
	}
	admissionCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	raw, err := c.ClientFor(admissionCtx, inst.ID)
	if err != nil {
		return err
	}
	client, ok := raw.(github.RepairClient)
	if !ok {
		return nil
	}
	pull, err := client.RepairPull(admissionCtx, repo, p.Number)
	if err != nil {
		var changed *github.RepairTargetChanged
		if errors.As(err, &changed) && changed.Code == "closed" || errors.Is(err, github.ErrRepairUnsupported) {
			return nil
		}
		return err
	}
	if watchExclusionReason(pull) != "" {
		return nil
	}
	_, _, err = c.st.CreateEliRepairWatchIfAbsent(ctx, store.EliRepairWatch{
		InstallationID:   inst.ID,
		Repo:             repo,
		PullNumber:       p.Number,
		OwnerID:          subscription.OwnerID,
		PolicyProviderID: policy.ProviderID,
		PolicyUpdatedAt:  policy.UpdatedAt,
		Checks:           subscription.Checks,
		Conflicts:        subscription.Conflicts,
		HeadSHA:          pull.HeadSHA,
		BaseRef:          pull.Base,
		BaseSHA:          pull.BaseSHA,
	})
	return err
}
