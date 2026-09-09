//go:build drill

package drill

import (
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

// The fourth fault drill: GitHub takes the installation's quota away.
//
// A rate limit is the fault most likely to happen to a real fleet and the one
// least visible from inside it: nothing has crashed, nothing is misconfigured,
// and the fleet simply stops doing anything. That is why the product answers
// it with a stand-down that is *visible* -- a gauge per installation, so an
// operator watching a fleet that has gone quiet can tell "out of quota" from
// "nothing to do", which no other series here distinguishes.
//
// The drill's claim is the pair: the fleet holds off while the quota is gone,
// and it starts again by itself when the reset passes. The second half is what
// nothing in process can prove, because the hold is a wall-clock deadline the
// controller keeps for itself.
func TestARateLimitedInstallationStandsDownAndComesBack(t *testing.T) {
	f := newFleet(t)
	rec := newRecord(t, "rate-limit")
	defer rec.write()

	label := "drill-rate-limit"
	poolID := f.createPool("drill-quota", label)

	// A 403 with the rate-limit headers is how GitHub says it, and the reset
	// is put a few seconds out so the drill can watch the recovery rather than
	// the fifteen-minute backoff a limit with no reset would earn.
	reset := time.Now().Add(10 * time.Second)
	rec.faultInjected("GitHub answers 403 with no quota remaining")
	f.gh.SetRateLimit(5000, 0, reset)
	f.gh.SetError("", http.StatusForbidden, "API rate limit exceeded for installation")

	job := f.gh.AddQueuedJob("acme/api", "CI", "build", []string{"self-hosted", label})

	// The stand-down is per installation and it is on the scrape, which is the
	// only place it shows: a fleet holding off looks exactly like an idle one
	// everywhere else.
	waitFor(t, waitRecovery, "the controller to report the installation as paused", func() bool {
		return f.gaugeIsOne("zoomies_github_paused")
	})
	rec.note("stand-down visible on /metrics", "zoomies_github_paused is 1")

	if live := f.liveWorkloads(); len(live) != 0 {
		t.Errorf("the host holds %v while GitHub is refusing every call; a fleet out of quota must not spend it starting runners it cannot register", live)
	}

	// The quota comes back. Nothing is restarted: the hold is a deadline the
	// controller keeps for itself, and passing it is the whole test.
	f.gh.ClearErrors()
	f.gh.SetRateLimit(5000, 5000, time.Now().Add(time.Hour))
	waitFor(t, waitRecovery, "a workload to appear once the quota is back", func() bool {
		return len(f.liveWorkloads()) == 1
	})
	rec.recovered()
	rec.note("recovered by itself", f.liveWorkloads()[0])

	waitFor(t, waitRecovery, "the installation to stop being reported as paused", func() bool {
		return !f.gaugeIsOne("zoomies_github_paused")
	})
	rec.note("stand-down cleared", "zoomies_github_paused is 0")

	// And the job it was all for runs.
	var runner runnerView
	waitFor(t, waitRunnerCreated, "a runner that is not one of the failed attempts", func() bool {
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
	rec.pass("an installation out of quota was held off and said so on its scrape, then came back by itself when the reset passed and ran the job")
}

// gaugeIsOne reports whether any sample of a gauge is 1 on the controller's own
// scrape.
//
// The scrape rather than the API on purpose: this is the signal an operator's
// monitoring actually watches, and a stand-down that is only visible to a
// reader of the logs is not visible at all.
func (f *fleet) gaugeIsOne(name string) bool {
	f.t.Helper()
	resp, err := http.Get(f.baseURL + "/metrics")
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return false
	}
	for _, line := range strings.Split(string(body), "\n") {
		if strings.HasPrefix(line, name) && strings.HasSuffix(strings.TrimSpace(line), " 1") {
			return true
		}
	}
	return false
}
