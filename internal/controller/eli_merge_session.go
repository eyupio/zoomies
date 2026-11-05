package controller

import (
	"context"
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

// mergePrepared is what an agent is told to fetch: the attempt's id, the image to
// run, and the bytes to unpack.
type mergePrepared struct {
	ID         string
	Image      string
	Repository []byte
}

// prepare builds the repository for one incident, packs it, and holds it until an
// agent answers.
//
// The bytes are returned as well as held, because the endpoint that serves them
// must not have to reach back into the attempt for a second copy: an agent that
// pulls twice gets the same bytes from the same directory, and the directory is
// removed exactly once.
func (s *mergeSession) prepare(ctx context.Context, client github.Client, repo string, pin gitfetch.Pin, image string) (mergePrepared, error) {
	built, err := mergeRepository(ctx, client, repo, pin)
	if err != nil {
		return mergePrepared{}, err
	}
	packed, err := gitbare.Pack(ctx, built.Dir, 0)
	if err != nil {
		_ = discardMergeRepository(built)
		return mergePrepared{}, fmt.Errorf("packing the merge repository: %w", err)
	}
	id, err := s.attempts.add(built, image)
	if err != nil {
		_ = discardMergeRepository(built)
		return mergePrepared{}, err
	}
	return mergePrepared{ID: id, Image: image, Repository: packed}, nil
}

// repository returns the packed repository an agent asked for, which is the same
// bytes it was given the first time.
func (s *mergeSession) repository(ctx context.Context, id string) ([]byte, error) {
	held, err := s.attempts.repository(id)
	if err != nil {
		return nil, err
	}
	packed, err := gitbare.Pack(ctx, held.Dir, 0)
	if err != nil {
		// A repository that cannot be packed is one no agent can use, so it is
		// let go rather than left to expire.
		if released, forgetErr := s.attempts.forget(id); forgetErr == nil {
			_ = discardMergeRepository(released)
		}
		return nil, fmt.Errorf("packing the merge repository: %w", err)
	}
	return packed, nil
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
