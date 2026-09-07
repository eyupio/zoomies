package controller

import (
	"testing"
	"time"

	"github.com/eyupio/zoomies/internal/agent"
	"github.com/eyupio/zoomies/internal/store"
)

// The restart boundary table.
//
// A controller restart drops the task queue, by design: every task is derived
// from state the database already holds, so persisting the queue would add a
// second source of truth that could disagree with the runners table. The price
// is that a result arriving after the restart answers a task the new
// controller has never heard of, and it must still be applied -- the agent did
// the work, and the row is the only place that fact can land.
//
// Each row here puts a runner at one boundary with a task in flight, restarts
// the controller over the same store, replays the agent's result, and asserts
// the fleet converges: the state the result implies, and never two live rows
// for one runner name. Duplicate live rows are the failure this guards
// against, because a name is a GitHub registration, and two of them means one
// runner executing jobs that the fleet is accounting for twice.
func TestALateResultAfterARestartLeavesOneLiveRowPerRunner(t *testing.T) {
	cases := []struct {
		name string
		// state the runner is in when the controller goes down.
		state store.RunnerState
		// kind of the task the agent is still working on.
		kind agent.TaskKind
		// inflight polls the task, so it is in flight rather than queued.
		inflight bool
		// result the agent reports once the controller is back.
		ok        bool
		reports   store.RunnerState
		wantState store.RunnerState
	}{
		{
			name:  "create enqueued but never collected",
			state: store.RunnerProvisioning, kind: agent.TaskCreateRunner,
			ok: true, reports: store.RunnerRegistering, wantState: store.RunnerRegistering,
		},
		{
			name:  "create acknowledged and in flight",
			state: store.RunnerProvisioning, kind: agent.TaskCreateRunner, inflight: true,
			ok: true, reports: store.RunnerRegistering, wantState: store.RunnerRegistering,
		},
		{
			name:  "create failed on the host",
			state: store.RunnerProvisioning, kind: agent.TaskCreateRunner, inflight: true,
			ok: false, wantState: store.RunnerFailed,
		},
		{
			name:  "registering when the controller went down",
			state: store.RunnerRegistering, kind: agent.TaskCreateRunner, inflight: true,
			ok: true, reports: store.RunnerIdle, wantState: store.RunnerIdle,
		},
		{
			name:  "busy with a stop in flight",
			state: store.RunnerBusy, kind: agent.TaskStopRunner, inflight: true,
			ok: true, reports: store.RunnerDraining, wantState: store.RunnerDraining,
		},
		{
			name:  "draining with a stop in flight",
			state: store.RunnerDraining, kind: agent.TaskStopRunner, inflight: true,
			ok: true, reports: store.RunnerRemoved, wantState: store.RunnerRemoved,
		},
		{
			name:  "remove in flight",
			state: store.RunnerDraining, kind: agent.TaskRemoveRunner, inflight: true,
			ok: true, reports: store.RunnerRemoved, wantState: store.RunnerRemoved,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := newHarness(t)
			_, pool, host := h.fleet()
			r := h.runnerRow(pool, host, tc.state)

			task := agent.Task{ID: "task_boundary", Kind: tc.kind, RunnerID: r.ID, Backend: pool.Backend}
			h.c.enqueueLifecycle(h.ctx, host.ID, task)
			if tc.inflight {
				if _, err := h.c.PollTasks(h.ctx, host.ID, time.Second); err != nil {
					t.Fatalf("PollTasks: %v", err)
				}
			}

			// The restart. The rows are all that crosses it.
			c := h.restart()

			// The agent finishes the work it was already doing and reports a
			// result for a task this controller has never issued. Kind is
			// carried on the result precisely so it can still be understood.
			err := c.ReportResult(h.ctx, host.ID, agent.TaskResult{
				TaskID: task.ID, Kind: tc.kind, RunnerID: r.ID,
				OK: tc.ok, State: tc.reports, Error: errorFor(tc.ok),
			})
			if err != nil {
				t.Fatalf("ReportResult after the restart: %v", err)
			}

			got := h.runnerByID(t, r.ID)
			if got.State != tc.wantState {
				t.Errorf("state = %q, want %q: the agent's work was lost across the restart", got.State, tc.wantState)
			}
			assertOneLiveRowPerName(t, h)
		})
	}
}

func errorFor(ok bool) string {
	if ok {
		return ""
	}
	return "the workload could not be created"
}

// assertOneLiveRowPerName is the invariant the table exists for. A runner name
// is a GitHub registration, so two live rows sharing one would have the fleet
// account twice for a single runner -- and place work against capacity that
// does not exist.
func assertOneLiveRowPerName(t *testing.T, h *harness) {
	t.Helper()
	live := map[string]string{}
	for _, r := range h.runners() {
		if !r.State.Live() {
			continue
		}
		if other, dup := live[r.Name]; dup {
			t.Fatalf("runners %s and %s are both live as %q", other, r.ID, r.Name)
		}
		live[r.Name] = r.ID
	}
}

// The other half of the boundary: a result that arrives after the fleet has
// already given up on the runner. The five-minute host-lost reclaim is shorter
// than the twenty-minute create lease, so a create in flight on a host that
// goes quiet is failed before its lease has even expired -- and the agent,
// which never heard any of that, reports success when it comes back.
//
// The terminal row stands. It is what the operator and the job's timeline were
// told, and a success arriving afterwards does not make it untrue that this
// fleet stopped counting on the runner.
func TestALateSuccessDoesNotResurrectARunnerTheFleetGaveUpOn(t *testing.T) {
	h := newHarness(t)
	_, pool, host := h.fleet()
	r := h.runnerRow(pool, host, store.RunnerProvisioning)

	task := agent.Task{ID: "task_late", Kind: agent.TaskCreateRunner, RunnerID: r.ID, Backend: pool.Backend}
	h.c.enqueueLifecycle(h.ctx, host.ID, task)
	if _, err := h.c.PollTasks(h.ctx, host.ID, time.Second); err != nil {
		t.Fatalf("PollTasks: %v", err)
	}
	if err := h.c.failRunnerID(h.ctx, r.ID, "host went quiet for longer than hostLostAfter"); err != nil {
		t.Fatalf("failRunnerID: %v", err)
	}

	c := h.restart()
	if err := c.ReportResult(h.ctx, host.ID, agent.TaskResult{
		TaskID: task.ID, Kind: agent.TaskCreateRunner, RunnerID: r.ID,
		OK: true, State: store.RunnerRegistering,
	}); err != nil {
		t.Fatalf("ReportResult: %v", err)
	}

	got := h.runnerByID(t, r.ID)
	if got.State != store.RunnerFailed {
		t.Fatalf("state = %q, want it to stay failed; the fleet had already told an operator this runner was gone", got.State)
	}
	assertOneLiveRowPerName(t, h)
}
