package controller

import (
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/eyupio/zoomies/internal/github"
	"github.com/eyupio/zoomies/internal/store"
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

// A full admission budget defers demand without losing it or allocating rows.
func TestRegistrationDeferredDemandResumesOnLaterPass(t *testing.T) {
	h := newHarness(t)
	inst, pool, _ := h.fleet()
	pool.MinRunners = 2
	if err := h.st.UpdatePool(h.ctx, pool); err != nil {
		t.Fatal(err)
	}
	if !h.c.admitCredentialMint(inst.ID) {
		t.Fatal("could not occupy admission")
	}
	if err := h.c.Reconcile(h.ctx); err != nil {
		t.Fatal(err)
	}
	if len(h.runners()) != 0 {
		t.Fatal("deferred demand allocated provisioning rows")
	}
	h.c.releaseCredentialMint(inst.ID)
	for range pool.MinRunners {
		if err := h.c.Reconcile(h.ctx); err != nil {
			t.Fatal(err)
		}
		h.c.lifecycleCalls.Wait()
	}
	if got := len(h.runners()); got != pool.MinRunners {
		t.Fatalf("created %d runners, want %d", got, pool.MinRunners)
	}
	h.c.githubMu.Lock()
	defer h.c.githubMu.Unlock()
	if len(h.c.credentialMints) != 0 {
		t.Fatal("completed lifecycle retained admission")
	}
}

func TestRegistrationAdmissionPreservesPoolFairness(t *testing.T) {
	h := newHarness(t)
	inst := h.installation()
	a, b := h.pool(inst, "a"), h.pool(inst, "b")
	h.host("host")
	for _, p := range []*store.Pool{a, b} {
		p.MinRunners = 2
		if err := h.st.UpdatePool(h.ctx, p); err != nil {
			t.Fatal(err)
		}
	}
	h.gh.SetDelay("", "generate-jitconfig", 500*time.Millisecond)
	for pass := 0; pass < 2; pass++ {
		if err := h.c.Reconcile(h.ctx); err != nil {
			t.Fatal(err)
		}
		h.c.lifecycleCalls.Wait()
	}
	counts := map[string]int{}
	for _, r := range h.runners() {
		counts[r.PoolID]++
	}
	if counts[a.ID] != 1 || counts[b.ID] != 1 {
		t.Fatalf("admission starved a pool: %v", counts)
	}
}

func TestCreateTaskCarriesProvisionDeadlineAndTokenExpiry(t *testing.T) {
	h := newHarness(t)
	_, pool, host := h.fleet()
	pool.Ephemeral = false
	pool.MinRunners = 1
	timeout := store.Duration(17 * time.Minute)
	pool.RunnerSettings.ProvisionTimeout = &timeout
	if err := h.st.UpdatePool(h.ctx, pool); err != nil {
		t.Fatal(err)
	}
	if err := h.c.Reconcile(h.ctx); err != nil {
		t.Fatal(err)
	}
	h.c.lifecycleCalls.Wait()
	tasks := h.tasksFor(host.ID)
	for _, task := range tasks {
		if task.Spec == nil || task.Spec.Credentials.RegistrationToken == "" {
			continue
		}
		runners := h.runners()
		if len(runners) != 1 {
			t.Fatalf("runners: %d", len(runners))
		}
		if !task.Spec.StartBefore.Truncate(time.Millisecond).Equal(runners[0].CreatedAt.Add(time.Duration(timeout))) {
			t.Fatalf("deadline %s, created %s, timeout %s", task.Spec.StartBefore, runners[0].CreatedAt, timeout)
		}
		if task.Spec.Credentials.ExpiresAt.IsZero() {
			t.Fatal("GitHub token expiry was discarded")
		}
		return
	}
	t.Fatal("no credential-bearing create task")
}
