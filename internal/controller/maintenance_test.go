package controller

import (
	"database/sql"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/eyupio/zoomies/internal/backup"
	"github.com/eyupio/zoomies/internal/config"
	"github.com/eyupio/zoomies/internal/store"
)

// plantPreMigrationCopies makes the directories the store names its copies, with
// stamps a year apart so the order is unambiguous, and returns their names.
func (h *harness) plantPreMigrationCopies(stamps ...string) []string {
	h.t.Helper()
	var names []string
	for _, stamp := range stamps {
		name := store.BackupDirPrefix + stamp
		if err := os.MkdirAll(filepath.Join(h.onDisk(), store.PreMigrationDir, name), 0o700); err != nil {
			h.t.Fatal(err)
		}
		names = append(names, name)
	}
	return names
}

func (h *harness) preMigrationNames() []string {
	h.t.Helper()
	entries, err := os.ReadDir(filepath.Join(h.onDisk(), store.PreMigrationDir))
	if err != nil {
		h.t.Fatal(err)
	}
	var names []string
	for _, e := range entries {
		names = append(names, e.Name())
	}
	return names
}

// takeBackups takes n of the controller's own backups at distinct instants a day
// apart, without retention, which backup.Take leaves to its callers.
func (h *harness) takeBackups(n int) []string {
	h.t.Helper()
	var ids []string
	for i := range n {
		at := time.Date(2026, 1, 1+i, 0, 0, 0, 0, time.UTC)
		e, err := backup.Take(h.ctx, h.c.Store(), backup.TakeOptions{
			Config: h.c.Config(), Source: backup.SourceScheduled, Now: func() time.Time { return at },
		})
		if err != nil {
			h.t.Fatalf("Take: %v", err)
		}
		ids = append(ids, e.ID)
	}
	return ids
}

// Open takes a copy before a migration and prunes nothing, because it runs before
// any setting can be read. So the controller prunes as soon as it has its
// configuration, and what it prunes to is the setting and not a number compiled
// in: a higher backup.pre_migration_keep keeps the copies it was meant to keep.
func TestTheControllerPrunesThePreMigrationCopiesWhenItStartsToTheSetting(t *testing.T) {
	for _, tc := range []struct {
		keep int
		want int
	}{{keep: 5, want: 5}, {keep: 2, want: 2}} {
		h := newHarness(t)
		h.c.UpdateConfig(func(c *config.Config) { c.Backup.PreMigrationKeep = tc.keep })
		// Four older copies and the one an Open has just taken.
		all := h.plantPreMigrationCopies("20230101-000000", "20240101-000000", "20250101-000000", "20260101-000000", "20261109-120000")

		h.c.pruneAtStart()

		if left := h.preMigrationNames(); !slices.Equal(left, all[len(all)-tc.want:]) {
			t.Errorf("keep %d left %v, want the newest %d: %v", tc.keep, left, tc.want, all[len(all)-tc.want:])
		}
	}
}

// Switching the scheduled pass off must not leave copies that nothing removes, so
// the prune at start does not depend on the interval. It says what it removed.
func TestThePruneAtStartHappensWithTheScheduledPassSwitchedOffAndIsReported(t *testing.T) {
	h := newHarness(t)
	h.c.UpdateConfig(func(c *config.Config) {
		c.Maintenance.Interval = 0
		c.Backup.PreMigrationKeep = 1
	})
	all := h.plantPreMigrationCopies("20250101-000000", "20260101-000000")

	h.c.pruneAtStart()

	if left := h.preMigrationNames(); !slices.Equal(left, all[1:]) {
		t.Errorf("left %v, want only the newest %v", left, all[1:])
	}
	last := h.c.LastMaintenance()
	if last == nil || !slices.Equal(last.PreMigration, all[:1]) {
		t.Errorf("last maintenance = %+v, want the prune at start said to have removed %v", last, all[:1])
	}
}

// A prune that removes nothing says nothing: the status is for what changed.
func TestThePruneAtStartThatRemovesNothingLeavesNoLastPass(t *testing.T) {
	h := newHarness(t)
	h.plantPreMigrationCopies("20260101-000000")
	h.c.pruneAtStart()
	if last := h.c.LastMaintenance(); last != nil {
		t.Errorf("last maintenance = %+v after a prune that removed nothing", last)
	}
}

// The first pass is due as soon as the loop is running, so a controller that is
// restarted more often than its interval still gets tidied, and after it the next
// is an interval away.
func TestAScheduledPassRunsWhenDueAndNotBeforeItIsDueAgain(t *testing.T) {
	h := newHarness(t)
	h.c.UpdateConfig(func(c *config.Config) {
		c.Maintenance.Interval = time.Hour
		c.Backup.Keep = 2
	})
	ids := h.takeBackups(4)

	h.c.maintenancePass(h.ctx, false)

	last := h.c.LastMaintenance()
	if last == nil || !slices.Equal(last.Backups, ids[:2]) {
		t.Fatalf("last maintenance = %+v, want the two oldest backups removed: %v", last, ids[:2])
	}
	first := last.At

	// Inside the interval, with something to remove, nothing happens.
	h.takeBackupsAt(time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC))
	h.c.maintenancePass(h.ctx, false)
	if got := h.c.LastMaintenance(); !got.At.Equal(first) || len(got.Backups) != 2 {
		t.Errorf("a pass ran inside its interval: %+v", got)
	}

	// An interval later it is due again and takes the surplus.
	h.offset.Add(int64(2 * time.Hour))
	h.c.maintenancePass(h.ctx, false)
	if got := h.c.LastMaintenance(); !got.At.After(first) || len(got.Backups) != 1 {
		t.Errorf("the next pass = %+v, want a later pass that removed the one surplus backup", got)
	}
}

func (h *harness) takeBackupsAt(at time.Time) {
	h.t.Helper()
	if _, err := backup.Take(h.ctx, h.c.Store(), backup.TakeOptions{
		Config: h.c.Config(), Source: backup.SourceScheduled, Now: func() time.Time { return at },
	}); err != nil {
		h.t.Fatalf("Take: %v", err)
	}
}

// 0 switches the scheduled pass off, which is not the same as switching
// maintenance off: an operator who lowers backup.keep and presses the button on the
// Backups page asked for it, and gets it.
func TestAnIntervalOfZeroRunsNoScheduledPassButAnAskedForOneStillRuns(t *testing.T) {
	h := newHarness(t)
	h.c.UpdateConfig(func(c *config.Config) {
		c.Maintenance.Interval = 0
		c.Backup.Keep = 1
	})
	ids := h.takeBackups(3)

	h.c.maintenancePass(h.ctx, false)
	if last := h.c.LastMaintenance(); last != nil {
		t.Fatalf("a scheduled pass ran with the interval at 0: %+v", last)
	}

	h.c.maintenancePass(h.ctx, true)
	last := h.c.LastMaintenance()
	if last == nil || !slices.Equal(last.Backups, ids[:2]) {
		t.Errorf("an asked-for pass = %+v, want the two oldest removed: %v", last, ids[:2])
	}
}

// A backup being taken is not something to prune beside: TakeBackup runs its own
// retention when it finishes, and a prune that raced the copy could delete the one
// being written. The pass waits for the next tick instead of skipping for good.
func TestAPassWaitsForABackupInProgressAndRunsOnTheNextTick(t *testing.T) {
	h := newHarness(t)
	h.c.UpdateConfig(func(c *config.Config) {
		c.Maintenance.Interval = time.Hour
		c.Backup.Keep = 1
	})
	ids := h.takeBackups(3)

	if !h.c.beginBackup() {
		t.Fatal("could not simulate a backup in progress")
	}
	h.c.maintenancePass(h.ctx, true)
	if last := h.c.LastMaintenance(); last != nil {
		t.Fatalf("a pass ran while a backup was being taken: %+v", last)
	}
	h.c.endBackup()

	// Not due by the clock, and not asked for again: it is owed.
	h.c.maintenancePass(h.ctx, false)
	last := h.c.LastMaintenance()
	if last == nil || !slices.Equal(last.Backups, ids[:2]) {
		t.Errorf("the owed pass = %+v, want the two oldest removed: %v", last, ids[:2])
	}
}

// An operator who has just lowered backup.pre_migration_keep and presses "apply
// retention" on the Backups page asked for the copies to go now, not at the next
// tick or the next restart. That button runs the same pass the schedule does, so
// the answer they are given and what the storage report says agree at once.
func TestApplyingRetentionOnDemandAlsoTidiesThePreMigrationCopies(t *testing.T) {
	h := newHarness(t)
	h.c.UpdateConfig(func(c *config.Config) {
		c.Maintenance.Interval = 0
		c.Backup.PreMigrationKeep = 1
	})
	all := h.plantPreMigrationCopies("20250101-000000", "20260101-000000")

	if _, err := h.c.PruneBackups(h.ctx); err != nil {
		t.Fatalf("PruneBackups: %v", err)
	}

	if left := h.preMigrationNames(); !slices.Equal(left, all[1:]) {
		t.Errorf("left %v, want only the newest: the button was pressed, and the interval being 0 does not stop that", left)
	}
	if last := h.c.LastMaintenance(); last == nil || !slices.Equal(last.PreMigration, all[:1]) {
		t.Errorf("last maintenance = %+v, want it to say the oldest copy was removed", last)
	}
}

// fileStore swaps the harness's in-memory store for one in the file the
// configuration names, because compaction is about the file: a database in memory
// has no length to give back. older writes the database the way a release before
// incremental vacuum left one, a file that exists before the store opens it.
func (h *harness) fileStore(older bool) {
	h.t.Helper()
	path := h.cfg.Database.Path
	if older {
		db, err := sql.Open("sqlite", "file:"+path)
		if err != nil {
			h.t.Fatal(err)
		}
		if _, err := db.Exec(`CREATE TABLE older_marker (id INTEGER)`); err != nil {
			h.t.Fatal(err)
		}
		db.Close()
	}
	st, err := store.Open(h.ctx, store.Options{Path: path})
	if err != nil {
		h.t.Fatalf("store.Open: %v", err)
	}
	h.t.Cleanup(func() { st.Close() })
	h.c.st = st
	h.st = st
}

// churn leaves the database file about 48 MiB long with nine tenths of it free,
// as a busy week followed by retention does, through a connection of its own.
func (h *harness) churn() {
	h.t.Helper()
	db, err := sql.Open("sqlite", "file:"+h.cfg.Database.Path+"?_pragma=busy_timeout(10000)&_pragma=journal_mode(WAL)")
	if err != nil {
		h.t.Fatal(err)
	}
	defer db.Close()
	for _, q := range []string{
		`CREATE TABLE IF NOT EXISTS compaction_scratch (id INTEGER PRIMARY KEY, body BLOB)`,
		`WITH RECURSIVE n(i) AS (SELECT 1 UNION ALL SELECT i + 1 FROM n WHERE i < 12000) INSERT INTO compaction_scratch (body) SELECT zeroblob(4000) FROM n`,
		`DELETE FROM compaction_scratch WHERE id % 10 <> 0`,
		`PRAGMA wal_checkpoint(TRUNCATE)`,
	} {
		if _, err := db.Exec(q); err != nil {
			h.t.Fatalf("%s: %v", q, err)
		}
	}
}

func (h *harness) databaseBytes() int64 {
	h.t.Helper()
	info, err := os.Stat(h.cfg.Database.Path)
	if err != nil {
		h.t.Fatal(err)
	}
	return info.Size()
}

// The reason for the whole change: a database that has had a lot deleted from it is
// as long as it was, and a controller that never gives that back carries it to the
// next disk. The scheduled pass does, and says how much.
func TestTheScheduledPassGivesTheDatabasesFreeSpaceBackAndSaysSo(t *testing.T) {
	h := newHarness(t)
	h.fileStore(false)
	h.c.UpdateConfig(func(c *config.Config) { c.Maintenance.Interval = time.Hour })
	h.churn()
	before := h.databaseBytes()

	h.c.maintenancePass(h.ctx, false)

	last := h.c.LastMaintenance()
	if last == nil || last.Compaction == nil || last.Compaction.FreedBytes < before/2 {
		t.Fatalf("last maintenance = %+v, want a compaction that gave back most of the %d bytes", last, before)
	}
	if after := h.databaseBytes(); after > before/2 {
		t.Errorf("the file is %d bytes after the pass, from %d", after, before)
	}
}

// What a person asks for with a button is what the button says. Pruning copies on
// the Backups page returns when it is done, and a conversion can hold the writer
// for minutes, so only the schedule compacts.
func TestAnAskedForPassPrunesAndDoesNotCompact(t *testing.T) {
	h := newHarness(t)
	h.fileStore(false)
	h.churn()
	before := h.databaseBytes()

	h.c.maintenancePass(h.ctx, true)

	if after := h.databaseBytes(); after != before {
		t.Errorf("the file went from %d to %d bytes under a pass somebody asked for", before, after)
	}
	if last := h.c.LastMaintenance(); last != nil && last.Compaction != nil {
		t.Errorf("an asked-for pass reported a compaction: %+v", last.Compaction)
	}
}

// A conversion is something an operator opted into, so being refused is something
// they are told, and told for as long as it stands. It goes when what it names is
// put right, and a setting changed since the pass ran has already answered it.
func TestARefusedConversionIsOnTheProblemsListUntilItIsPutRight(t *testing.T) {
	h := newHarness(t)
	h.fileStore(true)
	h.c.UpdateConfig(func(c *config.Config) {
		c.Maintenance.Interval = time.Hour
		c.Maintenance.Compact = "convert"
		c.Backup.Interval = time.Hour
	})

	// No backup of this database from the last day: the rewrite is refused.
	h.c.maintenancePass(h.ctx, false)
	if codes := h.problemCodes(); !slices.Contains(codes, "maintenance.compact_skipped") {
		t.Fatalf("problems = %v, want maintenance.compact_skipped after a refused conversion", codes)
	}
	if mode := h.autoVacuumMode(); mode == store.AutoVacuumIncremental {
		t.Fatal("the database was converted without a backup to go back to")
	}

	// Asking for something else answers it at once, without waiting for a pass.
	h.c.UpdateConfig(func(c *config.Config) { c.Maintenance.Compact = "incremental" })
	if codes := h.problemCodes(); slices.Contains(codes, "maintenance.compact_skipped") {
		t.Errorf("problems = %v: the setting was changed away from convert and the entry stayed", codes)
	}
	h.c.UpdateConfig(func(c *config.Config) { c.Maintenance.Compact = "convert" })
	if codes := h.problemCodes(); !slices.Contains(codes, "maintenance.compact_skipped") {
		t.Fatalf("problems = %v, want the refusal back while it still stands", codes)
	}

	// A backup is taken, and the next pass converts and clears it.
	if _, err := backup.Take(h.ctx, h.c.Store(), backup.TakeOptions{
		Config: h.c.Config(), Source: backup.SourceScheduled, Now: h.c.Now,
	}); err != nil {
		t.Fatalf("Take: %v", err)
	}
	h.offset.Add(int64(2 * time.Hour))
	h.c.maintenancePass(h.ctx, false)

	if mode := h.autoVacuumMode(); mode != store.AutoVacuumIncremental {
		t.Errorf("auto_vacuum = %d after the pass, want the database converted now that a backup exists", mode)
	}
	if codes := h.problemCodes(); slices.Contains(codes, "maintenance.compact_skipped") {
		t.Errorf("problems = %v, want the entry gone once the database was converted", codes)
	}
	if last := h.c.LastMaintenance(); last == nil || last.Compaction == nil || !last.Compaction.Converted {
		t.Errorf("last maintenance = %+v, want it to say the database was converted", last)
	}
}

func (h *harness) autoVacuumMode() int64 {
	h.t.Helper()
	stats, err := h.c.Store().PageStats(h.ctx)
	if err != nil {
		h.t.Fatal(err)
	}
	return stats.AutoVacuum
}

// A conversion that failed holds the writer and the backup gate while it runs, so
// the scheduled pass must not start it again every interval. The failure stays on
// the problems list, and the rewrite is tried again once the back-off is over.
func TestAFailedConversionIsNotRetriedUntilTheBackOffIsOver(t *testing.T) {
	h := newHarness(t)
	h.fileStore(true)
	h.c.UpdateConfig(func(c *config.Config) {
		c.Maintenance.Interval = time.Hour
		c.Maintenance.Compact = "convert"
		c.Backup.Interval = 0
	})
	now := h.c.Now()
	failed := &backup.CompactionResult{Mode: "convert", Error: "no space left", ConversionFailed: true}
	h.c.maintenance.mu.Lock()
	h.c.maintenance.notCompacted, h.c.maintenance.notCompactedAt = failed, now.Add(-time.Hour)
	h.c.maintenance.mu.Unlock()

	if got := h.c.compact(h.ctx, h.c.cfg(), now); got != nil {
		t.Fatalf("compact = %+v an hour after a failed conversion, want it left alone", got)
	}
	if stats, err := h.st.PageStats(h.ctx); err != nil || stats.AutoVacuum == store.AutoVacuumIncremental {
		t.Fatalf("the database was converted inside the back-off (stats %+v, err %v)", stats, err)
	}
	if len(h.c.compactionProblems()) != 1 {
		t.Error("the failed conversion left the problems list during its back-off")
	}

	got := h.c.compact(h.ctx, h.c.cfg(), now.Add(conversionRetryAfter))
	if got == nil || !got.Converted {
		t.Fatalf("compact = %+v after the back-off, want the conversion tried again", got)
	}
}
