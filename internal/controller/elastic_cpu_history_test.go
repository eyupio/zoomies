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
func (h *harness) historyRuns(pool *store.Pool, job string, n int, peak float64, throttled int64) {
	h.t.Helper()
	at := h.c.Now().Add(-48 * time.Hour)
	for i := range n {
		runner := fmt.Sprintf("run_hist_%s_%d_%d", job, int64(peak*100), i)
		start := at.Add(time.Duration(i) * time.Hour)
		j := &store.Job{
			GitHubJobID: int64(9000 + i + int(peak*1000) + 100*len(job)), Repo: "acme/widgets", Workflow: "CI", JobName: job,
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
			target, base := h.boostFor(tc.mode, func(p *store.Pool) { h.historyRuns(p, "unit", 6, 2.1, tc.throttled) })
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
	target, _ := h.boostFor(scheduler.SizeOn, func(p *store.Pool) { h.historyRuns(p, "unit", 3, 2.1, 5) })
	if target <= 2.75 {
		t.Fatalf("target = %v: three runs capped a job, want a loan as before", target)
	}
}

// busyRunner is a busy runner of the pool running the named job, guaranteed two
// cores, as a host-sized automatic pool would have launched it.
func (h *harness) busyRunner(pool *store.Pool, host *store.Host, ghID int64, job string) *store.Runner {
	h.t.Helper()
	r := h.runnerRow(pool, host, store.RunnerBusy)
	r.AllocatedCPUs = 2
	r.AllocationSource = store.AllocationFromHost
	if err := h.st.UpdateRunner(h.ctx, r); err != nil {
		h.t.Fatal(err)
	}
	start := h.c.Now()
	if _, _, err := h.st.ApplyJob(h.ctx, &store.Job{
		GitHubJobID: ghID, Repo: "acme/widgets", Workflow: "CI", JobName: job, Labels: store.StringSlice{"self-hosted"},
		State: store.JobInProgress, QueuedAt: start, StartedAt: &start, Matched: true, PoolID: pool.ID, RunnerID: r.ID,
	}); err != nil {
		h.t.Fatal(err)
	}
	return r
}

// plan is what elastic CPU tells the host's agent for runners reporting the CPU
// given, in percent of one core, keyed by runner.
func (h *harness) plan(host *store.Host, mode string, use map[string]float64) map[string]float64 {
	h.t.Helper()
	h.c.live.Update(func(c *config.Config) { c.Scheduler.ElasticCPUHistory = mode })
	now := h.c.Now()
	cpu := 20.0
	host.Usage = store.HostUsage{CPUPercent: &cpu, SampledAt: now}
	sampled := now
	var reports []agent.RunnerReport
	for id, pct := range use {
		reports = append(reports, agent.RunnerReport{RunnerID: id, Stats: backend.Stats{SampledAt: &sampled, CPUPercent: pct}})
	}
	out := map[string]float64{}
	for _, d := range h.c.elasticCPUTargets(h.ctx, host, agent.HeartbeatRequest{
		Features: []string{agent.FeatureElasticCPU}, Runners: reports,
	}, now) {
		out[d.RunnerID] = d.TargetCPUs
	}
	return out
}

func (h *harness) automaticPool(pool *store.Pool) {
	h.t.Helper()
	pool.CPUBurst = store.CPUBurstPolicy{Mode: store.CPUBurstAutomatic}
	if err := h.st.UpdatePool(h.ctx, pool); err != nil {
		h.t.Fatal(err)
	}
}

// A job whose history says it scales is lent CPU while it is using half its
// guarantee, where one nothing is known about waits for four fifths. The
// history was always going to be read when the job started; what is new is
// that it is acted on before the job has filled what it has.
func TestElasticCPULendsEarlyToAJobKnownToScale(t *testing.T) {
	for _, tc := range []struct {
		name     string
		mode     string
		runs     int
		throttle int64
		want     bool
	}{
		{"on, held back in most runs", scheduler.SizeOn, 6, 400, true},
		{"shadow counts it and lends nothing yet", scheduler.SizeShadow, 6, 400, false},
		{"off lends nothing yet", scheduler.SizeOff, 6, 400, false},
		{"on, too few runs to know", scheduler.SizeOn, 3, 400, false},
		{"on, never held back", scheduler.SizeOn, 6, 5, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := newHarness(t)
			_, pool, host := h.fleet()
			h.automaticPool(pool)
			h.historyRuns(pool, "unit", tc.runs, 2.1, tc.throttle)
			r := h.busyRunner(pool, host, 77001, "unit")
			// 120% of one core is 60% of a two core guarantee: over half of it, and
			// short of the 160% an unknown job is lent at.
			got := h.plan(host, tc.mode, map[string]float64{r.ID: 120})
			if lent := got[r.ID] > 2.01; lent != tc.want {
				t.Fatalf("target = %v, lent = %v, want lent = %v", got[r.ID], lent, tc.want)
			}
		})
	}
}

// When there is not enough spare CPU for every demanding runner, the job known
// to scale takes the larger share, in proportion to how often it was held back.
func TestElasticCPUGivesAJobKnownToScaleTheLargerShare(t *testing.T) {
	h := newHarness(t)
	_, pool, host := h.fleet()
	host.CPUs = 8
	if err := h.st.UpdateHost(h.ctx, host); err != nil {
		t.Fatal(err)
	}
	h.automaticPool(pool)
	h.historyRuns(pool, "unit", 6, 2.1, 900) // held back in every run: weight 2
	scaler := h.busyRunner(pool, host, 77001, "unit")
	plain := h.busyRunner(pool, host, 77002, "lint") // nothing known
	use := map[string]float64{scaler.ID: 400, plain.ID: 400}

	equal := h.plan(host, scheduler.SizeShadow, use)
	if d := equal[scaler.ID] - equal[plain.ID]; d > 0.02 || d < -0.02 {
		t.Fatalf("shadow targets = %v, want an equal split until the setting is on", equal)
	}
	weighted := h.plan(host, scheduler.SizeOn, use)
	gotScaler, gotPlain := weighted[scaler.ID]-2, weighted[plain.ID]-2
	if gotPlain <= 0 || gotScaler < 1.9*gotPlain || gotScaler > 2.1*gotPlain {
		t.Fatalf("targets = %v, want the scaling job's loan (%.2f) about twice the other's (%.2f)", weighted, gotScaler, gotPlain)
	}
}
