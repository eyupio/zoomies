package controller

import (
	"errors"
	"reflect"
	"strconv"
	"testing"
	"time"

	"github.com/eyupio/zoomies/internal/config"
	"github.com/eyupio/zoomies/internal/store"
	"github.com/eyupio/zoomies/internal/toolscan"
)

// demandBrief renders what a pool's jobs ask for as "tool version xN" lines,
// which is what these tests compare.
func demandBrief(ds []ToolchainDemand) []string {
	var out []string
	for _, d := range ds {
		v := d.Version
		if d.Unresolved != "" {
			v = "? " + d.Unresolved
		}
		out = append(out, d.Tool+" "+v+" x"+strconv.Itoa(d.Jobs))
	}
	return out
}

// The pool a scanned job is counted against has to be the one the scheduler
// would give that job when it is queued -- otherwise the pool's cache is
// filled for jobs another pool runs.
func TestAggregateToolchainsGivesEachJobThePoolTheSchedulerWould(t *testing.T) {
	x64 := &store.Pool{ID: "pool_x64", Name: "x64", InstallationID: "ins_1", Enabled: true, Labels: []string{"linux", "x64"}}
	arm := &store.Pool{ID: "pool_arm", Name: "arm", InstallationID: "ins_1", Enabled: true, Labels: []string{"linux", "arm64"}}
	req := func(repo, job string, labels []string, tool toolscan.Tool, version string) scannedRequirement {
		return scannedRequirement{installationID: "ins_1", repo: repo,
			req: toolscan.Requirement{Workflow: ".github/workflows/ci.yml", Job: job, Labels: labels, Tool: tool, Version: version}}
	}
	found := []scannedRequirement{
		req("acme/a", "test", []string{"linux", "x64"}, toolscan.Python, "3.12"),
		req("acme/b", "test", []string{"linux", "x64"}, toolscan.Python, "3.12"),
		req("acme/b", "lint", []string{"linux", "x64"}, toolscan.Node, "22"),
		req("acme/a", "arm", []string{"linux", "arm64"}, toolscan.Go, "1.27"),
		req("acme/a", "hosted", []string{"ubuntu-latest"}, toolscan.Python, "3.13"),
		req("acme/a", "dynamic", nil, toolscan.Python, "3.11"),
	}
	pools, unmatched := aggregateToolchains([]*store.Pool{x64, arm}, found)

	if got, want := demandBrief(pools["pool_x64"]), []string{"node 22 x1", "python 3.12 x2"}; !reflect.DeepEqual(got, want) {
		t.Errorf("x64 pool: got %q, want %q", got, want)
	}
	if got := pools["pool_x64"][1].Repositories; !reflect.DeepEqual(got, []string{"acme/a", "acme/b"}) {
		t.Errorf("x64 python repositories = %q", got)
	}
	if got, want := demandBrief(pools["pool_arm"]), []string{"go 1.27 x1"}; !reflect.DeepEqual(got, want) {
		t.Errorf("arm pool: got %q, want %q", got, want)
	}
	// A hosted label no pool carries, and labels an expression chooses, are
	// not guessed onto a pool.
	if got, want := demandBrief(unmatched), []string{"python 3.11 x1", "python 3.13 x1"}; !reflect.DeepEqual(got, want) {
		t.Errorf("unmatched: got %q, want %q", got, want)
	}
}

func TestToolchainScanReadsTheWorkflowsOfEveryRepository(t *testing.T) {
	h := newHarness(t)
	inst := h.installation()
	pool := h.pool(inst, "linux-x64", "zoomies-linux-x64")
	h.gh.AddWorkflow("acme/widgets", ".github/workflows/ci.yml", `
jobs:
  test:
    runs-on: zoomies-linux-x64
    strategy:
      matrix:
        python: ["3.12", "3.13"]
    steps:
      - uses: actions/setup-python@v6
        with:
          python-version: ${{ matrix.python }}
  hosted:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/setup-node@v5
        with:
          node-version: "24"
`)
	h.gh.AddWorkflow("acme/gadgets", ".github/workflows/build.yml", `
jobs:
  build:
    runs-on: zoomies-linux-x64
    steps:
      - uses: actions/setup-go@v6
        with:
          go-version-file: go.mod
`)

	scan := h.c.scanToolchains(h.ctx)

	want := []string{"go ? read from go.mod in the repository x1", "python 3.12 x1", "python 3.13 x1"}
	if got := demandBrief(scan.Pools[pool.ID]); !reflect.DeepEqual(got, want) {
		t.Errorf("pool demand: got %q, want %q", got, want)
	}
	if got := demandBrief(scan.Unmatched); !reflect.DeepEqual(got, []string{"node 24 x1"}) {
		t.Errorf("unmatched: got %q", got)
	}
	if len(scan.Installations) != 1 || scan.Installations[0].Workflows != 2 || scan.Installations[0].Error != "" {
		t.Errorf("installation report = %+v", scan.Installations)
	}
	if scan.StartedAt == nil || scan.FinishedAt == nil {
		t.Error("a finished scan says when it ran")
	}
}

// GitHub answers an App without Contents exactly as it answers a repository
// with no workflows. Without asking first, the scan would report an
// organisation that needs nothing, when the truth is that it could not look.
func TestToolchainScanSaysWhenTheAppCannotReadWorkflows(t *testing.T) {
	h := newHarness(t)
	inst := h.installation()
	h.pool(inst, "linux-x64", "zoomies-linux-x64")
	h.gh.AddWorkflow("acme/widgets", ".github/workflows/ci.yml", "jobs: {}\n")
	h.gh.SetPermissions(map[string]string{"metadata": "read", "organization_self_hosted_runners": "write"})

	scan := h.c.scanToolchains(h.ctx)

	if len(scan.Installations) != 1 || scan.Installations[0].Error == "" {
		t.Fatalf("installation report = %+v, want an error naming the missing permission", scan.Installations)
	}
	if len(scan.Pools) != 0 {
		t.Errorf("pools = %+v, want nothing from an installation that was not read", scan.Pools)
	}
}

// A scan is minutes of GitHub calls on a large organisation; a second one
// started beside it would spend the same quota twice for the same answer.
func TestOnlyOneToolchainScanRunsAtATime(t *testing.T) {
	h := newHarness(t)
	h.c.toolchains.mu.Lock()
	h.c.toolchains.running = true
	h.c.toolchains.mu.Unlock()

	if err := h.c.StartToolchainScan(h.ctx); !errors.Is(err, ErrToolchainScanRunning) {
		t.Fatalf("StartToolchainScan = %v, want ErrToolchainScanRunning", err)
	}
	if !h.c.ToolchainScanResult().Running {
		t.Error("the result does not say a scan is running")
	}
}

// With an interval set, the background loop scans on its own; left at 0 it
// never spends the quota a scan costs unless somebody asks.
func TestHousekeepingScansToolchainsOnlyWhenAnIntervalIsSet(t *testing.T) {
	h := newHarness(t)
	inst := h.installation()
	pool := h.pool(inst, "linux-x64", "zoomies-linux-x64")
	h.gh.AddWorkflow("acme/widgets", ".github/workflows/ci.yml", `
jobs:
  test:
    runs-on: zoomies-linux-x64
    steps:
      - uses: actions/setup-node@v5
        with:
          node-version: "24"
`)

	h.c.live.Update(func(c *config.Config) { c.GitHub.ToolchainScanInterval = 0 })
	h.c.housekeep(h.ctx, &housekeeping{})
	if r := h.c.ToolchainScanResult(); r.Running || r.StartedAt != nil {
		t.Fatalf("a scan ran with the interval at 0: %+v", r)
	}

	h.c.live.Update(func(c *config.Config) { c.GitHub.ToolchainScanInterval = 24 * time.Hour })
	h.c.housekeep(h.ctx, &housekeeping{})
	deadline := time.Now().Add(10 * time.Second)
	for {
		r := h.c.ToolchainScanResult()
		if !r.Running && r.FinishedAt != nil {
			if got := demandBrief(r.Pools[pool.ID]); !reflect.DeepEqual(got, []string{"node 24 x1"}) {
				t.Fatalf("scheduled scan found %q", got)
			}
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("the scheduled scan did not finish")
		}
		time.Sleep(10 * time.Millisecond)
	}
}
