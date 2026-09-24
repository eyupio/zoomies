package docs

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/eyupio/zoomies/internal/config"
)

// Settings live in the database. The settings page writes there and nowhere
// else, and a controller layers them over its file at start -- see
// config.Effective and CLAUDE.md, "Configuration". Code that wants a setting's
// value reads the effective configuration; code that reads a ZOOMIES_*
// variable, or a .env file's copy of one, to learn a setting answers from the
// wrong place, and is wrong the moment an operator has used the page.
//
// It is an easy mistake to make, because every setting has a variable and the
// installer writes some of them, so this test makes it an impossible one: it
// fails on any read of a stored setting's variable outside internal/config --
// os.Getenv, os.LookupEnv, or an index into a map by that name, which is what
// a parsed .env is.
//
// allowedSettingEnvReads are the exceptions, each with the reason it is not a
// setting being read.
var allowedSettingEnvReads = map[string]string{
	// A pool's own env names ZOOMIES_DOCKER_WAIT for its runners; that is the
	// runner container's variable, read from the pool, not this process's
	// setting.
	"internal/backend/docker.go ZOOMIES_DOCKER_WAIT": "a pool's env, passed to its runner",
}

func TestNoCodeReadsASettingFromTheEnvironment(t *testing.T) {
	names := map[string]string{}
	for _, s := range config.Settings() {
		if s.Env != "" && s.Stored() {
			names[s.Env] = s.Key
		}
	}
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	var found []string
	err = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(root, path)
		rel = filepath.ToSlash(rel)
		if d.IsDir() {
			// Test harnesses start processes with an environment on purpose,
			// and internal/config is where the environment layer lives.
			if slices.Contains([]string{".git", "node_modules", "web", "site", "test", "internal/config"}, rel) {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(rel, ".go") || strings.HasSuffix(rel, "_test.go") {
			return nil
		}
		f, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
		if err != nil {
			return err
		}
		ast.Inspect(f, func(n ast.Node) bool {
			var arg ast.Expr
			switch x := n.(type) {
			case *ast.CallExpr:
				if sel, ok := x.Fun.(*ast.SelectorExpr); ok && len(x.Args) > 0 {
					if pkg, ok := sel.X.(*ast.Ident); ok && pkg.Name == "os" && (sel.Sel.Name == "Getenv" || sel.Sel.Name == "LookupEnv") {
						arg = x.Args[0]
					}
				}
			case *ast.IndexExpr:
				arg = x.Index
			}
			lit, ok := arg.(*ast.BasicLit)
			if !ok || lit.Kind != token.STRING {
				return true
			}
			v, _ := strconv.Unquote(lit.Value)
			key, isSetting := names[v]
			if !isSetting {
				return true
			}
			if _, ok := allowedSettingEnvReads[rel+" "+v]; ok {
				return true
			}
			found = append(found, rel+": reads "+v+" -- read "+key+" from the effective configuration (the database, as config.Effective layers it) instead")
			return true
		})
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range found {
		t.Error(f)
	}
}
