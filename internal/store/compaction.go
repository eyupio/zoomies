package store

import (
	"context"
	"errors"
	"fmt"
	"time"
)

// SQLite's auto_vacuum modes, as PRAGMA auto_vacuum reports them.
const (
	AutoVacuumNone        = 0
	AutoVacuumFull        = 1
	AutoVacuumIncremental = 2
)

const (
	// DefaultReclaimSliceBytes is how much one slice gives back. A slice holds
	// the one writer, so it is sized to be over in the time a heartbeat can wait:
	// the slice is a rewrite of that many bytes of pages, and eight mebibytes is a
	// fraction of a second on the disks this runs on.
	DefaultReclaimSliceBytes = 8 << 20
	// DefaultReclaimBudget bounds one pass, so that a very large file is given
	// back over several nights and not in one long one. What the budget leaves is
	// still free pages, and the next pass carries on.
	DefaultReclaimBudget = 30 * time.Second
	// DefaultMinReclaimFreeBytes is the least free space worth a pass at all. A
	// small file with a quarter of it free is a few megabytes SQLite will reuse
	// for new rows before it grows the file, and a pass that reclaimed it every
	// night would be a write a day for nothing.
	DefaultMinReclaimFreeBytes = 32 << 20
	// DefaultMinReclaimFreePercent is the share of the file that has to be free
	// before a pass acts, and the share it stops at. Stopping there and not at
	// zero leaves some free pages in the file for new rows to reuse, which is what
	// keeps the file from growing again the next time a burst of rows arrives.
	DefaultMinReclaimFreePercent = 20
)

// ErrNotIncremental is a database that was made in another auto_vacuum mode. It
// cannot be given its free pages back in slices, and only a rewrite of the whole
// file (ConvertToIncrementalVacuum) changes that.
var ErrNotIncremental = errors.New("store: this database is not in incremental auto-vacuum mode, so its free pages cannot be given back in slices")

// ReclaimOptions says how much to give back and how. The zero value of each field
// is its default.
type ReclaimOptions struct {
	// SliceBytes is how much each slice gives back.
	SliceBytes int64
	// Budget is how long a pass may go on for. It always gets one slice, so that a
	// pass makes progress however small its budget.
	Budget time.Duration
	// MinFreePercent and MinFreeBytes say when there is anything to do: the free
	// pages must be more than this share of the file and at least this many bytes
	// for a pass to start. Once it has started it goes on until the free pages are
	// down to the share, whatever the bytes: the bytes are a bar to clear, and a
	// pass that stopped when it fell under them would leave a small file holding
	// most of what it set out to give back.
	MinFreePercent int
	MinFreeBytes   int64
	// AfterSlice is called after each slice, with the writer released. It exists so
	// a test can prove that a write gets in between slices.
	AfterSlice func()
}

func (o ReclaimOptions) withDefaults() ReclaimOptions {
	if o.SliceBytes <= 0 {
		o.SliceBytes = DefaultReclaimSliceBytes
	}
	if o.Budget <= 0 {
		o.Budget = DefaultReclaimBudget
	}
	if o.MinFreePercent <= 0 {
		o.MinFreePercent = DefaultMinReclaimFreePercent
	}
	if o.MinFreeBytes <= 0 {
		o.MinFreeBytes = DefaultMinReclaimFreeBytes
	}
	return o
}

// worth says the free pages are enough to be worth starting a pass for.
func (o ReclaimOptions) worth(p PageStats) bool {
	return o.over(p) && p.FreeBytes() >= o.MinFreeBytes
}

// over says more than the share of the file is free, which is what a pass that has
// started goes on for. The share is where it stops, not zero, so that some free
// pages are left in the file for new rows to reuse.
func (o ReclaimOptions) over(p PageStats) bool {
	if p.PageCount <= 0 || p.FreelistCount <= 0 {
		return false
	}
	return p.FreelistCount*100 > p.PageCount*int64(o.MinFreePercent)
}

// ReclaimResult is what one pass gave back.
type ReclaimResult struct {
	// FreedBytes is the pages the pass removed from the file, times the page size.
	// It is counted in pages and not in the file's length at that instant, because
	// a reader holding an old snapshot stops the checkpoint that shortens the file;
	// the pages are gone all the same, and the file follows at the next checkpoint
	// after the reader has finished.
	FreedBytes int64
	// Slices is how many slices it took.
	Slices int
	// BudgetSpent says the pass stopped because its time ran out and not because
	// there was nothing left worth giving back.
	BudgetSpent bool
}

// Reclaim gives a database's free pages back to the disk, in slices.
//
// Each slice is PRAGMA incremental_vacuum through the one writer, bounded to
// about SliceBytes of pages, and the writer is released between slices: a write
// that is waiting gets in after the slice that is running and not after the whole
// pass, which is the property that makes it safe to run on a controller that is
// busy. It starts only when the free pages are worth giving back, goes on until they
// are down to the share the options name or Budget is spent, then checkpoints the log so the file really shortens.
//
// It refuses a database that is not in incremental mode.
func (s *Store) Reclaim(ctx context.Context, opts ReclaimOptions) (ReclaimResult, error) {
	if s.readOnly {
		return ReclaimResult{}, ErrReadOnly
	}
	o := opts.withDefaults()
	stats, err := s.PageStats(ctx)
	if err != nil {
		return ReclaimResult{}, err
	}
	if stats.AutoVacuum != AutoVacuumIncremental {
		return ReclaimResult{}, ErrNotIncremental
	}

	var result ReclaimResult
	startPages := stats.PageCount
	started := time.Now()
	for ready := o.worth(stats); ready; ready = o.over(stats) {
		pages := max(o.SliceBytes/stats.PageSize, 1)
		if _, err := s.exec(ctx, fmt.Sprintf("PRAGMA incremental_vacuum(%d)", pages)); err != nil {
			return result, fmt.Errorf("store: giving back free pages of %s: %w", s.path, err)
		}
		result.Slices++
		if o.AfterSlice != nil {
			o.AfterSlice()
		}
		if err := ctx.Err(); err != nil {
			return result, err
		}
		if stats, err = s.PageStats(ctx); err != nil {
			return result, err
		}
		if o.over(stats) && time.Since(started) >= o.Budget {
			result.BudgetSpent = true
			break
		}
	}

	if result.Slices > 0 {
		// Passive: it copies what it can without waiting for anybody. A reader that
		// holds an old snapshot stops it short, and the file shortens at the next
		// checkpoint once that reader has finished, so waiting here would only hold
		// the writer for a reader's sake.
		if _, err := s.exec(ctx, `PRAGMA wal_checkpoint(PASSIVE)`); err != nil {
			return result, fmt.Errorf("store: checkpointing %s after giving back free pages: %w", s.path, err)
		}
		if end, err := s.PageStats(ctx); err == nil {
			result.FreedBytes = (startPages - end.PageCount) * end.PageSize
		}
	}
	return result, nil
}

// ConvertToIncrementalVacuum rewrites a database that was made in another mode
// into incremental mode, and reports whether it did anything.
//
// It is PRAGMA auto_vacuum = INCREMENTAL followed by VACUUM, which rewrites the
// whole file and holds the one writer for as long as that takes: everything that
// writes waits, agent heartbeats and webhook ingest included. That is why it is
// only done when somebody has asked for it, and why the caller checks there is
// room on the disk and a recent backup first. VACUUM builds the new file beside the
// old and replaces it at the end, so a failure, a full disk included, leaves the
// original intact.
func (s *Store) ConvertToIncrementalVacuum(ctx context.Context) (bool, error) {
	if s.readOnly {
		return false, ErrReadOnly
	}
	stats, err := s.PageStats(ctx)
	if err != nil {
		return false, err
	}
	if stats.AutoVacuum == AutoVacuumIncremental {
		return false, nil
	}
	// Both statements on the one connection, under the one hold of the writer: the
	// mode a VACUUM writes is the one the connection was last told.
	defer s.lockWriter()()
	if _, err := s.write.ExecContext(ctx, `PRAGMA auto_vacuum = INCREMENTAL`); err != nil {
		return false, fmt.Errorf("store: asking %s for incremental auto-vacuum: %w", s.path, err)
	}
	if _, err := s.write.ExecContext(ctx, `VACUUM`); err != nil {
		return false, fmt.Errorf("store: rewriting %s into incremental auto-vacuum mode (the original file is unchanged): %w", s.path, err)
	}
	if _, err := s.write.ExecContext(ctx, `PRAGMA wal_checkpoint(PASSIVE)`); err != nil {
		return true, fmt.Errorf("store: checkpointing %s after rewriting it: %w", s.path, err)
	}
	return true, nil
}
