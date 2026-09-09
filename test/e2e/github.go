//go:build e2e

package e2e

import (
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"strings"

	"github.com/eyupio/zoomies/internal/backend"
)

// Everything here asks GitHub and the Docker host directly, rather than asking
// Zoomies.
//
// That is the whole point. The failure this harness exists to catch is a
// registration left behind on an organisation after a runner is gone, and
// Zoomies' own /runners endpoint cannot see it: the row says "removed" exactly
// when Zoomies believes it removed it, which is the belief under test. Asking
// the controller whether the controller was right is not evidence.
//
// The calls go through the gh CLI, as the workflow dispatch already does, so
// this test needs no GitHub token of its own in its environment.

// ghRunner is one self-hosted runner as GitHub sees it.
type ghRunner struct {
	ID     int64  `json:"id"`
	Name   string `json:"name"`
	Status string `json:"status"`
	Labels []struct {
		Name string `json:"name"`
	} `json:"labels"`
}

// runnersPath is the API path for the target's self-hosted runners.
func (e env) runnersPath() string {
	if e.targetType == "repo" {
		return "/repos/" + e.target + "/actions/runners"
	}
	return "/orgs/" + e.target + "/actions/runners"
}

// githubRunnersWithLabel lists the registrations GitHub currently holds that
// carry one label. Per-run labels are what make this answer belong to this run
// rather than to whatever else the organisation is doing.
func githubRunnersWithLabel(e env, label string) ([]ghRunner, error) {
	out, err := exec.Command("gh", "api", "--paginate", e.runnersPath()).Output()
	if err != nil {
		return nil, fmt.Errorf("asking GitHub for %s's runners: %w", e.target, ghError(err))
	}
	// --paginate concatenates pages, so decode as a stream rather than once.
	dec := json.NewDecoder(strings.NewReader(string(out)))
	var found []ghRunner
	for {
		var page struct {
			Runners []ghRunner `json:"runners"`
		}
		if err := dec.Decode(&page); err != nil {
			break
		}
		for _, r := range page.Runners {
			for _, l := range r.Labels {
				if l.Name == label {
					found = append(found, r)
					break
				}
			}
		}
	}
	return found, nil
}

// deleteGitHubRunner removes one registration. It is used only by the sweep,
// on the leavings of a run that did not finish: the scenario itself asserts
// that Zoomies removed the registration, and doing it here would hide exactly
// the fault being looked for.
func deleteGitHubRunner(e env, id int64) error {
	cmd := exec.Command("gh", "api", "-X", "DELETE", fmt.Sprintf("%s/%d", e.runnersPath(), id))
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("deleting runner %d from %s: %w\n%s", id, e.target, err, out)
	}
	return nil
}

// containersForPool lists the workload containers still on this host that
// belong to one pool, by the labels the backend stamps on every workload it
// creates (internal/backend.Spec.Labels).
//
// The host is asked directly for the same reason GitHub is: a controller that
// has lost track of a container will report it gone, and the container will
// still be holding the disk and, if it is a docker-in-docker sidecar, still
// running as root.
func containersForPool(poolID string) ([]string, error) {
	cmd := exec.Command("docker", "ps", "-a",
		"--filter", "label="+backend.LabelManaged+"=true",
		"--filter", "label="+backend.LabelPoolID+"="+poolID,
		"--format", "{{.ID}} {{.Names}} {{.Status}}")
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("asking the Docker daemon what is still running: %w", err)
	}
	var found []string
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if line != "" {
			found = append(found, line)
		}
	}
	return found, nil
}

// ghError unwraps an ExitError so the message carries what gh printed rather
// than "exit status 1".
func ghError(err error) error {
	var ee *exec.ExitError
	if errors.As(err, &ee) && len(ee.Stderr) > 0 {
		return fmt.Errorf("%w: %s", err, strings.TrimSpace(string(ee.Stderr)))
	}
	return err
}
