package github

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/eyupio/zoomies/internal/store"
)

// The reset GitHub sends is the difference between waiting exactly as long as
// the quota needs and guessing a flat fifteen minutes -- which is either most
// of a window wasted or most of a window spent on refusals. It used to be
// formatted into the message and lost, so this checks it survives as a number.
func TestARateLimitCarriesWhenToComeBack(t *testing.T) {
	now := time.Date(2025, 3, 4, 12, 0, 0, 0, time.UTC)
	reset := now.Add(9 * time.Minute)

	err := error(&RateLimitedError{ResetAt: reset})
	if !errors.Is(err, ErrRateLimited) {
		t.Fatal("callers that only ask whether it was a rate limit must still get yes")
	}
	at, ok := RetryAfterRateLimit(err, now)
	if !ok || !at.Equal(reset) {
		t.Fatalf("RetryAfterRateLimit = %v, %v; want %v, true", at, ok, reset)
	}
	if !strings.Contains(err.Error(), reset.Format(time.RFC3339)) {
		t.Fatalf("the message still has to name the time for an operator: %q", err)
	}
}

// GitHub's secondary limit answers with a duration rather than an instant.
func TestASecondaryRateLimitCarriesItsRetryAfter(t *testing.T) {
	now := time.Date(2025, 3, 4, 12, 0, 0, 0, time.UTC)
	err := error(&RateLimitedError{RetryAfter: 90 * time.Second})

	at, ok := RetryAfterRateLimit(err, now)
	if !ok || !at.Equal(now.Add(90*time.Second)) {
		t.Fatalf("RetryAfterRateLimit = %v, %v", at, ok)
	}
}

// A reset already behind us is no answer at all: clocks drift and a response
// can sit in a queue, and resuming on it would buy another refusal. The caller
// is told nothing rather than told to go now.
func TestARateLimitResetInThePastIsNotAnAnswer(t *testing.T) {
	now := time.Date(2025, 3, 4, 12, 0, 0, 0, time.UTC)
	err := error(&RateLimitedError{ResetAt: now.Add(-time.Minute)})

	if at, ok := RetryAfterRateLimit(err, now); ok {
		t.Fatalf("a reset in the past was offered as a resume time: %v", at)
	}
}

// Anything that is not a rate limit must not be mistaken for one.
func TestOtherFailuresCarryNoResumeTime(t *testing.T) {
	now := time.Date(2025, 3, 4, 12, 0, 0, 0, time.UTC)
	for _, err := range []error{ErrForbidden, ErrNotFound, errors.New("boom"), nil} {
		if _, ok := RetryAfterRateLimit(err, now); ok {
			t.Fatalf("%v was read as a rate limit", err)
		}
	}
}

// The end of the same story, through a real client against a real response:
// what GitHub sends has to arrive as a number a caller can wait on. The tests
// above build the error by hand and would go on passing if classify stopped
// producing it.
func TestAGitHubRefusalForQuotaArrivesWithItsResetTime(t *testing.T) {
	f := newFake(t)
	reset := time.Now().Add(11 * time.Minute).Truncate(time.Second)
	f.SetRateLimit(5000, 0, reset)
	f.SetError("/actions/runners", http.StatusForbidden, "API rate limit exceeded")

	_, err := f.Client("acme", store.TargetOrg).ListRunners(context.Background())
	if !errors.Is(err, ErrRateLimited) {
		t.Fatalf("got %v, want a rate limit", err)
	}
	at, ok := RetryAfterRateLimit(err, time.Now())
	if !ok {
		t.Fatalf("the refusal carried no time to come back at: %v", err)
	}
	if !at.Equal(reset.UTC()) {
		t.Fatalf("resume at %v, want the reset GitHub sent, %v", at, reset.UTC())
	}
}
