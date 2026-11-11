package backup

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/eyupio/zoomies/internal/config"
	"github.com/eyupio/zoomies/internal/store"
)

// freeDiskBytes is how the pass asks how much room the disk has. It is a variable
// so that a test can say the disk is nearly full, which no test machine will
// oblige by being.
var freeDiskBytes = DiskFree

// recentBackupWithin is how old the backup a conversion waits for may be.
const recentBackupWithin = 24 * time.Hour

// CompactionResult is what the maintenance pass did to the database file, and is
// absent when it did nothing, so that a quiet night does not look like news.
type CompactionResult struct {
	// Mode is maintenance.compact as the pass read it.
	Mode string `json:"mode"`
	// Converted says an older database was rewritten into incremental mode.
	Converted bool `json:"converted"`
	// FreedBytes is the pages given back, times the page size.
	FreedBytes int64 `json:"freed_bytes"`
	// Skipped says why a conversion that was asked for was refused, as a sentence
	// that names what to do. It is empty when nothing was refused.
	Skipped string `json:"skipped,omitempty"`
	// ConversionFailed says the rewrite itself was attempted and failed, which is
	// what the controller backs off from: a rewrite holds the writer and the backup
	// gate for as long as it runs, and a pass that retried it every interval would
	// do that every interval.
	ConversionFailed bool `json:"conversion_failed,omitempty"`
	// Error says what failed. A rewrite that fails leaves the original file as it
	// was.
	Error string `json:"error,omitempty"`
}

// Compact gives the database file's free pages back to the disk, if the
// configuration says to and the file is in a mode that can.
//
// "incremental" gives them back from a database that was made in incremental
// mode, which every database this release creates is, in slices that let other
// writes in between. It leaves an older database alone and says nothing, because
// that is the default for everybody and not a fault. "convert" also rewrites an
// older database into incremental mode, once, which holds the one writer for as
// long as the rewrite takes: so it is only done when the disk has room for a second
// copy and a backup of the database from the last day exists, and otherwise it does
// nothing and says what to do. "off" does nothing.
//
// It returns nil when there was nothing to do.
func Compact(ctx context.Context, st *store.Store, cfg *config.Config, now time.Time) *CompactionResult {
	mode := strings.ToLower(strings.TrimSpace(cfg.Maintenance.Compact))
	if mode == "" {
		mode = "incremental"
	}
	if mode == "off" {
		return nil
	}
	result := &CompactionResult{Mode: mode}
	stats, err := st.PageStats(ctx)
	if err != nil {
		result.Error = err.Error()
		return result
	}

	if stats.AutoVacuum != store.AutoVacuumIncremental {
		if mode != "convert" {
			return nil
		}
		if reason := refuseConversion(cfg, now); reason != "" {
			result.Skipped = reason
			return result
		}
		converted, err := st.ConvertToIncrementalVacuum(ctx)
		result.Converted = converted
		if err != nil {
			result.Error = err.Error()
			result.ConversionFailed = !converted
		}
		if after, perr := st.PageStats(ctx); perr == nil {
			result.FreedBytes = max((stats.PageCount-after.PageCount)*after.PageSize, 0)
		}
		return result
	}

	reclaimed, err := st.Reclaim(ctx, store.ReclaimOptions{MinFreePercent: cfg.Maintenance.CompactFreePercent})
	result.FreedBytes = reclaimed.FreedBytes
	if err != nil {
		result.Error = err.Error()
		return result
	}
	if reclaimed.Slices == 0 {
		return nil
	}
	return result
}

// refuseConversion says why an older database may not be rewritten now, or "" when
// it may. Each reason is a sentence an operator can act on, because it is what the
// problems drawer shows.
//
// A rewrite builds a second copy of the file beside the first, so it needs about
// twice the database's size free, and a disk that cannot say how much it has free
// is not assumed to have enough. It holds the one writer, so the database must
// have been backed up in the last day: if the rewrite goes wrong, that backup is
// the way back. An operator who has switched backups off has said they do not want
// one, and a refusal that could never clear would only be noise.
func refuseConversion(cfg *config.Config, now time.Time) string {
	dir := filepath.Dir(cfg.Database.Path)
	info, err := os.Stat(cfg.Database.Path)
	if err != nil {
		return fmt.Sprintf("the database file could not be read to say how much room a rewrite needs: %v", err)
	}
	free, ok := freeDiskBytes(dir)
	// VACUUM builds its working copy in the temporary directory, which is not the
	// database's and on many hosts is a small tmpfs. A rewrite that fills it fails
	// after it has held the writer for as long as it took to get that far.
	tmp := os.TempDir()
	tmpFree, tmpOK := freeDiskBytes(tmp)
	dirNeeds, tmpNeeds := 2*info.Size(), info.Size()
	if sameDirectory(tmp, dir) {
		// One disk holds the new file and the working copy both.
		dirNeeds, tmpNeeds = 3*info.Size(), 0
	}
	switch {
	case !ok:
		return "this platform cannot say how much disk is free, so a rewrite of the database, which needs room for a second copy, is not attempted; leave maintenance.compact at incremental"
	case !tmpOK:
		return fmt.Sprintf("this platform cannot say how much room %s has, and the rewrite of the database builds a working copy there; leave maintenance.compact at incremental", tmp)
	case tmpFree < tmpNeeds:
		return fmt.Sprintf("the temporary directory %s has %s free and rewriting the %s database builds a working copy there; free some space or point TMPDIR at a bigger disk, or leave maintenance.compact at incremental", tmp, sizeText(tmpFree), sizeText(info.Size()))
	case free < dirNeeds:
		return fmt.Sprintf("the disk has %s free and rewriting the %s database needs about %s; free some space, or leave maintenance.compact at incremental", sizeText(free), sizeText(info.Size()), sizeText(dirNeeds))
	}
	if cfg.Backup.Interval > 0 && !hasRecentBackup(cfg, now) {
		return fmt.Sprintf("there is no backup of this database from the last day in %s, and the rewrite holds the writer while it runs; take one (zoomies backup, or the Backups page) and the next pass will convert it", Dir(cfg))
	}
	return ""
}

// sameDirectory says two paths are one directory, so that the room both of a
// rewrite's needs ask for is not counted twice over.
func sameDirectory(a, b string) bool {
	ai, err := os.Stat(a)
	if err != nil {
		return false
	}
	bi, err := os.Stat(b)
	return err == nil && os.SameFile(ai, bi)
}

// hasRecentBackup says the controller or an operator took a backup of this
// database within the last day. One that was uploaded or fetched back is somebody
// else's copy of some other moment, and does not count.
func hasRecentBackup(cfg *config.Config, now time.Time) bool {
	entries, err := List(Dir(cfg))
	if err != nil {
		return false
	}
	for _, e := range entries {
		if e.Manifest == nil || e.Problem != "" || e.Source == SourceUploaded || e.Source == SourceFetched {
			continue
		}
		if now.Sub(e.TakenAt) <= recentBackupWithin {
			return true
		}
	}
	return false
}

// sizeText is a size in the words a person reads a disk in.
func sizeText(n int64) string {
	switch {
	case n >= 1<<30:
		return fmt.Sprintf("%.1f GB", float64(n)/(1<<30))
	case n >= 1<<20:
		return fmt.Sprintf("%.1f MB", float64(n)/(1<<20))
	default:
		return fmt.Sprintf("%d KB", n>>10)
	}
}
