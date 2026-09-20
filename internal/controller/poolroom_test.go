package controller

import (
	"strings"
	"testing"

	"github.com/eyupio/zoomies/internal/store"
)

// An elastic pool is elastic only where its host's agent can move a live
// quota. The pool cannot tell from its own settings which of its hosts can, so
// the room -- the one count made from the hosts it will actually land on --
// says so, and the review step names them rather than letting a runner there
// sit at its share while the pool reads automatic.
func TestAnElasticPoolNamesTheHostsWhoseAgentsCannotLendCPU(t *testing.T) {
	elastic := &store.Pool{
		ID: "pool_e", Name: "zoomies-elastic", Enabled: true,
		Backend:  store.BackendDocker,
		CPUBurst: store.CPUBurstPolicy{Mode: store.CPUBurstAutomatic},
	}
	room := PoolRoom{Runners: 6, Slots: 6, Hosts: []PoolHostRoom{
		{HostID: "host_1", Host: "new-1", Slots: 4, Fits: 4, Room: 4, ElasticCPU: true},
		{HostID: "host_2", Host: "old-1", Slots: 2, Fits: 2, Room: 2},
	}}

	w := warned(PoolRoomWarnings(elastic, room), "pool.elastic_cpu_unsupported")
	if w == nil {
		t.Fatal("an automatic pool with a host that cannot lend CPU said nothing")
	}
	if !strings.Contains(w.Title, "1 of its 2 hosts runs") {
		t.Errorf("title = %q, want the count of hosts held at their share", w.Title)
	}
	if !strings.Contains(w.Detail, "old-1") || strings.Contains(w.Detail, "new-1") {
		t.Errorf("detail = %q, want the old agent named and the new one not", w.Detail)
	}
	if !strings.Contains(w.Fix, "upgrade the agent") {
		t.Errorf("fix = %q, want the one thing that changes the answer", w.Fix)
	}

	// Observe measures and never moves a quota, so it asks nothing of the
	// agent and the warning would be about a change nothing makes.
	elastic.CPUBurst.Mode = store.CPUBurstObserve
	if w := warned(PoolRoomWarnings(elastic, room), "pool.elastic_cpu_unsupported"); w != nil {
		t.Errorf("an observe pool was warned about an agent it never asks anything of: %s", w.Title)
	}

	// And a fleet whose every agent can lend CPU says nothing either: a warning
	// an operator sees on every correct pool is one they stop reading.
	elastic.CPUBurst.Mode = store.CPUBurstAutomatic
	room.Hosts[1].ElasticCPU = true
	if w := warned(PoolRoomWarnings(elastic, room), "pool.elastic_cpu_unsupported"); w != nil {
		t.Errorf("a fleet of capable agents was warned anyway: %s", w.Title)
	}
}
