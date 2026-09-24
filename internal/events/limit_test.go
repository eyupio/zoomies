package events

import (
	"context"
	"testing"
)

// limits.event_subscribers is enforced here rather than by counting first and
// subscribing after, so that the ceiling holds for streams opened together.
// Closing one has to give its place back, or a browser that reconnects would
// find the instance permanently full of its own dead streams.
func TestSubscribeWithinRefusesAtTheCeilingAndFreesAPlaceOnClose(t *testing.T) {
	b := New()
	ctx := context.Background()

	first, ok := b.SubscribeWithin(ctx, SubscribeOptions{}, 2)
	if !ok {
		t.Fatal("the first subscription was refused under a ceiling of 2")
	}
	if _, ok := b.SubscribeWithin(ctx, SubscribeOptions{}, 2); !ok {
		t.Fatal("the second subscription was refused under a ceiling of 2")
	}
	if s, ok := b.SubscribeWithin(ctx, SubscribeOptions{}, 2); ok || s != nil {
		t.Fatalf("a third subscription was admitted under a ceiling of 2 (%d open)", b.Subscribers())
	}
	if n := b.Subscribers(); n != 2 {
		t.Fatalf("a refused subscription was still counted: %d open", n)
	}

	first.Close()
	if _, ok := b.SubscribeWithin(ctx, SubscribeOptions{}, 2); !ok {
		t.Fatal("closing a subscription did not free its place")
	}
	if _, ok := b.SubscribeWithin(ctx, SubscribeOptions{}, 0); !ok {
		t.Fatal("a ceiling of 0 refused a subscription; 0 is unlimited")
	}
}
