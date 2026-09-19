package controller

import (
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/eyupio/zoomies/internal/github"
)

func TestRegistrationAdmissionIsBoundedAndInstallationScoped(t *testing.T) {
	h := newHarness(t)
	if !h.c.admitCredentialMint("one") || h.c.admitCredentialMint("one") {
		t.Fatal("default must admit exactly one mint")
	}
	if !h.c.admitCredentialMint("two") {
		t.Fatal("unrelated installation was blocked")
	}
	h.c.releaseCredentialMint("one")
	h.c.holdGitHub("one", h.c.Now().Add(time.Minute))
	if h.c.admitCredentialMint("one") {
		t.Fatal("held installation admitted")
	}
	h.advance(2 * time.Minute)
	if !h.c.admitCredentialMint("one") {
		t.Fatal("expired hold did not recover")
	}
	h.c.releaseCredentialMint("one")
	h.c.releaseCredentialMint("two")
	if len(h.c.credentialMints) != 0 {
		t.Fatal("admission bookkeeping leaked")
	}
}

func TestRegistrationRateLimitCreatesSharedHold(t *testing.T) {
	for _, ephemeral := range []bool{true, false} {
		h := newHarness(t)
		inst, pool, _ := h.fleet()
		pool.Ephemeral = ephemeral
		reset := h.c.Now().Add(10 * time.Minute)
		h.gh.SetRateLimit(5000, 0, reset)
		pattern := "registration-token"
		if ephemeral {
			pattern = "generate-jitconfig"
		}
		h.gh.SetError(pattern, http.StatusForbidden, "rate limit exceeded")
		_, _, err := h.c.mintCredentials(h.ctx, inst, pool, "limited")
		if !errors.Is(err, github.ErrRateLimited) {
			t.Fatalf("mint error: %v", err)
		}
		if !h.c.githubHeld(inst.ID, h.c.Now()) {
			t.Fatal("registration did not hold installation")
		}
		before := h.c.heldInstallations(h.c.Now())[inst.ID]
		if before.Before(reset.Truncate(time.Second)) {
			t.Fatal("hold ignored GitHub reset")
		}
		h.gh.ClearErrors()
		_, _, err = h.c.mintCredentials(h.ctx, inst, pool, "still-held")
		if !errors.Is(err, github.ErrRateLimited) {
			t.Fatalf("held mint error: %v", err)
		}
		if after := h.c.heldInstallations(h.c.Now())[inst.ID]; !after.Equal(before) {
			t.Fatal("skipped call extended hold")
		}
	}
}

func TestRegistrationGroupLookupRateLimitPreventsMint(t *testing.T) {
	h := newHarness(t)
	inst, pool, _ := h.fleet()
	h.gh.SetRateLimit(5000, 0, h.c.Now().Add(time.Minute))
	h.gh.SetError("runner-groups", http.StatusForbidden, "rate limit exceeded")
	_, _, err := h.c.mintCredentials(h.ctx, inst, pool, "group-limited")
	if !errors.Is(err, github.ErrRateLimited) || !h.c.githubHeld(inst.ID, h.c.Now()) {
		t.Fatalf("group quota not held: %v", err)
	}
	if len(h.gh.Runners()) != 0 {
		t.Fatal("minted after group lookup rate limit")
	}
}
