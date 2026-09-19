package controller

import (
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/eyupio/zoomies/internal/store"
)

func TestCleanupBusyObservationRefreshesLegacyErrorWithoutDeletion(t *testing.T) {
	h := newHarness(t)
	_, pool, host := h.fleet()
	r := h.runnerRow(pool, host, store.RunnerRemoved)
	h.gh.AddRunner(r.Name, pool.Labels)
	h.gh.SetRunnerBusy(r.Name, true)
	if err := h.st.RecordRegistrationCleanupFailure(h.ctx, r.ID, "the GitHub runner registration could not be deleted: Bad request - Runner is currently running a job and cannot be deleted"); err != nil {
		t.Fatal(err)
	}
	first := h.runnerByID(t, r.ID)
	// Existing, persisted messages must get correct advice before the next reap.
	if fix := h.problem(t, "runners.cleanup_failed").Fix; strings.Contains(fix, "permission the App has lost") {
		t.Fatal(fix)
	}
	h.advance(15 * time.Hour)
	h.c.reap(h.ctx)
	got := h.runnerByID(t, r.ID)
	if !strings.Contains(got.CleanupError, "cleanup is deferred") || got.CleanupAttempts != first.CleanupAttempts || !got.CleanupFailedAt.Equal(*first.CleanupFailedAt) {
		t.Fatalf("busy observation: %+v", got)
	}
	for _, req := range h.gh.Requests() {
		if strings.HasPrefix(req, "DELETE ") {
			t.Fatalf("deleted busy registration: %s", req)
		}
	}
	if strings.Contains(h.problem(t, "runners.cleanup_failed").Detail, "will not go away on its own") {
		t.Fatal("automatic recovery described as impossible")
	}
	h.gh.SetRunnerBusy(r.Name, false)
	h.c.reap(h.ctx)
	got = h.runnerByID(t, r.ID)
	if got.RegistrationDeletedAt == nil || got.CleanupError != "" || len(h.gh.Runners()) != 0 {
		t.Fatalf("idle registration not recovered: %+v", got)
	}
	if got.HostRemovedAt != nil || got.CleanedUpAt != nil {
		t.Fatal("GitHub cleanup fabricated host removal")
	}
}

func TestCleanupMissingRegistrationSettlesOnlyGitHubSide(t *testing.T) {
	for _, hostFailed := range []bool{false, true} {
		h := newHarness(t)
		_, pool, host := h.fleet()
		r := h.runnerRow(pool, host, store.RunnerRemoved)
		if err := h.st.RecordRegistrationCleanupFailure(h.ctx, r.ID, "GitHub still reports the runner as running a job"); err != nil {
			t.Fatal(err)
		}
		if hostFailed {
			if err := h.st.RecordCleanupFailure(h.ctx, r.ID, "host cleanup is unavailable"); err != nil {
				t.Fatal(err)
			}
		} else {
			if err := h.c.confirmCleanup(h.ctx, r.ID, true); err != nil {
				t.Fatal(err)
			}
		}
		h.c.reap(h.ctx)
		got := h.runnerByID(t, r.ID)
		if got.RegistrationDeletedAt == nil {
			t.Fatal("absent registration never confirmed")
		}
		if hostFailed {
			if got.CleanupError != "host cleanup is unavailable" || got.CleanedUpAt != nil {
				t.Fatalf("host failure hidden: %+v", got)
			}
		} else if got.CleanupError != "" || got.CleanedUpAt == nil {
			t.Fatalf("cleanup never completed: %+v", got)
		}
	}
}

func TestCleanupFailedListingDoesNotConfirmAbsence(t *testing.T) {
	h := newHarness(t)
	_, pool, host := h.fleet()
	r := h.runnerRow(pool, host, store.RunnerRemoved)
	h.gh.SetMethodError(http.MethodGet, "/actions/runners", http.StatusServiceUnavailable, "unavailable")
	h.c.reap(h.ctx)
	if h.runnerByID(t, r.ID).RegistrationDeletedAt != nil {
		t.Fatal("failed listing was treated as proof of absence")
	}
}

func TestCleanupDoesNotDeleteDifferentRegistrationWithSameName(t *testing.T) {
	h := newHarness(t)
	_, pool, host := h.fleet()
	r := h.runnerRow(pool, host, store.RunnerRemoved)
	remote := h.gh.AddRunner(r.Name, pool.Labels)
	if err := h.st.SetRunnerGitHubID(h.ctx, r.ID, remote.ID+123); err != nil {
		t.Fatal(err)
	}
	h.c.reap(h.ctx)
	if len(h.gh.Runners()) != 1 || h.runnerByID(t, r.ID).RegistrationDeletedAt != nil {
		t.Fatal("registration identity mismatch was ignored")
	}
}

func TestCleanupBusyLookupDefersBeforeDelete(t *testing.T) {
	h := newHarness(t)
	_, pool, host := h.fleet()
	r := h.runnerRow(pool, host, store.RunnerRemoved)
	h.gh.AddRunner(r.Name, pool.Labels)
	h.gh.SetRunnerBusy(r.Name, true)
	h.c.deleteRegistration(h.ctx, r, pool)
	for _, req := range h.gh.Requests() {
		if strings.HasPrefix(req, "DELETE ") {
			t.Fatalf("known-busy registration deleted: %s", req)
		}
	}
	got := h.runnerByID(t, r.ID)
	if got.CleanupAttempts != 0 || !strings.Contains(got.CleanupError, "deferred") {
		t.Fatalf("deferral recorded as failure: %+v", got)
	}
}

func TestCleanupBusyDeleteResponseIsDeferred(t *testing.T) {
	h := newHarness(t)
	_, pool, host := h.fleet()
	r := h.runnerRow(pool, host, store.RunnerRemoved)
	remote := h.gh.AddRunner(r.Name, pool.Labels)
	h.gh.SetRunnerBusy(r.Name, true)
	if err := h.st.SetRunnerGitHubID(h.ctx, r.ID, remote.ID); err != nil {
		t.Fatal(err)
	}
	h.c.deleteRegistration(h.ctx, h.runnerByID(t, r.ID), pool)
	got := h.runnerByID(t, r.ID)
	if got.CleanupAttempts != 0 || !strings.Contains(got.CleanupError, "deferred") || got.RegistrationDeletedAt != nil {
		t.Fatalf("busy response: %+v", got)
	}
	if len(h.gh.Runners()) != 1 {
		t.Fatal("busy registration was removed")
	}
}

func TestCleanupSnapshotNeverSettlesALiveRunner(t *testing.T) {
	h := newHarness(t)
	_, pool, host := h.fleet()
	r := h.runnerRow(pool, host, store.RunnerBusy)
	h.c.reap(h.ctx)
	if h.runnerByID(t, r.ID).RegistrationDeletedAt != nil {
		t.Fatal("empty snapshot settled a live runner")
	}
}

func TestCleanupIdleListingCanRaceWithBusyDelete(t *testing.T) {
	h := newHarness(t)
	_, pool, host := h.fleet()
	r := h.runnerRow(pool, host, store.RunnerRemoved)
	h.gh.AddRunner(r.Name, pool.Labels)
	h.gh.SetMethodError(http.MethodDelete, "/actions/runners/", http.StatusUnprocessableEntity, "Bad request - Runner is currently running a job and cannot be deleted")
	h.c.reap(h.ctx)
	got := h.runnerByID(t, r.ID)
	if got.CleanupAttempts != 0 || !strings.Contains(got.CleanupError, "deferred") || got.RegistrationDeletedAt != nil {
		t.Fatalf("busy race not deferred: %+v", got)
	}
	h.gh.ClearErrors()
	h.c.reap(h.ctx)
	if h.runnerByID(t, r.ID).RegistrationDeletedAt == nil {
		t.Fatal("idle retry never recovered")
	}
}
