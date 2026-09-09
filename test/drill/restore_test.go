//go:build drill

package drill

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The mechanical half of ZF-203's acceptance: take a backup from a running
// fleet, stop it, restore into a clean state directory, and start a controller
// on what came back.
//
// It is a drill rather than a unit test because every part of it is about the
// product as an operator has it: `zoomies backup` and `zoomies restore` are
// commands run against a database on disk, the controller is a process that
// reads a fence at startup and refuses readiness, and the thing being proved
// is that the file travelled. None of that exists inside the test process.
//
// It now runs a job on the restored fleet too, which is the half Gate F's
// sixth bullet asks for and the half that is easy to skip: a controller that
// answers readiness has proved that it starts, not that it works. The agent is
// never told any of this happened -- it is still polling the address it joined
// -- so a restore that lost the host row or its credentials would leave a
// fleet that looks healthy and can place nothing.
//
// What remains an owner action against a real organisation is the same journey
// against real GitHub, and that is recorded in roadmap/validation/ instead.
func TestABackupCanBeRestoredIntoACleanDirectoryAndComesBackFenced(t *testing.T) {
	f := newFleet(t)
	rec := newRecord(t, "restore")
	defer rec.write()
	// Something worth restoring: a pool this drill made, which is a row the
	// restored database either has or does not.
	label := "drill-restore"
	poolID := f.createPool("drillrestore", label)
	rec.note("created pool", poolID)

	// Taken while the controller is running, which is the case VACUUM INTO
	// exists for: copying the file here would leave the pool in the
	// write-ahead log.
	backups := t.TempDir()
	out := f.run(f.stateDir, "backup", "--dir", backups)
	if !strings.Contains(out, "integrity ok") {
		t.Fatalf("the backup did not check out:\n%s", out)
	}
	src := onlyDir(t, backups)
	rec.note("backed up", filepath.Base(src))

	f.controller.kill()
	rec.note("stopped the controller", "kill")

	// A clean directory: nothing here but the encryption key and what the
	// restore puts in it. The key is the operator's to carry across -- the
	// backup deliberately does not contain it -- and restoring without it is
	// refused, which is a thing this drill proves by having to do the work.
	fresh := t.TempDir()
	carryKey(t, f.stateDir, fresh)
	rec.note("carried the encryption key across", "encryption.key")
	out = f.run(fresh, "restore", src)
	if !strings.Contains(out, "Restored") {
		t.Fatalf("restore did not report success:\n%s", out)
	}
	rec.note("restored", "into a clean state directory")

	f.startControllerOn(fresh)

	// Fenced: serving, and doing nothing. This is the assertion the whole
	// drill exists for -- a restored fleet that came up acting would already
	// have created runners against an organisation whose state it last saw
	// when the backup was taken.
	resp, err := http.Get(f.baseURL + "/readyz")
	if err != nil {
		t.Fatalf("readiness: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Errorf("a restored controller answered readiness with %d; want 503 while fenced", resp.StatusCode)
	}
	var ready struct {
		OK     bool   `json:"ok"`
		Fenced bool   `json:"fenced"`
		Reason string `json:"reason"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&ready); err != nil {
		t.Fatalf("decoding the readiness body: %v", err)
	}
	if !ready.Fenced || ready.OK {
		t.Errorf("readiness does not say it is fenced: %+v", ready)
	}
	if !strings.Contains(ready.Reason, filepath.Base(src)) {
		t.Errorf("the reason does not name the backup it came from: %q", ready.Reason)
	}
	rec.note("came back fenced", ready.Reason)

	// The data travelled.
	var pools struct {
		Items []struct {
			ID string `json:"id"`
		} `json:"items"`
	}
	f.api.get("/pools", &pools)
	var found bool
	for _, p := range pools.Items {
		if p.ID == poolID {
			found = true
		}
	}
	if !found {
		t.Errorf("the pool created before the backup is not in the restored fleet: %+v", pools.Items)
	}

	// And the fence lifts.
	var lifted struct {
		Fenced bool `json:"fenced"`
	}
	f.api.post("/recovery/unfence", nil, &lifted)
	if lifted.Fenced {
		t.Fatal("the fence did not lift")
	}
	waitFor(t, waitProcessUp, "readiness to come back", func() bool {
		r, err := http.Get(f.baseURL + "/readyz")
		if err != nil {
			return false
		}
		r.Body.Close()
		return r.StatusCode == http.StatusOK
	})
	rec.note("lifted the fence", "the fleet is ready again")

	// And it works: a queued job becomes a runner on the host that was already
	// there. Nothing re-joined the agent -- the restored database carries the
	// host it had when the backup was taken, and the agent is still polling
	// the address it has always polled.
	job := f.gh.AddQueuedJob("acme/api", "CI", "build", []string{"self-hosted", label})
	var runner runnerView
	waitFor(t, waitRunnerCreated, "the restored fleet to create a runner", func() bool {
		for _, r := range f.runners(poolID) {
			runner = r
			return true
		}
		return false
	})
	waitFor(t, waitWorkloadUp, "a workload to appear on the host", func() bool {
		return len(f.liveWorkloads()) == 1
	})
	rec.note("job placed after the restore", runner.Name)

	f.gh.StartJob(job.ID, runner.Name)
	f.deliverJob("in_progress", job, runner.Name, "")
	f.gh.CompleteJob(job.ID, "success")
	f.deliverJob("completed", job, runner.Name, "success")
	if err := finishJob(f.runnerDir(runner.Name)); err != nil {
		t.Fatalf("telling the stub runner its job is over: %v", err)
	}
	waitFor(t, waitRunnerGone, "the workload to be gone from the host", func() bool {
		return len(f.liveWorkloads()) == 0
	})
	rec.recovered()
	rec.note("host workloads after the restored fleet finished the job", "0")
	rec.pass("a backup taken from a running fleet restores into an empty state directory, " +
		"comes back fenced, serves again once the fence is lifted, and runs a job on the host it had before")
}

// carryKey copies the instance encryption key from one state directory to
// another, which is exactly the step docs/backup-and-restore.md tells an
// operator to take by hand.
func carryKey(t *testing.T, from, to string) {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(from, "encryption.key"))
	if err != nil {
		t.Fatalf("reading the encryption key the controller generated: %v", err)
	}
	if err := os.WriteFile(filepath.Join(to, "encryption.key"), raw, 0o600); err != nil {
		t.Fatalf("putting the encryption key in place: %v", err)
	}
}

// onlyDir returns the single directory under root.
func onlyDir(t *testing.T, root string) string {
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
		t.Fatalf("want one directory under %s, found %v", root, dirs)
	}
	return dirs[0]
}
