package controller

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/eyupio/zoomies/internal/github"
	"github.com/eyupio/zoomies/internal/store"
)

type watchLoopFake struct {
	*repairFake
	pull     github.RepairPull
	pullErr  error
	check    string
	checkErr error
	pulls    int
	checks   int
	onPull   func()
}

func (f *watchLoopFake) RepairPull(context.Context, string, int) (github.RepairPull, error) {
	f.pulls++
	if f.onPull != nil {
		f.onPull()
	}
	if f.pullErr != nil {
		return github.RepairPull{}, f.pullErr
	}
	return f.pull, nil
}

func (f *watchLoopFake) RepairChecks(context.Context, string, string) (string, error) {
	f.checks++
	return f.check, f.checkErr
}

func watchLoopFixture(t *testing.T, h *harness) (*watchLoopFake, *store.EliRepairWatch, *store.Installation) {
	t.Helper()
	inst := h.installation()
	provider := &store.AssistantProvider{Name: "Team", Kind: "openai", Enabled: true}
	if err := h.st.CreateAssistantProvider(h.ctx, provider); err != nil {
		t.Fatal(err)
	}
	repo := inst.Target + "/repo"
	if err := h.st.SetEliRepairPolicy(h.ctx, store.EliRepairPolicy{Repo: repo, InstallationID: inst.ID, ProviderID: provider.ID, Enabled: true, Automatic: true}); err != nil {
		t.Fatal(err)
	}
	fake := &watchLoopFake{
		repairFake: &repairFake{Client: h.gh.Client(inst.Target, inst.TargetType)},
		pull:       github.RepairPull{Number: 1, Branch: "feature", Base: "release", BaseSHA: "base-new", HeadSHA: "head-new", Open: true},
		check:      "failed",
	}
	h.c.clients.entries[inst.ID] = &clientEntry{client: fake, updatedAt: inst.UpdatedAt.Truncate(time.Millisecond)}
	watch, err := h.st.AdmitEliRepairWatch(h.ctx, store.EliRepairWatch{
		InstallationID: inst.ID, Repo: repo, PullNumber: 1, OwnerID: "usr_owner", Checks: true,
		HeadSHA: "head-old", BaseRef: "main", BaseSHA: "base-old",
	})
	if err != nil {
		t.Fatal(err)
	}
	return fake, watch, inst
}

func TestADueWatchRecordsItsPullAndCheckState(t *testing.T) {
	h := newHarness(t)
	fake, watch, _ := watchLoopFixture(t, h)
	started := h.c.Now()
	if !h.c.observeDueEliRepairWatch(h.ctx) {
		t.Fatal("the due watch was not observed")
	}
	got, err := h.st.EliRepairWatch(h.ctx, watch.ID)
	if err != nil {
		t.Fatal(err)
	}
	if fake.pulls != 1 || fake.checks != 1 || got.HeadSHA != "head-new" || got.BaseRef != "release" ||
		got.BaseSHA != "base-new" || got.CheckState != "failed" || got.LastObservedAt.IsZero() {
		t.Fatalf("observation lost: %+v, pulls=%d checks=%d", got, fake.pulls, fake.checks)
	}
	if got.LeaseToken != "" || got.NextCheckAt.UnixMilli() < started.Add(eliWatchPending).UnixMilli() {
		t.Fatalf("observation did not return the watch to its schedule: %+v", got)
	}
}

func TestADisabledRepositoryPolicyPausesItsWatchesWithoutARead(t *testing.T) {
	h := newHarness(t)
	fake, watch, inst := watchLoopFixture(t, h)
	if err := h.st.SetEliRepairPolicy(h.ctx, store.EliRepairPolicy{Repo: watch.Repo, InstallationID: inst.ID, Enabled: false}); err != nil {
		t.Fatal(err)
	}
	if !h.c.observeDueEliRepairWatch(h.ctx) {
		t.Fatal("the watch was not considered")
	}
	got, err := h.st.EliRepairWatch(h.ctx, watch.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Enabled || !strings.Contains(got.PausedReason, "no longer enabled") || fake.pulls != 0 || fake.checks != 0 {
		t.Fatalf("a disabled policy was still observed: %+v, pulls=%d checks=%d", got, fake.pulls, fake.checks)
	}
}

func TestAClosedPullRequestStopsItsWatch(t *testing.T) {
	h := newHarness(t)
	fake, watch, _ := watchLoopFixture(t, h)
	fake.pullErr = &github.RepairTargetChanged{Code: "closed", Message: "The PR is closed."}
	if !h.c.observeDueEliRepairWatch(h.ctx) {
		t.Fatal("the watch was not considered")
	}
	got, err := h.st.EliRepairWatch(h.ctx, watch.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Enabled || !strings.Contains(got.StopReason, "closed or merged") || fake.checks != 0 {
		t.Fatalf("a closed pull request kept its watch: %+v", got)
	}
}

func TestARateLimitedCheckReadUsesTheRetryItWasGiven(t *testing.T) {
	h := newHarness(t)
	fake, watch, _ := watchLoopFixture(t, h)
	fake.checkErr = &github.RateLimitedError{RetryAfter: 45 * time.Second}
	started := h.c.Now()
	if !h.c.observeDueEliRepairWatch(h.ctx) {
		t.Fatal("the watch was not considered")
	}
	got, err := h.st.EliRepairWatch(h.ctx, watch.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.CheckState != "unknown" || got.NextCheckAt.UnixMilli() != started.Add(45*time.Second).UnixMilli() {
		t.Fatalf("a rate limit was not honoured: %+v", got)
	}
	if got.HeadSHA != "head-new" || got.BaseSHA != "base-new" {
		t.Fatalf("a check read failure discarded a pull identity that was read: %+v", got)
	}
}

func TestAStaleClosedObservationDoesNotStopAReAdmittedWatch(t *testing.T) {
	h := newHarness(t)
	fake, watch, inst := watchLoopFixture(t, h)
	fake.pullErr = &github.RepairTargetChanged{Code: "closed", Message: "The PR is closed."}
	fake.onPull = func() {
		if err := h.st.PauseEliRepairWatch(h.ctx, watch.ID, "Paused."); err != nil {
			t.Errorf("pause: %v", err)
			return
		}
		if _, err := h.st.AdmitEliRepairWatch(h.ctx, store.EliRepairWatch{
			InstallationID: inst.ID, Repo: watch.Repo, PullNumber: 1, OwnerID: "usr_owner", Checks: true,
		}); err != nil {
			t.Errorf("re-admit: %v", err)
		}
	}
	if !h.c.observeDueEliRepairWatch(h.ctx) {
		t.Fatal("the watch was not considered")
	}
	got, err := h.st.EliRepairWatch(h.ctx, watch.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !got.Enabled || got.StopReason != "" {
		t.Fatalf("a stale closed response stopped a newer admission: %+v", got)
	}
}
