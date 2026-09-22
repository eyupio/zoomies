package controller

import (
	"errors"
	"fmt"
	"github.com/eyupio/zoomies/internal/github"
	"time"
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
	if c.Now().Before(c.githubPaused[id]) {
		return false
	}
	if c.credentialMints[id] >= limit {
		// Held at our own limit rather than GitHub's, which is the case
		// nothing else in the fleet can see. Counted so the metric and the
		// problems list can both say it is happening; the caller retains the
		// demand and a later pass takes it.
		c.noteDeferredMintLocked(id)
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

// noteDeferredMintLocked records one creation held back at the minting limit.
// Called with githubMu held.
func (c *Controller) noteDeferredMintLocked(id string) {
	if c.deferredMints == nil {
		c.deferredMints = make(map[string]int)
		c.deferredSince = make(map[string]time.Time)
	}
	if c.deferredMints[id] == 0 {
		if _, ok := c.deferredSince[id]; !ok {
			c.deferredSince[id] = c.Now()
		}
	}
	c.deferredMints[id]++
	if c.metrics != nil {
		c.metrics.registrationsDeferred.WithLabelValues(id).Inc()
	}
}

// resetDeferredMints clears the last pass's count, keeping the moment the
// holding back started for as long as it is still happening. A pass that
// defers nothing forgets both, so the problem clears itself the moment the
// fleet catches up.
func (c *Controller) resetDeferredMints() {
	c.githubMu.Lock()
	defer c.githubMu.Unlock()
	for id, n := range c.deferredMints {
		if n == 0 {
			delete(c.deferredSince, id)
		}
		c.deferredMints[id] = 0
	}
}

// deferredMintsNow is what the last pass held back, and since when.
func (c *Controller) deferredMintsNow() (map[string]int, map[string]time.Time) {
	c.githubMu.Lock()
	defer c.githubMu.Unlock()
	out := make(map[string]int, len(c.deferredMints))
	since := make(map[string]time.Time, len(c.deferredSince))
	for id, n := range c.deferredMints {
		if n > 0 {
			out[id] = n
			if t, ok := c.deferredSince[id]; ok {
				since[id] = t
			}
		}
	}
	return out, since
}
