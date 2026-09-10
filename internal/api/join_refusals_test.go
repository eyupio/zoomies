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

// The fourth way, which used to be the one that said nothing.
//
// A host of this name is already enrolled and the joiner cannot prove it owns
// it. The controller's refusal is the most useful sentence on this route -- it
// names the host id and both ways forward -- but it was a plain error, so the
// handler sent it through s.internal and the agent got HTTP 500 and "the cause
// is in the controller's log; quote the request ID when reporting it". The one
// message that says what to do was replaced, at the moment it was needed, by
// one that says nothing.
//
// It reaches an operator, too: this is what a rebuilt machine gets when its
// credentials are gone, and what any second machine gets when somebody names it
// after one that already exists.
func TestAgentJoinRefusesAnAlreadyEnrolledHostWithAdviceRatherThanA500(t *testing.T) {
	h := newHarness(t)
	existing := h.host("vm-1")

	_, token, err := h.ctrl.Auth().CreateJoinToken(h.ctx, time.Hour, nil, 2, "test")
	if err != nil {
		t.Fatalf("CreateJoinToken: %v", err)
	}
	resp := h.do(request{method: http.MethodPost, path: "/api/v1/agent/join", body: map[string]any{
		"protocol_version": 1, "join_token": token, "name": existing.Name,
		"capacity": 2, "os": "linux", "arch": "amd64", "version": "test",
	}})

	resp.mustStatus(t, http.StatusUnprocessableEntity, "a name that is already enrolled")
	msg := resp.errorMessage(t)
	for _, want := range []string{"already enrolled", existing.ID, "hosts delete"} {
		if !strings.Contains(msg, want) {
			t.Errorf("the refusal should carry %q so the operator knows what to do: %q", want, msg)
		}
	}
	if strings.Contains(msg, "controller's log") {
		t.Errorf("the refusal was replaced by the generic internal-error text: %q", msg)
	}
}
