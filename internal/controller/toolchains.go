package controller

import (
	"context"
	"errors"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/eyupio/zoomies/internal/scheduler"
	"github.com/eyupio/zoomies/internal/store"
	"github.com/eyupio/zoomies/internal/toolscan"
)

// A toolchain scan reads every workflow every installation can see and says,
// per pool, which setup-python, setup-node, setup-go, setup-java and
// setup-dotnet versions its jobs will ask for. It is what lets a pool's cache
// be filled with those versions before the jobs arrive, rather than each
// ephemeral runner downloading them again.
//
// The result lives in memory. It is a reading of GitHub that the next scan
// replaces, not fleet state: nothing schedules from it, and a controller that
// restarts without it has lost a cache hint, not a record. Keeping it out of
// the store also keeps a scan -- the most GitHub-expensive thing the
// controller does on its own initiative -- from contending with the one
// writer.

// ToolchainDemand is one toolchain version a pool's jobs ask for.
type ToolchainDemand struct {
	Tool         string `json:"tool"`
	Version      string `json:"version,omitempty"`
	Distribution string `json:"distribution,omitempty"`
	// Unresolved says why the version is not known from the workflows; see
	// toolscan.Requirement.
	Unresolved string `json:"unresolved,omitempty"`
	// Jobs is how many workflow jobs ask for it, counting each matrix
	// combination once, and Repositories which repositories they are in.
	Jobs         int      `json:"jobs"`
	Repositories []string `json:"repositories"`
}

// ToolchainScanInstallation is how the scan went for one installation.
type ToolchainScanInstallation struct {
	InstallationID string `json:"installation_id"`
	Target         string `json:"target"`
	Repositories   int    `json:"repositories"`
	Workflows      int    `json:"workflows"`
	// Error is why this installation was not read, or only partly: GitHub
	// refusing it, a rate-limit hold, or an App without the Contents
	// permission, whose every repository would otherwise look empty.
	Error string `json:"error,omitempty"`
	// Unreadable lists repositories GitHub would not give up workflows for,
	// with the reason.
	Unreadable map[string]string `json:"unreadable,omitempty"`
}

// ToolchainScan is the latest scan.
type ToolchainScan struct {
	Running    bool       `json:"running"`
	StartedAt  *time.Time `json:"started_at,omitempty"`
	FinishedAt *time.Time `json:"finished_at,omitempty"`
	// Pools maps a pool ID to what its jobs ask for.
	Pools map[string][]ToolchainDemand `json:"pools"`
	// Unmatched is what jobs ask for that no pool would run: jobs on
	// GitHub-hosted labels, on labels no pool carries, or on labels an
	// expression chooses.
	Unmatched     []ToolchainDemand           `json:"unmatched"`
	Installations []ToolchainScanInstallation `json:"installations"`
}

// scannedRequirement is one requirement and the repository it was found in.
type scannedRequirement struct {
	installationID string
	repo           string
	req            toolscan.Requirement
}

// toolchainScans holds the latest scan and whether one is running.
type toolchainScans struct {
	mu      sync.Mutex
	running bool
	latest  ToolchainScan
}

// ErrToolchainScanRunning is returned when a scan is asked for while one is
// already under way; the running one's result is the answer.
var ErrToolchainScanRunning = errors.New("a toolchain scan is already running")

// ToolchainScanResult returns the latest scan, and whether one is running.
func (c *Controller) ToolchainScanResult() ToolchainScan {
	c.toolchains.mu.Lock()
	defer c.toolchains.mu.Unlock()
	out := c.toolchains.latest
	out.Running = c.toolchains.running
	if out.Pools == nil {
		out.Pools = map[string][]ToolchainDemand{}
	}
	if out.Unmatched == nil {
		out.Unmatched = []ToolchainDemand{}
	}
	if out.Installations == nil {
		out.Installations = []ToolchainScanInstallation{}
	}
	return out
}

// StartToolchainScan begins a scan in the background and returns at once. A
// scan reads every workflow file in every repository, which on a large
// organisation is minutes of GitHub calls: far longer than a request should
// hold a connection open, so the scan is detached from the request.
func (c *Controller) StartToolchainScan(ctx context.Context) error {
	return c.startToolchainScan(ctx, true)
}

// startToolchainScan runs a scan in the background, on ctx or detached from it.
func (c *Controller) startToolchainScan(ctx context.Context, detach bool) error {
	c.toolchains.mu.Lock()
	if c.toolchains.running {
		c.toolchains.mu.Unlock()
		return ErrToolchainScanRunning
	}
	c.toolchains.running = true
	c.toolchains.mu.Unlock()

	if detach {
		ctx = context.WithoutCancel(ctx)
	}
	go func() {
		scan := c.scanToolchains(ctx)
		c.toolchains.mu.Lock()
		c.toolchains.latest = scan
		c.toolchains.running = false
		c.toolchains.mu.Unlock()
	}()
	return nil
}

// scanToolchains is one scan, start to finish.
func (c *Controller) scanToolchains(ctx context.Context) ToolchainScan {
	started := c.Now()
	scan := ToolchainScan{StartedAt: &started}

	var found []scannedRequirement
	insts, err := c.st.ListInstallations(ctx)
	if err != nil {
		c.log.Warn("could not list installations to scan their workflows", "error", err)
	}
	for _, inst := range insts {
		if ctx.Err() != nil {
			break
		}
		if IsDemoID(inst.ID) {
			continue
		}
		report, reqs := c.scanInstallation(ctx, inst)
		scan.Installations = append(scan.Installations, report)
		found = append(found, reqs...)
	}

	pools, err := c.st.ListPools(ctx)
	if err != nil {
		c.log.Warn("could not list pools to match scanned workflows against", "error", err)
	}
	scan.Pools, scan.Unmatched = aggregateToolchains(pools, found)
	finished := c.Now()
	scan.FinishedAt = &finished
	c.log.Info("scanned workflows for the toolchains they install",
		"installations", len(scan.Installations), "pools", len(scan.Pools),
		"took", finished.Sub(started).Round(time.Second))
	return scan
}

// scanInstallation reads one installation's workflows.
func (c *Controller) scanInstallation(ctx context.Context, inst *store.Installation) (ToolchainScanInstallation, []scannedRequirement) {
	report := ToolchainScanInstallation{InstallationID: inst.ID, Target: inst.Target}
	now := c.Now()
	if c.githubHeld(inst.ID, now) {
		report.Error = "GitHub is rate-limiting this installation; it will be read on the next scan"
		return report, nil
	}
	client, err := c.clients.get(ctx, inst)
	if err != nil {
		report.Error = err.Error()
		return report, nil
	}
	// The same question the migration wizard asks first, for the same
	// reason: GitHub answers an App without Contents with a 404, exactly as
	// it answers a repository with no workflows, so without asking every
	// repository would read as needing nothing.
	if info, err := client.Probe(ctx); err == nil && !info.CanReadContents() {
		report.Error = "the GitHub App cannot read repository contents, so no workflow can be read; grant it Contents: read on the installation"
		return report, nil
	}
	repos, err := client.ListRepositories(ctx, 0)
	c.observeGitHub(inst.ID, err)
	if err != nil {
		c.holdRateLimited(inst.ID, err, now, "scanning workflows for toolchains")
		report.Error = err.Error()
		return report, nil
	}
	report.Repositories = len(repos)

	var out []scannedRequirement
	for _, src := range readWorkflows(ctx, client, repos) {
		if src.err != "" {
			if report.Unreadable == nil {
				report.Unreadable = map[string]string{}
			}
			report.Unreadable[src.repo.FullName] = src.err
			continue
		}
		for _, wf := range src.workflows {
			report.Workflows++
			reqs, err := toolscan.Scan(wf.Path, wf.Content)
			if err != nil {
				// A workflow GitHub itself would refuse to run asks for
				// nothing; it is not worth failing the repository over.
				continue
			}
			for _, r := range reqs {
				out = append(out, scannedRequirement{installationID: inst.ID, repo: src.repo.FullName, req: r})
			}
		}
	}
	return report, out
}

// aggregateToolchains decides which pool would run each job and totals what
// each pool's jobs ask for. It is the scan without GitHub, which is what makes
// it testable.
//
// A job goes to the pool the scheduler would give it -- scheduler.BestPool, on
// the same labels and installation a queued job would carry -- so the answer
// here and the pool a real job lands on cannot disagree.
func aggregateToolchains(pools []*store.Pool, found []scannedRequirement) (map[string][]ToolchainDemand, []ToolchainDemand) {
	type key struct{ pool, tool, version, distribution, unresolved string }
	type tally struct {
		jobs  map[string]bool
		repos map[string]bool
	}
	tallies := map[key]*tally{}
	for _, f := range found {
		pool := ""
		if f.req.Labels != nil {
			job := &store.Job{Repo: f.repo, Labels: store.StringSlice(f.req.Labels), InstallationID: f.installationID}
			if p := scheduler.BestPool(pools, job); p != nil {
				pool = p.ID
			}
		}
		k := key{pool, string(f.req.Tool), f.req.Version, f.req.Distribution, f.req.Unresolved}
		t := tallies[k]
		if t == nil {
			t = &tally{jobs: map[string]bool{}, repos: map[string]bool{}}
			tallies[k] = t
		}
		// A job is its repository, file, key and labels: the same job with
		// two matrix combinations asking for one version counts once per
		// combination's labels, which is how many runners it takes.
		t.jobs[f.repo+"\x00"+f.req.Workflow+"\x00"+f.req.Job+"\x00"+strings.Join(f.req.Labels, ",")] = true
		t.repos[f.repo] = true
	}

	byPool := map[string][]ToolchainDemand{}
	var unmatched []ToolchainDemand
	for k, t := range tallies {
		repos := make([]string, 0, len(t.repos))
		for r := range t.repos {
			repos = append(repos, r)
		}
		sort.Strings(repos)
		d := ToolchainDemand{Tool: k.tool, Version: k.version, Distribution: k.distribution,
			Unresolved: k.unresolved, Jobs: len(t.jobs), Repositories: repos}
		if k.pool == "" {
			unmatched = append(unmatched, d)
		} else {
			byPool[k.pool] = append(byPool[k.pool], d)
		}
	}
	for id := range byPool {
		sortDemand(byPool[id])
	}
	sortDemand(unmatched)
	return byPool, unmatched
}

// sortDemand orders by tool, then the most-asked-for first, then version, so
// the list reads as "what matters most for each toolchain".
func sortDemand(ds []ToolchainDemand) {
	sort.Slice(ds, func(i, j int) bool {
		a, b := ds[i], ds[j]
		if a.Tool != b.Tool {
			return a.Tool < b.Tool
		}
		if a.Jobs != b.Jobs {
			return a.Jobs > b.Jobs
		}
		if a.Version != b.Version {
			return a.Version < b.Version
		}
		return a.Unresolved < b.Unresolved
	})
}
