package controller

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/eyupio/zoomies/internal/agent"
	"github.com/eyupio/zoomies/internal/assistant"
	"github.com/eyupio/zoomies/internal/github"
	"github.com/eyupio/zoomies/internal/gitmerge"
	"github.com/eyupio/zoomies/internal/store"
)

type conflictFixture struct {
	fake  *watchLoopFake
	watch *store.EliRepairWatch
	inst  *store.Installation
	host  *store.Host
	repo  string
	head  string
	base  string
	merge string
}

func newConflictFixture(t *testing.T, h *harness) conflictFixture {
	t.Helper()
	fake, _, inst := watchLoopFixture(t, h)
	repo := inst.Target + "/repo"
	ancestor := strings.Repeat("a", 40)
	base := strings.Repeat("b", 40)
	head := strings.Repeat("c", 40)
	mergeFixture(t, h.gh, repo, ancestor, nil, map[string]string{"file.txt": "a\nb\nc\n"})
	mergeFixture(t, h.gh, repo, base, []string{ancestor}, map[string]string{"file.txt": "a\nb\nC\n"})
	mergeFixture(t, h.gh, repo, head, []string{ancestor}, map[string]string{"file.txt": "a\nB\nc\n"})
	fake.pull = github.RepairPull{Number: 1, Branch: "feature", Base: "release", BaseSHA: base, HeadSHA: head, Open: true}
	fake.conflict = &github.RepairConflict{
		Number: 1, HeadSHA: head, BaseRef: "release", BaseSHA: base, MergeBaseSHA: ancestor,
		Mergeable: false, MergeableKnown: true, MergeState: "dirty",
	}
	host := h.hostWith("merge-host", agent.FeatureMergeWorker)
	watch, err := h.c.AdmitEliRepairWatch(h.ctx, EliWatchAdmission{
		Repo: repo, PullNumber: 1, InstallationID: inst.ID, OwnerID: "usr_owner",
		Capabilities: []string{EliWatchChecks, EliWatchConflicts},
	})
	if err != nil {
		t.Fatalf("admitting the conflict watch: %v", err)
	}
	return conflictFixture{fake: fake, watch: watch, inst: inst, host: host, repo: repo, head: head, base: base, merge: ancestor}
}

func waitForMergeTask(t *testing.T, h *harness, hostID string) agent.Task {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if h.hasTaskOfKind(hostID, agent.TaskMergeConflict) {
			return h.taskOfKind(hostID, agent.TaskMergeConflict)
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("no merge task was queued for %s", hostID)
	return agent.Task{}
}

func (f conflictFixture) resolveAnswer() gitmerge.WorkerResponse {
	return gitmerge.WorkerResponse{Result: &gitmerge.Result{
		Tree: "merged-tree",
		Conflicts: []gitmerge.Conflict{{
			Path: "file.txt", Ancestor: "a\nb\nc\n", Base: "a\nb\nC\n", Head: "a\nB\nc\n",
		}},
		Changes: []gitmerge.Change{{
			Path: "file.txt", Mode: "100644", Content: []byte("<<<<<<< target\na\nb\nC\n=======\na\nB\nc\n>>>>>>> pull request\n"),
		}},
	}}
}

func TestAConflictedWatchStartsAWorkerAndPublishesTheResolvedMerge(t *testing.T) {
	h := newHarness(t)
	fixture := newConflictFixture(t, h)
	h.c.openProvider = func(*store.AssistantProvider, string) (assistant.Provider, error) {
		return assistant.NewFake(assistant.FakeOptions{Model: "scripted", Script: func(assistant.Request) []assistant.Event {
			return []assistant.Event{{Delta: `{"summary":"keep both edits","files":[{"path":"file.txt","content":"a\nb\nc\n"}]}`}}
		}}), nil
	}

	if !h.c.observeDueEliRepairWatch(h.ctx) {
		t.Fatal("the due conflict watch was not observed")
	}
	task := waitForMergeTask(t, h, fixture.host.ID)
	if task.MergeID == "" || task.MergeAncestor == "" || task.MergeBase == "" || task.MergeHead == "" {
		t.Fatalf("merge task = %+v", task)
	}
	if task.MergeImage == "" || !strings.HasPrefix(task.MergeImage, "ghcr.io/eyupio/zoomies-git:") {
		t.Fatalf("merge image = %q", task.MergeImage)
	}

	if err := h.c.ReportMergeAnswer(h.ctx, fixture.host.ID, task.MergeID, fixture.resolveAnswer()); err != nil {
		t.Fatalf("ReportMergeAnswer: %v", err)
	}
	h.c.processConflictAnswer(h.ctx, task.MergeID)
	if fixture.fake.commitConflicts != 1 {
		t.Fatalf("conflict commits = %d, want 1", fixture.fake.commitConflicts)
	}
	if len(fixture.fake.conflictChanges) != 1 || fixture.fake.conflictChanges[0].Path != "file.txt" ||
		string(fixture.fake.conflictChanges[0].Content) != "a\nb\nc\n" {
		t.Fatalf("published changes = %+v", fixture.fake.conflictChanges)
	}
}

func TestAPausedWatchRefusesToPublishItsConflictResolution(t *testing.T) {
	h := newHarness(t)
	fixture := newConflictFixture(t, h)
	h.c.openProvider = func(*store.AssistantProvider, string) (assistant.Provider, error) {
		return assistant.NewFake(assistant.FakeOptions{Model: "scripted", Script: func(assistant.Request) []assistant.Event {
			return []assistant.Event{{Delta: `{"summary":"keep both edits","files":[{"path":"file.txt","content":"a\nb\nc\n"}]}`}}
		}}), nil
	}
	if !h.c.observeDueEliRepairWatch(h.ctx) {
		t.Fatal("the due conflict watch was not observed")
	}
	task := waitForMergeTask(t, h, fixture.host.ID)
	if err := h.c.PauseEliRepairWatch(h.ctx, fixture.watch.ID, "paused by the test"); err != nil {
		t.Fatal(err)
	}
	if err := h.c.ReportMergeAnswer(h.ctx, fixture.host.ID, task.MergeID, fixture.resolveAnswer()); err != nil {
		t.Fatal(err)
	}
	h.c.processConflictAnswer(h.ctx, task.MergeID)
	if fixture.fake.commitConflicts != 0 {
		t.Fatalf("a paused watch published %d commits", fixture.fake.commitConflicts)
	}
}

func TestAnUnsupportedConflictIsHeldForAPersonAndNeverReachesTheModel(t *testing.T) {
	h := newHarness(t)
	fixture := newConflictFixture(t, h)
	h.c.openProvider = func(*store.AssistantProvider, string) (assistant.Provider, error) {
		t.Fatal("the model was opened for a conflict it may not resolve")
		return nil, nil
	}
	if !h.c.observeDueEliRepairWatch(h.ctx) {
		t.Fatal("the due conflict watch was not observed")
	}
	task := waitForMergeTask(t, h, fixture.host.ID)
	answer := gitmerge.WorkerResponse{Result: &gitmerge.Result{
		Tree:        "merged-tree",
		Unsupported: []gitmerge.UnsupportedConflict{{Path: "image.bin", Reason: "binary content is not resolved by a model"}},
	}}
	if err := h.c.ReportMergeAnswer(h.ctx, fixture.host.ID, task.MergeID, answer); err != nil {
		t.Fatal(err)
	}
	h.c.processConflictAnswer(h.ctx, task.MergeID)
	if fixture.fake.commitConflicts != 0 {
		t.Fatalf("an unsupported conflict published %d commits", fixture.fake.commitConflicts)
	}
	key := conflictKey(fixture.watch.ID, fixture.head, fixture.base, fixture.merge)
	if !h.c.conflictClaimed(key) {
		t.Fatal("the unsupported incident was not held for a person")
	}
}

// The conflict loop is the only caller of processConflictAnswer in production.
// It must see an answered attempt even when the answer arrived before the loop
// started.
func TestTheConflictLoopProcessesAnAnswerAlreadyWaiting(t *testing.T) {
	h := newHarness(t)
	fixture := newConflictFixture(t, h)
	h.c.openProvider = func(*store.AssistantProvider, string) (assistant.Provider, error) {
		return assistant.NewFake(assistant.FakeOptions{Model: "scripted", Script: func(assistant.Request) []assistant.Event {
			return []assistant.Event{{Delta: `{"summary":"keep both edits","files":[{"path":"file.txt","content":"a\nb\nc\n"}]}`}}
		}}), nil
	}
	if !h.c.observeDueEliRepairWatch(h.ctx) {
		t.Fatal("the due conflict watch was not observed")
	}
	task := waitForMergeTask(t, h, fixture.host.ID)
	if err := h.c.ReportMergeAnswer(h.ctx, fixture.host.ID, task.MergeID, fixture.resolveAnswer()); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(h.ctx)
	defer cancel()
	h.c.processPendingConflictAnswers(ctx)
	if fixture.fake.commitConflicts != 1 {
		t.Fatalf("the waiting answer published %d commits, want 1", fixture.fake.commitConflicts)
	}
}

func TestTheConflictClaimHoldsUntilItIsReleased(t *testing.T) {
	h := newHarness(t)
	key := "watch|head|base|merge"
	until := h.c.Now().Add(time.Minute)
	if !h.c.claimConflict(key, until) {
		t.Fatal("the first claim was refused")
	}
	if h.c.claimConflict(key, until) {
		t.Fatal("a second claim took an incident that was still held")
	}
	if !h.c.conflictClaimed(key) {
		t.Fatal("the held incident was not reported as claimed")
	}
	h.c.releaseConflict(key)
	if h.c.conflictClaimed(key) {
		t.Fatal("a released incident was still claimed")
	}
	h.c.holdConflict(key, time.Minute)
	if !h.c.conflictClaimed(key) {
		t.Fatal("a held incident was not claimed")
	}
}

func conflictJobFor(f conflictFixture) *conflictJob {
	return &conflictJob{
		WatchID: f.watch.ID, InstallationID: f.inst.ID, Repo: f.repo, PullNumber: 1,
		HeadSHA: f.head, BaseSHA: f.base, MergeBaseSHA: f.merge,
	}
}

func prepareConflictAnswer(t *testing.T, h *harness, f conflictFixture, answer gitmerge.WorkerResponse) {
	t.Helper()
	if !h.c.observeDueEliRepairWatch(h.ctx) {
		t.Fatal("the due conflict watch was not observed")
	}
	task := waitForMergeTask(t, h, f.host.ID)
	if err := h.c.ReportMergeAnswer(h.ctx, f.host.ID, task.MergeID, answer); err != nil {
		t.Fatalf("ReportMergeAnswer: %v", err)
	}
	h.c.processConflictAnswer(h.ctx, task.MergeID)
}

func TestAConflictAnswerWithoutAResultIsLetGo(t *testing.T) {
	h := newHarness(t)
	f := newConflictFixture(t, h)
	prepareConflictAnswer(t, h, f, gitmerge.WorkerResponse{Code: "merge_failed", Error: "the merge base is missing"})
	if f.fake.commitConflicts != 0 {
		t.Fatalf("a failed worker answer published %d commits", f.fake.commitConflicts)
	}
	if h.c.conflictClaimed(conflictKey(f.watch.ID, f.head, f.base, f.merge)) {
		t.Fatal("a failed worker answer kept its claim")
	}
}

func TestAConflictAnswerForAnUnknownAttemptDoesNothing(t *testing.T) {
	h := newHarness(t)
	h.c.processConflictAnswer(h.ctx, "merge_nosuch")
}

func TestAConflictRepairStopsWhenThePullRequestMoved(t *testing.T) {
	h := newHarness(t)
	f := newConflictFixture(t, h)
	if !h.c.observeDueEliRepairWatch(h.ctx) {
		t.Fatal("the due conflict watch was not observed")
	}
	task := waitForMergeTask(t, h, f.host.ID)
	f.fake.pull.HeadSHA = strings.Repeat("d", 40)
	if err := h.c.ReportMergeAnswer(h.ctx, f.host.ID, task.MergeID, f.resolveAnswer()); err != nil {
		t.Fatal(err)
	}
	h.c.processConflictAnswer(h.ctx, task.MergeID)
	if f.fake.commitConflicts != 0 {
		t.Fatalf("a moved pull request published %d commits", f.fake.commitConflicts)
	}
}

func TestAConflictRepairStopsWhenTheProviderCannotOpen(t *testing.T) {
	h := newHarness(t)
	f := newConflictFixture(t, h)
	h.c.openProvider = func(*store.AssistantProvider, string) (assistant.Provider, error) {
		return nil, errors.New("the provider is unreachable")
	}
	prepareConflictAnswer(t, h, f, f.resolveAnswer())
	if f.fake.commitConflicts != 0 {
		t.Fatalf("a closed provider published %d commits", f.fake.commitConflicts)
	}
}

func TestAConflictRepairStopsWhenTheModelCannotResolveIt(t *testing.T) {
	h := newHarness(t)
	f := newConflictFixture(t, h)
	h.c.openProvider = func(*store.AssistantProvider, string) (assistant.Provider, error) {
		return assistant.NewFake(assistant.FakeOptions{Model: "scripted", Script: func(assistant.Request) []assistant.Event {
			return []assistant.Event{{Delta: "not a plan"}}
		}}), nil
	}
	prepareConflictAnswer(t, h, f, f.resolveAnswer())
	if f.fake.commitConflicts != 0 {
		t.Fatalf("an unusable resolution published %d commits", f.fake.commitConflicts)
	}
}

func TestAConflictResolutionTheMergeDidNotChangeIsHeld(t *testing.T) {
	h := newHarness(t)
	f := newConflictFixture(t, h)
	h.c.openProvider = func(*store.AssistantProvider, string) (assistant.Provider, error) {
		return assistant.NewFake(assistant.FakeOptions{Model: "scripted", Script: func(assistant.Request) []assistant.Event {
			return []assistant.Event{{Delta: `{"summary":"resolved elsewhere","files":[{"path":"other.txt","content":"resolved\n"}]}`}}
		}}), nil
	}
	answer := f.resolveAnswer()
	answer.Result.Conflicts = []gitmerge.Conflict{{
		Path: "other.txt", Ancestor: "a\n", Base: "b\n", Head: "c\n",
	}}
	prepareConflictAnswer(t, h, f, answer)
	if f.fake.commitConflicts != 0 {
		t.Fatalf("a resolution outside the merge published %d commits", f.fake.commitConflicts)
	}
	if !h.c.conflictClaimed(conflictKey(f.watch.ID, f.head, f.base, f.merge)) {
		t.Fatal("a resolution the merge did not change was not held")
	}
}

func TestAConflictRepairStopsWhenTheConflictIsGone(t *testing.T) {
	h := newHarness(t)
	f := newConflictFixture(t, h)
	h.c.openProvider = func(*store.AssistantProvider, string) (assistant.Provider, error) {
		return assistant.NewFake(assistant.FakeOptions{Model: "scripted", Script: func(assistant.Request) []assistant.Event {
			return []assistant.Event{{Delta: `{"summary":"keep both edits","files":[{"path":"file.txt","content":"a\nb\nc\n"}]}`}}
		}}), nil
	}
	if !h.c.observeDueEliRepairWatch(h.ctx) {
		t.Fatal("the due conflict watch was not observed")
	}
	task := waitForMergeTask(t, h, f.host.ID)
	f.fake.conflict = &github.RepairConflict{
		Number: 1, HeadSHA: f.head, BaseRef: "release", BaseSHA: f.base, MergeBaseSHA: f.merge,
		Mergeable: true, MergeableKnown: true, MergeState: "clean",
	}
	if err := h.c.ReportMergeAnswer(h.ctx, f.host.ID, task.MergeID, f.resolveAnswer()); err != nil {
		t.Fatal(err)
	}
	h.c.processConflictAnswer(h.ctx, task.MergeID)
	if f.fake.commitConflicts != 0 {
		t.Fatalf("a conflict that was gone published %d commits", f.fake.commitConflicts)
	}
}

func TestAConflictRepairStopsWhileTheControllerIsDraining(t *testing.T) {
	h := newHarness(t)
	f := newConflictFixture(t, h)
	h.c.openProvider = func(*store.AssistantProvider, string) (assistant.Provider, error) {
		return assistant.NewFake(assistant.FakeOptions{Model: "scripted", Script: func(assistant.Request) []assistant.Event {
			return []assistant.Event{{Delta: `{"summary":"keep both edits","files":[{"path":"file.txt","content":"a\nb\nc\n"}]}`}}
		}}), nil
	}
	if !h.c.observeDueEliRepairWatch(h.ctx) {
		t.Fatal("the due conflict watch was not observed")
	}
	task := waitForMergeTask(t, h, f.host.ID)
	h.c.transferDraining.Store(true)
	if err := h.c.ReportMergeAnswer(h.ctx, f.host.ID, task.MergeID, f.resolveAnswer()); err != nil {
		t.Fatal(err)
	}
	h.c.processConflictAnswer(h.ctx, task.MergeID)
	if f.fake.commitConflicts != 0 {
		t.Fatalf("a draining controller published %d commits", f.fake.commitConflicts)
	}
}

func TestAConflictPublishFailureIsLetGoForTheNextObservation(t *testing.T) {
	for name, failure := range map[string]error{
		"moved setup": github.ErrSetupConflict,
		"other":       errors.New("GitHub is not answering"),
	} {
		t.Run(name, func(t *testing.T) {
			h := newHarness(t)
			f := newConflictFixture(t, h)
			h.c.openProvider = func(*store.AssistantProvider, string) (assistant.Provider, error) {
				return assistant.NewFake(assistant.FakeOptions{Model: "scripted", Script: func(assistant.Request) []assistant.Event {
					return []assistant.Event{{Delta: `{"summary":"keep both edits","files":[{"path":"file.txt","content":"a\nb\nc\n"}]}`}}
				}}), nil
			}
			f.fake.conflictCommitErr = failure
			prepareConflictAnswer(t, h, f, f.resolveAnswer())
			if h.c.conflictClaimed(conflictKey(f.watch.ID, f.head, f.base, f.merge)) {
				t.Fatal("a publish failure kept its claim")
			}
		})
	}
}

func TestAConflictWithNoWorkerHostIsLetGo(t *testing.T) {
	h := newHarness(t)
	watch := &store.EliRepairWatch{ID: "watch_1", InstallationID: "ins_1", Repo: "acme/repo", PullNumber: 1}
	key := conflictKey(watch.ID, "head", "base", "merge")
	h.c.claimConflict(key, h.c.Now().Add(time.Minute))
	h.c.startConflictRepair(h.ctx, key, nil, watch, github.RepairPull{}, github.RepairConflict{})
	if h.c.conflictClaimed(key) {
		t.Fatal("an incident with no worker host kept its claim")
	}
}

func TestCurrentConflictAuthorityRefusesEveryChangedGate(t *testing.T) {
	newJob := func(t *testing.T) (*harness, conflictFixture, *conflictJob) {
		h := newHarness(t)
		f := newConflictFixture(t, h)
		return h, f, conflictJobFor(f)
	}
	check := func(t *testing.T, h *harness, job *conflictJob) error {
		t.Helper()
		_, _, _, _, err := h.c.currentConflictAuthority(h.ctx, job)
		return err
	}

	t.Run("watch missing", func(t *testing.T) {
		h, _, job := newJob(t)
		job.WatchID = "watch_nosuch"
		if err := check(t, h, job); err == nil {
			t.Fatal("a missing watch was accepted")
		}
	})
	t.Run("watch moved", func(t *testing.T) {
		h, _, job := newJob(t)
		job.HeadSHA = strings.Repeat("9", 40)
		if err := check(t, h, job); err == nil {
			t.Fatal("a moved watch was accepted")
		}
	})
	t.Run("policy disabled", func(t *testing.T) {
		h, f, job := newJob(t)
		if err := h.st.SetEliRepairPolicy(h.ctx, store.EliRepairPolicy{
			Repo: f.repo, InstallationID: f.inst.ID, ProviderID: f.watch.PolicyProviderID, Enabled: false, Automatic: true,
		}); err != nil {
			t.Fatal(err)
		}
		if err := check(t, h, job); err == nil {
			t.Fatal("a disabled policy was accepted")
		}
	})
	t.Run("provider changed", func(t *testing.T) {
		h, f, job := newJob(t)
		other := &store.AssistantProvider{Name: "Other", Kind: "openai", Enabled: true}
		if err := h.st.CreateAssistantProvider(h.ctx, other); err != nil {
			t.Fatal(err)
		}
		if err := h.st.SetEliRepairPolicy(h.ctx, store.EliRepairPolicy{
			Repo: f.repo, InstallationID: f.inst.ID, ProviderID: other.ID, Enabled: true, Automatic: true,
		}); err != nil {
			t.Fatal(err)
		}
		if err := check(t, h, job); err == nil {
			t.Fatal("a changed provider was accepted")
		}
	})
	t.Run("provider cannot pay", func(t *testing.T) {
		h, f, job := newJob(t)
		policy := store.EliRepairPolicy{Repo: f.repo, InstallationID: f.inst.ID, ProviderID: f.watch.PolicyProviderID, Enabled: true, Automatic: true}
		if err := h.st.SetEliRepairPolicy(h.ctx, policy); err != nil {
			t.Fatal(err)
		}
		provider, err := h.st.GetAssistantProvider(h.ctx, f.watch.PolicyProviderID)
		if err != nil {
			t.Fatal(err)
		}
		provider.Enabled = false
		if err := h.st.UpdateAssistantProvider(h.ctx, provider); err != nil {
			t.Fatal(err)
		}
		if err := check(t, h, job); err == nil {
			t.Fatal("a disabled provider was accepted")
		}
	})
}
