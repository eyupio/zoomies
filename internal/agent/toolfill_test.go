package agent

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/eyupio/zoomies/internal/backend"
	"github.com/eyupio/zoomies/internal/store"
)

// fillingBackend is the fake backend plus a tool cache fill that waits until
// the test lets it finish.
type fillingBackend struct {
	*fakeBackend
	release chan struct{}
	got     chan []backend.ToolRequest
}

func (f *fillingBackend) FillToolCache(ctx context.Context, spec backend.Spec, tools []backend.ToolRequest) ([]backend.ToolFill, error) {
	f.got <- tools
	select {
	case <-f.release:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	out := make([]backend.ToolFill, 0, len(tools))
	for _, t := range tools {
		out = append(out, backend.ToolFill{Tool: t.Tool, Request: t.Version, Outcome: backend.ToolInstalled, Version: t.Version + ".0"})
	}
	return out, nil
}

func fillTask(id string) Task {
	return Task{ID: id, Kind: TaskFillToolCache, Backend: store.BackendDocker, PoolID: "pool-1",
		Spec:  &backend.Spec{PoolID: "pool-1", Image: "ghcr.io/example/runner:latest", Cache: store.CacheConfig{Enabled: true, Tools: true}},
		Tools: []backend.ToolRequest{{Tool: "python", Version: "3.12"}, {Tool: "node", Version: "22"}}}
}

// A fill is minutes of downloading. In a lifecycle slot it would hold back
// the runners it exists to speed up: on a one-slot host, a job queued behind
// a fill waited for every JDK to arrive. It runs beside the slots instead.
func TestAFillDoesNotHoldARunnerSlot(t *testing.T) {
	tr := newFakeTransport()
	be := &fillingBackend{fakeBackend: newFakeBackend(store.BackendDocker), release: make(chan struct{}), got: make(chan []backend.ToolRequest, 1)}
	a, err := New(Options{
		Name: "test-host", WorkDir: t.TempDir(), Capacity: 1, Backends: backend.NewRegistry(be),
		DefaultBackend: store.BackendDocker, Transport: tr, HeartbeatInterval: time.Second, Logger: testLogger(), Clock: newTestClock().Now,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := a.Join(context.Background(), "join-token"); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- a.Run(ctx) }()
	t.Cleanup(func() {
		cancel()
		<-done
	})
	h := &harness{t: t, agent: a, tr: tr, be: be.fakeBackend}

	tr.tasks <- []Task{fillTask("fill-1")}
	select {
	case tools := <-be.got:
		if len(tools) != 2 {
			t.Fatalf("the fill was given %v", tools)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the fill never reached the backend")
	}

	tr.tasks <- []Task{createTask("create-1", "runner-1")}
	if res := h.nextResult(); res.TaskID != "create-1" || !res.OK {
		t.Fatalf("while a fill ran, the create reported %+v", res)
	}

	close(be.release)
	res := h.nextResult()
	if res.TaskID != "fill-1" || !res.OK || len(res.ToolFills) != 2 || res.ToolFills[0].Version != "3.12.0" {
		t.Fatalf("fill result = %+v", res)
	}
}

// A backend with no tool cache -- the process backend, a fake -- answers the
// task rather than dropping it, or the controller's lease on it never clears.
func TestAFillOnABackendWithoutAToolCacheSaysSo(t *testing.T) {
	h := newHarness(t, 1)
	h.tr.tasks <- []Task{fillTask("fill-1")}
	res := h.nextResult()
	if res.OK || !strings.Contains(res.Error, "keeps no tool cache") {
		t.Fatalf("result = %+v, want a failure saying the backend keeps no tool cache", res)
	}
}

func TestAFillWithNothingToFillIsRefused(t *testing.T) {
	task := fillTask("fill-1")
	task.Tools = nil
	if err := validateTask(task); err == nil {
		t.Error("a fill naming no toolchains was accepted")
	}
	task = fillTask("fill-2")
	task.Spec = nil
	if err := validateTask(task); err == nil {
		t.Error("a fill with no spec was accepted; the backend could not find the pool's cache")
	}
}

// The controller sends fills only to agents that say they understand them.
func TestTheAgentAdvertisesToolCacheFills(t *testing.T) {
	h := newHarness(t, 1)
	h.tr.mu.Lock()
	req := h.tr.joinReq
	h.tr.mu.Unlock()
	if req == nil || !strings.Contains(strings.Join(req.Features, ","), FeatureToolCacheFill) {
		t.Fatalf("join features = %v, want %s", req, FeatureToolCacheFill)
	}
}
