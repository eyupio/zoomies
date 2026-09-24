package controller

import (
	"context"
	"sort"
	"strings"
	"time"

	"github.com/eyupio/zoomies/internal/agent"
	"github.com/eyupio/zoomies/internal/backend"
	"github.com/eyupio/zoomies/internal/scheduler"
	"github.com/eyupio/zoomies/internal/store"
)

// A finished toolchain scan is turned into work: every pool that keeps a tool
// cache is sent the versions its jobs ask for, to each host that could run
// it, and each host's agent fills its copy of that cache ahead of the jobs.
//
// Which fills ran, and how each went, lives beside the scan in memory, for
// the same reason the scan does: it describes a cache the next fill brings
// up to date, and losing it on a restart loses a report, not a record.

// maxToolFillRequests caps what one pool is sent. A pool whose jobs ask for
// forty versions between them wants the ones most of them ask for, not an
// hour of downloads on every host for the long tail.
const maxToolFillRequests = 12

// ToolCacheFill is one pool's fill on one host.
type ToolCacheFill struct {
	HostID   string `json:"host_id"`
	HostName string `json:"host_name"`
	// State is pending while the agent works, then succeeded or failed. A
	// fill that succeeded may still have skipped or failed requests; Tools
	// says which.
	State      string             `json:"state"`
	Requested  int                `json:"requested"`
	Tools      []backend.ToolFill `json:"tools"`
	Error      string             `json:"error,omitempty"`
	QueuedAt   time.Time          `json:"queued_at"`
	FinishedAt *time.Time         `json:"finished_at,omitempty"`
}

// fillToolCaches sends every pool that keeps a tool cache the versions scan
// found its jobs asking for, and returns how many fills it queued.
func (c *Controller) fillToolCaches(ctx context.Context, scan ToolchainScan) int {
	pools, err := c.st.ListPools(ctx)
	if err != nil {
		c.log.Warn("could not list pools to fill their tool caches", "error", err)
		return 0
	}
	hosts, err := c.st.ListHosts(ctx)
	if err != nil {
		c.log.Warn("could not list hosts to fill tool caches on", "error", err)
		return 0
	}
	now := c.Now()
	queued := 0
	for _, p := range pools {
		if !p.Enabled || !p.Cache.Enabled || !p.Cache.Tools || p.Backend == store.BackendProcess {
			continue
		}
		tools := toolRequests(scan.Pools[p.ID])
		if len(tools) == 0 {
			continue
		}
		spec, err := c.toolFillSpec(ctx, p)
		if err != nil {
			c.log.Warn("could not describe a pool to fill its tool cache", "pool", p.Name, "error", err)
			continue
		}
		for _, h := range hosts {
			// An older agent refuses the task outright; asking it would
			// only put a failure on every host that has not upgraded.
			if !scheduler.HostCanRun(h, p, now) || !h.Supports(agent.FeatureToolCacheFill) {
				continue
			}
			task := agent.Task{Kind: agent.TaskFillToolCache, PoolID: p.ID, Backend: p.Backend,
				Spec: &spec, Tools: tools, IssuedAt: now}
			if !c.enqueue(h.ID, task) {
				continue
			}
			queued++
			c.noteToolFill(p.ID, ToolCacheFill{HostID: h.ID, HostName: h.Name, State: "pending",
				Requested: len(tools), Tools: []backend.ToolFill{}, QueuedAt: now})
		}
	}
	if queued > 0 {
		c.log.Info("asked hosts to fill their pools' tool caches", "fills", queued)
	}
	return queued
}

// toolFillSpec is what a fill needs of the pool: the parts of a runner's spec
// that decide its image, its way out to the internet and which tool cache it
// is given. It has to name the same folder a runner of the pool is bound, or
// the fill warms a cache no job reads.
func (c *Controller) toolFillSpec(ctx context.Context, p *store.Pool) (backend.Spec, error) {
	target := ""
	if p.InstallationID != "" {
		inst, err := c.st.GetInstallation(ctx, p.InstallationID)
		if err != nil {
			return backend.Spec{}, err
		}
		target = inst.Target
	}
	return backend.Spec{
		PoolID:     p.ID,
		PoolName:   p.Name,
		Image:      c.RunnerImage(p),
		PullPolicy: p.PullPolicy,
		Env:        runnerEnv(c.cfg().Runners, p),
		Cache:      p.Cache,
		Repository: firstNonEmpty(strings.TrimSpace(p.Cache.Repository), target),
		Network:    c.cfg().Agent.Network,
	}, nil
}

// toolRequests is what a fill can act on, most-asked-for first: a version the
// workflows state, of a toolchain the tool cache holds. A version only a run
// knows -- read from a file, passed as an input -- has nothing to fill yet.
func toolRequests(demand []ToolchainDemand) []backend.ToolRequest {
	var ds []ToolchainDemand
	for _, d := range demand {
		if d.Unresolved != "" || d.Version == "" || strings.ContainsAny(d.Version, " \t\n") {
			continue
		}
		switch d.Tool {
		case "python", "node", "go", "java":
			ds = append(ds, d)
		}
	}
	sort.SliceStable(ds, func(i, j int) bool { return ds[i].Jobs > ds[j].Jobs })
	if len(ds) > maxToolFillRequests {
		ds = ds[:maxToolFillRequests]
	}
	out := make([]backend.ToolRequest, 0, len(ds))
	for _, d := range ds {
		out = append(out, backend.ToolRequest{Tool: d.Tool, Version: d.Version, Distribution: d.Distribution})
	}
	return out
}

// noteToolFill records a fill's latest state for its pool and host.
func (c *Controller) noteToolFill(poolID string, f ToolCacheFill) {
	c.toolchains.mu.Lock()
	defer c.toolchains.mu.Unlock()
	if c.toolchains.fills == nil {
		c.toolchains.fills = map[string]map[string]ToolCacheFill{}
	}
	if c.toolchains.fills[poolID] == nil {
		c.toolchains.fills[poolID] = map[string]ToolCacheFill{}
	}
	c.toolchains.fills[poolID][f.HostID] = f
}

// recordToolFill is an agent's answer to a fill.
func (c *Controller) recordToolFill(hostID string, task agent.Task, res agent.TaskResult) {
	c.toolchains.mu.Lock()
	f, ok := c.toolchains.fills[task.PoolID][hostID]
	c.toolchains.mu.Unlock()
	if !ok {
		f = ToolCacheFill{HostID: hostID, Requested: len(task.Tools), QueuedAt: task.IssuedAt}
		if h, err := c.st.GetHost(context.Background(), hostID); err == nil {
			f.HostName = h.Name
		}
	}
	f.State = "succeeded"
	if !res.OK {
		f.State = "failed"
	}
	f.Error = res.Error
	f.Tools = res.ToolFills
	if f.Tools == nil {
		f.Tools = []backend.ToolFill{}
	}
	done := res.CompletedAt
	if done.IsZero() {
		done = c.Now()
	}
	f.FinishedAt = &done
	c.noteToolFill(task.PoolID, f)
	if !res.OK {
		c.log.Warn("a host could not fill a pool's tool cache", "host", hostID, "pool", task.PoolID, "error", res.Error)
	}
}

// toolFillsByPool is the fills as the API shows them: per pool, by host name.
// The caller holds c.toolchains.mu.
func (c *Controller) toolFillsByPool() map[string][]ToolCacheFill {
	out := make(map[string][]ToolCacheFill, len(c.toolchains.fills))
	for pool, byHost := range c.toolchains.fills {
		list := make([]ToolCacheFill, 0, len(byHost))
		for _, f := range byHost {
			list = append(list, f)
		}
		sort.Slice(list, func(i, j int) bool {
			if list[i].HostName != list[j].HostName {
				return list[i].HostName < list[j].HostName
			}
			return list[i].HostID < list[j].HostID
		})
		out[pool] = list
	}
	return out
}
