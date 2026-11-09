package controller

import (
	"context"
	"strings"
	"sync"
	"time"

	"github.com/eyupio/zoomies/internal/backup"
	"github.com/eyupio/zoomies/internal/config"
)

// maintenanceTick is how often the loop asks whether a pass is due. A minute is
// as fine as the interval's own floor, and coarse enough that an idle controller
// does not read its backup directory a thousand times an hour.
const maintenanceTick = time.Minute

// maintenanceState is what the loop remembers, in memory: the directories
// themselves are the durable record, and the storage report only needs to say
// what the last thing that happened was.
type maintenanceState struct {
	mu sync.Mutex
	// last is what the last pass, or the prune at start, removed or could not.
	// It is never nil once something was removed, and is never changed after it
	// is stored, so it is handed out as it is.
	last *backup.MaintenanceResult
	// lastPassAt is when a whole pass last ran. It is zero until one has, so the
	// first tick after a start finds a pass due: a controller restarted more often
	// than its interval would otherwise never be tidied.
	lastPassAt time.Time
	// owed says a pass was asked for, or came due, while a backup was being taken,
	// and has not run yet.
	owed bool
	// notCompacted is the last scheduled pass's compaction when it was refused or
	// failed, and is nil as soon as a pass finds nothing to complain of. It is what
	// the problems list is made from, so a refusal stays on the list for as long as
	// it stands and goes when it is put right.
	notCompacted *backup.CompactionResult
	// notCompactedAt is when that pass ran.
	notCompactedAt time.Time
}

// maintenanceLoop tidies the controller's own state directory.
//
// It is its own loop, as backupLoop is, so that a pass over a large directory never
// delays housekeeping. It prunes the copies taken before a migration once before
// it waits for anything, whatever maintenance.interval says: Open takes a copy and
// prunes nothing, because it runs before any setting can be read, and an operator
// who has switched the scheduled pass off must not be left with copies that
// nothing ever removes.
func (c *Controller) maintenanceLoop(ctx context.Context) {
	c.pruneAtStart()
	ticker := time.NewTicker(maintenanceTick)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
		c.maintenancePass(ctx, false)
	}
}

// pruneAtStart removes the copies taken before a migration beyond
// backup.pre_migration_keep, with the configuration the controller has just
// loaded. If that configuration says no number a person could have meant, nothing
// is removed: keeping too many copies is the safe direction.
func (c *Controller) pruneAtStart() {
	cfg := c.cfg()
	removed, err := backup.PrunePreMigrationCopies(cfg)
	if len(removed) == 0 && err == nil {
		return
	}
	result := &backup.MaintenanceResult{At: c.Now(), Backups: []string{}, PreMigration: removed, Errors: []string{}}
	if result.PreMigration == nil {
		result.PreMigration = []string{}
	}
	if err != nil {
		result.Errors = append(result.Errors, "tidying the pre-migration copies at start: "+err.Error())
	}
	c.recordMaintenance(result, false)
}

// maintenancePass is one turn of the loop, pulled out so a test can run one
// without waiting for a tick. asked says a person wanted it now, as the button on
// the Backups page does (PruneBackups), which runs it whatever the interval says,
// including 0.
func (c *Controller) maintenancePass(ctx context.Context, asked bool) {
	if ctx.Err() != nil {
		return
	}
	cfg := c.cfg()
	now := c.Now()
	if !asked && !c.maintenanceDue(now, cfg.Maintenance.Interval) {
		return
	}
	// Not beside a backup being taken: TakeBackup runs its own retention when it
	// finishes, and a prune that raced the copy could delete the one being
	// written. The pass is owed and runs on the next tick rather than being
	// dropped, so a person who asked for it still gets it.
	if !c.beginBackup() {
		c.maintenance.mu.Lock()
		c.maintenance.owed = true
		c.maintenance.mu.Unlock()
		return
	}
	defer c.endBackup()
	result := backup.Maintain(cfg, now)
	// Only the scheduled pass compacts. The button on the Backups page waits for
	// its answer, and a conversion can hold the writer for minutes: nobody asked
	// for that by asking for old copies to be removed.
	if !asked {
		result.Compaction = c.compact(ctx, cfg, now)
	}
	c.recordMaintenance(result, true)
}

// compact gives the database file's free pages back, if the configuration says to,
// and remembers a refusal or a failure for the problems list.
func (c *Controller) compact(ctx context.Context, cfg *config.Config, now time.Time) *backup.CompactionResult {
	result := backup.Compact(ctx, c.st, cfg, now)
	if result != nil && ctx.Err() != nil {
		// A controller that is stopping interrupts a pass, and that is not a fault
		// anybody can act on. What was given back before it stopped still counts.
		result.Error = ""
		if result.FreedBytes == 0 && !result.Converted && result.Skipped == "" {
			result = nil
		}
	}
	c.maintenance.mu.Lock()
	defer c.maintenance.mu.Unlock()
	if result != nil && (result.Skipped != "" || result.Error != "") {
		c.maintenance.notCompacted, c.maintenance.notCompactedAt = result, now
	} else {
		c.maintenance.notCompacted = nil
	}
	return result
}

// compactionProblems says why the database file was not compacted, while that is
// still so. The conversion is the one thing here an operator opted into and can be
// refused, so the entry names the reason the pass gave and what to change.
func (c *Controller) compactionProblems() []Problem {
	c.maintenance.mu.Lock()
	result, at := c.maintenance.notCompacted, c.maintenance.notCompactedAt
	c.maintenance.mu.Unlock()
	// A setting changed since the pass ran has already answered it: the entry goes
	// when the operator asks for something else, and not a day later.
	if result == nil || strings.ToLower(strings.TrimSpace(c.cfg().Maintenance.Compact)) != result.Mode {
		return nil
	}
	p := Problem{
		Code:     "maintenance.compact_skipped",
		Severity: config.SeverityWarning,
		Setting:  "maintenance.compact",
		Since:    &at,
	}
	if result.Skipped != "" {
		p.Title = "the database was not converted to give back its free space"
		p.Detail = "maintenance.compact is convert, and the last maintenance pass refused to rewrite the database: " + result.Skipped + ". Nothing was changed."
		p.Fix = "put right what that says and the next maintenance pass converts the database, or set maintenance.compact to incremental to stop asking."
	} else {
		p.Title = "giving back the database's free space failed"
		p.Detail = "the last maintenance pass could not give the database file's free pages back: " + result.Error + ". The database was not damaged: a rewrite that fails leaves the original file as it was."
		p.Fix = "read the error and put right what it names; the next maintenance pass tries again. Set maintenance.compact to off to stop it trying."
	}
	return []Problem{p}
}

// maintenanceDue says whether the schedule, or a pass owed from earlier, wants a
// pass now. An interval of 0 is a schedule that never comes due.
func (c *Controller) maintenanceDue(now time.Time, interval time.Duration) bool {
	c.maintenance.mu.Lock()
	defer c.maintenance.mu.Unlock()
	if c.maintenance.owed {
		return true
	}
	if interval <= 0 {
		return false
	}
	return c.maintenance.lastPassAt.IsZero() || now.Sub(c.maintenance.lastPassAt) >= interval
}

// recordMaintenance keeps what a pass did and says it in the log. Only a pass
// that removed something, or could not, is kept as the last: the status is for
// what changed, and a quiet pass every day would overwrite the one that did.
func (c *Controller) recordMaintenance(r *backup.MaintenanceResult, wholePass bool) {
	c.maintenance.mu.Lock()
	if wholePass {
		c.maintenance.lastPassAt = r.At
		c.maintenance.owed = false
	}
	if len(r.Backups) > 0 || len(r.PreMigration) > 0 || len(r.Errors) > 0 || r.Compaction != nil {
		c.maintenance.last = r
	}
	c.maintenance.mu.Unlock()

	cfg := c.cfg()
	if len(r.Backups) > 0 {
		c.log.Info("removed old backups", "removed", r.Backups, "keep", cfg.Backup.Keep, "dir", backup.Dir(cfg))
	}
	if len(r.PreMigration) > 0 {
		c.log.Info("removed old pre-migration copies", "removed", r.PreMigration, "keep", cfg.Backup.PreMigrationKeep, "dir", backup.PreMigrationDir(cfg.Database.Path))
	}
	for _, e := range r.Errors {
		c.log.Warn("could not tidy the state directory", "error", e)
	}
	if k := r.Compaction; k != nil {
		switch {
		case k.Error != "":
			c.log.Warn("could not give the database's free space back", "mode", k.Mode, "error", k.Error)
		case k.Skipped != "":
			c.log.Warn("did not convert the database to give back its free space", "reason", k.Skipped)
		case k.Converted:
			c.log.Info("converted the database to incremental auto-vacuum", "freed_bytes", k.FreedBytes)
		default:
			c.log.Info("gave the database's free space back to the disk", "freed_bytes", k.FreedBytes)
		}
	}
}

// LastMaintenance is what the last pass that removed something, or failed to,
// did. Nil until there is one.
func (c *Controller) LastMaintenance() *backup.MaintenanceResult {
	c.maintenance.mu.Lock()
	defer c.maintenance.mu.Unlock()
	return c.maintenance.last
}
