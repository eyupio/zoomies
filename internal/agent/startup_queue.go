package agent

import (
	"context"
	"errors"
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
	a.runtimeFailures = min(a.runtimeFailures+1, 5)
	delay := min(5*time.Second*time.Duration(1<<(a.runtimeFailures-1)), time.Minute)
	a.runtimeRetryAt = a.now().Add(delay)
	a.log.Warn("container runtime failed; holding new starts before one recovery attempt",
		"error", err, "retry_in", delay, "consecutive_failures", a.runtimeFailures)
}

func (a *Agent) waitForRuntime(ctx context.Context) bool {
	a.mu.Lock()
	wait := a.runtimeRetryAt.Sub(a.now())
	a.mu.Unlock()
	return sleepCtx(ctx, wait)
}
