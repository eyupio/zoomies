package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// The offsite destinations an administrator keeps in the database.
//
// They are the same thing `backup.remotes` describes in zoomies.yaml, stored
// where a page can edit them. The file's are still read, and still win a name
// collision: a fleet whose database is gone has only the file, so the file has
// to remain the authority on what it says.
//
// The two secrets are written by their own statement rather than by the update
// that carries the form, for the reason SetProviderCredentials exists: an
// operator pasting a new secret key is not also re-submitting the retention
// from a page they opened yesterday.

// BackupRemote is one S3-compatible destination as the database holds it.
type BackupRemote struct {
	ID string `json:"id"`
	// Name is what every other surface addresses this destination by -- the
	// API route, the log line, the problems drawer -- so it is unique.
	Name string `json:"name"`
	// Endpoint is the service's URL; its scheme decides whether the
	// connection is encrypted. Region is what requests are signed for.
	Endpoint string `json:"endpoint"`
	Region   string `json:"region"`
	// Bucket and Prefix are where the archives land. Zoomies never creates a
	// bucket.
	Bucket string `json:"bucket"`
	Prefix string `json:"prefix"`
	// AccessKeyID is not sealed: it is an identifier, it appears in the
	// service's own access logs, and an operator comparing two destinations
	// needs to see it. The secret beside it is.
	AccessKeyID string `json:"access_key_id"`
	// SecretKeyEnc and PassphraseEnc are sealed with the instance key and
	// never leave this process in plaintext.
	SecretKeyEnc  []byte `json:"-"`
	PassphraseEnc []byte `json:"-"`
	// PathStyle puts the bucket in the path rather than the hostname. Nil
	// chooses from the endpoint, which is the answer that is right without
	// anybody having to know what it means.
	PathStyle *bool `json:"path_style"`
	// Keep is this destination's own retention; 0 keeps every copy.
	Keep int `json:"keep"`
	// Enabled off stops uploads without losing what the bucket holds or what
	// it took to describe it.
	Enabled   bool      `json:"enabled"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

const backupRemoteCols = `id, name, endpoint, region, bucket, prefix, access_key_id,
	secret_key_enc, passphrase_enc, path_style, keep, enabled, created_at, updated_at`

func scanBackupRemote(sc interface{ Scan(...any) error }) (*BackupRemote, error) {
	var r BackupRemote
	var enabled int
	var pathStyle sql.NullInt64
	var created, updated int64
	err := sc.Scan(&r.ID, &r.Name, &r.Endpoint, &r.Region, &r.Bucket, &r.Prefix, &r.AccessKeyID,
		&r.SecretKeyEnc, &r.PassphraseEnc, &pathStyle, &r.Keep, &enabled, &created, &updated)
	if err != nil {
		return nil, err
	}
	r.Enabled = enabled == 1
	if pathStyle.Valid {
		style := pathStyle.Int64 == 1
		r.PathStyle = &style
	}
	r.CreatedAt, r.UpdatedAt = at(created), at(updated)
	return &r, nil
}

// CreateBackupRemote inserts a destination. The secrets are not part of it:
// SetBackupRemoteSecrets writes the sealed bytes, so the one place that has to
// hold the instance key is the one place that holds it.
func (s *Store) CreateBackupRemote(ctx context.Context, r *BackupRemote) error {
	if r.ID == "" {
		r.ID = NewID(PrefixBackupRemote)
	}
	now := s.Now()
	r.CreatedAt, r.UpdatedAt = now, now
	_, err := s.exec(ctx, `INSERT INTO backup_remotes
		(id, name, endpoint, region, bucket, prefix, access_key_id, path_style, keep, enabled, created_at, updated_at)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?)`,
		r.ID, r.Name, r.Endpoint, r.Region, r.Bucket, r.Prefix, r.AccessKeyID,
		nullableBool(r.PathStyle), r.Keep, boolInt(r.Enabled), ms(now), ms(now))
	return wrapWrite(err)
}

// UpdateBackupRemote persists the form. The secrets are absent on purpose.
func (s *Store) UpdateBackupRemote(ctx context.Context, r *BackupRemote) error {
	now := s.Now()
	r.UpdatedAt = now
	res, err := s.exec(ctx, `UPDATE backup_remotes SET name=?, endpoint=?, region=?, bucket=?,
		prefix=?, access_key_id=?, path_style=?, keep=?, enabled=?, updated_at=? WHERE id=?`,
		r.Name, r.Endpoint, r.Region, r.Bucket, r.Prefix, r.AccessKeyID,
		nullableBool(r.PathStyle), r.Keep, boolInt(r.Enabled), ms(now), r.ID)
	if err != nil {
		return wrapWrite(err)
	}
	return affected(res, "backup remote", r.ID)
}

// SetBackupRemoteSecrets replaces the sealed secret key and passphrase. A nil
// slice clears that secret; leaving one out entirely is the caller's job,
// which is why both are explicit here.
func (s *Store) SetBackupRemoteSecrets(ctx context.Context, id string, secretKey, passphrase []byte) error {
	res, err := s.exec(ctx, `UPDATE backup_remotes SET secret_key_enc=?, passphrase_enc=?, updated_at=?
		WHERE id=?`, secretKey, passphrase, ms(s.Now()), id)
	if err != nil {
		return err
	}
	return affected(res, "backup remote", id)
}

// GetBackupRemote returns one destination by id.
func (s *Store) GetBackupRemote(ctx context.Context, id string) (*BackupRemote, error) {
	row := s.read.QueryRowContext(ctx, `SELECT `+backupRemoteCols+` FROM backup_remotes WHERE id = ?`, id)
	r, err := scanBackupRemote(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("backup remote %s: %w", id, ErrNotFound)
	}
	return r, err
}

// GetBackupRemoteByName returns one destination by the name every surface
// addresses it with.
func (s *Store) GetBackupRemoteByName(ctx context.Context, name string) (*BackupRemote, error) {
	row := s.read.QueryRowContext(ctx, `SELECT `+backupRemoteCols+` FROM backup_remotes WHERE name = ?`, name)
	r, err := scanBackupRemote(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("backup remote %q: %w", name, ErrNotFound)
	}
	return r, err
}

// ListBackupRemotes returns every destination, by name. Unpaginated for the
// reason ListProviders is: a fleet has a handful of these and every offsite
// pass reads the lot.
func (s *Store) ListBackupRemotes(ctx context.Context) ([]*BackupRemote, error) {
	rows, err := s.read.QueryContext(ctx, `SELECT `+backupRemoteCols+` FROM backup_remotes ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*BackupRemote
	for rows.Next() {
		r, err := scanBackupRemote(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// DeleteBackupRemote removes a destination. What the bucket holds is not
// touched: the copies are somebody's backups, and forgetting where they are is
// not the same as deciding they should not exist.
func (s *Store) DeleteBackupRemote(ctx context.Context, id string) error {
	res, err := s.exec(ctx, `DELETE FROM backup_remotes WHERE id = ?`, id)
	if err != nil {
		return err
	}
	return affected(res, "backup remote", id)
}

// nullableBool renders an unset tri-state for a SQLite INTEGER column.
func nullableBool(b *bool) any {
	if b == nil {
		return nil
	}
	return boolInt(*b)
}
