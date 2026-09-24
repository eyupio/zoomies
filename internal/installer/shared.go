package installer

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/eyupio/zoomies/internal/config"
)

// SharedHostDir is the shared folder of a container deployment: the host path
// the controller's container mounts at the same path, so a folder the embedded
// agent hands the host's daemon for a runner's mount is the same folder on
// both sides. It is config.SharedDir as the container sees it, which is why
// the container's state directory has to stay /var/lib/zoomies.
const SharedHostDir = ContainerStateDir + "/shared"

// sharedFolders is the shared folder and every folder this release keeps in
// it, parents first.
func sharedFolders(dir string) []string {
	out := []string{dir}
	for _, sub := range config.SharedLayout {
		out = append(out, filepath.Join(dir, filepath.FromSlash(sub)))
	}
	return out
}

// PrepareSharedDir creates the shared folder and its layout, owned by uid (the
// account Zoomies runs as there) where this process may say so, and returns
// the folders it created.
//
// The owner is the point. The folder is where later releases add folders of
// their own, at start, without an installer run -- so the account that runs
// Zoomies has to be able to write to it, not only read it. A folder that is
// already there is left as it is: an operator who put it on another disk or
// gave it another owner meant to.
func PrepareSharedDir(dir string, uid, gid int) ([]string, error) {
	var created []string
	for _, d := range sharedFolders(dir) {
		if _, err := os.Stat(d); err == nil {
			continue
		}
		if err := os.MkdirAll(d, 0o750); err != nil {
			return created, fmt.Errorf("installer: creating %s: %w", d, err)
		}
		if err := os.Chmod(d, 0o750); err != nil {
			return created, fmt.Errorf("installer: setting permissions on %s: %w", d, err)
		}
		if uid >= 0 {
			if err := os.Lchown(d, uid, gid); err != nil && !errors.Is(err, os.ErrPermission) {
				return created, fmt.Errorf("installer: giving %s to uid %d: %w", d, uid, err)
			}
		}
		created = append(created, d)
	}
	return created, nil
}

// SharedDirProblems says what is wrong with the shared folder for an account
// that has to write to it: each folder of the layout that is missing, and the
// shared folder itself when that account does not own it. Empty is ready.
func SharedDirProblems(dir string, uid int) []string {
	var out []string
	for _, d := range sharedFolders(dir) {
		fi, err := os.Stat(d)
		if err != nil {
			out = append(out, d+" is missing")
			continue
		}
		if d != dir || uid < 0 {
			continue
		}
		if owner, _, ok := fileOwner(fi); ok && owner != uid {
			out = append(out, fmt.Sprintf("%s is owned by uid %d, not uid %d, so Zoomies cannot add folders to it", d, owner, uid))
		}
	}
	return out
}
