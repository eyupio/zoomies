package agent

import (
	"bytes"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

// drain takes everything a closed buffer still holds.
func drain(t *testing.T, b *relayBuffer) [][]byte {
	t.Helper()
	var chunks [][]byte
	for {
		chunk, ok := b.next()
		if !ok {
			return chunks
		}
		chunks = append(chunks, chunk)
	}
}

var fellBehind = regexp.MustCompile(`\n\[log stream fell behind, (\d+) bytes dropped\]\n`)

// accounted splits relayed output into what the runner said and what the relay
// said it had dropped, so a test can ask whether the two add up.
func accounted(output string) (payload string, dropped int) {
	for _, m := range fellBehind.FindAllStringSubmatch(output, -1) {
		n, _ := strconv.Atoi(m[1])
		dropped += n
	}
	return fellBehind.ReplaceAllString(output, ""), dropped
}

// A hundred bytes a read is a thousand writes to the controller if each read is
// passed on as it came, and the writer then runs slower than the source for the
// whole of a backlog. Coalescing is what lets it catch up.
func TestRelayBufferCoalescesShortReadsIntoFewWrites(t *testing.T) {
	b := newRelayBuffer()
	var want strings.Builder
	for i := range 1000 {
		line := fmt.Sprintf("line %04d %s\n", i, strings.Repeat("x", 90))
		want.WriteString(line)
		b.add([]byte(line))
	}
	b.close()

	chunks := drain(t, b)
	var got bytes.Buffer
	for _, c := range chunks {
		if len(c) > relayChunkSize {
			t.Fatalf("a chunk of %d bytes, want at most %d", len(c), relayChunkSize)
		}
		got.Write(c)
	}
	if got.String() != want.String() {
		t.Fatal("the buffer changed the output it was given")
	}
	if limit := want.Len()/relayChunkSize + 2; len(chunks) > limit {
		t.Fatalf("%d chunks for %d bytes, want at most %d", len(chunks), want.Len(), limit)
	}
}

// The reader hands over the same slice on every read, so what the buffer keeps
// has to be its own.
func TestRelayBufferCopiesWhatItIsGiven(t *testing.T) {
	b := newRelayBuffer()
	p := []byte("hello\n")
	b.add(p)
	copy(p, "XXXXXX")

	chunk, ok := b.next()
	if !ok || string(chunk) != "hello\n" {
		t.Fatalf("got %q, want the output as it was when it was added", chunk)
	}
}

// The writer is still sending a chunk while the reader goes on adding, so a
// chunk that has been handed out must never be written to again.
func TestRelayBufferNeverChangesAChunkTheWriterIsHolding(t *testing.T) {
	b := newRelayBuffer()
	b.add([]byte("first\n"))
	held, _ := b.next()
	b.add([]byte("second\n"))

	if string(held) != "first\n" {
		t.Fatalf("the chunk being written became %q", held)
	}
	if next, _ := b.next(); string(next) != "second\n" {
		t.Fatalf("the next chunk was %q", next)
	}
}

// A read that does not fit is refused whole rather than cut, and the viewer is
// told how much it missed at the point it missed it: ahead of the next output
// that is accepted, not at the end of a log it may never reach.
func TestRelayBufferRefusesWholeReadsWhenFullAndSaysWhatItMissed(t *testing.T) {
	b := newRelayBuffer()
	read := bytes.Repeat([]byte("a"), relayChunkSize)
	reads := relayBufferBytes / relayChunkSize
	for range reads {
		b.add(read)
	}
	b.add([]byte("lost one\n"))
	b.add([]byte("lost two\n"))
	for range reads {
		if _, ok := b.next(); !ok {
			t.Fatal("the buffer ended before it was drained")
		}
	}

	b.add([]byte("after the gap\n"))
	b.close()
	chunks := drain(t, b)
	if len(chunks) != 1 {
		t.Fatalf("%d chunks after the gap, want the notice and the output that followed it together", len(chunks))
	}
	if want := "\n[log stream fell behind, 18 bytes dropped]\nafter the gap\n"; string(chunks[0]) != want {
		t.Fatalf("got %q, want %q", chunks[0], want)
	}
}

// The runner has stopped producing, so making room for the notice throttles
// nothing, and a viewer is always told what was lost even when it was lost in
// the last moments of the stream.
func TestRelayBufferReportsWhatItDroppedWhenTheStreamEndsFull(t *testing.T) {
	b := newRelayBuffer()
	read := bytes.Repeat([]byte("a"), relayChunkSize)
	for range relayBufferBytes / relayChunkSize {
		b.add(read)
	}
	b.add([]byte("lost\n"))
	b.close()

	var all bytes.Buffer
	for _, c := range drain(t, b) {
		all.Write(c)
	}
	payload, dropped := accounted(all.String())
	if dropped != 5 {
		t.Fatalf("told %d bytes were dropped, want 5", dropped)
	}
	if len(payload) != relayBufferBytes {
		t.Fatalf("kept %d bytes of payload, want %d", len(payload), relayBufferBytes)
	}
	if !strings.HasSuffix(all.String(), "5 bytes dropped]\n") {
		t.Fatal("the notice was not the last thing the viewer was told")
	}
}

func TestRelayBufferWaitsForOutputAndEndsWhenClosed(t *testing.T) {
	b := newRelayBuffer()
	got := make(chan string)
	go func() {
		defer close(got)
		for {
			chunk, ok := b.next()
			if !ok {
				return
			}
			got <- string(chunk)
		}
	}()

	select {
	case chunk := <-got:
		t.Fatalf("next returned %q with nothing in the buffer", chunk)
	case <-time.After(50 * time.Millisecond):
	}

	b.add([]byte("first\n"))
	select {
	case chunk := <-got:
		if chunk != "first\n" {
			t.Fatalf("got %q", chunk)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("next slept through an add")
	}

	b.close()
	select {
	case _, open := <-got:
		if open {
			t.Fatal("next returned output after the buffer was closed and empty")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("next did not end when the buffer was closed")
	}
}

// A container whose stdout nobody drains eventually stops making progress, so
// the reader must get to the end of a runner's output even when the controller
// has taken none of it. The writer here takes nothing until the reader is done:
// a buffer that waited for room would never let it finish.
func TestRelayBufferNeverMakesTheReaderWaitForAStalledWriter(t *testing.T) {
	b := newRelayBuffer()
	produced := make(chan struct{})
	go func() {
		defer close(produced)
		for range 4 * relayBufferBytes / relayChunkSize {
			b.add(bytes.Repeat([]byte("a"), relayChunkSize))
		}
		b.close()
	}()
	select {
	case <-produced:
	case <-time.After(10 * time.Second):
		t.Fatal("the reader was made to wait for a writer that was not taking anything")
	}

	var all bytes.Buffer
	for _, c := range drain(t, b) {
		all.Write(c)
	}
	payload, dropped := accounted(all.String())
	if len(payload) != relayBufferBytes || dropped != 3*relayBufferBytes {
		t.Fatalf("kept %d bytes and counted %d dropped, want %d kept and %d dropped",
			len(payload), dropped, relayBufferBytes, 3*relayBufferBytes)
	}
}

// Whatever the writer's pace, every byte the runner said is either delivered or
// counted in a notice. Nothing is lost without the viewer being told.
func TestRelayBufferAccountsForEveryByteAgainstASlowWriter(t *testing.T) {
	b := newRelayBuffer()
	const reads, size = 3000, 4 << 10

	var delivered strings.Builder
	var writer sync.WaitGroup
	writer.Add(1)
	go func() {
		defer writer.Done()
		for {
			chunk, ok := b.next()
			if !ok {
				return
			}
			delivered.Write(chunk)
			time.Sleep(time.Millisecond)
		}
	}()

	for i := range reads {
		b.add(bytes.Repeat([]byte{byte('a' + i%26)}, size))
	}
	b.close()
	writer.Wait()

	payload, dropped := accounted(delivered.String())
	if got, want := len(payload)+dropped, reads*size; got != want {
		t.Fatalf("%d bytes delivered or counted as dropped, but the runner said %d", got, want)
	}
}
