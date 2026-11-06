package controller

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/eyupio/zoomies/internal/gitbare"
	"github.com/eyupio/zoomies/internal/gitfetch"
	"github.com/eyupio/zoomies/internal/github"
	"github.com/eyupio/zoomies/internal/gitmerge"
	"github.com/eyupio/zoomies/internal/store"
)

// mergeFixture registers one commit with the fake GitHub whose tree and archive
// agree by construction: the tree id is hashed from the very files the archive
// serves, so nothing in the fixture can disagree with itself.
func mergeFixture(t *testing.T, f *github.FakeGitHub, repo, commit string, parents []string, files map[string]string) {
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

// The whole controller-side fetch, end to end: three pinned commits read from
// GitHub one call at a time, written into a repository, and merged by the worker
// into the conflict the repair is made from.
func TestMergeRepositoryBuildsWhatTheWorkerMerges(t *testing.T) {
	f := github.NewFake()
	t.Cleanup(f.Close)
	repo := "acme/widgets"
	ancestor := strings.Repeat("a", 40)
	base := strings.Repeat("b", 40)
	head := strings.Repeat("c", 40)
	mergeFixture(t, f, repo, ancestor, nil, map[string]string{"file.txt": "a\nb\nc\n"})
	mergeFixture(t, f, repo, base, []string{ancestor}, map[string]string{"file.txt": "a\nb\nC\n"})
	mergeFixture(t, f, repo, head, []string{ancestor}, map[string]string{"file.txt": "a\nB\nc\n"})

	built, err := mergeRepository(context.Background(), f.Client(repo, store.TargetRepo), repo,
		gitfetch.Pin{Ancestor: ancestor, Base: base, Head: head})
	if err != nil {
		t.Fatalf("preparing the repository: %v", err)
	}
	t.Cleanup(func() { _ = discardMergeRepository(built) })

	result, err := gitmerge.Merge(context.Background(), gitmerge.Request{
		Dir:      built.Dir,
		Ancestor: built.Ancestor,
		Base:     built.Base,
		Head:     built.Head,
	})
	if err != nil {
		t.Fatalf("the worker refused the prepared repository: %v", err)
	}
	if len(result.Conflicts) != 1 {
		t.Fatalf("merging what the controller prepared gave %+v", result)
	}
	conflict := result.Conflicts[0]
	if conflict.Path != "file.txt" || conflict.Ancestor != "a\nb\nc\n" || conflict.Base != "a\nb\nC\n" || conflict.Head != "a\nB\nc\n" {
		t.Fatalf("conflict %+v", conflict)
	}
}

// A failed fetch leaves nothing behind: the repository is removed on every path
// out, or a controller that keeps failing would keep filling its host.
func TestMergeRepositoryRemovesWhatItBuiltWhenTheFetchFails(t *testing.T) {
	f := github.NewFake()
	t.Cleanup(f.Close)
	repo := "acme/widgets"
	ancestor := strings.Repeat("a", 40)
	mergeFixture(t, f, repo, ancestor, nil, map[string]string{"file.txt": "a\nb\nc\n"})
	// The target tip is never registered, so the fetch cannot finish.
	_, err := mergeRepository(context.Background(), f.Client(repo, store.TargetRepo), repo,
		gitfetch.Pin{Ancestor: ancestor, Base: strings.Repeat("b", 40), Head: strings.Repeat("c", 40)})
	if err == nil {
		t.Fatal("a fetch with a missing commit succeeded")
	}
	entries, readErr := os.ReadDir(os.TempDir())
	if readErr != nil {
		t.Fatal(readErr)
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), "zoomies-merge-") {
			t.Fatalf("the failed fetch left %s behind", entry.Name())
		}
	}
}

// A pin that is not three commit ids is refused before GitHub is asked anything,
// because a watch row that names a branch is a row no repair can trust.
func TestMergeRepositoryRefusesAPinThatIsNotCommitIDs(t *testing.T) {
	f := github.NewFake()
	t.Cleanup(f.Close)
	_, err := mergeRepository(context.Background(), f.Client("acme/widgets", store.TargetRepo), "acme/widgets",
		gitfetch.Pin{Ancestor: "main", Base: strings.Repeat("b", 40), Head: strings.Repeat("c", 40)})
	if !errors.Is(err, gitfetch.ErrUnpinned) {
		t.Fatalf("a pin naming a branch answered %v", err)
	}
}
