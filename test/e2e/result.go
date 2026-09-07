//go:build e2e

package e2e

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// Category is how one scenario ended, and it is the only vocabulary a run
// reports in.
//
// The distinction that matters is between NotRun and Blocked. Before this, a
// missing prerequisite was a skip and a skip is exit code zero, so a harness
// that had never once talked to GitHub reported the same green as one that had
// run the whole scenario. Blocked says "this could not start, and that is a
// finding"; NotRun says "nobody asked it to". Only Passed is a pass.
type Category string

const (
	// Passed: the scenario ran and every assertion held.
	Passed Category = "passed"
	// Failed: the scenario ran and something did not hold.
	Failed Category = "failed"
	// NotRun: nothing was attempted, because this run was not asked to.
	NotRun Category = "not_run"
	// Blocked: the scenario was asked for and could not start, because a
	// prerequisite was missing. In required mode this is a failure.
	Blocked Category = "blocked"
)

// Result is the record one scenario writes, whatever happens to it. It is
// written even when the scenario never started, because "we do not know" is
// the answer a gate needs to be able to read.
type Result struct {
	Scenario string   `json:"scenario"`
	Category Category `json:"category"`
	// Reason says why, in a sentence a person can act on. Empty only when the
	// scenario passed.
	Reason string `json:"reason,omitempty"`
	// Commit is the revision under test, so a result file found later can be
	// tied to the code it was produced by.
	Commit string `json:"commit"`
	// RunID is this run's identity, and the suffix on every label and name it
	// creates. It is what makes two concurrent runs safe and what a sweep uses
	// to recognise the leavings of a run that never finished.
	RunID string `json:"run_id"`
	// RunURL is the workflow run on GitHub, when there was one to link to.
	RunURL string `json:"run_url,omitempty"`
	// Marker is the value this run dispatched the workflow with, and asserted
	// on the job that came back. Without it the scenario could pass on a run
	// somebody else triggered.
	Marker     string    `json:"marker,omitempty"`
	StartedAt  time.Time `json:"started_at"`
	FinishedAt time.Time `json:"finished_at"`
	// ResidualCleanup is what this run created and could not remove. Empty is
	// the only good value; anything in it is somebody's real organisation
	// carrying this test's litter.
	ResidualCleanup []string `json:"residual_cleanup,omitempty"`
	// SweptFromEarlierRuns is what the start-up sweep found and cleared before
	// this run began. It is reported rather than silently tidied, because a
	// sweep that keeps finding things is a harness that keeps crashing.
	SweptFromEarlierRuns []string `json:"swept_from_earlier_runs,omitempty"`
}

// write saves the result as JSON, one file per scenario per run.
func (r *Result) write(dir string) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("creating the results directory: %w", err)
	}
	raw, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return fmt.Errorf("encoding the result: %w", err)
	}
	name := fmt.Sprintf("%s-%s.json", r.Scenario, r.RunID)
	return os.WriteFile(filepath.Join(dir, name), append(raw, '\n'), 0o644)
}

// commit is the revision under test. A result that cannot name its commit is
// still worth writing, so a failure here becomes the string rather than an
// error: the alternative is losing the whole record to a missing git.
func commit() string {
	out, err := exec.Command("git", "rev-parse", "--short", "HEAD").Output()
	if err != nil {
		return "unknown"
	}
	return strings.TrimSpace(string(out))
}
