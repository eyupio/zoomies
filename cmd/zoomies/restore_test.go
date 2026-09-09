package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/eyupio/zoomies/internal/cryptox"
	"github.com/eyupio/zoomies/internal/store"
)

// takeBackup runs `zoomies backup` on the host prepared by backupHost and
// returns the directory it wrote. Restoring what this command actually
// produces, rather than a fixture shaped like it, is the point: the two halves
// have to agree about the manifest and the file names.
func takeBackup(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	e, _, errOut := newTestEnv(t)
	if code := dispatch(context.Background(), e, []string{"backup", "--dir", root}); code != exitOK {
		t.Fatalf("backup exit code = %d\n%s", code, errOut)
	}
	return onlyBackup(t, root)
}

// The database at database.path is the fleet. Overwriting it because somebody
// typed a command is the one mistake a restore must not make on its own, and
// the copy it moves aside is what makes --replace recoverable.
func TestRestoreWillNotOverwriteADatabaseWithoutBeingTold(t *testing.T) {
	dir, _ := backupHost(t)
	src := takeBackup(t)
	live := filepath.Join(dir, "zoomies.db")

	e, _, errOut := newTestEnv(t)
	if code := dispatch(context.Background(), e, []string{"restore", src}); code != exitError {
		t.Fatalf("restore over a live database exit code = %d, want %d", code, exitError)
	}
	if !strings.Contains(errOut.String(), "--replace") {
		t.Errorf("the refusal does not name the flag that allows it:\n%s", errOut)
	}

	e2, out, errOut2 := newTestEnv(t)
	if code := dispatch(context.Background(), e2, []string{"restore", src, "--replace"}); code != exitOK {
		t.Fatalf("exit code = %d\n%s", code, errOut2)
	}
	// The database that was there is beside it, not gone: a restore of the
	// wrong backup is survivable only if the old one is still on disk.
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var kept bool
	for _, en := range entries {
		if strings.HasPrefix(en.Name(), "zoomies.db.before-restore-") {
			kept = true
		}
	}
	if !kept {
		t.Errorf("--replace deleted the database that was there; %v", entries)
	}
	if !strings.Contains(out.String(), "Moved the database that was there") {
		t.Errorf("the summary does not say where the old database went:\n%s", out)
	}
	if _, err := os.Stat(live); err != nil {
		t.Errorf("nothing was restored to %s: %v", live, err)
	}
}

// A key that does not open the restored database produces a fleet that starts,
// reports itself healthy and fails inside its first GitHub call -- by which
// time the original may be gone. The fingerprint in the manifest is what lets
// this be caught before anything is moved.
func TestRestoreRefusesAKeyThatDidNotSealTheBackup(t *testing.T) {
	dir, _ := backupHost(t)
	src := takeBackup(t)

	// A different key on this host, and the live database out of the way so
	// the key check is the only thing that can refuse.
	other, err := cryptox.GenerateKey()
	if err != nil {
		t.Fatal(err)
	}
	wrong := filepath.Join(dir, "wrong.key")
	if err := cryptox.WriteKeyFile(wrong, other); err != nil {
		t.Fatal(err)
	}
	t.Setenv("ZOOMIES_ENCRYPTION_KEY_FILE", wrong)
	live := filepath.Join(dir, "zoomies.db")
	if err := os.Remove(live); err != nil {
		t.Fatal(err)
	}

	e, _, errOut := newTestEnv(t)
	if code := dispatch(context.Background(), e, []string{"restore", src}); code != exitError {
		t.Fatalf("exit code = %d, want %d", code, exitError)
	}
	// Both fingerprints, because the operator's next act is to go and find the
	// right key and they need to know what they are looking for.
	for _, want := range []string{other.Fingerprint(), "not the one that sealed this backup"} {
		if !strings.Contains(errOut.String(), want) {
			t.Errorf("the refusal does not mention %q:\n%s", want, errOut)
		}
	}
	if _, err := os.Stat(live); !os.IsNotExist(err) {
		t.Error("the refusal happened after the database had already been put in place")
	}
}

// A backup that did not survive being copied somewhere is worse than no backup,
// because it is restored with confidence. The check happens before anything is
// moved, while the original is still there.
func TestRestoreRefusesACopyThatIsNotSound(t *testing.T) {
	dir, _ := backupHost(t)
	src := takeBackup(t)

	// Truncating is what a half-finished transfer looks like.
	whole, err := os.ReadFile(filepath.Join(src, backupDBName))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, backupDBName), whole[:len(whole)/3], 0o600); err != nil {
		t.Fatal(err)
	}

	live := filepath.Join(dir, "zoomies.db")
	before, err := os.ReadFile(live)
	if err != nil {
		t.Fatal(err)
	}

	e, _, errOut := newTestEnv(t)
	if code := dispatch(context.Background(), e, []string{"restore", src, "--replace"}); code != exitError {
		t.Fatalf("a truncated backup was restored; exit code = %d", code)
	}
	after, err := os.ReadFile(live)
	if err != nil || len(after) != len(before) {
		t.Errorf("the live database was touched before the copy had been checked: %v", err)
	}
	if errOut.Len() == 0 {
		t.Error("the refusal said nothing")
	}
}

// The credentials a backup froze are still valid when it is restored, and a
// cookie from the day it was taken signs somebody in. What is a default and
// what is a flag is the judgement here: ending sessions costs a sign-in, and
// revoking API tokens breaks whatever automation holds one.
func TestRestoreInvalidatesTheCredentialsTheBackupFroze(t *testing.T) {
	dir, _ := backupHost(t)
	ctx := context.Background()
	live := filepath.Join(dir, "zoomies.db")

	// A session, an unredeemed join token, a redeemed one, and an API token,
	// all of them in the backup because they are in the database when it is
	// taken.
	st, err := store.Open(ctx, store.Options{Path: live})
	if err != nil {
		t.Fatal(err)
	}
	user := &store.User{Username: "ops", Role: store.RoleAdmin, PasswordHash: "x"}
	if err := st.CreateUser(ctx, user); err != nil {
		t.Fatal(err)
	}
	if err := st.CreateSession(ctx, &store.Session{UserID: user.ID, TokenHash: "sess", ExpiresAt: st.Now().Add(time.Hour)}); err != nil {
		t.Fatal(err)
	}
	if err := st.CreateJoinToken(ctx, &store.JoinToken{TokenHash: "unused", ExpiresAt: st.Now().Add(time.Hour)}); err != nil {
		t.Fatal(err)
	}
	if err := st.CreateAPIToken(ctx, &store.APIToken{Name: "ci", Role: store.RoleViewer, TokenHash: "api"}); err != nil {
		t.Fatal(err)
	}
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}

	src := takeBackup(t)
	e, out, errOut := newTestEnv(t)
	if code := dispatch(ctx, e, []string{"restore", src, "--replace"}); code != exitOK {
		t.Fatalf("exit code = %d\n%s", code, errOut)
	}

	restored, err := store.Open(ctx, store.Options{Path: live})
	if err != nil {
		t.Fatal(err)
	}
	defer restored.Close()

	if _, _, err := restored.GetSessionByTokenHash(ctx, "sess"); err == nil {
		t.Error("a session survived the restore, so a cookie from the day of the backup is still signed in")
	}
	joins, err := restored.ListJoinTokens(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(joins) != 0 {
		t.Errorf("an unredeemed join token survived the restore: %+v", joins)
	}
	// API tokens are a flag, not a default: revoking them breaks automation,
	// and that is the operator's call rather than this command's.
	tokens, err := restored.ListAPITokens(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(tokens) != 1 || tokens[0].Revoked {
		t.Errorf("the API token was revoked without being asked for: %+v", tokens)
	}
	if !strings.Contains(out.String(), "--revoke-api-tokens") {
		t.Errorf("the summary does not offer the flag that would revoke them:\n%s", out)
	}

	// And the restored database says it is in recovery, with a reason naming
	// where it came from.
	fence, err := restored.RecoveryFenced(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !fence.Fenced {
		t.Fatal("the restored database is not marked for recovery")
	}
	if !strings.Contains(fence.Reason, src) {
		t.Errorf("the reason does not say where this came from: %q", fence.Reason)
	}
	// An audit row, in the restored database, where anyone asking why this
	// fleet is fenced will look.
	rows, _, err := restored.ListAudit(ctx, store.AuditFilter{Actions: []string{"instance.restore"}}, store.Page{Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 {
		t.Errorf("the restore wrote %d audit rows, want 1", len(rows))
	}
}

// Restoring a backup from a later release onto an older binary is a rollback,
// which is a thing people do under pressure. The store refuses the schema and
// this names the release, before anything is moved.
func TestRestoreRefusesABackupFromANewerRelease(t *testing.T) {
	dir, _ := backupHost(t)
	src := takeBackup(t)

	// A migration this build does not have, planted in the copy: what a backup
	// taken by a newer release looks like from here.
	st, err := store.Open(context.Background(), store.Options{Path: filepath.Join(src, backupDBName)})
	if err != nil {
		t.Fatal(err)
	}
	if err := st.SetSetting(context.Background(), "unused", "x", false); err != nil {
		t.Fatal(err)
	}
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}
	if err := plantFutureMigration(filepath.Join(src, backupDBName)); err != nil {
		t.Fatalf("planting a future migration: %v", err)
	}

	live := filepath.Join(dir, "zoomies.db")
	before, err := os.ReadFile(live)
	if err != nil {
		t.Fatal(err)
	}

	e, _, errOut := newTestEnv(t)
	if code := dispatch(context.Background(), e, []string{"restore", src, "--replace"}); code != exitError {
		t.Fatalf("a backup from a newer release was restored; exit code = %d", code)
	}
	if !strings.Contains(errOut.String(), "9999_from_the_future.sql") {
		t.Errorf("the refusal does not name the migration this build lacks:\n%s", errOut)
	}
	after, err := os.ReadFile(live)
	if err != nil || len(after) != len(before) {
		t.Errorf("the live database was replaced anyway: %v", err)
	}
}
