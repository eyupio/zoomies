package store

import (
	"context"
	"fmt"
)

// PageStats is how the database file is laid out: how big a page is, how many
// the file has, and how many of those SQLite has freed and kept.
//
// It exists for the storage report. The file's length says how much disk the
// database takes, and the free pages say how much of that a delete has already
// given up without the file shrinking, so an operator can tell a database that
// is large from one that is merely large because it once was.
type PageStats struct {
	PageSize      int64
	PageCount     int64
	FreelistCount int64
}

// FreeBytes is the space inside the file that holds no data: the free pages
// times the page size. Deleting rows moves pages here, and nothing moves them
// back out of the file.
func (p PageStats) FreeBytes() int64 { return p.FreelistCount * p.PageSize }

// PageStats reads the file's page counts through the reader. It takes no write
// lock, so asking is free for everything that writes.
func (s *Store) PageStats(ctx context.Context) (PageStats, error) {
	var p PageStats
	for _, q := range []struct {
		pragma string
		into   *int64
	}{
		{"page_size", &p.PageSize},
		{"page_count", &p.PageCount},
		{"freelist_count", &p.FreelistCount},
	} {
		// The pragma names are constants of this function, never input, which is
		// the only reason it is safe to build the statement from one.
		if err := s.read.QueryRowContext(ctx, "PRAGMA "+q.pragma).Scan(q.into); err != nil {
			return PageStats{}, fmt.Errorf("store: reading %s of %s: %w", q.pragma, s.path, err)
		}
	}
	return p, nil
}
