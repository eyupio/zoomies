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

// The two things a person may ask a durable watch to keep up with. They are
// capabilities rather than booleans in the request so a mix is explicit and a
// new one cannot silently become a contradictory combination of flags.
const (
	EliWatchChecks    = "checks"
	EliWatchConflicts = "conflicts"
)

// EliWatchAdmission is a person's explicit request to keep following one pull
// request. It is not inferred from an existing policy or a webhook: those keep
// their one-attempt meaning until somebody asks for continuous watching.
type EliWatchAdmission struct {
	Repo           string   `json:"repo"`
	PullNumber     int      `json:"pull_number"`
	InstallationID string   `json:"installation_id"`
	OwnerID        string   `json:"owner_id"`
	Capabilities   []string `json:"capabilities"`
}

func eliWatchCapabilities(caps []string) (checks, conflicts bool, err error) {
	seen := map[string]bool{}
	for _, capability := range caps {
		capability = strings.TrimSpace(strings.ToLower(capability))
		if capability == "" || seen[capability] {
			continue
		}
		seen[capability] = true
		switch capability {
		case EliWatchChecks:
			checks = true
		case EliWatchConflicts:
			conflicts = true
		default:
			return false, false, fmt.Errorf("unknown watch capability %q; choose failed checks, merge conflicts, or both", capability)
		}
	}
	if !checks && !conflicts {
		return false, false, errors.New("choose at least one thing to watch: failed checks or merge conflicts")
	}
	if conflicts {
		return false, false, errors.New("merge-conflict watching is not available yet; only failed-check watching can be admitted")
	}
	if !checks {
		return false, false, errors.New("choose at least one thing to watch: failed checks or merge conflicts")
	}
	return checks, conflicts, nil
}

// AdmitEliRepairWatch records a watch only after checking the repository's
// current policy, the installation's identity and the pull request itself. It
// is deliberately explicit: an existing Automatic policy, a failed job or a
// controller restart can never turn itself into continuous watching.
//
// The pull request is read now, so the watch starts from a head and base that
// were true when the person asked. A later slice observes those identities and
// decides whether anything needs repairing; this one only keeps the decision.
func (c *Controller) AdmitEliRepairWatch(ctx context.Context, in EliWatchAdmission) (*store.EliRepairWatch, error) {
	repo := normaliseRepairRepo(in.Repo)
	if repo == "" || in.PullNumber <= 0 {
		return nil, errors.New("choose an open pull request to watch")
	}
	if strings.TrimSpace(in.OwnerID) == "" {
		return nil, errors.New("a durable watch needs the person who authorised it")
	}
	checks, conflicts, err := eliWatchCapabilities(in.Capabilities)
	if err != nil {
		return nil, err
	}
	policy, err := c.repairPolicy(ctx, repo, in.InstallationID)
	if errors.Is(err, store.ErrNotFound) {
		return nil, errors.New("repairs are not enabled for this repository and installation; enable Eli repairs and choose an installation provider first")
	}
	if err != nil {
		return nil, err
	}
	if !policy.Automatic {
		return nil, errors.New("continuous watching needs automatic repairs for this repository; ask me first and manual only keep their one-attempt behaviour")
	}
	canPay, err := c.automaticProviderCanPay(ctx, *policy)
	if err != nil {
		return nil, err
	}
	if !canPay {
		return nil, errors.New("the installation provider for this repository is missing, disabled or belongs to a person, so it cannot pay for unattended watching")
	}
	if _, err := c.st.GetInstallation(ctx, in.InstallationID); err != nil {
		return nil, fmt.Errorf("the GitHub installation for this repository is unavailable: %w", err)
	}
	admissionCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	raw, err := c.ClientFor(admissionCtx, in.InstallationID)
	if err != nil {
		return nil, fmt.Errorf("the GitHub installation is unavailable: %w", err)
	}
	client, ok := raw.(github.RepairClient)
	if !ok {
		return nil, errors.New("this GitHub client does not support pull request watching")
	}
	pull, err := client.RepairPull(admissionCtx, repo, in.PullNumber)
	if err != nil {
		return nil, fmt.Errorf("the pull request cannot be watched: %w", err)
	}
	if pull.Number != in.PullNumber {
		return nil, errors.New("GitHub returned a different pull request from the one that was chosen")
	}
	return c.st.AdmitEliRepairWatch(ctx, store.EliRepairWatch{
		InstallationID:   in.InstallationID,
		Repo:             repo,
		PullNumber:       in.PullNumber,
		OwnerID:          strings.TrimSpace(in.OwnerID),
		PolicyProviderID: policy.ProviderID,
		PolicyUpdatedAt:  policy.UpdatedAt,
		Checks:           checks,
		Conflicts:        conflicts,
		HeadSHA:          pull.HeadSHA,
		BaseRef:          pull.Base,
		BaseSHA:          pull.BaseSHA,
	})
}

// Pausing one pull request wins over the repository's automation until an
// explicit admission resumes it. The reason is kept so a later page can say why
// it stopped without guessing from the policy.
func (c *Controller) PauseEliRepairWatch(ctx context.Context, id, reason string) error {
	if strings.TrimSpace(reason) == "" {
		reason = "Watching was paused for this pull request."
	}
	return c.st.PauseEliRepairWatch(ctx, id, reason)
}

// Stopping is the terminal form of pausing: the watch keeps its history but no
// admission can resume it without a new explicit request.
func (c *Controller) StopEliRepairWatch(ctx context.Context, id, reason string) error {
	if strings.TrimSpace(reason) == "" {
		reason = "Watching was stopped for this pull request."
	}
	return c.st.StopEliRepairWatch(ctx, id, reason)
}
