package controller

import (
	"context"
	"time"

	"github.com/eyupio/zoomies/internal/scheduler"
	"github.com/eyupio/zoomies/internal/store"
)

// cpuDemandEntry is one runner's job and what that job's history said, kept for
// as long as the runner holds the job.
type cpuDemandEntry struct {
	hostID string
	jobID  string
	demand scheduler.CPUDemand
}

// jobCPUDemand reads what the history of the job a busy runner holds says about
// its CPU, and says whether it could be read at all.
//
// It is read once per job. The first heartbeat that finds the runner busy on a
// job reads the job's recent runs, profiles them and keeps the answer until the
// runner takes another job or goes, so the planner, which runs on every
// heartbeat of every host, does one map lookup for a job it has seen.
//
// The runner is anonymous until it takes a job: GitHub, not Zoomies, hands a
// queued job to an idle runner. So this cannot size a runner for the job it will
// get, only say what a runner already holding one has shown a job of that name to
// do, which is why it is elastic CPU that can use it and creation cannot.
func (c *Controller) jobCPUDemand(ctx context.Context, r *store.Runner, p *store.Pool, mode string, now time.Time) (scheduler.CPUDemand, bool) {
	jobs, err := c.st.ListRunningJobsForRunner(ctx, r.ID)
	if err != nil || len(jobs) == 0 {
		return scheduler.CPUDemand{}, false
	}
	j := jobs[0]
	c.cpuDemandMu.Lock()
	if e, ok := c.cpuDemand[r.ID]; ok && e.jobID == j.ID {
		c.cpuDemandMu.Unlock()
		return e.demand, true
	}
	c.cpuDemandMu.Unlock()

	key := store.JobUsageKey{Repo: j.Repo, Workflow: j.Workflow, JobName: j.JobName}
	history, err := c.st.JobUsageHistory(ctx, []store.JobUsageKey{key})
	if err != nil {
		c.log.Debug("could not read a job's history for elastic CPU", "runner", r.ID, "error", err)
		return scheduler.CPUDemand{}, false
	}
	key.PoolID = r.PoolID
	demand := scheduler.ProfileCPU(history[key])

	c.cpuDemandMu.Lock()
	if c.cpuDemand == nil {
		c.cpuDemand = map[string]cpuDemandEntry{}
	}
	c.cpuDemand[r.ID] = cpuDemandEntry{hostID: r.HostID, jobID: j.ID, demand: demand}
	c.cpuDemandMu.Unlock()
	c.metrics.elasticCPUJobHistory.WithLabelValues(p.Name, mode, demand.Verdict).Inc()
	c.log.Debug("elastic CPU read a job's history", "runner", r.ID, "job", j.JobName, "verdict", demand.Verdict,
		"runs", demand.Runs, "bound", demand.Bound, "ceiling", demand.Ceiling, "mode", mode)
	return demand, true
}

// forgetCPUDemand drops what was read for every runner of a host that is not
// busy now: an idle runner's next job is a different job.
func (c *Controller) forgetCPUDemand(hostID string, busy map[string]bool) {
	c.cpuDemandMu.Lock()
	defer c.cpuDemandMu.Unlock()
	for id, e := range c.cpuDemand {
		if e.hostID == hostID && !busy[id] {
			delete(c.cpuDemand, id)
		}
	}
}
