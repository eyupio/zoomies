package cacheservice

import (
	"go/parser"
	gotoken "go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// This package is the cache protocol and nothing about the fleet, so that it
// can be tested against GitHub's own client without a database, a controller
// or a clock, and so that a host's cache cannot come to depend on them. The
// rule is held by reading the imports of its shipped files.
func TestTheCacheServiceImportsOnlyTheStandardLibrary(t *testing.T) {
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range files {
		if strings.HasSuffix(name, "_test.go") {
			continue
		}
		src, err := os.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		f, err := parser.ParseFile(gotoken.NewFileSet(), name, src, parser.ImportsOnly)
		if err != nil {
			t.Fatal(err)
		}
		for _, imp := range f.Imports {
			path := strings.Trim(imp.Path.Value, `"`)
			first, _, _ := strings.Cut(path, "/")
			if strings.Contains(first, ".") {
				t.Errorf("%s imports %s: the cache service may use only the standard library, because it is the protocol and nothing about the fleet", name, path)
			}
		}
	}
}
