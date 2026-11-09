package store

import (
	"context"
	"os"
	"testing"
	"time"
)

// fillAndEmpty puts about 48 MiB of rows in a scratch table and deletes nine in
// ten, which is what retention does to a database that once held a busy week: the
// rows are gone and the file is not smaller. It returns the file's size with the
// free pages still in it.
func fillAndEmpty(t *testing.T, s *Store) int64 {
	t.Helper()
	ctx := context.Background()
	if _, err := s.write.ExecContext(ctx, `CREATE TABLE compaction_scratch (id INTEGER PRIMARY KEY, body BLOB)`); err != nil {
		t.Fatal(err)
	}
	if _, err := s.write.ExecContext(ctx, `
		WITH RECURSIVE n(i) AS (SELECT 1 UNION ALL SELECT i + 1 FROM n WHERE i < 12000)
		INSERT INTO compaction_scratch (body) SELECT zeroblob(4000) FROM n`); err != nil {
		t.Fatal(err)
	}
	if _, err := s.write.ExecContext(ctx, `DELETE FROM compaction_scratch WHERE id % 10 <> 0`); err != nil {
		t.Fatal(err)
	}
	if _, err := s.write.ExecContext(ctx, `PRAGMA wal_checkpoint(TRUNCATE)`); err != nil {
		t.Fatal(err)
	}
	return fileSize(t, s.path)
}

func fileSize(t *testing.T, path string) int64 {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	return info.Size()
}

func scratchRows(t *testing.T, s *Store) int {
	t.Helper()
	var n int
	if err := s.read.QueryRow(`SELECT count(*) FROM compaction_scratch`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

// A database that gives space back has to be made that way before it holds a
// table, because the mode lives in the file header and cannot be set later without
// rewriting the file. So a database created by this build is incremental, and one
// that already exists is left in whatever mode it was made in: switching it is a
// rewrite that holds the writer, which only an operator may ask for.
func TestAFreshDatabaseIsInIncrementalVacuumModeAndAnOlderOneIsLeftAlone(t *testing.T) {
	ctx := context.Background()

	fresh := newFileStore(t)
	stats, err := fresh.PageStats(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if stats.AutoVacuum != AutoVacuumIncremental {
		t.Errorf("a new database is in auto_vacuum mode %d, want %d (incremental)", stats.AutoVacuum, AutoVacuumIncremental)
	}

	// atSchema writes a database the way an older release left one.
	older, err := Open(ctx, Options{Path: atSchema(t, 1)})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer older.Close()
	stats, err = older.PageStats(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if stats.AutoVacuum != AutoVacuumNone {
		t.Errorf("an existing database was switched to auto_vacuum mode %d by being opened; only a rewrite the operator asked for may do that", stats.AutoVacuum)
	}
	// SQLite ignores a requested mode on a database that has tables until the next
	// VACUUM, and then applies it. So opening must not have asked: anything that ever
	// vacuums an existing database would otherwise convert it without being told to.
	if _, err := older.write.ExecContext(ctx, `VACUUM`); err != nil {
		t.Fatal(err)
	}
	if stats, err = older.PageStats(ctx); err != nil || stats.AutoVacuum != AutoVacuumNone {
		t.Errorf("a VACUUM converted an existing database to mode %d (%v): opening it must not have requested incremental mode", stats.AutoVacuum, err)
	}
}

// This is the test that proves the mechanism, and it is the one the part stands
// on: delete most of what a database holds, give the space back, and the file is
// smaller. Everything else here is about doing that without hurting anything.
func TestReclaimGivesTheSpaceADeleteLeavesBackToTheDisk(t *testing.T) {
	ctx := context.Background()
	s := newFileStore(t)
	before := fillAndEmpty(t, s)
	rows := scratchRows(t, s)

	result, err := s.Reclaim(ctx, ReclaimOptions{MinFreePercent: 20, MinFreeBytes: 1 << 20})
	if err != nil {
		t.Fatalf("Reclaim: %v", err)
	}

	after := fileSize(t, s.path)
	if after > before/2 {
		t.Errorf("the file is %d bytes after reclaiming, from %d: nine tenths of it was free, so it should be well under half", after, before)
	}
	if result.FreedBytes < before/2 {
		t.Errorf("FreedBytes = %d, want most of the %d the file lost", result.FreedBytes, before-after)
	}
	if result.Slices < 2 {
		t.Errorf("Slices = %d: the work is meant to be done in slices, so a writer can get in between", result.Slices)
	}
	if got := scratchRows(t, s); got != rows {
		t.Errorf("rows = %d after reclaiming, want the %d that were there: compaction must not change what a database holds", got, rows)
	}
}

// Nothing is done for a little free space: a pass that reclaimed a few pages
// every night would be a write a day for nothing, and SQLite reuses free pages for
// new rows before it grows the file.
func TestReclaimLeavesAFileWithLittleFreeSpaceAlone(t *testing.T) {
	ctx := context.Background()
	s := newFileStore(t)
	before := fillAndEmpty(t, s)

	for name, opts := range map[string]ReclaimOptions{
		"too small to be worth it": {MinFreePercent: 20, MinFreeBytes: 1 << 30},
		"too small a share":        {MinFreePercent: 99, MinFreeBytes: 1 << 20},
	} {
		result, err := s.Reclaim(ctx, opts)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if result.Slices != 0 || result.FreedBytes != 0 || fileSize(t, s.path) != before {
			t.Errorf("%s: reclaimed %+v and the file went from %d to %d, want nothing done", name, result, before, fileSize(t, s.path))
		}
	}
}

// The reason this is done in slices is that each one holds the one writer, and
// heartbeats, webhooks and the scheduler all wait for it. So the writer must be
// let go between slices, and a write issued then must get in. The hook runs
// between slices on the goroutine that is reclaiming, so if the writer were still
// held, the write inside it would never return.
func TestAWriteGetsInBetweenSlices(t *testing.T) {
	ctx := context.Background()
	s := newFileStore(t)
	fillAndEmpty(t, s)
	var observed int
	s.ObserveWrites(func(_, _ time.Duration) { observed++ })

	inBetween := 0
	result, err := s.Reclaim(ctx, ReclaimOptions{
		MinFreePercent: 20, MinFreeBytes: 1 << 20,
		AfterSlice: func() {
			inBetween++
			done := make(chan error, 1)
			go func() {
				_, err := s.exec(ctx, `INSERT INTO compaction_scratch (body) VALUES (zeroblob(10))`)
				done <- err
			}()
			select {
			case err := <-done:
				if err != nil {
					t.Errorf("a write between slices failed: %v", err)
				}
			case <-time.After(5 * time.Second):
				t.Error("a write issued between two slices did not complete: the writer is being held across them")
			}
		},
	})
	if err != nil {
		t.Fatalf("Reclaim: %v", err)
	}
	if inBetween == 0 || inBetween != result.Slices {
		t.Errorf("the hook ran %d times for %d slices", inBetween, result.Slices)
	}
	// Every slice, and every write between them, is a write the observer saw: the
	// writer is taken through the same door as everything else.
	if observed < result.Slices+inBetween {
		t.Errorf("the write observer saw %d writes, want at least the %d slices and the %d writes between them", observed, result.Slices, inBetween)
	}
}

// A pass has a budget so it cannot hold up the controller for as long as a large
// file takes, and the space not yet reclaimed is still free for the next pass.
func TestReclaimStopsAtItsTimeBudget(t *testing.T) {
	ctx := context.Background()
	s := newFileStore(t)
	before := fillAndEmpty(t, s)

	result, err := s.Reclaim(ctx, ReclaimOptions{MinFreePercent: 1, MinFreeBytes: 1, Budget: time.Nanosecond})
	if err != nil {
		t.Fatalf("Reclaim: %v", err)
	}
	if !result.BudgetSpent {
		t.Errorf("result = %+v, want it to say the budget was spent", result)
	}
	if result.Slices != 1 {
		t.Errorf("Slices = %d, want the one slice a pass always gets so that it always makes progress", result.Slices)
	}
	if fileSize(t, s.path) > before {
		t.Error("the file grew")
	}
}

// A database that is not incremental cannot be reclaimed, and says so rather than
// doing nothing in silence: the caller needs to tell "nothing to do" from "cannot".
func TestReclaimRefusesADatabaseThatIsNotIncremental(t *testing.T) {
	ctx := context.Background()
	older, err := Open(ctx, Options{Path: atSchema(t, 1)})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer older.Close()
	if _, err := older.Reclaim(ctx, ReclaimOptions{}); err != ErrNotIncremental {
		t.Errorf("Reclaim on an older database = %v, want ErrNotIncremental", err)
	}
}

// The conversion is the rewrite that moves an older database into incremental
// mode, and it is the one thing here that holds the writer for as long as it takes.
// What it must not do is lose a row.
func TestConvertingAnOlderDatabaseMakesItIncrementalAndShortensIt(t *testing.T) {
	ctx := context.Background()
	older, err := Open(ctx, Options{Path: atSchema(t, 1)})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer older.Close()
	before := fillAndEmpty(t, older)
	rows := scratchRows(t, older)

	converted, err := older.ConvertToIncrementalVacuum(ctx)
	if err != nil {
		t.Fatalf("ConvertToIncrementalVacuum: %v", err)
	}
	if !converted {
		t.Error("converted = false for a database that was not incremental")
	}

	stats, err := older.PageStats(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if stats.AutoVacuum != AutoVacuumIncremental {
		t.Errorf("auto_vacuum = %d after converting, want %d", stats.AutoVacuum, AutoVacuumIncremental)
	}
	if after := fileSize(t, older.path); after > before/2 {
		t.Errorf("the file is %d bytes after converting, from %d: the rewrite should have dropped the free pages", after, before)
	}
	if got := scratchRows(t, older); got != rows {
		t.Errorf("rows = %d after converting, want %d", got, rows)
	}

	// Converting what is already incremental is not a rewrite.
	again, err := older.ConvertToIncrementalVacuum(ctx)
	if err != nil || again {
		t.Errorf("converting twice = %v, %v, want a no-op that says nothing was converted", again, err)
	}
}

// A copy taken by VACUUM INTO is how a backup is made, and the copy must come back
// as incremental as the original was, or a restore would quietly put the database
// back to a mode that gives nothing back. It is read here as a restore reads it.
func TestABackupOfAnIncrementalDatabaseIsIncrementalWhenItIsOpenedAgain(t *testing.T) {
	ctx := context.Background()
	s := newFileStore(t)
	dest := t.TempDir() + "/copy/zoomies.db"
	if err := s.Backup(ctx, dest); err != nil {
		t.Fatalf("Backup: %v", err)
	}
	restored, err := Open(ctx, Options{Path: dest})
	if err != nil {
		t.Fatalf("opening the copy as a restore does: %v", err)
	}
	defer restored.Close()
	stats, err := restored.PageStats(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if stats.AutoVacuum != AutoVacuumIncremental {
		t.Errorf("a backup of an incremental database opens in mode %d, want %d", stats.AutoVacuum, AutoVacuumIncremental)
	}
}

// The least free space worth a pass decides whether a pass starts, not where it
// stops. A file that clears the bar is given back down to the free share, and not
// left holding 32 MiB of free pages because that was the bar: on a 50 MB file that
// would be two thirds of it.
func TestAPassThatStartsGivesBackDownToTheShareAndNotDownToTheMinimum(t *testing.T) {
	ctx := context.Background()
	s := newFileStore(t)
	before := fillAndEmpty(t, s)

	// The file holds about 45 MB of free pages and the bar is 32 MiB, so it starts.
	if _, err := s.Reclaim(ctx, ReclaimOptions{MinFreePercent: 20, MinFreeBytes: 32 << 20}); err != nil {
		t.Fatalf("Reclaim: %v", err)
	}

	stats, err := s.PageStats(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if stats.FreelistCount*100 > stats.PageCount*20 {
		t.Errorf("%d of %d pages are still free after a pass that started: it stopped above the 20%% share, at the minimum (file %d from %d)", stats.FreelistCount, stats.PageCount, fileSize(t, s.path), before)
	}
}
