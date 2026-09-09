package controller

import (
	"testing"
	"time"

	"github.com/eyupio/zoomies/internal/agent"
	"github.com/eyupio/zoomies/internal/config"
	"github.com/eyupio/zoomies/internal/store"
)

// The constants that govern reconciliation live in four packages, and each
// was chosen against one in another: the host-lost reclaim against the
// heartbeat timeout, the heartbeat timeout against the interval the controller
// hands the agent, every task lease against the time the agent gives itself to
// do that task. Until now nothing but a comment held those pairs together, so
// a change to one side did not have to say what it did to the other. These two
// tests are the section of the same name in docs/architecture.md, made to
// fail.

// A host passes through two judgements as it goes quiet, and they have to come
// in this order: late (one missed heartbeat is nothing), then unhealthy (the
// scheduler stops placing runners on it), then lost (the runners already on
// it are failed so a pool can replace them). Unhealthy before late would fail
// healthy hosts on every hiccup; lost before unhealthy would destroy runners
// on a host the scheduler was still placing new ones on.
func TestAHostIsLateThenUnhealthyThenLostInThatOrder(t *testing.T) {
	interval := config.Default().Agent.HeartbeatInterval
	if interval <= 0 {
		t.Fatalf("the default heartbeat interval is %s; the ladder needs a positive one", interval)
	}
	// Three missed heartbeats, not one: a single late POST is a busy host or
	// a slow link, and marking it unhealthy would bounce placement on and off.
	if store.HeartbeatTimeout < 3*interval {
		t.Fatalf("store.HeartbeatTimeout is %s, less than three default heartbeat intervals of %s; one slow heartbeat would make a host unhealthy", store.HeartbeatTimeout, interval)
	}
	if hostLostAfter <= store.HeartbeatTimeout {
		t.Fatalf("hostLostAfter is %s, not later than store.HeartbeatTimeout at %s; runners would be failed on a host the scheduler had not yet stopped using", hostLostAfter, store.HeartbeatTimeout)
	}
}

// A task's lease is how long the controller waits before offering the task to
// the host again. If it were shorter than the work, a create still pulling its
// image would be redelivered while the first attempt was running, and the
// agent's adopt-before-create is a backstop for a lost result, not a licence
// to race it. So each lease exceeds the longest the agent will spend on that
// kind of task, with room for the result to travel.
func TestEveryTaskLeaseOutlastsTheWorkItCovers(t *testing.T) {
	cases := []struct {
		kind  agent.TaskKind
		lease time.Duration
		work  time.Duration
	}{
		{agent.TaskCreateRunner, createLease, agent.CreateTimeout},
		// The controller hands the agent DefaultStopTimeout on every stop
		// (reconcile.go), and the agent adds StopMargin to it.
		{agent.TaskStopRunner, stopLease, agent.DefaultStopTimeout + agent.StopMargin},
		{agent.TaskRemoveRunner, removeLease, agent.RemoveTimeout},
	}
	for _, c := range cases {
		if got := requeueAfter(c.kind); got != c.lease {
			t.Errorf("%s: requeueAfter gives %s, want the %s lease this test pins", c.kind, got, c.lease)
		}
		if c.lease <= c.work {
			t.Errorf("%s: the lease is %s and the agent may spend %s on the task; the controller would redeliver it while the first attempt was still running", c.kind, c.lease, c.work)
		}
	}
}
