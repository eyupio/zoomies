//go:build unix

package backup

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"testing"
)

// ownedByAnother makes dir belong to somebody other than root and returns who.
// Root can give a directory away, and anybody else already is that somebody, so
// the tests that follow run, and mean the same thing, under either.
func ownedByAnother(t *testing.T, dir string) (uid, gid int) {
	t.Helper()
	if os.Geteuid() == 0 {
		if err := os.Chown(dir, 4242, 4243); err != nil {
			t.Skipf("this sandbox will not change ownership: %v", err)
		}
	}
	fi, err := os.Stat(dir)
	if err != nil {
		t.Fatal(err)
	}
	uid, gid, _ = fileOwner(fi)
	return uid, gid
}

type chownCall struct {
	path     string
	uid, gid int
}

// recordChown stands in for os.Lchown, so the choice of who gets the copy is
// tested without needing to be root to see it made.
func recordChown(calls *[]chownCall, failOn ...string) func(string, int, int) error {
	return func(path string, uid, gid int) error {
		*calls = append(*calls, chownCall{path, uid, gid})
		if slices.Contains(failOn, filepath.Base(path)) {
			return errors.New("operation not permitted")
		}
		return nil
	}
}

// The service that lists and prunes the backups runs as the account that owns
// their directory. A copy that stays root's is one it cannot open, which is how
// an upgrade once left a backup the retention could not see.
func TestACopyTakenAsRootIsGivenToTheOwnerOfTheBackupDirectory(t *testing.T) {
	root := filepath.Join(t.TempDir(), "backups")
	if err := os.Mkdir(root, 0o700); err != nil {
		t.Fatal(err)
	}
	uid, gid := ownedByAnother(t, root)
	dest := filepath.Join(root, DirPrefix+"20260101-000000")
	if err := os.Mkdir(dest, 0o700); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{DBName, ManifestName} {
		if err := os.WriteFile(filepath.Join(dest, name), nil, 0o600); err != nil {
			t.Fatal(err)
		}
	}

	gotUID, gotGID, ok := inheritedOwner(root, 0)
	if !ok || gotUID != uid || gotGID != gid {
		t.Fatalf("inheritedOwner = %d:%d, %v; want %d:%d, true", gotUID, gotGID, ok, uid, gid)
	}
	var calls []chownCall
	if err := handOver(root, dest, uid, gid, recordChown(&calls)); err != nil {
		t.Fatalf("handOver: %v", err)
	}
	want := []string{filepath.Join(dest, DBName), filepath.Join(dest, ManifestName), dest, root}
	var got []string
	for _, c := range calls {
		got = append(got, c.path)
		if c.uid != uid || c.gid != gid {
			t.Errorf("%s was given to %d:%d, want %d:%d", c.path, c.uid, c.gid, uid, gid)
		}
	}
	slices.Sort(want)
	slices.Sort(got)
	if !slices.Equal(got, want) {
		t.Errorf("handed over %v, want %v", got, want)
	}
}

// The first backup on a host is taken before its directory exists, and root's
// MkdirAll would make that directory root's, which the service could not write
// to at all. The directory beside the database is what to follow.
func TestAMissingBackupDirectoryFollowsTheOwnerOfTheDirectoryItWillBeIn(t *testing.T) {
	state := t.TempDir()
	uid, gid := ownedByAnother(t, state)

	gotUID, gotGID, ok := inheritedOwner(filepath.Join(state, "backups"), 0)
	if !ok || gotUID != uid || gotGID != gid {
		t.Errorf("inheritedOwner = %d:%d, %v; want %d:%d, true", gotUID, gotGID, ok, uid, gid)
	}
}

// Only root can give a file away, and a directory that is root's is meant to
// stay that way.
func TestACopyIsOnlyHandedOnWhenRootTookItForAnotherAccount(t *testing.T) {
	root := t.TempDir()
	ownedByAnother(t, root)
	if _, _, ok := inheritedOwner(root, 1000); ok {
		t.Error("a process that is not root was told to hand a copy on")
	}
	if _, _, ok := inheritedOwner(filepath.Join(root, "missing", "deeper"), 0); ok {
		t.Error("a directory with no existing parent was given an owner")
	}

	if os.Geteuid() != 0 {
		return
	}
	rootsOwn := t.TempDir()
	if err := os.Chown(rootsOwn, 0, 0); err != nil {
		t.Fatal(err)
	}
	if _, _, ok := inheritedOwner(rootsOwn, 0); ok {
		t.Error("a directory root owns was handed to somebody else")
	}
}

// A share that cannot change ownership fails every file alike, and the copy is
// sound all the same: the rest are still tried, and the failure is reported.
func TestHandingOnGoesOnAfterAFileCannotBeGiven(t *testing.T) {
	root := t.TempDir()
	dest := filepath.Join(root, DirPrefix+"20260101-000000")
	if err := os.Mkdir(dest, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dest, DBName), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	var calls []chownCall
	err := handOver(root, dest, 1000, 1000, recordChown(&calls, DBName))
	if err == nil {
		t.Fatal("a file that could not be given was not reported")
	}
	if len(calls) != 3 {
		t.Errorf("tried %d paths, want the file, the copy's directory and the backup directory", len(calls))
	}
}

// The whole of it, through Take: what the upgrade and `sudo zoomies backup`
// both call.
func TestTakeAsRootLeavesNothingBehindThatTheServiceCannotOpen(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("only root can give a copy away")
	}
	cfg, st, _ := host(t)
	state := filepath.Dir(cfg.Database.Path)
	uid, gid := ownedByAnother(t, state)

	e := take(t, cfg, st, TakeOptions{IncludeKey: true, Source: SourceUpgrade})

	owned := func(path string) {
		t.Helper()
		fi, err := os.Lstat(path)
		if err != nil {
			t.Fatal(err)
		}
		if u, g, _ := fileOwner(fi); u != uid || g != gid {
			t.Errorf("%s belongs to %d:%d, want %d:%d", path, u, g, uid, gid)
		}
	}
	owned(Dir(cfg))
	owned(e.Dir)
	files, err := os.ReadDir(e.Dir)
	if err != nil || len(files) == 0 {
		t.Fatalf("the copy holds %d files, %v", len(files), err)
	}
	for _, f := range files {
		owned(filepath.Join(e.Dir, f.Name()))
	}
}
