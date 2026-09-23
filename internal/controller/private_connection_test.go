package controller

import (
	"testing"
	"time"
)

// A relay that stays down is retried every twenty seconds, and each retry
// reports again. "Since" has to stay at the first failure, or the entry would
// say the private hosts were cut off moments ago however long it had been.
func TestAPrivateConnectionFaultKeepsWhenItStartedAcrossRetries(t *testing.T) {
	h := newHarness(t)
	first := time.Date(2026, 9, 1, 10, 0, 0, 0, time.UTC)
	h.c.SetPrivateConnectionFault(&PrivateConnectionFault{Since: first, Reason: "relay did not answer"})
	h.c.SetPrivateConnectionFault(&PrivateConnectionFault{Since: first.Add(time.Minute), Reason: "still no answer"})

	var got *Problem
	problems, err := h.c.Problems(h.ctx)
	if err != nil {
		t.Fatal(err)
	}
	for i := range problems {
		if problems[i].Code == "tailcat.unavailable" {
			got = &problems[i]
		}
	}
	if got == nil {
		t.Fatal("a recorded private-connection fault raised no tailcat.unavailable")
	}
	if got.Since == nil || !got.Since.Equal(first) {
		t.Errorf("since = %v, want the first failure %v", got.Since, first)
	}
	if got.Audience != AudiencePlatform {
		t.Errorf("audience = %q; the listener is the process's, so the platform's", got.Audience)
	}

	h.c.SetPrivateConnectionFault(nil)
	problems, _ = h.c.Problems(h.ctx)
	for _, p := range problems {
		if p.Code == "tailcat.unavailable" {
			t.Fatal("tailcat.unavailable stayed after the relay answered")
		}
	}
}
