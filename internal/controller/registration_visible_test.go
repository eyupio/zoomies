package controller

import (
	"strings"
	"testing"
	"time"
)

// scheduler.registration_concurrency is the one limit in this fleet that used
// to bind without saying so. The pool reports jobs waiting and nowhere to run
// them, an operator reads that, looks at the idle hosts and adds more -- and
// the new hosts do not help, because hosts were never what ran out.
func TestAHeldBackRegistrationSaysSoRatherThanJustNotHappening(t *testing.T) {
	h := newHarness(t)
	inst := h.installation()

	// One in flight, which is the default limit, so the next is held.
	if !h.c.admitCredentialMint(inst.ID) {
		t.Fatal("the first mint was refused, so nothing below is testing the limit")
	}
	if h.c.admitCredentialMint(inst.ID) {
		t.Fatal("a second mint was admitted at a limit of one")
	}

	problems, err := h.c.Problems(h.ctx)
	if err != nil {
		t.Fatalf("Problems: %v", err)
	}
	var found *Problem
	for i := range problems {
		if problems[i].Code == "scheduler.registration_throttled" {
			found = &problems[i]
		}
	}
	if found == nil {
		var codes []string
		for _, p := range problems {
			codes = append(codes, p.Code)
		}
		t.Fatalf("nothing said the registration was held back; the list was %s", strings.Join(codes, ", "))
	}

	// The entry has to name the setting, or an operator reading it still does
	// not know which number to change.
	if found.Setting != "scheduler.registration_concurrency" {
		t.Errorf("the entry names setting %q", found.Setting)
	}
	if found.TargetID != inst.ID {
		t.Errorf("the entry targets %q, not the installation it is about", found.TargetID)
	}
	// And it has to say the thing that stops somebody buying hosts.
	if !strings.Contains(found.Detail, "hosts are not what ran out") {
		t.Errorf("the entry does not say adding hosts will not help: %q", found.Detail)
	}
	if found.Since == nil {
		t.Error("the entry does not say since when, so nobody can tell a blip from a standing condition")
	}
}

// A throttle that is no longer binding must stop being reported, or it becomes
// one more line an operator learns to scroll past.
func TestTheHeldBackRegistrationClearsWhenItStops(t *testing.T) {
	h := newHarness(t)
	inst := h.installation()

	if !h.c.admitCredentialMint(inst.ID) {
		t.Fatal("the first mint was refused")
	}
	if h.c.admitCredentialMint(inst.ID) {
		t.Fatal("a second mint was admitted at a limit of one")
	}
	h.c.releaseCredentialMint(inst.ID)

	// A pass that defers nothing forgets it.
	h.c.resetDeferredMints()
	h.c.resetDeferredMints()

	problems, err := h.c.Problems(h.ctx)
	if err != nil {
		t.Fatalf("Problems: %v", err)
	}
	for _, p := range problems {
		if p.Code == "scheduler.registration_throttled" {
			t.Errorf("the fleet is no longer holding registrations back and still says it is: %q", p.Title)
		}
	}
}

// GitHub's own rate-limit hold is a different thing with a different fix, and
// counting it here would send an operator to raise a limit that is not the one
// refusing them.
func TestAGitHubRateLimitHoldIsNotCountedAsOurThrottle(t *testing.T) {
	h := newHarness(t)
	inst := h.installation()

	h.c.holdGitHub(inst.ID, h.c.Now().Add(time.Hour))
	if h.c.admitCredentialMint(inst.ID) {
		t.Fatal("a mint was admitted while the installation was held by GitHub")
	}

	held, _ := h.c.deferredMintsNow()
	if n := held[inst.ID]; n != 0 {
		t.Errorf("a GitHub rate-limit hold was counted as %d of our own deferrals", n)
	}
}
