package github

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/eyupio/zoomies/internal/store"
	gh "github.com/google/go-github/v88/github"
)

const publishBefore = "on: push\njobs:\n  build:\n    runs-on: ubuntu-latest\n    steps:\n      - uses: actions/checkout@aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa\n        with:\n          fetch-depth: 0\n"
const publishAfter = "on: push\njobs:\n  build:\n    runs-on: ubuntu-latest\n    steps:\n      - uses: actions/checkout@aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa\n        with:\n          fetch-depth: 1\n"

func publishFake(t *testing.T) (*FakeGitHub, *appClient, string) {
	t.Helper()
	f := NewFake()
	t.Cleanup(f.Close)
	f.AddWorkflow("acme/widgets", ".github/workflows/ci.yml", publishBefore)
	c := f.Client("acme/widgets", store.TargetRepo).(*appClient)
	ref, _, err := c.asInstallation.Git.GetRef(context.Background(), "acme", "widgets", "heads/main")
	if err != nil {
		t.Fatal(err)
	}
	return f, c, ref.GetObject().GetSHA()
}

func publishRequest(base string) OptimisationPublishRequest {
	return OptimisationPublishRequest{
		Repo: "acme/widgets", Base: "main", BaseCommit: base, Head: "zoomies/checkout-opv_test",
		Title: "Read one commit in .github/workflows/ci.yml", Body: "Review this draft. Nothing was measured or merged.",
		CommitMessage: "Read one commit in .github/workflows/ci.yml", PreviewID: "opv_test",
		Path: ".github/workflows/ci.yml", Mode: "100644", Content: publishAfter, SourceSHA256: strings.Repeat("c", 64),
	}
}

func TestPublishOptimisationOpensOneDraftAndReconcilesRetries(t *testing.T) {
	f, c, base := publishFake(t)
	req := publishRequest(base)
	pr, err := c.PublishOptimisation(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	if pr.Number != 1 || !strings.HasSuffix(pr.HTMLURL, "/acme/widgets/pull/1") || pr.Branch != req.Head {
		t.Fatalf("pull request = %+v", pr)
	}
	if branches := f.Branches("acme/widgets"); len(branches) != 2 || branches[0] != "main" || branches[1] != req.Head {
		t.Fatalf("branches = %v", branches)
	}
	// The commit is one parent on the reviewed base, and the new tree carries
	// the reviewed content.
	headRef, _, err := c.asInstallation.Git.GetRef(context.Background(), "acme", "widgets", "heads/"+req.Head)
	if err != nil {
		t.Fatal(err)
	}
	headCommit, _, err := c.asInstallation.Git.GetCommit(context.Background(), "acme", "widgets", headRef.GetObject().GetSHA())
	if err != nil || len(headCommit.Parents) != 1 || headCommit.Parents[0].GetSHA() != base {
		t.Fatalf("published commit = %+v, %v", headCommit, err)
	}
	tree, _, err := c.asInstallation.Git.GetTree(context.Background(), "acme", "widgets", headCommit.GetTree().GetSHA(), true)
	if err != nil || tree == nil {
		t.Fatal(err)
	}
	var found *gh.TreeEntry
	for _, e := range tree.Entries {
		if e.GetPath() == req.Path {
			found = e
		}
	}
	if found == nil || found.GetSHA() != contextGitBlobSHA(req.Content) || found.GetMode() != "100644" {
		t.Fatalf("published tree entry = %+v", found)
	}
	// The pull request is a draft, and the body is the reviewed one.
	repo := f.repoLocked("acme/widgets")
	if len(repo.contextGit().Pulls) != 1 || !repo.contextGit().Pulls[0].GetDraft() || repo.contextGit().Pulls[0].GetBody() != req.Body {
		t.Fatalf("pull request state = %+v", repo.contextGit().Pulls)
	}
	// A retry after an uncertain response is the same act, not a second branch
	// or a second pull request.
	again, err := c.PublishOptimisation(context.Background(), req)
	if err != nil || again.Number != pr.Number {
		t.Fatalf("reconcile = %+v, %v", again, err)
	}
	if len(f.Branches("acme/widgets")) != 2 || len(f.repoLocked("acme/widgets").contextGit().Pulls) != 1 {
		t.Fatal("reconcile duplicated the branch or pull request")
	}
}

func TestPublishOptimisationRefusesAMovedBaseOrADifferentExistingBranch(t *testing.T) {
	_, c, base := publishFake(t)
	req := publishRequest(base)
	if _, err := c.PublishOptimisation(context.Background(), req); err != nil {
		t.Fatal(err)
	}
	other := req
	other.Content = strings.Replace(publishAfter, "fetch-depth: 1", "fetch-depth: 0", 1)
	if _, err := c.PublishOptimisation(context.Background(), other); !errors.Is(err, ErrOptimisationPublishConflict) {
		t.Fatalf("a different existing branch was overwritten: %v", err)
	}
	// The branch is still the reviewed change, not the second one.
	headRef, _, err := c.asInstallation.Git.GetRef(context.Background(), "acme", "widgets", "heads/"+req.Head)
	if err != nil {
		t.Fatal(err)
	}
	headCommit, _, err := c.asInstallation.Git.GetCommit(context.Background(), "acme", "widgets", headRef.GetObject().GetSHA())
	if err != nil {
		t.Fatal(err)
	}
	tree, _, err := c.asInstallation.Git.GetTree(context.Background(), "acme", "widgets", headCommit.GetTree().GetSHA(), true)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range tree.Entries {
		if e.GetPath() == req.Path && e.GetSHA() == contextGitBlobSHA(other.Content) {
			t.Fatal("the conflicting branch was rewritten")
		}
	}
	// A base commit that is no longer the branch head is a refusal for a new
	// branch.
	moved := publishRequest(strings.Repeat("d", 40))
	moved.Head = "zoomies/checkout-opv_moved"
	if _, err := c.PublishOptimisation(context.Background(), moved); !errors.Is(err, ErrOptimisationPublishConflict) {
		t.Fatalf("a moved base was accepted: %v", err)
	}
}

func TestPublishOptimisationExplainsAMissingWorkflowPermission(t *testing.T) {
	f, c, base := publishFake(t)
	f.SetPermissions(map[string]string{"contents": "write", "pull_requests": "write", "workflows": "read"})
	_, err := c.PublishOptimisation(context.Background(), publishRequest(base))
	if !errors.Is(err, ErrForbidden) || !strings.Contains(err.Error(), "Workflows") {
		t.Fatalf("permission refusal = %v", err)
	}
}

func TestPublishOptimisationReconcilesALostPullResponseImmediately(t *testing.T) {
	f, c, base := publishFake(t)
	req := publishRequest(base)
	f.LoseNextContextPullResponse("acme/widgets")
	pr, err := c.PublishOptimisation(context.Background(), req)
	if err != nil || pr.Number != 1 || len(f.repoLocked("acme/widgets").contextGit().Pulls) != 1 {
		t.Fatalf("lost response was not reconciled: %+v, %v", pr, err)
	}
}

func TestPublishOptimisationValidatesTheReviewedChangeBeforeWriting(t *testing.T) {
	_, c, base := publishFake(t)
	for _, mode := range []string{"branch", "path", "mode", "content", "source", "title", "preview"} {
		req := publishRequest(base)
		switch mode {
		case "branch":
			req.Base = "not a branch"
		case "path":
			req.Path = ".github/workflows/nested/ci.yml"
		case "mode":
			req.Mode = "120000"
		case "content":
			req.Content = ""
		case "source":
			req.SourceSHA256 = "short"
		case "title":
			req.Title = ""
		case "preview":
			req.PreviewID = "bad id"
		}
		if _, err := c.PublishOptimisation(context.Background(), req); !errors.Is(err, ErrOptimisationPublishConflict) {
			t.Fatalf("%s: %v", mode, err)
		}
	}
}

// A response or a record lost after the branch was made, and an unrelated push
// to the base before the retry, must not strand the draft: the same reviewed
// change on an earlier base is the same act, and the retry adopts it.
func TestPublishOptimisationAdoptsItsOwnBranchAfterTheBaseMoved(t *testing.T) {
	f, c, base := publishFake(t)
	req := publishRequest(base)
	req.Body = "Read at " + base + ". Review this draft. Preview opv_test."
	first, err := c.PublishOptimisation(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	moved := f.AdvanceContextBranch("acme/widgets", "README.md", "unrelated\n")
	retry := publishRequest(moved)
	retry.Body = "Read at " + moved + ". Review this draft. Preview opv_test."
	again, err := c.PublishOptimisation(context.Background(), retry)
	if err != nil {
		t.Fatalf("the retry after the base moved was refused: %v", err)
	}
	if again.Number != first.Number || len(f.Branches("acme/widgets")) != 2 || len(f.repoLocked("acme/widgets").contextGit().Pulls) != 1 {
		t.Fatalf("retry = %+v, branches %v: it opened a second branch or pull request", again, f.Branches("acme/widgets"))
	}
}

// Adopting is for the same change only. A branch whose content differs, or a
// pull request that is not for this preview, is still preserved and refused.
func TestPublishOptimisationStillPreservesADifferentBranchAfterTheBaseMoved(t *testing.T) {
	f, c, base := publishFake(t)
	req := publishRequest(base)
	if _, err := c.PublishOptimisation(context.Background(), req); err != nil {
		t.Fatal(err)
	}
	moved := f.AdvanceContextBranch("acme/widgets", "README.md", "unrelated\n")
	other := publishRequest(moved)
	other.Content = strings.Replace(publishAfter, "fetch-depth: 1", "fetch-depth: 0", 1)
	if _, err := c.PublishOptimisation(context.Background(), other); !errors.Is(err, ErrOptimisationPublishConflict) {
		t.Fatalf("a different change was adopted: %v", err)
	}
	// The same change, but a pull request that is not for this preview.
	f.repoLocked("acme/widgets").contextGit().Pulls[0].Body = gh.Ptr("somebody else's pull request")
	retry := publishRequest(moved)
	retry.Body = "Read at " + moved + ". Review this draft."
	if _, err := c.PublishOptimisation(context.Background(), retry); !errors.Is(err, ErrOptimisationPublishConflict) {
		t.Fatalf("another pull request was adopted: %v", err)
	}
}

// An earlier base that is further back than the walk reaches is not adopted;
// the branch is preserved and the operator can look at it.
func TestPublishOptimisationDoesNotAdoptABranchFromAFarEarlierBase(t *testing.T) {
	f, c, base := publishFake(t)
	req := publishRequest(base)
	if _, err := c.PublishOptimisation(context.Background(), req); err != nil {
		t.Fatal(err)
	}
	var moved string
	for i := range maxAdoptedBaseCommits + 2 {
		moved = f.AdvanceContextBranch("acme/widgets", "README.md", strings.Repeat("x", i+1))
	}
	if _, err := c.PublishOptimisation(context.Background(), publishRequest(moved)); !errors.Is(err, ErrOptimisationPublishConflict) {
		t.Fatalf("a branch from a far earlier base was adopted: %v", err)
	}
}

type countingTransport struct {
	base  http.RoundTripper
	reads atomic.Int32
}

func (t *countingTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	if r.Method == http.MethodGet && strings.Contains(r.URL.Path, "/git/commits/") {
		t.reads.Add(1)
	}
	return t.base.RoundTrip(r)
}

// A merge brings in several parents at once. The walk is bounded by the commits
// it reads and not by how deep it goes, so a merge-heavy history is refused
// after a fixed number of requests instead of spending the installation's quota.
func TestPublishOptimisationBoundsTheCommitsItReadsInAMergeHeavyHistory(t *testing.T) {
	f, c, base := publishFake(t)
	req := publishRequest(base)
	if _, err := c.PublishOptimisation(context.Background(), req); err != nil {
		t.Fatal(err)
	}
	moved := f.AdvanceContextBranch("acme/widgets", "README.md", "x\n")
	// A binary tree of merges ten levels deep: a thousand commits, none of them
	// the branch's parent, below the current head.
	git := f.repoLocked("acme/widgets").contextGit()
	tree := git.Commits[moved].GetTree()
	var build func(level int, tag string) string
	build = func(level int, tag string) string {
		sha := blobSHA("merge:" + tag)
		commit := &gh.Commit{SHA: gh.Ptr(sha), Tree: tree}
		if level > 0 {
			commit.Parents = []*gh.Commit{{SHA: gh.Ptr(build(level-1, tag+"a"))}, {SHA: gh.Ptr(build(level-1, tag+"b"))}}
		}
		git.Commits[sha] = commit
		return sha
	}
	head := blobSHA("merge-head")
	git.Commits[head] = &gh.Commit{SHA: gh.Ptr(head), Tree: tree, Parents: []*gh.Commit{{SHA: gh.Ptr(build(10, "r"))}}}
	f.repoLocked("acme/widgets").branches["main"] = head
	f.repoLocked("acme/widgets").files["README.md"] = "x\n"

	counter := &countingTransport{base: c.asInstallation.Client().Transport}
	if counter.base == nil {
		counter.base = http.DefaultTransport
	}
	hc := *c.asInstallation.Client()
	hc.Transport = counter
	client, err := newGitHubClient(&hc, c.asInstallation.BaseURL(), c.asInstallation.UploadURL())
	if err != nil {
		t.Fatal(err)
	}
	bounded := *c
	bounded.asInstallation = client
	// pinContextSource re-pins the default branch to its source when read, so
	// the head the publisher sees is the source commit; the merge history is
	// reached from the commit the request names.
	retry := publishRequest(head)
	_, err = bounded.sameChangeOnAnEarlierBase(context.Background(), "acme", "widgets", retry, mustCommit(t, c, req))
	if err != nil {
		t.Fatal(err)
	}
	if got := counter.reads.Load(); got > int32(maxAdoptedBaseCommits)+2 {
		t.Fatalf("the walk read %d commits through a merge-heavy history, want a fixed bound near %d", got, maxAdoptedBaseCommits)
	}
}

// mustCommit reads the commit an earlier attempt left on the request's branch.
func mustCommit(t *testing.T, c *appClient, req OptimisationPublishRequest) *gh.Commit {
	t.Helper()
	ref, _, err := c.asInstallation.Git.GetRef(context.Background(), "acme", "widgets", "heads/"+req.Head)
	if err != nil {
		t.Fatal(err)
	}
	commit, _, err := c.asInstallation.Git.GetCommit(context.Background(), "acme", "widgets", ref.GetObject().GetSHA())
	if err != nil {
		t.Fatal(err)
	}
	return commit
}
