package controller

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/eyupio/zoomies/internal/github"
	"github.com/eyupio/zoomies/internal/store"
)

type watchLoopFake struct {
	*repairFake
	pull        github.RepairPull
	pullErr     error
	check       string
	checkErr    error
	pulls       int
	checks      int
	onPull      func()
	openPulls   []github.RepairPull
	openCap     bool
	openErr     error
	openCalls   int
	conflict    *github.RepairConflict
	conflictErr error
	// conflictReads counts the conflict reads the watch made, by either route.
	conflictReads int

	commitConflicts   int
	conflictChanges   []github.ConflictChange
	conflictCommitErr error
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

func (f *watchLoopFake) RepairConflictEvidence(context.Context, string, int) (github.RepairConflict, error) {
	f.conflictReads++
	if f.conflictErr != nil {
		return github.RepairConflict{}, f.conflictErr
	}
	if f.conflict == nil {
		return github.RepairConflict{}, nil
	}
	return *f.conflict, nil
}

func (f *watchLoopFake) CommitConflictResolution(_ context.Context, _ string, _ github.RepairPull, _ string, changes []github.ConflictChange) (string, error) {
	f.commitConflicts++
	f.conflictChanges = append([]github.ConflictChange(nil), changes...)
	if f.conflictCommitErr != nil {
		return "", f.conflictCommitErr
	}
	return "merge-commit", nil
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

// followConflictsToo re-admits the fixture's watch so it follows merge conflicts
// as well as checks. A watch that does not follow conflicts no longer reads them.
func followConflictsToo(t *testing.T, h *harness, watch *store.EliRepairWatch) {
	t.Helper()
	if _, err := h.st.AdmitEliRepairWatch(h.ctx, store.EliRepairWatch{
		InstallationID: watch.InstallationID, Repo: watch.Repo, PullNumber: watch.PullNumber, OwnerID: watch.OwnerID,
		PolicyProviderID: watch.PolicyProviderID, Checks: true, Conflicts: true,
		HeadSHA: watch.HeadSHA, BaseRef: watch.BaseRef, BaseSHA: watch.BaseSHA,
	}); err != nil {
		t.Fatal(err)
	}
}

func TestADueWatchRecordsItsPullAndCheckState(t *testing.T) {
	h := newHarness(t)
	fake, watch, _ := watchLoopFixture(t, h)
	followConflictsToo(t, h, watch)
	fake.conflict = &github.RepairConflict{Number: 1, HeadSHA: "head-new", BaseRef: "release", BaseSHA: "base-new", MergeBaseSHA: "merge-base", Mergeable: false, MergeableKnown: true, MergeState: "dirty"}
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
		!strings.Contains(got.CheckEvidence, `"name":"CI"`) || !strings.Contains(got.ConflictEvidence, `"merge_base_sha":"merge-base"`) || !strings.Contains(got.ConflictEvidence, `"mergeable":false`) {
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
	subscriptions, err := h.st.EliRepairWatchRepositories(h.ctx)
	if err != nil || len(subscriptions) != 1 || subscriptions[0].Repo != repo || !subscriptions[0].Enabled {
		t.Fatalf("repository decision not recorded: %+v, %v", subscriptions, err)
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

// The chain's limit used to be applied by a gate before the store was asked, so
// the store never got the chance to record why it stopped and the page went on
// saying a repair was under way.
func TestAFailedWatchWithNoAttemptsLeftSaysAPersonMustLook(t *testing.T) {
	h := newHarness(t)
	fake, watch, _ := watchLoopFixture(t, h)
	fake.repairFake.failedJobs = &github.RepairFailureRun{RunID: 44, Workflow: "CI", RunNumber: 12, Jobs: []github.RepairFailedJob{{ID: 9001, Name: "Go (api)", Conclusion: "failure"}}}
	for attempt := 1; attempt <= maxEliWatchRepairs; attempt++ {
		r := &store.EliRepair{
			InstallationID: watch.InstallationID, Repo: watch.Repo, PullNumber: 1, HeadSHA: "head-" + string(rune('0'+attempt)),
			ProviderID: "asp_team", Trigger: "automatic", DedupKey: "watch:" + watch.ID + ":earlier-" + string(rune('0'+attempt)),
		}
		if added, _, err := h.st.EnqueueEliWatchRepair(h.ctx, watch.ID, r, "signature", maxEliWatchRepairs, "unused"); err != nil || !added {
			t.Fatalf("attempt %d: added=%t err=%v", attempt, added, err)
		}
	}
	if !h.c.observeDueEliRepairWatch(h.ctx) {
		t.Fatal("the watch was not observed")
	}
	got, err := h.st.EliRepairWatch(h.ctx, watch.ID)
	if err != nil || got.ChainStopped == "" || got.ChainAttempts != maxEliWatchRepairs {
		t.Fatalf("the exhausted chain was not recorded: %+v, %v", got, err)
	}
}

// With no checks to wait on there is nothing to see at thirty-second intervals;
// a conflicts-only watch over a busy repository would otherwise spend two GitHub
// reads per pull request every half minute for as long as it lives.
func TestAConflictsOnlyWatchIsReadAtTheRestingPace(t *testing.T) {
	h := newHarness(t)
	fake, watch, inst := watchLoopFixture(t, h)
	if _, err := h.st.AdmitEliRepairWatch(h.ctx, store.EliRepairWatch{
		InstallationID: inst.ID, Repo: watch.Repo, PullNumber: 1, OwnerID: "usr_owner", Conflicts: true,
		HeadSHA: "head-old", BaseRef: "main", BaseSHA: "base-old", PolicyProviderID: watch.PolicyProviderID,
	}); err != nil {
		t.Fatal(err)
	}
	fake.conflict = &github.RepairConflict{MergeableKnown: true, Mergeable: true, HeadSHA: "head-new", BaseSHA: "base-new", MergeBaseSHA: "mb"}
	if !h.c.observeDueEliRepairWatch(h.ctx) {
		t.Fatal("the watch was not observed")
	}
	got, err := h.st.EliRepairWatch(h.ctx, watch.ID)
	if err != nil {
		t.Fatal(err)
	}
	if gap := got.NextCheckAt.Sub(got.LastObservedAt); gap < eliWatchPassed-time.Second || gap > eliWatchPassed+time.Second {
		t.Fatalf("a mergeable conflicts-only watch is read again after %s, want %s", gap, eliWatchPassed)
	}
}

// A failed read says nothing about the pull request, so it must not replace the
// last good reading with an empty one that the page shows as "not read".
func TestAFailedConflictReadKeepsTheLastGoodEvidence(t *testing.T) {
	h := newHarness(t)
	fake, watch, _ := watchLoopFixture(t, h)
	followConflictsToo(t, h, watch)
	fake.conflict = &github.RepairConflict{MergeableKnown: true, Mergeable: true, HeadSHA: "head-new", BaseSHA: "base-new", MergeBaseSHA: "mb"}
	if !h.c.observeDueEliRepairWatch(h.ctx) {
		t.Fatal("the watch was not observed")
	}
	first, err := h.st.EliRepairWatch(h.ctx, watch.ID)
	if err != nil || first.ConflictEvidence == "" || first.ConflictEvidence == "{}" {
		t.Fatalf("the first reading was not kept: %+v, %v", first, err)
	}
	if _, err := h.st.AdmitEliRepairWatch(h.ctx, store.EliRepairWatch{
		InstallationID: first.InstallationID, Repo: first.Repo, PullNumber: 1, OwnerID: "usr_owner", Checks: true, Conflicts: true,
		PolicyProviderID: first.PolicyProviderID,
	}); err != nil {
		t.Fatal(err)
	}
	fake.conflictErr = errors.New("GitHub is unavailable")
	if !h.c.observeDueEliRepairWatch(h.ctx) {
		t.Fatal("the watch was not observed the second time")
	}
	second, err := h.st.EliRepairWatch(h.ctx, watch.ID)
	if err != nil || second.ConflictEvidence != first.ConflictEvidence {
		t.Fatalf("a failed read replaced the evidence: %q, %v", second.ConflictEvidence, err)
	}
}

// Held incidents are only ever released when the work ends, so the ones that wait
// for a person would stay for as long as the process does.
func TestExpiredConflictClaimsAreDroppedWhenAnotherIsMade(t *testing.T) {
	h := newHarness(t)
	h.c.holdConflict("old", time.Minute)
	h.c.holdConflict("current", time.Hour)
	h.c.conflictMu.Lock()
	h.c.conflictClaims["old"] = h.c.Now().Add(-time.Second)
	h.c.conflictMu.Unlock()
	if !h.c.claimConflict("new", h.c.Now().Add(time.Minute)) {
		t.Fatal("a fresh incident was refused")
	}
	h.c.conflictMu.Lock()
	defer h.c.conflictMu.Unlock()
	if _, kept := h.c.conflictClaims["old"]; kept {
		t.Fatal("an expired hold was kept")
	}
	if _, kept := h.c.conflictClaims["current"]; !kept {
		t.Fatal("a live hold was dropped")
	}
}

// The conflict reading is two GitHub requests, and a watch that only follows
// checks shows nothing it could change, so it is not asked for.
func TestAChecksOnlyWatchDoesNotReadConflicts(t *testing.T) {
	h := newHarness(t)
	fake, watch, _ := watchLoopFixture(t, h)
	fake.conflict = &github.RepairConflict{MergeableKnown: true, Mergeable: false, HeadSHA: "head-new", BaseSHA: "base-new", MergeBaseSHA: "mb"}
	if !h.c.observeDueEliRepairWatch(h.ctx) {
		t.Fatal("the watch was not observed")
	}
	if fake.conflictReads != 0 {
		t.Fatalf("a checks-only watch read conflicts %d times", fake.conflictReads)
	}
	got, err := h.st.EliRepairWatch(h.ctx, watch.ID)
	if err != nil || got.ConflictEvidence != "{}" || got.CheckState != "failed" {
		t.Fatalf("watch %+v, %v", got, err)
	}
}

// pullConflictFake is a client that can read conflicts from the pull request the
// watch has just read, as the real one can.
type pullConflictFake struct {
	*watchLoopFake
	fromPull int
	seen     github.RepairPull
}

func (f *pullConflictFake) RepairConflictFor(_ context.Context, _ string, pull github.RepairPull) (github.RepairConflict, error) {
	f.fromPull++
	f.seen = pull
	return github.RepairConflict{Number: pull.Number, HeadSHA: pull.HeadSHA, BaseRef: pull.Base, BaseSHA: pull.BaseSHA, MergeBaseSHA: "mb", MergeableKnown: true, Mergeable: true}, nil
}

// The pull request was read a moment before, so a client that can use that read
// is not made to fetch it again.
func TestAConflictWatchReusesThePullRequestItJustRead(t *testing.T) {
	h := newHarness(t)
	fake, watch, inst := watchLoopFixture(t, h)
	followConflictsToo(t, h, watch)
	wrapped := &pullConflictFake{watchLoopFake: fake}
	h.c.clients.entries[inst.ID] = &clientEntry{client: wrapped, updatedAt: inst.UpdatedAt.Truncate(time.Millisecond)}
	if !h.c.observeDueEliRepairWatch(h.ctx) {
		t.Fatal("the watch was not observed")
	}
	if wrapped.fromPull != 1 || fake.conflictReads != 0 || fake.pulls != 1 {
		t.Fatalf("conflict reads from the pull = %d, by number = %d, pull reads = %d; want 1, 0, 1", wrapped.fromPull, fake.conflictReads, fake.pulls)
	}
	if wrapped.seen.HeadSHA != "head-new" {
		t.Fatalf("the conflict read was given %+v, not the pull request just read", wrapped.seen)
	}
	got, err := h.st.EliRepairWatch(h.ctx, watch.ID)
	if err != nil || !strings.Contains(got.ConflictEvidence, `"merge_base_sha":"mb"`) {
		t.Fatalf("conflict evidence %q, %v", got.ConflictEvidence, err)
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
	fake, watch, inst := watchLoopFixture(t, h)
	if _, err := h.st.SetEliRepairWatchRepository(h.ctx, store.EliRepairWatchRepository{
		InstallationID: inst.ID, Repo: watch.Repo, OwnerID: watch.OwnerID, Checks: true,
	}); err != nil {
		t.Fatal(err)
	}
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

func TestPeriodicDiscoveryDoesNotInferARepositoryFromOnePRWatch(t *testing.T) {
	h := newHarness(t)
	fake, _, _ := watchLoopFixture(t, h)
	fake.openPulls = []github.RepairPull{{Number: 2, Title: "new", Open: true, HeadSHA: "head-2", Branch: "fix-2", Base: "main"}}
	h.c.discoverEliRepairWatches(h.ctx)
	if fake.openCalls != 0 {
		t.Fatalf("a per-PR watch admitted a repository-wide scan %d times", fake.openCalls)
	}
	watches, err := h.st.EliRepairWatches(h.ctx)
	if err != nil || len(watches) != 1 {
		t.Fatalf("a per-PR watch admitted another watch: %+v, %v", watches, err)
	}
}

func TestARepositoryDecisionCanBePausedAndResumed(t *testing.T) {
	h := newHarness(t)
	fake, inst, repo := repositoryWatchFixture(t, h)
	fake.openPulls = []github.RepairPull{{
		Number: 1, Title: "watched", Open: true, HeadSHA: "head-1", Branch: "fix-1", Base: "main", BaseSHA: "base-1",
	}}
	if _, err := h.c.AdmitEliRepositoryWatches(h.ctx, EliRepositoryWatchAdmission{
		Repo: repo, InstallationID: inst.ID, OwnerID: "usr_owner", Capabilities: []string{EliWatchChecks, EliWatchConflicts},
	}); err != nil {
		t.Fatal(err)
	}
	decisions, err := h.st.EliRepairWatchRepositories(h.ctx)
	if err != nil || len(decisions) != 1 {
		t.Fatalf("decisions = %+v, %v", decisions, err)
	}
	paused, err := h.c.PauseEliRepositoryWatches(h.ctx, decisions[0].ID)
	if err != nil || paused.Enabled {
		t.Fatalf("paused = %+v, %v", paused, err)
	}
	resumed, err := h.c.ResumeEliRepositoryWatches(h.ctx, decisions[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	if resumed == nil || len(resumed.Admitted) != 0 || resumed.Existing != 1 {
		t.Fatalf("resumed = %+v", resumed)
	}
	if _, err := h.c.ResumeEliRepositoryWatches(h.ctx, "erwr_nosuch"); err == nil {
		t.Fatal("a missing repository decision was resumed")
	}
}
