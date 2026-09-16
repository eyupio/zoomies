package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/eyupio/zoomies/internal/backup"
	"github.com/eyupio/zoomies/internal/config"
	"github.com/eyupio/zoomies/internal/store"
)

// listRemoteCopies prints what one remote holds. It is what `zoomies restore
// --from-remote offsite` with no id does, because the alternative on a machine
// that has lost everything is an operator guessing at timestamps.
func listRemoteCopies(ctx context.Context, e *env, cfg *config.Config, name string) error {
	remote, err := backup.FindRemote(cfg, name, nil)
	if err != nil {
		return err
	}
	copies, err := remote.List(ctx)
	if err != nil {
		return err
	}
	if len(copies) == 0 {
		fmt.Fprintf(e.out, "%s holds no backups of this fleet.\n", remote.Where())
		return nil
	}
	fmt.Fprintf(e.out, "%s holds %s:\n\n", remote.Where(), countOf(len(copies), "backup"))
	for _, c := range copies {
		sealed := ""
		if c.Encrypted {
			sealed = ", encrypted"
		}
		fmt.Fprintf(e.out, "  %s  %s  taken %s%s\n",
			c.ID, humanBytes(int(c.Bytes)), c.TakenAt.Format(time.RFC3339), sealed)
	}
	fmt.Fprintf(e.out, "\nRestore one with:\n  zoomies restore --from-remote %s %s --replace\n",
		name, copies[0].ID)
	return nil
}

// fetchFromRemote brings one copy back into the backup directory, where it is
// an ordinary backup that the rest of this command restores exactly as it
// restores one that was already there.
func fetchFromRemote(ctx context.Context, e *env, cfg *config.Config, name, id, passphrase string) (*backup.Entry, error) {
	remote, err := backup.FindRemote(cfg, name, nil)
	if err != nil {
		return nil, err
	}
	if id == "latest" {
		copies, err := remote.List(ctx)
		if err != nil {
			return nil, err
		}
		if len(copies) == 0 {
			return nil, fmt.Errorf("%s holds no backups of this fleet", remote.Where())
		}
		id = copies[0].ID
	}
	fmt.Fprintf(e.out, "Fetching %s from %s...\n", id, remote.Where())
	entry, err := remote.Fetch(ctx, backup.Dir(cfg), id, backup.FetchOptions{
		Passphrase: passphrase, TakenBy: "zoomies restore --from-remote " + name,
	})
	if err != nil {
		return nil, err
	}
	fmt.Fprintf(e.out, "Fetched to %s (%s), verified.\n\n", entry.Dir, humanBytes(int(entry.Bytes)))
	return entry, nil
}

// runRestore is `zoomies restore <backup-directory>`.
//
// Every check and every refusal lives in internal/backup, shared with the
// restore the settings page stages: the command adds the lock, the flags and
// the words. It restores the database and nothing else. The encryption key,
// the configuration and the service unit are the operator's to put back,
// because each of them is a decision about this host rather than a copy of the
// data.
func runRestore(ctx context.Context, e *env, args []string) error {
	fs := newFlagSet(e, "zoomies restore <backup-directory> [--replace]",
		"Put a backup's database in place, invalidating the credentials it froze and fencing the fleet.")
	cfgPath := fs.String("config", "", "path to zoomies.yaml (default: "+config.DefaultConfigFile()+")")
	replace := fs.Bool("replace", false, "overwrite the database already at database.path, keeping a copy of it first")
	revokeTokens := fs.Bool("revoke-api-tokens", false, "revoke every API token as well; they are valid credentials the backup froze")
	resetAgents := fs.Bool("reset-agent-tokens", false, "forget every host's agent credential, so each agent joins again")
	from := fs.String("from-remote", "", "bring the copy back from this backup remote first; the argument is then a backup id, or `latest`, and naming no backup lists what the remote holds")
	passphrase := fs.String("passphrase", "", "the passphrase that copy was sealed with, when it is not the one in backup.remotes")
	fs.example("zoomies restore /var/backups/zoomies/zoomies-20260908-181718",
		"zoomies restore /var/backups/zoomies/zoomies-20260908-181718 --replace",
		"zoomies restore --from-remote offsite",
		"zoomies restore --from-remote offsite latest --replace",
		"zoomies restore ... --revoke-api-tokens --reset-agent-tokens")
	if err := fs.parse(args); err != nil {
		return err
	}

	cfg, err := config.Load(*cfgPath)
	if err != nil {
		return err
	}

	var src string
	if *from != "" {
		// The disaster path: a host with the configuration file, the key, and
		// nothing else. Naming no backup lists what is there rather than
		// failing, because an operator standing in front of an empty machine
		// has no way to know the ids.
		if fs.NArg() == 0 {
			return listRemoteCopies(ctx, e, cfg, *from)
		}
		id, err := fs.oneArg("the backup id to bring back, or `latest`")
		if err != nil {
			return err
		}
		entry, err := fetchFromRemote(ctx, e, cfg, *from, id, *passphrase)
		if err != nil {
			return err
		}
		src = entry.Dir
	} else {
		if src, err = fs.oneArg("the backup directory to restore"); err != nil {
			return err
		}
	}
	if _, err := os.Stat(filepath.Join(src, backup.DBName)); err != nil {
		return fmt.Errorf("%s does not look like a Zoomies backup: %w", src, err)
	}

	live := cfg.Database.Path

	// Nothing moves under a running controller. This is the same lock a
	// controller takes before it opens the database, so holding it for the rest
	// of the restore both proves none is running now and stops one starting
	// halfway through the swap.
	//
	// The check earns its place because the damage it prevents is silent.
	// Renaming a file does not reach a process that already has it open: a live
	// controller goes on reading the database that was moved aside, every
	// connection it opens after the swap reads the restored one instead, and
	// the writes it makes in between land in the file nobody will look at
	// again. Nothing fails at the time, and what is lost is whatever the fleet
	// did during the restore.
	//
	// The directory is created first because the lock sits beside the database,
	// and restoring onto a host that has never run one is the ordinary case.
	if err := os.MkdirAll(filepath.Dir(live), 0o750); err != nil {
		return fmt.Errorf("creating %s: %w", filepath.Dir(live), err)
	}
	unlock, err := store.Lock(live)
	if err != nil {
		if errors.Is(err, store.ErrLocked) {
			return fmt.Errorf("a controller is running on %s, and restoring under it would lose "+
				"whatever the fleet does while the file is being replaced. Stop the controller "+
				"(systemctl stop zoomies), restore, then start it again", live)
		}
		return fmt.Errorf("checking whether a controller is running on %s: %w", live, err)
	}
	defer func() { _ = unlock() }()

	report, err := backup.Restore(ctx, cfg, src, backup.RestoreOptions{
		Replace: *replace, RevokeAPITokens: *revokeTokens, ResetAgentTokens: *resetAgents,
	})
	if err != nil {
		return err
	}
	printRestoreReport(e, report, live)
	return nil
}

// printRestoreReport is the restore's own words, in the order things happened.
func printRestoreReport(e *env, report *backup.RestoreReport, live string) {
	if report.KeyChecked {
		fmt.Fprintf(e.out, "The encryption key on this host matches the backup.\n")
	}
	if report.MovedAside != "" {
		fmt.Fprintf(e.out, "Moved the database that was there to %s\n", report.MovedAside)
	}
	if len(report.MovedLogs) > 0 {
		fmt.Fprintf(e.out, "Moved a write-ahead log that had outlived its database to %s\n", strings.Join(report.MovedLogs, ", "))
	}
	fmt.Fprintf(e.out, "Restored %s to %s\n\n", report.Source, live)
	if report.ManifestProblem != "" {
		fmt.Fprintf(e.out, "There was no readable manifest in the backup (%s), so the key fingerprint\n"+
			"and the build that wrote it could not be checked. The database itself passed its integrity check.\n\n", report.ManifestProblem)
	}
	for _, line := range report.Invalidated {
		// The command has flags the page does not, so the sentence that
		// offers the choice names them here.
		line = strings.Replace(line, "may have been read by anyone else, revoke them.", "may have been read by anyone else, re-run with --revoke-api-tokens.", 1)
		fmt.Fprintln(e.out, line)
	}
	fmt.Fprintf(e.out, "\nMarked the restored database for recovery (%s), with the reason above.\n"+
		"Before you start the controller, check the three things a restore does not bring with it:\n"+
		"the external URL this fleet answers on, the agents, and the runners that were live when the\n"+
		"backup was taken. docs/backup-and-restore.md walks through each.\n", store.SettingRecoveryFenced)
}
