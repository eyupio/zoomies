package controller

import (
	"context"
	"errors"
	"fmt"
)

// ErrLimitReached marks a refusal because one of the limits.* ceilings has
// been reached. The API answers it with a 409 and the stable code
// limit_reached, since the request is well formed and would succeed on an
// instance with room: it is the state of the fleet that refuses it, not the
// request.
var ErrLimitReached = errors.New("limit reached")

// LimitError is ErrLimitReached with the setting that refused, so the message
// an operator reads names the key they would change -- or the one they would
// ask whoever runs the instance to change, since every limit is platform-scoped.
type LimitError struct {
	Setting string
	Limit   int
	What    string
	// Free says how to make room without changing the setting. Empty is
	// "remove one first", which is right for everything stored.
	Free string
}

func (e *LimitError) Error() string {
	free := e.Free
	if free == "" {
		free = "remove one first"
	}
	return fmt.Sprintf("this instance already holds %d %s, the most %s allows; %s, or ask whoever runs this instance to raise %s",
		e.Limit, e.What, e.Setting, free, e.Setting)
}

func (e *LimitError) Unwrap() error { return ErrLimitReached }

// admit refuses when a ceiling is set and count has already reached it.
func admit(setting, what string, limit int, count func() (int, error)) error {
	if limit <= 0 {
		return nil
	}
	n, err := count()
	if err != nil {
		return fmt.Errorf("counting %s for %s: %w", what, setting, err)
	}
	if n >= limit {
		return &LimitError{Setting: setting, Limit: limit, What: what}
	}
	return nil
}

// AdmitPool says whether one more pool fits under limits.pools.
func (c *Controller) AdmitPool(ctx context.Context) error {
	return admit("limits.pools", "pools", c.cfg().Limits.Pools, func() (int, error) {
		return c.st.CountPools(ctx)
	})
}

// AdmitJoinToken says whether one more outstanding join token fits under
// limits.join_tokens.
func (c *Controller) AdmitJoinToken(ctx context.Context) error {
	return admit("limits.join_tokens", "outstanding join tokens", c.cfg().Limits.JoinTokens, func() (int, error) {
		return c.st.CountOutstandingJoinTokens(ctx)
	})
}

// admitHost says whether one more host fits under limits.hosts.
func (c *Controller) admitHost(ctx context.Context) error {
	return admit("limits.hosts", "hosts", c.cfg().Limits.Hosts, func() (int, error) {
		return c.st.CountHosts(ctx)
	})
}
