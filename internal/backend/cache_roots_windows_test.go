package backend

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The filesystem accepts a differently cased path. Resolving its spelling is
// not evidence of a symlink, and must not disable every Windows cache prune.
func TestCacheRootsAcceptDifferentWindowsPathCasing(t *testing.T) {
	dir := t.TempDir()
	differentlyCased := strings.ToLower(dir)
	root, err := openCacheRoot(differentlyCased)
	if err != nil {
		t.Fatalf("ordinary Windows directory %q was refused: %v", differentlyCased, err)
	}
	defer root.Close()
	if err := os.WriteFile(filepath.Join(dir, "entry"), []byte("cache"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := root.RemoveAll("entry"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "entry")); !os.IsNotExist(err) {
		t.Fatal("rooted eviction did not reach the same Windows directory")
	}
}
