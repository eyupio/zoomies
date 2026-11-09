package api

import (
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/eyupio/zoomies/internal/backup"
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
