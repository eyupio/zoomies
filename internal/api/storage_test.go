package api

import (
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/eyupio/zoomies/internal/backup"
	"github.com/eyupio/zoomies/internal/config"
	"github.com/eyupio/zoomies/internal/store"
)

// The storage report is the answer to "what is in this controller's state
// directory, and which of it could I leave behind". The handler is transport
// over backup.Storage, so what is held here is that the route says it about the
// directory the running controller really uses, as a caller can read it, and
// that a backup taken a moment ago shows up as one it could make again.
func TestTheStorageReportSaysWhatTheRunningControllerHasInItsStateDirectory(t *testing.T) {
	h := newHarness(t)
	h.installation()
	cookie := h.platform()
	taken := h.takeBackup(t, cookie)
	// The harness runs the store in memory, so nothing is at the path the
	// configuration names for the database. The test puts a file of a known size
	// there, because what is held here is that the handler reports the directory
	// the controller is configured with, not what SQLite writes (the report's own
	// tests, and the store's, hold that).
	dbPath := h.ctrl.DatabasePath()
	const dbSize = 8192
	if err := os.WriteFile(dbPath, make([]byte, dbSize), 0o600); err != nil {
		t.Fatal(err)
	}

	resp := h.do(request{method: http.MethodGet, path: "/api/v1/storage", cookie: cookie})
	resp.mustStatus(t, http.StatusOK, "read the storage report")
	var report backup.StorageReport
	resp.into(t, &report)

	if report.Version != backup.StorageVersion {
		t.Errorf("version = %d, want %d", report.Version, backup.StorageVersion)
	}
	if report.Directory != filepath.Dir(dbPath) {
		t.Errorf("directory = %q, want the one the controller's database is in, %q", report.Directory, filepath.Dir(dbPath))
	}
	if report.BackupDirectory != h.ctrl.BackupDir() {
		t.Errorf("backup directory = %q, want %q", report.BackupDirectory, h.ctrl.BackupDir())
	}

	byPath := map[string]backup.StorageEntry{}
	var sum int64
	for _, e := range report.Entries {
		byPath[e.Path] = e
		sum += e.Bytes
	}
	if db := byPath[filepath.Base(dbPath)]; db.Class != backup.ClassEssential || db.Bytes != dbSize {
		t.Errorf("the database = %+v, want essential and %d bytes", db, dbSize)
	}
	if b := byPath["backups/"+taken.ID]; b.Class != backup.ClassRegenerable || b.Bytes == 0 {
		t.Errorf("the backup just taken = %+v, want regenerable: the controller could take another", b)
	}
	if sum != report.TotalBytes {
		t.Errorf("the entries add up to %d but the report says %d", sum, report.TotalBytes)
	}
	if report.DatabaseBytes != dbSize {
		t.Errorf("database bytes = %d, want the file's %d", report.DatabaseBytes, dbSize)
	}
	if report.DatabaseFreeBytes < 0 {
		t.Errorf("free bytes = %d", report.DatabaseFreeBytes)
	}
}

// The report says what the last tidy of the state directory removed, so an
// operator who lowered a count can see it take effect, and one who did not can see
// why the directory is still large. Pressing "apply retention" runs the same pass
// the schedule does, and the next read of the report has its answer.
func TestTheStorageReportSaysWhatTheLastTidyRemoved(t *testing.T) {
	h := newHarness(t)
	cookie := h.platform()
	h.ctrl.UpdateConfig(func(c *config.Config) { c.Backup.PreMigrationKeep = 1 })
	dir := filepath.Dir(h.ctrl.DatabasePath())
	var planted []string
	for _, stamp := range []string{"20250101-000000", "20260101-000000"} {
		name := store.BackupDirPrefix + stamp
		if err := os.MkdirAll(filepath.Join(dir, store.PreMigrationDir, name), 0o700); err != nil {
			t.Fatal(err)
		}
		planted = append(planted, name)
	}

	// Before anything has been tidied there is nothing to say.
	before := h.do(request{method: http.MethodGet, path: "/api/v1/storage", cookie: cookie})
	before.mustStatus(t, http.StatusOK, "read the storage report")
	var first backup.StorageReport
	before.into(t, &first)
	if first.LastMaintenance != nil {
		t.Fatalf("last_maintenance = %+v before any tidy, want none", first.LastMaintenance)
	}

	h.do(request{method: http.MethodPost, path: "/api/v1/backups/prune", cookie: cookie}).mustStatus(t, http.StatusOK, "apply retention")

	after := h.do(request{method: http.MethodGet, path: "/api/v1/storage", cookie: cookie})
	after.mustStatus(t, http.StatusOK, "read the storage report")
	var report backup.StorageReport
	after.into(t, &report)
	if report.LastMaintenance == nil {
		t.Fatal("the report does not say anything was tidied")
	}
	if got := report.LastMaintenance.PreMigration; len(got) != 1 || got[0] != planted[0] {
		t.Errorf("last_maintenance.pre_migration = %v, want the older copy %q", got, planted[0])
	}
	if report.LastMaintenance.At.IsZero() {
		t.Error("last_maintenance.at is not set")
	}
	// The copy that was kept is still an entry, and the one removed is not.
	for _, e := range report.Entries {
		if e.Path == "pre-migration/"+planted[0] {
			t.Errorf("%s is still listed after it was removed", e.Path)
		}
	}
}
