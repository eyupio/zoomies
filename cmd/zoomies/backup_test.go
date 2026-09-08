package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/eyupio/zoomies/internal/cryptox"
	"github.com/eyupio/zoomies/internal/store"
)

// backupHost prepares a host with a database that has something sealed in it
// and a key file that opens it, which is the only configuration where the
// manifest has anything interesting to say.
func backupHost(t *testing.T) (dir string, key *cryptox.Key) {
	t.Helper()
	dir = isolateHost(t)
	ctx := context.Background()

	dbPath := filepath.Join(dir, "zoomies.db")
	t.Setenv("ZOOMIES_DB_PATH", dbPath)

	key, err := cryptox.GenerateKey()
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}
	keyPath := filepath.Join(dir, "encryption.key")
	if err := cryptox.WriteKeyFile(keyPath, key); err != nil {
		t.Fatalf("WriteKeyFile: %v", err)
	}
	t.Setenv("ZOOMIES_ENCRYPTION_KEY_FILE", keyPath)

	st, err := store.Open(ctx, store.Options{Path: dbPath})
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	sealed, err := key.SealString("-----BEGIN RSA PRIVATE KEY-----\nthe-app-key\n-----END RSA PRIVATE KEY-----")
	if err != nil {
		t.Fatalf("sealing: %v", err)
	}
	if err := st.CreateInstallation(ctx, &store.Installation{
		AppID: 4242, InstallationID: 99, Target: "acme", TargetType: store.TargetOrg,
		PrivateKeyEnc: sealed,
	}); err != nil {
		t.Fatalf("CreateInstallation: %v", err)
	}
	if err := st.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	return dir, key
}

func readManifest(t *testing.T, dir string) backupManifest {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(dir, backupManifestName))
	if err != nil {
		t.Fatalf("reading the manifest: %v", err)
	}
	var m backupManifest
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatalf("parsing the manifest: %v", err)
	}
	return m
}

// onlyBackup returns the single backup directory under root, failing when
// there is not exactly one.
func onlyBackup(t *testing.T, root string) string {
	t.Helper()
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatalf("reading %s: %v", root, err)
	}
	var dirs []string
	for _, e := range entries {
		if e.IsDir() {
			dirs = append(dirs, filepath.Join(root, e.Name()))
		}
	}
	if len(dirs) != 1 {
		t.Fatalf("want one backup under %s, found %v", root, dirs)
	}
	return dirs[0]
}

// The manifest exists because none of what it holds can be recovered from the
// database file once the instance that produced it is gone: which build wrote
// it, how far the schema had got, and whether the key file in the operator's
// hand is the one that opens it.
func TestABackupCarriesWhatARestoreWillNeedToKnow(t *testing.T) {
	_, key := backupHost(t)
	root := t.TempDir()

	e, out, errOut := newTestEnv(t)
	if code := dispatch(context.Background(), e, []string{"backup", "--dir", root}); code != exitOK {
		t.Fatalf("exit code = %d\n%s", code, errOut)
	}
	dir := onlyBackup(t, root)
	m := readManifest(t, dir)

	if m.ManifestVersion != backupManifestVersion || m.TakenAt.IsZero() {
		t.Errorf("the manifest does not say what it is or when: %+v", m)
	}
	// The build, because a restore has to run this release or a later one: the
	// store refuses a database whose ledger names migrations it does not have.
	if m.Zoomies.Version == "" || m.Zoomies.OS == "" {
		t.Errorf("the manifest does not name the build that took it: %+v", m.Zoomies)
	}
	if len(m.Database.Migrations) == 0 {
		t.Error("the manifest carries no migration ledger, so nothing says which release can open this")
	}
	if m.Database.Integrity != "ok" || m.Database.SHA256 == "" || m.Database.Bytes == 0 {
		t.Errorf("the copy was not checked or measured: %+v", m.Database)
	}
	// The fingerprint answers "is this the right key?" without the alternative,
	// which is finding out when the first GitHub call fails.
	if m.Key.Fingerprint != key.Fingerprint() {
		t.Errorf("key fingerprint = %q, want %q", m.Key.Fingerprint, key.Fingerprint())
	}
	// What the key is needed for, so an operator who has lost it knows exactly
	// what they have lost rather than guessing.
	if len(m.Secrets) != 1 || m.Secrets[0].Target != "acme" || !m.Secrets[0].PrivateKey {
		t.Errorf("the manifest does not say what the key opens: %+v", m.Secrets)
	}
	if len(m.Config) == 0 {
		t.Error("the manifest carries no configuration, which is the record of what this instance was")
	}

	// And the copy is a database, not a file: it opens on its own.
	copied, err := store.Open(context.Background(), store.Options{Path: filepath.Join(dir, backupDBName), ReadOnly: true})
	if err != nil {
		t.Fatalf("the copy does not open: %v", err)
	}
	defer copied.Close()
	insts, err := copied.ListInstallations(context.Background())
	if err != nil || len(insts) != 1 {
		t.Fatalf("the copy does not have the installation: %v, %d", err, len(insts))
	}
	if !strings.Contains(out.String(), dir) {
		t.Errorf("the terminal does not name the backup it wrote:\n%s", out)
	}
}

// The key is the difference between a backup and a folder of unreadable bytes,
// and between a backup and a credential. Both halves of that have to be true
// by default and said out loud, because an operator who reads "backed up" and
// stops has a database nobody can ever decrypt.
func TestTheKeyIsLeftOutUnlessItIsAskedForAndTheOperatorIsTold(t *testing.T) {
	t.Run("left out by default", func(t *testing.T) {
		dir, _ := backupHost(t)
		root := t.TempDir()
		e, out, errOut := newTestEnv(t)
		if code := dispatch(context.Background(), e, []string{"backup", "--dir", root}); code != exitOK {
			t.Fatalf("exit code = %d\n%s", code, errOut)
		}
		b := onlyBackup(t, root)
		if _, err := os.Stat(filepath.Join(b, backupKeyName)); !os.IsNotExist(err) {
			t.Error("the encryption key was copied into a backup nobody asked to include it in")
		}
		if m := readManifest(t, b); m.Key.Included {
			t.Error("the manifest claims the key is included when it is not")
		}
		// Naming the file is the point: the operator has to go and keep it.
		if !strings.Contains(out.String(), filepath.Join(dir, "encryption.key")) {
			t.Errorf("the summary does not name the key file to keep:\n%s", out)
		}
		if !strings.Contains(out.String(), "NOT in this backup") {
			t.Errorf("the summary does not say the key is missing:\n%s", out)
		}
	})

	t.Run("included when asked", func(t *testing.T) {
		_, key := backupHost(t)
		root := t.TempDir()
		e, out, errOut := newTestEnv(t)
		if code := dispatch(context.Background(), e, []string{"backup", "--dir", root, "--include-key"}); code != exitOK {
			t.Fatalf("exit code = %d\n%s", code, errOut)
		}
		b := onlyBackup(t, root)
		copied, err := cryptox.LoadKeyFile(filepath.Join(b, backupKeyName))
		if err != nil {
			t.Fatalf("the key was not copied: %v", err)
		}
		if copied.Encode() != key.Encode() {
			t.Error("the copied key is not this instance's key")
		}
		info, err := os.Stat(filepath.Join(b, backupKeyName))
		if err != nil || info.Mode().Perm() != 0o600 {
			t.Errorf("the copied key is mode %v (%v); it is a credential", info.Mode().Perm(), err)
		}
		// The backup now decrypts itself, which is a different object from the
		// one the default produces, and the operator has to know that.
		if !strings.Contains(out.String(), "IN this backup") {
			t.Errorf("the summary does not say the backup now carries its own key:\n%s", out)
		}
	})
}

// Retention deletes, so it only ever deletes what this command made. The
// directory an operator points --dir at is often shared with somebody else's
// copies, and a retention rule that guessed would eventually take one.
func TestRetentionRemovesOnlyThisCommandsOwnBackups(t *testing.T) {
	backupHost(t)
	root := t.TempDir()

	// A stranger's directory, named plausibly, with no manifest in it.
	strangers := filepath.Join(root, backupDirPrefix+"00000000-000000")
	if err := os.MkdirAll(strangers, 0o700); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(filepath.Join(strangers, "notes.txt"), []byte("somebody else's"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	// Three of ours, distinguishable because the name carries the timestamp.
	for _, stamp := range []string{"20250101-000001", "20250101-000002", "20250101-000003"} {
		d := filepath.Join(root, backupDirPrefix+stamp)
		if err := os.MkdirAll(d, 0o700); err != nil {
			t.Fatalf("MkdirAll: %v", err)
		}
		if err := os.WriteFile(filepath.Join(d, backupManifestName), []byte("{}"), 0o600); err != nil {
			t.Fatalf("WriteFile: %v", err)
		}
	}

	e, out, errOut := newTestEnv(t)
	if code := dispatch(context.Background(), e, []string{"backup", "--dir", root, "--keep", "2"}); code != exitOK {
		t.Fatalf("exit code = %d\n%s", code, errOut)
	}

	if _, err := os.Stat(filepath.Join(strangers, "notes.txt")); err != nil {
		t.Errorf("retention deleted a directory this command did not write: %v", err)
	}
	names, err := os.ReadDir(root)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	var ours []string
	for _, n := range names {
		if n.Name() != filepath.Base(strangers) {
			ours = append(ours, n.Name())
		}
	}
	// The new one and the newest old one.
	if len(ours) != 2 {
		t.Errorf("--keep 2 left %v", ours)
	}
	if !strings.Contains(out.String(), "keeping the newest 2") {
		t.Errorf("the summary does not say what it removed:\n%s", out)
	}
}
