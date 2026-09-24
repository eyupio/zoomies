package controller

import (
	"bytes"
	"errors"
	"testing"
	"time"

	"github.com/eyupio/zoomies/internal/agent"
	"github.com/eyupio/zoomies/internal/backend"
	"github.com/eyupio/zoomies/internal/store"
	"github.com/prometheus/client_golang/prometheus/testutil"
)

// The budget exists for the agent that is not behaving; the one that is must
// never meet it, or the limit turns a busy host into a broken one. One
// heartbeat, a result for every task in two full batches and a report for each
// of them is far more than a real interval holds, and it still has to fit.
func TestTheAgentCallBudgetFitsABusyWellBehavedAgentAtEveryInterval(t *testing.T) {
	busy := 1 + 2*maxTasksPerPoll + 2*maxTasksPerPoll
	for _, interval := range []time.Duration{time.Second, 5 * time.Second, 30 * time.Second, 5 * time.Minute} {
		if got := agentCallBudget(interval); got < busy {
			t.Errorf("agentCallBudget(%s) = %d, want at least %d so a busy agent never meets it", interval, got, busy)
		}
	}
	if got, want := agentCallBudget(time.Minute), 60*agentCallsPerSecond; got != want {
		t.Errorf("agentCallBudget(1m) = %d, want %d: the budget scales with the interval", got, want)
	}
}

// One host spending its budget is refused with a wait to honour, and a second
// host is not touched by it: the whole point of keying by host is that one
// misbehaving agent is its own problem.
func TestAHostOverItsCallBudgetIsRefusedAndOtherHostsAreNot(t *testing.T) {
	h := newHarness(t)
	budget := agentCallBudget(h.c.heartbeatInterval())

	for i := range budget {
		if ok, _ := h.c.AllowAgentCall("host_noisy"); !ok {
			t.Fatalf("call %d of a budget of %d was refused", i+1, budget)
		}
	}
	ok, retry := h.c.AllowAgentCall("host_noisy")
	if ok {
		t.Fatalf("call %d was allowed; the budget is %d", budget+1, budget)
	}
	if retry <= 0 || retry > h.c.heartbeatInterval() {
		t.Errorf("retry after = %s, want a wait within one heartbeat interval", retry)
	}
	if ok, _ := h.c.AllowAgentCall("host_quiet"); !ok {
		t.Error("a quiet host was refused because another host spent its budget")
	}
	if got := testutil.ToFloat64(h.c.metrics.agentLimited.WithLabelValues(limitRate)); got != 1 {
		t.Errorf("zoomies_agent_requests_limited_total{limit=rate} = %v, want 1", got)
	}

	h.advance(h.c.heartbeatInterval() + time.Second)
	if ok, _ := h.c.AllowAgentCall("host_noisy"); !ok {
		t.Error("the noisy host was still refused a full interval later")
	}
}

// A host has one agent and an agent polls one at a time, so a second held poll
// is a bug or an abuse. It is answered at once rather than held, and it does
// not take the first one's place: a task queued afterwards still reaches the
// poll that was there first.
func TestASecondPollWhileOneIsHeldIsAnsweredAtOnceAndEmpty(t *testing.T) {
	h := newHarness(t)
	_, _, host := h.fleet()

	first := make(chan *agent.TaskBatch, 1)
	go func() {
		b, err := h.c.PollTasks(h.ctx, host.ID, 10*time.Second)
		if err != nil {
			t.Errorf("first PollTasks: %v", err)
		}
		first <- b
	}()
	deadline := time.Now().Add(2 * time.Second)
	for !h.c.queues.get(host.ID).polling.Load() {
		if time.Now().After(deadline) {
			t.Fatal("the first poll never started being held")
		}
		time.Sleep(time.Millisecond)
	}

	started := time.Now()
	second, err := h.c.PollTasks(h.ctx, host.ID, 10*time.Second)
	if err != nil {
		t.Fatalf("second PollTasks: %v", err)
	}
	if took := time.Since(started); took > time.Second {
		t.Errorf("the second poll was held for %s; it should be answered at once", took)
	}
	if len(second.Tasks) != 0 {
		t.Errorf("the second poll got %d tasks, want none", len(second.Tasks))
	}
	if got := testutil.ToFloat64(h.c.metrics.agentLimited.WithLabelValues(limitPoll)); got != 1 {
		t.Errorf("zoomies_agent_requests_limited_total{limit=poll} = %v, want 1", got)
	}

	h.c.enqueue(host.ID, agent.Task{Kind: agent.TaskCreateRunner, RunnerID: "run_example"})
	select {
	case b := <-first:
		if len(b.Tasks) != 1 {
			t.Errorf("the held poll got %d tasks, want the one queued", len(b.Tasks))
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the held poll never received the task queued after the refused one")
	}
}

// Every entry in a report is a store read and maybe a write behind the single
// writer, so one host sending a huge one holds the writer for every other.
func TestAReportWithMoreRunnersThanAnyHostRunsIsRefused(t *testing.T) {
	h := newHarness(t)
	_, _, host := h.fleet()
	tooMany := make([]agent.RunnerReport, agent.MaxRunnersPerReport+1)

	if err := h.c.ReportRunners(h.ctx, host.ID, tooMany); !errors.Is(err, ErrReportTooLarge) {
		t.Errorf("ReportRunners with %d runners = %v, want ErrReportTooLarge", len(tooMany), err)
	}
	beat := agent.HeartbeatRequest{ProtocolVersion: agent.ProtocolVersion, Runners: tooMany}
	if _, err := h.c.Heartbeat(h.ctx, host.ID, beat); !errors.Is(err, ErrReportTooLarge) {
		t.Errorf("Heartbeat with %d runners = %v, want ErrReportTooLarge", len(tooMany), err)
	}
	if err := h.c.checkReportSize(agent.MaxRunnersPerReport); err != nil {
		t.Errorf("a report of exactly the cap was refused: %v", err)
	}
	if got := testutil.ToFloat64(h.c.metrics.agentLimited.WithLabelValues(limitRunners)); got != 2 {
		t.Errorf("zoomies_agent_requests_limited_total{limit=runners} = %v, want 2", got)
	}
}

// spendStep is one call to logStream.spend, after a pause.
type spendStep struct {
	after time.Duration
	n     int
	want  bool
}

func TestALogStreamSpendsItsBudgetAndRefillsWithTime(t *testing.T) {
	tests := []struct {
		name  string
		steps []spendStep
	}{
		{"the burst is there from the first byte", []spendStep{
			{0, logStreamBurst, true}, {0, 1, false}}},
		{"a second refills a second's worth", []spendStep{
			{0, logStreamBurst, true}, {time.Second, logStreamBytesPerSecond, true}, {0, 1, false}}},
		{"idling never saves more than the burst", []spendStep{
			{0, 1, true}, {time.Hour, logStreamBurst, true}, {0, 2, false}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := &logStream{}
			now := time.Unix(1_000_000, 0)
			for i, step := range tt.steps {
				now = now.Add(step.after)
				if got := s.spend(step.n, now); got != step.want {
					t.Fatalf("step %d: spend(%d) = %v, want %v", i, step.n, got, step.want)
				}
			}
		})
	}
}

// An agent pushing output faster than the budget is read and dropped, never
// waited on: the relay returns as soon as the agent stops writing, the viewer
// gets the burst's worth, and the rest is counted.
func TestALogStreamOverItsByteBudgetIsDroppedRatherThanBlocked(t *testing.T) {
	h := newHarness(t)
	_, pool, host := h.fleet()
	r := h.runnerRow(pool, host, store.RunnerBusy)

	ch, cancel, err := h.c.OpenLogStream(h.ctx, r.ID, backend.LogOptions{Follow: true})
	if err != nil {
		t.Fatalf("OpenLogStream: %v", err)
	}
	defer cancel()
	task := h.taskOfKind(host.ID, agent.TaskStreamLogs)

	flood := logStreamBurst + 4*logStreamBytesPerSecond
	done := make(chan error, 1)
	go func() {
		done <- h.c.AcceptLogStream(host.ID, task.StreamID, bytes.NewReader(bytes.Repeat([]byte("x"), flood)))
	}()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("AcceptLogStream: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("the relay blocked on a stream over its budget instead of dropping")
	}

	got := 0
	for chunk := range ch {
		got += len(chunk)
	}
	// A little refills while the flood is read, so the bound is the burst plus
	// a second's worth rather than the burst exactly.
	if got > logStreamBurst+logStreamBytesPerSecond {
		t.Errorf("the viewer received %d of %d bytes; the budget is a burst of %d", got, flood, logStreamBurst)
	}
	if dropped := testutil.ToFloat64(h.c.metrics.logRelayDropped); int(dropped) != flood-got {
		t.Errorf("zoomies_log_relay_dropped_bytes_total = %v, want %d", dropped, flood-got)
	}
}
