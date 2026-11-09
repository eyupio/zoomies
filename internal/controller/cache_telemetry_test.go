package controller

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/prometheus/client_golang/prometheus/testutil"

	"github.com/eyupio/zoomies/internal/agent"
	"github.com/eyupio/zoomies/internal/backend"
	"github.com/eyupio/zoomies/internal/cacheservice"
)

func cacheStats(pool string, s cacheservice.Stats) backend.CacheStats {
	return backend.CacheStats{PoolID: pool, Stats: s}
}

// What a host's cache services report on its heartbeat is exposed as the
// totals the agent keeps, labelled by the pool's name and the host's id, and
// the pool and the host are what a restart or a deletion is told apart by.
func TestACacheReportOnAHeartbeatBecomesSeriesLabelledByPoolAndHost(t *testing.T) {
	h := newHarness(t)
	inst := h.installation()
	pool := h.pool(inst, "zoomies-linux-x64")
	one, two := h.hostWith("one"), h.hostWith("two")

	beat := func(host string, caches ...backend.CacheStats) {
		t.Helper()
		if _, err := h.c.Heartbeat(h.ctx, host, agent.HeartbeatRequest{Caches: caches}); err != nil {
			t.Fatalf("Heartbeat: %v", err)
		}
	}
	beat(one.ID, cacheStats(pool.ID, cacheservice.Stats{
		RestoreHits: 7, RestoreMisses: 3, PutsStarted: 5, PutsFinalised: 3, PutsDuplicate: 1, PutsFailed: 1,
		Evictions: 2, EvictedBytes: 200, Entries: 4, StoredBytes: 4096,
	}))
	beat(two.ID, cacheStats(pool.ID, cacheservice.Stats{RestoreHits: 1, StoredBytes: 10}))

	for _, tt := range []struct {
		name   string
		series string
		labels map[string]string
		want   float64
	}{
		{"hits", "zoomies_cache_restores_total", map[string]string{"pool": pool.Name, "host": one.ID, "result": "hit"}, 7},
		{"misses", "zoomies_cache_restores_total", map[string]string{"pool": pool.Name, "host": one.ID, "result": "miss"}, 3},
		{"puts started", "zoomies_cache_puts_total", map[string]string{"pool": pool.Name, "host": one.ID, "outcome": "started"}, 5},
		{"puts finalised", "zoomies_cache_puts_total", map[string]string{"pool": pool.Name, "host": one.ID, "outcome": "finalised"}, 3},
		{"puts duplicate", "zoomies_cache_puts_total", map[string]string{"pool": pool.Name, "host": one.ID, "outcome": "duplicate"}, 1},
		{"puts failed", "zoomies_cache_puts_total", map[string]string{"pool": pool.Name, "host": one.ID, "outcome": "failed"}, 1},
		{"evictions", "zoomies_cache_evictions_total", map[string]string{"pool": pool.Name, "host": one.ID}, 2},
		{"stored bytes", "zoomies_cache_stored_bytes", map[string]string{"pool": pool.Name, "host": one.ID}, 4096},
		// Another host's totals are its own series, not added to the first's.
		{"the other host's hits", "zoomies_cache_restores_total", map[string]string{"pool": pool.Name, "host": two.ID, "result": "hit"}, 1},
		// A result that has not happened is still reported, so a rate has a
		// series to start from.
		{"the other host's misses", "zoomies_cache_restores_total", map[string]string{"pool": pool.Name, "host": two.ID, "result": "miss"}, 0},
	} {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := gatherValue(t, h.c, tt.series, tt.labels)
			if !ok || got != tt.want {
				t.Errorf("%s%v = %v (present %v), want %v", tt.series, tt.labels, got, ok, tt.want)
			}
		})
	}

	// The totals are the agent's: a later beat replaces them rather than adding.
	beat(one.ID, cacheStats(pool.ID, cacheservice.Stats{RestoreHits: 9}))
	if got, _ := gatherValue(t, h.c, "zoomies_cache_restores_total", map[string]string{"host": one.ID, "result": "hit"}); got != 9 {
		t.Errorf("hits after the next beat = %v, want the agent's new total 9", got)
	}
}

// A heartbeat with no block, which is what an agent without this feature or a
// host with no service sends, clears what was held, and a report for a pool the
// controller does not have or a host it has deleted is never exported: the
// agent does not get to decide what a label is.
func TestCacheSeriesAreBoundedByThePoolsAndHostsTheControllerHas(t *testing.T) {
	h := newHarness(t)
	inst := h.installation()
	pool := h.pool(inst, "zoomies-linux-x64")
	host := h.hostWith("one")

	known := cacheStats(pool.ID, cacheservice.Stats{RestoreHits: 1})
	invented := cacheStats("pool_nobody_made", cacheservice.Stats{RestoreHits: 1})
	if _, err := h.c.Heartbeat(h.ctx, host.ID, agent.HeartbeatRequest{Caches: []backend.CacheStats{known, invented}}); err != nil {
		t.Fatal(err)
	}
	if _, ok := gatherValue(t, h.c, "zoomies_cache_restores_total", map[string]string{"pool": "pool_nobody_made"}); ok {
		t.Error("a pool the agent named, and the controller does not have, was exported")
	}
	if _, ok := gatherValue(t, h.c, "zoomies_cache_restores_total", map[string]string{"pool": pool.Name}); !ok {
		t.Fatal("the known pool was not exported")
	}

	// A pool reported twice in one beat is one series, because a scrape that
	// emitted a series twice would fail whole.
	if _, err := h.c.Heartbeat(h.ctx, host.ID, agent.HeartbeatRequest{Caches: []backend.CacheStats{known, known}}); err != nil {
		t.Fatal(err)
	}
	if got, _ := gatherValue(t, h.c, "zoomies_cache_restores_total", map[string]string{"pool": pool.Name, "result": "hit"}); got != 2 {
		t.Errorf("hits = %v, want the two reports of one pool summed", got)
	}

	if _, err := h.c.Heartbeat(h.ctx, host.ID, agent.HeartbeatRequest{}); err != nil {
		t.Fatal(err)
	}
	if _, ok := gatherValue(t, h.c, "zoomies_cache_restores_total", map[string]string{"pool": pool.Name}); ok {
		t.Error("a heartbeat that reported no cache left the earlier totals standing")
	}

	// A deleted host's totals go with it.
	if _, err := h.c.Heartbeat(h.ctx, host.ID, agent.HeartbeatRequest{Caches: []backend.CacheStats{known}}); err != nil {
		t.Fatal(err)
	}
	if _, err := h.st.DeleteHost(h.ctx, host.ID); err != nil {
		t.Fatal(err)
	}
	if _, ok := gatherValue(t, h.c, "zoomies_cache_restores_total", map[string]string{"host": host.ID}); ok {
		t.Error("a deleted host is still exported")
	}
	if n := len(h.c.cacheReportsFor(map[string]bool{})); n != 0 {
		t.Errorf("%d host(s) still held in memory after the host was deleted", n)
	}
}

// The figures move with every cache request, so they must never ride on the
// host's view or any event: a frame per request is the cost this was designed
// around, and the view is what the UI drops straight into its cache.
func TestCacheTotalsAreNotPartOfTheHostView(t *testing.T) {
	h := newHarness(t)
	inst := h.installation()
	pool := h.pool(inst, "zoomies-linux-x64")
	host := h.hostWith("one")
	if _, err := h.c.Heartbeat(h.ctx, host.ID, agent.HeartbeatRequest{Caches: []backend.CacheStats{
		cacheStats(pool.ID, cacheservice.Stats{RestoreHits: 123456, StoredBytes: 654321}),
	}}); err != nil {
		t.Fatal(err)
	}
	stored, err := h.st.GetHost(h.ctx, host.ID)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(h.c.HostView(stored))
	if err != nil {
		t.Fatal(err)
	}
	for _, leaked := range []string{"123456", "654321", "restore", "cache_"} {
		if strings.Contains(string(raw), leaked) {
			t.Errorf("the host view carries %q: %s", leaked, raw)
		}
	}
}

// Each version a fill was asked for is counted once, by what became of it, under
// the pool's name.
func TestToolCacheFillsAreCountedByPoolAndOutcome(t *testing.T) {
	tools := func(n int) []backend.ToolRequest { return make([]backend.ToolRequest, n) }
	fill := func(outcomes ...string) []backend.ToolFill {
		var out []backend.ToolFill
		for _, o := range outcomes {
			out = append(out, backend.ToolFill{Tool: "go", Request: "1.27", Outcome: o})
		}
		return out
	}
	tests := []struct {
		name  string
		tools int
		res   agent.TaskResult
		want  map[string]float64
	}{
		{
			name: "every outcome a fill can report", tools: 5,
			res:  agent.TaskResult{OK: true, ToolFills: fill(backend.ToolInstalled, backend.ToolInstalled, backend.ToolPresent, backend.ToolSkipped, backend.ToolFailed)},
			want: map[string]float64{"installed": 2, "present": 1, "skipped": 1, "failed": 1},
		},
		{
			// An agent supplies the word, so one this controller has not heard of
			// must not mint a series of its own.
			name: "an outcome nobody has heard of is counted as unknown", tools: 1,
			res:  agent.TaskResult{OK: true, ToolFills: fill("quarantined")},
			want: map[string]float64{"unknown": 1},
		},
		{
			name: "a fill that failed before answering any version fails every version it was sent", tools: 3,
			res:  agent.TaskResult{OK: false, Error: "backend: pulling the image: denied"},
			want: map[string]float64{"failed": 3},
		},
		{
			name: "a fill that was sent nothing and answered nothing counts nothing", tools: 0,
			res:  agent.TaskResult{OK: true},
			want: map[string]float64{},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := newHarness(t)
			inst := h.installation()
			pool := h.pool(inst, "kept", "zoomies-kept")
			host := h.hostWith("current", agent.FeatureToolCacheFill)
			task := agent.Task{ID: "task-1", Kind: agent.TaskFillToolCache, PoolID: pool.ID, Tools: tools(tt.tools)}

			h.c.recordToolFill(host.ID, task, tt.res)

			for _, outcome := range []string{"installed", "present", "skipped", "failed", "unknown"} {
				got := testutil.ToFloat64(h.c.metrics.toolCacheFills.WithLabelValues(pool.Name, outcome))
				if got != tt.want[outcome] {
					t.Errorf("zoomies_tool_cache_fills_total{pool=%q,outcome=%q} = %v, want %v", pool.Name, outcome, got, tt.want[outcome])
				}
			}
			// Counted under the name, as every other pool-labelled series is.
			if got := testutil.ToFloat64(h.c.metrics.toolCacheFills.WithLabelValues(pool.ID, "installed")); got != 0 {
				t.Errorf("fills were counted under the pool id %q", pool.ID)
			}
		})
	}
}
