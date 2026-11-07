package github

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/eyupio/zoomies/internal/gitbare"
	"github.com/eyupio/zoomies/internal/store"
)

// archiveClient is the production path: a client from the factory, holding the
// installation token and the bare transport the redirect is fetched with.
func archiveClient(t *testing.T, f *FakeGitHub) Client {
	t.Helper()
	c, err := NewAppFactory(f.Server().Client()).For(context.Background(),
		installationFor(f, f.URL(), store.TargetRepo, "acme/widgets"), testKey())
	if err != nil {
		t.Fatalf("building the client: %v", err)
	}
	return c
}

// The controller builds the merge worker's repository from exactly this: one
// commit's files, with the wrapper directory GitHub puts round them removed.
func TestArchiveOfReturnsTheCommitTree(t *testing.T) {
	f := newFake(t)
	f.Archive("acme/widgets", "abc1234", map[string]string{
		"README.md":   "hello\n",
		"bin/run.sh":  "#!/bin/sh\necho hi\n",
		"src/main.go": "package main\n",
	})
	files, err := archiveClient(t, f).ArchiveOf(context.Background(), "", "abc1234", gitbare.ArchiveOptions{StripTopLevel: true})
	if err != nil {
		t.Fatalf("reading the archive: %v", err)
	}
	got := map[string]gitbare.File{}
	for _, file := range files {
		got[file.Path] = file
	}
	if len(got) != 3 {
		t.Fatalf("files %+v", files)
	}
	if string(got["README.md"].Content) != "hello\n" {
		t.Errorf("README.md is %q", got["README.md"].Content)
	}
	if got["bin/run.sh"].Mode != "100755" {
		t.Errorf("bin/run.sh has mode %q, want 100755", got["bin/run.sh"].Mode)
	}
	if got["src/main.go"].Mode != "100644" {
		t.Errorf("src/main.go has mode %q, want 100644", got["src/main.go"].Mode)
	}
}

// The archive endpoint redirects to storage. The token must not travel with that
// redirect, and the fake refuses a storage request that carries one, so this
// asserts both the behaviour and the intent.
func TestArchiveOfSendsNoCredentialToStorage(t *testing.T) {
	f := newFake(t)
	f.Archive("acme/widgets", "abc1234", map[string]string{"README.md": "hello\n"})
	if _, err := archiveClient(t, f).ArchiveOf(context.Background(), "", "abc1234", gitbare.ArchiveOptions{StripTopLevel: true}); err != nil {
		t.Fatalf("reading the archive: %v", err)
	}
	seen := f.ArchiveTokensSeen("acme/widgets", "abc1234")
	if len(seen) == 0 {
		t.Fatal("the storage was never fetched, so this test proves nothing")
	}
	for _, header := range seen {
		if header != "" {
			t.Fatalf("a storage request carried %q; the installation token must stay on the API host", header)
		}
	}
}

// A commit the provider will not hand over names itself in the error, because
// the operator has to know which of the three pinned commits went.
func TestArchiveOfNamesTheCommitItCouldNotRead(t *testing.T) {
	f := newFake(t)
	f.ArchiveStatus("acme/widgets", "deadbeef", 404)
	_, err := archiveClient(t, f).ArchiveOf(context.Background(), "", "deadbeef", gitbare.ArchiveOptions{})
	if err == nil {
		t.Fatal("a missing archive was read")
	}
	var archiveErr *ArchiveError
	if !errors.As(err, &archiveErr) {
		t.Fatalf("the failure is %v, not an *ArchiveError", err)
	}
	if archiveErr.Commit != "deadbeef" || archiveErr.Status != 404 {
		t.Fatalf("the failure is %+v", archiveErr)
	}
	if !strings.Contains(err.Error(), "deadbeef") || !strings.Contains(err.Error(), "404") {
		t.Errorf("the message does not say which commit and what happened: %v", err)
	}
}

// The decompressed budget belongs to the caller, and an archive past it is
// refused rather than truncated into a repository that disagrees with GitHub.
func TestArchiveOfHonoursTheCallersBudget(t *testing.T) {
	f := newFake(t)
	f.Archive("acme/widgets", "abc1234", map[string]string{
		"big.txt": strings.Repeat("a", 4096),
	})
	_, err := archiveClient(t, f).ArchiveOf(context.Background(), "", "abc1234", gitbare.ArchiveOptions{
		StripTopLevel: true,
		Budget:        gitbare.Budget{Bytes: 128},
	})
	if !errors.Is(err, gitbare.ErrArchive) {
		t.Fatalf("an archive over the budget answered %v", err)
	}
}

// An installation that names an organisation has no single repository, so the
// caller must say which one rather than the client guessing.
func TestArchiveOfNeedsARepositoryForAnOrganisation(t *testing.T) {
	f := newFake(t)
	c, err := NewAppFactory(f.Server().Client()).For(context.Background(),
		installationFor(f, f.URL(), store.TargetOrg, "acme"), testKey())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.ArchiveOf(context.Background(), "", "abc1234", gitbare.ArchiveOptions{}); err == nil {
		t.Fatal("an organisation installation archived a repository nobody named")
	}
}

// A GHES instance that serves the archive from the API host answers the
// authenticated request with the archive. Asking again without the token, as the
// redirect path does, is a 404 for a private repository.
func TestArchiveOfReadsAnArchiveTheAPIServedItself(t *testing.T) {
	f := newFake(t)
	f.Archive("acme/widgets", "abc1234", map[string]string{"README.md": "hello\n"})
	f.ArchiveServedByTheAPI("acme/widgets", "abc1234")
	files, err := archiveClient(t, f).ArchiveOf(context.Background(), "", "abc1234", gitbare.ArchiveOptions{StripTopLevel: true})
	if err != nil {
		t.Fatalf("reading the archive: %v", err)
	}
	if len(files) != 1 || string(files[0].Content) != "hello\n" {
		t.Fatalf("files %+v", files)
	}
}
