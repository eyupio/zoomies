package store

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

// EliRepairWatchRepository is one person's explicit decision to follow the open
// pull requests of a whole repository. It is separate from a per-PR watch so
// discovery can never infer repository-wide consent from one named PR.
type EliRepairWatchRepository struct {
	ID               string    `json:"id"`
	InstallationID   string    `json:"installation_id"`
	Repo             string    `json:"repo"`
	OwnerID          string    `json:"owner_id"`
	Checks           bool      `json:"checks"`
	Conflicts        bool      `json:"conflicts"`
	Enabled          bool      `json:"enabled"`
	LastDiscoveredAt time.Time `json:"last_discovered_at,omitempty"`
	CreatedAt        time.Time `json:"created_at"`
	UpdatedAt        time.Time `json:"updated_at"`
}

const eliRepairWatchRepositoryCols = `id,installation_id,repo,owner_id,checks,conflicts,enabled,last_discovered_at,created_at,updated_at`

func scanEliRepairWatchRepository(row interface{ Scan(...any) error }) (*EliRepairWatchRepository, error) {
	var r EliRepairWatchRepository
	var discovered, created, updated int64
	err := row.Scan(&r.ID, &r.InstallationID, &r.Repo, &r.OwnerID, &r.Checks, &r.Conflicts, &r.Enabled, &discovered, &created, &updated)
	if discovered > 0 {
		r.LastDiscoveredAt = at(discovered)
	}
	r.CreatedAt, r.UpdatedAt = at(created), at(updated)
	return &r, err
}

// SetEliRepairWatchRepository records or refreshes an explicit repository-wide
// decision. It never disables a subscription; a later pause control can.
func (s *Store) SetEliRepairWatchRepository(ctx context.Context, in EliRepairWatchRepository) (*EliRepairWatchRepository, error) {
	now := s.Now()
	var out *EliRepairWatchRepository
	err := s.tx(ctx, func(tx *sql.Tx) error {
		existing, err := scanEliRepairWatchRepository(tx.QueryRowContext(ctx,
			`SELECT `+eliRepairWatchRepositoryCols+` FROM eli_repair_watch_repositories WHERE repo COLLATE NOCASE=? AND installation_id=?`,
			in.Repo, in.InstallationID))
		switch {
		case errors.Is(err, sql.ErrNoRows):
			in.ID = NewID(PrefixEliRepairWatchRepository)
			in.Enabled = true
			in.CreatedAt, in.UpdatedAt = now, now
			in.LastDiscoveredAt = time.Time{}
			_, err = tx.ExecContext(ctx, `INSERT INTO eli_repair_watch_repositories(`+eliRepairWatchRepositoryCols+`) VALUES(?,?,?,?,?,?,?,?,?,?)`,
				in.ID, in.InstallationID, in.Repo, in.OwnerID, boolInt(in.Checks), boolInt(in.Conflicts), boolInt(in.Enabled), ms(in.LastDiscoveredAt), ms(in.CreatedAt), ms(in.UpdatedAt))
			if err != nil {
				return err
			}
			out = &in
			return nil
		case err != nil:
			return err
		}
		existing.OwnerID = in.OwnerID
		existing.Checks, existing.Conflicts = in.Checks, in.Conflicts
		existing.Enabled = true
		existing.UpdatedAt = now
		_, err = tx.ExecContext(ctx, `UPDATE eli_repair_watch_repositories SET owner_id=?,checks=?,conflicts=?,enabled=1,updated_at=? WHERE id=?`,
			existing.OwnerID, boolInt(existing.Checks), boolInt(existing.Conflicts), ms(existing.UpdatedAt), existing.ID)
		if err != nil {
			return err
		}
		out = existing
		return nil
	})
	if err != nil {
		return nil, wrapWrite(err)
	}
	return out, nil
}

func (s *Store) EliRepairWatchRepositoryFor(ctx context.Context, repo, installation string) (*EliRepairWatchRepository, error) {
	r, err := scanEliRepairWatchRepository(s.read.QueryRowContext(ctx, `SELECT `+eliRepairWatchRepositoryCols+` FROM eli_repair_watch_repositories WHERE repo COLLATE NOCASE=? AND installation_id=?`, repo, installation))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return r, err
}

func (s *Store) EliRepairWatchRepositories(ctx context.Context) ([]*EliRepairWatchRepository, error) {
	rows, err := s.read.QueryContext(ctx, `SELECT `+eliRepairWatchRepositoryCols+` FROM eli_repair_watch_repositories ORDER BY repo COLLATE NOCASE,installation_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*EliRepairWatchRepository{}
	for rows.Next() {
		r, err := scanEliRepairWatchRepository(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// DueEliRepairWatchRepositories returns the subscriptions least recently
// scanned, so a bounded pass rotates through all of them instead of pinning the
// same first page for ever.
func (s *Store) DueEliRepairWatchRepositories(ctx context.Context, limit int) ([]*EliRepairWatchRepository, error) {
	if limit <= 0 {
		return []*EliRepairWatchRepository{}, nil
	}
	rows, err := s.read.QueryContext(ctx, `SELECT `+eliRepairWatchRepositoryCols+` FROM eli_repair_watch_repositories WHERE enabled=1 ORDER BY last_discovered_at,repo COLLATE NOCASE,installation_id LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*EliRepairWatchRepository{}
	for rows.Next() {
		r, err := scanEliRepairWatchRepository(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func (s *Store) MarkEliRepairWatchRepositoryDiscovered(ctx context.Context, id string, now time.Time) error {
	_, err := s.exec(ctx, `UPDATE eli_repair_watch_repositories SET last_discovered_at=?,updated_at=? WHERE id=?`, ms(now), ms(now), id)
	return err
}

// EliRepairWatchRepositoryByID reads one repository-wide decision.
func (s *Store) EliRepairWatchRepositoryByID(ctx context.Context, id string) (*EliRepairWatchRepository, error) {
	r, err := scanEliRepairWatchRepository(s.read.QueryRowContext(ctx,
		`SELECT `+eliRepairWatchRepositoryCols+` FROM eli_repair_watch_repositories WHERE id=?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return r, err
}

// SetEliRepairWatchRepositoryEnabled pauses or resumes discovery for one
// repository decision. Resuming does not create a watch for a pull request; the
// normal admission path does that from fresh evidence.
func (s *Store) SetEliRepairWatchRepositoryEnabled(ctx context.Context, id string, enabled bool) (*EliRepairWatchRepository, error) {
	if _, err := s.exec(ctx,
		`UPDATE eli_repair_watch_repositories SET enabled=?,updated_at=? WHERE id=?`,
		boolInt(enabled), ms(s.Now()), id); err != nil {
		return nil, wrapWrite(err)
	}
	return s.EliRepairWatchRepositoryByID(ctx, id)
}
