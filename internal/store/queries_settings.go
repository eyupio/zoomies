package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// The fleet's configuration, as rows.
//
// These are deliberately separate from GetSetting/SetSetting next door, which
// work on the `settings` table. That one holds facts the product keeps about
// itself; this one holds settings an operator owns. The difference matters at
// exactly three moments: rendering a settings page, exporting a configuration,
// and resetting one -- and in all three, a fence or a network identity turning
// up among the retention windows would be a bug.
//
// Nothing here encrypts anything. The store never has the key; a caller seals a
// secret before it arrives and opens it after it leaves, the same way the
// private-network identity and an installation's private key are handled.

// InstanceSetting is one configuration key as it is stored.
type InstanceSetting struct {
	Key string `json:"key"`
	// Value is the text an operator would have typed. For a secret it is the
	// sealed ciphertext, base64-encoded, and never leaves the process in that
	// form -- see ListInstanceSettings.
	Value     string    `json:"value"`
	Secret    bool      `json:"secret"`
	UpdatedAt time.Time `json:"updated_at"`
	UpdatedBy string    `json:"updated_by,omitempty"`
}

const instanceSettingCols = `key, value, secret, updated_at, updated_by`

func scanInstanceSetting(sc interface{ Scan(...any) error }) (*InstanceSetting, error) {
	var s InstanceSetting
	var secret int
	var updated int64
	if err := sc.Scan(&s.Key, &s.Value, &secret, &updated, &s.UpdatedBy); err != nil {
		return nil, err
	}
	s.Secret, s.UpdatedAt = secret == 1, at(updated)
	return &s, nil
}

// InstanceSettings returns every stored setting, sealed values and all.
//
// It is the startup path: the caller holds the encryption key and is the only
// thing that can make sense of a secret row. A handler wanting to render these
// for a person wants ListInstanceSettings instead, which cannot leak one.
func (s *Store) InstanceSettings(ctx context.Context) ([]InstanceSetting, error) {
	rows, err := s.read.QueryContext(ctx,
		`SELECT `+instanceSettingCols+` FROM instance_settings ORDER BY key`)
	if err != nil {
		return nil, fmt.Errorf("store: listing instance settings: %w", err)
	}
	defer rows.Close()
	var out []InstanceSetting
	for rows.Next() {
		row, err := scanInstanceSetting(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *row)
	}
	return out, rows.Err()
}

// ListInstanceSettings is InstanceSettings with every secret's value blanked,
// so it is safe to hand to anything that renders.
func (s *Store) ListInstanceSettings(ctx context.Context) ([]InstanceSetting, error) {
	out, err := s.InstanceSettings(ctx)
	if err != nil {
		return nil, err
	}
	for i := range out {
		if out[i].Secret {
			out[i].Value = ""
		}
	}
	return out, nil
}

// GetInstanceSetting returns one stored setting, or ErrNotFound.
//
// Unlike GetSetting next door, a missing row is an error rather than an empty
// string. The distinction is the whole design of this table: "" is a value an
// operator can legitimately set -- an empty external URL, no trusted proxies --
// and a caller that cannot tell it from "never set" would overwrite the layer
// underneath with a blank.
func (s *Store) GetInstanceSetting(ctx context.Context, key string) (*InstanceSetting, error) {
	row := s.read.QueryRowContext(ctx,
		`SELECT `+instanceSettingCols+` FROM instance_settings WHERE key = ?`, key)
	out, err := scanInstanceSetting(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("setting %s: %w", key, ErrNotFound)
	}
	if err != nil {
		return nil, err
	}
	return out, nil
}

// PutInstanceSettings writes a batch in one transaction.
//
// A batch, because one request from the settings page is one change: applying
// the keys that parsed and leaving the rest would put the fleet in a state
// nobody asked for, and would do it without an audit row saying which half took
// effect. An empty value is written, not skipped -- see GetInstanceSetting.
func (s *Store) PutInstanceSettings(ctx context.Context, by string, values []InstanceSetting) error {
	return s.ApplyInstanceSettings(ctx, by, values, nil)
}

// ApplyInstanceSettings writes and clears in one transaction.
//
// One request from the settings page is one change, and a request that both
// sets a key and clears another has to be one too: two transactions can leave
// half of it in effect, with an audit row claiming both halves took.
func (s *Store) ApplyInstanceSettings(ctx context.Context, by string, put []InstanceSetting, clear []string) error {
	if len(put) == 0 && len(clear) == 0 {
		return nil
	}
	now := ms(s.Now())
	return s.tx(ctx, func(tx *sql.Tx) error {
		for _, v := range put {
			_, err := tx.ExecContext(ctx,
				`INSERT INTO instance_settings (key, value, secret, updated_at, updated_by)
				 VALUES (?,?,?,?,?)
				 ON CONFLICT(key) DO UPDATE SET
				   value=excluded.value, secret=excluded.secret,
				   updated_at=excluded.updated_at, updated_by=excluded.updated_by`,
				v.Key, v.Value, boolInt(v.Secret), now, by)
			if err != nil {
				return fmt.Errorf("store: writing setting %s: %w", v.Key, err)
			}
		}
		for _, key := range clear {
			if _, err := tx.ExecContext(ctx, `DELETE FROM instance_settings WHERE key = ?`, key); err != nil {
				return fmt.Errorf("store: clearing setting %s: %w", key, err)
			}
		}
		return nil
	})
}

// DeleteInstanceSettings removes keys, which is how a setting goes back to
// whatever the layer beneath it says. It is not an error to delete a key that
// was never set: the caller asked for it to be unset, and it is.
func (s *Store) DeleteInstanceSettings(ctx context.Context, keys []string) error {
	return s.ApplyInstanceSettings(ctx, "", nil, keys)
}

// HasInstanceSettings reports whether anything has been stored at all.
//
// It is what tells a first run from a fleet that has been configured, which is
// the question the installer asks before seeding a configuration file's values
// in, and the one the first-run wizard asks before offering to collect them.
func (s *Store) HasInstanceSettings(ctx context.Context) (bool, error) {
	var n int
	if err := s.read.QueryRowContext(ctx, `SELECT COUNT(*) FROM instance_settings`).Scan(&n); err != nil {
		return false, fmt.Errorf("store: counting instance settings: %w", err)
	}
	return n > 0, nil
}
