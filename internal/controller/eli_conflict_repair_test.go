package controller

import (
	"context"
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
