//go:build drill

package drill

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The rows are the deliverable, and a row is only useful if it can be taken
// back to the run that wrote it: what the file holds is a summary, and the
// logs are the rest of the story.
func TestARowSaysWhichRunWroteIt(t *testing.T) {
	t.Setenv("GITHUB_SERVER_URL", "https://github.com")
	t.Setenv("GITHUB_REPOSITORY", "eyupio/zoomies")
	t.Setenv("GITHUB_RUN_ID", "12345")

	if got := runLink(); got != "https://github.com/eyupio/zoomies/actions/runs/12345" {
		t.Fatalf("run link = %q, want the run's address", got)
	}
}

// A drill run on a developer's machine has no run to point at, and a link to
// nowhere would be worse than saying so.
func TestARowWrittenOffCISaysSo(t *testing.T) {
	t.Setenv("GITHUB_SERVER_URL", "https://github.com")
	t.Setenv("GITHUB_REPOSITORY", "eyupio/zoomies")
	t.Setenv("GITHUB_RUN_ID", "")

	if got := runLink(); got != "local" {
		t.Fatalf("run link = %q off CI, want it to say the row is local", got)
	}
}

// Appending is the whole design: the value of these rows is the comparison,
// and a writer that replaced the file would leave one run's worth of history
// however many times it ran.
func TestASecondRowLeavesTheFirstWhereItIs(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("ZOOMIES_DRILL_RECORD_DIR", dir)

	first := newRecord(t, "first")
	first.pass("the first thing")
	first.write()
	second := newRecord(t, "second")
	second.pass("the second thing")
	second.write()

	body, err := os.ReadFile(filepath.Join(dir, "drills.md"))
	if err != nil {
		t.Fatalf("reading the record: %v", err)
	}
	if !strings.Contains(string(body), "| first |") {
		t.Fatalf("the first drill's row is gone after a second one was written:\n%s", body)
	}
	if !strings.Contains(string(body), "| second |") {
		t.Fatalf("the second drill's row was not written:\n%s", body)
	}
	if n := strings.Count(string(body), "# Drill record"); n != 1 {
		t.Fatalf("the record carries its header %d times, want once", n)
	}
}

// A row with more cells than the header renders as a table with a column
// nobody can read the name of, and the file is read by people rather than by
// a parser -- so the two have to agree, in the writer and in what it has
// already written.
func TestEveryRowFitsTheHeader(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("ZOOMIES_DRILL_RECORD_DIR", dir)
	fresh := newRecord(t, "fresh")
	fresh.pass("that a row and its header agree")
	fresh.write()

	written, err := os.ReadFile(filepath.Join(dir, "drills.md"))
	if err != nil {
		t.Fatalf("reading the record: %v", err)
	}
	for _, path := range []string{"", filepath.Join("..", "..", "roadmap", "validation", "drills.md")} {
		body := string(written)
		name := "a record this test wrote"
		if path != "" {
			b, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("reading %s: %v", path, err)
			}
			body, name = string(b), path
		}
		want := -1
		for _, line := range strings.Split(body, "\n") {
			if !strings.HasPrefix(line, "| ") || strings.HasPrefix(line, "| --- ") {
				continue
			}
			cells := strings.Count(line, " | ")
			if want < 0 {
				want = cells
				continue
			}
			if cells != want {
				t.Errorf("%s has a row with %d separators where the header has %d:\n%s", name, cells, want, line)
			}
		}
		if want < 0 {
			t.Errorf("%s has no table in it at all", name)
		}
	}
}
