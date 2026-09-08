// Command verify reads the end-to-end harness's result files and exits
// non-zero unless every scenario passed.
//
// It exists so that "the gate is green" is a statement about scenarios rather
// than about `go test`'s exit code. A test binary can exit zero for reasons
// that have nothing to do with the product -- every scenario skipped, no
// scenario compiled in, a filter that matched nothing -- and the whole point
// of the required mode is that those are not passes. Reading the records back
// makes the check say what it means: this many scenarios ran, and this is what
// each of them concluded.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type result struct {
	Scenario        string   `json:"scenario"`
	Category        string   `json:"category"`
	Reason          string   `json:"reason"`
	RunID           string   `json:"run_id"`
	Commit          string   `json:"commit"`
	ResidualCleanup []string `json:"residual_cleanup"`
}

func main() {
	dir := flag.String("dir", "roadmap/validation/e2e", "directory holding the scenario result files")
	flag.Parse()

	if err := verify(*dir, os.Stdout); err != nil {
		fmt.Fprintf(os.Stderr, "e2e: %v\n", err)
		os.Exit(1)
	}
}

// verify reads every result in dir, reports each on w, and returns an error
// unless all of them passed cleanly.
func verify(dir string, w io.Writer) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return fmt.Errorf("no results in %s: %w\n"+
			"the harness writes one file per scenario; if it wrote none, it never ran", dir, err)
	}
	var results []result
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			return fmt.Errorf("reading %s: %w", e.Name(), err)
		}
		var r result
		if err := json.Unmarshal(raw, &r); err != nil {
			return fmt.Errorf("%s is not a result file: %w", e.Name(), err)
		}
		results = append(results, r)
	}
	if len(results) == 0 {
		return fmt.Errorf("no scenario results in %s; a run that produced no record is not a pass", dir)
	}
	sort.Slice(results, func(i, j int) bool { return results[i].Scenario < results[j].Scenario })

	var bad int
	for _, r := range results {
		line := fmt.Sprintf("  %-40s %s", r.Scenario, r.Category)
		if r.Reason != "" {
			line += ": " + r.Reason
		}
		// Litter counts against a run even when its assertions held: a
		// scenario that passed and left a runner registered on somebody's
		// organisation has not finished.
		if len(r.ResidualCleanup) > 0 {
			line += fmt.Sprintf("\n    LEFT BEHIND: %s", strings.Join(r.ResidualCleanup, ", "))
			bad++
		} else if r.Category != "passed" {
			bad++
		}
		fmt.Fprintln(w, line)
	}
	if bad > 0 {
		return fmt.Errorf("%d of %d scenarios did not pass cleanly", bad, len(results))
	}
	fmt.Fprintf(w, "all %d scenarios passed\n", len(results))
	return nil
}
