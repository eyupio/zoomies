//go:build !windows

package backend

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/eyupio/zoomies/internal/store"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestNativeSlotsReserveBeforeRegistrationAndReleaseAfterRemoval(t *testing.T) {
	b, root := newStubProcessBackend(t)
	installStubRunner(t, root, stubVersion, "#!/bin/sh\ntrap 'exit 0' TERM\necho ready\nwhile :; do sleep 1; done\n", stubConfigOK)
	b.admissionFile = filepath.Join(root, "admission.json")
	policy := ProcessAdmission{Directory: filepath.Join(root, "slots"), Capacity: 2, Env: map[string]string{"BUILD_SLOT": "test-{slot}"}}
	data, _ := json.Marshal(policy)
	os.WriteFile(b.admissionFile, data, 0600)
	handles := make(chan Handle, 3)
	errs := make(chan error, 3)
	var wg sync.WaitGroup
	for i := 0; i < 3; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			spec := processSpec()
			spec.Name += "-" + strconv.Itoa(i)
			h, err := b.Create(context.Background(), spec)
			if err != nil {
				errs <- err
			} else {
				handles <- h
			}
		}(i)
	}
	wg.Wait()
	close(handles)
	close(errs)
	if len(handles) != 2 || len(errs) != 1 {
		t.Fatalf("created %d runners, errors %d", len(handles), len(errs))
	}
	for err := range errs {
		if Fault(err) != store.FaultBackendBusy {
			t.Fatal(err)
		}
	}
	seen := map[int]bool{}
	for h := range handles {
		lease := readProcessLease(string(h))
		if lease == nil || seen[lease.Slot] {
			t.Fatal("missing or duplicate slot")
		}
		seen[lease.Slot] = true
		waitForLog(t, string(h), "ready", 5*time.Second)
		if err := b.Stop(context.Background(), h, 5*time.Second); err != nil {
			t.Fatal(err)
		}
		if err := b.Remove(context.Background(), h); err != nil {
			t.Fatal(err)
		}
	}
	entries, _ := os.ReadDir(policy.Directory)
	for _, entry := range entries {
		if entry.IsDir() {
			t.Fatal("slot not released")
		}
	}
}

func TestNativeAdmissionPreservesOtherOwnersAndFailsClosed(t *testing.T) {
	b, root := newStubProcessBackend(t)
	b.admissionFile = filepath.Join(root, "policy")
	dir := filepath.Join(root, "job")
	os.Mkdir(dir, 0700)
	spec := processSpec()
	if _, err := b.reserveSlot(dir, &spec); err == nil {
		t.Fatal("missing policy admitted a job")
	}
	policy := ProcessAdmission{Directory: filepath.Join(root, "slots"), Capacity: 1}
	data, _ := json.Marshal(policy)
	os.WriteFile(b.admissionFile, data, 0600)
	occupied := filepath.Join(policy.Directory, "1")
	os.MkdirAll(occupied, 0700)
	os.WriteFile(filepath.Join(occupied, "owner"), []byte("legacy-lane"), 0600)
	if _, err := b.reserveSlot(dir, &spec); Fault(err) != store.FaultBackendBusy {
		t.Fatalf("occupied slot: %v", err)
	}
	owner, _ := os.ReadFile(filepath.Join(occupied, "owner"))
	if string(owner) != "legacy-lane" {
		t.Fatal("modified another owner")
	}
	policy.Capacity = 0
	data, _ = json.Marshal(policy)
	os.WriteFile(b.admissionFile, data, 0600)
	if _, err := b.reserveSlot(dir, &spec); Fault(err) != store.FaultBackendBusy {
		t.Fatalf("paused policy: %v", err)
	}
}

func TestNativeAdmissionExportsItsOwnSlotAndRejectsExpiredPolicy(t *testing.T) {
	b, root := newStubProcessBackend(t)
	b.admissionFile = filepath.Join(root, "policy")
	dir := filepath.Join(root, "job")
	os.Mkdir(dir, 0700)
	policy := ProcessAdmission{Directory: filepath.Join(root, "slots"), Capacity: 1, Env: map[string]string{"BUILD_SLOT": "native-{slot}", "TMPDIR": "{job}/tmp"}}
	data, _ := json.Marshal(policy)
	os.WriteFile(b.admissionFile, data, 0600)
	spec := processSpec()
	lease, err := b.reserveSlot(dir, &spec)
	if err != nil {
		t.Fatal(err)
	}
	if spec.Env["CI_SLOT"] != "1" || spec.Env["BUILD_SLOT"] != "native-1" || spec.Env["TMPDIR"] != filepath.Join(dir, "tmp") {
		t.Fatalf("slot env: %v", spec.Env)
	}
	if _, err := os.Stat(spec.Env["TMPDIR"]); err != nil {
		t.Fatal(err)
	}
	lease.release()
	policy.ExpiresAt = float64(time.Now().Add(-time.Minute).Unix())
	data, _ = json.Marshal(policy)
	os.WriteFile(b.admissionFile, data, 0600)
	if _, err := b.reserveSlot(dir, &spec); Fault(err) != store.FaultBackendBusy {
		t.Fatalf("expired policy: %v", err)
	}
}

func TestLowerNativeCapacityWaitsForHigherNumberedSlots(t *testing.T) {
	b, root := newStubProcessBackend(t)
	b.admissionFile = filepath.Join(root, "policy")
	dir := filepath.Join(root, "job")
	os.Mkdir(dir, 0700)
	policy := ProcessAdmission{Directory: filepath.Join(root, "slots"), Capacity: 1}
	os.MkdirAll(filepath.Join(policy.Directory, "2"), 0700)
	data, _ := json.Marshal(policy)
	os.WriteFile(b.admissionFile, data, 0600)
	spec := processSpec()
	if _, err := b.reserveSlot(dir, &spec); Fault(err) != store.FaultBackendBusy {
		t.Fatalf("higher slot did not block admission: %v", err)
	}
}

func TestNativeRemovalRecoversSlotAfterPartialDirectoryDeletion(t *testing.T) {
	for _, directoryGone := range []bool{false, true} {
		t.Run(strconv.FormatBool(directoryGone), func(t *testing.T) {
			b, root := newStubProcessBackend(t)
			b.admissionFile = filepath.Join(root, "policy")
			dir := filepath.Join(root, "job")
			os.Mkdir(dir, 0700)
			policy := ProcessAdmission{Directory: filepath.Join(root, "slots"), Capacity: 1}
			data, _ := json.Marshal(policy)
			os.WriteFile(b.admissionFile, data, 0600)
			spec := processSpec()
			lease, err := b.reserveSlot(dir, &spec)
			if err != nil {
				t.Fatal(err)
			}
			// Metadata disappeared while the owner was still alive: never free it.
			os.Remove(filepath.Join(dir, ".zoomies-slot"))
			if directoryGone {
				os.RemoveAll(dir)
			}
			if err := b.Remove(context.Background(), Handle(dir)); err == nil {
				t.Fatal("removed a live owner's slot")
			}
			if _, err := os.Stat(lease.Path); err != nil {
				t.Fatal(err)
			}
			// Use the PID of a child that has exited and been reaped.
			cmd := exec.Command("/bin/sh", "-c", "exit 0")
			if err := cmd.Run(); err != nil {
				t.Fatal(err)
			}
			if err := lease.bind(cmd.Process.Pid); err != nil {
				t.Fatal(err)
			}
			if err := b.Remove(context.Background(), Handle(dir)); err != nil {
				t.Fatal(err)
			}
			if _, err := os.Stat(lease.Path); !os.IsNotExist(err) {
				t.Fatalf("stale slot remains: %v", err)
			}
		})
	}
}

func TestConcurrentNativeAdmissionsRespectALoweredCapacity(t *testing.T) {
	b, root := newStubProcessBackend(t)
	b.admissionFile = filepath.Join(root, "policy")
	policy := ProcessAdmission{Directory: filepath.Join(root, "slots"), Capacity: 2}
	os.MkdirAll(filepath.Join(policy.Directory, "3"), 0700)
	data, _ := json.Marshal(policy)
	os.WriteFile(b.admissionFile, data, 0600)
	for round := 0; round < 50; round++ {
		start := make(chan struct{})
		var admitted atomic.Int32
		var leases sync.Map
		var wg sync.WaitGroup
		for worker := 0; worker < 8; worker++ {
			wg.Add(1)
			go func(worker int) {
				defer wg.Done()
				dir := filepath.Join(root, fmt.Sprintf("job-%d-%d", round, worker))
				os.Mkdir(dir, 0700)
				spec := processSpec()
				<-start
				if lease, err := b.reserveSlot(dir, &spec); err == nil {
					admitted.Add(1)
					leases.Store(worker, lease)
				} else if Fault(err) != store.FaultBackendBusy {
					t.Error(err)
				}
			}(worker)
		}
		close(start)
		wg.Wait()
		if admitted.Load() != 1 {
			t.Fatalf("admitted %d beside one existing job at limit 2", admitted.Load())
		}
		leases.Range(func(_, value any) bool { value.(*processLease).release(); return true })
	}
}
