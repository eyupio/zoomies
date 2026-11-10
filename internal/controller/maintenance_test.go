package controller

import (
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
