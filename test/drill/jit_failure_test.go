//go:build drill

package drill

import (
	"net/http"
	"testing"
)

// The third fault drill: GitHub refuses to mint runner configurations.
//
// This is the fault with the least to see and the most to get wrong. A runner
// is registered by asking GitHub for a just-in-time configuration, and that
// call failing is not a fleet problem, a host problem or a pool problem --
// there is nothing wrong with any of them, and there is nothing an operator
// can do but wait. So the two things that matter are that the fleet does not
// make it worse (no half-built runner left on the host, no registration left
// on GitHub, no runaway retry against an endpoint that is already unhappy) and
// that it comes back on its own, without anybody restarting anything.
func TestAFailingJITEndpointLeavesNothingBehindAndRecovers(t *testing.T) {
	f := newFleet(t)
	rec := newRecord(t, "jit-endpoint")
	defer rec.write()

	label := "drill-jit"
	poolID := f.createPool("drill-jit", label)

	// The fault is injected before there is any work, so the very first
	// attempt meets it: a drill that broke the endpoint afterwards would be
	// testing a retry rather than a start.
	rec.faultInjected("the just-in-time configuration endpoint answers 500")
	f.gh.SetError("generate-jitconfig", http.StatusInternalServerError, "the drill broke this on purpose")

	job := f.gh.AddQueuedJob("acme/api", "CI", "build", []string{"self-hosted", label})

	// The fleet tries, and says so on the runner rather than silently doing
	// nothing: an operator looking at this pool has to be able to see that
	// something was attempted and what refused it.
	waitFor(t, waitRunnerCreated, "the fleet to try to create a runner", func() bool {
		return len(f.runners(poolID)) > 0
	})
	waitFor(t, waitRunnerCreated, "the attempt to be recorded as failed", func() bool {
		for _, r := range f.runners(poolID) {
			if r.State == "failed" {
				return true
			}
		}
		return false
	})
	rec.note("first attempt", "failed, as the endpoint refuses")

	// Nothing was left behind by the attempt. This is the half that would hurt
	// silently: a directory on the host nobody owns, or a registration on
	// GitHub for a runner that never existed.
	if live := f.liveWorkloads(); len(live) != 0 {
		t.Errorf("the host holds %v after a registration that never succeeded; a failed start must not leave a workload", live)
	}
	if got := f.gh.Runners(); len(got) != 0 {
		t.Errorf("GitHub holds %d registration(s) after a failed configuration; there is nothing there to register", len(got))
	}
	rec.note("left behind by the failure", "nothing on the host, nothing on GitHub")

	// GitHub recovers. Nothing is restarted and nothing is asked again by
	// hand: the fleet is expected to try again by itself, which is the whole
	// claim of a drill about a transient fault.
	f.gh.ClearErrors()
	waitFor(t, waitRecovery, "a workload to appear once the endpoint is back", func() bool {
		return len(f.liveWorkloads()) == 1
	})
	rec.recovered()
	rec.note("recovered by itself", f.liveWorkloads()[0])

	// And the job it was all for actually runs.
	var runner runnerView
	waitFor(t, waitRunnerCreated, "the fleet to have a runner that is not the failed one", func() bool {
		for _, r := range f.runners(poolID) {
			if r.State != "failed" {
				runner = r
				return true
			}
		}
		return false
	})
	f.gh.StartJob(job.ID, runner.Name)
	f.deliverJob("in_progress", job, runner.Name, "")
	f.gh.CompleteJob(job.ID, "success")
	f.deliverJob("completed", job, runner.Name, "success")
	if err := finishJob(f.runnerDir(runner.Name)); err != nil {
		t.Fatalf("telling the stub runner its job is over: %v", err)
	}
	waitFor(t, waitRunnerGone, "the workload to be gone from the host", func() bool {
		return len(f.liveWorkloads()) == 0
	})
	rec.note("job run after the recovery", runner.Name)
	rec.pass("a refused runner configuration failed the attempt without leaving a workload or a registration behind, and the fleet placed a runner by itself once GitHub answered again")
}
