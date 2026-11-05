package controller

import (
	"errors"
	"testing"
	"time"

	"github.com/eyupio/zoomies/internal/gitfetch"
	"github.com/eyupio/zoomies/internal/gitmerge"
)

// attemptsAt gives a registry whose clock a test moves rather than waits out.
func attemptsAt(start time.Time, ttl time.Duration, max int) (*mergeAttempts, *time.Time) {
	clock := start
	attempts := newMergeAttempts(func() time.Time { return clock }, ttl, max)
	return attempts, &clock
}

// An agent that loses the reply and asks again gets the same repository, not a
// second attempt: the incident is one merge however many times it is fetched.
func TestAMergeAttemptIsFetchableUntilItIsAnswered(t *testing.T) {
	attempts, _ := attemptsAt(time.Unix(1700000000, 0), time.Minute, 2)
	id, err := attempts.add(gitfetch.Repository{Dir: t.TempDir()}, "ghcr.io/eyupio/zoomies-git:v1")
	if err != nil {
		t.Fatalf("adding an attempt: %v", err)
	}
	first, err := attempts.repository(id)
	if err != nil {
		t.Fatalf("fetching the repository: %v", err)
	}
	second, err := attempts.repository(id)
	if err != nil {
		t.Fatalf("fetching the repository again: %v", err)
	}
	if first.Dir != second.Dir {
		t.Fatalf("the second fetch named %q, not %q", second.Dir, first.Dir)
	}
}

// One merge, one answer. A second answer would be a second tree for the same
// incident, and the repair would have no way to say which it published.
func TestAMergeAttemptTakesOneAnswer(t *testing.T) {
	attempts, _ := attemptsAt(time.Unix(1700000000, 0), time.Minute, 2)
	id, err := attempts.add(gitfetch.Repository{Dir: t.TempDir()}, "image")
	if err != nil {
		t.Fatal(err)
	}
	tree := gitmerge.Result{Tree: "abc"}
	repo, err := attempts.answer(id, gitmerge.WorkerResponse{Result: &tree})
	if err != nil {
		t.Fatalf("the first answer was refused: %v", err)
	}
	if repo.Dir == "" {
		t.Fatal("the answer did not give the repository back")
	}
	if _, err := attempts.answer(id, gitmerge.WorkerResponse{Result: &tree}); !errors.Is(err, ErrMergeAttemptAnswered) {
		t.Fatalf("a second answer answered %v", err)
	}
	// Pulling the repository after answering is the same lost-reply retry, and it
	// is told what happened rather than that the attempt never existed.
	if _, err := attempts.repository(id); !errors.Is(err, ErrMergeAttemptAnswered) {
		t.Fatalf("an answered attempt answered %v", err)
	}
}

// A deadline is what stops an agent that never comes back holding a repository
// and a temporary directory for ever.
func TestAMergeAttemptExpiresAtItsDeadline(t *testing.T) {
	start := time.Unix(1700000000, 0)
	attempts, clock := attemptsAt(start, time.Minute, 2)
	repo := gitfetch.Repository{Dir: t.TempDir()}
	id, err := attempts.add(repo, "image")
	if err != nil {
		t.Fatal(err)
	}
	*clock = start.Add(30 * time.Second)
	if expired := attempts.expire(); len(expired) != 0 {
		t.Fatalf("%d attempts expired before their deadline", len(expired))
	}
	*clock = start.Add(2 * time.Minute)
	if _, err := attempts.repository(id); !errors.Is(err, ErrNoMergeAttempt) {
		t.Fatalf("an expired attempt was fetchable: %v", err)
	}
	expired := attempts.expire()
	if len(expired) != 1 || expired[0].Dir != repo.Dir {
		t.Fatalf("expiry gave %+v, want the one repository", expired)
	}
	if attempts.len() != 0 {
		t.Fatalf("%d attempts are still held", attempts.len())
	}
}

// The cap is what keeps a busy controller from holding an unbounded number of
// prepared repositories, each of which is a temporary directory and its objects.
func TestMergeAttemptsRefuseMoreThanTheirCap(t *testing.T) {
	attempts, _ := attemptsAt(time.Unix(1700000000, 0), time.Minute, 2)
	for i := 0; i < 2; i++ {
		if _, err := attempts.add(gitfetch.Repository{Dir: t.TempDir()}, "image"); err != nil {
			t.Fatalf("adding attempt %d: %v", i, err)
		}
	}
	if _, err := attempts.add(gitfetch.Repository{Dir: t.TempDir()}, "image"); !errors.Is(err, ErrMergeAttemptsFull) {
		t.Fatalf("a third attempt answered %v", err)
	}
}

// Shutdown gives everything back, so a controller that stops mid-repair does not
// leave its host's temporary directory holding the evidence.
func TestClosingMergeAttemptsGivesEverythingBack(t *testing.T) {
	attempts, _ := attemptsAt(time.Unix(1700000000, 0), time.Minute, 4)
	dirs := map[string]bool{}
	for i := 0; i < 3; i++ {
		dir := t.TempDir()
		dirs[dir] = true
		if _, err := attempts.add(gitfetch.Repository{Dir: dir}, "image"); err != nil {
			t.Fatal(err)
		}
	}
	held := attempts.close()
	if len(held) != 3 {
		t.Fatalf("closing gave back %d repositories, want 3", len(held))
	}
	for _, repo := range held {
		if !dirs[repo.Dir] {
			t.Fatalf("closing gave back %q, which it was never given", repo.Dir)
		}
	}
	if attempts.len() != 0 {
		t.Fatalf("%d attempts survived the close", attempts.len())
	}
}

// An attempt that was never here and one that has expired are the same answer,
// which is what an agent's stale retry needs to hear.
func TestAnUnknownMergeAttemptIsTheSameAnswerAsAnExpiredOne(t *testing.T) {
	attempts, _ := attemptsAt(time.Unix(1700000000, 0), time.Minute, 2)
	if _, err := attempts.repository("nosuchattempt"); !errors.Is(err, ErrNoMergeAttempt) {
		t.Fatalf("an unknown attempt answered %v", err)
	}
	if _, err := attempts.answer("nosuchattempt", gitmerge.WorkerResponse{}); !errors.Is(err, ErrNoMergeAttempt) {
		t.Fatalf("answering an unknown attempt gave %v", err)
	}
}
