package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/eyupio/zoomies/internal/config"
	"github.com/eyupio/zoomies/internal/cryptox"
	"github.com/eyupio/zoomies/internal/store"
	"github.com/eyupio/zoomies/internal/version"
)

// runRestore is `zoomies restore <backup-directory>`.
//
// Every refusal in it is one an operator would otherwise discover after the
// controller was running on the restored data, which is the worst moment: the
// fleet is live, the original may already be gone, and the symptom -- a
// decryption error, a schema nobody understands -- says nothing about the
// restore that caused it. So the checks run before anything is moved.
//
// It restores the database and nothing else. The encryption key, the
// configuration and the service unit are the operator's to put back, because
// each of them is a decision about this host rather than a copy of the data.
func runRestore(ctx context.Context, e *env, args []string) error {
	fs := newFlagSet(e, "zoomies restore <backup-directory> [--replace]",
		"Put a backup's database in place, invalidating the credentials it froze and fencing the fleet.")
	cfgPath := fs.String("config", "", "path to zoomies.yaml (default: "+config.DefaultConfigFile()+")")
	replace := fs.Bool("replace", false, "overwrite the database already at database.path, keeping a copy of it first")
	revokeTokens := fs.Bool("revoke-api-tokens", false, "revoke every API token as well; they are valid credentials the backup froze")
	resetAgents := fs.Bool("reset-agent-tokens", false, "forget every host's agent credential, so each agent joins again")
	fs.example("zoomies restore /var/backups/zoomies/zoomies-20260908-181718",
		"zoomies restore /var/backups/zoomies/zoomies-20260908-181718 --replace",
		"zoomies restore ... --revoke-api-tokens --reset-agent-tokens")
	if err := fs.parse(args); err != nil {
		return err
	}
	src, err := fs.oneArg("the backup directory to restore")
	if err != nil {
		return err
	}

	cfg, err := config.Load(*cfgPath)
	if err != nil {
		return err
	}

	backupDB := filepath.Join(src, backupDBName)
	if _, err := os.Stat(backupDB); err != nil {
		return fmt.Errorf("%s does not look like a Zoomies backup: %w", src, err)
	}
	manifest, manifestErr := loadBackupManifest(src)

	// Checked before anything is moved, in the order an operator would want to
	// be stopped: is this file sound, can this binary read it, and is the key
	// on this host the one that opens it?
	if err := checkRestoreCopy(ctx, backupDB); err != nil {
		return err
	}
	if manifest != nil {
		if err := checkRestoreKey(cfg, manifest, e.out); err != nil {
			return err
		}
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

	// Restoring a backup over itself would move the source aside and then copy
	// from a path that is no longer there, so it is refused while both still
	// exist. It is an easy mistake to make: the argument is a directory, and
	// pointing it at the state directory reads as "restore what is here".
	if same, err := sameFile(backupDB, live); err != nil {
		return err
	} else if same {
		return fmt.Errorf("%s is the database being restored to, not a backup of it. "+
			"Point restore at a directory `zoomies backup` wrote", backupDB)
	}

	switch _, err := os.Stat(live); {
	case err == nil:
		if !*replace {
			return fmt.Errorf("%s already exists, and restoring over it would lose whatever is in it. "+
				"Pass --replace to overwrite it; a copy of it is taken first, beside it", live)
		}
		kept, err := setAsideLiveDatabase(live)
		if err != nil {
			return err
		}
		fmt.Fprintf(e.out, "Moved the database that was there to %s\n", kept)

	case errors.Is(err, os.ErrNotExist):
		// The database is gone; its write-ahead log may not be. That pair is
		// exactly what a corrupt-database recovery leaves behind, and it is
		// the state this command exists to be run in.
		//
		// setAsideLiveDatabase already knows the rule -- SQLite replays a log
		// it finds beside a database, whatever database wrote it -- but it
		// only ran when the .db was still there, so this path fell straight
		// through to the copy. The restored database was then opened on top of
		// somebody else's log and came back "database disk image is
		// malformed", and the replay consumed and unlinked the log doing it:
		// the committed transactions that only existed in that log were gone,
		// destroyed by a command that then reported failure.
		//
		// So the log is moved aside rather than removed. It is the last copy
		// of whatever those transactions were, and an operator who came here
		// to recover data should not lose more of it to the recovery.
		if !hasOrphanedLogs(live) {
			break
		}
		if !*replace {
			return fmt.Errorf("%s is gone but its write-ahead log is still there, and SQLite would replay "+
				"that log into whatever is restored, corrupting it and destroying the log in the process. "+
				"Pass --replace to move the log aside first; it is kept, not deleted, beside the database", live)
		}
		kept, err := setAsideOrphanedLogs(live)
		if err != nil {
			return err
		}
		fmt.Fprintf(e.out, "Moved a write-ahead log that had outlived its database to %s\n", strings.Join(kept, ", "))

	default:
		return fmt.Errorf("checking %s: %w", live, err)
	}

	if err := copyFile(backupDB, live); err != nil {
		return err
	}

	invalidated, err := invalidateAfterRestore(ctx, live, src, manifest, *revokeTokens, *resetAgents)
	if err != nil {
		return err
	}

	fmt.Fprintf(e.out, "Restored %s to %s\n\n", src, live)
	if manifestErr != nil {
		fmt.Fprintf(e.out, "There was no readable manifest in the backup (%v), so the key fingerprint\n"+
			"and the build that wrote it could not be checked. The database itself passed its integrity check.\n\n", manifestErr)
	}
	for _, line := range invalidated {
		fmt.Fprintln(e.out, line)
	}
	fmt.Fprintf(e.out, "\nMarked the restored database for recovery (%s), with the reason above.\n"+
		"Before you start the controller, check the three things a restore does not bring with it:\n"+
		"the external URL this fleet answers on, the agents, and the runners that were live when the\n"+
		"backup was taken. docs/backup-and-restore.md walks through each.\n", store.SettingRecoveryFenced)
	return nil
}

// loadBackupManifest reads the manifest beside a backup's database.
//
// A missing one is not fatal. The database is the thing being restored, and a
// backup that arrived without its manifest -- copied file by file, or written
// by something older -- is still worth putting back; what is lost is the
// checking, and the summary says so rather than pretending.
func loadBackupManifest(dir string) (*backupManifest, error) {
	raw, err := os.ReadFile(filepath.Join(dir, backupManifestName))
	if err != nil {
		return nil, err
	}
	var m backupManifest
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil, err
	}
	return &m, nil
}

// checkRestoreCopy opens the backup's database and asks it two questions: is it
// sound, and can this binary read it?
//
// The second is the one that catches a rollback. The store refuses a ledger
// naming migrations this build does not have, so restoring a backup from a
// newer release onto an older binary stops here with the names, rather than
// after the controller has been started on it.
func checkRestoreCopy(ctx context.Context, path string) error {
	st, err := store.Open(ctx, store.Options{Path: path, ReadOnly: true})
	if err != nil {
		if errors.Is(err, store.ErrSchemaNewer) {
			return fmt.Errorf("this backup was written by a newer release than %s: %w", version.Short(), err)
		}
		return fmt.Errorf("opening the backup's database: %w", err)
	}
	defer func() { _ = st.Close() }()
	return st.IntegrityCheck(ctx)
}

// checkRestoreKey compares the key this host is configured with against the one
// that sealed the backup.
//
// A mismatch is refused rather than warned about. The alternative is a
// controller that starts, reports itself healthy, and fails inside its first
// GitHub call -- and by then the original database may be gone.
func checkRestoreKey(cfg *config.Config, m *backupManifest, out io.Writer) error {
	if m.Key.Fingerprint == "" {
		return nil
	}
	key, err := loadConfiguredKey(cfg)
	if err != nil {
		return fmt.Errorf("this backup needs the encryption key whose fingerprint is %s, and this host has none: %w "+
			"(put it where security.encryption_key_file points, or pass it in ZOOMIES_ENCRYPTION_KEY)", m.Key.Fingerprint, err)
	}
	if got := key.Fingerprint(); got != m.Key.Fingerprint {
		return fmt.Errorf("the encryption key on this host (%s) is not the one that sealed this backup (%s). "+
			"Restoring anyway would produce a fleet that starts and cannot authenticate to GitHub; "+
			"put the right key in place first", got, m.Key.Fingerprint)
	}
	fmt.Fprintf(out, "The encryption key on this host matches the backup (%s).\n", m.Key.Fingerprint)
	return nil
}

// loadConfiguredKey resolves the key this host is configured with, without
// generating one: a restore is never a first run.
func loadConfiguredKey(cfg *config.Config) (*cryptox.Key, error) {
	if raw := strings.TrimSpace(cfg.Security.EncryptionKey); raw != "" {
		return cryptox.ParseKey(raw)
	}
	if path := strings.TrimSpace(cfg.Security.EncryptionKeyFile); path != "" {
		return cryptox.LoadKeyFile(path)
	}
	return nil, cryptox.ErrNoKey
}

// setAsideLiveDatabase moves the database that is already there out of the way
// and returns where it went.
//
// It is a raw file move of all three files rather than a store backup, and it
// has to be: the live database may be the newer one -- an operator restoring an
// old copy after a bad upgrade -- and the store refuses to open a database
// whose ledger it does not know. A pre-restore copy that only worked when it
// was not needed would be worse than none.
// hasOrphanedLogs reports whether a write-ahead log or shared-memory file is
// sitting beside a database that is not there. It is the state a crashed
// controller leaves, and the state somebody reaches for restore in.
func hasOrphanedLogs(live string) bool {
	for _, ext := range []string{"-wal", "-shm"} {
		if _, err := os.Stat(live + ext); err == nil {
			return true
		}
	}
	return false
}

// setAsideOrphanedLogs moves that pair out of the way, under the same name the
// database itself would have been kept under. Renamed and never removed: the
// log may hold committed transactions that exist nowhere else, and this
// command is reached by people who are already missing data.
//
// It returns the paths it wrote, so the report can name files that are really
// there rather than the stem they share.
func setAsideOrphanedLogs(live string) ([]string, error) {
	kept := live + ".before-restore-" + time.Now().UTC().Format("20060102-150405")
	var moved []string
	for _, ext := range []string{"-wal", "-shm"} {
		if err := os.Rename(live+ext, kept+ext); err != nil {
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			return nil, fmt.Errorf("moving %s aside: %w", live+ext, err)
		}
		moved = append(moved, kept+ext)
	}
	return moved, nil
}

// sameFile reports whether two paths are the same file on disk. A path that is
// not there is not the same file as anything, and is not an error here: the
// caller has already established the backup exists, and a missing destination
// is the ordinary case.
func sameFile(a, b string) (bool, error) {
	fa, err := os.Stat(a)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return false, nil
		}
		return false, fmt.Errorf("checking %s: %w", a, err)
	}
	fb, err := os.Stat(b)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return false, nil
		}
		return false, fmt.Errorf("checking %s: %w", b, err)
	}
	return os.SameFile(fa, fb), nil
}

func setAsideLiveDatabase(live string) (string, error) {
	kept := live + ".before-restore-" + time.Now().UTC().Format("20060102-150405")
	if err := os.Rename(live, kept); err != nil {
		return "", fmt.Errorf("moving %s aside: %w", live, err)
	}
	// The write-ahead log and shared-memory file are part of that database.
	// Leaving them beside the restored copy would be worse than losing them:
	// SQLite would replay another database's log into this one.
	for _, ext := range []string{"-wal", "-shm"} {
		if err := os.Rename(live+ext, kept+ext); err != nil && !errors.Is(err, os.ErrNotExist) {
			return "", fmt.Errorf("moving %s aside: %w", live+ext, err)
		}
	}
	return kept, nil
}

// invalidateAfterRestore does the part that is not copying a file: it takes
// away the credentials the backup froze, and fences the fleet.
func invalidateAfterRestore(ctx context.Context, live, src string, m *backupManifest, revokeTokens, resetAgents bool) ([]string, error) {
	st, err := store.Open(ctx, store.Options{Path: live})
	if err != nil {
		return nil, fmt.Errorf("opening the restored database: %w", err)
	}
	defer func() { _ = st.Close() }()

	var lines []string
	sessions, err := st.DeleteAllSessions(ctx)
	if err != nil {
		return nil, err
	}
	lines = append(lines, fmt.Sprintf("Ended %s: a cookie from the day of the backup would otherwise still be signed in.", countOf(int(sessions), "session")))

	joins, err := st.DeleteUnusedJoinTokens(ctx)
	if err != nil {
		return nil, err
	}
	lines = append(lines, fmt.Sprintf("Removed %s that had never been redeemed; the redeemed ones are history and were kept.", countOf(int(joins), "join token")))

	// The two that are flags rather than defaults, because each has a cost the
	// operator is the one to weigh: revoking API tokens breaks whatever
	// automation holds them, and resetting agent tokens means walking every
	// host. Both are right when the backup may have been seen by somebody
	// else, and neither is right by reflex.
	if revokeTokens {
		n, err := st.RevokeAllAPITokens(ctx)
		if err != nil {
			return nil, err
		}
		lines = append(lines, fmt.Sprintf("Revoked %s. Anything automated that held one needs a new one.", countOf(int(n), "API token")))
	} else {
		lines = append(lines, "Left the API tokens working. If this backup may have been read by anyone else, re-run with --revoke-api-tokens.")
	}
	if resetAgents {
		n, err := st.ResetAgentTokens(ctx)
		if err != nil {
			return nil, err
		}
		lines = append(lines, fmt.Sprintf("Forgot the agent credential on %s; each will exit with the command to join again.", countOf(int(n), "host")))
	}

	reason := "restored from " + src
	if m != nil && !m.TakenAt.IsZero() {
		reason = fmt.Sprintf("restored from %s, a backup taken %s", src, m.TakenAt.Format(time.RFC3339))
	}
	if err := st.SetRecoveryFence(ctx, true, reason); err != nil {
		return nil, err
	}
	// The audit row is written to the restored database, which is where anyone
	// asking "why is this fleet fenced?" will look.
	if err := st.AppendAudit(ctx, &store.AuditEvent{
		ActorKind: "system", ActorName: "zoomies restore",
		Action: "instance.restore", TargetKind: "instance",
		After: reason,
	}); err != nil {
		return nil, err
	}
	return lines, nil
}

// copyFile writes src to dest at owner-only permissions, refusing to leave a
// half-written database behind.
func copyFile(src, dest string) error {
	if err := os.MkdirAll(filepath.Dir(dest), 0o750); err != nil {
		return fmt.Errorf("creating %s: %w", filepath.Dir(dest), err)
	}
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dest, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return fmt.Errorf("writing %s: %w", dest, err)
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		_ = os.Remove(dest)
		return fmt.Errorf("writing %s: %w", dest, err)
	}
	if err := out.Close(); err != nil {
		_ = os.Remove(dest)
		return fmt.Errorf("writing %s: %w", dest, err)
	}
	return nil
}
