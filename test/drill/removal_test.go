//go:build drill

package drill

import (
	"net/http"
	"slices"
	"testing"
)

// Removing a runner must take its GitHub registration with it, promptly.
//
// This is the sharp version of the cleanup assertion, and it needs its own
// drill because the lifecycle path cannot carry it. There, the runner finishes
// its job and exits, the agent reports it gone, and the row is marked removed
// without anything being deleted on GitHub -- correctly, because real GitHub
// removes a just-in-time runner itself once it has run its job.
//
// An operator removing an idle runner is the other case, and the one the
// roadmap named: GitHub will not tidy that up, so if Zoomies does not, the
// organisation's runner list fills with dead entries. Here deleting the
// registration really is removeRunner's job, and the drill holds it to doing so
// well inside the reaper's first sweep -- otherwise the assertion would be
// satisfied by the backstop, which is how the first version of the lifecycle
// drill passed with the deletion commented out.
func TestRemovingARunnerDeletesItsRegistration(t *testing.T) {
	f := newFleet(t)
	rec := newRecord(t, "removal")
	defer rec.write()

	label := "drill-removal"
	poolID := f.createPool("drillrm", label)

	// A queued job is only the way to get a runner made; this drill is about
	// what happens when one is taken away without ever finishing work.
	f.gh.AddQueuedJob("acme/api", "CI", "build", []string{"self-hosted", label})

	var runner runnerView
	waitFor(t, waitRunnerCreated, "a runner to be created", func() bool {
		for _, r := range f.runners(poolID) {
			runner = r
			return true
		}
		return false
	})
	waitFor(t, waitWorkloadUp, "the runner to be registered with GitHub", func() bool {
		return f.registeredWithGitHub(runner.Name)
	})
	rec.note("registered with github", runner.Name)

	// Remove it the way an operator does.
	f.api.do(http.MethodDelete, "/runners/"+runner.ID+"?force=true", nil, nil)
	rec.note("removal requested", runner.Name)

	// Both assertions name the runner that was removed rather than counting
	// what is left, because the job is still queued and the scheduler is right
	// to put a replacement on the host straight away. Counting would have this
	// drill fail on correct behaviour -- and worse, a version of it that waited
	// for zero would pass or fail on which of the two happened first.
	waitFor(t, waitPromptCleanup, "GitHub's registration to be deleted by the removal itself", func() bool {
		return !f.registeredWithGitHub(runner.Name)
	})
	rec.note("github registration after removal", "gone")

	waitFor(t, waitRunnerGone, "the removed runner's workload to be gone from the host", func() bool {
		return !slices.Contains(f.liveWorkloads(), runner.Name)
	})
	rec.note("host workload after removal", "gone")
	rec.pass("removing a runner deleted its GitHub registration without waiting for the reaper")
}
