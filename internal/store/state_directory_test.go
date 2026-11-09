package store

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

// The storage report in internal/backup classifies every top-level entry of the
// state directory from a table, and a test there builds a directory and fails
// on anything the table does not know. That test cannot provoke a migration,
// which is the one moment the store writes something beside the database of its
// own accord, so this is the other half: it starts the store on an older
// database for real and says what is left in the directory.
//
// A later change that makes the store write one more thing fails here, naming
// it, and points at the table that has to learn it, so an operator is never told
// that something the controller made is something nobody can explain.
func TestAMigratingStartLeavesOnlyWhatTheStorageReportKnows(t *testing.T) {
	ctx := context.Background()
	path := atSchema(t, 1)
	s, err := Open(ctx, Options{Path: path})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer s.Close()

	dir := filepath.Dir(path)
	base := filepath.Base(path)
	known := map[string]bool{base: true, base + "-wal": true, base + "-shm": true, PreMigrationDir: true}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if !known[e.Name()] {
			t.Errorf("a migrating start left %q in the state directory, and the storage report does not know it: "+
				"classify it in knownEntries in internal/backup/storage.go, then add it to this list", e.Name())
		}
	}

	// The report reads a pre-migration copy as regenerable only while it is a
	// bare database with no manifest, which is how the store has always written
	// one. A manifest in there would make every copy unknown.
	copies := preMigrationCopies(t, path)
	if len(copies) != 1 {
		t.Fatalf("the upgrade left %v pre-migration copies, want one", copies)
	}
	inside, err := os.ReadDir(filepath.Join(dir, PreMigrationDir, copies[0]))
	if err != nil {
		t.Fatal(err)
	}
	if len(inside) != 1 || inside[0].Name() != BackupDBName {
		var names []string
		for _, e := range inside {
			names = append(names, e.Name())
		}
		t.Errorf("a pre-migration copy holds %v, want only %s: classifyCopy in internal/backup/storage.go "+
			"reads a copy with no manifest as the store's own", names, BackupDBName)
	}
}
