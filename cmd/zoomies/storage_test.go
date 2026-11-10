package main

import (
	"context"
	"strings"
	"testing"
)

// A report an operator has just pasted into a question: a database, its
// write-ahead file, one scheduled copy the controller could take again, one
// backup somebody uploaded, and a directory this release cannot name.
const storageFixture = `{
	"version": 1,
	"directory": "/var/lib/zoomies",
	"backup_directory": "/var/lib/zoomies/backups",
	"entries": [
		{"path": "zoomies.db", "bytes": 1181116006, "class": "essential"},
		{"path": "zoomies.db-wal", "bytes": 4194304, "class": "essential", "optional": true},
		{"path": "backups/zoomies-20261109-040000", "bytes": 1181116006, "class": "regenerable"},
		{"path": "backups/zoomies-20261101-000000", "bytes": 1181116006, "class": "kept"},
		{"path": "pre-migration/zoomies-20261105-100000", "bytes": 1048576, "class": "regenerable"},
		{"path": "work", "bytes": 2147483648, "class": "unknown"}
	],
	"database_bytes": 1181116006,
	"database_free_bytes": 268435456,
	"total_bytes": 5696074546,
	"disk_free_bytes": 45097156608
}`

// The operator's question is "what is this made of, and what do I have to
// carry", so the biggest entry comes first and the answer to the second
// question is a line of its own and not a sum they do in their head.
func TestStorageListsTheBiggestEntryFirstAndSaysWhatToCarry(t *testing.T) {
	srv := jsonRoutes(t, map[string]string{"/api/v1/storage": storageFixture})

	out, _ := runCLI(t, "storage", "--url", srv.URL)

	for _, want := range []string{
		"/var/lib/zoomies",
		"work", "2.0 GB", "unknown",
		"backups/zoomies-20261109-040000", "regenerable",
		"backups/zoomies-20261101-000000", "kept",
		"zoomies.db-wal", "4.0 MB", "optional",
		"5.3 GB",   // the total
		"4.2 GB",   // essential, kept and unknown: what a move carries
		"1.1 GB",   // what can be left behind
		"256.0 MB", // free pages inside the database file
		"42.0 GB",  // room left on the disk
	} {
		if !strings.Contains(out, want) {
			t.Errorf("storage must show %q:\n%s", want, out)
		}
	}
	if work, db := strings.Index(out, "\nwork"), strings.Index(out, "\nzoomies.db "); work < 0 || db < 0 || work > db {
		t.Errorf("the 2.0 GB directory should be listed before the 1.1 GB database:\n%s", out)
	}
	if !strings.Contains(out, "carry") {
		t.Errorf("storage must say what moving the controller carries:\n%s", out)
	}
	// A pre-migration copy is in the class of things to leave behind and is the
	// one the controller cannot take again, so somebody deleting for space is told.
	if !strings.Contains(out, "rollback for the last upgrade") {
		t.Errorf("storage must warn that a pre-migration copy cannot be taken again:\n%s", out)
	}
	// The key is in the configuration directory unless it was put here, and a
	// move that forgets it leaves every sealed secret unreadable.
	if !strings.Contains(out, "encryption key") {
		t.Errorf("storage must say the encryption key is listed only when it lives in this directory:\n%s", out)
	}
}

// A shape this build does not know is refused rather than misread: carrying too
// little because the classes changed meaning is the failure the version is for.
func TestStorageRefusesAReportShapeItDoesNotKnow(t *testing.T) {
	srv := jsonRoutes(t, map[string]string{
		"/api/v1/storage": strings.Replace(storageFixture, `"version": 1`, `"version": 2`, 1),
	})
	e, _, errOut := newTestEnv(t)

	if code := dispatch(context.Background(), e, []string{"storage", "--url", srv.URL}); code == exitOK {
		t.Fatal("a report with a version this build does not know should be refused")
	}
	if got := errOut.String(); !strings.Contains(got, "version 2") || !strings.Contains(got, "upgrade") {
		t.Errorf("the refusal should name the version and say what to do:\n%s", got)
	}
}

func TestStorageCanBePrintedAsTheRawReport(t *testing.T) {
	srv := jsonRoutes(t, map[string]string{"/api/v1/storage": storageFixture})

	out, _ := runCLI(t, "storage", "--url", srv.URL, "--output", "json")

	if !strings.Contains(out, `"database_free_bytes"`) || !strings.Contains(out, `"class"`) {
		t.Errorf("--output json should print the report as the controller sent it:\n%s", out)
	}
}

// An operator who has lowered a count wants to see it take effect, and one who
// has not wants to know why the directory is still large. The report carries what
// the last tidy removed and what it could not, and the command says both where the
// totals are.
func TestStorageSaysWhatTheLastTidyRemovedAndWhatItCouldNot(t *testing.T) {
	body := strings.Replace(storageFixture, `"disk_free_bytes": 45097156608`, `"disk_free_bytes": 45097156608,
		"last_maintenance": {
			"at": "2026-11-09T18:00:00Z",
			"backups": ["zoomies-20261101-000000", "zoomies-20261102-000000"],
			"pre_migration": ["zoomies-20260101-000000"],
			"errors": ["tidying the backups in /mnt/backups: permission denied"]
		}`, 1)
	srv := jsonRoutes(t, map[string]string{"/api/v1/storage": body})

	out, _ := runCLI(t, "storage", "--url", srv.URL)

	for _, want := range []string{
		"last tidy",
		"2 backups",
		"1 pre-migration copy",
		"permission denied",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("storage must show %q:\n%s", want, out)
		}
	}
}

// With nothing tidied yet there is nothing to say, and the line is not there to be
// misread as "tidied, and nothing was removed".
func TestStorageHasNoLastTidyLineBeforeThereHasBeenOne(t *testing.T) {
	srv := jsonRoutes(t, map[string]string{"/api/v1/storage": storageFixture})
	out, _ := runCLI(t, "storage", "--url", srv.URL)
	if strings.Contains(out, "last tidy") {
		t.Errorf("storage invented a last tidy:\n%s", out)
	}
}
