package agent

import (
	"context"
	"errors"
	"math/rand/v2"
	"net"
	"sync"
	"time"

	"github.com/eyupio/zoomies/internal/backend"
)

// Foreground work is FIFO and goes ahead of background image refreshes.
// Enqueue happens in dispatch order, before goroutines can reorder a burst.
type startupQueue struct {
	mu      sync.Mutex
	active  *startupTicket
	pending []*startupTicket
}

type startupTicket struct {
	ready      chan struct{}
	foreground bool
}

func (q *startupQueue) enqueue(foreground bool) *startupTicket {
	q.mu.Lock()
	defer q.mu.Unlock()
	t := &startupTicket{ready: make(chan struct{}), foreground: foreground}
	q.pending = append(q.pending, t)
	q.advance()
	return t
}

func (q *startupQueue) advance() {
	if q.active != nil || len(q.pending) == 0 {
		return
	}
	i := 0
	for n, t := range q.pending {
		if t.foreground {
			i = n
			break
		}
	}
	q.active = q.pending[i]
	copy(q.pending[i:], q.pending[i+1:])
	q.pending[len(q.pending)-1] = nil
	q.pending = q.pending[:len(q.pending)-1]
	close(q.active.ready)
}

func (q *startupQueue) done(t *startupTicket) {
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.active == t {
		q.active = nil
	} else {
		for i, waiting := range q.pending {
			if waiting == t {
				copy(q.pending[i:], q.pending[i+1:])
				q.pending[len(q.pending)-1] = nil
				q.pending = q.pending[:len(q.pending)-1]
				break
			}
		}
	}
	q.advance()
}

// A failed runtime gets one new startup attempt after a bounded cooldown.
// Authentication, image configuration and ordinary workflow failures do not
// open this hold. Existing workloads and their lifecycle tasks keep running.
func (a *Agent) runtimeResult(err error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if err == nil {
		a.runtimeFailures = 0
		a.runtimeRetryAt = time.Time{}
		a.runtimeKind, a.runtimeError = "", ""
		return
	}
	// context.DeadlineExceeded satisfies net.Error with Timeout() true, so a
	// pull that outran the create budget, or a cancelled task, would otherwise
	// open the hold and stall every start behind it. The transport already
	// wraps a daemon that stopped answering as ErrUnavailable; only that and a
	// genuine network timeout say the runtime is the problem.
	var ne net.Error
	transportTimeout := errors.As(err, &ne) && ne.Timeout() &&
		!errors.Is(err, context.DeadlineExceeded) && !errors.Is(err, context.Canceled)
	if !errors.Is(err, backend.ErrUnavailable) && !transportTimeout {
		return
	}
	a.warmed = nil
	a.runtimeKind = RuntimeUnavailable
	if !errors.Is(err, backend.ErrUnavailable) {
		a.runtimeKind = RuntimeTimeout
	}
	a.runtimeError = truncateRunes(err.Error(), maxRuntimeError)
	a.runtimeFailures = min(a.runtimeFailures+1, 5)
	delay := min(5*time.Second*time.Duration(1<<(a.runtimeFailures-1)), time.Minute)
	delay = recoveryDelay(delay, a.randomFraction())
	a.runtimeRetryAt = a.now().Add(delay)
	a.log.Warn("container runtime failed; holding new starts before one recovery attempt",
		"error", err, "retry_in", delay, "consecutive_failures", a.runtimeFailures)
}

// maxRuntimeError bounds the error a heartbeat carries: it is shown on a host
// card and stored on the host row, and a daemon's reply can be a page long.
const maxRuntimeError = 500

// runtimeReport is the cooldown as the heartbeat carries it, nil when there
// is none. It reports a runtime whose retry time has passed too: the hold is
// over but the recovery attempt has not yet said whether it worked, and only
// a success clears it.
func (a *Agent) runtimeReport() *RuntimeReport {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.runtimeFailures == 0 {
		return nil
	}
	return &RuntimeReport{
		Failures: a.runtimeFailures,
		Kind:     a.runtimeKind,
		Error:    a.runtimeError,
		RetryIn:  max(0, a.runtimeRetryAt.Sub(a.now())),
	}
}

func truncateRunes(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "..."
}

func (a *Agent) waitForRuntime(ctx context.Context) bool {
	a.mu.Lock()
	wait := a.runtimeRetryAt.Sub(a.now())
	a.mu.Unlock()
	return sleepCtx(ctx, wait)
}

// recoveryDelay takes an injected draw for deterministic bounds/distribution tests.
// Jitter adds up to 25 percent and never shortens the cooldown floor.
func recoveryDelay(base time.Duration, draw float64) time.Duration {
	return base + time.Duration(float64(base)*0.25*max(0, min(1, draw)))
}

func (a *Agent) randomFraction() float64 {
	if a.opts.RandomFloat64 != nil {
		return max(0, min(1, a.opts.RandomFloat64()))
	}
	return rand.Float64()
}
