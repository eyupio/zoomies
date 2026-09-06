package controller

import (
	"bytes"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/eyupio/zoomies/internal/agent"
	"github.com/eyupio/zoomies/internal/backend"
	"github.com/eyupio/zoomies/internal/store"
)

// The controller never dials an agent, so a log tail is a task out and a
// chunked POST back. This is that round trip.
func TestLogRelayDeliversToASubscriber(t *testing.T) {
	h := newHarness(t)
	_, pool, host := h.fleet()
	r := h.runnerRow(pool, host, store.RunnerBusy)

	ch, cancel, err := h.c.OpenLogStream(h.ctx, r.ID, backend.LogOptions{Follow: true, Tail: 100})
	if err != nil {
		t.Fatalf("OpenLogStream: %v", err)
	}
	defer cancel()

	task := h.taskOfKind(host.ID, agent.TaskStreamLogs)
	if task.RunnerID != r.ID || task.StreamID == "" {
		t.Fatalf("stream task = %+v, want one for runner %s with a stream ID", task, r.ID)
	}
	if task.LogOptions == nil || !task.LogOptions.Follow || task.LogOptions.Tail != 100 {
		t.Fatalf("log options = %+v, want the ones the viewer asked for", task.LogOptions)
	}

	done := make(chan []byte, 1)
	go func() { done <- drain(t, ch, 2*time.Second) }()

	if err := h.c.AcceptLogStream(host.ID, task.StreamID, strings.NewReader("run 1/3\nrun 2/3\nrun 3/3\n")); err != nil {
		t.Fatalf("AcceptLogStream: %v", err)
	}

	got := <-done
	if !bytes.Contains(got, []byte("run 2/3")) {
		t.Fatalf("subscriber received %q, want the runner's output", got)
	}
}

// Two viewers of the same runner share one stream, so opening a second tab
// does not make the agent read the container's output twice.
func TestSecondViewerSharesTheStream(t *testing.T) {
	h := newHarness(t)
	_, pool, host := h.fleet()
	r := h.runnerRow(pool, host, store.RunnerBusy)

	_, cancel1, err := h.c.OpenLogStream(h.ctx, r.ID, backend.LogOptions{Follow: true})
	if err != nil {
		t.Fatalf("first OpenLogStream: %v", err)
	}
	defer cancel1()
	ch2, cancel2, err := h.c.OpenLogStream(h.ctx, r.ID, backend.LogOptions{Follow: true})
	if err != nil {
		t.Fatalf("second OpenLogStream: %v", err)
	}
	defer cancel2()

	var streams int
	for _, task := range h.tasksFor(host.ID) {
		if task.Kind == agent.TaskStreamLogs {
			streams++
		}
	}
	if streams != 1 {
		t.Fatalf("queued %d stream tasks for two viewers, want 1", streams)
	}

	streamID := h.c.relay.streamIDFor(r.ID)
	go func() { _ = h.c.AcceptLogStream(host.ID, streamID, strings.NewReader("shared output\n")) }()
	if got := drain(t, ch2, 2*time.Second); !bytes.Contains(got, []byte("shared output")) {
		t.Fatalf("the second viewer received %q", got)
	}
}

// When the last viewer goes away the agent is told to stop reading, otherwise
// a closed browser tab would keep a container's output flowing forever.
func TestLastViewerLeavingCancelsTheStream(t *testing.T) {
	h := newHarness(t)
	_, pool, host := h.fleet()
	r := h.runnerRow(pool, host, store.RunnerBusy)

	ch, cancel, err := h.c.OpenLogStream(h.ctx, r.ID, backend.LogOptions{Follow: true})
	if err != nil {
		t.Fatalf("OpenLogStream: %v", err)
	}
	streamID := h.c.relay.streamIDFor(r.ID)
	cancel()

	if !h.hasTaskOfKind(host.ID, agent.TaskCancelLogs) {
		t.Fatal("no cancel task was queued when the last viewer left")
	}
	if _, open := <-ch; open {
		t.Fatal("the subscriber's channel is still open after it unsubscribed")
	}
	if err := h.c.AcceptLogStream(host.ID, streamID, strings.NewReader("late")); !errors.Is(err, ErrStreamUnknown) {
		t.Fatalf("AcceptLogStream after cancel = %v, want ErrStreamUnknown", err)
	}
}

// A browser that has stopped reading must lose lines rather than hold up the
// agent writing into the relay.
func TestLogRelayDropsRatherThanBlocks(t *testing.T) {
	h := newHarness(t)
	_, pool, host := h.fleet()
	r := h.runnerRow(pool, host, store.RunnerBusy)

	// Subscribe and deliberately never read.
	_, cancel, err := h.c.OpenLogStream(h.ctx, r.ID, backend.LogOptions{Follow: true})
	if err != nil {
		t.Fatalf("OpenLogStream: %v", err)
	}
	defer cancel()
	streamID := h.taskOfKind(host.ID, agent.TaskStreamLogs).StreamID

	// One byte per Read, so this is several times the subscriber's queue depth
	// in separate chunks.
	payload := bytes.Repeat([]byte("x"), logSubscriberQueue*3)
	done := make(chan error, 1)
	go func() { done <- h.c.AcceptLogStream(host.ID, streamID, &byteReader{data: payload}) }()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("AcceptLogStream: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("AcceptLogStream blocked on a subscriber that stopped reading")
	}
}

// Logs for a runner whose container is gone are not an empty stream: the API
// gets an error it can turn into an explanation.
func TestLogStreamForARemovedRunnerIsRefused(t *testing.T) {
	h := newHarness(t)
	_, pool, host := h.fleet()
	r := h.runnerRow(pool, host, store.RunnerIdle)
	if _, err := h.st.TransitionRunner(h.ctx, r.ID, store.RunnerRemoved, "gone"); err != nil {
		t.Fatalf("TransitionRunner: %v", err)
	}

	if _, _, err := h.c.OpenLogStream(h.ctx, r.ID, backend.LogOptions{}); err == nil {
		t.Fatal("OpenLogStream succeeded for a removed runner")
	} else if !strings.Contains(err.Error(), "ephemeral") {
		t.Fatalf("error = %v, want one explaining that the container is gone", err)
	}
}

// Whatever arrives on a log relay is shown to operators as that runner's
// output. Every other agent endpoint checks the reporting host owns what it is
// reporting on; this one has to check the same thing against the stream, or one
// host in the fleet can put words in another's mouth.
func TestLogRelayRefusesAnotherHostsStream(t *testing.T) {
	h := newHarness(t)
	_, pool, host := h.fleet()
	r := h.runnerRow(pool, host, store.RunnerBusy)
	intruder := h.host("vm-intruder")

	ch, cancel, err := h.c.OpenLogStream(h.ctx, r.ID, backend.LogOptions{Follow: true})
	if err != nil {
		t.Fatalf("OpenLogStream: %v", err)
	}
	defer cancel()
	streamID := h.taskOfKind(host.ID, agent.TaskStreamLogs).StreamID

	err = h.c.AcceptLogStream(intruder.ID, streamID, strings.NewReader("you have been pwned\n"))
	if !errors.Is(err, ErrStreamUnknown) {
		t.Fatalf("another host writing into the stream = %v; want ErrStreamUnknown", err)
	}
	// And the answer is the same one a closed stream gets, so probing for
	// streams you do not own tells you nothing.
	if err := h.c.AcceptLogStream(intruder.ID, "log_nosuchstream", strings.NewReader("x")); err.Error() == "" {
		t.Fatal("an unknown stream should still be refused")
	}

	// Nothing the intruder sent reached the viewer.
	select {
	case chunk, ok := <-ch:
		if ok {
			t.Fatalf("the viewer received %q from a host that does not own the runner", chunk)
		}
		t.Fatal("the stream was closed by a refused write")
	default:
	}

	// The owning host is unaffected.
	go func() { _ = h.c.AcceptLogStream(host.ID, streamID, strings.NewReader("real output\n")) }()
	if got := drain(t, ch, 2*time.Second); !bytes.Contains(got, []byte("real output")) {
		t.Fatalf("the owning host's output did not arrive: %q", got)
	}
}
