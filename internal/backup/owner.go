package backup

import (
	"errors"
	"os"
	"path/filepath"
)

// inheritedOwner says who a copy taken by root should belong to: whoever owns
// the directory it goes in, or the directory that holds that one when it does
// not exist yet.
//
// A copy taken by `sudo zoomies upgrade` or `sudo zoomies backup` is root's
// unless somebody hands it on, and the service that lists, prunes and ships the
// backups runs as an unprivileged account. One copy it cannot open is invisible
// to the Backups page and to retention, so the backup directory itself is the
// record of which account that is. It needs no lookup by name, which a
// container's volume would not have a host account for.
//
// It answers only for root, and only when the directory belongs to somebody
// else: anyone else cannot give a file away, and a directory root owns is
// meant to stay root's.
func inheritedOwner(root string, euid int) (uid, gid int, ok bool) {
	if euid != 0 {
		return 0, 0, false
	}
	fi, err := os.Stat(root)
	if errors.Is(err, os.ErrNotExist) {
		fi, err = os.Stat(filepath.Dir(root))
	}
	if err != nil {
		return 0, 0, false
	}
	uid, gid, ok = fileOwner(fi)
	if !ok || uid == 0 {
		return 0, 0, false
	}
	return uid, gid, true
}

// handOver gives the backup directory, the new backup's own directory and the
// files in it to uid and gid. It goes on after a failure and reports every
// one, because a share that cannot change ownership fails them all alike and
// the copy is sound either way.
//
// shortcut: only root's own leaf is handed over, not a chain of directories
// MkdirAll made above it; upgrade when a configured backup directory is several
// levels deep and new.
func handOver(root, dest string, uid, gid int, chown func(path string, uid, gid int) error) error {
	var errs []error
	files, err := os.ReadDir(dest)
	if err != nil {
		errs = append(errs, err)
	}
	for _, f := range files {
		errs = append(errs, chown(filepath.Join(dest, f.Name()), uid, gid))
	}
	errs = append(errs, chown(dest, uid, gid), chown(root, uid, gid))
	return errors.Join(errs...)
}
