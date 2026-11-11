package backup

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/eyupio/zoomies/internal/config"
	"github.com/eyupio/zoomies/internal/cryptox"
	"github.com/eyupio/zoomies/internal/store"
)

// churn puts about 48 MiB of rows in a scratch table of the database at path and
// deletes nine in ten, through a connection of its own, which is what a busy week
// followed by retention does: the rows are gone and the file is as long as ever.
func churn(t *testing.T, path string) {
	t.Helper()
	db, err := sql.Open("sqlite", "file:"+path+"?_pragma=busy_timeout(10000)&_pragma=journal_mode(WAL)")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	for _, q := range []string{
		`CREATE TABLE IF NOT EXISTS compaction_scratch (id INTEGER PRIMARY KEY, body BLOB)`,
		`WITH RECURSIVE n(i) AS (SELECT 1 UNION ALL SELECT i + 1 FROM n WHERE i < 12000) INSERT INTO compaction_scratch (body) SELECT zeroblob(4000) FROM n`,
		`DELETE FROM compaction_scratch WHERE id % 10 <> 0`,
		`PRAGMA wal_checkpoint(TRUNCATE)`,
	} {
		if _, err := db.Exec(q); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
	}
}

func fileBytes(t *testing.T, path string) int64 {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	return info.Size()
}

// compactable is a configuration and a store over a database made the way this
// release makes one, which is incremental. older writes it the way a release before
// incremental vacuum left one instead: a file that exists before the store opens it.
func compactable(t *testing.T, older bool) (*config.Config, *store.Store) {
	t.Helper()
	cfg := config.Default()
	dir := t.TempDir()
	cfg.Database.Path = filepath.Join(dir, "state", "zoomies.db")
	// A backup needs the key that seals the secrets in it, and the conversion
	// guard asks for a backup, so the configuration has one to read.
	key, err := cryptox.GenerateKey()
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}
	cfg.Security.EncryptionKeyFile = filepath.Join(dir, "encryption.key")
	if err := cryptox.WriteKeyFile(cfg.Security.EncryptionKeyFile, key); err != nil {
		t.Fatalf("WriteKeyFile: %v", err)
	}
	if older {
		if err := os.MkdirAll(filepath.Dir(cfg.Database.Path), 0o750); err != nil {
			t.Fatal(err)
		}
		db, err := sql.Open("sqlite", "file:"+cfg.Database.Path)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := db.Exec(`CREATE TABLE older_marker (id INTEGER)`); err != nil {
			t.Fatal(err)
		}
		db.Close()
	}
	st, err := store.Open(context.Background(), store.Options{Path: cfg.Database.Path})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	return cfg, st
}

func modeOf(t *testing.T, st *store.Store) int64 {
	t.Helper()
	stats, err := st.PageStats(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return stats.AutoVacuum
}

// The default: a database that can give its free pages back does, and says how much.
func TestACompactionPassGivesAnIncrementalDatabasesFreePagesBack(t *testing.T) {
	cfg, st := compactable(t, false)
	churn(t, cfg.Database.Path)
	before := fileBytes(t, cfg.Database.Path)

	result := Compact(context.Background(), st, cfg, time.Now())

	if result == nil || result.Error != "" || result.Skipped != "" {
		t.Fatalf("result = %+v, want a pass that gave space back", result)
	}
	if result.Mode != "incremental" || result.Converted {
		t.Errorf("result = %+v, want incremental and not converted", result)
	}
	if result.FreedBytes < before/2 {
		t.Errorf("FreedBytes = %d, want most of the %d the file held", result.FreedBytes, before)
	}
	if after := fileBytes(t, cfg.Database.Path); after > before/2 {
		t.Errorf("the file is %d bytes after the pass, from %d", after, before)
	}
}

// "off" never touches the file, and a database with nothing worth giving back is
// not news: the status is for what changed.
func TestACompactionPassThatHasNothingToDoSaysNothing(t *testing.T) {
	cfg, st := compactable(t, false)
	if result := Compact(context.Background(), st, cfg, time.Now()); result != nil {
		t.Errorf("a database with no free space reported %+v", result)
	}

	churn(t, cfg.Database.Path)
	before := fileBytes(t, cfg.Database.Path)
	cfg.Maintenance.Compact = "off"
	if result := Compact(context.Background(), st, cfg, time.Now()); result != nil || fileBytes(t, cfg.Database.Path) != before {
		t.Errorf("compact off reported %+v and the file went from %d to %d", result, before, fileBytes(t, cfg.Database.Path))
	}
}

// An existing database is left alone by "incremental", with nothing to report: it
// is the default for everyone, and rewriting a database is a choice.
func TestIncrementalModeLeavesAnOlderDatabaseAlone(t *testing.T) {
	cfg, st := compactable(t, true)
	churn(t, cfg.Database.Path)
	before := fileBytes(t, cfg.Database.Path)

	if result := Compact(context.Background(), st, cfg, time.Now()); result != nil {
		t.Errorf("result = %+v, want nothing: an older database is not an error", result)
	}
	if modeOf(t, st) != store.AutoVacuumNone || fileBytes(t, cfg.Database.Path) != before {
		t.Error("an older database was changed by a pass that was only asked to reclaim")
	}
}

// Converting rewrites the file and holds the writer, so it needs a second copy's
// worth of disk and a backup from the last day, and without either it does nothing
// and says what to do. With both it converts and gives the space back.
func TestConvertRewritesAnOlderDatabaseOnlyWhenADiskAndABackupAllowIt(t *testing.T) {
	ctx := context.Background()
	now := time.Now()

	t.Run("with room and a recent backup it converts", func(t *testing.T) {
		cfg, st := compactable(t, true)
		churn(t, cfg.Database.Path)
		before := fileBytes(t, cfg.Database.Path)
		cfg.Maintenance.Compact = "convert"
		take(t, cfg, st, TakeOptions{Source: SourceScheduled, Now: func() time.Time { return now.Add(-time.Hour) }})

		result := Compact(ctx, st, cfg, now)

		if result == nil || !result.Converted || result.Error != "" || result.Skipped != "" {
			t.Fatalf("result = %+v, want a conversion", result)
		}
		if modeOf(t, st) != store.AutoVacuumIncremental {
			t.Error("the database is not incremental after a conversion")
		}
		if after := fileBytes(t, cfg.Database.Path); after > before/2 || result.FreedBytes < before/2 {
			t.Errorf("file %d -> %d, FreedBytes %d: the rewrite should have dropped the free pages", before, after, result.FreedBytes)
		}
	})

	t.Run("without a backup from the last day it converts nothing and says so", func(t *testing.T) {
		cfg, st := compactable(t, true)
		churn(t, cfg.Database.Path)
		before := fileBytes(t, cfg.Database.Path)
		cfg.Maintenance.Compact = "convert"
		take(t, cfg, st, TakeOptions{Source: SourceScheduled, Now: func() time.Time { return now.Add(-72 * time.Hour) }})

		result := Compact(ctx, st, cfg, now)

		if result == nil || result.Converted || !strings.Contains(result.Skipped, "backup") {
			t.Fatalf("result = %+v, want a refusal that says a backup is needed", result)
		}
		if modeOf(t, st) != store.AutoVacuumNone || fileBytes(t, cfg.Database.Path) != before {
			t.Error("the database was rewritten without a recent backup")
		}
	})

	t.Run("an uploaded backup is not a backup of this database", func(t *testing.T) {
		cfg, st := compactable(t, true)
		cfg.Maintenance.Compact = "convert"
		take(t, cfg, st, TakeOptions{Source: SourceUploaded, Now: func() time.Time { return now.Add(-time.Hour) }})
		if result := Compact(ctx, st, cfg, now); result == nil || result.Converted || result.Skipped == "" {
			t.Errorf("result = %+v, want a refusal: somebody carried that backup here", result)
		}
	})

	t.Run("with backups switched off there is no backup to wait for", func(t *testing.T) {
		cfg, st := compactable(t, true)
		cfg.Maintenance.Compact = "convert"
		cfg.Backup.Interval = 0
		if result := Compact(ctx, st, cfg, now); result == nil || !result.Converted {
			t.Errorf("result = %+v, want a conversion: the operator switched backups off, and the refusal would never clear", result)
		}
	})

	t.Run("without room for a second copy it converts nothing and says so", func(t *testing.T) {
		cfg, st := compactable(t, true)
		churn(t, cfg.Database.Path)
		cfg.Maintenance.Compact = "convert"
		take(t, cfg, st, TakeOptions{Source: SourceScheduled, Now: func() time.Time { return now.Add(-time.Hour) }})
		size := fileBytes(t, cfg.Database.Path)
		was := freeDiskBytes
		freeDiskBytes = func(string) (int64, bool) { return size + size/2, true } // less than twice the file
		t.Cleanup(func() { freeDiskBytes = was })

		result := Compact(ctx, st, cfg, now)

		if result == nil || result.Converted || !strings.Contains(result.Skipped, "disk") {
			t.Fatalf("result = %+v, want a refusal that says the disk is too full", result)
		}
		if modeOf(t, st) != store.AutoVacuumNone {
			t.Error("the database was rewritten without room for a second copy")
		}
	})

	t.Run("a disk that cannot say how much is free is not assumed to have room", func(t *testing.T) {
		cfg, st := compactable(t, true)
		cfg.Maintenance.Compact = "convert"
		take(t, cfg, st, TakeOptions{Source: SourceScheduled, Now: func() time.Time { return now.Add(-time.Hour) }})
		was := freeDiskBytes
		freeDiskBytes = func(string) (int64, bool) { return 0, false }
		t.Cleanup(func() { freeDiskBytes = was })
		if result := Compact(ctx, st, cfg, now); result == nil || result.Converted || result.Skipped == "" {
			t.Errorf("result = %+v, want a refusal", result)
		}
	})
}

// Conversion then reclaiming is one story: a database converted this pass is
// incremental for the next, and a later pass under the same setting reclaims as a
// default pass does.
func TestAnAlreadyIncrementalDatabaseUnderConvertIsJustReclaimed(t *testing.T) {
	cfg, st := compactable(t, false)
	churn(t, cfg.Database.Path)
	cfg.Maintenance.Compact = "convert"
	result := Compact(context.Background(), st, cfg, time.Now())
	if result == nil || result.Converted || result.Skipped != "" || result.FreedBytes == 0 {
		t.Errorf("result = %+v, want a reclaim and no conversion: the database was already incremental", result)
	}
}

// The copy a backup takes is made by VACUUM INTO, and whether that keeps the
// database's mode decides what a restore does to it. It does keep it, so an
// incremental database restores as one, and an older one restores as it was and
// waits for maintenance.compact to be convert: the restore documentation says so,
// and this is what holds it to that.
func TestARestoredBackupKeepsTheModeItWasTakenIn(t *testing.T) {
	ctx := context.Background()
	for _, tc := range []struct {
		name  string
		older bool
		want  int64
	}{
		{"a database made by this release stays incremental", false, store.AutoVacuumIncremental},
		{"an older database restores as it was", true, store.AutoVacuumNone},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg, st := compactable(t, tc.older)
			e := take(t, cfg, st, TakeOptions{})

			if _, err := Restore(ctx, cfg, e.Dir, RestoreOptions{Replace: true}); err != nil {
				t.Fatalf("Restore: %v", err)
			}

			restored, err := store.Open(ctx, store.Options{Path: cfg.Database.Path, ReadOnly: true})
			if err != nil {
				t.Fatalf("opening what was restored: %v", err)
			}
			defer restored.Close()
			if got := modeOf(t, restored); got != tc.want {
				t.Errorf("auto_vacuum = %d after the restore, want %d", got, tc.want)
			}
		})
	}
}

// VACUUM builds its working copy in the temporary directory, which is often a
// small tmpfs, so a rewrite is refused when that directory cannot hold one even
// though the database's own disk can.
func TestAConversionIsRefusedWhenTheTemporaryDirectoryIsTooSmall(t *testing.T) {
	cfg, st := compactable(t, true)
	churn(t, cfg.Database.Path)
	cfg.Maintenance.Compact = "convert"
	now := time.Now()
	take(t, cfg, st, TakeOptions{Source: SourceScheduled, Now: func() time.Time { return now.Add(-time.Hour) }})
	size := fileBytes(t, cfg.Database.Path)
	was := freeDiskBytes
	freeDiskBytes = func(p string) (int64, bool) {
		if p == os.TempDir() {
			return size / 2, true
		}
		return 100 * size, true
	}
	t.Cleanup(func() { freeDiskBytes = was })

	result := Compact(context.Background(), st, cfg, now)

	if result == nil || result.Converted || !strings.Contains(result.Skipped, "temporary directory") {
		t.Fatalf("result = %+v, want a refusal that names the temporary directory", result)
	}
	if modeOf(t, st) != store.AutoVacuumNone {
		t.Error("the database was rewritten with no room for VACUUM's working copy")
	}
}
