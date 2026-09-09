package api

import (
	"net/http"
	"strings"
	"testing"

	"github.com/eyupio/zoomies/internal/store"
)

// Verify has to name the permission, not the status code.
//
// An App created without the self-hosted-runners permission is the commonest
// way a connected installation fails, and it fails at the first runner call
// rather than at anything the connect dialog does -- so "403" arrives minutes
// later, attached to a pool that will not scale. The handler's own contract is
// that it says which permission and at what level; nothing asserted it.
func TestVerifyNamesTheMissingPermissionRatherThanTheStatus(t *testing.T) {
	h := newHarness(t)
	inst := h.installation()
	admin, _ := h.user("root", store.RoleAdmin)

	// Everything but the one that matters for an organisation.
	h.gh.SetPermissions(map[string]string{
		"metadata": "read",
		"actions":  "read",
	})

	res := h.do(request{method: http.MethodPost, path: "/api/v1/installations/" + inst.ID + "/verify",
		cookie: h.session(admin)})
	res.mustStatus(t, http.StatusOK, "verify")

	var out struct {
		OK                 bool     `json:"ok"`
		Message            string   `json:"message"`
		MissingPermissions []string `json:"missing_permissions"`
	}
	res.into(t, &out)
	if out.OK {
		t.Fatal("an App missing the runner permission verified as healthy")
	}
	// The probe makes a real runner call, so an App without the permission
	// usually fails there rather than on what /app reported -- either way the
	// operator is told which permission, at what level, and what it is now.
	t.Logf("verify said: %s", out.Message)
	// The words an operator has to find on GitHub's own settings page, and the
	// level to set it to -- not the API's name for the field.
	if !strings.Contains(out.Message, "Self-hosted runners") {
		t.Errorf("the message does not name the permission an operator must grant: %q", out.Message)
	}
	if !strings.Contains(out.Message, "write") {
		t.Errorf("the message does not say what level to grant it at: %q", out.Message)
	}
	// And what it currently is, so somebody who granted read can see the
	// difference rather than wondering whether they granted anything.
	if !strings.Contains(strings.ToLower(out.Message), "not granted") {
		t.Errorf("the message does not say what the permission is now: %q", out.Message)
	}
}

// The other half: an App with the permissions and no `workflow_job`
// subscription still works, and says why it will be slower rather than
// refusing. Scaling falls back to the poller, which is a different problem
// from a broken credential and must not read like one.
func TestVerifySeparatesAMissingSubscriptionFromABrokenCredential(t *testing.T) {
	h := newHarness(t)
	inst := h.installation()
	admin, _ := h.user("root", store.RoleAdmin)
	h.gh.SetEvents("push")

	res := h.do(request{method: http.MethodPost, path: "/api/v1/installations/" + inst.ID + "/verify",
		cookie: h.session(admin)})
	res.mustStatus(t, http.StatusOK, "verify")

	var out struct {
		OK            bool     `json:"ok"`
		Message       string   `json:"message"`
		MissingEvents []string `json:"missing_events"`
	}
	res.into(t, &out)
	if !out.OK {
		t.Fatalf("a working App with no webhook subscription was reported as broken: %q", out.Message)
	}
	if len(out.MissingEvents) != 1 || out.MissingEvents[0] != "workflow_job" {
		t.Fatalf("missing events = %v, want workflow_job", out.MissingEvents)
	}
	if !strings.Contains(out.Message, "poller") {
		t.Errorf("the message does not say what happens instead: %q", out.Message)
	}
}

// Verify says which repositories the installation can actually see.
//
// It is the second commonest setup mistake after a missing permission, and the
// quietest: an App with every permission correct, installed on "only select
// repositories" and not on the one somebody pushes to, is a fleet where
// nothing ever queues and no page says why. GitHub knows the answer and
// Zoomies never asked it.
func TestVerifySaysWhichRepositoriesTheInstallationCanSee(t *testing.T) {
	h := newHarness(t)
	inst := h.installation()
	admin, _ := h.user("root", store.RoleAdmin)
	h.gh.SetRepositorySelection("selected")
	// What "selected" means on this installation: the three somebody ticked.
	h.gh.AddRepo("acme/widgets")
	h.gh.AddRepo("acme/site")
	h.gh.AddRepo("acme/infra")

	res := h.do(request{method: http.MethodPost, path: "/api/v1/installations/" + inst.ID + "/verify",
		cookie: h.session(admin)})
	res.mustStatus(t, http.StatusOK, "verify")

	var out struct {
		OK                  bool     `json:"ok"`
		RepositorySelection string   `json:"repository_selection"`
		RepositoryCount     int      `json:"repository_count"`
		Repositories        []string `json:"repositories"`
	}
	res.into(t, &out)
	if !out.OK {
		t.Fatal("a healthy installation failed to verify")
	}
	if out.RepositorySelection != "selected" {
		t.Errorf("repository_selection = %q, want GitHub's own word for it", out.RepositorySelection)
	}
	if out.RepositoryCount == 0 || len(out.Repositories) == 0 {
		t.Fatalf("the installation can see %d repositories and none was named; the dialog has nothing to show",
			out.RepositoryCount)
	}
	// Named, so an operator can look for the one they were expecting rather
	// than counting.
	for _, name := range out.Repositories {
		if !strings.Contains(name, "/") {
			t.Errorf("repository %q is not an owner/name", name)
		}
	}
}
