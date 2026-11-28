//go:build !windows

package backend

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"syscall"
	"testing"
	"time"
)

func TestMacDefaultStopsWithTERM(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("macOS stop signal")
	}
	b, root := newStubProcessBackend(t)
	installStubRunner(t, root, stubVersion, "#!/bin/sh\ntrap 'echo terminated; exit 0' TERM\ntrap '' INT\necho ready\nwhile :; do sleep 0.1; done\n", stubConfigOK)
	h, err := b.Create(context.Background(), processSpec())
	if err != nil {
		t.Fatal(err)
	}
	defer b.Remove(context.Background(), h)
	waitForLog(t, string(h), "ready", 5*time.Second)
	if err := b.Stop(context.Background(), h, 5*time.Second); err != nil {
		t.Fatal(err)
	}
	waitForLog(t, string(h), "terminated", time.Second)
}

func TestMacDefaultWaitsForWorkersAndEscalatesAfterTERM(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("macOS process groups")
	}
	b, root := newStubProcessBackend(t)
	installStubRunner(t, root, stubVersion, "#!/bin/sh\ntrap 'echo terminated; exit 0' TERM\nsh -c 'trap \"\" TERM INT; echo child-ready; while :; do sleep 0.1; done' &\nwait\n", stubConfigOK)
	h, err := b.Create(context.Background(), processSpec())
	if err != nil {
		t.Fatal(err)
	}
	defer b.Remove(context.Background(), h)
	waitForLog(t, string(h), "child-ready", 5*time.Second)
	pid := readPID(string(h))
	started := time.Now()
	if err := b.Stop(context.Background(), h, 100*time.Millisecond); err != nil {
		t.Fatal(err)
	}
	if time.Since(started) < 100*time.Millisecond {
		t.Fatal("did not allow TERM its timeout")
	}
	waitForLog(t, string(h), "terminated", time.Second)
	if runnerGroupAlive(pid) {
		t.Fatal("worker group survived escalation")
	}
	if err := b.Remove(context.Background(), h); err != nil {
		t.Fatal(err)
	}
}

func TestNativeFailedStartStopsAResistantChildWithinTheTimeout(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("macOS startup cleanup")
	}
	b, root := newStubProcessBackend(t)
	log, err := os.Create(filepath.Join(root, "runner.log"))
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("/bin/sh", "-c", "trap '' TERM; echo ready; while :; do sleep 0.1; done")
	cmd.Stdout, cmd.Stderr = log, log
	detachRunner(cmd)
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer signalRunner(cmd.Process, syscall.SIGKILL)
	waitForLog(t, root, "ready", 5*time.Second)
	if err := b.abandon(cmd, log, 50*time.Millisecond); err != nil {
		t.Fatal(err)
	}
	if runnerGroupAlive(cmd.Process.Pid) {
		t.Fatal("failed startup left a live child")
	}
}

func TestNativeOrphanRetainsItsSlotUntilTheGroupExits(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("macOS process groups")
	}
	b, root := newStubProcessBackend(t)
	b.admissionFile = filepath.Join(root, "policy")
	policy := ProcessAdmission{Directory: filepath.Join(root, "slots"), Capacity: 1}
	data, _ := json.Marshal(policy)
	os.WriteFile(b.admissionFile, data, 0600)
	installStubRunner(t, root, stubVersion, "#!/bin/sh\nsh -c 'trap \"\" TERM; echo child-ready; while [ ! -f release-child ]; do sleep 0.1; done' &\nwhile [ ! -f exit-parent ]; do sleep 0.1; done\n", stubConfigOK)
	h, err := b.Create(context.Background(), processSpec())
	if err != nil {
		t.Fatal(err)
	}
	pid := readPID(string(h))
	defer func() {
		os.WriteFile(filepath.Join(string(h), "release-child"), nil, 0600)
		waitFor(context.Background(), func() bool { return !runnerGroupAlive(pid) }, 5*time.Second, 20*time.Millisecond)
		b.Remove(context.Background(), h)
	}()
	waitForLog(t, string(h), "child-ready", 5*time.Second)
	os.WriteFile(filepath.Join(string(h), "exit-parent"), nil, 0600)
	if !waitFor(context.Background(), func() bool { return !processAlive(pid) }, 5*time.Second, 20*time.Millisecond) {
		t.Fatal("parent did not exit")
	}
	if err := b.Stop(context.Background(), h, time.Second); err == nil {
		t.Fatal("unverified orphan group reported stopped")
	}
	if err := b.Remove(context.Background(), h); err == nil {
		t.Fatal("removed a job with surviving workers")
	}
	if _, err := os.Stat(filepath.Join(policy.Directory, "1")); err != nil {
		t.Fatal("released orphan reservation", err)
	}
}
