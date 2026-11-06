package controller

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/eyupio/zoomies/internal/gitfetch"
	"github.com/eyupio/zoomies/internal/gitmerge"
)

var (
	// ErrNoMergeAttempt says the attempt an agent asked about is not here: it was
	// answered, it expired, or it was never this controller's.
	ErrNoMergeAttempt = errors.New("controller: no such merge attempt")
	// ErrMergeAttemptsFull says this controller is holding as many prepared
	// repositories as it will.
	ErrMergeAttemptsFull = errors.New("controller: too many merge attempts are in flight")
	// ErrMergeAttemptAnswered says this attempt already has its one answer. A
	// merge is one incident, and a second answer would be a second tree for it.
	ErrMergeAttemptAnswered = errors.New("controller: this merge attempt already answered")
)

// mergeAttempt is one prepared repository waiting for an agent to merge it.
//
// Decision 0018 has an agent run the worker, so the controller keeps the
// repository it built until that agent has the answer or the deadline passes. The
// attempt is the unit both the pull and the answer are keyed by.
type mergeAttempt struct {
	id       string
	repo     gitfetch.Repository
	image    string
	job      *conflictJob
	deadline time.Time
	answer   *gitmerge.WorkerResponse
	// handled says the answer has been taken for resolution. A duplicate
	// answer still receives "already answered"; the work is not repeated.
	handled bool
}

// mergeAttempts holds the attempts in flight and owns their repositories.
//
// Everything it holds is a temporary directory on this host, so it has three ways
// to let one go: an answer, a deadline, and shutdown. Each of them removes the
// directory, because a controller that leaked one per conflict would fill its own
// disk with the evidence of repairs it had already given up on.
type mergeAttempts struct {
	mu   sync.Mutex
	now  func() time.Time
	ttl  time.Duration
	max  int
	byID map[string]*mergeAttempt
}

// newMergeAttempts holds at most max prepared repositories for ttl each. The
// clock is injected for the same reason the store's is: an expiry a test cannot
// reach without waiting is an expiry nobody tests.
func newMergeAttempts(now func() time.Time, ttl time.Duration, max int) *mergeAttempts {
	if now == nil {
		now = time.Now
	}
	if max <= 0 {
		max = 8
	}
	if ttl <= 0 {
		ttl = 10 * time.Minute
	}
	return &mergeAttempts{now: now, ttl: ttl, max: max, byID: map[string]*mergeAttempt{}}
}

// add takes ownership of a prepared repository and returns the id an agent is
// told to fetch it by.
func (m *mergeAttempts) add(repo gitfetch.Repository, image string, job ...*conflictJob) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.byID) >= m.max {
		return "", ErrMergeAttemptsFull
	}
	id, err := newMergeAttemptID()
	if err != nil {
		return "", err
	}
	attempt := &mergeAttempt{
		id:       id,
		repo:     repo,
		image:    image,
		deadline: m.now().Add(m.ttl),
	}
	if len(job) > 0 {
		attempt.job = job[0]
	}
	m.byID[id] = attempt
	return id, nil
}

// repository returns the prepared repository an agent asked for.
//
// The repository stays the controller's until the answer arrives: an agent that
// pulls it twice, because a reply was lost, gets the same bytes rather than a
// second merge of the same incident.
func (m *mergeAttempts) repository(id string) (gitfetch.Repository, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	attempt, ok := m.byID[id]
	if !ok {
		return gitfetch.Repository{}, ErrNoMergeAttempt
	}
	if attempt.answer != nil {
		return gitfetch.Repository{}, ErrMergeAttemptAnswered
	}
	if !m.now().Before(attempt.deadline) {
		return gitfetch.Repository{}, ErrNoMergeAttempt
	}
	return attempt.repo, nil
}

// answer accepts one answer for one attempt and gives the repository back, so
// the caller can remove it once the answer has been recorded.
func (m *mergeAttempts) answer(id string, response gitmerge.WorkerResponse) (gitfetch.Repository, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	attempt, ok := m.byID[id]
	if !ok {
		return gitfetch.Repository{}, ErrNoMergeAttempt
	}
	if attempt.answer != nil {
		return gitfetch.Repository{}, ErrMergeAttemptAnswered
	}
	// The attempt stays, without its repository, until it expires: a duplicate
	// answer is a lost reply, and "already answered" is what that agent needs to
	// hear. The directory belongs to the caller from here.
	attempt.answer = &response
	repo := attempt.repo
	attempt.repo = gitfetch.Repository{}
	return repo, nil
}

// pending lists the attempts whose one answer has arrived and has not been
// taken for resolution yet.
func (m *mergeAttempts) pending() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	var ids []string
	for id, attempt := range m.byID {
		if attempt.answer != nil && !attempt.handled {
			ids = append(ids, id)
		}
	}
	return ids
}

// takeAnswer hands the answer and the job it belongs to to the resolver once.
func (m *mergeAttempts) takeAnswer(id string) (*conflictJob, gitmerge.WorkerResponse, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	attempt, ok := m.byID[id]
	if !ok || attempt.answer == nil || attempt.handled {
		return nil, gitmerge.WorkerResponse{}, false
	}
	attempt.handled = true
	return attempt.job, *attempt.answer, true
}

// forget lets go of one attempt without an answer, which is what a repair that
// was paused, cancelled or superseded does before any agent replies. The
// repository is handed back for removal like every other exit.
func (m *mergeAttempts) forget(id string) (gitfetch.Repository, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	attempt, ok := m.byID[id]
	if !ok {
		return gitfetch.Repository{}, ErrNoMergeAttempt
	}
	repo := attempt.repo
	delete(m.byID, id)
	return repo, nil
}

// expire lets go of every attempt past its deadline and reports how many it
// removed, so the caller can log a number rather than a guess.
func (m *mergeAttempts) expire() []gitfetch.Repository {
	m.mu.Lock()
	defer m.mu.Unlock()
	now := m.now()
	var expired []gitfetch.Repository
	for id, attempt := range m.byID {
		// A completed worker result waits for the bounded resolver queue;
		// another model call must not consume its worker deadline.
		if attempt.answer != nil && !attempt.handled && attempt.job != nil {
			continue
		}
		if now.Before(attempt.deadline) {
			continue
		}
		if attempt.repo.Dir != "" {
			expired = append(expired, attempt.repo)
		}
		delete(m.byID, id)
	}
	return expired
}

// close lets go of everything, which is what shutdown does.
func (m *mergeAttempts) close() []gitfetch.Repository {
	m.mu.Lock()
	defer m.mu.Unlock()
	held := make([]gitfetch.Repository, 0, len(m.byID))
	for id, attempt := range m.byID {
		if attempt.repo.Dir != "" {
			held = append(held, attempt.repo)
		}
		delete(m.byID, id)
	}
	return held
}

// len reports how many attempts are held, which a status view and a test both ask.
func (m *mergeAttempts) len() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.byID)
}

// newMergeAttemptID is a name an agent can be told without it meaning anything:
// no repository, no commit and no operator's identity go into it.
func newMergeAttemptID() (string, error) {
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", fmt.Errorf("controller: naming a merge attempt: %w", err)
	}
	return hex.EncodeToString(raw[:]), nil
}
