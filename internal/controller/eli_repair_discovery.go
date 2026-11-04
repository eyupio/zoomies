package controller

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/eyupio/zoomies/internal/github"
	"github.com/eyupio/zoomies/internal/store"
)

// EliRepositoryWatchAdmission is the explicit request to follow the open pull
// requests a repository already has. It is the backfill beside admitting one
// named pull request; neither is inferred from a policy or a webhook.
type EliRepositoryWatchAdmission struct {
	Repo           string   `json:"repo"`
	InstallationID string   `json:"installation_id"`
	OwnerID        string   `json:"owner_id"`
	Capabilities   []string `json:"capabilities"`
}

// EliWatchExclusion says why one open pull request was not admitted, so a fork,
// a draft or a per-PR pause is visible rather than silently absent.
type EliWatchExclusion struct {
	PullNumber int    `json:"pull_number"`
	Title      string `json:"title,omitempty"`
	Reason     string `json:"reason"`
}

// EliRepositoryWatchReport is what one repository-wide admission did.
type EliRepositoryWatchReport struct {
	Admitted []*store.EliRepairWatch `json:"admitted"`
	Excluded []EliWatchExclusion     `json:"excluded"`
	// Existing counts pull requests already watched and left as they were.
	Existing int  `json:"existing"`
	Capped   bool `json:"capped"`
}

// AdmitEliRepositoryWatches enumerates the repository's open pull requests once
// and admits a watch for each eligible one. The list is read before any write,
// so a failed page admits nothing. A watch that already exists is never resumed
// here: an explicit admission of the repository must not undo a person's pause
// on one pull request.
func (c *Controller) AdmitEliRepositoryWatches(ctx context.Context, in EliRepositoryWatchAdmission) (*EliRepositoryWatchReport, error) {
	repo := normaliseRepairRepo(in.Repo)
	if repo == "" {
		return nil, errors.New("choose a repository to watch")
	}
	if strings.TrimSpace(in.OwnerID) == "" {
		return nil, errors.New("a durable watch needs the person who authorised it")
	}
	checks, conflicts, err := eliWatchCapabilities(in.Capabilities)
	if err != nil {
		return nil, err
	}
	policy, err := c.eliWatchPolicy(ctx, repo, in.InstallationID)
	if err != nil {
		return nil, err
	}
	discoveryCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	raw, err := c.ClientFor(discoveryCtx, in.InstallationID)
	if err != nil {
		return nil, fmt.Errorf("the GitHub installation is unavailable: %w", err)
	}
	discovery, ok := raw.(github.RepairDiscoveryClient)
	if !ok {
		return nil, errors.New("this GitHub client does not support listing pull requests for watching")
	}
	pulls, capped, err := discovery.RepairOpenPulls(discoveryCtx, repo, github.RepairPullDiscoveryLimit)
	if err != nil {
		return nil, fmt.Errorf("the repository's open pull requests cannot be listed: %w", err)
	}

	report := &EliRepositoryWatchReport{Excluded: []EliWatchExclusion{}, Capped: capped}
	for _, pull := range pulls {
		reason := watchExclusionReason(pull)
		if reason != "" {
			report.Excluded = append(report.Excluded, EliWatchExclusion{PullNumber: pull.Number, Title: pull.Title, Reason: reason})
			continue
		}
		watch, created, err := c.st.CreateEliRepairWatchIfAbsent(ctx, store.EliRepairWatch{
			InstallationID:   in.InstallationID,
			Repo:             repo,
			PullNumber:       pull.Number,
			OwnerID:          strings.TrimSpace(in.OwnerID),
			PolicyProviderID: policy.ProviderID,
			PolicyUpdatedAt:  policy.UpdatedAt,
			Checks:           checks,
			Conflicts:        conflicts,
			HeadSHA:          pull.HeadSHA,
			BaseRef:          pull.Base,
			BaseSHA:          pull.BaseSHA,
		})
		if err != nil {
			return nil, err
		}
		if created {
			report.Admitted = append(report.Admitted, watch)
			continue
		}
		if watch.Enabled {
			report.Existing++
			continue
		}
		reason = watch.PausedReason
		if reason == "" {
			reason = watch.StopReason
		}
		if reason == "" {
			reason = "This pull request has a watch that is not active."
		}
		report.Excluded = append(report.Excluded, EliWatchExclusion{PullNumber: pull.Number, Title: pull.Title, Reason: reason})
	}
	return report, nil
}

func watchExclusionReason(pull github.RepairPull) string {
	switch {
	case !pull.Open:
		return "The pull request is not open."
	case pull.Fork:
		return "The pull request comes from a fork, which Eli cannot write to."
	case pull.Draft:
		return "The pull request is a draft. Mark it ready for review before watching it."
	case pull.HeadSHA == "" || pull.Branch == "" || pull.Branch == pull.DefaultBranch:
		return "The pull request has no writable head branch."
	}
	return ""
}
