package agent

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/eyupio/zoomies/internal/backend"
)

type timeoutErr struct{}

func (timeoutErr) Error() string   { return "i/o timeout" }
func (timeoutErr) Timeout() bool   { return true }
func (timeoutErr) Temporary() bool { return true }

var _ net.Error = timeoutErr{}

func nextBeat(t *testing.T, a *Agent, tr *fakeTransport) HeartbeatRequest {
	t.Helper()
	if err := a.heartbeat(context.Background()); err != nil {
		t.Fatalf("heartbeat: %v", err)
	}
	select {
	case beat := <-tr.beats:
		return beat
	case <-time.After(time.Second):
		t.Fatal("no heartbeat was sent")
	}
	return HeartbeatRequest{}
}

// The cooldown used to exist only in this process's memory and one log line,
// so a host whose daemon kept falling over looked, from the controller, like
// a host that was merely slow to start runners. The beat is what carries it
// off the machine, and a success is what takes it away again.
func TestTheHeartbeatCarriesTheRuntimeCooldownUntilASuccessClearsIt(t *testing.T) {
	a, tr, _, clock := newAgent(t, 2)
	a.opts.RandomFloat64 = func() float64 { return 0 }
	if err := a.Join(context.Background(), "join-token"); err != nil {
		t.Fatalf("Join: %v", err)
	}

	if beat := nextBeat(t, a, tr); beat.Runtime != nil {
		t.Fatalf("a healthy runtime was reported as recovering: %+v", beat.Runtime)
	}

	a.runtimeResult(fmt.Errorf("the docker socket: %w", backend.ErrUnavailable))
	a.runtimeResult(fmt.Errorf("the docker socket: %w", backend.ErrUnavailable))
	clock.advance(3 * time.Second)
	beat := nextBeat(t, a, tr)
	if beat.Runtime == nil {
		t.Fatal("the heartbeat did not carry the runtime cooldown")
	}
	got := *beat.Runtime
	// Two failures: the second waits 10 s with no jitter, 3 s of which have gone.
	if got.Failures != 2 || got.Kind != RuntimeUnavailable || got.RetryIn != 7*time.Second {
		t.Fatalf("reported %+v, want two unavailable failures retrying in 7s", got)
	}
	if !strings.Contains(got.Error, "docker socket") {
		t.Fatalf("the report lost the backend's own sentence: %q", got.Error)
	}

	// Past the retry time the hold is over but nothing has said the runtime
	// works, so the report stands with nothing left to wait.
	clock.advance(time.Minute)
	if beat := nextBeat(t, a, tr); beat.Runtime == nil || beat.Runtime.RetryIn != 0 {
		t.Fatalf("an unproven recovery was reported as %+v, want the cooldown with nothing left to wait", beat.Runtime)
	}

	a.runtimeResult(timeoutErr{})
	if beat := nextBeat(t, a, tr); beat.Runtime == nil || beat.Runtime.Kind != RuntimeTimeout || beat.Runtime.Failures != 3 {
		t.Fatalf("a daemon that did not answer was reported as %+v, want a third failure of kind timeout", beat.Runtime)
	}

	a.runtimeResult(nil)
	if beat := nextBeat(t, a, tr); beat.Runtime != nil {
		t.Fatalf("a success did not clear the report: %+v", beat.Runtime)
	}
}

// A failure the runtime is not to blame for opens no cooldown, so it must not
// be reported as one either: an operator sent to restart a daemon over a
// typo in a pool's image would be sent to the wrong machine.
func TestFailuresThatAreNotTheRuntimesAreNotReported(t *testing.T) {
	a, _, _, _ := newAgent(t, 2)
	for _, err := range []error{
		errors.New("bad pool configuration"),
		fmt.Errorf("pulling: %w", context.DeadlineExceeded),
		context.Canceled,
	} {
		a.runtimeResult(err)
		if r := a.runtimeReport(); r != nil {
			t.Fatalf("%v was reported as a runtime cooldown: %+v", err, r)
		}
	}
}

func TestARuntimeErrorIsBoundedBeforeItIsReported(t *testing.T) {
	a, _, _, _ := newAgent(t, 2)
	a.runtimeResult(fmt.Errorf("%s: %w", strings.Repeat("x", 4*maxRuntimeError), backend.ErrUnavailable))
	r := a.runtimeReport()
	if r == nil || len([]rune(r.Error)) > maxRuntimeError+3 {
		t.Fatalf("the reported error was not bounded: %d runes", len([]rune(r.Error)))
	}
}
