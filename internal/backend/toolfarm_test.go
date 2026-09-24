package backend

import (
	"os"
	"path/filepath"
	"testing"
)

// Two runners filling the same writable tool cache left a .complete marker
// beside half a Go, and every later job trusted it. A runner now sees only
// versions whose install finished, as links into the read-only kept cache,
// and has a folder of its own to download anything else into.
func TestARunnersToolCacheLinksOnlyFinishedVersions(t *testing.T) {
	requirePOSIX(t)
	shared := t.TempDir()
	for _, p := range []string{"go/1.27.1/x64", "node/22.23.3/x64"} {
		if err := os.MkdirAll(filepath.Join(shared, p), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(shared, "go/1.27.1/x64.complete"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	farm := filepath.Join(t.TempDir(), "run-one")
	if err := os.MkdirAll(filepath.Join(farm, "stale"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := buildToolFarm(shared, farm); err != nil {
		t.Fatalf("buildToolFarm: %v", err)
	}

	target, err := os.Readlink(filepath.Join(farm, "go/1.27.1/x64"))
	if err != nil {
		t.Fatalf("finished Go is not linked: %v", err)
	}
	if want := RunnerToolCacheSharedMount + "/go/1.27.1/x64"; target != want {
		t.Errorf("link = %q, want %q: where the kept cache is mounted in the runner", target, want)
	}
	if _, err := os.Stat(filepath.Join(farm, "go/1.27.1/x64.complete")); err != nil {
		t.Errorf("finished Go has no marker, so setup-go would download it again: %v", err)
	}
	if _, err := os.Lstat(filepath.Join(farm, "node")); !os.IsNotExist(err) {
		t.Errorf("an unfinished Node was handed to the runner: %v", err)
	}
	if _, err := os.Stat(filepath.Join(farm, "stale")); !os.IsNotExist(err) {
		t.Errorf("a previous runner's leftovers survived: %v", err)
	}
	for _, d := range []string{farm, filepath.Join(farm, "go"), filepath.Join(farm, "go/1.27.1")} {
		if fi, _ := os.Stat(d); fi.Mode().Perm() != 0o777 {
			t.Errorf("%s mode = %v, want writable by the runner, whatever its uid", d, fi.Mode().Perm())
		}
	}
}
