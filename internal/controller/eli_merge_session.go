package controller

import (
	"context"
	"errors"
	"fmt"

	"github.com/eyupio/zoomies/internal/gitbare"
	"github.com/eyupio/zoomies/internal/gitfetch"
	"github.com/eyupio/zoomies/internal/github"
	"github.com/eyupio/zoomies/internal/gitmerge"
)

// mergeSession is one conflict repair's journey through the worker: the repository
// prepared from the three pinned commits, the attempt an agent answers, and the
// cleanup when either of those ends.
//
// It is the controller's whole transport surface. The agent endpoints are thin
// handlers over the three methods below, and everything that is easy to get wrong
// (one answer, a deadline, a directory removed on every path) is here where it can
// be tested without a runtime or a network.
type mergeSession struct {
	attempts *mergeAttempts
}

func newMergeSession(attempts *mergeAttempts) *mergeSession {
	return &mergeSession{attempts: attempts}
}

// MergePrepared is what an agent is told to fetch: the attempt's id, the image
// to run, and the three commit ids its worker request names. The bytes stay on
// the controller until the agent pulls them.
type MergePrepared struct {
	ID       string
	Image    string
	Ancestor string
	Base     string
	Head     string
}

// MergeRepository is one attempt's packed repository with the three pinned
// commit ids the worker's request needs. The API puts the ids in headers and the
// bytes in the body, which is the same shape the agent pulls.
type MergeRepository struct {
	Ancestor   string
	Base       string
	Head       string
	Repository []byte
}

// prepare builds the repository for one incident, packs it, and holds it until an
// agent answers.
//
// The pack is a check as much as an output: a repository the controller cannot
// turn into the bytes an agent fetches is one no worker could unpack, so the
// repair fails now rather than at the host that was asked to merge it. Every
// pull afterwards packs the same directory again, and the directory is removed
// exactly once.
func (s *mergeSession) prepare(ctx context.Context, client github.Client, repo string, pin gitfetch.Pin, image string) (MergePrepared, error) {
	built, err := mergeRepository(ctx, client, repo, pin)
	if err != nil {
		return MergePrepared{}, err
	}
	if _, err := gitbare.Pack(ctx, built.Dir, 0); err != nil {
		// Pack before an agent is told to fetch: a repository the controller
		// cannot hand out is one no worker could unpack, so the repair fails now
		// rather than at the host that was asked to merge it.
		_ = discardMergeRepository(built)
		return MergePrepared{}, fmt.Errorf("packing the merge repository: %w", err)
	}
	id, err := s.attempts.add(built, image)
	if err != nil {
		_ = discardMergeRepository(built)
		return MergePrepared{}, err
	}
	return MergePrepared{
		ID: id, Image: image,
		Ancestor: built.Ancestor, Base: built.Base, Head: built.Head,
	}, nil
}

// repository returns the packed repository an agent asked for, which is the same
// bytes it was given the first time, with the ids its worker request names.
func (s *mergeSession) repository(ctx context.Context, id string) (MergeRepository, error) {
	held, err := s.attempts.repository(id)
	if err != nil {
		return MergeRepository{}, err
	}
	packed, err := gitbare.Pack(ctx, held.Dir, 0)
	if err != nil {
		// A repository that cannot be packed is one no agent can use, so it is
		// let go rather than left to expire.
		if released, forgetErr := s.attempts.forget(id); forgetErr == nil {
			_ = discardMergeRepository(released)
		}
		return MergeRepository{}, fmt.Errorf("packing the merge repository: %w", err)
	}
	return MergeRepository{
		Ancestor: held.Ancestor, Base: held.Base, Head: held.Head,
		Repository: packed,
	}, nil
}

// accept records the one answer for an attempt and removes the repository it was
// built from, whatever the answer says. A conflicted merge is a successful answer:
// the conflict is the evidence the repair is made from.
func (s *mergeSession) accept(id string, response gitmerge.WorkerResponse) (gitmerge.WorkerResponse, error) {
	released, err := s.attempts.answer(id, response)
	if err != nil {
		return gitmerge.WorkerResponse{}, err
	}
	_ = discardMergeRepository(released)
	return response, nil
}

// abandon lets go of an attempt nobody will answer, which is what a repair that
// was paused, cancelled or superseded does.
func (s *mergeSession) abandon(id string) error {
	released, err := s.attempts.forget(id)
	if err != nil {
		return err
	}
	return discardMergeRepository(released)
}

// expire removes every repository whose deadline has passed, so a repair whose
// agent never answered leaves nothing on disk.
func (s *mergeSession) expire() int {
	expired := s.attempts.expire()
	for _, repo := range expired {
		_ = discardMergeRepository(repo)
	}
	return len(expired)
}

// close removes every repository still held, which is what shutdown does.
func (s *mergeSession) close() int {
	held := s.attempts.close()
	for _, repo := range held {
		_ = discardMergeRepository(repo)
	}
	return len(held)
}

// PrepareMerge builds the repository one conflict repair merges in and holds it
// until an agent answers. Nothing is published and no branch moves.
func (c *Controller) PrepareMerge(ctx context.Context, client github.Client, repo string, pin gitfetch.Pin, image string) (MergePrepared, error) {
	return c.merge.prepare(ctx, client, repo, pin, image)
}

// PullMergeRepository serves one attempt's repository to the agent that was told
// to merge it.
func (c *Controller) PullMergeRepository(ctx context.Context, hostID, id string) (MergeRepository, error) {
	if hostID == "" {
		return MergeRepository{}, errors.New("controller: a merge repository pull carried no host ID")
	}
	return c.merge.repository(ctx, id)
}

// ReportMergeAnswer accepts the one answer a merge worker produced.
func (c *Controller) ReportMergeAnswer(ctx context.Context, hostID, id string, answer gitmerge.WorkerResponse) error {
	if hostID == "" {
		return errors.New("controller: a merge answer carried no host ID")
	}
	_, err := c.merge.accept(id, answer)
	return err
}
