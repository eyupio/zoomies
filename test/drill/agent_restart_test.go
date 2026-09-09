//go:build drill

package drill

import "testing"

// The second fault drill: the agent is killed in the middle of a job.
//
// This is the fault an operator causes themselves -- an upgrade, a restart, a
// machine rebooting its services -- and the one with the most to lose, because
// the agent is the process on the same host as the work. Two things have to
// hold and they are separate claims. The runner must survive the agent dying,
// or every agent restart is a lost build. And the agent must come back as the
// host it already was, from the credentials in its work directory, or a
// restart means re-joining a fleet by hand on every machine.
//
// Both hold. What this drill found is the third thing, at the end: the
// adopted runner's exit code died with the old agent, so a build that
// succeeded is recorded as removed or failed depending on which path gets
// there first.
func TestKillingTheAgentMidJobLeavesTheWorkRunning(t *testing.T) {
	f := newFleet(t)
	rec := newRecord(t, "agent-restart")
	defer rec.write()

	label := "drill-agent-restart"
	poolID := f.createPool("drill-agent", label)
	job := f.gh.AddQueuedJob("acme/api", "CI", "build", []string{"self-hosted", label})

	var runner runnerView
	waitFor(t, waitRunnerCreated, "a runner to be created for the pool", func() bool {
		for _, r := range f.runners(poolID) {
			runner = r
			return true
		}
		return false
	})
	waitFor(t, waitWorkloadUp, "a workload to appear on the host", func() bool {
		return len(f.liveWorkloads()) == 1
	})
	rec.note("workload on host", f.liveWorkloads()[0])

	f.gh.StartJob(job.ID, runner.Name)
	f.deliverJob("in_progress", job, runner.Name, "")
	waitFor(t, waitJobDone, "the fleet to see the job start", func() bool {
		for _, r := range f.runners(poolID) {
			if r.ID == runner.ID && (r.State == "busy" || r.State == "draining") {
				return true
			}
		}
		return false
	})

	hostsBefore := f.hostIDs()
	rec.faultInjected("killed the agent while the job was running")
	f.agent.kill()

	// The claim that matters most on this path: the runner is a process doing
	// somebody's work, and the agent going away is not a reason to end it.
	f.requireStillRunning(runner.Name, "killing the agent must not take the job with it")
	rec.note("workload after the agent died", "still running")

	f.restartAgent()
	waitFor(t, waitRecovery, "the agent to be back", func() bool {
		return len(f.hostIDs()) > 0
	})
	if after := f.hostIDs(); len(after) != len(hostsBefore) {
		t.Fatalf("the fleet has hosts %v after the restart but had %v before; an agent that comes back as a second host leaves the first one to be reaped and its runners with it",
			after, hostsBefore)
	}
	rec.note("host after the restart", "the same one, no second host")

	// And the fleet finishes what was running: the restarted agent picked the
	// runner back up rather than leaving it for a timeout.
	f.gh.CompleteJob(job.ID, "success")
	f.deliverJob("completed", job, runner.Name, "success")
	if err := finishJob(f.runnerDir(runner.Name)); err != nil {
		t.Fatalf("telling the stub runner its job is over: %v", err)
	}
	waitFor(t, waitRecovery, "the runner to reach a terminal state after the agent came back", func() bool {
		for _, r := range f.runners(poolID) {
			if r.ID == runner.ID {
				return r.State == "removed" || r.State == "failed"
			}
		}
		return false
	})
	waitFor(t, waitRecovery, "the workload to be gone from the host", func() bool {
		return len(f.liveWorkloads()) == 0
	})
	rec.recovered()
	rec.note("host workloads after the restarted agent finished the job", "0")

	// The finding, and it is a race rather than a wrong answer -- which is
	// worse, because it means the fleet's record of a successful build depends
	// on which path lands first. Run this drill repeatedly and the runner ends
	// `removed` sometimes and `failed` others.
	//
	// The reason is in the process backend, which says it in as many words: the
	// exit code is written by the parent that reaped the process, so an agent
	// restarted while its runner was running finds the process gone with
	// nothing recorded and calls that a failure. It cannot know better on its
	// own -- it is not the process's parent any more -- and it is racing the
	// removal the completed job set off. Nothing consults the job's own
	// outcome, which GitHub has already reported as a success.
	//
	// So this asserts what is true either way and records the rest for a
	// person: a drill that asserted one of the two would be a flaky test
	// pretending to be a rule.
	final := f.runnerByID(runner.ID, poolID)
	if final.State != "removed" && final.State != "failed" {
		t.Errorf("runner ended in state %q, want a terminal one", final.State)
	}
	rec.note("runner state after the restart finished the job", final.State)
	rec.finding("decide what a runner whose exit nobody recorded should be called: after an agent restart a successful job's runner ends removed or failed depending on which path lands first, and the fleet's own accounting differs run to run")
	rec.pass("an agent killed mid-job left the work running, came back as the same host, and the job was finished and cleaned up; what the runner ends up called is the finding")
}

// hostIDs is the fleet's own list of hosts, which is how a drill tells a
// restart from a second machine appearing.
func (f *fleet) hostIDs() []string {
	f.t.Helper()
	var out struct {
		Items []struct {
			ID string `json:"id"`
		} `json:"items"`
	}
	f.api.get("/hosts", &out)
	ids := make([]string, 0, len(out.Items))
	for _, h := range out.Items {
		ids = append(ids, h.ID)
	}
	return ids
}
