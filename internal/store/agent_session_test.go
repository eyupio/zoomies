package store

import (
	"testing"
)

func hostForSessions(t *testing.T, s *Store) *Host {
	t.Helper()
	h := &Host{Name: "host-a", Capacity: 2}
	if err := s.CreateHost(t.Context(), h); err != nil {
		t.Fatalf("CreateHost: %v", err)
	}
	return h
}

// Restarting is the ordinary thing an agent does, and it must never look like
// a duplicate. The agent's unit restarts always, so a controller that flagged
// every session change would flag every host that had ever been upgraded.
func TestAnAgentRestartingIsNotADuplicate(t *testing.T) {
	s := newTestStore(t)
	h := hostForSessions(t, s)

	for _, session := range []string{"ses_a", "ses_a", "ses_b", "ses_b", "ses_c"} {
		alternated, err := s.RecordAgentSession(t.Context(), h.ID, session)
		if err != nil {
			t.Fatalf("RecordAgentSession(%s): %v", session, err)
		}
		if alternated {
			t.Fatalf("session %s was called an alternation; an agent only ever moves forward", session)
		}
	}
	got, err := s.GetHost(t.Context(), h.ID)
	if err != nil {
		t.Fatalf("GetHost: %v", err)
	}
	if got.AgentSessionAlternations != 0 || got.AgentSessionAltAt != nil {
		t.Fatalf("alternations = %d at %v, want none recorded", got.AgentSessionAlternations, got.AgentSessionAltAt)
	}
}

// The signal itself. A session id is minted at start-up and never reused, so
// going back to one this host has already left behind is something a single
// agent cannot do however often it restarts: it means two of them hold this
// host's token and are splitting its tasks.
func TestTwoAgentsSharingAHostAreSeenAlternating(t *testing.T) {
	s := newTestStore(t)
	h := hostForSessions(t, s)
	ctx := t.Context()

	if alt, _ := s.RecordAgentSession(ctx, h.ID, "ses_one"); alt {
		t.Fatal("the first session cannot be an alternation")
	}
	if alt, _ := s.RecordAgentSession(ctx, h.ID, "ses_two"); alt {
		t.Fatal("the second session on its own looks exactly like a restart")
	}
	// Back to the first: this is the moment a restart cannot explain.
	alt, err := s.RecordAgentSession(ctx, h.ID, "ses_one")
	if err != nil {
		t.Fatalf("RecordAgentSession: %v", err)
	}
	if !alt {
		t.Fatal("going back to a session the host had left behind was not reported")
	}

	// And it keeps being reported as they carry on swapping.
	if alt, _ := s.RecordAgentSession(ctx, h.ID, "ses_two"); !alt {
		t.Fatal("the swap back was not reported")
	}
	got, err := s.GetHost(ctx, h.ID)
	if err != nil {
		t.Fatalf("GetHost: %v", err)
	}
	if got.AgentSessionAlternations != 2 {
		t.Fatalf("alternations = %d, want 2", got.AgentSessionAlternations)
	}
	if got.AgentSessionAltAt == nil {
		t.Fatal("no time recorded for the last alternation; the problem could never age out")
	}
}

// An agent that predates the header sends nothing, and a controller must not
// read that as a host whose session has changed.
func TestAnAgentWithNoSessionIsNotRecorded(t *testing.T) {
	s := newTestStore(t)
	h := hostForSessions(t, s)
	ctx := t.Context()

	if _, err := s.RecordAgentSession(ctx, h.ID, "ses_one"); err != nil {
		t.Fatalf("RecordAgentSession: %v", err)
	}
	alt, err := s.RecordAgentSession(ctx, h.ID, "")
	if err != nil {
		t.Fatalf("RecordAgentSession(empty): %v", err)
	}
	if alt {
		t.Fatal("an empty session was treated as an alternation")
	}
	got, _ := s.GetHost(ctx, h.ID)
	if got.AgentSessionID != "ses_one" {
		t.Fatalf("agent_session_id = %q, want the real session left in place", got.AgentSessionID)
	}
}
