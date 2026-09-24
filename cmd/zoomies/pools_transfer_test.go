package main

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const testPoolsYAML = "# Zoomies pools\nexport_version: 1\npools:\n  - name: zoomies-builders\n    installation: acme\n"

// The file is the server's document byte for byte, comment included: it is
// meant to be committed, and a CLI that re-rendered it would drop the header
// that says where it came from.
func TestPoolsExportWritesTheServersDocument(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/pools/export" || r.URL.Query().Get("format") != "yaml" {
			t.Errorf("the CLI asked for %s", r.URL)
		}
		_, _ = w.Write([]byte(testPoolsYAML))
	}))
	t.Cleanup(srv.Close)
	path := filepath.Join(t.TempDir(), "pools.yaml")

	e, out, errOut := newTestEnv(t)
	if code := dispatch(context.Background(), e, []string{"pools", "export", "--url", srv.URL, "--file", path}); code != exitOK {
		t.Fatalf("exit code = %d\n%s", code, errOut)
	}
	written, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(written) != testPoolsYAML {
		t.Errorf("wrote %q", written)
	}
	if !strings.Contains(out.String(), path) {
		t.Errorf("the terminal does not name the file it wrote:\n%s", out)
	}
}

// An import sends the file and the operator's choices, and prints the plan
// in words: which pools, which settings, and whether anything was written.
func TestPoolsImportSendsTheDocumentAndPrintsThePlan(t *testing.T) {
	var sent map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/pools/import" || r.Method != http.MethodPost {
			t.Errorf("the CLI asked for %s %s", r.Method, r.URL)
		}
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &sent)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"applied": false, "summary": {"create": 0, "change": 1, "unchanged": 0, "refused": 1, "skipped": 1},
			"changes": [
			  {"pool": "zoomies-builders", "action": "change", "fields": [{"field": "max_runners", "current": 11, "incoming": 7}], "warnings": [], "env_keys": ["REGISTRY_PASSWORD"]},
			  {"pool": "zoomies-elsewhere", "action": "refused", "reason": "no installation on this instance covers globex", "fields": [], "warnings": [], "env_keys": []}
			]}`))
	}))
	t.Cleanup(srv.Close)
	path := filepath.Join(t.TempDir(), "pools.yaml")
	if err := os.WriteFile(path, []byte(testPoolsYAML), 0o600); err != nil {
		t.Fatal(err)
	}

	e, out, errOut := newTestEnv(t)
	args := []string{"pools", "import", path, "--url", srv.URL, "--dry-run", "--skip", "zoomies-legacy, zoomies-old"}
	if code := dispatch(context.Background(), e, args); code != exitOK {
		t.Fatalf("exit code = %d\n%s", code, errOut)
	}
	if sent["document"] != testPoolsYAML || sent["dry_run"] != true {
		t.Errorf("sent %v", sent)
	}
	if skip, _ := sent["skip"].([]any); len(skip) != 2 || skip[1] != "zoomies-old" {
		t.Errorf("skip = %v", sent["skip"])
	}
	for _, want := range []string{"max_runners: 11 -> 7", "no installation on this instance covers globex",
		"set by hand: REGISTRY_PASSWORD", "Fix the refused pools or --skip them"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("the plan does not say %q:\n%s", want, out)
		}
	}
}
