package controller

import (
	"testing"

	"github.com/eyupio/zoomies/internal/store"
)

// An installation's target is stored folded to lower case, and a repository
// arrives in GitHub's own case. Compared exactly, every organisation with a
// capital letter in its name was refused its own repositories' evidence.
func TestEvidenceIsCollectedForARepositoryNamedInGitHubsOwnCase(t *testing.T) {
	h := newHarness(t)
	org := h.installationOn("acme", store.TargetOrg)
	if _, err := h.c.CollectExperimentAttempts(h.ctx, org.ID, "Acme/Widgets", []ExperimentAttemptSelection{{RunID: 1, Attempt: 1}}); err != nil {
		t.Fatalf("an installation on acme refused Acme/Widgets: %v", err)
	}
}
