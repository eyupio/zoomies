package docs

import (
	"os"
	"os/exec"
	"sort"
	"strings"
	"testing"
)

// trackedFiles lists what git tracks under the repository root, slash
// separated and relative to it. A scratch file in a working tree is not part of
// the repository, and a test about the repository's shape should not fail for
// one.
func trackedFiles(t *testing.T) []string {
	t.Helper()
	out, err := exec.Command("git", "-C", "../..", "ls-files", "-z").Output()
	if err != nil {
		t.Fatalf("listing tracked files: %v", err)
	}
	var files []string
	for _, f := range strings.Split(strings.TrimRight(string(out), "\x00"), "\x00") {
		if f != "" {
			files = append(files, f)
		}
	}
	return files
}

// rootDocuments is every Markdown file the repository root may hold. The root
// is the first thing a visitor reads on GitHub, and a planning note that landed
// there while it was being written stays there for months unless something
// says it may not: three did, and each had a place in roadmap/ that its own
// header named.
//
// Adding a name here is a decision that the file belongs in front of every
// visitor. A planning document does not; it belongs in roadmap/ with a line in
// roadmap/README.md. The one planning document that still lives at the root is
// the live Eli handoff, which progress.md and the agents working from it link
// to at this path.
var rootDocuments = map[string]bool{
	"AGENTS.md":          true,
	"CLAUDE.md":          true,
	"CODE_OF_CONDUCT.md": true,
	"CONTRIBUTING.md":    true,
	// The durable product and design records, read from the root by the
	// design tooling. They are context, not planning notes.
	"DESIGN.md":                          true,
	"PRODUCT.md":                         true,
	"GLOSSARY.md":                        true,
	"README.md":                          true,
	"RELEASING.md":                       true,
	"ROADMAP.md":                         true,
	"SECURITY.md":                        true,
	"SUPPORT.md":                         true,
	"eli-enhancement-roadmap-handoff.md": true,
}

func TestTheRepositoryRootHoldsOnlyTheDocumentsItIsMeantTo(t *testing.T) {
	for _, f := range trackedFiles(t) {
		if strings.Contains(f, "/") || !strings.HasSuffix(strings.ToLower(f), ".md") {
			continue
		}
		if !rootDocuments[f] {
			t.Errorf("%s is a Markdown file at the repository root that is not on the list in planning_docs_test.go; a planning or audit document belongs in roadmap/ (finished ones in roadmap/archive/) with a line in roadmap/README.md, so that the root stays what a visitor needs", f)
		}
	}
}

// The roadmap folder holds fifty documents of five kinds, and the only thing
// that tells a reader which one answers their question is roadmap/README.md. A
// file that is not in it is invisible: nobody knows whether it is current,
// finished or a draft, and the next person writes a duplicate.
func TestEveryRoadmapFileIsIndexed(t *testing.T) {
	b, err := os.ReadFile("../../roadmap/README.md")
	if err != nil {
		t.Fatal(err)
	}
	index := string(b)

	children := map[string]bool{}
	for _, f := range trackedFiles(t) {
		rest, ok := strings.CutPrefix(f, "roadmap/")
		if !ok {
			continue
		}
		name, _, nested := strings.Cut(rest, "/")
		if nested {
			name += "/"
		}
		children[name] = true
	}
	var names []string
	for name := range children {
		names = append(names, name)
	}
	sort.Strings(names)

	for _, name := range names {
		if name == "README.md" {
			continue
		}
		if !strings.Contains(index, "]("+name+")") {
			t.Errorf("roadmap/%s is not linked from roadmap/README.md; say which question it answers, or whether it is a record, so a reader can tell it from the current documents", name)
		}
	}
}
