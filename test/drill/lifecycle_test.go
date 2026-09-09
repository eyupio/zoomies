//go:build drill

package drill

import (
	"testing"
	"time"
)

// The lifecycle drill: a queued job becomes a real process on this machine,
// and everything is taken away again afterwards.
//
// This is the one that runs on every pull request, and it is the first thing
// in this repository that qualifies the product as an operator gets it. Every
// other test builds a controller in the test process and calls its methods.
// Here the controller is the built binary, the agent is a second copy of that
// binary on the other end of a socket having joined with a token, and the
// backend puts a process on the host. The only fake is GitHub.
//
// What it is really pinning is the seam nothing else can: that the decision the
// scheduler makes actually reaches a host, that the host does something real
// with it, and that the fleet's own accounting agrees with the world afterwards
// -- on GitHub's side and on the host's.
func TestAQueuedJobBecomesARealWorkloadAndIsCleanedUp(t *testing.T) {
	f := newFleet(t)
	rec := newRecord(t, "lifecycle")
	defer rec.write()

	label := "drill-lifecycle"
	poolID := f.createPool("drill", label)

	// GitHub has work. The controller finds it by polling, because a fake
	// cannot deliver a webhook to a process behind no address.
	job := f.gh.AddQueuedJob("acme/api", "CI", "build", []string{"self-hosted", label})

	// 1. The fleet decides to create a runner, and the remote agent gets it.
	var runner runnerView
	waitFor(t, waitRunnerCreated, "a runner to be created for the pool", func() bool {
		for _, r := range f.runners(poolID) {
			runner = r
			return true
		}
		return false
	})
	rec.note("runner created", runner.Name)

	// 2. A real process appears on this host. This is the assertion the whole
	//    tier exists for: not "the row says provisioning" but "there is a
	//    workload, laid out by the backend, running."
	waitFor(t, waitWorkloadUp, "a workload to appear on the host", func() bool {
		return len(f.liveWorkloads()) == 1
	})
	rec.note("workload on host", f.liveWorkloads()[0])

	// 3. And GitHub has the registration the runner was minted with.
	waitFor(t, waitWorkloadUp, "the runner to be registered with GitHub", func() bool {
		return len(f.gh.Runners()) == 1
	})
	registered := f.gh.Runners()[0]
	if registered.Name != runner.Name {
		t.Errorf("GitHub registered %q but the fleet made %q; the names must match or nothing can be reconciled",
			registered.Name, runner.Name)
	}

	// 4. The job runs and finishes. A stub runner does not talk to GitHub, so
	//    the drill drives both sides: the fake's state, and the webhook GitHub
	//    would have sent. That is what makes a fault drill possible at all --
	//    the timing is ours.
	f.gh.StartJob(job.ID, runner.Name)
	f.deliverJob("in_progress", job, runner.Name, "")
	waitFor(t, waitJobDone, "the fleet to see the job start", func() bool {
		for _, r := range f.runners(poolID) {
			if r.ID == runner.ID && (r.State == "busy" || r.State == "draining" || r.State == "removed") {
				return true
			}
		}
		return false
	})
	f.gh.CompleteJob(job.ID, "success")
	f.deliverJob("completed", job, runner.Name, "success")
	// The runner is ephemeral, so the real one would exit here of its own
	// accord. Telling the stub to do the same is what makes step 5 a test of
	// Zoomies noticing rather than of Zoomies tearing something down.
	if err := finishJob(f.runnerDir(runner.Name)); err != nil {
		t.Fatalf("telling the stub runner its job is over: %v", err)
	}

	// 5. The runner goes away by itself, because it is ephemeral.
	waitFor(t, waitRunnerGone, "the runner to reach a terminal state", func() bool {
		for _, r := range f.runners(poolID) {
			if r.ID == runner.ID {
				return r.State == "removed" || r.State == "failed"
			}
		}
		return false
	})
	final := f.runnerByID(runner.ID, poolID)
	if final.State != "removed" {
		t.Errorf("runner ended in state %q, want removed", final.State)
	}
	rec.note("runner terminal state", final.State)

	// 6. The two checks that make this a drill rather than an assertion about
	//    Zoomies' own opinion of itself. They are asserted separately, and the
	//    registration one is asserted *promptly*, for a reason worth spelling
	//    out.
	//
	//    On *this* path the registration check cannot be tight, and it is worth
	//    saying why rather than writing a sharp-looking assertion that is not.
	//    An ephemeral runner that finishes its job exits by itself; the agent
	//    reports it gone and applyRunnerState marks the row removed, which does
	//    not delete anything on GitHub. Nothing needs to, because real GitHub
	//    removes a just-in-time runner's registration itself once it has run
	//    its one job. The fake does not model that, so what clears it here is
	//    the reaper's sweep -- the backstop, a minute in.
	//
	//    So this asserts the weaker true thing: the fleet does not leave a
	//    registration behind for ever. TestRemovingARunnerDeletesItsRegistration
	//    is where the sharp version lives, on the path where deleting it really
	//    is Zoomies' job.
	waitFor(t, waitRunnerGone, "the workload to be gone from the host", func() bool {
		return len(f.liveWorkloads()) == 0
	})
	rec.note("host workloads after cleanup", "0")

	waitFor(t, waitRunnerGone, "GitHub to hold no registration for this run", func() bool {
		return len(f.gh.Runners()) == 0
	})
	rec.note("github registrations after cleanup", "0")
	rec.pass("a queued job became a real process on this host and everything was cleaned up")
}

// runnerByID re-reads one runner from the controller.
func (f *fleet) runnerByID(id, poolID string) runnerView {
	f.t.Helper()
	for _, r := range f.runners(poolID) {
		if r.ID == id {
			return r
		}
	}
	f.t.Fatalf("runner %s is no longer listed", id)
	return runnerView{}
}

var _ = time.Second
