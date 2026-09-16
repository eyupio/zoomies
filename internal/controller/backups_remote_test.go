package controller

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/eyupio/zoomies/internal/backup"
	"github.com/eyupio/zoomies/internal/config"
)

// offsite points the harness's fleet at a fake object store and returns it.
func offsite(h *harness, adjust func(*config.BackupRemote)) *backup.FakeS3 {
	h.t.Helper()
	fake := backup.NewFakeS3("backups")
	h.t.Cleanup(fake.Close)
	remote := config.BackupRemote{
		Name: "offsite", Endpoint: fake.Endpoint(), Bucket: fake.Bucket(), Prefix: "fleet",
		AccessKeyID: "AKIAEXAMPLE", SecretAccessKey: "secret",
	}
	if adjust != nil {
		adjust(&remote)
	}
	h.c.UpdateConfig(func(c *config.Config) {
		c.Backup.Interval = time.Hour
		c.Backup.Remotes = []config.BackupRemote{remote}
	})
	return fake
}

// The copy the schedule takes has to reach the bucket, because a backup on the
// disk that just died is not a backup.
func TestAScheduledBackupIsCopiedToTheRemote(t *testing.T) {
	h := newHarness(t)
	fake := offsite(h, nil)

	h.c.scheduledBackup(h.ctx)
	entries, err := backup.List(filepath.Join(h.onDisk(), backup.DefaultDirName))
	if err != nil || len(entries) != 1 {
		t.Fatalf("the schedule took %d backups, %v", len(entries), err)
	}
	// The loop nudges rather than uploading inside TakeBackup, so that an
	// operator pressing the button is not holding a browser open for the
	// length of a transfer. The pass is what sends it.
	if _, err := h.c.ShipBackups(h.ctx); err != nil {
		t.Fatalf("ShipBackups: %v", err)
	}

	keys := fake.Keys()
	if len(keys) != 1 || keys[0] != "fleet/"+entries[0].ID+".tar.gz" {
		t.Fatalf("the bucket holds %v, wanted the backup that was just taken", keys)
	}
	status := h.c.BackupRemotes()
	if len(status) != 1 {
		t.Fatalf("the page would show %d remotes", len(status))
	}
	if status[0].LastError != "" || status[0].LastUploadID != entries[0].ID || status[0].Copies != 1 {
		t.Errorf("the remote reports %+v after a successful copy", status[0])
	}

	// And a second pass sends nothing: the bucket already holds it, and a
	// remote that re-uploaded every backup every hour would cost money for
	// nothing.
	if _, err := h.c.ShipBackups(h.ctx); err != nil {
		t.Fatalf("the second pass: %v", err)
	}
	if fake.Puts() != 1 {
		t.Errorf("the bucket was written to %d times for one backup", fake.Puts())
	}
}

// A remote that was unreachable while two backups were taken is two behind,
// and the pass that finds it working again has to send both. A fleet whose
// offsite copy silently skips the nights the bucket was down has a gap nobody
// knows about.
func TestARemoteThatWasDownIsCaughtUpWithEveryBackupItMissed(t *testing.T) {
	h := newHarness(t)
	fake := offsite(h, nil)
	fake.BreakWith(403, "AccessDenied", "the policy says no")

	root := filepath.Join(h.onDisk(), backup.DefaultDirName)
	for range 2 {
		h.c.scheduledBackup(h.ctx)
		if _, err := h.c.ShipBackups(h.ctx); err == nil {
			t.Fatal("the pass reported success against a bucket that refused it")
		}
		h.offset.Add(int64(2 * time.Hour))
		// The backup's name carries the clock's second, so two must not land
		// inside one.
		time.Sleep(1100 * time.Millisecond)
	}

	entries, err := backup.List(root)
	if err != nil || len(entries) != 2 {
		t.Fatalf("the schedule took %d backups while the bucket was down, %v", len(entries), err)
	}
	// The failure is on the page and in the drawer, not only in a log line.
	if status := h.c.BackupRemotes(); status[0].LastError == "" {
		t.Error("a remote that is refusing every request reports no error")
	}
	codes := []string{}
	for _, p := range h.c.remoteProblems() {
		codes = append(codes, p.Code)
	}
	if len(codes) != 1 || codes[0] != "backup.remote_failed" {
		t.Errorf("the problems drawer says %v about a failing remote", codes)
	}

	fake.Mend()
	if _, err := h.c.ShipBackups(h.ctx); err != nil {
		t.Fatalf("the pass after the bucket came back: %v", err)
	}
	if got := fake.Count(); got != 2 {
		t.Errorf("the bucket holds %d copies; the catch-up was meant to send both backups it missed", got)
	}
	if status := h.c.BackupRemotes(); status[0].LastError != "" {
		t.Errorf("the remote still reports %q after a pass that worked", status[0].LastError)
	}
}

// A remote's own retention is its own: a fleet may keep a week on the disk and
// a year in the bucket, which is most of why the bucket is there.
func TestTheRemoteKeepsItsOwnNumberOfCopies(t *testing.T) {
	h := newHarness(t)
	fake := offsite(h, func(r *config.BackupRemote) { r.Keep = 1 })

	for range 2 {
		h.c.scheduledBackup(h.ctx)
		if _, err := h.c.ShipBackups(h.ctx); err != nil {
			t.Fatalf("ShipBackups: %v", err)
		}
		h.offset.Add(int64(2 * time.Hour))
		time.Sleep(1100 * time.Millisecond)
	}

	if got := fake.Count(); got != 1 {
		t.Fatalf("the bucket holds %d copies with keep: 1", got)
	}
	entries, err := backup.List(filepath.Join(h.onDisk(), backup.DefaultDirName))
	if err != nil || len(entries) != 2 {
		t.Fatalf("the local directory holds %d backups, %v -- the remote's ceiling is not the fleet's", len(entries), err)
	}
}

// Nothing leaves the host unless somebody says where it goes. The default
// configuration has no remotes, and a pass over none must be a pass that does
// nothing rather than an error somebody has to read.
func TestAFleetWithNoRemoteShipsNothingAndSaysSo(t *testing.T) {
	h := newHarness(t)
	sent, err := h.c.ShipBackups(h.ctx)
	if err != nil || len(sent) != 0 {
		t.Errorf("a fleet with no remotes shipped %d copies, %v", len(sent), err)
	}
	if remotes := h.c.BackupRemotes(); len(remotes) != 0 {
		t.Errorf("the page would show %d remotes on a fleet with none", len(remotes))
	}
	if problems := h.c.remoteProblems(); len(problems) != 0 {
		t.Errorf("a fleet with no remotes raised %d problems about them", len(problems))
	}
}

// A copy pulled back out of a bucket must survive local retention. It is here
// because the local copies are gone or suspect, and deleting it on the next
// pass because the directory is one over its ceiling would be the cruellest
// possible moment to enforce one.
func TestACopyFetchedBackIsNeverRemovedByRetention(t *testing.T) {
	h := newHarness(t)
	offsite(h, nil)
	h.c.UpdateConfig(func(c *config.Config) { c.Backup.Keep = 1 })

	h.c.scheduledBackup(h.ctx)
	if _, err := h.c.ShipBackups(h.ctx); err != nil {
		t.Fatalf("ShipBackups: %v", err)
	}
	root := filepath.Join(h.onDisk(), backup.DefaultDirName)
	entries, err := backup.List(root)
	if err != nil || len(entries) != 1 {
		t.Fatalf("setting up: %d backups, %v", len(entries), err)
	}
	id := entries[0].ID
	if err := backup.Delete(root, id); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	remote, err := h.c.RemoteBackup("offsite")
	if err != nil {
		t.Fatalf("RemoteBackup: %v", err)
	}
	if _, err := remote.Fetch(h.ctx, root, id, backup.FetchOptions{TakenBy: "alice", Now: h.c.Now}); err != nil {
		t.Fatalf("Fetch: %v", err)
	}

	// A new scheduled backup, and retention behind it, must leave the fetched
	// copy alone even though keep is 1.
	h.offset.Add(int64(2 * time.Hour))
	time.Sleep(1100 * time.Millisecond)
	h.c.scheduledBackup(h.ctx)

	entries, err = backup.List(root)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	found := false
	for _, e := range entries {
		if e.ID == id {
			found = true
			if e.Source != backup.SourceFetched {
				t.Errorf("the fetched copy says it came from %q", e.Source)
			}
		}
	}
	if !found {
		t.Error("retention removed the copy that was brought back from offsite")
	}
}
