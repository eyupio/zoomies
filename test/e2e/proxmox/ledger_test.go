package proxmox

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// A ledger directory the test can look inside. The real one is deliberately
// outside t.TempDir -- it has to outlive the run that wrote it -- so the
// environment override is how a test gets to see one at all.
func ledgerDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("ZOOMIES_PROXMOX_LEDGER_DIR", dir)
	return dir
}

func testEnv() env {
	return env{
		endpoint: "https://pve.example.com:8006",
		node:     "pve1",
		storage:  "local-lvm",
		vmidMin:  9000, vmidMax: 9099,
		target: "example-org", targetType: "org",
	}
}

// The blast radius is on disk before anything could have been created.
//
// This is the whole point of the file. A run killed between asking for a
// machine and seeing it caused something that exists and that nothing names, so
// the cluster, the node, the storage, the range and what was already in it are
// written first -- while there is still nothing to write about.
func TestALedgerNamesWhereToLookBeforeThereIsAnythingToFind(t *testing.T) {
	dir := ledgerDir(t)
	l, err := openLedger("run_abc", "zoomies-pve-run_abc", testEnv(), []int{9001, 9002})
	if err != nil {
		t.Fatalf("opening the ledger: %v", err)
	}

	rec, err := ReadLedger(filepath.Join(dir, "run_abc.json"))
	if err != nil {
		t.Fatalf("the ledger was not on disk immediately: %v", err)
	}
	if rec.Cluster != "https://pve.example.com:8006" || rec.Node != "pve1" || rec.Storage != "local-lvm" {
		t.Errorf("the ledger does not name the cluster it may touch: %+v", rec)
	}
	if rec.Range != (Range{Min: 9000, Max: 9099}) {
		t.Errorf("range = %v, want 9000-9099", rec.Range)
	}
	if len(rec.PreexistingVMIDs) != 2 {
		t.Errorf("what was already in the range was not recorded: %v", rec.PreexistingVMIDs)
	}
	if len(rec.Resources) != 0 {
		t.Errorf("a ledger with nothing created yet lists resources: %v", rec.Resources)
	}
	if rec.ClosedAt != nil {
		t.Error("a ledger is open until its resources are gone")
	}
	if got := l.record().RunID; got != "run_abc" {
		t.Errorf("record().RunID = %q", got)
	}
}

// Each resource reaches the disk as it is recorded, not when the run ends.
//
// A ledger held in memory and written at the end is no ledger at all: the run
// that needs it is the one that did not get to the end.
func TestEveryResourceIsOnDiskTheMomentItIsRecorded(t *testing.T) {
	dir := ledgerDir(t)
	l, err := openLedger("run_def", "label", testEnv(), nil)
	if err != nil {
		t.Fatalf("opening the ledger: %v", err)
	}
	if err := l.created(KindMachine, "mach_1", "zoomies-qualify-1"); err != nil {
		t.Fatalf("recording a machine: %v", err)
	}

	rec, err := ReadLedger(filepath.Join(dir, "run_def.json"))
	if err != nil {
		t.Fatalf("reading it back: %v", err)
	}
	if len(rec.Resources) != 1 || rec.Resources[0].ID != "mach_1" {
		t.Fatalf("the machine was not written when it was recorded: %+v", rec.Resources)
	}
	if rec.Resources[0].VMID != 0 {
		t.Error("a machine has no VMID until the provider has allocated one")
	}
}

// The identifier arrives after the row, and the ledger takes it separately.
//
// The window between "there is a machine row" and "it has a VMID" is exactly
// the window in which a run can die owning a guest it cannot name, so the row
// is recorded first and the identifier filled in when it is known.
func TestAMachineIsRecordedBeforeItHasAnIdentifierAndPlacedWhenItDoes(t *testing.T) {
	dir := ledgerDir(t)
	l, _ := openLedger("run_ghi", "label", testEnv(), nil)
	if err := l.created(KindMachine, "mach_1", "zoomies-qualify-1"); err != nil {
		t.Fatalf("recording a machine: %v", err)
	}
	if err := l.placed("mach_1", "pve1", 9042); err != nil {
		t.Fatalf("placing it: %v", err)
	}

	rec, _ := ReadLedger(filepath.Join(dir, "run_ghi.json"))
	if rec.Resources[0].VMID != 9042 || rec.Resources[0].Node != "pve1" {
		t.Errorf("the identifier was not recorded: %+v", rec.Resources[0])
	}

	// And a placement with nothing to place it on is refused loudly rather than
	// dropped: a VMID nobody wrote down is a guest nobody will find.
	err := l.placed("mach_missing", "pve1", 9043)
	if err == nil {
		t.Fatal("placing a resource with no ledger entry was accepted")
	}
	for _, want := range []string{"mach_missing", "9043"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the refusal does not name %s: %v", want, err)
		}
	}
}

// Removal is recorded per kind and identifier, and only once.
func TestRemovalMarksTheOneResourceItNamesAndLeavesTheRestOutstanding(t *testing.T) {
	l, _ := openLedger("run_jkl", "label", testEnv(), nil)
	for _, r := range []struct {
		kind     ResourceKind
		id, name string
	}{
		{KindMachine, "mach_1", "one"},
		{KindMachine, "mach_2", "two"},
		{KindPool, "pool_1", "pool"},
	} {
		if err := l.created(r.kind, r.id, r.name); err != nil {
			t.Fatalf("recording %s: %v", r.id, err)
		}
	}
	if err := l.placed("mach_2", "pve1", 9042); err != nil {
		t.Fatalf("placing mach_2: %v", err)
	}

	l.removed(KindMachine, "mach_1")

	out := l.outstanding()
	if len(out) != 2 {
		t.Fatalf("outstanding = %v, want the two that are left", out)
	}
	joined := strings.Join(out, "; ")
	if strings.Contains(joined, "mach_1") {
		t.Errorf("a removed resource is still outstanding: %s", joined)
	}
	// A guest that reached the cluster says where it is, because that is what
	// somebody reading this has to go and look at.
	if !strings.Contains(joined, "VM 9042 on pve1") {
		t.Errorf("an outstanding machine does not say where it is: %s", joined)
	}

	// The same kind and id under a different kind is a different resource.
	l.removed(KindPool, "mach_2")
	if len(l.outstanding()) != 2 {
		t.Errorf("removing a pool removed a machine: %v", l.outstanding())
	}
}

// A ledger is closed only when it describes nothing, and closing deletes it.
//
// Closing early would delete the only record of what is still on the cluster,
// which is the failure this whole file exists to prevent.
func TestALedgerRefusesToCloseWhileAnythingItNamesStillExists(t *testing.T) {
	dir := ledgerDir(t)
	l, _ := openLedger("run_mno", "label", testEnv(), nil)
	if err := l.created(KindMachine, "mach_1", "one"); err != nil {
		t.Fatalf("recording a machine: %v", err)
	}

	err := l.close()
	if err == nil {
		t.Fatal("a ledger with an outstanding machine was closed")
	}
	if !strings.Contains(err.Error(), "mach_1") {
		t.Errorf("the refusal does not say what is still there: %v", err)
	}
	if _, statErr := os.Stat(filepath.Join(dir, "run_mno.json")); statErr != nil {
		t.Errorf("a refused close removed the ledger anyway: %v", statErr)
	}

	l.removed(KindMachine, "mach_1")
	if err := l.close(); err != nil {
		t.Fatalf("closing a ledger with nothing left: %v", err)
	}
	if _, statErr := os.Stat(filepath.Join(dir, "run_mno.json")); !os.IsNotExist(statErr) {
		t.Error("a closed ledger is kept; the next sweep has to read past it")
	}
}

// Reading a directory of ledgers puts them in the order they were started, and
// a file that is not a ledger is reported rather than skipped.
//
// A half-written file is the shape a killed run leaves. Reading past it quietly
// is how its machines get forgotten, which is the one outcome this cannot have.
func TestReadingLedgersIsOldestFirstAndNeverSkipsOneItCannotParse(t *testing.T) {
	dir := t.TempDir()
	write := func(name string, rec LedgerRecord) {
		t.Helper()
		raw, err := json.Marshal(rec)
		if err != nil {
			t.Fatalf("encoding %s: %v", name, err)
		}
		if err := os.WriteFile(filepath.Join(dir, name), raw, 0o644); err != nil {
			t.Fatalf("writing %s: %v", name, err)
		}
	}
	base := time.Date(2026, 9, 14, 1, 0, 0, 0, time.UTC)
	write("b.json", LedgerRecord{RunID: "second", StartedAt: base.Add(time.Hour)})
	write("a.json", LedgerRecord{RunID: "first", StartedAt: base})
	// Not a ledger, and not named like one: skipped without complaint.
	if err := os.WriteFile(filepath.Join(dir, "notes.txt"), []byte("hello"), 0o644); err != nil {
		t.Fatalf("writing the decoy: %v", err)
	}

	got, err := ReadLedgers(dir)
	if err != nil {
		t.Fatalf("reading the ledgers: %v", err)
	}
	if len(got) != 2 || got[0].RunID != "first" || got[1].RunID != "second" {
		t.Fatalf("ledgers came back as %v, want oldest first", got)
	}

	// Now one that claims to be a ledger and is not.
	if err := os.WriteFile(filepath.Join(dir, "c.json"), []byte("{not json"), 0o644); err != nil {
		t.Fatalf("writing the broken ledger: %v", err)
	}
	got, err = ReadLedgers(dir)
	if err == nil {
		t.Fatal("an unreadable ledger was skipped silently")
	}
	if !strings.Contains(err.Error(), "c.json") {
		t.Errorf("the error does not name the file: %v", err)
	}
	if len(got) == 0 {
		t.Error("the ledgers read before the broken one were thrown away with it")
	}

	// A directory that was never created is no ledgers, not an error: the first
	// run on a machine has not written one yet.
	none, err := ReadLedgers(filepath.Join(dir, "nothing-here"))
	if err != nil || none != nil {
		t.Errorf("ReadLedgers on a missing directory = %v, %v", none, err)
	}
}

// A JSON file with no run id is not a ledger, however well it parses.
func TestALedgerWithoutARunIdIsRefused(t *testing.T) {
	path := filepath.Join(t.TempDir(), "x.json")
	if err := os.WriteFile(path, []byte(`{"cluster":"https://pve.example.com:8006"}`), 0o644); err != nil {
		t.Fatalf("writing: %v", err)
	}
	if _, err := ReadLedger(path); err == nil {
		t.Fatal("a ledger with no run id was accepted; nothing could be said about which run left it")
	}
}

// Stale means somebody else's unfinished run against this same cluster -- not
// this run, not a finished one, and never another cluster's.
//
// Acting on another cluster's ledger would be reaching into a cluster this run
// was never given, which is the one thing a harness holding hypervisor
// credentials must not do.
func TestStaleLedgersAreOtherUnfinishedRunsOnThisClusterAndNothingElse(t *testing.T) {
	dir := ledgerDir(t)
	e := testEnv()
	closed := time.Now().UTC()
	write := func(rec LedgerRecord) {
		t.Helper()
		raw, _ := json.Marshal(rec)
		if err := os.WriteFile(filepath.Join(dir, rec.RunID+".json"), raw, 0o644); err != nil {
			t.Fatalf("writing %s: %v", rec.RunID, err)
		}
	}
	base := time.Date(2026, 9, 14, 1, 0, 0, 0, time.UTC)
	write(LedgerRecord{RunID: "mine", StartedAt: base, Cluster: e.endpoint})
	write(LedgerRecord{RunID: "older", StartedAt: base.Add(-time.Hour), Cluster: e.endpoint})
	write(LedgerRecord{RunID: "newer", StartedAt: base.Add(time.Hour), Cluster: e.endpoint})
	write(LedgerRecord{RunID: "tidied", StartedAt: base, Cluster: e.endpoint, ClosedAt: &closed})
	write(LedgerRecord{RunID: "elsewhere", StartedAt: base, Cluster: "https://other.example.com:8006"})

	got, err := staleLedgers("mine", e)
	if err != nil {
		t.Fatalf("reading stale ledgers: %v", err)
	}
	var ids []string
	for _, rec := range got {
		ids = append(ids, rec.RunID)
	}
	if strings.Join(ids, ",") != "older,newer" {
		t.Errorf("stale ledgers = %v, want the two unfinished runs on this cluster, oldest first", ids)
	}

	dropLedger("older")
	got, _ = staleLedgers("mine", e)
	if len(got) != 1 || got[0].RunID != "newer" {
		t.Errorf("after dropping one, stale = %v", got)
	}
}

// Outstanding asked of a record read back from disk says the same thing as
// asked of a live ledger -- that is how the sweep and the standalone check ask
// it, and they are the ones asking when the run that wrote it is gone.
func TestOutstandingReadsTheSameFromDiskAsItDoesInMemory(t *testing.T) {
	dir := ledgerDir(t)
	l, _ := openLedger("run_pqr", "label", testEnv(), nil)
	if err := l.created(KindMachine, "mach_1", "one"); err != nil {
		t.Fatalf("recording: %v", err)
	}
	if err := l.placed("mach_1", "pve1", 9042); err != nil {
		t.Fatalf("placing: %v", err)
	}

	rec, err := ReadLedger(filepath.Join(dir, "run_pqr.json"))
	if err != nil {
		t.Fatalf("reading back: %v", err)
	}
	if strings.Join(rec.Outstanding(), "|") != strings.Join(l.outstanding(), "|") {
		t.Errorf("from disk: %v; in memory: %v", rec.Outstanding(), l.outstanding())
	}
}

// The commit is evidence, and a record that cannot name one is still worth
// writing -- so this never fails, it only ever says "unknown".
func TestTheCommitIsAStringEvenWhereThereIsNoGit(t *testing.T) {
	if got := commit(); got == "" {
		t.Error("commit() returned an empty string; the record would claim no revision at all")
	}
}

// Where the ledgers go: the override, then the home directory. Never t.TempDir,
// which is removed at exactly the moment the record becomes useful.
func TestTheLedgerDirectoryIsOverridableAndOtherwiseOutlivesTheRun(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("ZOOMIES_PROXMOX_LEDGER_DIR", dir)
	if got := LedgerDir(); got != dir {
		t.Errorf("LedgerDir() = %q, want the override %q", got, dir)
	}

	t.Setenv("ZOOMIES_PROXMOX_LEDGER_DIR", "")
	got := LedgerDir()
	if !strings.Contains(got, "proxmox-ledgers") {
		t.Errorf("LedgerDir() = %q, want somewhere named for what it holds", got)
	}
	if strings.HasPrefix(got, dir) {
		t.Errorf("LedgerDir() = %q, which is inside the test's own temp directory", got)
	}
}
