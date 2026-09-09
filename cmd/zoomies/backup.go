package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"time"

	"github.com/eyupio/zoomies/internal/config"
	"github.com/eyupio/zoomies/internal/cryptox"
	"github.com/eyupio/zoomies/internal/store"
	"github.com/eyupio/zoomies/internal/version"
)

// backupManifestVersion is the manifest's own shape number. It moves when a
// field is removed or renamed, not when one is added.
const backupManifestVersion = 1

// The names inside one backup directory. They are fixed rather than derived
// from the instance, so `zoomies restore` takes a directory and nothing else,
// and so an operator looking at an unlabelled folder can tell what it is.
const (
	// The two the store owns, because it writes this layout itself before a
	// migration: one naming for one thing, so a copy taken automatically is
	// restorable by the command that restores a copy taken by hand.
	backupDBName    = store.BackupDBName
	backupDirPrefix = store.BackupDirPrefix

	backupManifestName = "manifest.json"
	backupKeyName      = "encryption.key"
)

// backupManifest is what turns a copy of a database into a backup somebody can
// act on months later.
//
// Everything in it answers a question asked during a restore and nowhere else:
// which build wrote this, how far the schema had got, whether the key file in
// my hand is the right one, what the key is needed for at all, and what this
// instance was configured to be. None of it can be recovered from the database
// file itself once the instance that produced it is gone.
type backupManifest struct {
	ManifestVersion int             `json:"manifest_version"`
	TakenAt         time.Time       `json:"taken_at"`
	Zoomies         backupBuild     `json:"zoomies"`
	Database        backupDatabase  `json:"database"`
	Key             backupKey       `json:"key"`
	Secrets         []backupSecret  `json:"secrets"`
	ConfigPath      string          `json:"config_path,omitempty"`
	Config          json.RawMessage `json:"config,omitempty"`
}

// backupBuild is the build that took the copy. A restore has to run this
// release or a later one: the store refuses a database whose ledger names
// migrations the binary does not have.
type backupBuild struct {
	Version   string `json:"version"`
	Commit    string `json:"commit,omitempty"`
	BuildDate string `json:"build_date,omitempty"`
	Go        string `json:"go"`
	OS        string `json:"os"`
	Arch      string `json:"arch"`
}

type backupDatabase struct {
	SourcePath string   `json:"source_path"`
	Bytes      int64    `json:"bytes"`
	SHA256     string   `json:"sha256"`
	Integrity  string   `json:"integrity"`
	Migrations []string `json:"migrations"`
}

// backupKey says which key opens this database, and whether it is in the
// backup.
//
// The fingerprint is always here and the key itself is not, unless it was
// asked for. That asymmetry is the whole design: a backup that always carried
// the key would put the database and the thing that decrypts it in one file
// nobody thinks of as a credential, and a backup that said nothing about the
// key would leave an operator holding two files with no way to tell whether
// they belong together.
type backupKey struct {
	Fingerprint string `json:"fingerprint"`
	SourcePath  string `json:"source_path,omitempty"`
	Included    bool   `json:"included"`
	// From says where this instance's key came from, because a key passed in
	// the environment has no file to copy and an operator restoring needs to
	// know that is what they are looking for.
	From string `json:"from"`
}

// backupSecret is one thing in this database that only the key opens. It is a
// list of what will be unreadable without it, never of the secrets themselves.
type backupSecret struct {
	Kind          string `json:"kind"`
	ID            string `json:"id"`
	Target        string `json:"target,omitempty"`
	AppID         int64  `json:"app_id,omitempty"`
	PrivateKey    bool   `json:"private_key"`
	WebhookSecret bool   `json:"webhook_secret"`
}

// runBackup is `zoomies backup`.
//
// It opens the database file rather than talking to a controller, and that is
// deliberate: a backup is taken on the machine that holds the data, by whoever
// can read it, and it has to work when the controller will not start. There is
// no API route for it and no token to hold.
func runBackup(ctx context.Context, e *env, args []string) error {
	fs := newFlagSet(e, "zoomies backup [--dir /var/backups/zoomies] [--keep 7] [--include-key]",
		"Take a consistent copy of the database, with a manifest saying what it is and what it needs.")
	cfgPath := fs.String("config", "", "path to zoomies.yaml (default: "+config.DefaultConfigFile()+")")
	dir := fs.String("dir", "", "where to put the backup (default: a `backups` directory beside the database)")
	keep := fs.Int("keep", 0, "delete all but the newest N backups in --dir; 0 keeps every one")
	includeKey := fs.Bool("include-key", false,
		"copy the encryption key into the backup as well. Convenient and dangerous: the copy then decrypts itself")
	fs.example("zoomies backup",
		"zoomies backup --dir /var/backups/zoomies --keep 7",
		"zoomies backup --include-key")
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
		root = filepath.Join(filepath.Dir(cfg.Database.Path), "backups")
	}

	st, err := store.Open(ctx, store.Options{Path: cfg.Database.Path, ReadOnly: true})
	if err != nil {
		return fmt.Errorf("opening %s: %w", cfg.Database.Path, err)
	}
	defer func() { _ = st.Close() }()

	dest := filepath.Join(root, backupDirPrefix+time.Now().UTC().Format("20060102-150405"))
	if err := os.MkdirAll(dest, 0o700); err != nil {
		return fmt.Errorf("creating %s: %w", dest, err)
	}
	dbPath := filepath.Join(dest, backupDBName)
	if err := st.Backup(ctx, dbPath); err != nil {
		return err
	}

	manifest, err := buildBackupManifest(ctx, cfg, st, dbPath, dest, *includeKey)
	if err != nil {
		return err
	}
	raw, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(dest, backupManifestName), append(raw, '\n'), 0o600); err != nil {
		return fmt.Errorf("writing the manifest: %w", err)
	}

	fmt.Fprintf(e.out, "Backed up to %s\n\n", dest)
	printBackupSummary(e.out, manifest, dest)

	if *keep > 0 {
		removed, err := pruneBackups(root, *keep)
		if err != nil {
			return err
		}
		if len(removed) > 0 {
			fmt.Fprintf(e.out, "\nRemoved %s, keeping the newest %d.\n", countOf(len(removed), "older backup"), *keep)
		}
	}
	return nil
}

// buildBackupManifest gathers everything the restore will want to know.
func buildBackupManifest(ctx context.Context, cfg *config.Config, st *store.Store, dbPath, dest string, includeKey bool) (*backupManifest, error) {
	m := &backupManifest{
		ManifestVersion: backupManifestVersion,
		TakenAt:         time.Now().UTC(),
		Zoomies: backupBuild{
			Version: version.Version, Commit: version.Commit, BuildDate: version.Date,
			Go: runtime.Version(), OS: runtime.GOOS, Arch: runtime.GOARCH,
		},
		Database:   backupDatabase{SourcePath: cfg.Database.Path},
		ConfigPath: cfg.Path(),
		Secrets:    []backupSecret{},
	}

	// The copy is checked here rather than trusted, because the moment to find
	// out that a backup is unreadable is while the original is still there.
	copied, err := store.Open(ctx, store.Options{Path: dbPath, ReadOnly: true})
	if err != nil {
		return nil, fmt.Errorf("opening the copy at %s: %w", dbPath, err)
	}
	defer func() { _ = copied.Close() }()
	if err := copied.IntegrityCheck(ctx); err != nil {
		return nil, err
	}
	m.Database.Integrity = "ok"

	applied, err := copied.AppliedMigrations(ctx)
	if err != nil {
		return nil, err
	}
	for _, a := range applied {
		m.Database.Migrations = append(m.Database.Migrations, a.Name)
	}

	info, err := os.Stat(dbPath)
	if err != nil {
		return nil, err
	}
	m.Database.Bytes = info.Size()
	// The digest is for the operator moving this file somewhere else, so a
	// copy that arrived truncated is caught before it is restored rather than
	// after.
	if m.Database.SHA256, err = fileSHA256(dbPath); err != nil {
		return nil, err
	}

	insts, err := copied.ListInstallations(ctx)
	if err != nil {
		return nil, err
	}
	for _, i := range insts {
		if len(i.PrivateKeyEnc) == 0 && len(i.WebhookSecretEnc) == 0 {
			continue
		}
		m.Secrets = append(m.Secrets, backupSecret{
			Kind: "installation", ID: i.ID, Target: i.Target, AppID: i.AppID,
			PrivateKey: len(i.PrivateKeyEnc) > 0, WebhookSecret: len(i.WebhookSecretEnc) > 0,
		})
	}

	if err := describeBackupKey(cfg, dest, includeKey, m); err != nil {
		return nil, err
	}

	// The configuration, through the same blanking `zoomies config print` uses,
	// so a restore has the record of what this instance was without the backup
	// becoming a place secrets live.
	blob, err := json.Marshal(blankSecrets(cfg))
	if err != nil {
		return nil, err
	}
	m.Config = blob
	return m, nil
}

// describeBackupKey records which key opens this database and, if asked,
// copies it in.
//
// A backup with no secrets in it needs no key, and saying so is better than an
// empty fingerprint that reads like a failure.
func describeBackupKey(cfg *config.Config, dest string, includeKey bool, m *backupManifest) error {
	if len(m.Secrets) == 0 && strings.TrimSpace(cfg.Security.EncryptionKey) == "" &&
		strings.TrimSpace(cfg.Security.EncryptionKeyFile) == "" {
		m.Key.From = "none: nothing in this database is sealed"
		return nil
	}

	var (
		key  *cryptox.Key
		err  error
		path string
	)
	switch {
	case strings.TrimSpace(cfg.Security.EncryptionKey) != "":
		key, err = cryptox.ParseKey(cfg.Security.EncryptionKey)
		m.Key.From = "the environment or zoomies.yaml"
	default:
		path = strings.TrimSpace(cfg.Security.EncryptionKeyFile)
		key, err = cryptox.LoadKeyFile(path)
		m.Key.From = "security.encryption_key_file"
		m.Key.SourcePath = path
	}
	if err != nil {
		// A backup of a database whose key cannot be read is still worth
		// taking, and is exactly the moment to say the key is missing.
		return fmt.Errorf("reading the encryption key so the backup can record which one this is: %w "+
			"(the database has been copied; put the key where security.encryption_key_file points and take the backup again)", err)
	}
	m.Key.Fingerprint = key.Fingerprint()

	if !includeKey {
		return nil
	}
	if path == "" {
		return errors.New("--include-key: this instance's key comes from the environment rather than a file, " +
			"so there is nothing to copy; store it wherever you keep the rest of your secrets")
	}
	if err := cryptox.WriteKeyFile(filepath.Join(dest, backupKeyName), key); err != nil {
		return fmt.Errorf("copying the encryption key into the backup: %w", err)
	}
	m.Key.Included = true
	return nil
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

// pruneBackups deletes all but the newest keep backups under root.
//
// It only ever removes a directory this command made: the name has to match
// and a manifest has to be inside it. Retention that guessed would eventually
// delete the wrong thing, and the directory an operator points --dir at is
// often shared with somebody else's copies.
func pruneBackups(root string, keep int) ([]string, error) {
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", root, err)
	}
	var ours []string
	for _, e := range entries {
		if !e.IsDir() || !strings.HasPrefix(e.Name(), backupDirPrefix) {
			continue
		}
		if _, err := os.Stat(filepath.Join(root, e.Name(), backupManifestName)); err != nil {
			continue
		}
		ours = append(ours, e.Name())
	}
	// The names carry the timestamp, so sorting them sorts by age without
	// trusting a modification time that a copy between machines rewrites.
	slices.Sort(ours)
	if len(ours) <= keep {
		return nil, nil
	}
	var removed []string
	for _, name := range ours[:len(ours)-keep] {
		if err := os.RemoveAll(filepath.Join(root, name)); err != nil {
			return removed, fmt.Errorf("removing the old backup %s: %w", name, err)
		}
		removed = append(removed, name)
	}
	return removed, nil
}

func fileSHA256(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// countOf renders "1 backup" and "3 backups". The CLI has no plural helper of
// its own, and the controller's is not exported.
func countOf(n int, noun string) string {
	if n == 1 {
		return "1 " + noun
	}
	return fmt.Sprintf("%d %ss", n, noun)
}
