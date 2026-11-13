package controller

import (
	"strings"
	"testing"

	"github.com/eyupio/zoomies/internal/store"
)

// personalFleet is a fleet whose installation was recorded the way an operator
// with a personal account records it: their own login, as an account-wide
// target. GitHub knows it is a person; Zoomies has not asked yet.
func personalFleet(t *testing.T) (*harness, *store.Installation, *store.Pool) {
	t.Helper()
	h := newHarness(t)
	h.gh.SetAccountType("User")
	h.gh.SetPermissions(map[string]string{"administration": "write", "actions": "read", "metadata": "read"})
	h.gh.AddRepo("acme/widgets")
	inst, pool, _ := h.fleet()
	return h, inst, pool
}

func TestAProbeLearnsAPersonalAccountAndRecordsIt(t *testing.T) {
	// Before this the installation answered "list runners on acme: not found"
	// on every check, which reads like a missing permission and is not one: a
	// person has no runners of their own. The probe now asks GitHub what the
	// account is and the row keeps the answer, so an installation recorded
	// under the wrong guess starts working without anybody editing it.
	h, inst, _ := personalFleet(t)

	if _, err := h.c.ProbeInstallation(h.ctx, inst.ID); err != nil {
		t.Fatalf("ProbeInstallation: %v", err)
	}
	got, err := h.st.GetInstallation(h.ctx, inst.ID)
	if err != nil {
		t.Fatalf("GetInstallation: %v", err)
	}
	if got.AccountType != store.AccountUser || !got.Personal() {
		t.Fatalf("account type = %q, want %q", got.AccountType, store.AccountUser)
	}
	if got.LastError != "" {
		t.Fatalf("a working personal account was recorded unhealthy: %q", got.LastError)
	}
}

func TestAPersonalAccountsJobGetsARunnerOnItsOwnRepository(t *testing.T) {
	h, inst, pool := personalFleet(t)
	if _, err := h.c.ProbeInstallation(h.ctx, inst.ID); err != nil {
		t.Fatalf("ProbeInstallation: %v", err)
	}
	h.deliverJob(jobEvent{Action: "queued", JobID: 1, RunID: 1, Repo: "acme/gadgets", Labels: pool.Labels})

	if err := h.c.Reconcile(h.ctx); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	h.c.lifecycleCalls.Wait()

	r := h.onlyRunner()
	if r.Repo != "acme/gadgets" {
		t.Fatalf("the runner's row says it is on %q, want acme/gadgets", r.Repo)
	}
	if r.State == store.RunnerFailed {
		t.Fatalf("the runner failed to register: %s", r.Message)
	}
	registered := h.gh.Runners()
	if len(registered) != 1 || registered[0].Repo != "acme/gadgets" {
		t.Fatalf("GitHub has %+v, want one runner on acme/gadgets", registered)
	}
}

func TestAPersonalAccountKeepsNoWarmRunners(t *testing.T) {
	// There is no repository to register a warm runner on until a job names
	// one, and registering on a guess fails and counts against the pool.
	h, inst, pool := personalFleet(t)
	pool.MinRunners = 2
	if err := h.st.UpdatePool(h.ctx, pool); err != nil {
		t.Fatalf("UpdatePool: %v", err)
	}
	if _, err := h.c.ProbeInstallation(h.ctx, inst.ID); err != nil {
		t.Fatalf("ProbeInstallation: %v", err)
	}
	if err := h.c.Reconcile(h.ctx); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	h.c.lifecycleCalls.Wait()
	if got := h.runners(); len(got) != 0 {
		t.Fatalf("made %d runners with no job waiting", len(got))
	}
}

func TestAPersonalAccountsPersistentPoolSaysWhyItCannotRegister(t *testing.T) {
	h, inst, pool := personalFleet(t)
	pool.Ephemeral = false
	if _, err := h.c.ProbeInstallation(h.ctx, inst.ID); err != nil {
		t.Fatalf("ProbeInstallation: %v", err)
	}
	fresh, err := h.st.GetInstallation(h.ctx, inst.ID)
	if err != nil {
		t.Fatalf("GetInstallation: %v", err)
	}
	_, _, err = h.c.mintCredentials(h.ctx, fresh, pool, "persistent", "acme/widgets")
	if err == nil || !strings.Contains(err.Error(), "ephemeral") {
		t.Fatalf("mint error = %v, want one that names the ephemeral setting", err)
	}
}
