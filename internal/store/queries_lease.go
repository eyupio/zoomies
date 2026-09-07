package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// ---------------------------------------------------------------------------
// The controller lease
// ---------------------------------------------------------------------------

const leaseCols = `holder, host, pid, version, acquired_at, renewed_at`

func scanLease(sc interface{ Scan(...any) error }) (*ControllerLease, error) {
	var l ControllerLease
	var acquired, renewed int64
	if err := sc.Scan(&l.Holder, &l.Host, &l.PID, &l.Version, &acquired, &renewed); err != nil {
		return nil, err
	}
	l.AcquiredAt, l.RenewedAt = at(acquired), at(renewed)
	return &l, nil
}

// ControllerLeaseHolder returns the lease as it stands, or ErrNotFound when no
// controller has ever held one.
func (s *Store) ControllerLeaseHolder(ctx context.Context) (*ControllerLease, error) {
	l, err := scanLease(s.read.QueryRowContext(ctx, `SELECT `+leaseCols+` FROM controller_lease WHERE id = 1`))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return l, err
}

// AcquireControllerLease takes the lease for this controller, or refuses.
//
// It refuses with ErrConflict when another controller holds a lease it has
// renewed inside ttl; the returned lease is that holder's, so the caller can
// name it. A lease nobody has renewed inside ttl is taken without ceremony --
// a controller that was killed rather than stopped must not need an operator
// to come and clear up after it -- and takeover ignores the holder entirely.
//
// **The caller must already hold the database lock**, because that is what
// makes the same-host rule below sound.
//
// A lease naming this same host is reclaimed at once rather than waited out.
// The lock proves no other process on this host has the database, so a lease
// from here belongs to a controller that is gone. Without this rule the
// service unit's Restart=always would restart-loop for the whole TTL after
// every crash: the new process mints a new holder, finds its predecessor's
// lease renewed seconds ago, and refuses to start. The cross-host case is
// untouched -- that is the one no lock on this host can see, and the one the
// lease exists for.
//
// The read and the write are one transaction, which is what makes this safe
// between processes rather than only between goroutines: SQLite serialises
// write transactions on the file, so of two controllers starting together
// exactly one sees an empty or stale row.
func (s *Store) AcquireControllerLease(ctx context.Context, l *ControllerLease, ttl time.Duration, takeover bool) (*ControllerLease, error) {
	if l == nil || l.Holder == "" {
		return nil, fmt.Errorf("acquiring the controller lease: %w", errors.New("a lease needs a holder"))
	}
	var held *ControllerLease
	err := s.tx(ctx, func(tx *sql.Tx) error {
		now := s.Now()
		current, err := scanLease(tx.QueryRowContext(ctx, `SELECT `+leaseCols+` FROM controller_lease WHERE id = 1`))
		switch {
		case errors.Is(err, sql.ErrNoRows):
			current = nil
		case err != nil:
			return err
		}
		// Our own row from a previous life is not a rival: a controller that
		// restarts fast enough to beat its own lease's expiry would otherwise
		// refuse to start, which is the opposite of what this is for.
		mine := current != nil && current.Holder == l.Holder
		// Nor is a predecessor on this same host, for the reason above: the
		// caller holds the lock, so nothing here is still running.
		ours := current != nil && l.Host != "" && current.Host == l.Host
		if !takeover && !mine && !ours && !current.Stale(now, ttl) {
			held = current
			return ErrConflict
		}
		// Truncated to what the column holds, so a caller that writes a lease
		// and then compares it against the row is comparing like with like.
		l.RenewedAt = now.Truncate(time.Millisecond)
		l.AcquiredAt = l.RenewedAt
		if mine {
			l.AcquiredAt = current.AcquiredAt
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO controller_lease (id, `+leaseCols+`)
			VALUES (1,?,?,?,?,?,?)
			ON CONFLICT(id) DO UPDATE SET holder=excluded.holder, host=excluded.host,
				pid=excluded.pid, version=excluded.version,
				acquired_at=excluded.acquired_at, renewed_at=excluded.renewed_at`,
			l.Holder, l.Host, l.PID, l.Version, ms(l.AcquiredAt), ms(l.RenewedAt))
		return err
	})
	if errors.Is(err, ErrConflict) {
		return held, err
	}
	return l, err
}

// RenewControllerLease stamps the lease as still ours, and reports when it is
// not: another controller taking it over is the fleet's worst state, because
// both are scheduling, and the only thing worse than noticing is not.
func (s *Store) RenewControllerLease(ctx context.Context, holder string) (*ControllerLease, error) {
	var held *ControllerLease
	err := s.tx(ctx, func(tx *sql.Tx) error {
		current, err := scanLease(tx.QueryRowContext(ctx, `SELECT `+leaseCols+` FROM controller_lease WHERE id = 1`))
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
		if current.Holder != holder {
			held = current
			return ErrConflict
		}
		_, err = tx.ExecContext(ctx, `UPDATE controller_lease SET renewed_at = ? WHERE id = 1 AND holder = ?`,
			ms(s.Now()), holder)
		return err
	})
	return held, err
}

// ReleaseControllerLease drops the lease on a clean shutdown, so the next
// controller starts at once rather than waiting out a lease nobody holds.
func (s *Store) ReleaseControllerLease(ctx context.Context, holder string) error {
	_, err := s.exec(ctx, `DELETE FROM controller_lease WHERE id = 1 AND holder = ?`, holder)
	return err
}
