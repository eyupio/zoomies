package agent

import (
	"fmt"
	"sync"
)

// relayBuffer is the hand-off between the goroutine reading a runner's output
// and the one writing it to the controller.
//
// It is bounded in bytes, not in reads, because how many reads a backlog takes
// is the source's choice. Docker frames its log stream a record to a frame, so a
// thousand-line backlog is a thousand reads of about a hundred bytes each, and a
// queue of sixty-four reads held sixty-four lines of it however much room there
// was. Output is coalesced into chunks of up to relayChunkSize as it goes in, so
// the writer takes a few large writes where the source made a thousand small
// ones, and catches up with a burst instead of falling further behind it.
type relayBuffer struct {
	// wake carries a token whenever there is something for next to find, so a
	// writer that has just looked and found nothing cannot sleep through an add.
	wake chan struct{}

	mu      sync.Mutex
	chunks  [][]byte // oldest first; the newest is still being filled
	queued  int      // bytes held in chunks
	dropped int64    // bytes refused since the viewer was last told
	closed  bool
}

func newRelayBuffer() *relayBuffer {
	return &relayBuffer{wake: make(chan struct{}, 1)}
}

// add takes a copy of p, or refuses it when the buffer is full. It never waits:
// a container whose stdout nobody drains eventually stops making progress, so a
// slow viewer must not be able to stall a build.
//
// A read that does not fit is refused whole, because half a line is worse than
// a gap. The viewer is told what it missed at the point it missed it: the notice
// goes in ahead of the next output that is accepted.
func (b *relayBuffer) add(p []byte) {
	b.mu.Lock()
	if b.queued+len(p) > relayBufferBytes {
		b.dropped += int64(len(p))
		b.mu.Unlock()
		return
	}
	b.reportDropped()
	b.put(p)
	b.mu.Unlock()
	b.signal()
}

// close ends the buffer. What was refused since the viewer was last told is
// reported however full the buffer is: the runner has stopped producing, so
// making room throttles nothing, and a viewer must always be told what was
// lost, even when it was lost in the last moments of the stream.
func (b *relayBuffer) close() {
	b.mu.Lock()
	b.reportDropped()
	b.closed = true
	b.mu.Unlock()
	b.signal()
}

// next returns the oldest chunk, waiting for one to arrive, and reports false
// once the buffer is closed and everything in it has been taken.
func (b *relayBuffer) next() ([]byte, bool) {
	for {
		b.mu.Lock()
		if len(b.chunks) > 0 {
			chunk := b.chunks[0]
			b.chunks[0] = nil
			b.chunks = b.chunks[1:]
			b.queued -= len(chunk)
			b.mu.Unlock()
			return chunk, true
		}
		closed := b.closed
		b.mu.Unlock()
		if closed {
			return nil, false
		}
		<-b.wake
	}
}

// reportDropped queues the notice for whatever was refused since the last one.
// The caller holds the lock.
func (b *relayBuffer) reportDropped() {
	if b.dropped == 0 {
		return
	}
	b.put([]byte(fmt.Sprintf("\n[log stream fell behind, %d bytes dropped]\n", b.dropped)))
	b.dropped = 0
}

// put copies p onto the chunk being filled, or starts the next one. The caller
// holds the lock. A chunk that next has handed out is no longer in the list, so
// nothing here can touch bytes the writer is still sending.
func (b *relayBuffer) put(p []byte) {
	if n := len(b.chunks); n > 0 && len(b.chunks[n-1])+len(p) <= relayChunkSize {
		b.chunks[n-1] = append(b.chunks[n-1], p...)
	} else {
		b.chunks = append(b.chunks, append([]byte(nil), p...))
	}
	b.queued += len(p)
}

func (b *relayBuffer) signal() {
	select {
	case b.wake <- struct{}{}:
	default:
	}
}
