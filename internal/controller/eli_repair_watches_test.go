package controller

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/eyupio/zoomies/internal/store"
)

func watchPolicy(t *testing.T, h *harness, automatic bool) (*store.Installation, *store.AssistantProvider, string) {
	t.Helper()
	inst := h.installation()
	provider := &store.AssistantProvider{Name: "Team", Kind: "openai", Enabled: true}
	if err := h.st.CreateAssistantProvider(h.ctx, provider); err != nil {
		t.Fatal(err)
	}
	repo := inst.Target + "/repo"
	policy := store.EliRepairPolicy{Repo: repo, InstallationID: inst.ID, ProviderID: provider.ID, Enabled: true, Automatic: automatic, AskFirst: !automatic}
	if err := h.st.SetEliRepairPolicy(h.ctx, policy); err != nil {
		t.Fatal(err)
	}
	return inst, provider, repo
}

func TestOnlyAnExplicitAdmissionCreatesADurableWatch(t *testing.T) {
	h := newHarness(t)
	inst, provider, repo := watchPolicy(t, h, true)
	fake := &repairFake{Client: h.gh.Client(inst.Target, inst.TargetType), base: "release"}
	h.c.clients.entries[inst.ID] = &clientEntry{client: fake, updatedAt: inst.UpdatedAt.Truncate(time.Millisecond)}

	// A one-attempt Automatic policy is unchanged: nothing watching it exists
	// until a person asks, even after a failed job is queued for it.
	if err := h.c.enqueueRepairWebhook(h.ctx, inst, "workflow_job", []byte(`{"action":"completed","repository":{"full_name":"`+repo+`"},"workflow_job":{"conclusion":"failure","head_sha":"head-a","run_id":9,"id":1}}`)); err != nil {
		t.Fatal(err)
	}
	watches, err := h.st.EliRepairWatches(h.ctx)
	if err != nil || len(watches) != 0 {
		t.Fatalf("a policy or webhook admitted a watch by itself: %+v, %v", watches, err)
	}

	watch, err := h.c.AdmitEliRepairWatch(h.ctx, EliWatchAdmission{
		Repo: repo, PullNumber: 1, InstallationID: inst.ID, OwnerID: "usr_owner", Capabilities: []string{EliWatchChecks},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !watch.Enabled || !watch.Checks || watch.Conflicts || watch.OwnerID != "usr_owner" ||
		watch.PolicyProviderID != provider.ID || watch.PolicyUpdatedAt.IsZero() ||
		watch.HeadSHA != "old" || watch.BaseRef != "release" {
		t.Fatalf("admitted watch lost what authorised it: %+v", watch)
	}
	again, err := h.c.AdmitEliRepairWatch(h.ctx, EliWatchAdmission{
		Repo: strings.ToUpper(repo), PullNumber: 1, InstallationID: inst.ID, OwnerID: "usr_owner", Capabilities: []string{EliWatchChecks, EliWatchChecks},
	})
	if err != nil || again.ID != watch.ID {
		t.Fatalf("a repeated explicit admission made another watch: %+v, %v", again, err)
	}
}

func TestAdmissionRefusesWhatItCannotHonour(t *testing.T) {
	for _, tc := range []struct {
		name     string
		prepare  func(*testing.T, *harness, *store.Installation, string)
		admit    func(*store.Installation, string) EliWatchAdmission
		wantWord string
	}{
		{"no policy", func(*testing.T, *harness, *store.Installation, string) {}, func(inst *store.Installation, repo string) EliWatchAdmission {
			return EliWatchAdmission{Repo: repo, PullNumber: 1, InstallationID: inst.ID, OwnerID: "usr_owner", Capabilities: []string{EliWatchChecks}}
		}, "not enabled"},
		{"ask first is one attempt", func(t *testing.T, h *harness, inst *store.Installation, repo string) {
			p := &store.AssistantProvider{Name: "Team", Kind: "openai", Enabled: true}
			if err := h.st.CreateAssistantProvider(h.ctx, p); err != nil {
				t.Fatal(err)
			}
			if err := h.st.SetEliRepairPolicy(h.ctx, store.EliRepairPolicy{Repo: repo, InstallationID: inst.ID, ProviderID: p.ID, Enabled: true, AskFirst: true}); err != nil {
				t.Fatal(err)
			}
		}, func(inst *store.Installation, repo string) EliWatchAdmission {
			return EliWatchAdmission{Repo: repo, PullNumber: 1, InstallationID: inst.ID, OwnerID: "usr_owner", Capabilities: []string{EliWatchChecks}}
		}, "automatic repairs"},
		{"provider cannot pay", func(t *testing.T, h *harness, inst *store.Installation, repo string) {
			p := &store.AssistantProvider{Name: "Personal", Kind: "openai", Enabled: true, OwnerID: "usr_person"}
			if err := h.st.CreateAssistantProvider(h.ctx, p); err != nil {
				t.Fatal(err)
			}
			if err := h.st.SetEliRepairPolicy(h.ctx, store.EliRepairPolicy{Repo: repo, InstallationID: inst.ID, ProviderID: p.ID, Enabled: true, Automatic: true}); err != nil {
				t.Fatal(err)
			}
		}, func(inst *store.Installation, repo string) EliWatchAdmission {
			return EliWatchAdmission{Repo: repo, PullNumber: 1, InstallationID: inst.ID, OwnerID: "usr_owner", Capabilities: []string{EliWatchChecks}}
		}, "cannot pay"},
		{"no capability", func(*testing.T, *harness, *store.Installation, string) {}, func(inst *store.Installation, repo string) EliWatchAdmission {
			return EliWatchAdmission{Repo: repo, PullNumber: 1, InstallationID: inst.ID, OwnerID: "usr_owner"}
		}, "at least one"},
		{"no owner", func(*testing.T, *harness, *store.Installation, string) {}, func(inst *store.Installation, repo string) EliWatchAdmission {
			return EliWatchAdmission{Repo: repo, PullNumber: 1, InstallationID: inst.ID, Capabilities: []string{EliWatchChecks}}
		}, "person who authorised"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := newHarness(t)
			inst := h.installation()
			repo := inst.Target + "/repo"
			h.c.clients.entries[inst.ID] = &clientEntry{client: &repairFake{Client: h.gh.Client(inst.Target, inst.TargetType)}, updatedAt: inst.UpdatedAt.Truncate(time.Millisecond)}
			tc.prepare(t, h, inst, repo)
			_, err := h.c.AdmitEliRepairWatch(h.ctx, tc.admit(inst, repo))
			if err == nil || !strings.Contains(err.Error(), tc.wantWord) {
				t.Fatalf("admission answered %v, want a refusal mentioning %q", err, tc.wantWord)
			}
			if watches, e := h.st.EliRepairWatches(h.ctx); e != nil || len(watches) != 0 {
				t.Fatalf("a refused admission left a watch: %+v, %v", watches, e)
			}
		})
	}
}

func TestPauseAndStopAreRecordedWithoutResumingTheWatch(t *testing.T) {
	h := newHarness(t)
	inst := h.installation()
	watch, err := h.st.AdmitEliRepairWatch(h.ctx, store.EliRepairWatch{InstallationID: inst.ID, Repo: "acme/repo", PullNumber: 1, OwnerID: "usr_owner", Checks: true})
	if err != nil {
		t.Fatal(err)
	}
	if err := h.c.PauseEliRepairWatch(h.ctx, watch.ID, ""); err != nil {
		t.Fatal(err)
	}
	paused, err := h.st.EliRepairWatch(h.ctx, watch.ID)
	if err != nil || paused.Enabled || paused.PausedReason == "" {
		t.Fatalf("pause not recorded: %+v, %v", paused, err)
	}
	if err := h.c.StopEliRepairWatch(h.ctx, watch.ID, "The pull request was closed."); err != nil {
		t.Fatal(err)
	}
	stopped, err := h.st.EliRepairWatch(h.ctx, watch.ID)
	if err != nil || stopped.Enabled || stopped.StopReason != "The pull request was closed." {
		t.Fatalf("stop not recorded: %+v, %v", stopped, err)
	}
}

func TestStartingTheRepairLoopsRecoversAnInterruptedWatch(t *testing.T) {
	h := newHarness(t)
	inst := h.installation()
	watch, err := h.st.AdmitEliRepairWatch(h.ctx, store.EliRepairWatch{InstallationID: inst.ID, Repo: "acme/repo", PullNumber: 1, OwnerID: "usr_owner", Checks: true})
	if err != nil {
		t.Fatal(err)
	}
	claimed, err := h.st.ClaimDueEliRepairWatch(h.ctx, h.c.Now(), time.Hour)
	if err != nil || claimed.ID != watch.ID {
		t.Fatalf("watch was not claimed: %+v, %v", claimed, err)
	}
	// Recovery is what is being held here, and recovery makes the watch due. A
	// loop that has started would claim it at once, which is what it is for, and
	// would leave a lease where this reads the row. Cancelling straight after
	// starting does not prevent that: on a busy machine the loop's goroutine
	// runs first, and this failed about once in four hundred runs under load.
	// Draining stops the loops from acting while recovery, which runs before
	// they start, still happens.
	h.c.transferDraining.Store(true)
	ctx, cancel := context.WithCancel(h.ctx)
	h.c.startRepairLoops(ctx)
	cancel()
	done := make(chan struct{})
	go func() { h.c.wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("repair loops did not join")
	}
	recovered, err := h.st.EliRepairWatch(h.ctx, watch.ID)
	if err != nil || recovered.LeaseToken != "" || recovered.NextCheckAt.UnixMilli() > h.c.Now().UnixMilli() {
		t.Fatalf("startup did not recover the watch: %+v, %v", recovered, err)
	}
}

func TestAPolicyLookupDoesNotCrossCasingToAnEnabledRow(t *testing.T) {
	h := newHarness(t)
	inst := h.installation()
	provider := &store.AssistantProvider{Name: "Team", Kind: "openai", Enabled: true}
	if err := h.st.CreateAssistantProvider(h.ctx, provider); err != nil {
		t.Fatal(err)
	}
	for _, policy := range []store.EliRepairPolicy{
		{Repo: "ACME/REPO", InstallationID: inst.ID, ProviderID: provider.ID, Enabled: true, Automatic: true},
		{Repo: "acme/repo", InstallationID: inst.ID, ProviderID: provider.ID, Enabled: false},
	} {
		if err := h.st.SetEliRepairPolicy(h.ctx, policy); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := h.c.repairPolicy(h.ctx, "acme/repo", inst.ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("a disabled exact policy was bypassed through another casing: %v", err)
	}
	if _, err := h.c.repairPolicy(h.ctx, "Acme/Repo", inst.ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("an ambiguous casing selected a policy: %v", err)
	}
	got, err := h.c.repairPolicy(h.ctx, "ACME/REPO", inst.ID)
	if err != nil || !got.Enabled {
		t.Fatalf("the exact enabled policy was not selected: %+v, %v", got, err)
	}
}
