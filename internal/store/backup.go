package store

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Backup writes a consistent copy of the database to dest.
//
// It is `VACUUM INTO` rather than a file copy, and that is the whole reason
// this exists in the store: a running instance keeps a write-ahead log, so
// copying the .db file alone produces something that is missing every recent
// commit, and copying all three files while a write is in flight produces
// something that is missing part of one. VACUUM INTO takes the write lock, so
// what lands is one file, already checkpointed, with no WAL beside it -- a
// database an operator can move, verify and open anywhere.
//
// The documentation used to send operators to the sqlite3 command line for
// this. That binary is not in the container image, so the instructions could
// not be followed by most of the people reading them.
func (s *Store) Backup(ctx context.Context, dest string) error {
	if strings.TrimSpace(dest) == "" {
		return errors.New("store: a backup needs a destination path")
	}
	abs, err := filepath.Abs(dest)
	if err != nil {
		return fmt.Errorf("store: resolving %q: %w", dest, err)
	}
	// Refusing an existing file rather than overwriting it: the destination of
	// a backup is nearly always a name derived from the date, and the one time
	// it is not, the file already there is somebody's older backup. Losing that
	// to a typo is the failure this guards.
	// The destination is interpolated into the statement below rather than
	// bound, because SQLite parses VACUUM INTO's target at prepare time and
	// will not take a parameter for it. A path containing a quote is refused
	// rather than escaped: it is not a thing an operator means to type, and
	// quoting rules are exactly where an escape goes wrong.
	if strings.ContainsAny(abs, "'\"") {
		return fmt.Errorf("store: %s contains a quote; choose a path without one", abs)
	}
	if _, err := os.Stat(abs); err == nil {
		return fmt.Errorf("store: %s already exists; a backup never overwrites one", abs)
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("store: checking %s: %w", abs, err)
	}
	if err := os.MkdirAll(filepath.Dir(abs), 0o750); err != nil {
		return fmt.Errorf("store: creating %s: %w", filepath.Dir(abs), err)
	}

	s.wmu.Lock()
	defer s.wmu.Unlock()
	if _, err := s.write.ExecContext(ctx, `VACUUM INTO '`+abs+`'`); err != nil {
		return fmt.Errorf("store: copying the database to %s: %w", abs, err)
	}
	// A backup holds every sealed secret this instance has and every account's
	// password hash. VACUUM INTO creates the file with the process umask,
	// which on a default Debian image is world-readable.
	if err := os.Chmod(abs, 0o600); err != nil {
		return fmt.Errorf("store: tightening permissions on %s: %w", abs, err)
	}
	return nil
}

// IntegrityCheck asks SQLite whether the database is internally consistent.
//
// It is what makes a backup a backup rather than a file: a copy nobody has
// opened is a copy nobody knows about, and the moment to find out is while the
// original is still there. `PRAGMA integrity_check` walks every page, index
// and constraint, so it is not instant on a large database and is not run on
// the ordinary startup path.
func (s *Store) IntegrityCheck(ctx context.Context) error {
	rows, err := s.read.QueryContext(ctx, `PRAGMA integrity_check`)
	if err != nil {
		return fmt.Errorf("store: checking the integrity of %s: %w", s.path, err)
	}
	defer rows.Close()
	var problems []string
	for rows.Next() {
		var line string
		if err := rows.Scan(&line); err != nil {
			return fmt.Errorf("store: checking the integrity of %s: %w", s.path, err)
		}
		// SQLite answers a healthy database with the single row "ok".
		if line != "ok" {
			problems = append(problems, line)
		}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("store: checking the integrity of %s: %w", s.path, err)
	}
	if len(problems) > 0 {
		return fmt.Errorf("store: %s is corrupt: %s", s.path, strings.Join(problems, "; "))
	}
	return nil
}

// HasSealedSecrets reports whether anything in this database is encrypted with
// the instance key.
//
// It answers one question, asked at startup: may a new key be generated? A
// fresh install has nothing sealed and a generated key is right; a database
// with installations in it has a GitHub App private key and a webhook secret
// that only the original key opens, and generating a new one there produces an
// instance that starts, looks healthy, and fails inside its first GitHub call.
func (s *Store) HasSealedSecrets(ctx context.Context) (bool, error) {
	var n int
	err := s.read.QueryRowContext(ctx, `SELECT COUNT(*) FROM installations
		WHERE (private_key_enc IS NOT NULL AND LENGTH(private_key_enc) > 0)
		   OR (webhook_secret_enc IS NOT NULL AND LENGTH(webhook_secret_enc) > 0)`).Scan(&n)
	if err != nil {
		return false, fmt.Errorf("store: counting sealed secrets: %w", err)
	}
	return n > 0, nil
}
