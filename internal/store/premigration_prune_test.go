package store

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
)

// plantPreMigrationCopies makes directories named as the store names its copies,
// with stamps a year apart so the order is unambiguous.
func plantPreMigrationCopies(t *testing.T, dbPath string, stamps ...string) string {
	t.Helper()
	root := filepath.Join(filepath.Dir(dbPath), PreMigrationDir)
	for _, stamp := range stamps {
		if err := os.MkdirAll(filepath.Join(root, BackupDirPrefix+stamp), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

// The controller reads backup.pre_migration_keep and calls this; Open never
// does. What it must get right is the order (the names carry the instant), what
// it touches (only what this package named) and what it reports, because the
// controller says what it removed.
func TestPrunePreMigrationKeepsTheNewestCopiesAndTouchesNothingElse(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "zoomies.db")
	root := plantPreMigrationCopies(t, dbPath, "20230101-000000", "20240101-000000", "20250101-000000", "20260101-000000")
	stranger := filepath.Join(root, "notes")
	if err := os.MkdirAll(stranger, 0o700); err != nil {
		t.Fatal(err)
	}
	// Named like a copy but a file, which this package never writes.
	lookalike := filepath.Join(root, BackupDirPrefix+"20270101-000000")
	if err := os.WriteFile(lookalike, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}

	removed, err := PrunePreMigration(dbPath, 2)
	if err != nil {
		t.Fatalf("PrunePreMigration: %v", err)
	}
	if want := []string{BackupDirPrefix + "20230101-000000", BackupDirPrefix + "20240101-000000"}; !slices.Equal(removed, want) {
		t.Errorf("removed %v, want the two oldest, oldest first: %v", removed, want)
	}
	if left := preMigrationCopies(t, dbPath); !slices.Equal(left, []string{BackupDirPrefix + "20250101-000000", BackupDirPrefix + "20260101-000000"}) {
		t.Errorf("left %v, want the newest two", left)
	}
	for _, p := range []string{stranger, lookalike} {
		if _, err := os.Stat(p); err != nil {
			t.Errorf("%s was removed, and this package did not name it as a copy: %v", p, err)
		}
	}

	removed, err = PrunePreMigration(dbPath, 1)
	if err != nil || len(removed) != 1 || removed[0] != BackupDirPrefix+"20250101-000000" {
		t.Errorf("keeping one removed %v, %v; want the older of the two", removed, err)
	}
}

// A copy taken before a migration is the one rollback there is. A setting that
// says keep none is a mistake to refuse, not an instruction to obey, and it must
// not be able to take the last copy.
func TestPrunePreMigrationWithANumberBelowOneRemovesNothing(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "zoomies.db")
	plantPreMigrationCopies(t, dbPath, "20250101-000000", "20260101-000000")

	for _, keep := range []int{0, -1} {
		removed, err := PrunePreMigration(dbPath, keep)
		if err != nil || len(removed) != 0 {
			t.Errorf("keep %d removed %v, %v; want nothing", keep, removed, err)
		}
	}
	if left := preMigrationCopies(t, dbPath); len(left) != 2 {
		t.Errorf("copies left = %v, want both", left)
	}
}

// Most controllers have never migrated anything with data in it, so there is no
// directory, and asking is not a fault.
func TestPrunePreMigrationWhereThereAreNoCopiesIsNotAnError(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "zoomies.db")
	if removed, err := PrunePreMigration(dbPath, 2); err != nil || removed != nil {
		t.Errorf("PrunePreMigration = %v, %v, want nothing and no error", removed, err)
	}
}
