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
	pull      github.RepairPull
	pullErr   error
	check     string
	checkErr  error
	pulls     int
	checks    int
	onPull    func()
	openPulls []github.RepairPull
	openCap   bool
	openErr   error
	openCalls int
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

func (f *watchLoopFake) RepairCheckEvidence(context.Context, string, string) (github.RepairCheckEvidence, error) {
	f.checks++
	if f.checkErr != nil {
		return github.RepairCheckEvidence{}, f.checkErr
	}
	return github.RepairCheckEvidence{State: f.check, Total: 1, Checks: []github.RepairCheck{{Kind: "check_run", Name: "CI", Status: "completed", Conclusion: "failure", App: "github-actions"}}}, nil
}

func (f *watchLoopFake) RepairOpenPulls(context.Context, string, int) ([]github.RepairPull, bool, error) {
	f.openCalls++
	if f.openErr != nil {
		return nil, false, f.openErr
	}
	return f.openPulls, f.openCap, nil
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
		got.BaseSHA != "base-new" || got.CheckState != "failed" || got.LastObservedAt.IsZero() ||
		!strings.Contains(got.CheckEvidence, `"name":"CI"`) {
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
	finished := h.c.Now()
	got, err := h.st.EliRepairWatch(h.ctx, watch.ID)
	if err != nil {
		t.Fatal(err)
	}
	// The loop reads the clock for itself, after the test did, so the retry is 45s
	// after some instant between the two reads and not exactly 45s after the first.
	// Both ends are compared in milliseconds, which is what the store keeps.
	next := got.NextCheckAt.UnixMilli()
	if got.CheckState != "unknown" ||
		next < started.Add(45*time.Second).UnixMilli() || next > finished.Add(45*time.Second).UnixMilli() {
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

func repositoryWatchFixture(t *testing.T, h *harness) (*watchLoopFake, *store.Installation, string) {
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
	fake := &watchLoopFake{repairFake: &repairFake{Client: h.gh.Client(inst.Target, inst.TargetType)}}
	h.c.clients.entries[inst.ID] = &clientEntry{client: fake, updatedAt: inst.UpdatedAt.Truncate(time.Millisecond)}
	return fake, inst, repo
}

func TestRepositoryAdmissionBackfillsEligiblePullsAndShowsExclusions(t *testing.T) {
	h := newHarness(t)
	fake, inst, repo := repositoryWatchFixture(t, h)
	fake.openCap = true
	fake.openPulls = []github.RepairPull{
		{Number: 1, Title: "ready", Open: true, HeadSHA: "head-1", Branch: "fix-1", Base: "main", BaseSHA: "base-1"},
		{Number: 2, Title: "fork", Open: true, HeadSHA: "head-2", Branch: "fix-2", Fork: true},
		{Number: 3, Title: "draft", Open: true, HeadSHA: "head-3", Branch: "fix-3", Draft: true},
		{Number: 4, Title: "no head", Open: true},
	}
	report, err := h.c.AdmitEliRepositoryWatches(h.ctx, EliRepositoryWatchAdmission{
		Repo: repo, InstallationID: inst.ID, OwnerID: "usr_owner", Capabilities: []string{EliWatchChecks},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Admitted) != 1 || report.Admitted[0].PullNumber != 1 || report.Admitted[0].HeadSHA != "head-1" ||
		report.Admitted[0].BaseRef != "main" || report.Admitted[0].BaseSHA != "base-1" || !report.Capped {
		t.Fatalf("report %+v", report)
	}
	if len(report.Excluded) != 3 || report.Excluded[0].Reason == "" || report.Excluded[1].Reason == "" || report.Excluded[2].Reason == "" {
		t.Fatalf("exclusions were hidden: %+v", report.Excluded)
	}
	watches, err := h.st.EliRepairWatches(h.ctx)
	if err != nil || len(watches) != 1 || watches[0].PullNumber != 1 || !watches[0].Checks {
		t.Fatalf("watches %+v, %v", watches, err)
	}
}

func TestRepositoryAdmissionLeavesAnExistingPauseAlone(t *testing.T) {
	h := newHarness(t)
	fake, inst, repo := repositoryWatchFixture(t, h)
	fake.openPulls = []github.RepairPull{{Number: 1, Title: "paused", Open: true, HeadSHA: "head-1", Branch: "fix-1", Base: "main", BaseSHA: "base-1"}}
	watch, err := h.st.AdmitEliRepairWatch(h.ctx, store.EliRepairWatch{InstallationID: inst.ID, Repo: repo, PullNumber: 1, OwnerID: "usr_owner", Checks: true})
	if err != nil {
		t.Fatal(err)
	}
	if err := h.st.PauseEliRepairWatch(h.ctx, watch.ID, "Paused for this pull request."); err != nil {
		t.Fatal(err)
	}
	report, err := h.c.AdmitEliRepositoryWatches(h.ctx, EliRepositoryWatchAdmission{
		Repo: repo, InstallationID: inst.ID, OwnerID: "usr_owner", Capabilities: []string{EliWatchChecks},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Admitted) != 0 || report.Existing != 0 || len(report.Excluded) != 1 || !strings.Contains(report.Excluded[0].Reason, "Paused") {
		t.Fatalf("repository admission resumed a paused PR: %+v", report)
	}
	got, err := h.st.EliRepairWatch(h.ctx, watch.ID)
	if err != nil || got.Enabled {
		t.Fatalf("a paused watch was resumed: %+v, %v", got, err)
	}
}

func TestRepositoryAdmissionCountsAnExistingWatchWithoutReplacingIt(t *testing.T) {
	h := newHarness(t)
	fake, inst, repo := repositoryWatchFixture(t, h)
	fake.openPulls = []github.RepairPull{{Number: 1, Title: "watched", Open: true, HeadSHA: "head-1", Branch: "fix-1", Base: "main", BaseSHA: "base-1"}}
	existing, err := h.st.AdmitEliRepairWatch(h.ctx, store.EliRepairWatch{InstallationID: inst.ID, Repo: repo, PullNumber: 1, OwnerID: "usr_owner", Checks: true, HeadSHA: "old-head"})
	if err != nil {
		t.Fatal(err)
	}
	report, err := h.c.AdmitEliRepositoryWatches(h.ctx, EliRepositoryWatchAdmission{
		Repo: repo, InstallationID: inst.ID, OwnerID: "usr_owner", Capabilities: []string{EliWatchChecks},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Admitted) != 0 || report.Existing != 1 {
		t.Fatalf("an existing watch was replaced: %+v", report)
	}
	got, err := h.st.EliRepairWatch(h.ctx, existing.ID)
	if err != nil || got.HeadSHA != "old-head" {
		t.Fatalf("an existing watch was overwritten: %+v, %v", got, err)
	}
}

func TestRepositoryAdmissionWritesNothingWhenTheListFails(t *testing.T) {
	h := newHarness(t)
	fake, inst, repo := repositoryWatchFixture(t, h)
	fake.openErr = &github.RateLimitedError{RetryAfter: 30 * time.Second}
	if _, err := h.c.AdmitEliRepositoryWatches(h.ctx, EliRepositoryWatchAdmission{
		Repo: repo, InstallationID: inst.ID, OwnerID: "usr_owner", Capabilities: []string{EliWatchChecks},
	}); err == nil {
		t.Fatal("a failed list admitted a watch")
	}
	watches, err := h.st.EliRepairWatches(h.ctx)
	if err != nil || len(watches) != 0 {
		t.Fatalf("a failed list left watches: %+v, %v", watches, err)
	}
}

func TestAFailedWatchStartsOneRepairForItsHead(t *testing.T) {
	h := newHarness(t)
	fake, watch, _ := watchLoopFixture(t, h)
	fake.repairFake.failedJobs = &github.RepairFailureRun{RunID: 44, Workflow: "CI", RunNumber: 12, Jobs: []github.RepairFailedJob{{ID: 9001, Name: "Go (api)", Conclusion: "failure"}}}
	if !h.c.observeDueEliRepairWatch(h.ctx) {
		t.Fatal("the watch was not observed")
	}
	repair, err := h.st.NextEliRepair(h.ctx)
	if err != nil {
		t.Fatal(err)
	}
	if repair.WatchID != watch.ID || repair.Trigger != "automatic" || repair.HeadSHA != "head-new" ||
		repair.ProviderID == "" || repair.DedupKey != "watch:"+watch.ID+":head-new" {
		t.Fatalf("repair not linked to the watch: %+v", repair)
	}
	got, err := h.st.EliRepairWatch(h.ctx, watch.ID)
	if err != nil || got.ChainAttempts != 1 || got.LastFailure == "" {
		t.Fatalf("watch chain %+v, %v", got, err)
	}
	if h.c.repairWatchEnabled(h.ctx, repair) != true {
		t.Fatal("a live watch was treated as paused")
	}
	if err := h.c.PauseEliRepairWatch(h.ctx, watch.ID, "Paused."); err != nil {
		t.Fatal(err)
	}
	if h.c.repairWatchEnabled(h.ctx, repair) {
		t.Fatal("a paused watch was treated as live")
	}
}

func TestAQueuedWatchRepairStopsWhenTheWatchWasPaused(t *testing.T) {
	h := newHarness(t)
	inst := h.installation()
	provider := &store.AssistantProvider{Name: "Team", Kind: "openai", Enabled: true}
	if err := h.st.CreateAssistantProvider(h.ctx, provider); err != nil {
		t.Fatal(err)
	}
	repo := inst.Target + "/repo"
	if err := h.st.SetEliRepairPolicy(h.ctx, store.EliRepairPolicy{Repo: repo, InstallationID: inst.ID, ProviderID: provider.ID, Enabled: true, Automatic: true}); err != nil {
		t.Fatal(err)
	}
	watch, err := h.st.AdmitEliRepairWatch(h.ctx, store.EliRepairWatch{InstallationID: inst.ID, Repo: repo, PullNumber: 1, OwnerID: "usr_owner", Checks: true, PolicyProviderID: provider.ID})
	if err != nil {
		t.Fatal(err)
	}
	if err := h.st.PauseEliRepairWatch(h.ctx, watch.ID, "Paused for this pull request."); err != nil {
		t.Fatal(err)
	}
	r := &store.EliRepair{InstallationID: inst.ID, Repo: repo, PullNumber: 1, HeadSHA: "old", ProviderID: provider.ID, Trigger: "automatic", DedupKey: "watch-queued", WatchID: watch.ID}
	if _, err := h.st.EnqueueEliRepair(h.ctx, r); err != nil {
		t.Fatal(err)
	}
	h.c.runEliRepair(h.ctx, r)
	got, err := h.st.GetEliRepair(h.ctx, r.ID, "", false)
	if err != nil || got.State != "blocked" || !strings.Contains(got.Message, "paused") {
		t.Fatalf("a paused watch did not stop its queued repair: %+v, %v", got, err)
	}
}

func TestPeriodicDiscoveryAdmitsOnlyNewEligiblePulls(t *testing.T) {
	h := newHarness(t)
	fake, watch, _ := watchLoopFixture(t, h)
	fake.openPulls = []github.RepairPull{
		{Number: 1, Title: "already watched", Open: true, HeadSHA: "head-1", Branch: "fix-1", Base: "main", BaseSHA: "base-1"},
		{Number: 2, Title: "new", Open: true, HeadSHA: "head-2", Branch: "fix-2", Base: "main", BaseSHA: "base-2"},
		{Number: 3, Title: "draft", Open: true, HeadSHA: "head-3", Branch: "fix-3", Draft: true},
	}
	h.c.discoverEliRepairWatches(h.ctx)
	if fake.openCalls != 1 {
		t.Fatalf("open pulls called %d times", fake.openCalls)
	}
	watches, err := h.st.EliRepairWatches(h.ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(watches) != 2 {
		t.Fatalf("watches %+v", watches)
	}
	byPull := map[int]*store.EliRepairWatch{}
	for _, w := range watches {
		byPull[w.PullNumber] = w
	}
	if byPull[1] == nil || byPull[2] == nil || byPull[3] != nil || byPull[2].OwnerID != watch.OwnerID {
		t.Fatalf("new watch admission %+v", byPull)
	}
}

func TestPeriodicDiscoveryDoesNotScanWhenEveryWatchIsPaused(t *testing.T) {
	h := newHarness(t)
	fake, watch, _ := watchLoopFixture(t, h)
	if err := h.st.PauseEliRepairWatch(h.ctx, watch.ID, "Paused."); err != nil {
		t.Fatal(err)
	}
	fake.openPulls = []github.RepairPull{{Number: 2, Title: "new", Open: true, HeadSHA: "head-2", Branch: "fix-2", Base: "main"}}
	h.c.discoverEliRepairWatches(h.ctx)
	if fake.openCalls != 0 {
		t.Fatalf("a paused repository was scanned %d times", fake.openCalls)
	}
	watches, err := h.st.EliRepairWatches(h.ctx)
	if err != nil || len(watches) != 1 {
		t.Fatalf("a paused repository admitted a watch: %+v, %v", watches, err)
	}
}
