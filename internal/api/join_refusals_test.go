package api

import (
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/eyupio/zoomies/internal/auth"
	"github.com/eyupio/zoomies/internal/cryptox"
	"github.com/eyupio/zoomies/internal/store"
)

// The three ways a join is refused, at the route an agent actually calls.
//
// Each has a different remedy -- mint a fresh token, mint a fresh token, check
// what you pasted -- and the installer picks the sentence from the sentinel it
// gets back. Until this, only the spent case was asserted anywhere, and the
// expired one was the case an operator most often meets: a token minted with
// the default fifteen minutes, pasted into a machine they then went to make
// coffee about.
func TestAgentJoinRefusesGarbageExpiredAndSpentTokens(t *testing.T) {
	h := newHarness(t)
	body := func(token, name string) map[string]any {
		return map[string]any{
			"protocol_version": 1, "join_token": token, "name": name,
			"capacity": 2, "os": "linux", "arch": "amd64", "version": "test",
		}
	}

	// Something that is not a token at all: a truncated paste, or the token id
	// from the Hosts page rather than the secret shown once beside it.
	garbage := h.do(request{method: http.MethodPost, path: "/api/v1/agent/join", body: body("zoojoin_notatoken", "vm-garbage")})
	garbage.mustStatus(t, http.StatusUnprocessableEntity, "a token that never existed")
	if msg := garbage.errorMessage(t); !strings.Contains(msg, "join token") {
		t.Errorf("the refusal does not name what was wrong: %q", msg)
	}

	// A token whose time ran out. The row is written directly with a past
	// expiry, because `CreateJoinToken` reads a non-positive TTL as "use the
	// default" -- asking it for a negative one mints a perfectly good token and
	// would have made this test pass against a controller that never checked.
	expired := auth.JoinTokenPrefix + "expired_" + store.NewSecret(32)
	if err := h.st.CreateJoinToken(h.ctx, &store.JoinToken{
		ID:        store.NewID(store.PrefixJoin),
		TokenHash: cryptox.HashToken(expired),
		Prefix:    auth.JoinTokenPrefix + "expired",
		CreatedBy: "test",
		Capacity:  2,
		ExpiresAt: time.Now().Add(-time.Minute),
	}); err != nil {
		t.Fatalf("CreateJoinToken: %v", err)
	}
	stale := h.do(request{method: http.MethodPost, path: "/api/v1/agent/join", body: body(expired, "vm-expired")})
	stale.mustStatus(t, http.StatusUnprocessableEntity, "an expired token")
	if msg := stale.errorMessage(t); !strings.Contains(msg, "expired") {
		t.Errorf("the refusal does not say the token's time ran out: %q", msg)
	}

	// And a spent one: single use is the whole point, so a second machine
	// pasting the same line is refused rather than quietly enrolled.
	_, once, err := h.ctrl.Auth().CreateJoinToken(h.ctx, time.Hour, nil, 2, "test")
	if err != nil {
		t.Fatalf("CreateJoinToken: %v", err)
	}
	first := h.do(request{method: http.MethodPost, path: "/api/v1/agent/join", body: body(once, "vm-first")})
	first.mustStatus(t, http.StatusOK, "the first join")
	second := h.do(request{method: http.MethodPost, path: "/api/v1/agent/join", body: body(once, "vm-second")})
	second.mustStatus(t, http.StatusUnprocessableEntity, "the same token twice")

	// All three are the same status, which is why the agent's transport turns
	// a 422 on this route into its unauthorised sentinel: the route is
	// anonymous, so there is no credential to be unauthorised, and what was
	// rejected is the token in the body. Without that mapping the installer's
	// "expired, or already used" remedy never fired for any of these.
	if garbage.status != stale.status || stale.status != second.status {
		t.Fatalf("the three refusals answer %d, %d and %d; the installer keys its remedy on one of them",
			garbage.status, stale.status, second.status)
	}
}
