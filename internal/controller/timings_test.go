package controller

import (
	"context"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"

	"github.com/eyupio/zoomies/internal/agent"
	"github.com/eyupio/zoomies/internal/store"
)

func timingSample(t *testing.T, h *prometheus.HistogramVec, pool, backend string) (uint64, float64) {
	t.Helper()
	var m dto.Metric
	if err := h.WithLabelValues(pool, backend).(prometheus.Metric).Write(&m); err != nil {
		t.Fatal(err)
	}
	return m.GetHistogram().GetSampleCount(), m.GetHistogram().GetSampleSum()
}

func TestSchedulingTimingUsesTheJobsActualRunnerOnce(t *testing.T) {
	h := newHarness(t)
	_, pool, host := h.fleet()
	h.deliverJob(jobEvent{Action: "queued", JobID: 9201, Labels: []string{"self-hosted", "linux", "x64", "demo"}})
	if err := h.c.Reconcile(h.ctx); err != nil {
		t.Fatal(err)
	}
	h.advance(5 * time.Second)
	if _, err := h.c.PollTasks(h.ctx, host.ID, time.Second); err != nil {
		t.Fatal(err)
	}
	r := h.onlyRunner()
	first := *r.CreateTaskIssuedAt
	h.advance(time.Minute)
	h.c.stampIssued(h.ctx, []agent.Task{{Kind: agent.TaskCreateRunner, RunnerID: r.ID, IssuedAt: h.c.Now()}})
	h.deliverJob(jobEvent{Action: "in_progress", JobID: 9201, RunnerName: r.Name, Labels: []string{"self-hosted", "linux", "x64", "demo"}})
	h.deliverJob(jobEvent{Action: "in_progress", JobID: 9201, RunnerName: r.Name, Labels: []string{"self-hosted", "linux", "x64", "demo"}})
	jobs, _, err := h.st.ListJobs(h.ctx, store.JobFilter{}, store.Page{})
	if err != nil || len(jobs) != 1 || jobs[0].EligibleAt == nil {
		t.Fatalf("job eligibility: %+v, %v", jobs, err)
	}
	count, sum := timingSample(t, h.c.metrics.schedulingLatency, pool.Name, string(pool.Backend))
	if count != 1 || sum != first.Sub(*jobs[0].EligibleAt).Seconds() {
		t.Fatalf("scheduling histogram = %d samples, %v seconds; want one first-delivery interval", count, sum)
	}
}

func TestCleanupMetricWaitsForBothSidesAndDoesNotCountRetries(t *testing.T) {
	h := newHarness(t)
	_, pool, host := h.fleet()
	r := h.runnerRow(pool, host, store.RunnerIdle)
	if _, err := h.st.TransitionRunner(h.ctx, r.ID, store.RunnerRemoved, "finished"); err != nil {
		t.Fatal(err)
	}
	h.c.confirmCleanup(h.ctx, r.ID, false)
	if count, _ := timingSample(t, h.c.metrics.cleanupDuration, pool.Name, string(pool.Backend)); count != 0 {
		t.Fatal("registration removal alone produced a cleanup timing")
	}
	h.advance(2 * time.Minute)
	h.c.confirmCleanup(h.ctx, r.ID, true)
	h.c.confirmCleanup(h.ctx, r.ID, true)
	h.c.confirmCleanup(h.ctx, r.ID, false)
	count, sum := timingSample(t, h.c.metrics.cleanupDuration, pool.Name, string(pool.Backend))
	finished := h.runnerByID(t, r.ID)
	want := finished.CleanedUpAt.Sub(*finished.FinishedAt).Seconds()
	if count != 1 || sum != want {
		t.Fatalf("cleanup histogram = %d samples, %v seconds; want one confirmed interval of %v seconds", count, sum, want)
	}
}

func TestCleanupReportsAreNotAcknowledgedWhenTheStoreCannotBeRead(t *testing.T) {
	h := newHarness(t)
	_, pool, host := h.fleet()
	r := h.runnerRow(pool, host, store.RunnerRemoved)
	report := agent.RunnerReport{RunnerID: r.ID, State: store.RunnerRemoved, HostRemoved: true}
	ctx, cancel := context.WithCancel(h.ctx)
	cancel()
	if err := h.c.ReportRunners(ctx, host.ID, []agent.RunnerReport{report}); err == nil {
		t.Fatal("a failed database read acknowledged the cleanup receipt")
	}
	if err := h.c.ReportRunners(h.ctx, host.ID, []agent.RunnerReport{report}); err != nil {
		t.Fatal(err)
	}
	if h.runnerByID(t, r.ID).HostRemovedAt == nil {
		t.Fatal("the retried receipt was not stored")
	}
}
