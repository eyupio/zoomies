//go:build e2e

package e2e

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// env is everything the scenario needs from outside itself.
type env struct {
	appID          string
	installationID string
	privateKey     string
	target         string
	targetType     string
	repo           string
}

// required reports whether a missing prerequisite is a failure rather than a
// skip. A gate runs in this mode; a laptop does not.
func required() bool { return os.Getenv("ZOOMIES_E2E_REQUIRED") != "" }

// requested reports whether this run was asked to do anything at all.
func requested() bool { return os.Getenv("ZOOMIES_E2E") != "" || required() }

// preflight checks every prerequisite before anything is created.
//
// "Before anything is created" is the whole change. The previous harness
// checked its environment and Docker up front but looked for the gh CLI in the
// middle of the scenario, after it had already made a GitHub App installation
// and a pool on a real organisation -- and then called t.Skip, which is exit
// code zero. A green run could therefore mean "we created two things on your
// organisation, could not continue, left them there, and told you nothing".
//
// So every check that can be made without side effects is made here, and the
// scenario does not begin until they all hold.
func preflight() (env, []string) {
	e := env{
		appID:          os.Getenv("ZOOMIES_E2E_APP_ID"),
		installationID: os.Getenv("ZOOMIES_E2E_INSTALLATION_ID"),
		target:         os.Getenv("ZOOMIES_E2E_TARGET"),
		targetType:     orDefault(os.Getenv("ZOOMIES_E2E_TARGET_TYPE"), "org"),
		repo:           os.Getenv("ZOOMIES_E2E_REPO"),
	}
	keyFile := os.Getenv("ZOOMIES_E2E_PRIVATE_KEY_FILE")

	var missing []string
	for _, v := range []struct{ name, value string }{
		{"ZOOMIES_E2E_APP_ID", e.appID},
		{"ZOOMIES_E2E_INSTALLATION_ID", e.installationID},
		{"ZOOMIES_E2E_PRIVATE_KEY_FILE", keyFile},
		{"ZOOMIES_E2E_TARGET", e.target},
		{"ZOOMIES_E2E_REPO", e.repo},
	} {
		if v.value == "" {
			missing = append(missing, v.name+" is not set")
		}
	}
	switch e.targetType {
	case "org", "repo":
	default:
		missing = append(missing, fmt.Sprintf("ZOOMIES_E2E_TARGET_TYPE is %q; it must be org or repo", e.targetType))
	}
	if keyFile != "" {
		pem, err := os.ReadFile(keyFile)
		switch {
		case err != nil:
			missing = append(missing, fmt.Sprintf("ZOOMIES_E2E_PRIVATE_KEY_FILE %s cannot be read: %v", keyFile, err))
		case !strings.Contains(string(pem), "PRIVATE KEY"):
			missing = append(missing, fmt.Sprintf("%s does not look like a PEM private key", keyFile))
		default:
			e.privateKey = string(pem)
		}
	}

	// The tools, all of them, up front.
	for _, tool := range []struct{ bin, why string }{
		{"docker", "the runner backend needs a Docker daemon on this host"},
		{"gh", "the workflow is dispatched and GitHub is queried through the gh CLI"},
		{"git", "the result records the commit under test"},
	} {
		if _, err := exec.LookPath(tool.bin); err != nil {
			missing = append(missing, fmt.Sprintf("%s is not on PATH: %s", tool.bin, tool.why))
		}
	}
	// A Docker binary that cannot reach a daemon is the same problem as no
	// Docker, and it is worth finding here rather than sixty seconds into a
	// runner that will never start.
	if _, err := exec.LookPath("docker"); err == nil {
		if out, err := exec.Command("docker", "info", "--format", "{{.ServerVersion}}").CombinedOutput(); err != nil {
			missing = append(missing, fmt.Sprintf("the Docker daemon is not reachable: %s", strings.TrimSpace(string(out))))
		}
	}
	// gh must actually be authenticated, for the same reason.
	if _, err := exec.LookPath("gh"); err == nil {
		if out, err := exec.Command("gh", "auth", "status").CombinedOutput(); err != nil {
			missing = append(missing, fmt.Sprintf("the gh CLI is not authenticated: %s", strings.TrimSpace(string(out))))
		}
	}
	if _, err := os.Stat(builtBinary()); err != nil {
		missing = append(missing, "the zoomies binary is not built; run `make build` first")
	}
	return e, missing
}

func orDefault(v, def string) string {
	if v == "" {
		return def
	}
	return v
}
