package backend

import (
	"os"
	"path/filepath"
	"testing"
)

func TestNativeLogRetentionPreservesTheOpenWriter(t *testing.T) {
	p := filepath.Join(t.TempDir(), "runner.log")
	f, err := os.OpenFile(p, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if _, err = f.WriteString("old log tail"); err != nil {
		t.Fatal(err)
	}
	if err := boundLog(p, 4); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(p + ".previous")
	if err != nil || string(b) != "tail" {
		t.Fatalf("retained=%q %v", b, err)
	}
	f.WriteString("new")
	b, err = os.ReadFile(p)
	if err != nil || string(b) != "new" {
		t.Fatalf("open writer no longer usable: %q %v", b, err)
	}
}
