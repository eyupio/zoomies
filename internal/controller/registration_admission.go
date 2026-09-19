package controller

import (
	"errors"
	"fmt"
	"github.com/eyupio/zoomies/internal/github"
)

var errRegistrationHeld = fmt.Errorf("installation rate-limit hold: %w", github.ErrRateLimited)

var errRegistrationDeferred = errors.New("registration admission deferred until a later scheduling pass")

// Admission happens before creating a row or goroutine. The scheduler retains
// excess demand, so a slow installation cannot build an unbounded waiter queue.
func (c *Controller) admitCredentialMint(id string) bool {
	limit := max(1, min(16, c.cfg().Scheduler.RegistrationConcurrency))
	if c.githubHeld(id, c.Now()) {
		return false
	}
	c.githubMu.Lock()
	defer c.githubMu.Unlock()
	if c.Now().Before(c.githubPaused[id]) || c.credentialMints[id] >= limit {
		return false
	}
	if c.credentialMints == nil {
		c.credentialMints = make(map[string]int)
	}
	c.credentialMints[id]++
	return true
}

func (c *Controller) releaseCredentialMint(id string) {
	c.githubMu.Lock()
	defer c.githubMu.Unlock()
	c.credentialMints[id]--
	if c.credentialMints[id] <= 0 {
		delete(c.credentialMints, id)
	}
}
