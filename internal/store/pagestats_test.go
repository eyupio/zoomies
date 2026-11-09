package store

import (
	"context"
	"testing"
)

// The storage report says how much of the database file is free pages, and
// that number can only come from the connection that owns the file. This holds
// the arithmetic to SQLite's own: pages SQLite has freed stay in the file and
// are counted as free, which is exactly the space a delete does not give back.
func TestPageStatsCountTheFreePagesADeleteLeavesInTheFile(t *testing.T) {
	ctx := context.Background()
	s := newFileStore(t)

	before, err := s.PageStats(ctx)
	if err != nil {
		t.Fatalf("PageStats on a fresh database: %v", err)
	}
	if before.PageSize <= 0 || before.PageCount <= 0 {
		t.Fatalf("a fresh database should have pages: %+v", before)
	}

	if _, err := s.write.ExecContext(ctx, `CREATE TABLE page_stats_scratch (id INTEGER PRIMARY KEY, body BLOB)`); err != nil {
		t.Fatalf("creating the scratch table: %v", err)
	}
	// Rows of about a page each, so deleting them frees whole pages rather than
	// leaving the space inside pages that are still in use.
	if _, err := s.write.ExecContext(ctx, `
		WITH RECURSIVE n(i) AS (SELECT 1 UNION ALL SELECT i + 1 FROM n WHERE i < 200)
		INSERT INTO page_stats_scratch (body) SELECT zeroblob(?) FROM n`, before.PageSize); err != nil {
		t.Fatalf("filling the scratch table: %v", err)
	}
	grown, err := s.PageStats(ctx)
	if err != nil {
		t.Fatalf("PageStats after filling: %v", err)
	}
	if grown.PageCount <= before.PageCount {
		t.Fatalf("filling the table should have grown the file: %d pages before, %d after", before.PageCount, grown.PageCount)
	}

	if _, err := s.write.ExecContext(ctx, `DELETE FROM page_stats_scratch`); err != nil {
		t.Fatalf("emptying the scratch table: %v", err)
	}
	after, err := s.PageStats(ctx)
	if err != nil {
		t.Fatalf("PageStats after deleting: %v", err)
	}
	if after.FreelistCount == 0 {
		t.Fatalf("deleting the rows should have left free pages in the file: %+v", after)
	}
	if after.PageCount < grown.PageCount {
		t.Errorf("a delete does not shorten the file, but the page count fell from %d to %d", grown.PageCount, after.PageCount)
	}
	if got, want := after.FreeBytes(), after.FreelistCount*after.PageSize; got != want {
		t.Errorf("FreeBytes is the free pages times the page size: got %d, want %d", got, want)
	}
}
