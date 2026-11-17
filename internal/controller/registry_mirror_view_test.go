package controller

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/eyupio/zoomies/internal/backend"
	"github.com/eyupio/zoomies/internal/registrymirror"
)

// The host card says whether the host's registry mirror is on and what is wrong
// with it, in the controller's own sentences, from what the agent reports. It
// carries only what changes rarely: the figures move with every pull and are the
// metrics' business, as the cache's are.

func (h *harness) mirrorView(hostID string) *RegistryMirrorView {
	h.t.Helper()
	stored, err := h.st.GetHost(h.ctx, hostID)
	if err != nil {
		h.t.Fatal(err)
	}
	return h.c.HostView(stored).RegistryMirror
}

func TestAHostViewSaysWhatItsAgentSaysAboutItsMirror(t *testing.T) {
	h := newHarness(t)
	host := h.hostWith("builder")

	if got := h.mirrorView(host.ID); got != nil {
		t.Fatalf("a host that has said nothing about a mirror carries %+v, want none: nothing is on", got)
	}

	h.mirrorBeat(host.ID, &backend.RegistryMirrorReport{})
	got := h.mirrorView(host.ID)
	if got == nil {
		t.Fatal("a host whose mirror is on has no mirror in its view")
	}
	if got.Problem != "" || got.Detail != "" || got.Fix != "" || got.HostImageCustom {
		t.Fatalf("view = %+v, want a mirror that is on with nothing wrong", got)
	}

	h.mirrorBeat(host.ID, &backend.RegistryMirrorReport{Problem: backend.MirrorProblemListener, HostImageCustom: true})
	got = h.mirrorView(host.ID)
	sentence := mirrorProblemSentences[backend.MirrorProblemListener]
	if got == nil || got.Problem != backend.MirrorProblemListener || got.Detail != sentence.detail || got.Fix != sentence.fix || !got.HostImageCustom {
		t.Fatalf("view = %+v, want the listener problem in the controller's sentences, and the host's custom image", got)
	}

	// What one beat says is true of that beat.
	h.mirrorBeat(host.ID, nil)
	if got := h.mirrorView(host.ID); got != nil {
		t.Fatalf("a host that stopped reporting a mirror still carries %+v", got)
	}
}

// Every word an agent uses reaches the card as itself, and one this controller
// has never heard is not shown as it arrived: the view goes to every browser.
func TestAnAgentsWordIsNeverEchoedIntoTheView(t *testing.T) {
	h := newHarness(t)
	host := h.hostWith("builder")
	for _, word := range []string{
		backend.MirrorProblemContainer, backend.MirrorProblemSharedFolder, backend.MirrorProblemFolder,
		backend.MirrorProblemGateway, backend.MirrorProblemListener, backend.MirrorProblemUpstream,
	} {
		h.mirrorBeat(host.ID, &backend.RegistryMirrorReport{Problem: word})
		if got := h.mirrorView(host.ID); got == nil || got.Problem != word || got.Detail == "" || got.Fix == "" {
			t.Errorf("%s: view = %+v, want the word and both sentences", word, got)
		}
	}

	const hostile = "<img src=x onerror=alert(1)> `rm -rf /`"
	h.mirrorBeat(host.ID, &backend.RegistryMirrorReport{Problem: hostile})
	got := h.mirrorView(host.ID)
	if got == nil || got.Problem != "other" || got.Detail == "" || got.Fix == "" {
		t.Fatalf("view = %+v, want the general word and sentences for a problem this controller does not know", got)
	}
	raw, err := json.Marshal(got)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "onerror") || strings.Contains(string(raw), "rm -rf") {
		t.Fatalf("an agent's own words reached the view: %s", raw)
	}
}

// The figures move with every pull, so like the cache's they must never ride on
// the host's view or any event: the view is what the UI drops straight into its
// cache, and a frame per pull is the cost this was designed around.
func TestTheMirrorsFiguresAreNotPartOfTheHostView(t *testing.T) {
	h := newHarness(t)
	host := h.hostWith("builder")
	h.mirrorBeat(host.ID, &backend.RegistryMirrorReport{Stats: registrymirror.Stats{
		Requests: 123456, Hits: 234567, Misses: 345678, UpstreamErrors: 456789, UpstreamFailingSeconds: 567891,
		Evictions: 678912, BytesServed: 789123, BytesFetched: 891234, BytesStored: 912345,
	}})
	stored, err := h.st.GetHost(h.ctx, host.ID)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(h.c.HostView(stored))
	if err != nil {
		t.Fatal(err)
	}
	for _, leaked := range []string{"123456", "234567", "345678", "456789", "567891", "678912", "789123", "891234", "912345", "requests", "bytes_served", "upstream_errors"} {
		if strings.Contains(string(raw), leaked) {
			t.Errorf("the host view carries %q: %s", leaked, raw)
		}
	}
}

// The derived pass publishes a host when its rendered view changes, so what the
// mirror contributes must be the same bytes however much the figures move, and
// different when the state does.
func TestTheMirrorsPartOfTheViewMovesWithItsStateAndNotItsFigures(t *testing.T) {
	h := newHarness(t)
	host := h.hostWith("builder")
	fragment := func() string {
		raw, err := json.Marshal(h.mirrorView(host.ID))
		if err != nil {
			t.Fatal(err)
		}
		return string(raw)
	}

	h.mirrorBeat(host.ID, &backend.RegistryMirrorReport{Stats: registrymirror.Stats{Requests: 1, BytesStored: 10}})
	before := fragment()
	h.mirrorBeat(host.ID, &backend.RegistryMirrorReport{Stats: registrymirror.Stats{Requests: 9000, Hits: 8000, BytesStored: 999999, UpstreamFailingSeconds: 3}})
	if after := fragment(); after != before {
		t.Fatalf("the figures moved the view: %s then %s", before, after)
	}
	h.mirrorBeat(host.ID, &backend.RegistryMirrorReport{Problem: backend.MirrorProblemFolder})
	if after := fragment(); after == before {
		t.Fatal("a problem did not move the view")
	}
	h.mirrorBeat(host.ID, &backend.RegistryMirrorReport{HostImageCustom: true})
	if after := fragment(); after == before {
		t.Fatal("the host's image becoming custom did not move the view")
	}
}
