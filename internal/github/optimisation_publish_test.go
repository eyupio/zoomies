package github

import (
	"context"
	"errors"
	"strings"
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
	for i := range maxAdoptedBaseDepth + 2 {
		moved = f.AdvanceContextBranch("acme/widgets", "README.md", strings.Repeat("x", i+1))
	}
	if _, err := c.PublishOptimisation(context.Background(), publishRequest(moved)); !errors.Is(err, ErrOptimisationPublishConflict) {
		t.Fatalf("a branch from a far earlier base was adopted: %v", err)
	}
}
