package store

import (
	"context"
	"database/sql"
	"os"
	"regexp"
	"sync"
	"testing"
	"time"
)

// The writer's queue is what a busy instance runs out of first, so what the
// observer is told has to be what happened: one report per write, the wait a
// write spent behind another, and the hold of the write that kept it waiting.
func TestTheObserverIsToldHowLongEachWriteWaitedAndHeld(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	var mu sync.Mutex
	var waits, holds []time.Duration
	s.ObserveWrites(func(waited, held time.Duration) {
		mu.Lock()
		defer mu.Unlock()
		waits, holds = append(waits, waited), append(holds, held)
	})

	// A transaction that holds the writer, and a write that arrives while it
	// does. The margins are generous on purpose: this is about the shape of
	// what is reported, not the scheduler's punctuality.
	const hold = 150 * time.Millisecond
	inside := make(chan struct{})
	done := make(chan error, 1)
	go func() {
		done <- s.tx(ctx, func(*sql.Tx) error {
			close(inside)
			time.Sleep(hold)
			return nil
		})
	}()
	<-inside
	if _, err := s.exec(ctx, `SELECT 1`); err != nil {
		t.Fatalf("exec: %v", err)
	}
	if err := <-done; err != nil {
		t.Fatalf("tx: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()
	if len(waits) != 2 {
		t.Fatalf("the observer heard about %d writes, want 2: every write is reported, once", len(waits))
	}
	// Not by position: the observer runs after the writer is released, so the
	// write that waited can finish and report before the transaction that held
	// it up gets round to reporting. The longest hold is the transaction's and
	// the longest wait is the other write's, whichever order they arrived in.
	if longest := max(holds[0], holds[1]); longest < hold*2/3 {
		t.Errorf("the longest hold reported is %v, want about %v: a hold reported short hides the write that caused the queue", longest, hold)
	}
	if longest := max(waits[0], waits[1]); longest < hold/3 {
		t.Errorf("the longest wait reported is %v, want a good part of %v: the wait is the figure that says the writer is saturated", longest, hold)
	}
}

// Nothing observing is the state every CLI command and test runs in, and a
// write must not care.
func TestAWriteNeedsNoObserver(t *testing.T) {
	s := newTestStore(t)
	s.ObserveWrites(nil)
	if _, err := s.exec(context.Background(), `SELECT 1`); err != nil {
		t.Fatalf("exec with no observer: %v", err)
	}
}

// lockWriter is the one place the writer is taken, because a lock taken
// anywhere else is a write the measurement cannot see -- and a backup, which
// holds the writer for a whole VACUUM INTO, is exactly the write most worth
// seeing.
func TestTheWriterIsOnlyEverTakenThroughLockWriter(t *testing.T) {
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	direct := regexp.MustCompile(`\bwmu\.Lock\(\)`)
	found := 0
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !regexp.MustCompile(`\.go$`).MatchString(name) || regexp.MustCompile(`_test\.go$`).MatchString(name) {
			continue
		}
		b, err := os.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		found += len(direct.FindAll(b, -1))
	}
	if found != 1 {
		t.Errorf("wmu.Lock() appears %d times in the store; want once, inside lockWriter, so every write is measured", found)
	}
}
