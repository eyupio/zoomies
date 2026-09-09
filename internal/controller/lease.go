package controller

import (
	"context"
	"errors"
	"time"

	"github.com/eyupio/zoomies/internal/store"
)

const (
	// LeaseTTL is how long a controller's claim on the database stands without
	// being renewed. It is comfortably longer than the renewal interval, so a
	// slow disk or a paused process does not lose a lease it still holds, and
	// short enough that a controller killed rather than stopped does not keep
	// a fleet waiting for long.
	LeaseTTL = 90 * time.Second
	// leaseRenew is how often the holder stamps the row. Three renewals fit
	// inside the TTL, which is the same ratio the host heartbeat uses.
	leaseRenew = 30 * time.Second
)

// leaseLoop keeps this controller's claim on the database current, and notices
// when it no longer has one.
//
// Losing the lease is not recoverable by retrying: another controller is now
// running against the same database, and both are scheduling. This says so,
// loudly and permanently, rather than trying to take it back -- a lease this
// process fights another for would flap between them, and two controllers each
// convinced they are the only one is exactly the state the lease exists to
// prevent.
func (c *Controller) leaseLoop(ctx context.Context) {
	t := time.NewTicker(leaseRenew)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
		held, err := c.st.RenewControllerLease(ctx, c.lease.Holder)
		switch {
		case ctx.Err() != nil:
			return
		case errors.Is(err, store.ErrConflict), errors.Is(err, store.ErrNotFound):
			if c.leaseLost.Load() == nil {
				c.log.Error("another controller has taken this database's lease; two controllers are now running against it",
					"holder", holderName(held), "ours", c.lease.Holder)
			}
			if held == nil {
				// The row was deleted rather than taken. Somebody else's
				// clean shutdown, or a hand-edited database; either way this
				// controller can no longer prove it is the only one.
				held = &store.ControllerLease{Holder: "nobody"}
			}
			c.leaseLost.Store(held)
		case err != nil:
			// A transient write failure is not a lost lease, and the TTL is
			// three renewals wide precisely so one of these costs nothing.
			c.log.Warn("could not renew the controller lease", "error", err)
		}
	}
}

func holderName(l *store.ControllerLease) string {
	if l == nil {
		return "nobody"
	}
	return l.Describe()
}
