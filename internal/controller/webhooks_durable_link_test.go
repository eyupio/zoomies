package controller

import (
	"testing"

	"github.com/eyupio/zoomies/internal/agent"
	"github.com/eyupio/zoomies/internal/store"
)

// A job row is found by GitHub's job id alone, so a delivery that reuses
// another installation's id reaches that job's stored runner link. Job ids are
// in every job's address, and the link named a runner the delivery itself was
// never allowed to name: a completed delivery signed by globex removed acme's
// runner in the middle of acme's build.
func TestADeliveryReusingAJobIDCannotReleaseAnotherInstallationsRunner(t *testing.T) {
	h := newHarness(t)
	_, _, host := h.fleet()
	labels := []string{"self-hosted", "linux", "x64", "demo"}
	victim := liveRunnerOf(t, h, host.ID, 7000, labels)
	h.deliverJob(jobEvent{Action: "in_progress", JobID: 7000, Name: "test", Workflow: "CI", Labels: labels, RunnerName: victim.Name})
	h.installationOn("globex", store.TargetOrg)

	h.deliverJob(jobEvent{Action: "completed", JobID: 7000, Repo: "globex/app", Name: "test", Workflow: "CI", Conclusion: "cancelled"})

	after := h.runnerByID(t, victim.ID)
	if after.State != store.RunnerBusy || after.CurrentJobID == "" {
		t.Fatalf("a globex delivery reusing acme's job id moved acme's runner: state %q, current job %q; want it still busy",
			after.State, after.CurrentJobID)
	}
	if h.hasTaskOfKind(host.ID, agent.TaskRemoveRunner) {
		t.Fatal("a globex delivery reusing acme's job id queued the removal of acme's runner")
	}
}
