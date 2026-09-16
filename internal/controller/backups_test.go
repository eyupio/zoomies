package controller

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/eyupio/zoomies/internal/backup"
	"github.com/eyupio/zoomies/internal/config"
)

// onDisk is the directory the harness's configuration puts the database in,
// which is where the backup directory is derived from. The harness's own store
// is in memory, and VACUUM INTO copies that fine; the configured path is what
// the loop and the staged-restore files go by.
func (h *harness) onDisk() string {
	h.t.Helper()
	return filepath.Dir(h.cfg.Database.Path)
}

// A fleet whose only backup is the one somebody remembers to take has no
// backup. The loop takes one when none is recent, leaves the schedule alone
// while one is, and keeps to backup.keep.
func TestTheScheduleTakesABackupWhenOneIsDueAndKeepsToTheCeiling(t *testing.T) {
	h := newHarness(t)
	dir := h.onDisk()
	h.c.UpdateConfig(func(c *config.Config) {
		c.Backup.Interval = time.Hour
		c.Backup.Keep = 2
	})
	root := filepath.Join(dir, backup.DefaultDirName)

	h.c.scheduledBackup(h.ctx)
	entries, err := backup.List(root)
	if err != nil || len(entries) != 1 {
		t.Fatalf("after the first pass: %d backups, %v", len(entries), err)
	}
	if entries[0].Source != backup.SourceScheduled {
		t.Errorf("source = %q, want scheduled", entries[0].Source)
	}
	status := h.c.BackupStatus()
	if status.LastError != "" || status.LastScheduledID != entries[0].ID || status.NextDueAt.Before(entries[0].TakenAt.Add(time.Hour)) {
		t.Errorf("status = %+v", status)
	}

	// Not due again yet: nothing new.
	h.c.scheduledBackup(h.ctx)
	if entries, _ := backup.List(root); len(entries) != 1 {
		t.Fatalf("a second pass inside the interval took another backup: %d", len(entries))
	}

	// Time passes; the newest is stale, so another is taken -- and a third
	// removes the oldest.
	for range 2 {
		h.offset.Add(int64(2 * time.Hour))
		// The name carries the clock's second, so two passes must not land
		// in the same one.
		time.Sleep(1100 * time.Millisecond)
		h.c.scheduledBackup(h.ctx)
	}
	entries, _ = backup.List(root)
	if len(entries) != 2 {
		t.Fatalf("keep 2 left %d backups", len(entries))
	}
	if problems := h.c.backupProblems(); len(problems) != 0 {
		t.Errorf("a working schedule raised %+v", problems)
	}
}

// A schedule that cannot write is a fleet with no backup and a page that says
// so: the failure is a problem, it is retried later rather than every minute,
// and a backup that then succeeds clears it.
func TestAFailingScheduleIsAProblemAndBacksOff(t *testing.T) {
	h := newHarness(t)
	dir := h.onDisk()
	h.c.UpdateConfig(func(c *config.Config) { c.Backup.Interval = time.Hour })
	// A file where the directory should be.
	if err := os.WriteFile(filepath.Join(dir, backup.DefaultDirName), []byte("in the way"), 0o600); err != nil {
		t.Fatal(err)
	}

	h.c.scheduledBackup(h.ctx)
	status := h.c.BackupStatus()
	if status.LastError == "" {
		t.Fatal("the failure was not recorded")
	}
	var found bool
	for _, p := range h.c.backupProblems() {
		if p.Code == "backup.failed" && p.Fix != "" {
			found = true
		}
	}
	if !found {
		t.Errorf("no backup.failed problem: %+v", h.c.backupProblems())
	}

	// Cleared out of the way, but inside the retry window: no attempt.
	if err := os.Remove(filepath.Join(dir, backup.DefaultDirName)); err != nil {
		t.Fatal(err)
	}
	h.c.scheduledBackup(h.ctx)
	if entries, _ := backup.List(filepath.Join(dir, backup.DefaultDirName)); len(entries) != 0 {
		t.Fatal("the loop retried inside its back-off")
	}
	h.offset.Add(int64(backupRetry + time.Minute))
	h.c.scheduledBackup(h.ctx)
	if status := h.c.BackupStatus(); status.LastError != "" || status.LastScheduledID == "" {
		t.Errorf("a backup that worked did not clear the failure: %+v", status)
	}
}

// Two backups at once would race for one directory name and a prune could
// delete the copy being taken. The second caller is refused, not queued.
func TestOnlyOneBackupIsTakenAtATime(t *testing.T) {
	h := newHarness(t)
	h.onDisk()
	if !h.c.beginBackup() {
		t.Fatal("could not begin")
	}
	if _, err := h.c.TakeBackup(context.Background(), backup.SourceManual, "alice"); err != ErrBackupRunning {
		t.Errorf("a second backup was not refused: %v", err)
	}
	h.c.endBackup()
	if _, err := h.c.TakeBackup(context.Background(), backup.SourceManual, "alice"); err != nil {
		t.Errorf("after the first finished: %v", err)
	}
}

// A restart is asked for once and answered once; asking again is not an
// error, and the reason given first is the one kept.
func TestARestartIsRequestedOnce(t *testing.T) {
	h := newHarness(t)
	if on, _ := h.c.Restarting(); on {
		t.Fatal("restarting before anybody asked")
	}
	h.c.RequestRestart("to apply a restore")
	h.c.RequestRestart("again")
	select {
	case <-h.c.RestartRequested():
	default:
		t.Fatal("the channel is not closed")
	}
	if on, why := h.c.Restarting(); !on || why != "to apply a restore" {
		t.Errorf("Restarting = %v, %q", on, why)
	}
}

// The staged restore and the failed restore both belong in the drawer: one is
// a fleet about to change hands, the other a change that did not happen.
func TestAStagedAndAFailedRestoreAreProblems(t *testing.T) {
	h := newHarness(t)
	h.onDisk()
	entry, err := h.c.TakeBackup(h.ctx, backup.SourceManual, "alice")
	if err != nil {
		t.Fatalf("TakeBackup: %v", err)
	}
	if _, err := backup.Stage(h.ctx, h.cfg, entry, backup.Staged{RequestedBy: "alice"}); err != nil {
		t.Fatalf("Stage: %v", err)
	}
	codes := map[string]bool{}
	for _, p := range h.c.backupProblems() {
		codes[p.Code] = true
	}
	if !codes["backup.restore_staged"] {
		t.Errorf("a staged restore is not on the list: %v", codes)
	}

	// The next start applies it. Here the database does not exist yet, which
	// a restore handles; a backup sealed with another key does not.
	if err := os.WriteFile(filepath.Join(entry.Dir, backup.ManifestName), []byte(`{"key":{"fingerprint":"not-this-one"}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	outcome, err := backup.ApplyStaged(h.ctx, h.cfg, nil)
	if err != nil || outcome == nil || outcome.OK {
		t.Fatalf("ApplyStaged = %+v, %v; want a recorded failure", outcome, err)
	}
	codes = map[string]bool{}
	for _, p := range h.c.backupProblems() {
		codes[p.Code] = true
	}
	if codes["backup.restore_staged"] || !codes["backup.restore_failed"] {
		t.Errorf("after a failed restore: %v", codes)
	}
	if err := backup.ClearOutcome(h.cfg.Database.Path); err != nil {
		t.Fatal(err)
	}
	if problems := h.c.backupProblems(); len(problems) != 0 {
		t.Errorf("dismissed and still listed: %+v", problems)
	}
}
