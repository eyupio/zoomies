package controller

import (
	"bytes"
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/eyupio/zoomies/internal/gitbare"
	"github.com/eyupio/zoomies/internal/gitfetch"
	"github.com/eyupio/zoomies/internal/github"
	"github.com/eyupio/zoomies/internal/gitmerge"
	"github.com/eyupio/zoomies/internal/store"
)

// mergeSessionFixture is one incident registered in the fake GitHub, ready to be
// prepared and merged: the ancestor, the target tip and the head, each with the
// files its archive serves and the tree those files hash to.
type mergeSessionFixture struct {
	fake     *github.FakeGitHub
	client   github.Client
	repo     string
	pin      gitfetch.Pin
	session  *mergeSession
	attempts *mergeAttempts
}

func newMergeSessionFixture(t *testing.T) mergeSessionFixture {
	t.Helper()
	f := github.NewFake()
	t.Cleanup(f.Close)
	repo := "acme/widgets"
	ancestor := strings.Repeat("a", 40)
	base := strings.Repeat("b", 40)
	head := strings.Repeat("c", 40)
	mergeFixture(t, f, repo, ancestor, nil, map[string]string{"file.txt": "a\nb\nc\n"})
	mergeFixture(t, f, repo, base, []string{ancestor}, map[string]string{"file.txt": "a\nb\nC\n"})
	mergeFixture(t, f, repo, head, []string{ancestor}, map[string]string{"file.txt": "a\nB\nc\n"})
	attempts, _ := attemptsAt(time.Unix(1700000000, 0), time.Minute, 4)
	return mergeSessionFixture{
		fake:     f,
		client:   f.Client(repo, store.TargetRepo),
		repo:     repo,
		pin:      gitfetch.Pin{Ancestor: ancestor, Base: base, Head: head},
		session:  newMergeSession(attempts),
		attempts: attempts,
	}
}

// The controller's whole transport surface in one test: prepare an incident, hand
// its bytes out, take the one answer and leave nothing behind.
func TestAMergeSessionPreparesAnswersAndCleansUp(t *testing.T) {
	fixture := newMergeSessionFixture(t)
	prepared, err := fixture.session.prepare(context.Background(), fixture.client, fixture.repo, fixture.pin, "ghcr.io/eyupio/zoomies-git:v1")
	if err != nil {
		t.Fatalf("preparing the merge: %v", err)
	}
	if prepared.ID == "" || prepared.Image == "" || len(prepared.Repository) == 0 {
		t.Fatalf("prepared %+v", prepared)
	}
	if fixture.attempts.len() != 1 {
		t.Fatalf("%d attempts are held, want 1", fixture.attempts.len())
	}

	// The agent unpacks what it was given and merges in it, exactly as the worker
	// will, so the bytes are checked by the thing that consumes them.
	workspace := t.TempDir()
	if err := gitbare.Unpack(bytes.NewReader(prepared.Repository), workspace, gitbare.Budget{}); err != nil {
		t.Fatalf("unpacking what the controller handed out: %v", err)
	}
	held, err := fixture.attempts.repository(prepared.ID)
	if err != nil {
		t.Fatalf("reading the attempt: %v", err)
	}
	merged, err := gitmerge.Merge(context.Background(), gitmerge.Request{
		Dir:      workspace,
		Ancestor: held.Ancestor,
		Base:     held.Base,
		Head:     held.Head,
	})
	if err != nil {
		t.Fatalf("the worker refused what the controller handed out: %v", err)
	}
	if len(merged.Conflicts) != 1 || merged.Conflicts[0].Path != "file.txt" {
		t.Fatalf("merging what the controller handed out gave %+v", merged)
	}

	answer, err := fixture.session.accept(prepared.ID, gitmerge.WorkerResponse{Result: &merged})
	if err != nil {
		t.Fatalf("accepting the answer: %v", err)
	}
	if answer.Result == nil || answer.Result.Tree != merged.Tree {
		t.Fatalf("the accepted answer is %+v", answer)
	}
	if _, statErr := os.Stat(held.Dir); !os.IsNotExist(statErr) {
		t.Fatalf("the repository is still there after the answer: %v", statErr)
	}
}

// An answered attempt is not served again, and its repository is already gone: the
// bytes live in the answer, not on the controller's disk.
func TestAnAnsweredMergeSessionServesNothing(t *testing.T) {
	fixture := newMergeSessionFixture(t)
	prepared, err := fixture.session.prepare(context.Background(), fixture.client, fixture.repo, fixture.pin, "image")
	if err != nil {
		t.Fatal(err)
	}
	tree := "abc"
	if _, err := fixture.session.accept(prepared.ID, gitmerge.WorkerResponse{Result: &gitmerge.Result{Tree: tree}}); err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.session.repository(context.Background(), prepared.ID); !errors.Is(err, ErrMergeAttemptAnswered) {
		t.Fatalf("an answered attempt served %v", err)
	}
}

// A repair that is paused or superseded before any agent answers gives its
// repository back, so a pause costs the host nothing.
func TestAnAbandonedMergeSessionRemovesItsRepository(t *testing.T) {
	fixture := newMergeSessionFixture(t)
	prepared, err := fixture.session.prepare(context.Background(), fixture.client, fixture.repo, fixture.pin, "image")
	if err != nil {
		t.Fatal(err)
	}
	held, err := fixture.attempts.repository(prepared.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := fixture.session.abandon(prepared.ID); err != nil {
		t.Fatalf("abandoning the attempt: %v", err)
	}
	if _, statErr := os.Stat(held.Dir); !os.IsNotExist(statErr) {
		t.Fatalf("abandoning left the repository behind: %v", statErr)
	}
	if fixture.attempts.len() != 0 {
		t.Fatalf("%d attempts are still held", fixture.attempts.len())
	}
	if err := fixture.session.abandon(prepared.ID); !errors.Is(err, ErrNoMergeAttempt) {
		t.Fatalf("abandoning twice answered %v", err)
	}
}

// A pin the watch could not have pinned is refused before an attempt exists, so
// there is nothing to clean up and nothing an agent could be told to fetch.
func TestAMergeSessionPreparesNoAttemptForABadPin(t *testing.T) {
	fixture := newMergeSessionFixture(t)
	bad := fixture.pin
	bad.Head = "main"
	if _, err := fixture.session.prepare(context.Background(), fixture.client, fixture.repo, bad, "image"); !errors.Is(err, gitfetch.ErrUnpinned) {
		t.Fatalf("a pin naming a branch answered %v", err)
	}
	if fixture.attempts.len() != 0 {
		t.Fatalf("%d attempts were held for a pin that was refused", fixture.attempts.len())
	}
}
