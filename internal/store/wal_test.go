package store

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The write-ahead log grows while a reader holds an old snapshot, because a
// checkpoint cannot move pages past what that reader may still need. That is
// SQLite working as designed. What must not happen is the file staying at its
// high-water mark for ever afterwards: a backup verification or a long report
// on a busy fleet would leave a log hundreds of megabytes long on the disk the
// runners' images and caches share, for the life of the process.
func TestTheWriteAheadLogShrinksOnceTheReaderHoldingItBackIsDone(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "zoomies.db")
	s, err := Open(ctx, Options{Path: path})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { s.Close() })

	if _, err := s.exec(ctx, `CREATE TABLE wal_probe (id INTEGER PRIMARY KEY, body TEXT NOT NULL)`); err != nil {
		t.Fatal(err)
	}
	body := strings.Repeat("x", 4096)
	write := func(n int) {
		t.Helper()
		for range n {
			if _, err := s.exec(ctx, `INSERT INTO wal_probe (body) VALUES (?)`, body); err != nil {
				t.Fatal(err)
			}
		}
	}
	walSize := func() int64 {
		t.Helper()
		fi, err := os.Stat(path + "-wal")
		if err != nil {
			return 0
		}
		return fi.Size()
	}

	// A reader holds a snapshot open while a good deal is written: a query the
	// way the store makes them, one statement on the read pool, still being
	// read. (Not a transaction on the read pool -- both pools share a DSN that
	// begins every transaction IMMEDIATE, so one would take the write lock.)
	write(10)
	rows, err := s.read.QueryContext(ctx, `SELECT id FROM wal_probe`)
	if err != nil {
		t.Fatal(err)
	}
	if !rows.Next() {
		t.Fatal("the probe table is empty")
	}
	write(6000)
	grown := walSize()
	if err := rows.Close(); err != nil {
		t.Fatal(err)
	}

	// The reader is gone; ordinary writing carries on, and the automatic
	// checkpoints it triggers can now reach the end of the log.
	write(3000)
	settled := walSize()
	t.Logf("log while held: %d MB; after the reader finished and writing went on: %d MB", grown>>20, settled>>20)

	if grown < 16<<20 {
		t.Fatalf("the log only reached %d MB while held; the fixture has not reproduced a large one", grown>>20)
	}
	if settled > walSizeLimit {
		t.Errorf("the log is still %d MB after the reader that held it back finished, against %d MB while held and a limit of %d MB; it never gives the space back", settled>>20, grown>>20, walSizeLimit>>20)
	}
}
