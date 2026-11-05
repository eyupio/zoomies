package store

import (
	"context"
	"database/sql"
	"testing"
)

// The controller repeats scans of a month of job history, and a scan with no
// map of the file reads each page of it with a system call. Mapping the file
// makes those reads a copy, and they are most of what such a query costs. The
// setting is per connection, so it has to be on the writer and on every reader
// the pool opens, not only on whichever one a test happens to be handed.
func TestADatabaseFileIsMappedOnTheWriterAndOnEveryReader(t *testing.T) {
	ctx := context.Background()
	s := newFileStore(t)

	mapped := func(who string, q interface {
		QueryRowContext(context.Context, string, ...any) *sql.Row
	}) {
		t.Helper()
		var got int64
		if err := q.QueryRowContext(ctx, `PRAGMA mmap_size`).Scan(&got); err != nil {
			t.Fatalf("reading mmap_size on %s: %v", who, err)
		}
		if got != mmapSize {
			t.Errorf("%s maps up to %d bytes of the file; want %d", who, got, mmapSize)
		}
	}
	mapped("the writer", s.write)

	// Held at once, so the pool has to open several rather than hand back one.
	var conns []*sql.Conn
	for range 4 {
		c, err := s.read.Conn(ctx)
		if err != nil {
			t.Fatalf("taking a reader: %v", err)
		}
		defer c.Close()
		conns = append(conns, c)
	}
	for i, c := range conns {
		mapped("reader "+string(rune('1'+i)), c)
	}
}
