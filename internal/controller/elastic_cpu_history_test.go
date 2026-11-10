package controller

import (
	"fmt"
	"testing"
	"time"

	"github.com/eyupio/zoomies/internal/agent"
	"github.com/eyupio/zoomies/internal/backend"
	"github.com/eyupio/zoomies/internal/config"
	"github.com/eyupio/zoomies/internal/scheduler"
	"github.com/eyupio/zoomies/internal/store"
)

// historyRuns writes n finished runs of one job on the pool, each of which peaked
// at peak cores and was held back by its quota in throttled of its 1000 periods.
func (h *harness) historyRuns(pool *store.Pool, n int, peak float64, throttled int64) {
	h.t.Helper()
	at := h.c.Now().Add(-48 * time.Hour)
	for i := range n {
		runner := fmt.Sprintf("run_hist_%d_%d", int64(peak*100), i)
		start := at.Add(time.Duration(i) * time.Hour)
		j := &store.Job{
			GitHubJobID: int64(9000 + i + int(peak*1000)), Repo: "acme/widgets", Workflow: "CI", JobName: "unit",
			Labels: store.StringSlice{"self-hosted"}, State: store.JobInProgress, QueuedAt: start, StartedAt: &start,
			Matched: true, PoolID: pool.ID, RunnerID: runner,
		}
		if _, _, err := h.st.ApplyJob(h.ctx, j); err != nil {
			h.t.Fatalf("recording run %d: %v", i, err)
		}
		if err := h.st.RecordJobUsage(h.ctx, runner, peak, 900); err != nil {
			h.t.Fatal(err)
		}
		if err := h.st.RecordJobThrottle(h.ctx, runner, 1000, throttled); err != nil {
			h.t.Fatal(err)
		}
		done := start.Add(30 * time.Minute)
		j.State, j.Conclusion, j.CompletedAt = store.JobCompleted, "success", &done
		if _, _, err := h.st.ApplyJob(h.ctx, j); err != nil {
			h.t.Fatal(err)
		}
	}
}

// boostFor is the target elastic CPU gives a busy runner on a calm host that is
// running that job, with the history mode set as asked.
func (h *harness) boostFor(mode string, history func(*store.Pool)) (target, base float64) {
	h.t.Helper()
	_, pool, host := h.fleet()
	pool.CPUBurst = store.CPUBurstPolicy{Mode: store.CPUBurstAutomatic}
	if err := h.st.UpdatePool(h.ctx, pool); err != nil {
		h.t.Fatal(err)
	}
	history(pool)
	runner := h.runnerRow(pool, host, store.RunnerBusy)
	runner.AllocatedCPUs = 2
	runner.AllocationSource = store.AllocationFromHost
	if err := h.st.UpdateRunner(h.ctx, runner); err != nil {
		h.t.Fatal(err)
	}
	start := h.c.Now()
	running := &store.Job{
		GitHubJobID: 77001, Repo: "acme/widgets", Workflow: "CI", JobName: "unit", Labels: store.StringSlice{"self-hosted"},
		State: store.JobInProgress, QueuedAt: start, StartedAt: &start, Matched: true, PoolID: pool.ID, RunnerID: runner.ID,
	}
	if _, _, err := h.st.ApplyJob(h.ctx, running); err != nil {
		h.t.Fatal(err)
	}
	h.c.live.Update(func(c *config.Config) { c.Scheduler.ElasticCPUHistory = mode })

	now := h.c.Now()
	cpu := 20.0
	host.Usage = store.HostUsage{CPUPercent: &cpu, SampledAt: now}
	sampled := now
	directives := h.c.elasticCPUTargets(h.ctx, host, agent.HeartbeatRequest{
		Features: []string{agent.FeatureElasticCPU},
		Runners: []agent.RunnerReport{{RunnerID: runner.ID, Stats: backend.Stats{
			SampledAt: &sampled, CPUPercent: 400,
		}}},
	}, now)
	if len(directives) != 1 {
		h.t.Fatalf("directives = %+v, want one", directives)
	}
	return directives[0].TargetCPUs, directives[0].BaseCPUs
}

// A job whose recent runs were never held back by their quota is not lent more
// than a little above what it used: the cores above that are cores nobody uses,
// and every one of them is a loan to reclaim. Shadow records the verdict and lends
// as before, so the setting can be read on a fleet before it does anything.
func TestElasticCPUStopsLendingToAJobThatNeverWantedMore(t *testing.T) {
	for _, tc := range []struct {
		name      string
		mode      string
		throttled int64
		wantCap   bool
	}{
		{"off lends as it always did", scheduler.SizeOff, 5, false},
		{"shadow records and lends as before", scheduler.SizeShadow, 5, false},
		{"on caps a job that was never held back", scheduler.SizeOn, 5, true},
		{"on does not cap a job that was held back", scheduler.SizeOn, 400, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := newHarness(t)
			target, base := h.boostFor(tc.mode, func(p *store.Pool) { h.historyRuns(p, 6, 2.1, tc.throttled) })
			if base != 2 {
				t.Fatalf("base = %v, want the 2 CPU guarantee", base)
			}
			if tc.wantCap {
				if target > 2.75+0.001 || target < base {
					t.Fatalf("target = %v, want it held to the 2.75 ceiling and never under the %v guarantee", target, base)
				}
				return
			}
			if target <= 2.75 {
				t.Fatalf("target = %v, want a loan above the ceiling a capped job would have had", target)
			}
		})
	}
}

// A job with too few measured runs says nothing, and says so: it is lent to as
// before, and the verdict is counted as unknown.
func TestElasticCPUDoesNotCapAJobItHasNotSeenEnough(t *testing.T) {
	h := newHarness(t)
	target, _ := h.boostFor(scheduler.SizeOn, func(p *store.Pool) { h.historyRuns(p, 3, 2.1, 5) })
	if target <= 2.75 {
		t.Fatalf("target = %v: three runs capped a job, want a loan as before", target)
	}
}
