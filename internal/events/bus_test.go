package events

import (
	"context"
	"encoding/json"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestPublishReachesSubscribers(t *testing.T) {
	b := New()
	sub := b.Subscribe(context.Background(), SubscribeOptions{})
	defer sub.Close()

	b.Publish(KindRunnerUpdated, "runner:run_1", map[string]string{"state": "idle"})

	select {
	case e := <-sub.C:
		if e.Kind != KindRunnerUpdated {
			t.Errorf("kind = %s, want %s", e.Kind, KindRunnerUpdated)
		}
		if e.Topic != "runner:run_1" {
			t.Errorf("topic = %q", e.Topic)
		}
		var got map[string]string
		if err := json.Unmarshal(e.Data, &got); err != nil {
			t.Fatalf("payload: %v", err)
		}
		if got["state"] != "idle" {
			t.Errorf("payload = %v", got)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("no event arrived")
	}
}

func TestSubscribeFiltersByKindAndTopic(t *testing.T) {
	b := New()
	sub := b.Subscribe(context.Background(), SubscribeOptions{
		Kinds:       []Kind{KindRunnerUpdated},
		TopicPrefix: "runner:",
	})
	defer sub.Close()

	b.Publish(KindPoolUpdated, "pool:p1", nil)     // wrong kind
	b.Publish(KindRunnerUpdated, "host:h1", nil)   // wrong topic
	b.Publish(KindRunnerUpdated, "runner:r1", nil) // both right

	select {
	case e := <-sub.C:
		if e.Topic != "runner:r1" {
			t.Fatalf("got %s/%s; the filter let the wrong event through", e.Kind, e.Topic)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("the matching event never arrived")
	}
}

func TestReplayDeliversMissedEvents(t *testing.T) {
	b := New()
	b.Publish(KindScaling, "", map[string]int{"n": 1})
	after := b.LastID()
	b.Publish(KindScaling, "", map[string]int{"n": 2})

	// A client reconnecting with Last-Event-ID should be given what it missed.
	sub := b.Subscribe(context.Background(), SubscribeOptions{Replay: after})
	defer sub.Close()

	select {
	case e := <-sub.C:
		if e.ID != after+1 {
			t.Errorf("replayed event ID = %d, want %d", e.ID, after+1)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("nothing was replayed")
	}
}

func TestSlowSubscriberIsCutOffNotBlocking(t *testing.T) {
	b := New()
	b.buffer = 2
	sub := b.Subscribe(context.Background(), SubscribeOptions{})
	defer sub.Close()

	// A wedged browser tab must not be able to stop the fleet from scaling, so
	// publishing past a full queue never blocks.
	done := make(chan struct{})
	go func() {
		for i := 0; i < 500; i++ {
			b.Publish(KindHeartbeat, "", nil)
		}
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Publish blocked on a subscriber that was not reading")
	}

	// And the subscriber that fell behind is told so, by its feed ending. A
	// stream quietly thinned by dropped events would leave its dashboard wrong
	// until somebody reloaded it; an ended stream makes the browser reconnect
	// and replay. It keeps what it had already been handed.
	received := 0
	for range sub.C {
		received++
	}
	if received != b.buffer {
		t.Fatalf("received %d buffered events before the feed ended, want %d", received, b.buffer)
	}
	if n := b.Subscribers(); n != 0 {
		t.Fatalf("%d subscribers remain after the slow one was cut off, want 0", n)
	}
}

// Subscribe spawns a goroutine that closes the subscription when the context
// ends, so an explicit Close and that goroutine routinely race. Run this with
// -race: it is the regression test for an unsynchronised write in Close.
func TestCloseIsSafeConcurrentlyAndRepeatedly(t *testing.T) {
	b := New()
	for i := 0; i < 50; i++ {
		ctx, cancel := context.WithCancel(context.Background())
		sub := b.Subscribe(ctx, SubscribeOptions{})

		var wg sync.WaitGroup
		wg.Add(3)
		go func() { defer wg.Done(); cancel() }()
		go func() { defer wg.Done(); sub.Close() }()
		go func() { defer wg.Done(); sub.Close() }()
		wg.Wait()
		cancel()
	}
	// Give the context watchers a moment, then assert nothing leaked.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) && b.Subscribers() != 0 {
		time.Sleep(10 * time.Millisecond)
	}
	if n := b.Subscribers(); n != 0 {
		t.Errorf("%d subscriptions leaked", n)
	}
}

func TestContextCancellationUnsubscribes(t *testing.T) {
	b := New()
	ctx, cancel := context.WithCancel(context.Background())
	b.Subscribe(ctx, SubscribeOptions{})
	if b.Subscribers() != 1 {
		t.Fatalf("subscribers = %d, want 1", b.Subscribers())
	}
	cancel()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) && b.Subscribers() != 0 {
		time.Sleep(10 * time.Millisecond)
	}
	if n := b.Subscribers(); n != 0 {
		t.Errorf("subscribers = %d after cancellation, want 0", n)
	}
}

// A subscriber closing while a publish is in flight is the everyday case: a
// browser tab closes and its SSE handler's context ends while the reconcile
// loop is announcing a state change. Sending on the closed channel panics,
// and the loop that was publishing does not come back from a panic.
func TestPublishRacesClose(t *testing.T) {
	b := New()
	for i := 0; i < 2000; i++ {
		sub := b.Subscribe(context.Background(), SubscribeOptions{})
		var wg sync.WaitGroup
		wg.Add(2)
		go func() {
			defer wg.Done()
			b.Publish(KindRunnerUpdated, "runner:r1", map[string]string{"id": "r1"})
		}()
		go func() {
			defer wg.Done()
			sub.Close()
		}()
		wg.Wait()
	}
	if n := b.Subscribers(); n != 0 {
		t.Errorf("%d subscriptions leaked", n)
	}
}

// Replay used to be silent about what it could not do: a client whose last id
// predated the ring, or whose id came from before a restart, was handed
// nothing and told nothing. Subscribe now says whether the gap was covered.
func TestReplayReportsWhetherItCoveredTheGap(t *testing.T) {
	b := New()
	b.ringCap = 3
	for i := range 6 {
		b.Publish(KindScaling, "", map[string]int{"n": i})
	}
	last := b.LastID()

	cases := []struct {
		name     string
		replay   uint64
		complete bool
	}{
		{"a fresh subscription asks for nothing and misses nothing", 0, true},
		{"the id just before the ring's oldest is covered", last - 3, true},
		{"an id the ring no longer reaches back to is not", 1, false},
		{"the latest id, with nothing since, is covered", last, true},
		{"an id from another run of the process is not", last + 40, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			sub := b.Subscribe(context.Background(), SubscribeOptions{Replay: tc.replay})
			defer sub.Close()
			if sub.Complete != tc.complete {
				t.Fatalf("Complete = %v, want %v", sub.Complete, tc.complete)
			}
		})
	}

	t.Run("a caller that knows better can mark it incomplete", func(t *testing.T) {
		sub := b.Subscribe(context.Background(), SubscribeOptions{Replay: last, Incomplete: true})
		defer sub.Close()
		if sub.Complete {
			t.Fatal("Incomplete was ignored")
		}
	})
}

// IDs restart at one with every process, so an id alone cannot say which
// sequence it came from. On the wire it carries the run's epoch.
func TestWireIDsCarryTheEpoch(t *testing.T) {
	b := New()
	wire := b.WireID(42)
	if !strings.HasPrefix(wire, b.Epoch()+".") || !strings.HasSuffix(wire, ".42") {
		t.Fatalf("WireID(42) = %q, want <epoch>.42", wire)
	}
	if id, same := b.ParseWireID(wire); id != 42 || !same {
		t.Fatalf("ParseWireID(%q) = %d, %v; want 42 in this epoch", wire, id, same)
	}
	if id, same := b.ParseWireID("otherrun.42"); id != 42 || same {
		t.Fatalf("an id from another epoch parsed as %d, %v; want 42 and not this epoch", id, same)
	}
	// A bare number is what a client that predates the epoch sends; it is
	// taken as this epoch's so an upgrade does not resync every tab.
	if id, same := b.ParseWireID("42"); id != 42 || !same {
		t.Fatalf("a bare id parsed as %d, %v", id, same)
	}
	if id, same := b.ParseWireID("not-an-id"); id != 0 || same {
		t.Fatalf("garbage parsed as %d, %v", id, same)
	}
	other := New()
	if other.Epoch() == b.Epoch() {
		t.Fatal("two buses share an epoch; a restart would look like the same run")
	}
}

// Subscribe starts a goroutine to close the subscription when its context
// ends. The SSE handler closes its subscription itself, before the request's
// context is cancelled, and a long-lived caller may pass a context that is
// never cancelled at all; either way that goroutine used to wait for ever, one
// per subscription for the life of the process.
func TestClosingASubscriptionEndsItsContextWatcher(t *testing.T) {
	b := New()
	before := runtime.NumGoroutine()
	for i := 0; i < 50; i++ {
		b.Subscribe(context.Background(), SubscribeOptions{}).Close()
	}
	deadline := time.Now().Add(2 * time.Second)
	for runtime.NumGoroutine() > before+5 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if n := runtime.NumGoroutine(); n > before+5 {
		t.Fatalf("%d goroutines after closing 50 subscriptions, %d before them", n, before)
	}
}

// The ring and the wire have to agree with the sequence.
//
// Every event carries the number a reconnecting client comes back with, and
// the SSE handler replays everything above it. So an event that reaches the
// ring after one numbered above it is an event that client will never be sent
// -- and Subscribe, which reads the ring's first entry as the oldest number it
// holds, would still call the replay complete and send no resync. The number
// used to be drawn before the lock, which is exactly the window two publishers
// need to swap places.
func TestConcurrentPublishesKeepTheRingInSequence(t *testing.T) {
	b := New()
	const writers, each = 8, 64

	var wg sync.WaitGroup
	for w := 0; w < writers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < each; i++ {
				b.Publish(KindStats, "", map[string]int{"n": i})
			}
		}()
	}
	wg.Wait()

	b.mu.RLock()
	ring := append([]Event(nil), b.ring...)
	b.mu.RUnlock()

	if len(ring) == 0 {
		t.Fatal("nothing reached the ring")
	}
	for i := 1; i < len(ring); i++ {
		if ring[i].ID <= ring[i-1].ID {
			t.Fatalf("ring entry %d has id %d, after id %d: a client resuming from the higher one never sees the lower",
				i, ring[i].ID, ring[i-1].ID)
		}
	}
	if got, want := ring[len(ring)-1].ID, uint64(writers*each); got != want {
		t.Fatalf("the last event is %d, want %d", got, want)
	}
}

// A tab reconnecting to a busy fleet must not be cut off by its own replay.
//
// The replay is delivered before the subscriber has read anything, so a queue
// only as deep as the ring is full the instant the subscription exists. The
// next publish then finds no room and ends the feed -- and the tab reconnects
// to a ring that is still full and is ended again, on the fleet that most
// needs watching.
func TestAFullReplayLeavesRoomForTheNextEvent(t *testing.T) {
	b := New()
	// One more than the ring holds, so a replay from the first id reaches
	// back to the oldest entry still in it and carries every one of them.
	for i := 0; i <= b.ringCap; i++ {
		b.Publish(KindStats, "", map[string]int{"n": i})
	}

	sub := b.Subscribe(context.Background(), SubscribeOptions{Replay: 1})
	defer sub.Close()
	if !sub.Complete {
		t.Fatal("a replay reaching back to the oldest event in the ring reported itself incomplete")
	}

	b.Publish(KindStats, "", map[string]int{"n": -1})
	for i := 0; i < b.ringCap; i++ {
		if _, ok := <-sub.C; !ok {
			t.Fatalf("the feed ended after %d replayed events; the subscriber was dropped by its own replay", i)
		}
	}
	if _, ok := <-sub.C; !ok {
		t.Fatal("the feed ended before the event published after the replay")
	}
}

// A resync is about the connections it was sent to and about no other.
//
// It means "what you hold may be stale, fetch the resources again", which is
// never true of a client that connects afterwards -- that one is already
// fetching everything. Kept in the ring, the prune's resync would meet each
// new tab on its first reconnect and send it straight back to load a fleet it
// had only just loaded.
func TestATransientEventReachesSubscribersWithoutEnteringTheRing(t *testing.T) {
	b := New()
	sub := b.Subscribe(context.Background(), SubscribeOptions{})
	defer sub.Close()

	b.PublishTransient(KindResync, "", map[string]string{"reason": "everything went"})

	select {
	case ev := <-sub.C:
		if ev.Kind != KindResync {
			t.Fatalf("the subscriber got %q, want a resync", ev.Kind)
		}
		if ev.ID == 0 {
			t.Fatal("a transient event carried no id, so a client resuming from it would be sent what it has already seen")
		}
	case <-time.After(time.Second):
		t.Fatal("the subscriber never got the transient event")
	}

	b.mu.RLock()
	ring := append([]Event(nil), b.ring...)
	b.mu.RUnlock()
	for _, e := range ring {
		if e.Kind == KindResync {
			t.Fatal("the resync is in the ring; a client reconnecting later would be told to refetch for nothing")
		}
	}

	// The number it drew still belongs to the sequence, so an ordinary event
	// published after it follows on.
	b.Publish(KindStats, "", map[string]int{"n": 1})
	later := b.Subscribe(context.Background(), SubscribeOptions{Replay: 1})
	defer later.Close()
	if !later.Complete {
		t.Fatal("a replay across the transient event reported itself incomplete")
	}
}

// A reconnecting client and a tool asking "since id 3" must get the same
// answer, or the stream and the route disagree about what was lost.
func TestRecentAgreesWithTheStreamsReplay(t *testing.T) {
	b := New()
	for i := range 5 {
		b.Publish(KindScaling, "", map[string]int{"n": i})
	}

	items, complete, truncated := b.Recent(3, SubscribeOptions{}, 0)
	if !complete || truncated {
		t.Fatalf("complete = %v, truncated = %v, want true and false", complete, truncated)
	}
	if len(items) != 2 || items[0].ID != 4 || items[1].ID != 5 {
		t.Fatalf("Recent(3) = %v, want ids 4 and 5", ids(items))
	}

	sub := b.Subscribe(context.Background(), SubscribeOptions{Replay: 3})
	defer sub.Close()
	if sub.Complete != complete {
		t.Fatalf("the stream says complete = %v and Recent says %v", sub.Complete, complete)
	}
	for _, want := range items {
		select {
		case got := <-sub.C:
			if got.ID != want.ID {
				t.Fatalf("the stream replayed id %d where Recent returned %d", got.ID, want.ID)
			}
		case <-time.After(2 * time.Second):
			t.Fatalf("the stream never replayed id %d", want.ID)
		}
	}
	select {
	case got := <-sub.C:
		t.Fatalf("the stream replayed id %d, which Recent did not return", got.ID)
	default:
	}
}

func TestRecentSaysIncompleteWhenTheRingMovedOn(t *testing.T) {
	b := New()
	b.ringCap = 4
	for i := range 10 {
		b.Publish(KindScaling, "", map[string]int{"n": i})
	}

	items, complete, _ := b.Recent(2, SubscribeOptions{}, 0)
	if complete {
		t.Fatal("Recent claimed to be complete though the ring no longer reaches id 3")
	}
	if got := ids(items); len(got) != 4 || got[0] != 7 || got[3] != 10 {
		t.Fatalf("Recent(2) = %v, want what the ring holds, ids 7 to 10", got)
	}

	t.Run("an id from another run of the process is incomplete and empty", func(t *testing.T) {
		items, complete, truncated := b.Recent(b.LastID()+40, SubscribeOptions{}, 0)
		if complete || truncated || len(items) != 0 {
			t.Fatalf("got %v, complete %v, truncated %v", ids(items), complete, truncated)
		}
	})
	t.Run("the latest id has nothing since and misses nothing", func(t *testing.T) {
		items, complete, truncated := b.Recent(b.LastID(), SubscribeOptions{}, 0)
		if !complete || truncated || len(items) != 0 {
			t.Fatalf("got %v, complete %v, truncated %v", ids(items), complete, truncated)
		}
	})
	t.Run("an empty ring is complete from the start and incomplete from anywhere later", func(t *testing.T) {
		empty := New()
		if items, complete, _ := empty.Recent(0, SubscribeOptions{}, 0); !complete || len(items) != 0 {
			t.Fatalf("since 0: got %v, complete %v", ids(items), complete)
		}
		if _, complete, _ := empty.Recent(5, SubscribeOptions{}, 0); complete {
			t.Fatal("an id beyond an empty sequence was reported complete")
		}
	})
}

func TestRecentFiltersByKindAndTopicAndTruncatesOldestFirst(t *testing.T) {
	b := New()
	b.Publish(KindRunnerUpdated, "runner:a", nil)
	b.Publish(KindPoolUpdated, "pool:a", nil)
	b.Publish(KindRunnerUpdated, "runner:b", nil)
	b.Publish(KindRunnerUpdated, "pool:b", nil)
	b.Publish(KindRunnerUpdated, "runner:c", nil)

	items, complete, truncated := b.Recent(0, SubscribeOptions{Kinds: []Kind{KindRunnerUpdated}}, 0)
	if !complete || truncated || len(items) != 4 {
		t.Fatalf("kind filter: got %v, complete %v, truncated %v", ids(items), complete, truncated)
	}

	items, _, _ = b.Recent(0, SubscribeOptions{TopicPrefix: "runner:"}, 0)
	if got := ids(items); len(got) != 3 || got[0] != 1 || got[1] != 3 || got[2] != 5 {
		t.Fatalf("topic filter: got %v, want ids 1, 3 and 5", got)
	}

	items, _, truncated = b.Recent(0, SubscribeOptions{}, 2)
	if got := ids(items); len(got) != 2 || got[0] != 1 || got[1] != 2 || !truncated {
		t.Fatalf("limit 2 of 5: got %v, truncated %v, want the oldest two and true", got, truncated)
	}

	items, _, truncated = b.Recent(0, SubscribeOptions{}, 5)
	if len(items) != 5 || truncated {
		t.Fatalf("a limit equal to the matches: got %d, truncated %v, want 5 and false", len(items), truncated)
	}
}

func ids(events []Event) []uint64 {
	out := make([]uint64, 0, len(events))
	for _, e := range events {
		out = append(out, e.ID)
	}
	return out
}

func TestMatchesIsTheOneRuleForKindsAndTopics(t *testing.T) {
	ev := Event{Kind: KindJobUpdated, Topic: "job:job_1"}
	for _, tc := range []struct {
		name string
		opts SubscribeOptions
		want bool
	}{
		{"no filter takes everything", SubscribeOptions{}, true},
		{"a listed kind", SubscribeOptions{Kinds: []Kind{KindRunnerUpdated, KindJobUpdated}}, true},
		{"an unlisted kind", SubscribeOptions{Kinds: []Kind{KindRunnerUpdated}}, false},
		{"a topic prefix", SubscribeOptions{TopicPrefix: "job:"}, true},
		{"another topic prefix", SubscribeOptions{TopicPrefix: "runner:"}, false},
		{"both must hold", SubscribeOptions{Kinds: []Kind{KindJobUpdated}, TopicPrefix: "runner:"}, false},
	} {
		if got := tc.opts.Matches(ev); got != tc.want {
			t.Errorf("%s: Matches = %v, want %v", tc.name, got, tc.want)
		}
	}
}

// The statistics, the problems list and the host cards are worked out for the
// event stream, and they are dear, so they are worked out only for somebody who
// could be sent them. A subscription that named kinds asked for those and nothing
// else; one that named none asked for everything; and a subscription that has
// closed asked for nothing.
func TestWantsIsTrueOnlyForKindsAnOpenSubscriptionAsksFor(t *testing.T) {
	b := New()
	ctx := context.Background()

	if b.Wants(KindStats) {
		t.Fatal("a bus nobody has subscribed to wants a kind")
	}

	jobs := b.Subscribe(ctx, SubscribeOptions{Kinds: []Kind{KindJobUpdated, KindRunnerUpdated}})
	defer jobs.Close()
	for kind, want := range map[Kind]bool{
		KindJobUpdated: true, KindRunnerUpdated: true,
		KindStats: false, KindProblems: false, KindHostUpdated: false,
	} {
		if got := b.Wants(kind); got != want {
			t.Errorf("with one subscriber for jobs and runners, Wants(%s) = %v; want %v", kind, got, want)
		}
	}

	everything := b.Subscribe(ctx, SubscribeOptions{})
	for _, kind := range []Kind{KindStats, KindProblems, KindHostUpdated, KindJobUpdated} {
		if !b.Wants(kind) {
			t.Errorf("with a subscriber that named no kinds, Wants(%s) = false; it asked for them all", kind)
		}
	}
	everything.Close()
	if b.Wants(KindStats) {
		t.Error("a subscription that closed still makes the bus want a kind")
	}

	// A topic narrows which events of a kind arrive, not which kinds: the host
	// cards are worked out before any topic exists, so a subscriber that named a
	// topic and no kind is still owed all of them.
	topical := b.Subscribe(ctx, SubscribeOptions{TopicPrefix: "job:"})
	defer topical.Close()
	if !b.Wants(KindStats) {
		t.Error("a subscriber that named a topic and no kind was not counted as wanting every kind")
	}
}
