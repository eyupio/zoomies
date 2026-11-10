package controller

import (
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/eyupio/zoomies/internal/agent"
	"github.com/eyupio/zoomies/internal/config"
)

// channelStub answers the request for the dev release with notes that can be
// changed between two reads, and counts how many reads were made.
type channelStub struct {
	status int
	body   string
	asked  int
}

func (h *harness) stubChannel(notes string) *channelStub {
	h.t.Helper()
	s := &channelStub{status: http.StatusOK}
	s.say(notes)
	h.c.httpClient = &http.Client{Transport: roundTripFunc(func(r *http.Request) *http.Response {
		if r.URL.String() != devReleaseURL {
			return jsonResponse(http.StatusNotFound, `{}`)
		}
		s.asked++
		return jsonResponse(s.status, s.body)
	})}
	return s
}

func (s *channelStub) say(notes string) { s.body = fmt.Sprintf(`{"body":%q,"draft":false}`, notes) }

// A host downloads whatever the dev channel holds, so the build it holds is read
// from the notes CI writes once the last file is up, and from nothing else.
func TestTheDevChannelIsReadFromTheNotesCIWrites(t *testing.T) {
	h := newHarness(t)
	withVersion(t, "main-sha-d49e8bf4")
	stub := h.stubChannel("Rolling development build.\n\nBuild: main-sha-315d2dac\n")

	h.c.checkForDevChannel(h.ctx)
	if got := h.c.devChannelBuild(); got != "main-sha-315d2dac" {
		t.Fatalf("the channel holds %q, want main-sha-315d2dac", got)
	}

	// Notes that name no build leave it not known, which holds nothing back.
	stub.say("Rolling development build.")
	h.c.checkForDevChannel(h.ctx)
	if got := h.c.devChannelBuild(); got != "" {
		t.Fatalf("notes naming no build gave %q, want none", got)
	}

	// A draft is not published, whatever its notes say.
	stub.say("Build: main-sha-315d2dac")
	stub.body = `{"body":"Build: main-sha-315d2dac","draft":true}`
	h.c.checkForDevChannel(h.ctx)
	if got := h.c.devChannelBuild(); got != "" {
		t.Fatalf("a draft gave %q, want none", got)
	}

	// A read that fails keeps what the last one found: a controller that cannot
	// reach GitHub for a minute is not one whose channel was emptied.
	stub.say("Build: main-sha-315d2dac")
	h.c.checkForDevChannel(h.ctx)
	stub.status = http.StatusBadGateway
	h.c.checkForDevChannel(h.ctx)
	if got := h.c.devChannelBuild(); got != "main-sha-315d2dac" {
		t.Fatalf("after a failed read the channel holds %q, want the earlier answer", got)
	}
}

// The unauthenticated limit is shared with everything else on the controller's
// address, so the channel is read again only while it has not yet been seen
// holding this build, and not more than once in five minutes.
func TestTheDevChannelIsReadAgainOnlyUntilItHoldsTheControllersBuild(t *testing.T) {
	h := newHarness(t)
	withVersion(t, "main-sha-d49e8bf4")
	h.inMode("auto")
	stub := h.stubChannel("Build: main-sha-315d2dac")

	if !h.c.devChannelDue() {
		t.Fatal("a channel never read is not due")
	}
	h.c.checkForDevChannel(h.ctx)
	if h.c.devChannelDue() {
		t.Fatal("the channel is due again at once, so every pass would read it")
	}
	h.advance(6 * time.Minute)
	if !h.c.devChannelDue() {
		t.Fatal("the channel does not hold this build and five minutes have passed, but it is not due")
	}

	stub.say("Build: main-sha-d49e8bf4")
	h.c.checkForDevChannel(h.ctx)
	h.advance(time.Hour)
	if h.c.devChannelDue() {
		t.Fatal("the channel holds this build and is still read again")
	}
	if stub.asked != 2 {
		t.Fatalf("the channel was read %d times, want 2", stub.asked)
	}

	t.Run("a release controller has no channel to wait for", func(t *testing.T) {
		withVersion(t, "1.3.5")
		h.c.channel = nil
		if h.c.devChannelDue() {
			t.Fatal("a controller on a release reads the dev channel")
		}
	})
	t.Run("with updating off nothing is asked", func(t *testing.T) {
		withVersion(t, "main-sha-d49e8bf4")
		h.c.channel = nil
		h.c.live.Update(func(c *config.Config) { c.Updates.Mode = "off" })
		if h.c.devChannelDue() {
			t.Fatal("a controller with updating off reads the dev channel")
		}
	})
}

// The failure this stops: the controller moved to a build CI had not yet
// published, every host was asked at once to take "dev", each restarted onto the
// old binary, and each failure counted against it. Now the host waits, says what
// for, and is asked in the pass that sees the build named.
func TestAutoWaitsForTheDevChannelBeforeAskingAHostToUpdate(t *testing.T) {
	h := newHarness(t)
	withVersion(t, "main-sha-d49e8bf4")
	h.inMode("auto")
	h.installHelper()
	// The status speaks only once a release list has been read, empty or not.
	h.readTheList()
	host := h.agentHost("vm-a", "main-sha-315d2dac", agent.FeatureSelfUpdate)
	stub := h.stubChannel("Build: main-sha-315d2dac")
	// Reading the list asked the channel as well, against the stub that answered
	// the list. Start from a controller that has not asked yet.
	h.c.mu.Lock()
	h.c.channel, h.c.channelAsked = nil, time.Time{}
	h.c.mu.Unlock()

	h.pass(h.c)
	if r := h.openRollout(); r != nil {
		t.Fatalf("a rollout started to %s while the channel held another build", r.Target)
	}
	h.noAttemptFor(host)
	if reason := h.status().Reason; !hasAll(reason, "main-sha-315d2dac", "main-sha-d49e8bf4", "once the channel has it") {
		t.Fatalf("the status says %q, want what the host is waiting for", reason)
	}

	// Passes come every ten seconds; the channel is not read on each of them.
	h.pass(h.c)
	h.pass(h.c)
	if stub.asked != 1 {
		t.Fatalf("the channel was read %d times in three passes, want 1", stub.asked)
	}

	stub.say("Build: main-sha-d49e8bf4")
	h.c.mu.Lock()
	h.c.channelAsked = time.Time{}
	h.c.mu.Unlock()
	h.pass(h.c)
	r := h.openRollout()
	if r == nil || r.Target != "main-sha-d49e8bf4" {
		t.Fatalf("rollout = %+v after the channel named the build, want one to main-sha-d49e8bf4", r)
	}
	h.pass(h.c)
	if a := h.openHostAttempt(host); a.ToVersion != "main-sha-d49e8bf4" {
		t.Fatalf("the host's attempt = %+v, want one to main-sha-d49e8bf4", a)
	}
	h.pass(h.c)
	if stub.asked != 2 {
		t.Fatalf("the channel was read %d times, want 2: once it names the build nothing more is asked", stub.asked)
	}
}

func hasAll(s string, parts ...string) bool {
	for _, p := range parts {
		if !strings.Contains(s, p) {
			return false
		}
	}
	return true
}
