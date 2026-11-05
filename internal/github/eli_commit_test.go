package github

import (
	"context"
	"strings"
	"testing"

	"github.com/eyupio/zoomies/internal/store"
)

// The merge fetch reads a commit's tree and parents, then checks the archive
// against them. Both halves must come from the same commit, which is why this
// test registers one and reads it back through the production client.
func TestCommitOfReadsTheTreeAndParents(t *testing.T) {
	f := newFake(t)
	parent := strings.Repeat("1", 40)
	commit := strings.Repeat("2", 40)
	tree := strings.Repeat("3", 40)
	f.RegisterCommit("acme/widgets", commit, tree, parent)

	got, err := archiveClient(t, f).CommitOf(context.Background(), "", commit)
	if err != nil {
		t.Fatalf("reading the commit: %v", err)
	}
	if got.Tree != tree {
		t.Errorf("tree is %q, want %q", got.Tree, tree)
	}
	if len(got.Parents) != 1 || got.Parents[0] != parent {
		t.Errorf("parents are %v, want [%s]", got.Parents, parent)
	}
}

// A commit GitHub will not describe names the commit and the reason, because the
// operator has to know which of the three pinned commits went.
func TestCommitOfNamesACommitItCannotRead(t *testing.T) {
	f := newFake(t)
	missing := strings.Repeat("4", 40)
	_, err := archiveClient(t, f).CommitOf(context.Background(), "", missing)
	if err == nil {
		t.Fatal("a commit the fake does not have was read")
	}
	if !strings.Contains(err.Error(), missing) {
		t.Errorf("the failure does not name the commit: %v", err)
	}
}

// An installation that names an organisation has no single repository, so the
// caller must say which one rather than the client guessing.
func TestCommitOfNeedsARepositoryForAnOrganisation(t *testing.T) {
	f := newFake(t)
	c, err := NewAppFactory(f.Server().Client()).For(context.Background(),
		installationFor(f, f.URL(), store.TargetOrg, "acme"), testKey())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.CommitOf(context.Background(), "", strings.Repeat("5", 40)); err == nil {
		t.Fatal("an organisation installation read a commit from a repository nobody named")
	}
}
