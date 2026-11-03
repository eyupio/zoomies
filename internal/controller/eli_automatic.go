package controller

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/eyupio/zoomies/internal/store"
)

// EliAutomaticRepository is one repository that repairs its own failed checks,
// with what an administrator needs to trust that or stop it: who pays, whether
// they can, and what the automation did
// last. It is an administrator's view; automatic repairs have no owner, so an
// ordinary user cannot read any of them.
type EliAutomaticRepository struct {
	Repo           string `json:"repo"`
	InstallationID string `json:"installation_id"`
	ProviderID     string `json:"provider_id"`
	// ProviderName and Model are empty when the policy has no provider, which is
	// what deleting a provider leaves behind.
	ProviderName string `json:"provider_name"`
	Model        string `json:"model"`
	// Problem is present only when automatic repairs here cannot run, in a
	// sentence that says what to do about it.
	Problem        string                  `json:"problem,omitempty"`
	AllowWorkflows bool                    `json:"allow_workflows"`
	Running        int                     `json:"running"`
	Last           *store.EliAutomaticLast `json:"last,omitempty"`
}

// EliAutomaticRepairs lists the repositories whose policy is enabled and
// Automatic, the ones with a problem first and then by name without regard to
// case. A provider that cannot pay is reported here for the same reasons the
// worker refuses it, so the administrator reads it before the next failure is
// spent on it rather than after.
func (c *Controller) EliAutomaticRepairs(ctx context.Context) ([]EliAutomaticRepository, error) {
	policies, err := c.st.EliRepairPolicies(ctx)
	if err != nil {
		return nil, err
	}
	var automatic []store.EliRepairPolicy
	var repos []string
	for _, p := range policies {
		if p.Enabled && p.Automatic {
			automatic = append(automatic, p)
			repos = append(repos, p.Repo)
		}
	}
	out := []EliAutomaticRepository{}
	if len(automatic) == 0 {
		return out, nil
	}
	activity, err := c.st.EliAutomaticActivity(ctx, repos)
	if err != nil {
		return nil, err
	}
	for _, p := range automatic {
		entry := EliAutomaticRepository{
			Repo: p.Repo, InstallationID: p.InstallationID, ProviderID: p.ProviderID,
			AllowWorkflows: p.AllowWorkflows,
			Running:        activity[p.Repo].Running, Last: activity[p.Repo].Last,
		}
		if p.ProviderID == "" {
			entry.Problem = "No installation provider is set for this repository, so automatic repairs cannot start. Choose one in setup."
		} else {
			provider, err := c.st.GetAssistantProvider(ctx, p.ProviderID)
			switch {
			case errors.Is(err, store.ErrNotFound):
				entry.Problem = "The installation provider for this repository is missing, so automatic repairs cannot start. Choose one in setup."
			case err != nil:
				return nil, err
			default:
				entry.ProviderName, entry.Model = provider.Name, provider.Model
				switch {
				case provider.OwnerID != "":
					entry.Problem = "The provider chosen for this repository belongs to a person, and automatic repairs need an installation provider. Choose one in setup."
				case !provider.Enabled:
					entry.Problem = fmt.Sprintf("The installation provider %q is disabled, so automatic repairs cannot start. Enable it or choose another in setup.", provider.Name)
				}
			}
		}
		out = append(out, entry)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if broken := out[i].Problem != ""; broken != (out[j].Problem != "") {
			return broken
		}
		a, b := strings.ToLower(out[i].Repo), strings.ToLower(out[j].Repo)
		if a != b {
			return a < b
		}
		return out[i].Repo < out[j].Repo
	})
	return out, nil
}

// What a repository does once its automatic repairs are stopped. Ask me first
// hands the decision back to a person, with the repository's provider, allowance
// and workflow setting kept; off clears the policy's switch and nothing else, so
// the same switch in setup turns Automatic back on as it was.
const (
	EliStopAskFirst = "ask_first"
	EliStopOff      = "off"
)

// EliAutomaticStop is what happened to one repository in a stop.
type EliAutomaticStop struct {
	Repo string `json:"repo"`
	// Changed is false for a repository that was not automatic any more, so a
	// second press or a second administrator is not an error.
	Changed bool   `json:"changed"`
	Error   string `json:"error,omitempty"`
}

// StopEliAutomatic stops automatic repairs on the named repositories, once each,
// in the order given. It only relaxes a policy: it never enables a repository,
// raises a limit or changes a provider, and it does not look the provider up,
// because the day an administrator needs this is the day the provider is the
// problem. A repair already queued is blocked by the worker when it reaches it,
// and one that has not yet pushed stops before it does (see runEliRepair), so the
// stop needs no second write.
//
// One repository failing to save is that repository's result and does not stop
// the rest. An unknown choice is an error, so the API validates first.
func (c *Controller) StopEliAutomatic(ctx context.Context, repos []string, then string) ([]EliAutomaticStop, error) {
	if then != EliStopAskFirst && then != EliStopOff {
		return nil, fmt.Errorf("stopping automatic repairs: %q is neither %s nor %s", then, EliStopAskFirst, EliStopOff)
	}
	policies, err := c.st.EliRepairPolicies(ctx)
	if err != nil {
		return nil, err
	}
	byRepo := make(map[string]store.EliRepairPolicy, len(policies))
	for _, p := range policies {
		byRepo[p.Repo] = p
	}
	out := make([]EliAutomaticStop, 0, len(repos))
	seen := make(map[string]bool, len(repos))
	for _, repo := range repos {
		if seen[repo] {
			continue
		}
		seen[repo] = true
		p, ok := byRepo[repo]
		switch {
		case !ok:
			out = append(out, EliAutomaticStop{Repo: repo, Error: "No repair policy for this repository."})
		case !p.Enabled || !p.Automatic:
			out = append(out, EliAutomaticStop{Repo: repo})
		default:
			if then == EliStopOff {
				p.Enabled = false
			} else {
				p.Automatic, p.AskFirst = false, true
			}
			if err := c.st.SetEliRepairPolicy(ctx, p); err != nil {
				c.log.Warn("could not stop automatic repairs", "repo", repo, "error", err)
				out = append(out, EliAutomaticStop{Repo: repo, Error: "Could not save this repository's policy. Try again."})
				continue
			}
			out = append(out, EliAutomaticStop{Repo: repo, Changed: true})
		}
	}
	return out, nil
}

// automaticProviderCanPay is whether the policy names an installation provider
// that is switched on, the only kind that may pay for a repair nobody asked for.
// It is the test the overview describes in words and the worker applies again
// before it writes, so all three agree on what "cannot pay" means.
func (c *Controller) automaticProviderCanPay(ctx context.Context, p store.EliRepairPolicy) (bool, error) {
	if p.ProviderID == "" {
		return false, nil
	}
	provider, err := c.st.GetAssistantProvider(ctx, p.ProviderID)
	if errors.Is(err, store.ErrNotFound) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return provider.Enabled && provider.OwnerID == "", nil
}
