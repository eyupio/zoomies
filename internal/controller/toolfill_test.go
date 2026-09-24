package controller

import (
	"reflect"
	"strconv"
	"testing"
	"time"

	"github.com/eyupio/zoomies/internal/agent"
	"github.com/eyupio/zoomies/internal/backend"
	"github.com/eyupio/zoomies/internal/store"
)

// hostWith seeds a healthy docker host whose agent advertises features.
func (h *harness) hostWith(name string, features ...string) *store.Host {
	h.t.Helper()
	host := &store.Host{
		Name: name, Capacity: 4, Backends: store.StringSlice{"docker"}, Labels: store.StringMap{},
		OS: "linux", Arch: "amd64", CPUs: 16, MemoryMB: 64 * 1024, DiskTotalMB: 500 * 1024, DiskFreeMB: 400 * 1024,
		LastHeartbeat: time.Now(), Features: store.StringSlice(features),
	}
	if err := h.st.CreateHost(h.ctx, host); err != nil {
		h.t.Fatalf("CreateHost: %v", err)
	}
	return host
}

func (h *harness) keepToolCache(p *store.Pool) {
	h.t.Helper()
	p.Cache = store.CacheConfig{Enabled: true, Tools: true, Scope: store.CacheScopePool}
	if err := h.st.UpdatePool(h.ctx, p); err != nil {
		h.t.Fatalf("UpdatePool: %v", err)
	}
}

// A scan is what a pool's jobs ask for; a fill is that put in the cache the
// pool's runners are given. Only a pool that keeps a tool cache has one to
// fill, and only an agent that understands the task is sent it -- an older
// one refuses the kind, which would read as a fill failing on every host
// that has not upgraded yet.
func TestAScanFillsThePoolsThatKeepAToolCacheOnHostsThatCanRunThem(t *testing.T) {
	h := newHarness(t)
	inst := h.installation()
	kept := h.pool(inst, "kept", "zoomies-kept")
	h.keepToolCache(kept)
	plain := h.pool(inst, "plain", "zoomies-plain")
	current := h.hostWith("current", agent.FeatureToolCacheFill)
	older := h.hostWith("older")

	demand := []ToolchainDemand{{Tool: "python", Version: "3.12", Jobs: 3}, {Tool: "node", Version: "22", Jobs: 5}}
	scan := ToolchainScan{Pools: map[string][]ToolchainDemand{kept.ID: demand, plain.ID: demand}}
	if n := h.c.fillToolCaches(h.ctx, scan); n != 1 {
		t.Fatalf("fillToolCaches queued %d fills, want 1", n)
	}
	if h.hasTaskOfKind(older.ID, agent.TaskFillToolCache) {
		t.Error("a fill was sent to an agent that does not advertise it")
	}
	task := h.taskOfKind(current.ID, agent.TaskFillToolCache)
	if task.PoolID != kept.ID || task.Spec == nil || task.Spec.PoolID != kept.ID || !task.Spec.Cache.Tools {
		t.Fatalf("task = %+v, want the kept pool's cache", task)
	}
	if task.Spec.Image != h.c.RunnerImage(kept) {
		t.Errorf("image = %q, want the one the pool's runners use", task.Spec.Image)
	}
	want := []backend.ToolRequest{{Tool: "node", Version: "22"}, {Tool: "python", Version: "3.12"}}
	if !reflect.DeepEqual(task.Tools, want) {
		t.Errorf("tools = %+v, want the most-asked-for first: %+v", task.Tools, want)
	}

	fills := h.c.ToolchainScanResult().Fills[kept.ID]
	if len(fills) != 1 || fills[0].State != "pending" || fills[0].HostName != "current" || fills[0].Requested != 2 {
		t.Fatalf("fills = %+v, want one pending fill on current", fills)
	}

	// A second scan while that fill is still downloading leaves it be.
	if n := h.c.fillToolCaches(h.ctx, scan); n != 0 {
		t.Errorf("a second scan queued %d more fills behind the running one", n)
	}

	batch, err := h.c.PollTasks(h.ctx, current.ID, 0)
	if err != nil || len(batch.Tasks) == 0 {
		t.Fatalf("PollTasks = %v, %v", batch, err)
	}
	result := []backend.ToolFill{
		{Tool: "node", Request: "22", Outcome: backend.ToolInstalled, Version: "22.11.0"},
		{Tool: "python", Request: "3.12", Outcome: backend.ToolFailed, Reason: "could not download it"},
	}
	if err := h.c.ReportResult(h.ctx, current.ID, agent.TaskResult{TaskID: task.ID, Kind: task.Kind, OK: true, ToolFills: result, CompletedAt: h.c.Now()}); err != nil {
		t.Fatalf("ReportResult: %v", err)
	}
	fills = h.c.ToolchainScanResult().Fills[kept.ID]
	if len(fills) != 1 || fills[0].State != "succeeded" || fills[0].FinishedAt == nil || !reflect.DeepEqual(fills[0].Tools, result) {
		t.Fatalf("fills after the report = %+v", fills)
	}
}

func TestAFailedFillSaysWhy(t *testing.T) {
	h := newHarness(t)
	inst := h.installation()
	p := h.pool(inst, "kept", "zoomies-kept")
	h.keepToolCache(p)
	host := h.hostWith("current", agent.FeatureToolCacheFill)
	h.c.fillToolCaches(h.ctx, ToolchainScan{Pools: map[string][]ToolchainDemand{p.ID: {{Tool: "go", Version: "1.27", Jobs: 1}}}})
	task := h.taskOfKind(host.ID, agent.TaskFillToolCache)
	if _, err := h.c.PollTasks(h.ctx, host.ID, 0); err != nil {
		t.Fatal(err)
	}
	if err := h.c.ReportResult(h.ctx, host.ID, agent.TaskResult{TaskID: task.ID, Kind: task.Kind, OK: false, Error: "backend: pulling the image: denied"}); err != nil {
		t.Fatal(err)
	}
	f := h.c.ToolchainScanResult().Fills[p.ID]
	if len(f) != 1 || f[0].State != "failed" || f[0].Error != "backend: pulling the image: denied" {
		t.Fatalf("fills = %+v", f)
	}
	// A fill is not a runner, so its failure fails none.
	if len(h.runners()) != 0 {
		t.Errorf("runners = %v, want none", h.runners())
	}
}

// A fill acts only on what it can put in a tool cache, and on the versions
// most jobs ask for when there are more than it is worth fetching on every
// host.
func TestToolRequestsKeepWhatAFillCanActOnMostAskedFirst(t *testing.T) {
	demand := []ToolchainDemand{
		{Tool: "go", Unresolved: "read from go.mod in the repository", Jobs: 50},
		{Tool: "dotnet", Version: "8.0", Jobs: 40},
		{Tool: "java", Version: "21", Distribution: "temurin", Jobs: 2},
		{Tool: "python", Version: "3.12", Jobs: 9},
	}
	got := toolRequests(demand)
	want := []backend.ToolRequest{{Tool: "python", Version: "3.12"}, {Tool: "java", Version: "21", Distribution: "temurin"}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("toolRequests = %+v, want %+v", got, want)
	}

	var many []ToolchainDemand
	for i := range 20 {
		many = append(many, ToolchainDemand{Tool: "node", Version: strconv.Itoa(10 + i), Jobs: i})
	}
	got = toolRequests(many)
	if len(got) != maxToolFillRequests || got[0].Version != "29" {
		t.Errorf("toolRequests over the cap = %d, first %+v; want %d starting with the most-asked-for", len(got), got[0], maxToolFillRequests)
	}
}
