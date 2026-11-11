package backup

import (
	"fmt"
	"slices"
	"time"

	"github.com/eyupio/zoomies/internal/config"
	"github.com/eyupio/zoomies/internal/store"
)

// MaintenanceResult is what one maintenance pass removed, and what it could not.
//
// It is what the controller logs and what the storage report shows as the last
// pass, so an operator who has just lowered a count can see it take effect, and
// one who did not can see why a directory is still large.
type MaintenanceResult struct {
	// At is when the pass ran.
	At time.Time `json:"at"`
	// Backups are the ids of the controller's own backups removed because there
	// were more than backup.keep. An uploaded or fetched backup is never one.
	Backups []string `json:"backups"`
	// PreMigration are the names of the copies taken before a migration that were
	// removed because there were more than backup.pre_migration_keep.
	PreMigration []string `json:"pre_migration"`
	// Errors say what could not be done, one sentence each. A directory that
	// failed does not stop the other being tidied.
	Errors []string `json:"errors"`
	// Compaction is what the pass did to the database file itself, and is absent
	// when it did nothing, or was not asked to: only the scheduled pass compacts,
	// because a conversion holds the writer for as long as the rewrite takes and
	// that is not something to do inside the request of a button.
	Compaction *CompactionResult `json:"compaction,omitempty"`
}

// PrunePreMigrationCopies removes the copies the store took before a migration
// beyond backup.pre_migration_keep, for the database the configuration names, and
// returns the names it removed.
//
// It is separate from Maintain because the controller calls it on its own at
// start, as soon as its configuration is loaded: Open takes a copy and prunes
// nothing, since it runs before any setting can be read, and the prune at start is
// what stops a controller whose scheduled pass is switched off from holding copies
// that nothing ever removes.
func PrunePreMigrationCopies(cfg *config.Config) ([]string, error) {
	return store.PrunePreMigration(cfg.Database.Path, cfg.Backup.PreMigrationKeep)
}

// Maintain is one maintenance pass: it removes the controller's own backups
// beyond backup.keep and the copies taken before a migration beyond
// backup.pre_migration_keep, and says what it removed.
//
// Each directory is tried whatever happened to the other. A pass that gave up at
// its first error would leave the second directory growing for as long as the
// first stayed broken, so what failed is in Errors and what worked is in the
// lists beside it. backup.keep of zero keeps every backup, as it does everywhere
// else, and a pre-migration keep below one removes nothing.
func Maintain(cfg *config.Config, now time.Time) *MaintenanceResult {
	result := &MaintenanceResult{At: now, Backups: []string{}, PreMigration: []string{}, Errors: []string{}}

	root := Dir(cfg)
	removed, err := Prune(root, cfg.Backup.Keep)
	// What was removed before an error is still removed, so it is still said. Oldest
	// first, as the pre-migration copies are: Prune removes the newest of what it
	// removes first, and a report in two orders is one nobody can read at a glance.
	// The names carry the instant, so sorting them sorts by age.
	result.Backups = append(result.Backups, removed...)
	slices.Sort(result.Backups)
	if err != nil {
		result.Errors = append(result.Errors, fmt.Sprintf("tidying the backups in %s: %v", root, err))
	}

	removed, err = PrunePreMigrationCopies(cfg)
	result.PreMigration = append(result.PreMigration, removed...)
	if err != nil {
		result.Errors = append(result.Errors, fmt.Sprintf("tidying the pre-migration copies in %s: %v", PreMigrationDir(cfg.Database.Path), err))
	}
	return result
}
