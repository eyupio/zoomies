package controller

import (
	"testing"

	"github.com/prometheus/client_golang/prometheus/testutil"

	"github.com/eyupio/zoomies/internal/store"
)

// Every pool-labelled metric names the pool the same way.
//
// `zoomies_jobs_total` used the pool id while every other pool-labelled series
// used the name, so a PromQL query joining a pool's job count against its
// runner count on `pool` matched nothing at all -- silently, which is the worst
// way for a dashboard to be wrong. One helper decides what a pool label is now,
// and this holds it to names.
func TestPoolLabelsAreNamesEverywhere(t *testing.T) {
	h := newHarness(t)
	inst := h.installation()
	pool := h.pool(inst, "zoomies-linux-x64")

	if got := h.c.poolLabel(pool.ID); got != pool.Name {
		t.Errorf("poolLabel(%q) = %q, want the pool's name %q", pool.ID, got, pool.Name)
	}

	// Work no pool claims is counted under one agreed literal rather than an
	// empty label, which Prometheus cannot tell from a bug.
	if got := h.c.poolLabel(""); got != UnmatchedPool {
		t.Errorf("poolLabel(\"\") = %q, want %q", got, UnmatchedPool)
	}

	// A pool deleted between the job finishing and the metric being written
	// still has to be counted somewhere, and its id is the only name left.
	if got := h.c.poolLabel("pool_goneaway"); got != "pool_goneaway" {
		t.Errorf("poolLabel of a missing pool = %q, want the id back", got)
	}
}

// The completion counter carries the pool's name, not its id.
func TestJobCompletionIsCountedUnderThePoolName(t *testing.T) {
	h := newHarness(t)
	inst := h.installation()
	pool := h.pool(inst, "zoomies-linux-x64")

	h.c.observeJobCompletion(&store.Job{PoolID: pool.ID, Conclusion: "success"})

	if got := testutil.ToFloat64(h.c.metrics.jobsTotal.WithLabelValues(pool.Name, "success")); got != 1 {
		t.Errorf("zoomies_jobs_total{pool=%q} = %v, want 1", pool.Name, got)
	}
	if got := testutil.ToFloat64(h.c.metrics.jobsTotal.WithLabelValues(pool.ID, "success")); got != 0 {
		t.Errorf("zoomies_jobs_total is still counted under the pool id %q", pool.ID)
	}

	// And a job no pool claimed lands under the agreed literal.
	h.c.observeJobCompletion(&store.Job{Conclusion: "failure"})
	if got := testutil.ToFloat64(h.c.metrics.jobsTotal.WithLabelValues(UnmatchedPool, "failure")); got != 1 {
		t.Errorf("an unclaimed job was not counted under %q", UnmatchedPool)
	}
}
