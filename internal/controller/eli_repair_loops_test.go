package controller

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/eyupio/zoomies/internal/github"
	"github.com/eyupio/zoomies/internal/store"
)

// Model work and check observation share this client in the real controller.
// Mutable counters are atomic so the race detector exercises that overlap.
type observingRepairFake struct {
	*repairFake
	modelEntered <-chan struct{}
	panicNext    atomic.Bool
	pulls        atomic.Int32
	comments     atomic.Int32
	onPull       func()
	onComment    func(context.Context, string)
}

func (f *observingRepairFake) RepairPull(ctx context.Context, _ string, pull int) (github.RepairPull, error) {
	f.pulls.Add(1)
	if f.onPull != nil {
		f.onPull()
	}
	if pull == 1 {
		return github.RepairPull{Number: 1, Branch: "feature", HeadSHA: "old", Open: true}, nil
	}
	if f.panicNext.Swap(false) {
		panic("check transport panicked")
	}
	if f.modelEntered != nil {
		select {
		case <-f.modelEntered:
		case <-ctx.Done():
			return github.RepairPull{}, ctx.Err()
		}
	}
	if pull == 4 {
		return github.RepairPull{}, &github.RepairTargetChanged{Code: "closed", Message: "The PR is closed."}
	}
	return github.RepairPull{Number: pull, HeadSHA: fmt.Sprintf("published-%d", pull), Open: true}, nil
}

func (f *observingRepairFake) RepairChecks(_ context.Context, _, sha string) (string, error) {
	if sha == "published-2" {
		return "passed", nil
	}
	return "failed", nil
}

func (f *observingRepairFake) RepairComment(ctx context.Context, _ string, _ int, _ int64, text string) (int64, error) {
	f.comments.Add(1)
	if f.onComment != nil {
		f.onComment(ctx, text)
	}
	return 99, ctx.Err()
}

func seedObservedRepair(t *testing.T, h *harness, installation string, pull int) *store.EliRepair {
	t.Helper()
	r := &store.EliRepair{Repo: fmt.Sprintf("acme/observed-%d", pull), InstallationID: installation, PullNumber: pull, DedupKey: fmt.Sprintf("observed-%d", pull)}
	if _, err := h.st.EnqueueEliRepair(h.ctx, r); err != nil {
		t.Fatal(err)
	}
	r.State, r.CommitSHA = "checking", fmt.Sprintf("published-%d", pull)
	published := h.c.Now()
	r.PublishedAt = &published
	r.Details.Summary = "Keep the retry deadline."
	if err := h.st.SaveEliRepair(h.ctx, r); err != nil {
		t.Fatal(err)
	}
	return r
}

func TestRepairChecksProgressWhileTheOnlyModelWorkerIsBusy(t *testing.T) {
	for _, panicCheck := range []bool{false, true} {
		t.Run(fmt.Sprintf("check panic %t", panicCheck), func(t *testing.T) {
			h := newHarness(t)
			inst := h.installation()
			entered, release := make(chan struct{}), make(chan struct{})
			var modelCalls atomic.Int32
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if modelCalls.Add(1) == 1 {
					close(entered)
				}
				select {
				case <-release:
				case <-r.Context().Done():
				}
			}))
			t.Cleanup(srv.Close)
			fake := &observingRepairFake{repairFake: &repairFake{Client: h.gh.Client(inst.Target, inst.TargetType)}, modelEntered: entered}
			fake.panicNext.Store(panicCheck)
			h.c.loopRestartDelay = time.Millisecond
			h.c.clients.entries[inst.ID] = &clientEntry{client: fake, updatedAt: inst.UpdatedAt.Truncate(time.Millisecond)}
			provider := &store.AssistantProvider{Name: "Team", Kind: "openai_compatible", BaseURL: srv.URL, Model: "blocked-model", Enabled: true}
			if err := h.st.CreateAssistantProvider(h.ctx, provider); err != nil {
				t.Fatal(err)
			}
			queued := []*store.EliRepair{}
			for _, repo := range []string{"acme/first", "acme/second"} {
				if err := h.st.SetEliRepairPolicy(h.ctx, store.EliRepairPolicy{Repo: repo, InstallationID: inst.ID, ProviderID: provider.ID, Enabled: true, Automatic: true}); err != nil {
					t.Fatal(err)
				}
				r := &store.EliRepair{Repo: repo, InstallationID: inst.ID, PullNumber: 1, RunID: 9, HeadSHA: "old", Trigger: "automatic", DedupKey: repo}
				if _, err := h.st.EnqueueEliRepair(h.ctx, r); err != nil {
					t.Fatal(err)
				}
				queued = append(queued, r)
			}
			watched := []*store.EliRepair{}
			for _, pull := range []int{2, 3, 4} {
				watched = append(watched, seedObservedRepair(t, h, inst.ID, pull))
			}
			if err := h.st.SetEliRepairPolicy(h.ctx, store.EliRepairPolicy{Repo: watched[0].Repo, InstallationID: inst.ID, Enabled: false}); err != nil {
				t.Fatal(err)
			}
			interrupted := &store.EliRepair{Repo: "acme/interrupted", DedupKey: "old-worker"}
			if _, err := h.st.EnqueueEliRepair(h.ctx, interrupted); err != nil {
				t.Fatal(err)
			}
			interrupted.State = "working"
			if err := h.st.SaveEliRepair(h.ctx, interrupted); err != nil {
				t.Fatal(err)
			}
			first, err := h.st.NextEliRepair(h.ctx)
			if err != nil {
				t.Fatal(err)
			}
			stillQueued := queued[0]
			if first.ID == stillQueued.ID {
				stillQueued = queued[1]
			}
			h.advance(31 * time.Second)
			ctx, cancel := context.WithCancel(h.ctx)
			h.c.startRepairLoops(ctx)
			done := make(chan struct{})
			go func() { h.c.wg.Wait(); close(done) }()
			t.Cleanup(func() {
				cancel()
				close(release)
				select {
				case <-done:
				case <-time.After(5 * time.Second):
					t.Error("repair loops did not join after cancellation")
				}
			})
			select {
			case <-entered:
			case <-time.After(5 * time.Second):
				t.Fatal("the repair never reached the model")
			}
			for i, want := range []string{"succeeded", "checks_failed", "superseded"} {
				eventually(t, 5*time.Second, "checks to settle while the model is blocked", func() bool {
					r, err := h.st.GetEliRepair(h.ctx, watched[i].ID, "", false)
					return err == nil && r.State == want
				})
			}
			second, err := h.st.GetEliRepair(h.ctx, stillQueued.ID, "", false)
			if err != nil || second.State != "queued" || modelCalls.Load() != 1 {
				t.Fatalf("generation was not serial: %+v, %v, model calls %d", second, err, modelCalls.Load())
			}
			if panicCheck && h.c.LoopPanics()["eli-repair-checks"].Count != 1 {
				t.Fatal("the check loop panic was not reported and recovered")
			}
			old, err := h.st.GetEliRepair(h.ctx, interrupted.ID, "", false)
			if err != nil || old.State != "interrupted" {
				t.Fatalf("startup replayed uncertain work: %+v, %v", old, err)
			}
			cancel()
			select {
			case <-done:
			case <-time.After(5 * time.Second):
				t.Fatal("cancellation did not stop the busy model and check loop")
			}
			if fake.commits != 0 {
				t.Fatal("a cancelled model attempt published a commit")
			}
		})
	}
}

func TestRepairCheckMonitoringHonoursControllerGates(t *testing.T) {
	for _, gate := range []string{"draining", "expired lease", "fenced", "cancelled"} {
		for _, duringRead := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s during read %t", gate, duringRead), func(t *testing.T) {
				h := newHarness(t)
				inst := h.installation()
				fake := &observingRepairFake{repairFake: &repairFake{Client: h.gh.Client(inst.Target, inst.TargetType)}}
				h.c.clients.entries[inst.ID] = &clientEntry{client: fake, updatedAt: inst.UpdatedAt.Truncate(time.Millisecond)}
				r := seedObservedRepair(t, h, inst.ID, 2)
				h.advance(31 * time.Second)
				ctx, cancel := context.WithCancel(h.ctx)
				defer cancel()
				stop := func() {
					switch gate {
					case "draining":
						h.c.transferDraining.Store(true)
					case "expired lease":
						h.c.lease = &store.ControllerLease{Holder: "expired", RenewedAt: h.c.Now().Add(-2 * LeaseTTL)}
					case "fenced":
						h.c.fence.Store(&store.RecoveryFence{Fenced: true})
					case "cancelled":
						cancel()
					}
				}
				var wantPulls int32
				if duringRead {
					fake.onPull = stop
					wantPulls = 1
				} else {
					stop()
				}
				h.c.checkEliRepairs(ctx)
				got, err := h.st.GetEliRepair(h.ctx, r.ID, "", false)
				if err != nil || got.State != "checking" || fake.pulls.Load() != wantPulls || fake.comments.Load() != 0 {
					t.Fatalf("monitoring crossed the %s gate: %+v, %v", gate, got, err)
				}
			})
		}
	}
}

func TestADelayedRepairCommentCannotReplaceANewerCheckOutcome(t *testing.T) {
	h := newHarness(t)
	inst := h.installation()
	entered, release := make(chan struct{}), make(chan struct{})
	fake := &observingRepairFake{repairFake: &repairFake{Client: h.gh.Client(inst.Target, inst.TargetType)}}
	fake.onComment = func(ctx context.Context, text string) {
		if strings.Contains(text, "Pushed commit") {
			close(entered)
			select {
			case <-release:
			case <-ctx.Done():
			}
		}
	}
	h.c.clients.entries[inst.ID] = &clientEntry{client: fake, updatedAt: inst.UpdatedAt.Truncate(time.Millisecond)}
	r := seedObservedRepair(t, h, inst.ID, 2)
	h.advance(31 * time.Second)
	ctx, cancel := context.WithCancel(h.ctx)
	defer cancel()
	done := make(chan struct{})
	go func() { defer close(done); h.c.repairNotice(ctx, fake, r, "Pushed commit for review.") }()
	t.Cleanup(func() {
		cancel()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Error("the comment did not stop after cancellation")
		}
	})
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("the publication comment did not start")
	}
	h.c.checkEliRepairs(h.ctx)
	settled, err := h.st.GetEliRepair(h.ctx, r.ID, "", false)
	if err != nil || settled.State != "succeeded" {
		t.Fatalf("checks did not settle: %+v, %v", settled, err)
	}
	// Let the worker's old checking snapshot return after the new outcome.
	close(release)
	<-done
	got, err := h.st.GetEliRepair(h.ctx, r.ID, "", false)
	if err != nil || got.State != "succeeded" || got.Details.Summary != r.Details.Summary || got.Message != settled.Message || !got.UpdatedAt.Equal(settled.UpdatedAt) {
		t.Fatalf("delayed comment replaced the check outcome: %+v, %v", got, err)
	}
	before := fake.comments.Load()
	h.c.repairNotice(h.ctx, fake, r, "Stale checking update.")
	if fake.comments.Load() != before {
		t.Fatal("a stale notice was posted after the repair had settled")
	}
}
