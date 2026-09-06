// Package docs holds the tests that keep the documentation site honest about
// the code it describes.
//
// It has no non-test source of its own on purpose: nothing in the product
// imports it, and it exists so that a reference page which claims to be
// complete is checked rather than trusted. A page that goes stale is worse
// than no page, because an operator stops looking for the thing it left out.
package docs

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// The two files that raise problems, and the page that documents them.
const (
	validatorSource  = "../config/validate.go"
	controllerSource = "../controller/problems.go"
	reference        = "../../docs/problem-codes.md"
)

// codesIn returns every string assigned to a `Code:` field in a Go file.
//
// The source is parsed rather than searched, so a code inside a comment or a
// test fixture cannot be mistaken for one the product can emit.
func codesIn(t *testing.T, path string) []string {
	t.Helper()

	file, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.SkipObjectResolution)
	if err != nil {
		t.Fatalf("parsing %s: %v", path, err)
	}

	var out []string
	ast.Inspect(file, func(n ast.Node) bool {
		kv, ok := n.(*ast.KeyValueExpr)
		if !ok {
			return true
		}
		key, ok := kv.Key.(*ast.Ident)
		if !ok || key.Name != "Code" {
			return true
		}
		lit, ok := kv.Value.(*ast.BasicLit)
		if !ok || lit.Kind != token.STRING {
			return true
		}
		code, err := strconv.Unquote(lit.Value)
		if err == nil && code != "" {
			out = append(out, code)
		}
		return true
	})
	return out
}

// Every problem code an operator can be shown has a row explaining it.
//
// The code is the stable half of a problem -- it is what somebody searches
// for, alerts on and quotes in a bug report -- so a new one that reaches the
// startup output or the problems drawer without a row here is a code with no
// answer behind it. Eight of them had exactly that status before this test
// existed.
func TestEveryProblemCodeIsDocumented(t *testing.T) {
	page, err := os.ReadFile(reference)
	if err != nil {
		t.Fatalf("reading %s: %v", filepath.Clean(reference), err)
	}
	text := string(page)

	var missing []string
	seen := map[string]bool{}
	for _, source := range []string{validatorSource, controllerSource} {
		for _, code := range codesIn(t, source) {
			if seen[code] {
				continue
			}
			seen[code] = true
			// Backticked, so a code that only appears inside a sentence
			// about something else does not count as documented.
			if !strings.Contains(text, "`"+code+"`") {
				missing = append(missing, code)
			}
		}
	}

	if len(missing) > 0 {
		t.Errorf("these problem codes are raised but have no row in docs/problem-codes.md:\n  %s",
			strings.Join(missing, "\n  "))
	}
	if len(seen) == 0 {
		t.Fatal("no codes were found at all; the parser or the paths are wrong, not the docs")
	}
}

// And the reverse: the page does not describe codes that no longer exist.
//
// A row for a code that was renamed or removed sends somebody looking for a
// setting that is not there, which is the more confusing of the two failures.
func TestTheReferenceDescribesNoCodeThatIsGone(t *testing.T) {
	page, err := os.ReadFile(reference)
	if err != nil {
		t.Fatalf("reading %s: %v", filepath.Clean(reference), err)
	}

	real := map[string]bool{}
	for _, source := range []string{validatorSource, controllerSource} {
		for _, code := range codesIn(t, source) {
			real[code] = true
		}
	}

	// A code is `word.word` inside backticks at the start of a table row.
	var stale []string
	for _, line := range strings.Split(string(page), "\n") {
		if !strings.HasPrefix(line, "| `") {
			continue
		}
		code := line[3:]
		end := strings.Index(code, "`")
		if end < 0 {
			continue
		}
		code = code[:end]
		if strings.Contains(code, ".") && !real[code] {
			stale = append(stale, code)
		}
	}

	if len(stale) > 0 {
		t.Errorf("docs/problem-codes.md documents codes nothing raises any more:\n  %s",
			strings.Join(stale, "\n  "))
	}
}
