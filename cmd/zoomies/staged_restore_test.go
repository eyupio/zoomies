package main

import (
	"context"
	"errors"
	"log/slog"
	"path/filepath"
	"strings"
	"testing"

	"github.com/eyupio/zoomies/internal/backup"
	"github.com/eyupio/zoomies/internal/config"
	"github.com/eyupio/zoomies/internal/store"
)

// A restore staged from the settings page is applied by the next controller
// to start, before it opens the database. The controller command runs this
// between taking the lock and opening the store, so the database it goes on
// to open is the restored one.
func TestAStagedRestoreIsAppliedBeforeTheDatabaseIsOpened(t *testing.T) {
	dir, _ := backupHost(t)
	src := takeBackup(t)
	ctx := context.Background()
	cfg, err := config.Load("")
	if err != nil {
		t.Fatal(err)
	}
	live := filepath.Join(dir, "zoomies.db")

	// Something after the backup, which the restore takes away.
	st, err := store.Open(ctx, store.Options{Path: live})
	if err != nil {
		t.Fatal(err)
	}
	if err := st.CreateInstallation(ctx, &store.Installation{AppID: 1, InstallationID: 2, Target: "later", TargetType: store.TargetOrg}); err != nil {
		t.Fatal(err)
	}
	st.Close()

	entry, err := backup.Get(filepath.Dir(src), filepath.Base(src))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := backup.Stage(ctx, cfg, entry, backup.Staged{RequestedBy: "alice"}); err != nil {
		t.Fatalf("Stage: %v", err)
	}

	if err := applyStagedRestore(ctx, cfg, slog.New(slog.DiscardHandler)); err != nil {
		t.Fatalf("applyStagedRestore: %v", err)
	}
	restored, err := store.Open(ctx, store.Options{Path: live})
	if err != nil {
		t.Fatal(err)
	}
	defer restored.Close()
	insts, _ := restored.ListInstallations(ctx)
	if len(insts) != 1 || insts[0].Target != "acme" {
		t.Errorf("the database at start is not the backup: %+v", insts)
	}
	if f, _ := restored.RecoveryFenced(ctx); !f.Fenced {
		t.Error("the restored fleet is not fenced")
	}
	if outcome, _ := backup.LastOutcome(live); outcome == nil || !outcome.OK {
		t.Errorf("the outcome was not recorded for the page: %+v", outcome)
	}
	// And a start with nothing staged does nothing.
	if err := applyStagedRestore(ctx, cfg, slog.New(slog.DiscardHandler)); err != nil {
		t.Errorf("a second start: %v", err)
	}
}

// A restart the settings page asked for is not a failure and not a clean
// exit: a service manager set to restart on failure has to see a non-zero
// code, and a script has to be able to tell it from a crash.
func TestARequestedRestartHasItsOwnExitCode(t *testing.T) {
	e, _, errOut := newTestEnv(t)
	code := report(e, "controller", &restartRequested{reason: "restoring zoomies-20260916-120000"})
	if code != exitRestart {
		t.Fatalf("exit code = %d, want %d", code, exitRestart)
	}
	if !strings.Contains(errOut.String(), "restoring zoomies-20260916-120000") || !strings.Contains(errOut.String(), "Start it again") {
		t.Errorf("the message does not say why or what to do:\n%s", errOut)
	}
	if code := report(e, "controller", errors.New("bind: address in use")); code != exitError {
		t.Errorf("an ordinary failure exits %d", code)
	}
}
