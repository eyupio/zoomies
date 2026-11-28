//go:build unix

package backup

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"testing"
)

// Whoever archives a backup is not always the account that owns it: root ships
// a copy offsite after `sudo zoomies backup` has handed it to the service
// account, which can put a link where a file should be. What the link leads to
// must not end up in the archive.
func TestAnArchiveRefusesAFileThatIsALinkOutOfTheBackup(t *testing.T) {
	cfg, st, _ := host(t)
	e := take(t, cfg, st, TakeOptions{Source: SourceManual})

	outside := filepath.Join(t.TempDir(), "elsewhere.txt")
	if err := os.WriteFile(outside, []byte("NOT-PART-OF-THE-BACKUP"), 0o600); err != nil {
		t.Fatal(err)
	}
	db := filepath.Join(e.Dir, DBName)
	if err := os.Remove(db); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, db); err != nil {
		t.Fatal(err)
	}

	// Uncompressed, so that what was written can be searched for.
	var tar bytes.Buffer
	if _, _, err := writeArchive(&tar, e, false); err == nil {
		t.Fatal("a database that is a link out of the backup was archived")
	}
	if bytes.Contains(tar.Bytes(), []byte("NOT-PART-OF-THE-BACKUP")) {
		t.Error("the archive carries what the link led to")
	}
}

// The same holds when the backup's own directory is the link: the files in it
// are then somebody else's, whatever they are called.
func TestAnArchiveRefusesABackupDirectoryThatIsALink(t *testing.T) {
	cfg, st, _ := host(t)
	e := take(t, cfg, st, TakeOptions{Source: SourceManual})

	linked := *e
	linked.Dir = filepath.Join(filepath.Dir(e.Dir), "linked-backup")
	if err := os.Symlink(e.Dir, linked.Dir); err != nil {
		t.Fatal(err)
	}
	if _, _, err := writeArchive(io.Discard, &linked, false); err == nil {
		t.Fatal("a backup directory that is a link was archived")
	}

	// And the real one still is.
	if _, _, err := writeArchive(io.Discard, e, false); err != nil {
		t.Fatalf("an ordinary backup was refused: %v", err)
	}
}
