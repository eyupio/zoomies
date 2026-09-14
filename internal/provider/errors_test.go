package provider

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"
)

// Every category has to survive the trip through errors.Is, because that is how
// the reconciler asks its questions. A kind from a newer contract lands on
// internal, which retries nothing and deletes nothing -- the safe direction, and
// the one the contract promises.
func TestEveryFailureIsClassifiedAndTheUnrecognisedOnesAreInternal(t *testing.T) {
	for _, tc := range []struct {
		name     string
		err      error
		want     FailureKind
		sentinel error
	}{
		{"a configuration mistake", &Error{Kind: FailureConfig}, FailureConfig, ErrConfig},
		{"a rejected credential", &Error{Kind: FailureAuth}, FailureAuth, ErrAuth},
		{"a credential without the privilege", &Error{Kind: FailurePermission}, FailurePermission, ErrPermission},
		{"a provider we could not reach", &Error{Kind: FailureUnreachable}, FailureUnreachable, ErrUnreachable},
		{"no capacity", &Error{Kind: FailureQuota}, FailureQuota, ErrQuota},
		{"something else holding it", &Error{Kind: FailureConflict}, FailureConflict, ErrConflict},
		{"a resource that is not there", &Error{Kind: FailureNotFound}, FailureNotFound, ErrNotFound},
		{"a refusal in the provider's own words", &Error{Kind: FailureRefused}, FailureRefused, ErrRefused},
		{"an outcome nobody heard", &Error{Kind: FailureAmbiguous}, FailureAmbiguous, ErrAmbiguous},
		{"something we could not classify", &Error{Kind: FailureInternal}, FailureInternal, nil},
		{"a category from a later contract", &Error{Kind: "hibernated"}, FailureInternal, nil},
		{"a bare error from a provider that said nothing", errors.New("boom"), FailureInternal, nil},
		{"a bare sentinel", ErrNotFound, FailureNotFound, ErrNotFound},
		{"a wrapped provider error", fmt.Errorf("while creating: %w", &Error{Kind: FailureQuota}), FailureQuota, ErrQuota},
		{"a cancelled context, which is the caller's own doing", context.Canceled, FailureInternal, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := KindOf(tc.err); got != tc.want {
				t.Errorf("KindOf = %q, want %q", got, tc.want)
			}
			if tc.sentinel != nil && !errors.Is(tc.err, tc.sentinel) {
				t.Errorf("errors.Is against %v is false; a caller asking the cheap question would miss it", tc.sentinel)
			}
		})
	}
	if got := KindOf(nil); got != "" {
		t.Errorf("KindOf(nil) = %q; a call that worked has no failure category", got)
	}
}

// Retryable is the whole of the reconciler's "try again?" decision, and the
// ambiguous row is the one that costs money: an operation whose outcome is
// unknown must be resolved by looking, never by repeating, because repeating a
// create that already worked buys a second machine nobody is tracking.
func TestAnAmbiguousOutcomeIsNeverRetriedAndNeitherIsAnythingAPersonMustFix(t *testing.T) {
	for _, tc := range []struct {
		kind FailureKind
		want bool
	}{
		{FailureConfig, false},
		{FailureAuth, false},
		{FailurePermission, false},
		{FailureAmbiguous, false},
		{FailureInternal, false},
		{FailureUnreachable, true},
		{FailureQuota, true},
		{FailureConflict, true},
		{FailureNotFound, true},
		{FailureRefused, true},
	} {
		t.Run(string(tc.kind), func(t *testing.T) {
			if got := Retryable(&Error{Kind: tc.kind}); got != tc.want {
				t.Errorf("Retryable(%s) = %t, want %t", tc.kind, got, tc.want)
			}
		})
	}
	if Retryable(nil) {
		t.Error("Retryable(nil) is true; a call that worked would be made again")
	}
	if Retryable(errors.New("boom")) {
		t.Error("an unclassified failure is retryable; we would repeat an operation we know nothing about")
	}
}

// A retry time already in the past is no answer -- clocks drift and responses
// queue -- so the caller keeps its own backoff rather than resuming straight
// into the same refusal.
func TestARetryTimeAlreadyInThePastIsNoAnswer(t *testing.T) {
	now := time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)
	for _, tc := range []struct {
		name string
		err  error
		want time.Duration
		ok   bool
	}{
		{"the provider asked for a minute", &Error{Kind: FailureQuota, RetryAfter: time.Minute}, time.Minute, true},
		{"the provider said nothing", &Error{Kind: FailureQuota}, 0, false},
		{"a duration that has already elapsed", &Error{Kind: FailureQuota, RetryAfter: -time.Minute}, 0, false},
		{"not a provider failure at all", errors.New("boom"), 0, false},
		{"wrapped, because callers wrap", fmt.Errorf("creating: %w", &Error{Kind: FailureQuota, RetryAfter: time.Hour}), time.Hour, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			at, ok := RetryAfter(tc.err, now)
			if ok != tc.ok {
				t.Fatalf("RetryAfter said %t, want %t", ok, tc.ok)
			}
			if ok && !at.Equal(now.Add(tc.want)) {
				t.Errorf("RetryAfter = %s, want %s", at, now.Add(tc.want))
			}
		})
	}
}

// The message is read by an operator on a page, so it has to say what was being
// done, to what, in whose words, and what to change. A status code on its own
// sends somebody to the wrong half of the system.
func TestAFailureSaysWhatWasBeingDoneAndWhatToChange(t *testing.T) {
	err := &Error{
		Kind:    FailurePermission,
		Op:      "clone the template",
		Ref:     "zoomies-mach-abc",
		Message: "the credential may not write to this storage",
		Remedy:  "Grant the credential space on the storage the provider is configured with.",
	}
	got := err.Error()
	for _, want := range []string{"clone the template", "zoomies-mach-abc", "not allowed", "may not write", "Fix: Grant"} {
		if !strings.Contains(got, want) {
			t.Errorf("the message %q does not contain %q", got, want)
		}
	}

	// A failure with a cause and no words of its own still says something.
	cause := &Error{Kind: FailureUnreachable, Cause: context.DeadlineExceeded}
	if !strings.Contains(cause.Error(), context.DeadlineExceeded.Error()) {
		t.Errorf("a failure with only a cause says %q", cause.Error())
	}

	// The category is the Kind field and nothing else. An error carrying a
	// cause that happens to be another category must not be reclassified by it:
	// only the transport knows which side of the request the failure fell on.
	mixed := &Error{Kind: FailureUnreachable, Cause: ErrAmbiguous}
	if got := KindOf(mixed); got != FailureUnreachable {
		t.Errorf("KindOf = %q; the cause reclassified the failure", got)
	}
	if errors.Is(mixed, ErrAmbiguous) {
		t.Error("a failure classified as unreachable also matches ambiguous; a caller would resolve it by looking at a resource nothing ever touched")
	}
}
