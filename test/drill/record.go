//go:build drill

package drill

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// A record is one drill's row: what was expected, what happened, how long
// recovery took, whether cleanup was clean, and whether a person has to do
// anything.
//
// It is written whatever the outcome, because a drill that failed is the one
// worth reading. The last column is the one the roadmap actually wants: a
// drill that recovers on its own and a drill that needs somebody to go and
// delete a container are different findings, and only the row can say which.
type record struct {
	t         *testing.T
	name      string
	startedAt time.Time
	notes     []string
	expected  string
	// recoveredAt is set by a fault drill when the fleet is working again, so
	// the row can carry a recovery time rather than a total runtime.
	recoveredAt time.Time
	faultAt     time.Time
}

func newRecord(t *testing.T, name string) *record {
	t.Helper()
	return &record{t: t, name: name, startedAt: time.Now()}
}

// note records an observation, in the order they happened, stamped with how
// far into the drill it was.
//
// The elapsed time is not decoration. A drill can pass for the wrong reason --
// a slower backstop tidying up after the path under test failed to -- and the
// first sign of that is a step taking far longer than it should. The row
// carries the timings so that a pass which quietly became a different pass is
// visible.
func (r *record) note(what, value string) {
	r.notes = append(r.notes, fmt.Sprintf("%s @%s: %s", what,
		time.Since(r.startedAt).Round(100*time.Millisecond), value))
}

// pass states what the drill was expected to show. It is called at the end
// rather than the start so that the sentence describes what actually held; a
// drill that fails earlier writes its row without one.
func (r *record) pass(expected string) { r.expected = expected }

// faultInjected and recovered bracket a fault drill's recovery time.
func (r *record) faultInjected(what string) {
	r.faultAt = time.Now()
	r.note("fault injected", what)
}

func (r *record) recovered() { r.recoveredAt = time.Now() }

// write appends the row to the drill record in roadmap/validation/.
//
// One file rather than one per run, because the value of these is the history:
// a drill that has recovered cleanly forty times and then did not is a finding
// that only exists if the forty are there to compare against. Appending is all
// this side does; keeping the file is the workflow's half, and until a run
// commits it the history is one run long.
func (r *record) write() {
	r.t.Helper()
	dir := recordDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		r.t.Errorf("creating the drill record directory: %v", err)
		return
	}
	path := filepath.Join(dir, "drills.md")

	outcome := "passed"
	if r.t.Failed() {
		outcome = "FAILED"
	}
	recovery := "n/a"
	if !r.faultAt.IsZero() {
		if r.recoveredAt.IsZero() {
			recovery = "never"
		} else {
			recovery = r.recoveredAt.Sub(r.faultAt).Round(100 * time.Millisecond).String()
		}
	}
	// A drill that failed has left this machine in whatever state it failed
	// in, and saying so is the point of the column.
	human := "none"
	if r.t.Failed() {
		human = "check for a leftover runner directory under the drill's work dir"
	}
	expected := r.expected
	if expected == "" {
		expected = "(the drill did not reach its own statement of what it proves)"
	}

	row := fmt.Sprintf("| %s | %s | %s | %s | %s | %s | %s | %s | %s |\n",
		time.Now().UTC().Format(time.RFC3339), commit(), runLink(), r.name, outcome,
		expected, strings.Join(r.notes, "; "), recovery, human)

	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		r.t.Errorf("opening the drill record: %v", err)
		return
	}
	defer f.Close()
	if info, err := f.Stat(); err == nil && info.Size() == 0 {
		if _, err := f.WriteString(recordHeader); err != nil {
			r.t.Errorf("writing the drill record header: %v", err)
			return
		}
	}
	if _, err := f.WriteString(row); err != nil {
		r.t.Errorf("writing the drill record row: %v", err)
	}
}

const recordHeader = `# Drill record

Each row is one run of one drill against the built binary, with a fake GitHub
and a real workload on the machine that ran it. Appended rather than replaced:
a drill that has recovered cleanly forty times and then did not is a finding
that only exists if the forty are there to compare against.

**Human action needed** is the column to read first. A drill that recovers on
its own and one that leaves something for a person to delete are different
findings, and only the row says which. **Run** is where the row came from, so
a row that reads oddly a month later can be taken back to the logs that
produced it; a row written on a developer's machine says so instead.

The rows below are appended by the drill job on every push to the default
branch. A pull request's rows stay in that run's job summary: they describe a
commit that may never exist, and the comparison this file is for is the
default branch against itself.

| When | Commit | Run | Drill | Outcome | What it proves | Observed | Recovery | Human action needed |
| --- | --- | --- | --- | --- | --- | --- | --- | --- |
`

func recordDir() string {
	if d := os.Getenv("ZOOMIES_DRILL_RECORD_DIR"); d != "" {
		return d
	}
	return filepath.Join(filepath.Dir(builtBinary()), "roadmap", "validation")
}

// runLink is where this row came from. A row is worth keeping only if it can
// be taken back to the run that wrote it: the observations are a summary, and
// the logs are the rest.
func runLink() string {
	server, repo, id := os.Getenv("GITHUB_SERVER_URL"), os.Getenv("GITHUB_REPOSITORY"), os.Getenv("GITHUB_RUN_ID")
	if server == "" || repo == "" || id == "" {
		return "local"
	}
	return fmt.Sprintf("%s/%s/actions/runs/%s", server, repo, id)
}

func commit() string {
	out, err := exec.Command("git", "rev-parse", "--short", "HEAD").Output()
	if err != nil {
		return "unknown"
	}
	return strings.TrimSpace(string(out))
}
