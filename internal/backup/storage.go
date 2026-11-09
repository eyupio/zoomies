package backup

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"

	"github.com/eyupio/zoomies/internal/config"
	"github.com/eyupio/zoomies/internal/store"
)

// StorageVersion is the storage report's own shape number. It moves when a
// field is removed or renamed, not when one is added, so a reader that checks
// it refuses a shape it cannot read and carries on over fields it does not
// know.
const StorageVersion = 1

// What an entry in the state directory is, in terms of what an operator may do
// with it. The question behind the report is "which of these do I have to carry
// when I move this controller, and which can I leave behind", and the classes
// are the answers.
const (
	// ClassEssential: the controller cannot start without it, or must not be
	// started without it. The database, its write-ahead files and its lock while
	// it is open, and the encryption key when the key lives in this directory.
	ClassEssential = "essential"
	// ClassKept: something that is somebody's own and that nothing deletes: a
	// backup uploaded or fetched back from a remote, which retention never
	// removes, or the live database a restore set aside.
	ClassKept = "kept"
	// ClassRegenerable: the controller does not need it to start, and leaving it
	// behind loses nothing that is anybody's own. Most of it is made again (a
	// cache, the controller's own backups of itself, a note about a restore) or
	// removed by the controller (retention). The exception to "made again" is a
	// copy taken before a migration: it is the rollback for the last upgrade, and
	// once the upgrade has run nothing can take it again, so a person decides
	// before deleting one.
	ClassRegenerable = "regenerable"
	// ClassUnknown: this report does not class it, so a reader must treat it as
	// essential, because the safe mistake is to carry too much. It may be
	// something this release makes and cannot yet say is safe to leave behind
	// (the agent's work directory, which holds credentials beside scratch space)
	// or something it did not make at all.
	ClassUnknown = "unknown"
)

// StorageEntry is one thing in the state directory.
type StorageEntry struct {
	// Path is relative to the state directory and uses forward slashes. A
	// backup or a pre-migration copy is one entry of its own, under the
	// directory it is in, rather than the whole directory as one.
	Path string `json:"path"`
	// Bytes is the size of the regular files under it. A symbolic link counts
	// as nothing and is never followed, so the report cannot be pointed at a
	// disk that is not this directory's.
	Bytes int64  `json:"bytes"`
	Class string `json:"class"`
	// Optional says its absence is normal, so a reader does not take it for
	// damage: a clean stop leaves no write-ahead files.
	Optional bool `json:"optional,omitempty"`
}

// StorageReport says what is in a controller's state directory.
type StorageReport struct {
	Version int `json:"version"`
	// Directory is the state directory: the one the database is in.
	Directory string `json:"directory"`
	// BackupDirectory is where scheduled and manual backups go. Its copies are
	// listed one by one when it is a direct child of Directory, or Directory
	// itself. Nested deeper, it is counted whole under its top-level entry as
	// unknown. Outside Directory it is not walked, because a copy of the state
	// directory does not carry it either.
	BackupDirectory string         `json:"backup_directory"`
	Entries         []StorageEntry `json:"entries"`
	// DatabaseBytes is the database file's size, and DatabaseFreeBytes the
	// part of it that holds no data (the free pages times the page size),
	// which a delete leaves in the file.
	DatabaseBytes     int64 `json:"database_bytes"`
	DatabaseFreeBytes int64 `json:"database_free_bytes"`
	// DatabaseAutoVacuum is the mode the database file was made in: "incremental"
	// can give its free pages back to the disk, and "none" cannot until
	// maintenance.compact is convert and a pass has rewritten it.
	DatabaseAutoVacuum string `json:"database_auto_vacuum"`
	// TotalBytes is every entry added up, so what the report lists and what it
	// says the directory holds cannot differ.
	TotalBytes int64 `json:"total_bytes"`
	// DiskFreeBytes is the room left on the filesystem the directory is on,
	// and null where the platform cannot say.
	DiskFreeBytes *int64 `json:"disk_free_bytes"`
	// LastMaintenance is what the last tidy of this directory removed, or could
	// not, and is left out until there has been one. It is added by whoever knows
	// the controller's memory; Storage reads the disk and nothing else.
	LastMaintenance *MaintenanceResult `json:"last_maintenance,omitempty"`
}

// Storage reports what is in the state directory the configuration's database
// is in. pages is what the database says about its own file, which only the
// store can ask.
//
// It reads every file under the directory, and a cache can hold a great many, so
// it stops with the context's own error as soon as the caller has gone: the
// controller should not spend its disk on an answer nobody is waiting for.
//
// Every top-level entry is classified by a table in this file and not guessed
// from its name, so that a test can hold the table to what a controller really
// leaves there (the "every entry a controller leaves behind is classified"
// subtest of TestTheStorageReportOfARealStateDirectory). The directories of
// copies and the shared folder are opened one level, because neither is one
// thing: an uploaded backup is kept and a scheduled one is not, and only the
// caches under shared are rebuilt by the controller.
func Storage(ctx context.Context, cfg *config.Config, pages store.PageStats) (*StorageReport, error) {
	dbPath := cfg.Database.Path
	dir := filepath.Dir(dbPath)
	top, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("backup: reading the state directory %s: %w", dir, err)
	}

	backupDir := Dir(cfg)
	backupsHere := ""
	if sameDir(filepath.Dir(backupDir), dir) {
		backupsHere = filepath.Base(backupDir)
	}
	// backup.directory may be the state directory itself, and nothing refuses
	// it. The copies are then top-level entries, classed as they are anywhere.
	copiesInPlace := sameDir(backupDir, dir)
	sharedHere := ""
	if shared := config.SharedDir(); sameDir(filepath.Dir(shared), dir) {
		sharedHere = filepath.Base(shared)
	}
	base := filepath.Base(dbPath)
	known := knownEntries(cfg, dir)
	classify := func(e fs.DirEntry) (string, bool) {
		if k, ok := known[e.Name()]; ok {
			return k.class, k.optional
		}
		switch {
		case strings.HasPrefix(e.Name(), base+".before-restore-"):
			// The live database a restore set aside, with its write-ahead files.
			// Nothing deletes it: it is the way back, and somebody decides.
			return ClassKept, false
		case e.IsDir() && strings.HasPrefix(e.Name(), ".transfer-"):
			// A transfer's staging directory, which it removes when it finishes.
			return ClassRegenerable, false
		case copiesInPlace && e.IsDir():
			return copyClass(dir, e.Name(), false), false
		}
		return ClassUnknown, false
	}

	report := &StorageReport{
		Version:            StorageVersion,
		Directory:          dir,
		BackupDirectory:    backupDir,
		Entries:            []StorageEntry{},
		DatabaseFreeBytes:  pages.FreeBytes(),
		DatabaseAutoVacuum: pages.AutoVacuumName(),
	}
	if info, err := os.Stat(dbPath); err == nil {
		report.DatabaseBytes = info.Size()
	}

	for _, e := range top {
		name := e.Name()
		switch {
		case e.IsDir() && name == backupsHere:
			if err := listCopies(ctx, report, name, backupDir, false); err != nil {
				return nil, err
			}
		case e.IsDir() && name == store.PreMigrationDir:
			if err := listCopies(ctx, report, name, PreMigrationDir(dbPath), true); err != nil {
				return nil, err
			}
		case e.IsDir() && name == sharedHere:
			if err := listShared(ctx, report, name, filepath.Join(dir, name)); err != nil {
				return nil, err
			}
		default:
			class, optional := classify(e)
			bytes, err := sizeOf(ctx, filepath.Join(dir, name))
			if err != nil {
				return nil, err
			}
			report.add(name, class, optional, bytes)
		}
	}

	slices.SortFunc(report.Entries, func(a, b StorageEntry) int { return strings.Compare(a.Path, b.Path) })
	if free, ok := DiskFree(dir); ok {
		report.DiskFreeBytes = &free
	}
	return report, nil
}

func (r *StorageReport) add(entryPath, class string, optional bool, bytes int64) {
	r.Entries = append(r.Entries, StorageEntry{Path: entryPath, Bytes: bytes, Class: class, Optional: optional})
	r.TotalBytes += bytes
}

// classed is what the table says about one name.
type classed struct {
	class    string
	optional bool
}

// knownEntries is the table: every top-level name this release knows, and what
// losing it costs.
//
// What is left out is left out on purpose. The agent's work directory holds its
// credentials beside scratch space, and the self-signed certificate is made
// again at a start but agents that trusted the old one would not trust the new,
// so neither is something this table can honestly call regenerable. They are
// reported as unknown, which tells a reader to carry them, and the size is still
// there for an operator who wants to know where the space went.
//
// The names a restore and a transfer write are the ones the code that writes
// them uses (stagedPath, outcomePath), so renaming one cannot leave this table
// describing a file that no longer exists.
func knownEntries(cfg *config.Config, dir string) map[string]classed {
	dbPath := cfg.Database.Path
	base := filepath.Base(dbPath)
	table := map[string]classed{
		base:          {class: ClassEssential},
		base + "-wal": {class: ClassEssential, optional: true},
		base + "-shm": {class: ClassEssential, optional: true},
		// The lock is held on the file's descriptor and the file is never
		// removed, so deleting it under a running controller lets a second one
		// start on the same database. It is empty, so nobody drops it for space.
		filepath.Base(store.LockPath(dbPath)): {class: ClassEssential, optional: true},
		// What a restore says about itself: the one waiting for a restart, and
		// what became of the last. Neither belongs on another machine.
		filepath.Base(stagedPath(dbPath)):  {class: ClassRegenerable},
		filepath.Base(outcomePath(dbPath)): {class: ClassRegenerable},
	}
	// The key is essential only when it lives here. By default it is in the
	// configuration directory, and a report that listed a file from another
	// directory would be describing something a copy of this one does not carry.
	if _, keyPath := cfg.EncryptionKeyOrigin(); keyPath != "" && sameDir(filepath.Dir(keyPath), dir) {
		table[filepath.Base(keyPath)] = classed{class: ClassEssential}
	}
	return table
}

// sharedCacheDir is the one folder under shared that the controller rebuilds.
// The update helper's folder is made by the installer, with the service
// account's ownership, and the host health report needs a directory that root
// owns, so neither comes back by itself and neither is called regenerable.
const sharedCacheDir = "cache"

// listCopies adds one entry for each copy in a directory of backups, and one
// unknown entry for anything in there that is not a backup, so the directory is
// accounted for whole.
func listCopies(ctx context.Context, report *StorageReport, prefix, root string, preMigration bool) error {
	return listChildren(ctx, report, prefix, root, func(n fs.DirEntry) string {
		if !n.IsDir() {
			return ClassUnknown
		}
		return copyClass(root, n.Name(), preMigration)
	})
}

// listShared opens the shared folder one level: its caches are regenerable and
// everything else in it is unknown.
func listShared(ctx context.Context, report *StorageReport, prefix, root string) error {
	return listChildren(ctx, report, prefix, root, func(n fs.DirEntry) string {
		if n.IsDir() && n.Name() == sharedCacheDir {
			return ClassRegenerable
		}
		return ClassUnknown
	})
}

// listChildren adds an entry for each thing in a directory, classed by class. A
// directory that cannot be listed is one unknown entry of its own and not an
// error: this is the report an operator reaches for when the disk is full, and
// one thing it cannot read must not stop it saying about the rest.
func listChildren(ctx context.Context, report *StorageReport, prefix, root string, class func(fs.DirEntry) string) error {
	names, err := os.ReadDir(root)
	if err != nil {
		bytes, sizeErr := sizeOf(ctx, root)
		if sizeErr != nil {
			return sizeErr
		}
		report.add(prefix, ClassUnknown, false, bytes)
		return nil
	}
	for _, n := range names {
		bytes, err := sizeOf(ctx, filepath.Join(root, n.Name()))
		if err != nil {
			return err
		}
		report.add(path.Join(prefix, n.Name()), class(n), false, bytes)
	}
	return nil
}

// copyClass says what the directory called name in a directory of backups is. A
// name that is not a backup's, one nothing can describe, and one that cannot be
// read (a database nobody may open) are all unknown, which is the safe answer
// and keeps one bad copy from taking the report down with it.
func copyClass(root, name string, preMigration bool) string {
	if !ValidID(name) {
		return ClassUnknown
	}
	c, err := describe(root, name)
	if err != nil {
		return ClassUnknown
	}
	return classifyCopy(*c, preMigration)
}

// classifyCopy says what a backup is to an operator. A copy retention may
// delete is regenerable, which is the same test Prune applies, so the report
// never promises a deletion that nothing performs. One whose manifest cannot be
// read is something retention leaves alone, so it is unknown, not regenerable.
func classifyCopy(e Entry, preMigration bool) string {
	switch {
	case e.Source == SourceUploaded || e.Source == SourceFetched:
		return ClassKept
	case preMigration:
		// The store's own copies have no manifest and never did.
		if e.Manifest == nil && e.Problem == "" && e.Source == SourcePreMigration {
			return ClassRegenerable
		}
	case e.Manifest != nil:
		return ClassRegenerable
	}
	return ClassUnknown
}

// sizeOf adds up the regular files under p, which may be one file. What cannot
// be read counts as nothing: a file that vanishes while the walk is under way is
// a backup being pruned, and a report that failed on it could not be asked for
// on the day the disk is full. The one thing that stops it is the context, which
// is checked at every entry, so a caller who has gone ends the walk at the next
// file and not at the end of a cache.
func sizeOf(ctx context.Context, p string) (int64, error) {
	var total int64
	err := filepath.WalkDir(p, func(_ string, d fs.DirEntry, err error) error {
		if cerr := ctx.Err(); cerr != nil {
			return cerr
		}
		if err != nil {
			if d != nil && d.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		if d.Type().IsRegular() {
			if info, err := d.Info(); err == nil {
				total += info.Size()
			}
		}
		return nil
	})
	if err != nil {
		return 0, err
	}
	return total, nil
}

// sameDir says two paths are one directory, compared as absolute paths. It does
// not resolve links: both sides come from the same configuration, and resolving
// would let a link decide which directory is the state directory.
func sameDir(a, b string) bool {
	aa, errA := filepath.Abs(a)
	bb, errB := filepath.Abs(b)
	return errA == nil && errB == nil && aa == bb
}
