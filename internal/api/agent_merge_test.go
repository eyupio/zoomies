package api

import (
	"net/http"
	"strings"
	"testing"

	"github.com/eyupio/zoomies/internal/controller"
	"github.com/eyupio/zoomies/internal/gitbare"
	"github.com/eyupio/zoomies/internal/gitfetch"
	"github.com/eyupio/zoomies/internal/github"
	"github.com/eyupio/zoomies/internal/gitmerge"
	"github.com/eyupio/zoomies/internal/store"
)

func mergeCommit(t *testing.T, f *github.FakeGitHub, repo, commit string, parents []string, files map[string]string) {
	t.Helper()
	list := make([]gitbare.File, 0, len(files))
	for name, content := range files {
		list = append(list, gitbare.File{Path: name, Mode: "100644", Content: []byte(content)})
	}
	tree, err := gitbare.TreeID(t.TempDir(), list)
	if err != nil {
		t.Fatalf("hashing the fixture's files: %v", err)
	}
	f.RegisterCommit(repo, commit, tree, parents...)
	f.Archive(repo, commit, files)
}

func prepareMergeForTest(t *testing.T, h *harness) controller.MergePrepared {
	t.Helper()
	f := github.NewFake()
	t.Cleanup(f.Close)
	repo := "acme/widgets"
	ancestor := strings.Repeat("a", 40)
	base := strings.Repeat("b", 40)
	head := strings.Repeat("c", 40)
	mergeCommit(t, f, repo, ancestor, nil, map[string]string{"file.txt": "a\nb\nc\n"})
	mergeCommit(t, f, repo, base, []string{ancestor}, map[string]string{"file.txt": "a\nb\nC\n"})
	mergeCommit(t, f, repo, head, []string{ancestor}, map[string]string{"file.txt": "a\nB\nc\n"})
	pin := gitfetch.Pin{Ancestor: ancestor, Base: base, Head: head}
	prepared, err := h.ctrl.PrepareMerge(h.ctx, f.Client(repo, store.TargetRepo), repo, pin, "ghcr.io/eyupio/zoomies-git:v1")
	if err != nil {
		t.Fatalf("preparing the merge: %v", err)
	}
	return prepared
}

// The agent pulls the attempt's bytes with its three pins, then posts exactly one
// answer. A second pull or answer is refused rather than replacing the first.
func TestAnAgentPullsAndAnswersAMergeOnce(t *testing.T) {
	h := newHarness(t)
	_, token := h.agentToken("vm-merge")
	prepared := prepareMergeForTest(t, h)
	id := prepared.ID

	pull := h.do(request{method: http.MethodGet, path: "/api/v1/agent/merge/" + id, token: token})
	pull.mustStatus(t, http.StatusOK, "pulling the merge repository")
	if got := pull.header.Get("X-Zoomies-Merge-Ancestor"); got != prepared.Ancestor {
		t.Errorf("ancestor header = %q, want %q", got, prepared.Ancestor)
	}
	if got := pull.header.Get("X-Zoomies-Merge-Base"); got != prepared.Base {
		t.Errorf("base header = %q, want %q", got, prepared.Base)
	}
	if got := pull.header.Get("X-Zoomies-Merge-Head"); got != prepared.Head {
		t.Errorf("head header = %q, want %q", got, prepared.Head)
	}
	if len(pull.body) == 0 {
		t.Fatal("the pull returned no repository bytes")
	}
	if err := gitbare.Unpack(strings.NewReader(string(pull.body)), t.TempDir(), gitbare.Budget{}); err != nil {
		t.Fatalf("the pulled bytes are not a repository: %v", err)
	}

	answer := h.do(request{method: http.MethodPost, path: "/api/v1/agent/merge/" + id, token: token,
		body: gitmerge.WorkerResponse{Result: &gitmerge.Result{Tree: strings.Repeat("d", 40)}}})
	answer.mustStatus(t, http.StatusNoContent, "answering the merge")

	again := h.do(request{method: http.MethodGet, path: "/api/v1/agent/merge/" + id, token: token})
	again.mustStatus(t, http.StatusConflict, "pulling after the answer")
	second := h.do(request{method: http.MethodPost, path: "/api/v1/agent/merge/" + id, token: token,
		body: gitmerge.WorkerResponse{Result: &gitmerge.Result{Tree: strings.Repeat("e", 40)}}})
	second.mustStatus(t, http.StatusConflict, "a second answer")
}

func TestAMergeAnswerMustSayExactlyOneThing(t *testing.T) {
	h := newHarness(t)
	_, token := h.agentToken("vm-merge")
	prepared := prepareMergeForTest(t, h)
	id := prepared.ID

	empty := h.do(request{method: http.MethodPost, path: "/api/v1/agent/merge/" + id, token: token,
		body: map[string]any{}})
	empty.mustStatus(t, http.StatusBadRequest, "an answer with neither a result nor a code")

	both := h.do(request{method: http.MethodPost, path: "/api/v1/agent/merge/" + id, token: token,
		body: gitmerge.WorkerResponse{Result: &gitmerge.Result{Tree: "abc"}, Code: "merge_failed"}})
	both.mustStatus(t, http.StatusBadRequest, "an answer with both a result and a code")
}

func TestAMergePullForAnUnknownAttemptIsNotFound(t *testing.T) {
	h := newHarness(t)
	_, token := h.agentToken("vm-merge")
	resp := h.do(request{method: http.MethodGet, path: "/api/v1/agent/merge/not-an-attempt", token: token})
	resp.mustStatus(t, http.StatusNotFound, "pulling an unknown merge attempt")
}
