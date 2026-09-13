package agent

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/eyupio/zoomies/internal/backend"
	"github.com/eyupio/zoomies/internal/store"
)

// createdRunner puts a runner in the tracked set the way handleCreate does,
// without a task round trip: created with these resources, at its full
// allocation.
func createdRunner(a *Agent, runnerID string, handle backend.Handle, res store.Resources) {
	now := a.now()
	a.mu.Lock()
	defer a.mu.Unlock()
	a.runners[runnerID] = &tracked{
		runnerID: runnerID, name: "runner-" + runnerID, kind: store.BackendDocker, handle: handle,
		createdAt: now, state: store.RunnerRegistering, phase: backend.PhaseRunning, observedAt: now,
		resources: res, appliedCPUFactor: new(float64(1)),
	}
}

// beat answers one heartbeat with the given throttle and returns the calls
// the backend received during it.
func beat(t *testing.T, a *Agent, tr *fakeTransport, be *fakeBackend, d *ThrottleDirective) []resourceUpdate {
	t.Helper()
	tr.mu.Lock()
	tr.beatResp = &HeartbeatResponse{OK: true, Throttle: d}
	tr.mu.Unlock()
	before := len(be.resourceUpdates())
	if err := a.heartbeat(context.Background()); err != nil {
		t.Fatalf("heartbeat: %v", err)
	}
	return be.resourceUpdates()[before:]
}

func handlesOf(updates []resourceUpdate) []backend.Handle {
	out := make([]backend.Handle, 0, len(updates))
	for _, u := range updates {
		out = append(out, u.handle)
	}
	return out
}

// Once every runner on an overwhelmed host is busy, the effective capacity
// reaches nothing: the jobs already running are what is overwhelming it. The
// directive has to reach each of them, once, and the same directive on the
// next beat must cost the daemon nothing.
func TestAThrottleDirectiveReducesEachLimitedRunnerOnceAndNotAgainOnTheSameBeat(t *testing.T) {
	a, tr, be, _ := newAgent(t, 4)
	createdRunner(a, "run_a", "wl-a", store.Resources{CPUs: 2, MemoryMB: 4096, PidsLimit: 512})
	createdRunner(a, "run_b", "wl-b", store.Resources{CPUs: 0.75, MemoryMB: 1900})

	got := beat(t, a, tr, be, &ThrottleDirective{Level: 2, CPUFactor: 0.5})
	if len(got) != 2 {
		t.Fatalf("updates = %+v, want one per limited runner", got)
	}
	want := map[backend.Handle]store.Resources{
		"wl-a": {CPUs: 1, MemoryMB: 4096, PidsLimit: 512},
		// 0.375 is not a quota the daemon or the pool field speaks in.
		"wl-b": {CPUs: 0.38, MemoryMB: 1900},
	}
	for _, u := range got {
		if u.res != want[u.handle] {
			t.Errorf("%s was given %+v, want %+v: the base scaled by the factor, everything else as created", u.handle, u.res, want[u.handle])
		}
	}

	if again := beat(t, a, tr, be, &ThrottleDirective{Level: 2, CPUFactor: 0.5}); len(again) != 0 {
		t.Fatalf("the same directive was applied again on the next beat: %+v", again)
	}
	if lower := beat(t, a, tr, be, &ThrottleDirective{Level: 3, CPUFactor: 0.5}); len(lower) != 0 {
		t.Fatalf("a step that keeps the same factor was applied again: %+v", lower)
	}
}

// A throttle that never lifts is a job slowed for ever. A factor of 1 is the
// controller saying the host is calm, and every throttled runner goes back to
// what it was created with.
func TestAFactorOfOneRestoresEveryThrottledRunner(t *testing.T) {
	a, tr, be, _ := newAgent(t, 4)
	createdRunner(a, "run_a", "wl-a", store.Resources{CPUs: 2})
	beat(t, a, tr, be, &ThrottleDirective{Level: 1, CPUFactor: 0.75})

	got := beat(t, a, tr, be, &ThrottleDirective{Level: 0, CPUFactor: 1})
	if len(got) != 1 || got[0].handle != "wl-a" || got[0].res.CPUs != 2 {
		t.Fatalf("updates = %+v, want wl-a restored to 2 CPUs", got)
	}
	// A directive with no factor at all is a controller that has cleared the
	// throttle without saying so in numbers, and reads the same way.
	beat(t, a, tr, be, &ThrottleDirective{Level: 1, CPUFactor: 0.5})
	if got := beat(t, a, tr, be, &ThrottleDirective{}); len(got) != 1 || got[0].res.CPUs != 2 {
		t.Fatalf("updates = %+v, want a zero factor read as full allocation", got)
	}
}

// A runner with no CPU limit -- defaults off, or a row from before they
// existed -- has no quota to scale. Asking the daemon to set one would be a
// new limit rather than a throttle, and it is the controller's problem panel
// that says such runners exist.
func TestARunnerWithNoCPULimitIsLeftAlone(t *testing.T) {
	a, tr, be, _ := newAgent(t, 4)
	createdRunner(a, "run_unlimited", "wl-u", store.Resources{MemoryMB: 4096})
	createdRunner(a, "run_limited", "wl-l", store.Resources{CPUs: 1})

	got := beat(t, a, tr, be, &ThrottleDirective{Level: 2, CPUFactor: 0.5})
	if len(got) != 1 || got[0].handle != "wl-l" {
		t.Fatalf("updates = %+v, want only the limited runner touched", got)
	}
}

// An adopted runner was made by an agent that may have throttled it and then
// died, and this agent has no record either way. Its base comes from the
// workload's labels, and it is reconciled on the first beat whichever way the
// controller says -- the backend's update costs nothing when it already
// matches, so the safe direction is to ask.
func TestAnAdoptedRunnerIsReconciledOnTheFirstBeat(t *testing.T) {
	a, tr, be, _ := newAgent(t, 4)
	be.setWorkloads(backend.Workload{
		Handle: "wl-old", Name: "runner-old", RunnerID: "run_old",
		Status:    backend.Status{Phase: backend.PhaseRunning, StartedAt: time.Now().Add(-time.Hour)},
		Resources: store.Resources{CPUs: 4, MemoryMB: 8192},
	})
	a.adoptExisting(context.Background())

	got := beat(t, a, tr, be, &ThrottleDirective{Level: 0, CPUFactor: 1})
	if len(got) != 1 || got[0].handle != "wl-old" || got[0].res != (store.Resources{CPUs: 4, MemoryMB: 8192}) {
		t.Fatalf("updates = %+v, want the adopted runner reconciled to its labelled allocation", got)
	}
	if again := beat(t, a, tr, be, &ThrottleDirective{Level: 0, CPUFactor: 1}); len(again) != 0 {
		t.Fatalf("once reconciled the runner was asked about again: %+v", again)
	}
	// And a standing throttle reaches it the same way.
	if got := beat(t, a, tr, be, &ThrottleDirective{Level: 2, CPUFactor: 0.5}); len(got) != 1 || got[0].res.CPUs != 2 {
		t.Fatalf("updates = %+v, want the adopted runner throttled from its labelled base", got)
	}
}

// An older controller sends no directive. It never throttled anything, so
// there is nothing to restore, and reconciling every adopted runner against
// a controller that will never ask for it is a request per runner for nothing.
func TestANilDirectiveFromAnOlderControllerTouchesNothing(t *testing.T) {
	a, tr, be, _ := newAgent(t, 4)
	createdRunner(a, "run_a", "wl-a", store.Resources{CPUs: 2})
	be.setWorkloads(backend.Workload{
		Handle: "wl-old", Name: "runner-old", RunnerID: "run_old",
		Status:    backend.Status{Phase: backend.PhaseRunning, StartedAt: time.Now().Add(-time.Hour)},
		Resources: store.Resources{CPUs: 4},
	})
	a.adoptExisting(context.Background())

	if got := beat(t, a, tr, be, nil); len(got) != 0 {
		t.Fatalf("an older controller's beat changed quotas: %+v", got)
	}
}

// A controller downgraded under a standing throttle would otherwise leave
// every runner slowed for ever: the nil it now sends is the one nil that has
// to mean "restore".
func TestANilDirectiveAfterAThrottleRestoresTheRunners(t *testing.T) {
	a, tr, be, _ := newAgent(t, 4)
	createdRunner(a, "run_a", "wl-a", store.Resources{CPUs: 2})
	beat(t, a, tr, be, &ThrottleDirective{Level: 2, CPUFactor: 0.5})

	got := beat(t, a, tr, be, nil)
	if len(got) != 1 || got[0].res.CPUs != 2 {
		t.Fatalf("updates = %+v, want the runner restored", got)
	}
}

// A daemon that refuses -- Podman without the endpoint, a rootless daemon
// that cannot set a quota -- must not fail the beat that carries the host's
// runners, and must not say so on every one of them either: one line per
// runner per factor is what an operator can read.
func TestARefusedUpdateIsLoggedOnceAndDoesNotBreakTheHeartbeat(t *testing.T) {
	a, tr, be, _ := newAgent(t, 4)
	var logs lockedBuffer
	a.log = slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug}))
	createdRunner(a, "run_a", "wl-a", store.Resources{CPUs: 2})
	be.mu.Lock()
	be.updateErr = errors.New("the daemon does not support updating a running container")
	be.mu.Unlock()

	for range 3 {
		beat(t, a, tr, be, &ThrottleDirective{Level: 2, CPUFactor: 0.5})
	}
	if n := strings.Count(logs.String(), "level=WARN msg=\"could not change a runner's CPU quota"); n != 1 {
		t.Fatalf("the refusal was warned about %d times over three beats, want once:\n%s", n, logs.String())
	}
	if !strings.Contains(logs.String(), "throttling 1 runners to 50% of their CPU allocation") {
		t.Fatalf("the throttle was not announced:\n%s", logs.String())
	}

	// A different factor is a different refusal, worth its own line.
	beat(t, a, tr, be, &ThrottleDirective{Level: 1, CPUFactor: 0.75})
	if n := strings.Count(logs.String(), "level=WARN msg=\"could not change a runner's CPU quota"); n != 2 {
		t.Fatalf("a refusal at a new factor was not logged, %d warnings:\n%s", n, logs.String())
	}

	// The runner was never moved off its full allocation, so a factor of 1
	// has nothing to restore and must not pretend otherwise.
	beat(t, a, tr, be, &ThrottleDirective{Level: 0, CPUFactor: 1})
	if strings.Contains(logs.String(), "restoring") {
		t.Fatalf("a runner still at its full allocation was announced as restored:\n%s", logs.String())
	}

	// Once the daemon recovers the retry lands.
	be.mu.Lock()
	be.updateErr = nil
	be.mu.Unlock()
	if got := beat(t, a, tr, be, &ThrottleDirective{Level: 2, CPUFactor: 0.5}); len(got) != 1 || got[0].res.CPUs != 1 {
		t.Fatalf("updates = %+v, want the retry to land once the daemon answers", got)
	}
	if got := beat(t, a, tr, be, &ThrottleDirective{Level: 0, CPUFactor: 1}); len(got) != 1 || got[0].res.CPUs != 2 {
		t.Fatalf("updates = %+v, want the runner restored now that it really was throttled", got)
	}
}

// A runner created while the host is throttled comes up at its full
// allocation, and on an overwhelmed host that is one more full-speed job
// until the next beat. It is throttled as soon as it exists.
func TestARunnerCreatedUnderAThrottleIsThrottledAtOnce(t *testing.T) {
	h := newHarness(t, 2)
	h.tr.mu.Lock()
	h.tr.beatResp = &HeartbeatResponse{OK: true, Throttle: &ThrottleDirective{Level: 2, CPUFactor: 0.5}}
	h.tr.mu.Unlock()
	if err := h.agent.heartbeat(context.Background()); err != nil {
		t.Fatalf("heartbeat: %v", err)
	}

	task := createTask("task-1", "runner-1")
	task.Spec.Resources = store.Resources{CPUs: 2, MemoryMB: 2048}
	h.tr.tasks <- []Task{task}
	res := h.nextResult()
	if !res.OK {
		t.Fatalf("create failed: %+v", res)
	}
	got := h.be.resourceUpdates()
	if len(got) != 1 || got[0].handle != res.Handle || got[0].res != (store.Resources{CPUs: 1, MemoryMB: 2048}) {
		t.Fatalf("updates = %+v, want the new runner throttled to half before its result was reported", got)
	}
}

// A runner created while nothing is throttled already has its full
// allocation; asking the daemon to confirm that on the next beat would be a
// request per create for nothing.
func TestARunnerCreatedWithoutAThrottleIsNotAskedAboutOnTheNextBeat(t *testing.T) {
	h := newHarness(t, 2)
	task := createTask("task-1", "runner-1")
	task.Spec.Resources = store.Resources{CPUs: 2}
	h.tr.tasks <- []Task{task}
	h.nextResult()

	h.tr.mu.Lock()
	h.tr.beatResp = &HeartbeatResponse{OK: true, Throttle: &ThrottleDirective{Level: 0, CPUFactor: 1}}
	h.tr.mu.Unlock()
	if err := h.agent.heartbeat(context.Background()); err != nil {
		t.Fatalf("heartbeat: %v", err)
	}
	if got := h.be.resourceUpdates(); len(got) != 0 {
		t.Fatalf("updates = %+v, want none for a runner created at its full allocation", got)
	}
}

// A finished runner has nothing left to throttle, and a runner the backend
// no longer has is not a refusal: neither is worth a warning or a retry.
func TestFinishedAndGoneRunnersAreNotThrottled(t *testing.T) {
	a, tr, be, _ := newAgent(t, 4)
	var logs lockedBuffer
	a.log = slog.New(slog.NewTextHandler(&logs, nil))
	createdRunner(a, "run_done", "wl-done", store.Resources{CPUs: 2})
	a.markTerminal("run_done", store.RunnerRemoved, backend.PhaseExited, 0, "done", a.now())
	createdRunner(a, "run_gone", "wl-gone", store.Resources{CPUs: 2})
	be.mu.Lock()
	be.updateErr = backend.ErrNotFound
	be.mu.Unlock()

	if got := beat(t, a, tr, be, &ThrottleDirective{Level: 2, CPUFactor: 0.5}); len(got) != 0 {
		t.Fatalf("updates = %+v", got)
	}
	if strings.Contains(logs.String(), "level=WARN") {
		t.Fatalf("a gone runner was warned about:\n%s", logs.String())
	}
}

// lockedBuffer is a bytes.Buffer a logger can write from any goroutine.
type lockedBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *lockedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *lockedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}
