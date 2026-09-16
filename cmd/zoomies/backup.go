package main

import (
	"context"
	"fmt"
	"io"
	"path/filepath"
	"strings"
	"time"

	"github.com/eyupio/zoomies/internal/backup"
	"github.com/eyupio/zoomies/internal/config"
	"github.com/eyupio/zoomies/internal/store"
)

// The backup's own names, so the command and its tests speak of one layout.
// The implementation is internal/backup, shared with the controller's
// scheduled copies and the settings page: one copy taken by any of them is
// restorable by the command below.
const (
	backupManifestVersion = backup.ManifestVersion
	backupDBName          = backup.DBName
	backupDirPrefix       = backup.DirPrefix
	backupManifestName    = backup.ManifestName
	backupKeyName         = backup.KeyName
)

type backupManifest = backup.Manifest

// runBackup is `zoomies backup`.
//
// It opens the database file rather than talking to a controller, and that is
// deliberate: a backup is taken on the machine that holds the data, by whoever
// can read it, and it has to work when the controller will not start. The API
// route that takes one from the settings page is the other way in, and both
// write the same directory.
func runBackup(ctx context.Context, e *env, args []string) error {
	fs := newFlagSet(e, "zoomies backup [--dir /var/backups/zoomies] [--keep 7] [--include-key] [--no-offsite]",
		"Take a consistent copy of the database, with a manifest saying what it is and what it needs.")
	cfgPath := fs.String("config", "", "path to zoomies.yaml (default: "+config.DefaultConfigFile()+")")
	dir := fs.String("dir", "", "where to put the backup (default: backup.directory, or a `backups` directory beside the database)")
	keep := fs.Int("keep", 0, "delete all but the newest N backups in --dir; 0 keeps every one")
	includeKey := fs.Bool("include-key", false,
		"copy the encryption key into the backup as well. Convenient and dangerous: the copy then decrypts itself")
	remote := fs.String("remote", "", "send the copy to this backup remote only, instead of every one backup.remotes enables")
	noOffsite := fs.Bool("no-offsite", false, "keep the copy on this host, whatever backup.remotes says")
	fs.example("zoomies backup",
		"zoomies backup --dir /var/backups/zoomies --keep 7",
		"zoomies backup --include-key",
		"zoomies backup --remote offsite")
	if err := fs.parse(args); err != nil {
		return err
	}
	if err := fs.noMoreArgs(); err != nil {
		return err
	}

	cfg, err := config.Load(*cfgPath)
	if err != nil {
		return err
	}
	root := strings.TrimSpace(*dir)
	if root == "" {
		root = backup.Dir(cfg)
	}

	st, err := store.Open(ctx, store.Options{Path: cfg.Database.Path, ReadOnly: true})
	if err != nil {
		return fmt.Errorf("opening %s: %w", cfg.Database.Path, err)
	}
	defer func() { _ = st.Close() }()

	entry, err := backup.Take(ctx, st, backup.TakeOptions{
		Config: cfg, Dir: root, IncludeKey: *includeKey, Source: backup.SourceCLI,
	})
	if err != nil {
		return err
	}

	fmt.Fprintf(e.out, "Backed up to %s\n\n", entry.Dir)
	printBackupSummary(e.out, entry.Manifest, entry.Dir)

	if *keep > 0 {
		removed, err := backup.Prune(root, *keep)
		if err != nil {
			return err
		}
		if len(removed) > 0 {
			fmt.Fprintf(e.out, "\nRemoved %s, keeping the newest %d.\n", countOf(len(removed), "older backup"), *keep)
		}
	}

	// And then off the machine, because a copy beside the database is a backup
	// against a mistake rather than against the disk. It is the copy the
	// configuration already asked for: --no-offsite is how an operator taking a
	// one-off copy says this one stays here.
	if *noOffsite {
		return nil
	}
	return shipFromCLI(ctx, e, cfg, st, entry, *remote)
}

// shipFromCLI copies one backup to the remotes and says what happened, in the
// shape the command's other output takes.
//
// A failure here is reported and is not the command's exit status: the backup
// was taken, it is on the disk, and telling an operator their backup failed
// because a bucket refused a signature would be a lie about the thing they
// asked for.
func shipFromCLI(ctx context.Context, e *env, cfg *config.Config, st *store.Store, entry *backup.Entry, only string) error {
	resolved, err := remotesFor(ctx, cfg, st)
	if err != nil {
		return err
	}
	var remotes []config.BackupRemote
	for _, r := range resolved {
		switch {
		case only != "" && r.Remote.Name != only:
			continue
		case r.Shadowed:
			// Named explicitly, this is worth saying: the row exists and the
			// file is what is being used instead.
			if only != "" {
				return fmt.Errorf("the backup remote %q is described in zoomies.yaml as well, and the file wins; nothing was sent to the stored one", only)
			}
			continue
		case r.Problem != "":
			if only != "" {
				return fmt.Errorf("the backup remote %q cannot be used: %s", only, r.Problem)
			}
			fmt.Fprintf(e.out, "\n%s  NOT sent: %s\n", r.Remote.Name, r.Problem)
			continue
		case !r.Remote.Enabled():
			// A destination that is switched off stays switched off when it is
			// named: disabled is an answer about the destination, not about
			// which copies go to it.
			if only != "" {
				return fmt.Errorf("the backup remote %q is disabled or incomplete, so nothing was sent to it", only)
			}
			continue
		}
		remotes = append(remotes, r.Remote)
	}
	if only != "" && len(remotes) == 0 {
		return fmt.Errorf("no backup remote is called %q; this fleet has %s", only, remoteNames(resolved))
	}
	if len(remotes) == 0 {
		return nil
	}
	fmt.Fprintln(e.out)
	failed := false
	for _, cfgRemote := range remotes {
		remote, err := backup.NewRemote(cfgRemote, nil)
		if err != nil {
			failed = true
			fmt.Fprintf(e.out, "%s  NOT sent: %v\n", cfgRemote.Name, err)
			continue
		}
		copied, err := remote.Upload(ctx, entry)
		if err != nil {
			failed = true
			fmt.Fprintf(e.out, "%s  NOT sent: %v\n", remote.Name(), err)
			continue
		}
		sealed := "unencrypted"
		if copied.Encrypted {
			sealed = "encrypted"
		}
		fmt.Fprintf(e.out, "%s  sent to %s as %s (%s, %s)\n",
			remote.Name(), remote.Where(), copied.Key, humanBytes(int(copied.Bytes)), sealed)
		if remote.Keep() > 0 {
			removed, err := remote.Prune(ctx, remote.Keep())
			if err != nil {
				fmt.Fprintf(e.out, "%s  old copies were not removed: %v\n", remote.Name(), err)
			} else if len(removed) > 0 {
				fmt.Fprintf(e.out, "%s  removed %s, keeping the newest %d\n",
					remote.Name(), countOf(len(removed), "older copy"), remote.Keep())
			}
		}
	}
	if failed {
		fmt.Fprintf(e.out, "\nThe backup itself is on this host and is sound. What failed is getting a copy\n"+
			"off it, which is the half that survives the disk -- read the reason above.\n")
	}
	return nil
}

// remotesFor is every destination this host can see: the file's, and the ones
// stored in the database when it can be opened. The command is run on a host
// whose controller may not be running, so a database that will not open is not
// fatal -- the file's destinations are the ones that work in that case anyway.
func remotesFor(ctx context.Context, cfg *config.Config, st *store.Store) ([]backup.ResolvedRemote, error) {
	key, err := backup.ConfiguredKey(cfg)
	if err != nil {
		// Without the key the stored secrets cannot be opened, and
		// ResolveRemotes says so per destination rather than here.
		key = nil
	}
	var rows backup.RemoteLister
	if st != nil {
		rows = st
	}
	return backup.ResolveRemotes(ctx, cfg, rows, key)
}

// remoteNames lists what this fleet has, for the error that says a name is not
// one of them.
func remoteNames(resolved []backup.ResolvedRemote) string {
	var names []string
	for _, r := range resolved {
		names = append(names, r.Remote.Name)
	}
	if len(names) == 0 {
		return "no backup remotes"
	}
	return strings.Join(names, ", ")
}

// printBackupSummary says what is in the backup and, when the key is not, what
// is missing from it.
func printBackupSummary(w io.Writer, m *backupManifest, dest string) {
	rows := [][2]string{
		{"Database", fmt.Sprintf("%s (%s), integrity %s", filepath.Join(dest, backupDBName), humanBytes(int(m.Database.Bytes)), m.Database.Integrity)},
		{"Schema", lastMigration(m.Database.Migrations)},
		{"Taken by", strings.TrimSpace(m.Zoomies.Version + " " + m.Zoomies.OS + "/" + m.Zoomies.Arch)},
	}
	if m.Key.Fingerprint != "" {
		included := "not included"
		if m.Key.Included {
			included = "included in this backup"
		}
		rows = append(rows, [2]string{"Encryption key", m.Key.Fingerprint + ", " + included})
	}
	if n := len(m.Secrets); n > 0 {
		rows = append(rows, [2]string{"Needs the key for", countOf(n, "installation")})
	}
	width := 0
	for _, r := range rows {
		width = max(width, len(r[0]))
	}
	for _, r := range rows {
		fmt.Fprintf(w, "%s%s  %s\n", r[0], strings.Repeat(" ", width-len(r[0])), r[1])
	}

	// The one sentence that decides whether this backup is usable, said every
	// time rather than only when something is wrong: an operator who reads
	// "backed up" and stops has a database nobody can decrypt.
	if m.Key.Fingerprint != "" && !m.Key.Included {
		fmt.Fprintf(w, "\nThe encryption key is NOT in this backup. Without it the GitHub App credentials above\n"+
			"cannot be decrypted by anything, ever. Keep %s wherever you keep your secrets,\n"+
			"and check its fingerprint against %s when you restore.\n",
			keySourceOrPhrase(m), m.Key.Fingerprint)
	}
	if m.Key.Included {
		fmt.Fprintf(w, "\nThe encryption key is IN this backup, so the backup decrypts itself. Store it as you\n"+
			"would store the GitHub App's private key, because that is what it now contains.\n")
	}
}

func keySourceOrPhrase(m *backupManifest) string {
	if m.Key.SourcePath != "" {
		return m.Key.SourcePath
	}
	return "the key"
}

func lastMigration(names []string) string {
	if len(names) == 0 {
		return "none recorded"
	}
	return fmt.Sprintf("%s (%s applied)", names[len(names)-1], countOf(len(names), "migration"))
}

// countOf renders "1 backup" and "3 backups". The CLI has no plural helper of
// its own, and the controller's is not exported.
func countOf(n int, noun string) string {
	if n == 1 {
		return "1 " + noun
	}
	return fmt.Sprintf("%d %ss", n, noun)
}

// describeStaged says what a staged restore is waiting for, for the startup
// output: the operator who asked for it is watching the page, and the one who
// is reading the log should be able to tell why the database changed hands.
func describeStaged(s *backup.Staged) string {
	when := ""
	if !s.TakenAt.IsZero() {
		when = ", taken " + s.TakenAt.Format(time.RFC3339)
	}
	by := ""
	if s.RequestedBy != "" {
		by = " by " + s.RequestedBy
	}
	return fmt.Sprintf("%s%s, requested%s at %s", s.BackupID, when, by, s.RequestedAt.Format(time.RFC3339))
}
