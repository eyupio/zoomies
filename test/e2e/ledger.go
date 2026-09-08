//go:build e2e

package e2e

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// A ledger is the list of real things one run has created, written to disk
// before each is created and closed only when they have all been removed.
//
// It exists because the resources are somebody's: a GitHub App installation
// and a pool on a real organisation, and runners registered against it. The
// previous harness deleted them in its last step, so any failure before that
// step -- including a timeout, which it was guaranteed to hit -- left them
// behind with nothing recording that they existed.
//
// It lives outside the test's temp directory on purpose. t.TempDir is removed
// when the test ends however it ends, which is exactly the moment the record
// of what was not cleaned up becomes useful. It is also the reason the sweep
// can work at all: a later run reads these files, not a database that went
// away with the process that wrote it.
type ledger struct {
	path string
	rec  ledgerRecord
}

type ledgerRecord struct {
	RunID     string    `json:"run_id"`
	StartedAt time.Time `json:"started_at"`
	Commit    string    `json:"commit"`
	// Label is the per-run label this run's pool advertises and its workflow
	// asks for. It is how the sweep recognises a stale run's runners on
	// GitHub, and how two concurrent runs stay out of each other's way.
	Label string `json:"label"`
	// Target and TargetType name where on GitHub the leavings would be.
	Target     string `json:"target"`
	TargetType string `json:"target_type"`
	// Resources are what this run made, appended as each is created.
	Resources []resource `json:"resources"`
	// ClosedAt is set only when every resource has been removed. An open
	// ledger is a run that did not finish tidying up, whether it crashed, was
	// killed, or failed its cleanup.
	ClosedAt *time.Time `json:"closed_at,omitempty"`
}

type resource struct {
	Kind      string     `json:"kind"` // installation | pool
	ID        string     `json:"id"`
	CreatedAt time.Time  `json:"created_at"`
	RemovedAt *time.Time `json:"removed_at,omitempty"`
}

// ledgerDir is where ledgers live. It must survive the test process, so it is
// never t.TempDir.
func ledgerDir() string {
	if d := os.Getenv("ZOOMIES_E2E_LEDGER_DIR"); d != "" {
		return d
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join(os.TempDir(), "zoomies-e2e-ledgers")
	}
	return filepath.Join(home, ".zoomies", "e2e-ledgers")
}

// openLedger creates a run's ledger before anything is created.
func openLedger(runID, label string, e env) (*ledger, error) {
	dir := ledgerDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("creating the ledger directory %s: %w", dir, err)
	}
	l := &ledger{
		path: filepath.Join(dir, runID+".json"),
		rec: ledgerRecord{
			RunID: runID, StartedAt: time.Now(), Commit: commit(),
			Label: label, Target: e.target, TargetType: e.targetType,
		},
	}
	return l, l.save()
}

func (l *ledger) save() error {
	raw, err := json.MarshalIndent(l.rec, "", "  ")
	if err != nil {
		return fmt.Errorf("encoding the ledger: %w", err)
	}
	return os.WriteFile(l.path, append(raw, '\n'), 0o644)
}

// created records a resource. It is called immediately after the thing exists,
// and its error is fatal to the caller: a resource that exists without a
// ledger entry is one nothing will ever clean up.
func (l *ledger) created(kind, id string) error {
	l.rec.Resources = append(l.rec.Resources, resource{Kind: kind, ID: id, CreatedAt: time.Now()})
	return l.save()
}

func (l *ledger) removed(kind, id string) {
	now := time.Now()
	for i := range l.rec.Resources {
		if l.rec.Resources[i].Kind == kind && l.rec.Resources[i].ID == id {
			l.rec.Resources[i].RemovedAt = &now
		}
	}
	_ = l.save()
}

// outstanding is what this run created and has not removed.
func (l *ledger) outstanding() []string {
	var out []string
	for _, r := range l.rec.Resources {
		if r.RemovedAt == nil {
			out = append(out, r.Kind+" "+r.ID)
		}
	}
	return out
}

// close marks the ledger finished and deletes it. A ledger is only worth
// keeping while it describes something still on GitHub; one whose resources
// are all gone is noise the next sweep would have to read past.
func (l *ledger) close() error {
	if rem := l.outstanding(); len(rem) > 0 {
		return fmt.Errorf("not closing the ledger: %s still exist", strings.Join(rem, ", "))
	}
	now := time.Now()
	l.rec.ClosedAt = &now
	if err := l.save(); err != nil {
		return err
	}
	return os.Remove(l.path)
}

// staleLedgers returns the open ledgers left by earlier runs against this same
// target, oldest first. An open ledger is by definition a run that did not
// finish tidying up.
func staleLedgers(runID string, e env) ([]ledgerRecord, error) {
	entries, err := os.ReadDir(ledgerDir())
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out []ledgerRecord
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(ledgerDir(), entry.Name()))
		if err != nil {
			continue
		}
		var rec ledgerRecord
		if json.Unmarshal(raw, &rec) != nil {
			continue
		}
		// This run's own ledger is not stale, and neither is one describing
		// somebody else's organisation: cleaning that would be reaching into a
		// target this run was never given.
		if rec.RunID == runID || rec.ClosedAt != nil || rec.Target != e.target {
			continue
		}
		out = append(out, rec)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].StartedAt.Before(out[j].StartedAt) })
	return out, nil
}

// dropLedger removes a stale ledger once its leavings have been dealt with.
func dropLedger(runID string) {
	_ = os.Remove(filepath.Join(ledgerDir(), runID+".json"))
}
