package agent

import (
	"strings"
	"testing"
	"time"
)

// An operator who uncordons a host with a backlog waiting sees the controller
// place runners on it at once, but the agent only learns the flag has gone on
// its next beat. A create that arrives in that gap used to be refused from the
// agent's stale copy, failing every runner placed in the first half minute.
// The agent has to ask the controller again before it turns a create away.
func TestAnUncordonedHostTakesACreateBeforeItsNextBeat(t *testing.T) {
	h := newHarness(t, 4)
	h.tr.mu.Lock()
	h.tr.beatResp = &HeartbeatResponse{OK: true, Cordoned: true}
	h.tr.mu.Unlock()

	deadline := time.Now().Add(5 * time.Second)
	for h.agent.refuseNewWork() == "" {
		if time.Now().After(deadline) {
			t.Fatal("the agent never took in the cordon")
		}
		h.clock.advance(time.Second)
		time.Sleep(10 * time.Millisecond)
	}

	// The operator uncordons, and the controller places a runner before this
	// agent's next beat.
	h.tr.mu.Lock()
	h.tr.beatResp = &HeartbeatResponse{OK: true}
	h.tr.mu.Unlock()

	h.tr.tasks <- []Task{createTask("task-1", "runner-1")}
	res := h.nextResult()
	if !res.OK {
		t.Fatalf("a create for a host that is no longer cordoned was refused from a stale flag: %q", res.Error)
	}
}

// The controller already excludes a cordoned or incompatible host from
// placement, so a create arriving here is a controller that has not caught up:
// a plan computed before the flag, or one that does not know about the flag at
// all. Refusing it is the belt to that braces.
//
// What must not happen is the agent refusing everything. A cordoned host still
// has runners to drain, stop and stream logs from, and an agent that stopped
// polling would strand every one of them.
func TestARefusedHostStillDoesEverythingButCreate(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		resp *HeartbeatResponse
		want string
	}{{
		name: "cordoned",
		resp: &HeartbeatResponse{OK: true, Cordoned: true},
		want: "cordoned",
	}, {
		name: "incompatible",
		resp: &HeartbeatResponse{OK: true, Cordoned: true, Incompatible: true,
			IncompatibleReason: "this agent speaks protocol version 2 and the controller speaks 1"},
		want: "protocol version 2",
	}}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := newHarness(t, 4)
			h.tr.mu.Lock()
			h.tr.beatResp = tc.resp
			h.tr.mu.Unlock()

			// Wait for the agent to have taken the flag in, which it does on
			// its next beat.
			deadline := time.Now().Add(5 * time.Second)
			for h.agent.refuseNewWork() == "" {
				if time.Now().After(deadline) {
					t.Fatal("the agent never took in the controller's refusal")
				}
				h.clock.advance(time.Second)
				time.Sleep(10 * time.Millisecond)
			}

			h.tr.tasks <- []Task{createTask("task-1", "runner-1")}
			res := h.nextResult()
			if res.OK {
				t.Fatal("a refused host created a runner anyway")
			}
			// The reason travels to the controller, which is where an operator
			// looking at the runner's failure will read it.
			if !strings.Contains(res.Error, tc.want) {
				t.Errorf("the refusal does not say why: %q", res.Error)
			}
			if created, _, _ := h.be.counts(); created != 0 {
				t.Errorf("the backend was asked to create %d times", created)
			}

			// And a stop is still carried out: the runners already here are
			// exactly what a cordoned or superseded host has left to do.
			h.tr.tasks <- []Task{{ID: "stop-1", Kind: TaskStopRunner, RunnerID: "runner-1"}}
			stop := h.nextResult()
			if !stop.OK {
				t.Errorf("a refused host would not stop a runner: %+v", stop)
			}
		})
	}
}
