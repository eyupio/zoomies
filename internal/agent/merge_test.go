package agent

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/eyupio/zoomies/internal/backend"
	"github.com/eyupio/zoomies/internal/gitbare"
	"github.com/eyupio/zoomies/internal/gitmerge"
	"github.com/eyupio/zoomies/internal/store"
)

// mergePack builds the packed repository a merge task pulls, with one commit
// whose id stands in for all three pinned sides: the fake backend answers
// without merging, so the ids only have to be the ones the task carries.
func mergePack(t *testing.T) ([]byte, string) {
	t.Helper()
	dir := t.TempDir()
	ids, err := gitbare.Write(dir, []gitbare.Commit{{
		Files: []gitbare.File{{Path: "file.txt", Mode: "100644", Content: []byte("hello\n")}},
	}})
	if err != nil {
		t.Fatalf("writing the fixture repository: %v", err)
	}
	packed, err := gitbare.Pack(context.Background(), dir, 0)
	if err != nil {
		t.Fatalf("packing the fixture repository: %v", err)
	}
	return packed, ids[0]
}

func mergeTask(id, image, pin string) Task {
	return Task{
		ID: "task-" + id, Kind: TaskMergeConflict, MergeID: id, MergeImage: image,
		MergeAncestor: pin, MergeBase: pin, MergeHead: pin,
	}
}

func mergeResult(t *testing.T, tr *fakeTransport) TaskResult {
	t.Helper()
	select {
	case res := <-tr.results:
		return res
	default:
		t.Fatal("the agent reported no task result")
		return TaskResult{}
	}
}

// The whole agent half: pull the prepared repository, write the worker's
// request beside it, run the one-shot worker and post the answer it wrote.
func TestAMergeConflictPullsUnpacksRunsAndAnswers(t *testing.T) {
	a, tr, be, _ := newAgent(t, 1)
	packed, pin := mergePack(t)
	tr.merges["merge-1"] = MergeRepository{Ancestor: pin, Base: pin, Head: pin, Repository: packed}
	be.mergeAnswer = []byte(`{"result":{"tree":"` + strings.Repeat("d", 40) + `"}}`)

	a.handleMergeConflict(context.Background(), mergeTask("merge-1", "ghcr.io/eyupio/zoomies-git:v1", pin))

	res := mergeResult(t, tr)
	if !res.OK || res.Kind != TaskMergeConflict || res.RunnerID != "" {
		t.Fatalf("task result = %+v", res)
	}
	if be.mergeCalls != 1 || be.mergeImage != "ghcr.io/eyupio/zoomies-git:v1" {
		t.Fatalf("backend calls = %d, image = %q", be.mergeCalls, be.mergeImage)
	}
	var request gitmerge.WorkerRequest
	if err := json.Unmarshal(be.mergeRequest, &request); err != nil {
		t.Fatalf("the request written for the worker: %v (%q)", err, be.mergeRequest)
	}
	if request.Dir != "/work/repository" || request.Ancestor != pin || request.Base != pin || request.Head != pin {
		t.Fatalf("worker request = %+v", request)
	}
	answer, ok := tr.answers["merge-1"]
	if !ok || answer.Result == nil || answer.Result.Tree != strings.Repeat("d", 40) {
		t.Fatalf("posted answer = %+v", answer)
	}
}

// The task and the prepared repository must name the same three commits. A
// controller that mixed two incidents would otherwise merge one into the other.
func TestAMergeConflictRefusesAPinTheRepositoryDoesNotHold(t *testing.T) {
	a, tr, be, _ := newAgent(t, 1)
	packed, pin := mergePack(t)
	other := strings.Repeat("e", 40)
	tr.merges["merge-1"] = MergeRepository{Ancestor: other, Base: other, Head: other, Repository: packed}
	be.mergeAnswer = []byte(`{"result":{"tree":"abc"}}`)

	a.handleMergeConflict(context.Background(), mergeTask("merge-1", "image", pin))

	res := mergeResult(t, tr)
	if res.OK || be.mergeCalls != 0 {
		t.Fatalf("a mismatched pin ran the worker: result %+v, calls %d", res, be.mergeCalls)
	}
	if len(tr.answers) != 0 {
		t.Fatalf("a mismatched pin posted an answer: %+v", tr.answers)
	}
}

func TestAMergeConflictRefusesAnUnreadableWorkerAnswer(t *testing.T) {
	a, tr, be, _ := newAgent(t, 1)
	packed, pin := mergePack(t)
	tr.merges["merge-1"] = MergeRepository{Ancestor: pin, Base: pin, Head: pin, Repository: packed}
	be.mergeAnswer = []byte("this is not JSON")

	a.handleMergeConflict(context.Background(), mergeTask("merge-1", "image", pin))

	res := mergeResult(t, tr)
	if res.OK {
		t.Fatalf("an unreadable answer was reported as success: %+v", res)
	}
	if len(tr.answers) != 0 {
		t.Fatalf("an unreadable answer was posted: %+v", tr.answers)
	}
}

// A redelivery after the controller already took the answer is not a failure:
// the work is recorded and the agent only has to stop.
func TestAMergeConflictTreatsAnAlreadyAnsweredAttemptAsDone(t *testing.T) {
	a, tr, be, _ := newAgent(t, 1)
	packed, pin := mergePack(t)
	tr.merges["merge-1"] = MergeRepository{Ancestor: pin, Base: pin, Head: pin, Repository: packed}
	be.mergeAnswer = []byte(`{"result":{"tree":"abc"}}`)
	tr.answerErr = &HTTPError{Status: 409, Method: "POST", Path: "/api/v1/agent/merge/merge-1"}

	a.handleMergeConflict(context.Background(), mergeTask("merge-1", "image", pin))

	res := mergeResult(t, tr)
	if !res.OK {
		t.Fatalf("a lost reply was reported as a failure: %+v", res)
	}
}

// A worker that answered with an error is not a task failure the controller
// should retry: the answer is recorded, and the operator reads its sentence.
func TestAMergeConflictPostsAWorkerErrorAnswerAndReportsIt(t *testing.T) {
	a, tr, be, _ := newAgent(t, 1)
	packed, pin := mergePack(t)
	tr.merges["merge-1"] = MergeRepository{Ancestor: pin, Base: pin, Head: pin, Repository: packed}
	be.mergeAnswer = []byte(`{"code":"merge_failed","error":"the repository has no merge base"}`)

	a.handleMergeConflict(context.Background(), mergeTask("merge-1", "image", pin))

	answer, ok := tr.answers["merge-1"]
	if !ok || answer.Code != "merge_failed" {
		t.Fatalf("posted answer = %+v", answer)
	}
	res := mergeResult(t, tr)
	if res.OK || !strings.Contains(res.Error, "merge_failed") {
		t.Fatalf("task result = %+v", res)
	}
}

func TestAMergeConflictWithoutACapableBackendIsReported(t *testing.T) {
	a, tr, _, _ := newAgent(t, 1)
	packed, pin := mergePack(t)
	tr.merges["merge-1"] = MergeRepository{Ancestor: pin, Base: pin, Head: pin, Repository: packed}
	// The process backend runs no containers, so it cannot carry the worker.
	a.opts.Backends = backendRegistryWithoutMergeWorker(t)
	task := mergeTask("merge-1", "image", pin)
	task.Backend = store.BackendProcess

	a.handleMergeConflict(context.Background(), task)

	res := mergeResult(t, tr)
	if res.OK || !strings.Contains(res.Error, "cannot run the merge worker") {
		t.Fatalf("task result = %+v", res)
	}
}

// TestAMergeConflictRemovesItsWorkspaceWhateverHappened is covered by the
// handler's deferred cleanup, but this asserts the failure path too: the
// backend records the workspace it was given, and nothing of it is left after
// either a good or a bad answer.
func TestAMergeConflictRemovesItsWorkspace(t *testing.T) {
	a, tr, be, _ := newAgent(t, 1)
	packed, pin := mergePack(t)
	tr.merges["merge-1"] = MergeRepository{Ancestor: pin, Base: pin, Head: pin, Repository: packed}
	be.mergeAnswer = []byte("not JSON")

	a.handleMergeConflict(context.Background(), mergeTask("merge-1", "image", pin))
	_ = mergeResult(t, tr)
	if be.mergeWorkspace == "" {
		t.Fatal("the backend was not given a workspace")
	}
	if _, err := os.Stat(be.mergeWorkspace); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("the merge workspace is still there: %v", err)
	}
}

func backendRegistryWithoutMergeWorker(t *testing.T) *backend.Registry {
	t.Helper()
	// The process backend is the real one that runs no containers; using it
	// keeps the refusal the same one a host without a container backend gets.
	b, err := backend.NewProcess(backend.ProcessOptions{WorkDir: t.TempDir(), Logger: testLogger()})
	if err != nil {
		t.Fatalf("NewProcess: %v", err)
	}
	return backend.NewRegistry(b)
}

// A merge task reaches the agent's own handler without taking a runner's
// capacity slot or being refused on a cordoned host.
func TestAMergeTaskIsDispatchedBypassingRunnerAdmission(t *testing.T) {
	h := newHarness(t, 1)
	packed, pin := mergePack(t)
	h.tr.merges["merge-1"] = MergeRepository{Ancestor: pin, Base: pin, Head: pin, Repository: packed}
	h.be.mergeAnswer = []byte(`{"result":{"tree":"abc"}}`)

	h.tr.tasks <- []Task{mergeTask("merge-1", "image", pin)}

	res := h.nextResult()
	if !res.OK || res.Kind != TaskMergeConflict {
		t.Fatalf("task result = %+v", res)
	}
	if answer := h.tr.answers["merge-1"]; answer.Result == nil {
		t.Fatalf("answer = %+v", answer)
	}
}

func TestAMergeUsesAContainerBackendWhenTheDefaultRunsProcesses(t *testing.T) {
	a, tr, be, _ := newAgent(t, 1)
	process, err := backend.NewProcess(backend.ProcessOptions{WorkDir: t.TempDir(), Logger: testLogger()})
	if err != nil {
		t.Fatal(err)
	}
	a.opts.Backends = backend.NewRegistry(process, be)
	a.opts.DefaultBackend = store.BackendProcess
	if !a.canRunMergeWorker() {
		t.Fatal("the registered worker was not advertised")
	}
	packed, pin := mergePack(t)
	tr.merges["merge-1"] = MergeRepository{Ancestor: pin, Base: pin, Head: pin, Repository: packed}
	be.mergeAnswer = []byte(`{"result":{"tree":"abc"}}`)
	a.handleMergeConflict(t.Context(), mergeTask("merge-1", "image", pin))
	if res := mergeResult(t, tr); !res.OK || be.mergeCalls != 1 {
		t.Fatalf("result %+v, calls %d", res, be.mergeCalls)
	}
}

func TestAMergeConflictReportsAPullFailure(t *testing.T) {
	a, tr, be, _ := newAgent(t, 1)
	_, pin := mergePack(t)
	tr.pullErr = errors.New("the controller is unreachable")
	a.handleMergeConflict(context.Background(), mergeTask("merge-1", "image", pin))
	res := mergeResult(t, tr)
	if res.OK || !strings.Contains(res.Error, "could not pull the repository") || be.mergeCalls != 0 {
		t.Fatalf("result = %+v, calls = %d", res, be.mergeCalls)
	}
}

func TestAMergeConflictReportsAWorkspaceItCannotMake(t *testing.T) {
	a, tr, _, _ := newAgent(t, 1)
	packed, pin := mergePack(t)
	tr.merges["merge-1"] = MergeRepository{Ancestor: pin, Base: pin, Head: pin, Repository: packed}
	// A file where the workspace directory should be makes MkdirTemp fail
	// without touching anything else on the host.
	notADir := filepath.Join(t.TempDir(), "workspace")
	if err := os.WriteFile(notADir, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	a.opts.WorkDir = notADir
	a.handleMergeConflict(context.Background(), mergeTask("merge-1", "image", pin))
	res := mergeResult(t, tr)
	if res.OK || !strings.Contains(res.Error, "could not make a workspace") {
		t.Fatalf("result = %+v", res)
	}
}

func TestAMergeConflictRefusesAPackItCannotUnpack(t *testing.T) {
	a, tr, _, _ := newAgent(t, 1)
	_, pin := mergePack(t)
	tr.merges["merge-1"] = MergeRepository{Ancestor: pin, Base: pin, Head: pin, Repository: []byte("not a tarball")}
	a.handleMergeConflict(context.Background(), mergeTask("merge-1", "image", pin))
	res := mergeResult(t, tr)
	if res.OK || !strings.Contains(res.Error, "could not be unpacked") {
		t.Fatalf("result = %+v", res)
	}
}

func TestAMergeConflictReportsAWorkerThatDidNotAnswer(t *testing.T) {
	a, tr, be, _ := newAgent(t, 1)
	packed, pin := mergePack(t)
	tr.merges["merge-1"] = MergeRepository{Ancestor: pin, Base: pin, Head: pin, Repository: packed}
	be.mergeErr = errors.New("the container exited 137")
	a.handleMergeConflict(context.Background(), mergeTask("merge-1", "image", pin))
	res := mergeResult(t, tr)
	if res.OK || !strings.Contains(res.Error, "did not answer") {
		t.Fatalf("result = %+v", res)
	}
}

func TestAMergeConflictReportsAnAnswerTheControllerRejected(t *testing.T) {
	a, tr, be, _ := newAgent(t, 1)
	packed, pin := mergePack(t)
	tr.merges["merge-1"] = MergeRepository{Ancestor: pin, Base: pin, Head: pin, Repository: packed}
	be.mergeAnswer = []byte(`{"result":{"tree":"abc"}}`)
	tr.answerErr = &HTTPError{Status: 500, Method: "POST", Path: "/api/v1/agent/merge/merge-1"}
	a.handleMergeConflict(context.Background(), mergeTask("merge-1", "image", pin))
	res := mergeResult(t, tr)
	if res.OK || !strings.Contains(res.Error, "did not accept the merge answer") {
		t.Fatalf("result = %+v", res)
	}
}

func TestAMergeConflictReportsABackendNameThisHostDoesNotHave(t *testing.T) {
	a, tr, _, _ := newAgent(t, 1)
	packed, pin := mergePack(t)
	tr.merges["merge-1"] = MergeRepository{Ancestor: pin, Base: pin, Head: pin, Repository: packed}
	task := mergeTask("merge-1", "image", pin)
	task.Backend = "not-registered"
	a.handleMergeConflict(context.Background(), task)
	res := mergeResult(t, tr)
	if res.OK || !strings.Contains(res.Error, "has no not-registered backend") {
		t.Fatalf("result = %+v", res)
	}
}

func TestDecodeMergeAnswerRefusesEverythingButOneAnswer(t *testing.T) {
	for name, raw := range map[string]string{
		"unknown field": `{"result":{"tree":"x"},"surprise":true}`,
		"two answers":   `{"result":{"tree":"x"}}{"result":{"tree":"y"}}`,
		"neither":       `{}`,
		"both":          `{"result":{"tree":"x"},"code":"merge_failed"}`,
	} {
		if _, err := decodeMergeAnswer([]byte(raw)); err == nil {
			t.Errorf("%s: %s was accepted", name, raw)
		}
	}
}
