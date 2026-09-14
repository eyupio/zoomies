package main

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"testing"

	"github.com/eyupio/zoomies/internal/config"
	"github.com/eyupio/zoomies/internal/controller"
	"github.com/eyupio/zoomies/internal/store"
)

func leaseTestStore(t *testing.T) *store.Store {
	t.Helper()
	st, err := store.Open(t.Context(), store.Options{Path: ":memory:"})
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	return st
}

// The message is the whole point of the lease: an operator who has just been
// refused needs to know which machine to go and look at, and "the database is
// locked" would send them to the wrong one.
func TestTheSecondControllerIsToldWhichMachineHasTheDatabase(t *testing.T) {
	ctx := context.Background()
	st := leaseTestStore(t)

	// A live lease held by another machine. It has to be another machine:
	// a predecessor on this host is deliberately not a rival, since whoever is
	// asking holds the file lock and so nothing here is still running.
	other := &store.ControllerLease{
		Holder: store.NewID(store.PrefixController), Host: "vm-elsewhere",
		PID: 4242, Version: "1.0.0",
	}
	if _, err := st.AcquireControllerLease(ctx, other, controller.LeaseTTL, false); err != nil {
		t.Fatalf("seeding the other controller's lease: %v", err)
	}

	_, err := takeControllerLease(ctx, st, false)
	if err == nil {
		t.Fatal("a second controller took a database another machine holds")
	}
	msg := err.Error()
	if !strings.Contains(msg, "another controller holds this database") {
		t.Fatalf("error = %v, want it to name the conflict", err)
	}
	// The two things the operator needs: which machine, and how to proceed if
	// that machine is actually gone.
	if !strings.Contains(msg, "vm-elsewhere") {
		t.Errorf("the refusal must identify the holder: %v", err)
	}
	if !strings.Contains(msg, "--takeover") {
		t.Errorf("the refusal must name the way out: %v", err)
	}

	// --takeover exists for the machine that is really gone, and it has to
	// actually work -- otherwise the advice above is a dead end.
	mine, err := takeControllerLease(ctx, st, true)
	if err != nil {
		t.Fatalf("takeover: %v", err)
	}
	if mine.Holder == "" || mine.PID == 0 {
		t.Fatalf("the takeover produced a lease that says nothing about who holds it: %+v", mine)
	}
}

// A controller that restarts fast enough to beat its own lease's expiry must
// not refuse to start, which is the opposite of what the lease is for. The
// caller holds the file lock, so a predecessor on this host is already gone.
func TestRestartingOnTheSameHostIsNotAConflict(t *testing.T) {
	ctx := context.Background()
	st := leaseTestStore(t)

	first, err := takeControllerLease(ctx, st, false)
	if err != nil {
		t.Fatalf("taking the first lease: %v", err)
	}
	again, err := takeControllerLease(ctx, st, false)
	if err != nil {
		t.Fatalf("a restart on the same host was refused: %v", err)
	}
	if again.Host != first.Host {
		t.Fatalf("host = %q, want this machine's own", again.Host)
	}
}

// The configured level is where logging starts, and the gate it starts behind
// is the one SIGHUP later moves.
func TestSetupLoggingHonoursTheConfiguredLevel(t *testing.T) {
	cfg := config.Default()
	cfg.Log.Level = "warn"

	log, level := setupLogging(cfg)
	if log == nil || level == nil {
		t.Fatal("setupLogging returned nothing to log with")
	}
	if level.Level() != slog.LevelWarn {
		t.Fatalf("level = %v, want warn", level.Level())
	}

	// The inner handler is built at debug so that it never filters anything
	// itself; the wrapper is the only gate, which is what lets SIGHUP move it
	// without rebuilding loggers the controller already captured.
	level.Set(slog.LevelDebug)
	if !log.Enabled(context.Background(), slog.LevelDebug) {
		t.Fatal("the level change did not reach the logger")
	}
}

// A group on a level-gated handler must keep the gate, or a component that
// groups its attributes would quietly log at a level nobody asked for.
func TestGroupingALevelGatedHandlerKeepsTheGate(t *testing.T) {
	level := new(slog.LevelVar)
	level.Set(slog.LevelError)

	var buf bytes.Buffer
	inner := slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})
	log := slog.New(&levelHandler{Handler: inner, level: level}).WithGroup("agent")

	log.Info("suppressed")
	if buf.Len() != 0 {
		t.Fatalf("a grouped logger ignored the level: %s", buf.String())
	}

	level.Set(slog.LevelInfo)
	log.Info("allowed", "host", "vm-1")
	body := buf.String()
	if !strings.Contains(body, "allowed") {
		t.Fatalf("the grouped logger dropped a line it should have kept: %s", body)
	}
	if !strings.Contains(body, "agent.host") {
		t.Fatalf("the group was lost: %s", body)
	}
}
