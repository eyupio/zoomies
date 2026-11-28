package controller

import (
	"testing"

	"github.com/eyupio/zoomies/internal/agent"
)

// A runner's GitHub id is what its registration is later deleted by, with the
// App's own credentials. Taken from a host's report, it let a host name any
// runner of the organisation on its own reusable runner and have the fleet
// delete that registration for it. GitHub is who says which id a runner has.
func TestAHostCannotSayWhichGitHubRunnerItsRunnerIs(t *testing.T) {
	h := newHarness(t)
	_, pool, host := h.fleet()
	pool.Ephemeral = false
	if err := h.st.UpdatePool(h.ctx, pool); err != nil {
		t.Fatalf("UpdatePool: %v", err)
	}
	bystander := h.gh.AddRunner("someone-elses-runner", []string{"self-hosted"})
	h.deliverJob(jobEvent{Action: "queued", JobID: 9000, Name: "test", Workflow: "CI",
		Labels: []string{"self-hosted", "linux", "x64", "demo"}})
	if err := h.c.Reconcile(h.ctx); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	h.c.lifecycleCalls.Wait()
	r := h.onlyRunner()

	if err := h.c.ReportRunners(h.ctx, host.ID, []agent.RunnerReport{{RunnerID: r.ID, GitHubRunnerID: bystander.ID}}); err != nil {
		t.Fatalf("ReportRunners: %v", err)
	}

	if got := h.runnerByID(t, r.ID).GitHubRunnerID; got != 0 {
		t.Fatalf("the runner's GitHub id is %d after its host reported %d; want it left for GitHub to say", got, bystander.ID)
	}
}
