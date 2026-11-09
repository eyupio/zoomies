package main

import (
	"cmp"
	"context"
	"fmt"
	"slices"
	"strings"
	"time"
)

// storageReport is GET /api/v1/storage, as much of it as the table needs.
//
// It is declared here rather than imported from internal/backup, as every other
// command's types are, so the command reads the document the API promises and
// not whatever the controller package happens to be called this release.
type storageReport struct {
	Version           int            `json:"version"`
	Directory         string         `json:"directory"`
	BackupDirectory   string         `json:"backup_directory"`
	Entries           []storageEntry `json:"entries"`
	DatabaseBytes     int64          `json:"database_bytes"`
	DatabaseFreeBytes int64          `json:"database_free_bytes"`
	// DatabaseAutoVacuum is "incremental" for a database that can give free pages
	// back, and "none" for one that cannot until it is converted. A controller
	// that does not say leaves it empty, and the command then says nothing of it.
	DatabaseAutoVacuum string `json:"database_auto_vacuum"`
	TotalBytes         int64  `json:"total_bytes"`
	DiskFreeBytes      *int64 `json:"disk_free_bytes"`
	// LastMaintenance is what the last tidy of the directory removed, and is
	// absent until there has been one.
	LastMaintenance *storageTidy `json:"last_maintenance"`
}

// storageTidy is one maintenance pass: what it removed and what it could not.
type storageTidy struct {
	At           time.Time `json:"at"`
	Backups      []string  `json:"backups"`
	PreMigration []string  `json:"pre_migration"`
	Errors       []string  `json:"errors"`
	// Compaction is what the pass did to the database file, and is absent when it
	// did nothing.
	Compaction *storageCompaction `json:"compaction"`
}

// storageCompaction is what the scheduled pass did to the database file.
type storageCompaction struct {
	Mode       string `json:"mode"`
	Converted  bool   `json:"converted"`
	FreedBytes int64  `json:"freed_bytes"`
	Skipped    string `json:"skipped"`
	Error      string `json:"error"`
}

type storageEntry struct {
	Path     string `json:"path"`
	Bytes    int64  `json:"bytes"`
	Class    string `json:"class"`
	Optional bool   `json:"optional"`
}

// storageReportVersion is the one shape this command reads. A report in another
// is refused, because the classes are what an operator decides what to carry
// from, and reading a changed meaning as the old one carries the wrong things.
const storageReportVersion = 1

// runStorage is `zoomies storage`.
func runStorage(ctx context.Context, e *env, args []string) error {
	fs := newFlagSet(e, "zoomies storage",
		"Say what is in the controller's state directory, how big each entry is, and which of it\n"+
			"a move would have to carry and which the controller could make again.")
	cf := registerClientFlags(fs, true)
	fs.example(
		"zoomies storage",
		"zoomies storage --output json",
	)
	if err := fs.parse(args); err != nil {
		return err
	}
	if err := fs.noMoreArgs(); err != nil {
		return err
	}
	client, err := cf.client()
	if err != nil {
		return err
	}
	p, err := cf.printer(e)
	if err != nil {
		return err
	}

	var out storageReport
	raw, err := client.get(ctx, "/storage", nil, &out)
	if err != nil {
		return err
	}
	if out.Version != storageReportVersion {
		return fmt.Errorf("the controller sent a storage report in version %d, which this zoomies does not read (it reads version %d): "+
			"upgrade this command to match the controller", out.Version, storageReportVersion)
	}
	if p.structured() {
		return p.emit(raw)
	}

	// Biggest first: the question behind the command is where the space went.
	entries := slices.Clone(out.Entries)
	slices.SortStableFunc(entries, func(a, b storageEntry) int {
		if c := cmp.Compare(b.Bytes, a.Bytes); c != 0 {
			return c
		}
		return cmp.Compare(a.Path, b.Path)
	})
	rows := make([][]string, 0, len(entries))
	for _, entry := range entries {
		rows = append(rows, []string{entry.Path, p.storageClass(entry), sizeLabel(entry.Bytes)})
	}
	p.table([]string{"path", "class", "size"}, rows)

	// What a move carries is everything the controller needs, everything
	// somebody put here on purpose, and anything this release cannot name: the
	// safe mistake is to carry too much.
	var carry, regenerable int64
	for _, entry := range out.Entries {
		if entry.Class == "regenerable" {
			regenerable += entry.Bytes
		} else {
			carry += entry.Bytes
		}
	}
	database := sizeLabel(out.DatabaseBytes)
	if out.DatabaseFreeBytes > 0 {
		database += ", of which " + sizeLabel(out.DatabaseFreeBytes) + " is free pages a delete left in the file"
	}
	switch out.DatabaseAutoVacuum {
	case "incremental":
		database += " (incremental auto-vacuum: the maintenance pass gives free pages back)"
	case "none":
		// What an operator with a large file wants to know next is how to get the
		// space back, and the answer is a setting they may never have heard of.
		database += " (made without auto-vacuum: set maintenance.compact to convert to give free pages back)"
	}
	summary := [][2]string{
		{"directory", out.Directory},
		{"total", sizeLabel(out.TotalBytes)},
		{"carry when moving", sizeLabel(carry) + "  essential, kept and unknown"},
		{"can be left behind", sizeLabel(regenerable) + "  regenerable"},
		{"database", database},
		{"backups go to", out.BackupDirectory},
	}
	if out.DiskFreeBytes != nil {
		summary = append(summary, [2]string{"disk free", sizeLabel(*out.DiskFreeBytes)})
	}
	if t := out.LastMaintenance; t != nil {
		summary = append(summary, [2]string{"last tidy", p.relTime(t.At) + ", " + tidySentence(t)})
	}
	p.note("")
	p.keyValues(summary)
	if t := out.LastMaintenance; t != nil {
		for _, e := range t.Errors {
			p.note("could not tidy: %s", e)
		}
		if k := t.Compaction; k != nil {
			if k.Skipped != "" {
				p.note("did not compact the database: %s", k.Skipped)
			}
			if k.Error != "" {
				p.note("could not compact the database: %s", k.Error)
			}
		}
	}
	p.note("")
	// The one entry in that class that cannot be made again, said where somebody
	// about to delete things for space will read it.
	for _, entry := range out.Entries {
		if strings.HasPrefix(entry.Path, "pre-migration/") {
			p.note("A pre-migration copy is the rollback for the last upgrade, and cannot be taken again once the upgrade has run.")
			break
		}
	}
	p.note("The encryption key is listed only when it lives in this directory. By default it is in the")
	p.note("configuration directory, which this does not read, and a move needs it as well.")
	return nil
}

// tidySentence says what a maintenance pass did, in the units an operator counts:
// backups, the copies taken before a migration, and the space given back from the
// database file.
func tidySentence(t *storageTidy) string {
	var parts []string
	if n := len(t.Backups); n > 0 {
		parts = append(parts, countedAs(n, "backup", "backups"))
	}
	if n := len(t.PreMigration); n > 0 {
		parts = append(parts, countedAs(n, "pre-migration copy", "pre-migration copies"))
	}
	if len(parts) > 0 {
		parts = []string{"removed " + strings.Join(parts, " and ")}
	}
	if k := t.Compaction; k != nil {
		switch {
		case k.Converted:
			parts = append(parts, "converted the database to incremental auto-vacuum")
		case k.FreedBytes > 0:
			parts = append(parts, "gave "+sizeLabel(k.FreedBytes)+" of the database file back to the disk")
		}
	}
	if len(parts) == 0 {
		return "removed nothing"
	}
	return strings.Join(parts, ", ")
}

func countedAs(n int, one, many string) string {
	if n == 1 {
		return "1 " + one
	}
	return fmt.Sprintf("%d %s", n, many)
}

// storageClass is an entry's class as the table shows it: an entry whose
// absence is normal says so, and the class a reader should treat as essential is
// the one that stands out.
func (p *printer) storageClass(e storageEntry) string {
	class := e.Class
	if e.Optional {
		class += " (optional)"
	}
	if e.Class == "unknown" {
		return p.paint(colourYellow, class)
	}
	return class
}

// sizeLabel renders a size the way an operator reading a disk thinks about one.
// It goes up to gigabytes, because a state directory is measured in them.
func sizeLabel(n int64) string {
	switch {
	case n >= 1<<30:
		return fmt.Sprintf("%.1f GB", float64(n)/(1<<30))
	case n >= 1<<20:
		return fmt.Sprintf("%.1f MB", float64(n)/(1<<20))
	case n >= 1<<10:
		return fmt.Sprintf("%.1f KB", float64(n)/(1<<10))
	default:
		return fmt.Sprintf("%d B", n)
	}
}
