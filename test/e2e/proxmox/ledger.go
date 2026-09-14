package proxmox

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// A ledger is the list of real things one run is about to create, written to
// disk before each of them exists and closed only when they have all been
// removed.
//
// It is the same discipline the machine row has, for the same reason. A machine
// row exists before its virtual machine does, because a create whose outcome is
// unknown can only be reconciled by an identity that was persisted first; a run
// killed between the clone and the row would otherwise own a VM it has no record
// of. This harness is in exactly that position with respect to a whole cluster:
// if it dies between asking for demand and seeing the machine, something it
// caused still exists and nothing names it.
//
// So the order is: claim the blast radius first -- the cluster, the node, the
// storage, the VMID range and everything already in that range -- and only then
// do anything that could cause a guest to be built. A ledger with no machines in
// it still tells a person exactly where to look, which is the answer that
// matters when the harness died before it could say anything else.
//
// It lives outside the test's temp directory on purpose. t.TempDir is removed
// when the test ends however it ends, which is exactly the moment the record of
// what was not cleaned up becomes useful.
type Ledger struct {
	path string
	rec  LedgerRecord
}

// ResourceKind is the sort of thing a ledger entry names.
type ResourceKind string

const (
	KindInstallation ResourceKind = "installation"
	KindPool         ResourceKind = "pool"
	KindProvider     ResourceKind = "provider"
	// KindMachine is a machine row and, once the provider has allocated one,
	// the guest behind it. The two are one entry because they are one thing to
	// clean up: deleting the row without the guest is how an orphan is made.
	KindMachine ResourceKind = "machine"
)

// LedgerRecord is one run's whole record, as it is written to disk.
type LedgerRecord struct {
	RunID     string    `json:"run_id"`
	StartedAt time.Time `json:"started_at"`
	Commit    string    `json:"commit"`
	// Label is the per-run label this run's pools advertise and its workflows
	// ask for. It is how leftovers are recognised on GitHub and how two
	// concurrent runs stay out of each other's way.
	Label string `json:"label"`

	// The blast radius, written before anything is created.
	Cluster string `json:"cluster"`
	Node    string `json:"node"`
	Storage string `json:"storage"`
	Range   Range  `json:"vmid_range"`
	// PreexistingVMIDs is everything that was in the range before this run
	// began. Without it a guest found at the end cannot be told from one that
	// was always there, and the reconciliation would either accuse the cluster's
	// owner or excuse this run.
	PreexistingVMIDs []int `json:"preexisting_vmids"`

	// Target and TargetType name where on GitHub the leavings would be.
	Target     string `json:"target,omitempty"`
	TargetType string `json:"target_type,omitempty"`

	Resources []Resource `json:"resources"`
	// ClosedAt is set only when every resource has been removed. An open ledger
	// is a run that did not finish tidying up, whether it crashed, was killed,
	// or failed its cleanup.
	ClosedAt *time.Time `json:"closed_at,omitempty"`
}

// Resource is one thing this run made.
type Resource struct {
	Kind      ResourceKind `json:"kind"`
	ID        string       `json:"id"`
	Name      string       `json:"name,omitempty"`
	CreatedAt time.Time    `json:"created_at"`
	// Node and VMID are filled in when the provider allocates them, which is
	// after the row exists. A machine entry with no VMID is a machine that was
	// planned and may never have reached the cluster -- and the range on the
	// record is what says where to look for it anyway.
	Node      string     `json:"node,omitempty"`
	VMID      int        `json:"vmid,omitempty"`
	RemovedAt *time.Time `json:"removed_at,omitempty"`
}

// LedgerDir is where ledgers live. It must survive the test process, so it is
// never t.TempDir.
func LedgerDir() string {
	if d := os.Getenv("ZOOMIES_PROXMOX_LEDGER_DIR"); d != "" {
		return d
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join(os.TempDir(), "zoomies-proxmox-ledgers")
	}
	return filepath.Join(home, ".zoomies", "proxmox-ledgers")
}

// openLedger claims this run's blast radius, before anything is created.
func openLedger(runID, label string, e env, preexisting []int) (*Ledger, error) {
	dir := LedgerDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("creating the ledger directory %s: %w", dir, err)
	}
	l := &Ledger{
		path: filepath.Join(dir, runID+".json"),
		rec: LedgerRecord{
			RunID: runID, StartedAt: time.Now().UTC(), Commit: commit(), Label: label,
			Cluster: e.endpoint, Node: e.node, Storage: e.storage,
			Range:            Range{Min: e.vmidMin, Max: e.vmidMax},
			PreexistingVMIDs: preexisting,
			Target:           e.target, TargetType: e.targetType,
		},
	}
	return l, l.save()
}

func (l *Ledger) save() error {
	raw, err := json.MarshalIndent(l.rec, "", "  ")
	if err != nil {
		return fmt.Errorf("encoding the ledger: %w", err)
	}
	return os.WriteFile(l.path, append(raw, '\n'), 0o644)
}

// created records a resource. Its error is fatal to the caller: a resource that
// exists without a ledger entry is one nothing will ever clean up.
func (l *Ledger) created(kind ResourceKind, id, name string) error {
	l.rec.Resources = append(l.rec.Resources, Resource{
		Kind: kind, ID: id, Name: name, CreatedAt: time.Now().UTC(),
	})
	return l.save()
}

// placed fills in where a machine ended up. It is separate from created because
// the row exists before the provider has chosen a node and a VMID, and the
// ledger must not wait for them: the window between the row and the identifier
// is exactly the window in which a run can die owning something it cannot name.
func (l *Ledger) placed(id, node string, vmid int) error {
	for i := range l.rec.Resources {
		if l.rec.Resources[i].ID == id {
			l.rec.Resources[i].Node, l.rec.Resources[i].VMID = node, vmid
			return l.save()
		}
	}
	return fmt.Errorf("no ledger entry for %s, so its resource %s/%d would go unrecorded", id, node, vmid)
}

// removed marks a resource gone. It is called when the removal has been
// confirmed, never when it has merely been asked for.
func (l *Ledger) removed(kind ResourceKind, id string) {
	now := time.Now().UTC()
	for i := range l.rec.Resources {
		if l.rec.Resources[i].Kind == kind && l.rec.Resources[i].ID == id && l.rec.Resources[i].RemovedAt == nil {
			l.rec.Resources[i].RemovedAt = &now
		}
	}
	_ = l.save()
}

// outstanding is what this run created and has not removed, in a sentence each.
func (l *Ledger) outstanding() []string { return l.rec.Outstanding() }

// Outstanding is the same question asked of a record read back from disk, which
// is how the sweep and the standalone check ask it.
func (r LedgerRecord) Outstanding() []string {
	var out []string
	for _, res := range r.Resources {
		if res.RemovedAt != nil {
			continue
		}
		line := string(res.Kind) + " " + res.ID
		if res.VMID != 0 {
			line += fmt.Sprintf(" (VM %d on %s)", res.VMID, res.Node)
		}
		out = append(out, line)
	}
	return out
}

// close marks the ledger finished and deletes it. A ledger is only worth keeping
// while it describes something still on the cluster; one whose resources are all
// gone is noise the next sweep would have to read past.
func (l *Ledger) close() error {
	if rem := l.outstanding(); len(rem) > 0 {
		return fmt.Errorf("not closing the ledger: %s still exist", strings.Join(rem, ", "))
	}
	now := time.Now().UTC()
	l.rec.ClosedAt = &now
	if err := l.save(); err != nil {
		return err
	}
	return os.Remove(l.path)
}

// record is the ledger as it stands, for the evidence and the reconciliation.
func (l *Ledger) record() LedgerRecord { return l.rec }

// ReadLedgers returns every ledger in a directory, oldest first. The standalone
// inventory check reads them rather than a database, because the database that
// knew about these machines went away with the controller that wrote it.
func ReadLedgers(dir string) ([]LedgerRecord, error) {
	entries, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out []LedgerRecord
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		rec, err := ReadLedger(filepath.Join(dir, entry.Name()))
		if err != nil {
			// A file that is not a ledger is reported rather than skipped: a
			// half-written one is the shape a killed run leaves, and reading
			// past it silently is how its machines get forgotten.
			return out, fmt.Errorf("%s is not a ledger: %w", entry.Name(), err)
		}
		out = append(out, rec)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].StartedAt.Before(out[j].StartedAt) })
	return out, nil
}

// ReadLedger reads one.
func ReadLedger(path string) (LedgerRecord, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return LedgerRecord{}, err
	}
	var rec LedgerRecord
	if err := json.Unmarshal(raw, &rec); err != nil {
		return LedgerRecord{}, err
	}
	if rec.RunID == "" {
		return LedgerRecord{}, fmt.Errorf("no run id")
	}
	return rec, nil
}

// staleLedgers returns the open ledgers left by earlier runs against this same
// cluster and range, oldest first. An open ledger is by definition a run that
// did not finish tidying up.
func staleLedgers(runID string, e env) ([]LedgerRecord, error) {
	all, err := ReadLedgers(LedgerDir())
	if err != nil {
		return nil, err
	}
	var out []LedgerRecord
	for _, rec := range all {
		// This run's own ledger is not stale, and neither is one describing
		// somebody else's cluster: acting on that would be reaching into a
		// cluster this run was never given.
		if rec.RunID == runID || rec.ClosedAt != nil || rec.Cluster != e.endpoint {
			continue
		}
		out = append(out, rec)
	}
	return out, nil
}

// dropLedger removes a stale ledger once its leavings have been dealt with.
func dropLedger(runID string) {
	_ = os.Remove(filepath.Join(LedgerDir(), runID+".json"))
}

// commit is the revision under test. A record that cannot name its commit is
// still worth writing, so a failure here becomes the string rather than an
// error.
func commit() string {
	out, err := exec.Command("git", "rev-parse", "--short", "HEAD").Output()
	if err != nil {
		return "unknown"
	}
	return strings.TrimSpace(string(out))
}
