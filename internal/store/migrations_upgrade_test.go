package store

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// atSchema builds a database carrying only the first n migrations, which is
// what a release older than this one left behind.
//
// It applies the real embedded SQL rather than a hand-written schema dump: a
// fixture written by hand drifts from what the migrations actually produce,
// and then the upgrade it is testing is an upgrade from a database that never
// existed.
func atSchema(t *testing.T, n int) string {
	t.Helper()
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "zoomies.db")

	migs, err := loadMigrations()
	if err != nil {
		t.Fatalf("loadMigrations: %v", err)
	}
	if n > len(migs) {
		t.Fatalf("this build has %d migrations, so it cannot be rolled back to %d", len(migs), n)
	}

	// Applied straight onto the file rather than through Open, which would
	// migrate to head on the way past. Trimming the ledger afterwards was the
	// first attempt and is wrong: it leaves the columns the later migrations
	// added, so re-applying them fails on a duplicate and the fixture is a
	// database that never existed.
	db, err := sql.Open("sqlite", "file:"+path+"?_pragma=foreign_keys(1)")
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}
	defer db.Close()
	if _, err := db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (
		name TEXT PRIMARY KEY, applied_at INTEGER NOT NULL)`); err != nil {
		t.Fatalf("creating the ledger: %v", err)
	}
	for _, m := range migs[:n] {
		if _, err := db.ExecContext(ctx, m.sql); err != nil {
			t.Fatalf("applying %s: %v", m.name, err)
		}
		if _, err := db.ExecContext(ctx,
			`INSERT INTO schema_migrations (name, applied_at) VALUES (?, 0)`, m.name); err != nil {
			t.Fatalf("recording %s: %v", m.name, err)
		}
	}
	if err := db.Close(); err != nil {
		t.Fatalf("closing the fixture: %v", err)
	}
	return path
}

// A database from an older release has to open on this binary and come out at
// head. It is the upgrade every operator does, and until now nothing ran it.
//
// v0.1-alpha shipped one migration and v0.2-beta ten, so both are real points
// somebody's database is sitting at right now.
func TestADatabaseFromAnOlderReleaseMigratesToHead(t *testing.T) {
	ctx := context.Background()
	for _, tc := range []struct {
		name string
		at   int
	}{
		{"v0.1-alpha, which shipped 0001_init.sql alone", 1},
		{"v0.2-beta, which shipped through 0010", 10},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := atSchema(t, tc.at)

			s, err := Open(ctx, Options{Path: path})
			if err != nil {
				t.Fatalf("opening a %s database: %v", tc.name, err)
			}
			defer s.Close()

			applied, err := s.AppliedMigrations(ctx)
			if err != nil {
				t.Fatal(err)
			}
			migs, err := loadMigrations()
			if err != nil {
				t.Fatal(err)
			}
			if len(applied) != len(migs) {
				t.Fatalf("the ledger has %d migrations after the upgrade, want %d", len(applied), len(migs))
			}
			if err := s.IntegrityCheck(ctx); err != nil {
				t.Errorf("the upgraded database does not pass its integrity check: %v", err)
			}

			// And it is a working fleet database, not just a schema: the
			// columns the later migrations added are readable and writable.
			inst := &Installation{AppID: 1, InstallationID: 2, Target: "acme", TargetType: TargetOrg}
			if err := s.CreateInstallation(ctx, inst); err != nil {
				t.Fatalf("the upgraded database will not take an installation: %v", err)
			}
			if _, err := s.GetInstallation(ctx, inst.ID); err != nil {
				t.Fatalf("reading it back: %v", err)
			}
		})
	}
}

// A migration that fails has to stop startup and leave the ledger clean, so
// the next start tries it again. The failure mode this prevents is the worst
// one available: a half-applied migration recorded as done, which nothing will
// ever try again and which no later migration can assume the shape of.
func TestAFailingMigrationStopsStartupAndIsTriedAgain(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "zoomies.db")
	s, err := Open(ctx, Options{Path: path})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer s.Close()

	broken := []migration{
		{name: "9001_first_half_works.sql", sql: `CREATE TABLE half_applied (id TEXT PRIMARY KEY)`},
		{name: "9002_this_one_does_not.sql", sql: `CREATE TABLE nope (id TEXT PRIMARY KEY); THIS IS NOT SQL`},
	}
	err = s.applyMigrations(ctx, broken, map[string]bool{})
	if err == nil {
		t.Fatal("a migration full of nonsense was applied without complaint")
	}
	// Naming the file is the difference between "the database will not open"
	// and knowing which change to look at.
	if !strings.Contains(err.Error(), "9002_this_one_does_not.sql") {
		t.Errorf("the failure does not name the migration: %v", err)
	}

	applied, err := s.AppliedMigrations(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, a := range applied {
		names = append(names, a.Name)
	}
	// The one that worked is recorded and the one that failed is not: each
	// migration is its own transaction, so a failure costs only itself.
	if !contains(names, "9001_first_half_works.sql") {
		t.Errorf("the migration that succeeded was rolled back too: %v", names)
	}
	if contains(names, "9002_this_one_does_not.sql") {
		t.Fatalf("the failed migration is in the ledger, so it will never be tried again: %v", names)
	}

	// Fixed and re-run: it applies, and the one before it is not applied twice.
	fixed := []migration{
		broken[0],
		{name: "9002_this_one_does_not.sql", sql: `CREATE TABLE now_it_works (id TEXT PRIMARY KEY)`},
	}
	seen := map[string]bool{"9001_first_half_works.sql": true}
	if err := s.applyMigrations(ctx, fixed, seen); err != nil {
		t.Fatalf("the fixed migration did not apply on the re-run: %v", err)
	}
	if _, err := s.exec(ctx, `INSERT INTO now_it_works (id) VALUES ('x')`); err != nil {
		t.Errorf("the re-run did not actually create the table: %v", err)
	}
}

// A store that could not be opened must not be left half-built: Open closes
// both pools itself, so a caller that got an error has nothing to clean up and
// no file handle leaks into the next attempt.
func TestAFailedOpenLeavesNothingBehind(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "zoomies.db")
	s, err := Open(ctx, Options{Path: path})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.exec(ctx, `INSERT INTO schema_migrations (name, applied_at) VALUES (?, ?)`,
		"9999_from_the_future.sql", ms(s.Now())); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}

	bad, err := Open(ctx, Options{Path: path})
	if !errors.Is(err, ErrSchemaNewer) {
		t.Fatalf("Open = %v, want ErrSchemaNewer", err)
	}
	if bad != nil {
		t.Error("Open returned a store alongside its error, which a caller will not close")
	}
}

func contains(haystack []string, needle string) bool {
	for _, s := range haystack {
		if s == needle {
			return true
		}
	}
	return false
}

// preMigrationCopies lists the backups this package took before migrating.
func preMigrationCopies(t *testing.T, dbPath string) []string {
	t.Helper()
	entries, err := os.ReadDir(filepath.Join(filepath.Dir(dbPath), PreMigrationDir))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		t.Fatalf("reading the pre-migration directory: %v", err)
	}
	var out []string
	for _, e := range entries {
		// Only the ones this package names: anything else in there is
		// somebody's, and counting it would make the retention assertion
		// below pass or fail on a directory retention never touches.
		if e.IsDir() && strings.HasPrefix(e.Name(), BackupDirPrefix) {
			out = append(out, e.Name())
		}
	}
	sort.Strings(out)
	return out
}

// Migrations are one-way and the binary that wrote a database refuses to open
// it once a newer one has moved it on. So the moment before a migration is
// when a rollback is most likely to be wanted and least likely to have been
// prepared for -- an operator upgrading is not thinking about backups.
func TestUpgradingCopiesTheDatabaseFirst(t *testing.T) {
	ctx := context.Background()
	path := atSchema(t, 1)

	s, err := Open(ctx, Options{Path: path})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer s.Close()

	copies := preMigrationCopies(t, path)
	if len(copies) != 1 {
		t.Fatalf("the upgrade left %v pre-migration copies, want one", copies)
	}

	// It is a real database, and the layout `zoomies restore` takes: a copy
	// nobody can put back without knowing it is special is not a rollback.
	dir := filepath.Join(filepath.Dir(path), PreMigrationDir, copies[0])
	old, err := Open(ctx, Options{Path: filepath.Join(dir, BackupDBName), ReadOnly: true})
	if err != nil {
		t.Fatalf("the copy does not open: %v", err)
	}
	defer old.Close()
	if err := old.IntegrityCheck(ctx); err != nil {
		t.Errorf("the copy does not pass its integrity check: %v", err)
	}
	// And it is the database as it was *before* the upgrade, which is the
	// whole point: one migration, not the whole ledger.
	applied, err := old.AppliedMigrations(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(applied) != 1 {
		t.Errorf("the copy has %d migrations; it was taken after the upgrade, not before it", len(applied))
	}
}

// A fresh install has no rows to lose, and putting an empty copy beside every
// one of them would teach operators that the directory is noise.
func TestAFirstStartTakesNoCopy(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "zoomies.db")
	s, err := Open(ctx, Options{Path: path})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer s.Close()
	if copies := preMigrationCopies(t, path); len(copies) != 0 {
		t.Errorf("a first start left %v behind", copies)
	}
}

// A ledger that exists and is empty is what a crash between creating the table
// and applying the first migration leaves behind. There is still nothing to
// lose, and a copy of an empty database beside every such start would be noise
// -- so this is a separate case from the fresh file above, which never gets as
// far as reading the ledger at all.
func TestAnEmptyLedgerTakesNoCopyEither(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "zoomies.db")

	db, err := sql.Open("sqlite", "file:"+path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `CREATE TABLE schema_migrations (
		name TEXT PRIMARY KEY, applied_at INTEGER NOT NULL)`); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	s, err := Open(ctx, Options{Path: path})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer s.Close()
	if copies := preMigrationCopies(t, path); len(copies) != 0 {
		t.Errorf("a database with an empty ledger was copied: %v", copies)
	}
}

// Opening a database that is already at head is the ordinary case -- every
// restart -- and it must not copy the database each time.
func TestAnOpenWithNothingPendingTakesNoCopy(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "zoomies.db")
	first, err := Open(ctx, Options{Path: path})
	if err != nil {
		t.Fatal(err)
	}
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}

	again, err := Open(ctx, Options{Path: path})
	if err != nil {
		t.Fatal(err)
	}
	defer again.Close()
	if copies := preMigrationCopies(t, path); len(copies) != 0 {
		t.Errorf("restarting on an up-to-date database copied it: %v", copies)
	}
}

// Two is what is worth keeping: the upgrade that just happened, and the one
// before it -- which is the one an operator reaches for when the first went
// unnoticed. More would be a copy of the whole database per release, kept
// forever, on the disk the fleet also needs.
func TestOnlyTheLastTwoCopiesAreKept(t *testing.T) {
	ctx := context.Background()
	path := atSchema(t, 1)
	root := filepath.Join(filepath.Dir(path), PreMigrationDir)

	// Three upgrades' worth, planted with names a year apart so the order is
	// unambiguous, plus something that is not ours.
	for _, stamp := range []string{"20240101-000000", "20250101-000000", "20260101-000000"} {
		if err := os.MkdirAll(filepath.Join(root, BackupDirPrefix+stamp), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	stranger := filepath.Join(root, "notes")
	if err := os.MkdirAll(stranger, 0o700); err != nil {
		t.Fatal(err)
	}

	s, err := Open(ctx, Options{Path: path})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer s.Close()

	copies := preMigrationCopies(t, path)
	// The newest two: the one this open just took, and 20260101.
	if len(copies) != preMigrationKeep {
		t.Fatalf("kept %v, want %d", copies, preMigrationKeep)
	}
	if contains(copies, BackupDirPrefix+"20240101-000000") {
		t.Errorf("the oldest copy survived: %v", copies)
	}
	if _, err := os.Stat(stranger); err != nil {
		t.Errorf("retention removed a directory this package did not name: %v", err)
	}
}
