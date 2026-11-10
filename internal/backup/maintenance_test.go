package backup

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/eyupio/zoomies/internal/config"
	"github.com/eyupio/zoomies/internal/store"
)

// maintainable is a configuration whose database is in a directory of its own,
// which is all a maintenance pass reads: it never opens the database, so these
// tests plant what it prunes by hand and stay fast.
func maintainable(t *testing.T) *config.Config {
	t.Helper()
	cfg := config.Default()
	cfg.Database.Path = filepath.Join(t.TempDir(), "state", "zoomies.db")
	if err := os.MkdirAll(filepath.Dir(cfg.Database.Path), 0o700); err != nil {
		t.Fatal(err)
	}
	return cfg
}

// plantBackup writes the least that Prune counts as a backup the controller took:
// a directory with a manifest and a database file.
func plantBackup(t *testing.T, root, stamp, source string) string {
	t.Helper()
	id := DirPrefix + stamp
	dir := filepath.Join(root, id)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, DBName), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := writeManifest(dir, &Manifest{ManifestVersion: ManifestVersion, TakenAt: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), Source: source}); err != nil {
		t.Fatal(err)
	}
	return id
}

func plantPreMigration(t *testing.T, cfg *config.Config, stamps ...string) []string {
	t.Helper()
	var names []string
	for _, stamp := range stamps {
		name := store.BackupDirPrefix + stamp
		if err := os.MkdirAll(filepath.Join(PreMigrationDir(cfg.Database.Path), name), 0o700); err != nil {
			t.Fatal(err)
		}
		names = append(names, name)
	}
	return names
}

func preMigrationNames(t *testing.T, cfg *config.Config) []string {
	t.Helper()
	entries, err := os.ReadDir(PreMigrationDir(cfg.Database.Path))
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, e := range entries {
		names = append(names, e.Name())
	}
	return names
}

// One pass tidies both directories to the numbers the settings give, and says
// what it removed: that sentence is what the controller logs and what the
// storage report shows as the last pass.
func TestAMaintenancePassTidiesBothDirectoriesAndSaysWhatItRemoved(t *testing.T) {
	cfg := maintainable(t)
	cfg.Backup.Keep = 2
	cfg.Backup.PreMigrationKeep = 1
	root := Dir(cfg)
	// Two are removed, so the order they are reported in is held too: oldest
	// first, as the pre-migration copies are, whatever order Prune removes them in.
	oldest := plantBackup(t, root, "20251231-000000", SourceScheduled)
	older := plantBackup(t, root, "20260101-000000", SourceScheduled)
	middle := plantBackup(t, root, "20260102-000000", SourceManual)
	newest := plantBackup(t, root, "20260103-000000", SourceScheduled)
	uploaded := plantBackup(t, root, "20250101-000000", SourceUploaded)
	copies := plantPreMigration(t, cfg, "20240101-000000", "20250101-000000", "20260101-000000")
	now := time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC)

	result := Maintain(cfg, now)

	if !result.At.Equal(now) {
		t.Errorf("At = %v, want the instant the pass was given", result.At)
	}
	if want := []string{oldest, older}; !slices.Equal(result.Backups, want) {
		t.Errorf("removed backups %v, want the two oldest of the fleet's own, oldest first: %v (kept %s, %s)", result.Backups, want, middle, newest)
	}
	if !slices.Equal(result.PreMigration, copies[:2]) {
		t.Errorf("removed pre-migration copies %v, want the two oldest %v", result.PreMigration, copies[:2])
	}
	if len(result.Errors) != 0 {
		t.Errorf("errors = %v, want none", result.Errors)
	}
	if left := preMigrationNames(t, cfg); !slices.Equal(left, copies[2:]) {
		t.Errorf("pre-migration copies left = %v, want the newest only", left)
	}
	if _, err := Get(root, uploaded); err != nil {
		t.Errorf("a pass removed an uploaded backup, which retention never deletes: %v", err)
	}
	if _, err := Get(root, newest); err != nil {
		t.Errorf("a pass removed the newest backup: %v", err)
	}
}

// Every list in the result is an array and never null, so a client reads "nothing
// was removed" without a special case.
func TestAMaintenancePassThatRemovesNothingReportsEmptyListsNotNull(t *testing.T) {
	cfg := maintainable(t)
	result := Maintain(cfg, time.Now())
	if result.Backups == nil || result.PreMigration == nil || result.Errors == nil {
		t.Errorf("lists = %#v, want empty and not nil", result)
	}
}

// backup.keep of zero means keep every one. That has to survive the pass: it is
// the setting an operator uses to turn retention off.
func TestAMaintenancePassWithBackupKeepZeroRemovesNoBackups(t *testing.T) {
	cfg := maintainable(t)
	cfg.Backup.Keep = 0
	root := Dir(cfg)
	for _, stamp := range []string{"20260101-000000", "20260102-000000", "20260103-000000"} {
		plantBackup(t, root, stamp, SourceScheduled)
	}
	if result := Maintain(cfg, time.Now()); len(result.Backups) != 0 {
		t.Errorf("keep 0 removed %v", result.Backups)
	}
}

// What cannot be done must not stop what can. A directory that fails is reported
// and the other is still tidied, and both are in the answer, because a pass that
// gave up at its first error would leave the second directory growing for as long
// as the first stayed broken.
func TestAMaintenancePassThatFailsOnOneDirectoryStillTriesTheOther(t *testing.T) {
	t.Run("the backups cannot be read", func(t *testing.T) {
		cfg := maintainable(t)
		cfg.Backup.PreMigrationKeep = 1
		// A file where the directory should be: reading it is an error that is not
		// "there is nothing here".
		if err := os.WriteFile(Dir(cfg), []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
		copies := plantPreMigration(t, cfg, "20250101-000000", "20260101-000000")

		result := Maintain(cfg, time.Now())

		if len(result.Errors) != 1 || !strings.Contains(result.Errors[0], "backups") {
			t.Errorf("errors = %v, want one that names the backups", result.Errors)
		}
		if !slices.Equal(result.PreMigration, copies[:1]) {
			t.Errorf("the pre-migration copies were not tidied after the backups failed: removed %v", result.PreMigration)
		}
	})
	t.Run("the pre-migration copies cannot be read", func(t *testing.T) {
		cfg := maintainable(t)
		cfg.Backup.Keep = 1
		root := Dir(cfg)
		oldest := plantBackup(t, root, "20260101-000000", SourceScheduled)
		plantBackup(t, root, "20260102-000000", SourceScheduled)
		if err := os.WriteFile(PreMigrationDir(cfg.Database.Path), []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}

		result := Maintain(cfg, time.Now())

		if len(result.Errors) != 1 || !strings.Contains(result.Errors[0], "pre-migration") {
			t.Errorf("errors = %v, want one that names the pre-migration copies", result.Errors)
		}
		if !slices.Equal(result.Backups, []string{oldest}) {
			t.Errorf("the backups were not tidied after the pre-migration copies failed: removed %v", result.Backups)
		}
	})
}
