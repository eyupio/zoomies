package backup

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/eyupio/zoomies/internal/config"
	"github.com/eyupio/zoomies/internal/cryptox"
	"github.com/eyupio/zoomies/internal/store"
)

// stateDirectory is what a controller leaves in its state directory after it
// has run for a while: the open database with its write-ahead files, the key,
// the lock it holds, one backup from every source there is, a copy taken before
// a migration, and the shared folder its agent fills.
//
// The pre-migration copy is written through the store's own Backup into the
// directory and under the name the store uses before a migration, because a
// migration cannot be provoked from here. A test inside the store holds the
// other half: what a real migrating start leaves behind is only what the table
// in storage.go knows.
func stateDirectory(t *testing.T) (*config.Config, *store.Store) {
	t.Helper()
	cfg, st, key := host(t)
	dir := filepath.Dir(cfg.Database.Path)
	// The shared folder lives under the state directory the configuration names,
	// which is the environment's, so the test points that at its own directory.
	t.Setenv("ZOOMIES_STATE_DIR", dir)

	keyPath := filepath.Join(dir, "encryption.key")
	if err := cryptox.WriteKeyFile(keyPath, key); err != nil {
		t.Fatalf("WriteKeyFile: %v", err)
	}
	cfg.Security.EncryptionKeyFile = keyPath

	stamp := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	for i, source := range []string{SourceCLI, SourceManual, SourceScheduled, SourceUpgrade, SourceUploaded, SourceFetched} {
		at := stamp.Add(time.Duration(i) * time.Minute)
		take(t, cfg, st, TakeOptions{Source: source, Now: func() time.Time { return at }})
	}

	pre := filepath.Join(PreMigrationDir(cfg.Database.Path), DirPrefix+"20260101-000000")
	if err := st.Backup(context.Background(), filepath.Join(pre, DBName)); err != nil {
		t.Fatalf("writing a pre-migration copy: %v", err)
	}

	release, err := store.Lock(cfg.Database.Path)
	if err != nil {
		t.Fatalf("taking the database lock: %v", err)
	}
	t.Cleanup(func() { _ = release() })

	// The folders an agent creates when it starts come from the function that
	// creates them, so a purpose added to config.SharedLayout fails the
	// classification test until somebody says what it is.
	if err := config.EnsureSharedDir(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(config.SharedDir(), "cache", "pools", "entry"), make([]byte, 1000), 0o600); err != nil {
		t.Fatal(err)
	}

	// What a restore and a transfer leave beside the database, under the names
	// the code that writes them uses: the live database a restore set aside, the
	// notes about a restore, and a transfer's staging directory.
	live := cfg.Database.Path
	for path, size := range map[string]int{
		live + ".before-restore-20260101-000000":              300,
		stagedPath(live):                                      40,
		outcomePath(live):                                     50,
		filepath.Join(dir, ".transfer-export-123", "archive"): 60,
	} {
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, make([]byte, size), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return cfg, st
}

func storageOf(t *testing.T, cfg *config.Config, st *store.Store) *StorageReport {
	t.Helper()
	pages, err := st.PageStats(context.Background())
	if err != nil {
		t.Fatalf("PageStats: %v", err)
	}
	report, err := Storage(context.Background(), cfg, pages)
	if err != nil {
		t.Fatalf("Storage: %v", err)
	}
	return report
}

func entryAt(t *testing.T, report *StorageReport, path string) StorageEntry {
	t.Helper()
	for _, e := range report.Entries {
		if e.Path == path {
			return e
		}
	}
	paths := make([]string, 0, len(report.Entries))
	for _, e := range report.Entries {
		paths = append(paths, e.Path)
	}
	t.Fatalf("the report has no entry %q; it has %v", path, paths)
	return StorageEntry{}
}

// The report is asked of one real state directory, built once: opening a store
// is most of what these tests cost, and every check below only reads it. The one
// that deletes goes last.
func TestTheStorageReportOfARealStateDirectory(t *testing.T) {
	cfg, st := stateDirectory(t)

	// This is the test that keeps the table honest. A later change that makes
	// the controller write another file into its state directory fails here,
	// naming the file, instead of shipping a report that tells an operator to
	// treat it as something nobody could explain.
	t.Run("every entry a controller leaves behind is classified", func(t *testing.T) {
		report := storageOf(t, cfg, st)
		var sum int64
		for _, e := range report.Entries {
			sum += e.Bytes
			if e.Class == ClassUnknown {
				t.Errorf("%s is in the state directory but the storage report does not know what it is: "+
					"add it to the table in internal/backup/storage.go, as essential if the controller cannot "+
					"start without it, as regenerable if the controller could make it again, or as kept if it is "+
					"somebody's own copy", e.Path)
			}
		}
		if sum != report.TotalBytes {
			t.Errorf("the entries add up to %d bytes but the report says %d: something in the directory is not accounted for", sum, report.TotalBytes)
		}
		if report.Version != StorageVersion || StorageVersion != 1 {
			t.Errorf("version = %d (const %d), want 1: a reader refuses a shape it does not know by this number", report.Version, StorageVersion)
		}
	})

	t.Run("the database, the key and the lock are classed for what losing them costs", func(t *testing.T) {
		report := storageOf(t, cfg, st)
		for _, name := range []string{"zoomies.db", "encryption.key"} {
			e := entryAt(t, report, name)
			if e.Class != ClassEssential || e.Optional {
				t.Errorf("%s = %+v, want essential and not optional: the controller cannot start without it", name, e)
			}
		}
		// A clean stop leaves neither of these, so a reader must not treat their
		// absence as damage. They are present here because the store is open.
		for _, name := range []string{"zoomies.db-wal", "zoomies.db-shm"} {
			e := entryAt(t, report, name)
			if e.Class != ClassEssential || !e.Optional {
				t.Errorf("%s = %+v, want essential and optional", name, e)
			}
		}
		// Deleting the lock under a running controller lets a second one start
		// on the same database, so it is not something to drop to save space.
		if e := entryAt(t, report, "zoomies.db.lock"); e.Class != ClassEssential || !e.Optional {
			t.Errorf("the lock = %+v, want essential and optional: it exists while a controller runs, and removing it defeats it", e)
		}
	})

	// Only the caches under shared are rebuilt by the controller. The update
	// helper's folder is made by the installer and the host health report needs
	// a directory owned by root, so neither comes back by itself.
	t.Run("only the caches under shared are regenerable", func(t *testing.T) {
		report := storageOf(t, cfg, st)
		if e := entryAt(t, report, "shared/cache"); e.Class != ClassRegenerable || e.Bytes != 1000 {
			t.Errorf("shared/cache = %+v, want regenerable and 1000 bytes (counted all the way down)", e)
		}
		for _, e := range report.Entries {
			if e.Path == "shared" {
				t.Errorf("shared is listed whole as %s: it holds things the controller does not rebuild, so it is opened one level", e.Class)
			}
		}
	})

	t.Run("what a restore and a transfer leave behind is classed for what it is", func(t *testing.T) {
		report := storageOf(t, cfg, st)
		for path, want := range map[string]string{
			// The old live database, which nothing deletes: somebody decides.
			"zoomies.db.before-restore-20260101-000000": ClassKept,
			"restore-staged.json":                       ClassRegenerable,
			"restore-outcome.json":                      ClassRegenerable,
			".transfer-export-123":                      ClassRegenerable,
		} {
			if got := entryAt(t, report, path).Class; got != want {
				t.Errorf("%s is %s, want %s", path, got, want)
			}
		}
	})

	// The classes are what an operator moving a deployment reads: carry the
	// essential and the kept, and leave the rest. So an uploaded or fetched
	// backup, which retention never deletes, must be kept, and a copy the
	// controller took of itself must not be.
	t.Run("an uploaded backup is kept and a scheduled one is regenerable", func(t *testing.T) {
		report := storageOf(t, cfg, st)
		entries, err := List(Dir(cfg))
		if err != nil {
			t.Fatal(err)
		}
		if len(entries) != 6 {
			t.Fatalf("the fixture should hold one backup from each of six sources, found %d", len(entries))
		}
		for _, e := range entries {
			want := ClassRegenerable
			if e.Source == SourceUploaded || e.Source == SourceFetched {
				want = ClassKept
			}
			if got := entryAt(t, report, "backups/"+e.ID).Class; got != want {
				t.Errorf("a %s backup is %s, want %s", e.Source, got, want)
			}
		}
		if got := entryAt(t, report, "pre-migration/"+DirPrefix+"20260101-000000").Class; got != ClassRegenerable {
			t.Errorf("a pre-migration copy is %s, want regenerable", got)
		}
	})

	t.Run("the report says whether the database can give free pages back", func(t *testing.T) {
		for _, tc := range []struct {
			mode int64
			want string
		}{{store.AutoVacuumIncremental, "incremental"}, {store.AutoVacuumFull, "full"}, {store.AutoVacuumNone, "none"}} {
			report, err := Storage(context.Background(), cfg, store.PageStats{PageSize: 4096, PageCount: 10, AutoVacuum: tc.mode})
			if err != nil {
				t.Fatalf("Storage: %v", err)
			}
			if report.DatabaseAutoVacuum != tc.want {
				t.Errorf("auto_vacuum %d is reported as %q, want %q", tc.mode, report.DatabaseAutoVacuum, tc.want)
			}
		}
	})

	t.Run("the report says how much of the database and the disk is free", func(t *testing.T) {
		report, err := Storage(context.Background(), cfg, store.PageStats{PageSize: 4096, PageCount: 10, FreelistCount: 3})
		if err != nil {
			t.Fatalf("Storage: %v", err)
		}
		if report.DatabaseFreeBytes != 3*4096 {
			t.Errorf("DatabaseFreeBytes = %d, want the 3 free pages times 4096", report.DatabaseFreeBytes)
		}
		info, err := os.Stat(cfg.Database.Path)
		if err != nil {
			t.Fatal(err)
		}
		if report.DatabaseBytes != info.Size() {
			t.Errorf("DatabaseBytes = %d, want the file's own size %d", report.DatabaseBytes, info.Size())
		}
		if report.Directory != filepath.Dir(cfg.Database.Path) {
			t.Errorf("Directory = %q, want the one the database is in", report.Directory)
		}
		if _, ok := DiskFree(report.Directory); ok && report.DiskFreeBytes == nil {
			t.Error("the platform can say how much disk is free, so the report should")
		}
	})

	// Backups can be sent somewhere else with backup.directory, and then a copy
	// of the state directory does not carry them. The report says where they
	// are instead of reading a directory that is not part of this one.
	t.Run("backups kept outside the state directory are named and not walked", func(t *testing.T) {
		elsewhere := t.TempDir()
		was := cfg.Backup.Directory
		cfg.Backup.Directory = elsewhere
		defer func() { cfg.Backup.Directory = was }()

		report := storageOf(t, cfg, st)
		if report.BackupDirectory != elsewhere {
			t.Errorf("BackupDirectory = %q, want %q", report.BackupDirectory, elsewhere)
		}
		for _, e := range report.Entries {
			if strings.HasPrefix(e.Path, "backups/") {
				t.Errorf("%s is listed, but the backups are in %s, outside the state directory", e.Path, elsewhere)
			}
		}
	})

	// The report and retention have to agree about which backups are the
	// fleet's own, because an operator who is told a copy is regenerable will
	// delete it by hand, and one who is told it is kept will carry it. Pruning
	// is the other half of that sentence. It deletes, so it goes last.
	t.Run("what the report calls regenerable is what retention may delete", func(t *testing.T) {
		report := storageOf(t, cfg, st)
		removed, err := Prune(Dir(cfg), 1)
		if err != nil {
			t.Fatalf("Prune: %v", err)
		}
		if len(removed) == 0 {
			t.Fatal("pruning to one copy should have removed some of the four the controller took of itself")
		}
		for _, id := range removed {
			if got := entryAt(t, report, "backups/"+id).Class; got != ClassRegenerable {
				t.Errorf("retention removed %s, which the report called %s", id, got)
			}
		}
		for _, e := range report.Entries {
			if e.Class != ClassKept {
				continue
			}
			if _, err := os.Stat(filepath.Join(filepath.Dir(cfg.Database.Path), filepath.FromSlash(e.Path))); err != nil {
				t.Errorf("%s was called kept and retention removed it: %v", e.Path, err)
			}
		}
	})
}

// What the table does not recognise is unknown, and a reader must treat that
// as essential. A copy whose manifest cannot be read is one retention will not
// touch, so calling it regenerable would promise a deletion nothing performs.
// A link out of the state directory must not be followed: a report that read
// through one would count somebody else's disk and could be pointed at it.
func TestWhatNobodyCanNameIsUnknownAndStillCounted(t *testing.T) {
	cfg, st := stateDirectory(t)
	dir := filepath.Dir(cfg.Database.Path)
	root := Dir(cfg)

	if err := os.WriteFile(filepath.Join(root, "notes.txt"), []byte("0123456789"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, DirPrefix+"00000000-000000"), 0o700); err != nil {
		t.Fatal(err)
	}
	damaged := take(t, cfg, st, TakeOptions{Source: SourceScheduled, Now: func() time.Time { return time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC) }})
	if err := os.WriteFile(filepath.Join(damaged.Dir, ManifestName), []byte("not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "somebody-elses.txt"), make([]byte, 7), 0o600); err != nil {
		t.Fatal(err)
	}
	want := map[string]int64{
		"backups/notes.txt":                        10,
		"backups/" + DirPrefix + "00000000-000000": 0,
		"somebody-elses.txt":                       7,
	}
	// Things in shared that the controller does not rebuild.
	for _, path := range []string{"update/request", "host-health/report.json"} {
		full := filepath.Join(config.SharedDir(), filepath.FromSlash(path))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, make([]byte, 5), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	want["shared/update"] = 5
	want["shared/host-health"] = 5
	// A backup whose database cannot be read. A link to itself fails with an
	// error that is not "not found", which is what a permission failure looks
	// like and, unlike a mode, stops a test that runs as root.
	broken := filepath.Join(root, DirPrefix+"20260101-000001")
	if err := os.MkdirAll(broken, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(DBName, filepath.Join(broken, DBName)); err == nil {
		want["backups/"+DirPrefix+"20260101-000001"] = 0
	}
	elsewhere := filepath.Join(t.TempDir(), "big")
	if err := os.WriteFile(elsewhere, make([]byte, 1<<20), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(elsewhere, filepath.Join(dir, "link")); err == nil {
		want["link"] = 0
	}

	// One backup that cannot be read must not make the report unanswerable: it
	// is the tool an operator reaches for when the disk is full.
	report := storageOf(t, cfg, st)
	for path, bytes := range want {
		e := entryAt(t, report, path)
		if e.Class != ClassUnknown || e.Bytes != bytes {
			t.Errorf("%s = %+v, want unknown and %d bytes", path, e, bytes)
		}
	}
	if got := entryAt(t, report, "backups/"+damaged.ID).Class; got != ClassUnknown {
		t.Errorf("a backup whose manifest cannot be read is %s, want unknown: retention does not remove it", got)
	}
	if removed, _ := Prune(root, 1); slices.Contains(removed, damaged.ID) {
		t.Errorf("retention removed %s, so the report was wrong to call it unknown", damaged.ID)
	}
}

// A state directory can hold a cache with a great many files in it, and the
// report reads every one. A caller who has gone away, or a request that has been
// given up on, must not leave the walk running: the controller would spend its
// disk on an answer nobody is waiting for. The mechanism is the check inside the
// walk, so that is what is held, and then that the report passes it on.
func TestTheWalkStopsWhenTheCallerHasGone(t *testing.T) {
	dir := t.TempDir()
	for i := range 20 {
		if err := os.WriteFile(filepath.Join(dir, "f"+string(rune('a'+i))), make([]byte, 100), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	total, err := sizeOf(ctx, dir)
	if !errors.Is(err, context.Canceled) {
		t.Errorf("sizeOf = %d, %v, want the context's own error: it kept walking", total, err)
	}
	if total != 0 {
		t.Errorf("sizeOf counted %d bytes after the caller had gone, want none", total)
	}
	if total, err := sizeOf(context.Background(), dir); err != nil || total != 2000 {
		t.Errorf("sizeOf with a live context = %d, %v, want 2000 and no error", total, err)
	}
}

func TestAReportForACallerWhoHasGoneIsNotBuilt(t *testing.T) {
	cfg, st := stateDirectory(t)
	pages, err := st.PageStats(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if report, err := Storage(ctx, cfg, pages); !errors.Is(err, context.Canceled) || report != nil {
		t.Errorf("Storage = %v, %v, want no report and the context's error", report, err)
	}
}

// backup.directory can be the state directory itself, and nothing refuses it.
// The copies are then top-level entries, and an uploaded one must still be kept
// and a scheduled one regenerable, as they are in a directory of their own.
func TestBackupsKeptInTheStateDirectoryItselfAreStillClassed(t *testing.T) {
	cfg, st := stateDirectory(t)
	dir := filepath.Dir(cfg.Database.Path)
	cfg.Backup.Directory = dir
	scheduled := take(t, cfg, st, TakeOptions{Source: SourceScheduled, Now: func() time.Time { return time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC) }})
	uploaded := take(t, cfg, st, TakeOptions{Source: SourceUploaded, Now: func() time.Time { return time.Date(2026, 3, 2, 0, 0, 0, 0, time.UTC) }})

	report := storageOf(t, cfg, st)
	if got := entryAt(t, report, scheduled.ID).Class; got != ClassRegenerable {
		t.Errorf("a scheduled backup in the state directory is %s, want regenerable", got)
	}
	if got := entryAt(t, report, uploaded.ID).Class; got != ClassKept {
		t.Errorf("an uploaded backup in the state directory is %s, want kept: retention never deletes it", got)
	}
}
