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
// when I move this controller, and which could it make again", and the classes
// are the answers.
const (
	// ClassEssential: the controller cannot start without it. The database,
	// its write-ahead files while it is open, and the encryption key when the
	// key lives in this directory.
	ClassEssential = "essential"
	// ClassKept: a backup somebody put here on purpose, uploaded or fetched
	// back from a remote. Retention never deletes one, so nothing else will.
	ClassKept = "kept"
	// ClassRegenerable: something the controller could make again, whether it
	// is a copy of itself (a scheduled, manual, command-line or upgrade backup,
	// or a pre-migration copy) or something it rebuilds on demand (a cache).
	ClassRegenerable = "regenerable"
	// ClassUnknown: this release does not know what it is. A reader that does
	// not know must treat it as essential, because the safe mistake is to carry
	// too much.
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
	// BackupDirectory is where scheduled and manual backups go. When it is not
	// directly inside Directory its copies are not listed, because a copy of
	// the state directory does not carry them either.
	BackupDirectory string         `json:"backup_directory"`
	Entries         []StorageEntry `json:"entries"`
	// DatabaseBytes is the database file's size, and DatabaseFreeBytes the
	// part of it that holds no data (the free pages times the page size),
	// which a delete leaves in the file.
	DatabaseBytes     int64 `json:"database_bytes"`
	DatabaseFreeBytes int64 `json:"database_free_bytes"`
	// TotalBytes is every entry added up, so what the report lists and what it
	// says the directory holds cannot differ.
	TotalBytes int64 `json:"total_bytes"`
	// DiskFreeBytes is the room left on the filesystem the directory is on,
	// and null where the platform cannot say.
	DiskFreeBytes *int64 `json:"disk_free_bytes"`
}

// Storage reports what is in the state directory the configuration's database
// is in. pages is what the database says about its own file, which only the
// store can ask.
//
// It reads every file under the directory, and a cache can hold a great many, so
// it stops with the context's own error as soon as the caller has gone: the
// controller should not spend its disk on an answer nobody is waiting for.
//
// Every top-level entry is classified by a table in this function and not
// guessed from its name, so that a test can hold the table to what a
// controller really leaves there (see TestEveryEntryAControllerLeavesBehindIsClassified).
// The two directories of copies are opened one level, because "backups" is not
// one thing: an uploaded backup in it is kept and a scheduled one is not.
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
	known := knownEntries(cfg, dir)

	report := &StorageReport{
		Version:           StorageVersion,
		Directory:         dir,
		BackupDirectory:   backupDir,
		Entries:           []StorageEntry{},
		DatabaseFreeBytes: pages.FreeBytes(),
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
		default:
			class, optional := ClassUnknown, false
			if k, ok := known[name]; ok {
				class, optional = k.class, k.optional
			}
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
func knownEntries(cfg *config.Config, dir string) map[string]classed {
	base := filepath.Base(cfg.Database.Path)
	table := map[string]classed{
		base:          {class: ClassEssential},
		base + "-wal": {class: ClassEssential, optional: true},
		base + "-shm": {class: ClassEssential, optional: true},
		filepath.Base(store.LockPath(cfg.Database.Path)): {class: ClassRegenerable},
	}
	// The key is essential only when it lives here. By default it is in the
	// configuration directory, and a report that listed a file from another
	// directory would be describing something a copy of this one does not carry.
	if _, keyPath := cfg.EncryptionKeyOrigin(); keyPath != "" && sameDir(filepath.Dir(keyPath), dir) {
		table[filepath.Base(keyPath)] = classed{class: ClassEssential}
	}
	// The shared folder is the agent's caches, the update helper's requests and
	// the host health report, every one of them rebuilt on demand. It is only
	// Zoomies' own when it is the folder the configuration names, not any
	// directory that happens to be called shared.
	if shared := config.SharedDir(); sameDir(filepath.Dir(shared), dir) {
		table[filepath.Base(shared)] = classed{class: ClassRegenerable}
	}
	return table
}

// listCopies adds one entry for each copy in a directory of backups, and one
// unknown entry for anything in there that is not a backup, so the directory is
// accounted for whole.
func listCopies(ctx context.Context, report *StorageReport, prefix, root string, preMigration bool) error {
	copies, err := List(root)
	if err != nil {
		return err
	}
	byID := make(map[string]Entry, len(copies))
	for _, c := range copies {
		byID[c.ID] = c
	}
	names, err := os.ReadDir(root)
	if err != nil {
		return fmt.Errorf("backup: reading %s: %w", root, err)
	}
	for _, n := range names {
		c, isCopy := byID[n.Name()]
		class := ClassUnknown
		if isCopy {
			class = classifyCopy(c, preMigration)
		}
		bytes, err := sizeOf(ctx, filepath.Join(root, n.Name()))
		if err != nil {
			return err
		}
		report.add(path.Join(prefix, n.Name()), class, false, bytes)
	}
	return nil
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
