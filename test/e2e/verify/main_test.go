package main

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func write(t *testing.T, dir, name, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

const passed = `{"scenario":"a","category":"passed","run_id":"r1","commit":"abc"}`

// The whole reason this command exists. `go test` exits zero for reasons that
// have nothing to do with the product -- every scenario skipped, none
// compiled in, a -run filter that matched nothing -- and a gate that reads
// that exit code calls all of them a pass. An empty results directory is the
// shape all of those take on disk.
func TestNoResultsIsNotAPass(t *testing.T) {
	if err := verify(t.TempDir(), io.Discard); err == nil {
		t.Fatal("an empty results directory was accepted; a run that recorded nothing is not a pass")
	}
	if err := verify(filepath.Join(t.TempDir(), "nope"), io.Discard); err == nil {
		t.Fatal("a missing results directory was accepted")
	}
}

// Blocked is the category that exists to stop a missing prerequisite reading
// as success, so it must not read as success here either.
func TestOnlyPassedIsAPass(t *testing.T) {
	for _, category := range []string{"failed", "blocked", "not_run", ""} {
		dir := t.TempDir()
		write(t, dir, "a.json", `{"scenario":"a","category":"`+category+`","reason":"why"}`)
		if err := verify(dir, io.Discard); err == nil {
			t.Errorf("category %q was accepted as a pass", category)
		}
	}
	dir := t.TempDir()
	write(t, dir, "a.json", passed)
	if err := verify(dir, io.Discard); err != nil {
		t.Fatalf("a passed scenario was rejected: %v", err)
	}
}

// A scenario whose assertions all held but which left a runner registered on
// somebody's organisation has not finished. Counting it as a pass is how a
// harness quietly fills an organisation's runner list.
func TestAPassThatLeftLitterIsNotAPass(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "a.json",
		`{"scenario":"a","category":"passed","residual_cleanup":["pool pool_123"]}`)
	var sb strings.Builder
	err := verify(dir, &sb)
	if err == nil {
		t.Fatal("a passed scenario that left resources behind was accepted")
	}
	if !strings.Contains(sb.String(), "LEFT BEHIND") {
		t.Errorf("the report does not say what was left behind:\n%s", sb.String())
	}
}

// One failure among many must sink the run, and the report has to name which.
func TestOneFailureAmongPassesFailsTheRun(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "a.json", `{"scenario":"a","category":"passed"}`)
	write(t, dir, "b.json", `{"scenario":"b","category":"failed","reason":"the runner never registered"}`)
	write(t, dir, "c.json", `{"scenario":"c","category":"passed"}`)
	var sb strings.Builder
	if err := verify(dir, &sb); err == nil {
		t.Fatal("a run containing a failure was accepted")
	}
	if !strings.Contains(sb.String(), "the runner never registered") {
		t.Errorf("the report does not name the failure:\n%s", sb.String())
	}
}

// A file that is not a result is a broken harness, not something to read past:
// silently ignoring it would let a malformed write become a smaller run that
// still passes.
func TestAMalformedResultIsAnError(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "a.json", passed)
	write(t, dir, "b.json", "{not json")
	if err := verify(dir, io.Discard); err == nil {
		t.Fatal("a malformed result file was ignored")
	}
}
