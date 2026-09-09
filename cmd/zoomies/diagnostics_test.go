package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The bundle body a test controller answers with. It is deliberately not the
// shape this CLI declares: the extra section is there to prove the document is
// written through rather than re-marshalled from what the CLI understood.
const testBundle = `{
  "bundle_version": 1,
  "generated_at": "2026-01-02T03:04:05Z",
  "instance": {"version": "v1.2.3", "os": "linux", "arch": "amd64", "goroutines": 42},
  "problems": {"ok": false, "items": [{"code": "hosts.unhealthy", "title": "A host is silent"}]},
  "pools": [{"id": "pool_a"}],
  "hosts": [{"id": "hst_a"}],
  "a_section_this_cli_has_never_heard_of": {"kept": true},
  "logs": {"note": "not here", "runners": [{"runner_id": "run_a"}]},
  "errors": [{"section": "stats", "error": "the database is locked"}],
  "truncated": [{"section": "runners", "kept": 500, "reason": "the fleet has more"}]
}`

func bundleServer(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/diagnostics/bundle" {
			t.Errorf("the CLI asked for %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(testBundle))
	}))
	t.Cleanup(srv.Close)
	return srv
}

// The document written to disk is the server's, byte for byte. That is what
// lets a section added to the bundle tomorrow reach a support case without
// this CLI being rebuilt -- and re-marshalling from the partial type the CLI
// declares would silently drop exactly the section nobody has support for yet.
func TestTheBundleFileIsTheServersDocument(t *testing.T) {
	srv := bundleServer(t)
	path := filepath.Join(t.TempDir(), "bundle.json")

	e, out, errOut := newTestEnv(t)
	if code := dispatch(context.Background(), e, []string{"diagnostics", "--url", srv.URL, "--file", path}); code != exitOK {
		t.Fatalf("exit code = %d\n%s", code, errOut)
	}

	written, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading the bundle: %v", err)
	}
	var got map[string]any
	if err := json.Unmarshal(written, &got); err != nil {
		t.Fatalf("the written bundle is not JSON: %v", err)
	}
	if _, ok := got["a_section_this_cli_has_never_heard_of"]; !ok {
		t.Error("a section the CLI does not declare was dropped on the way to the file")
	}
	if !strings.Contains(out.String(), path) {
		t.Errorf("the terminal does not name the file it wrote:\n%s", out)
	}
}

// An operator about to attach a bundle to a public issue has one question, and
// it is what they are handing over. A short section is the other half: a reader
// who cannot tell a section that failed from a fleet that has nothing in it
// will draw the wrong conclusion from the same document.
func TestTheSummarySaysWhatWentInAndWhatDidNot(t *testing.T) {
	srv := bundleServer(t)

	e, out, errOut := newTestEnv(t)
	if code := dispatch(context.Background(), e, []string{"diagnostics", "--url", srv.URL, "--file", filepath.Join(t.TempDir(), "b.json")}); code != exitOK {
		t.Fatalf("exit code = %d\n%s", code, errOut)
	}
	for _, want := range []string{
		"v1.2.3 linux/amd64",
		"stats: the database is locked",
		"runners was shortened",
		"No workflow log is in it",
	} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("the summary does not mention %q:\n%s", want, out)
		}
	}
}

// --output json is a pipe, and a pipe wants the document rather than a file it
// would then have to go and find.
func TestStructuredOutputEmitsTheBundleRatherThanWritingAFile(t *testing.T) {
	srv := bundleServer(t)
	dir := t.TempDir()
	t.Chdir(dir)

	e, out, errOut := newTestEnv(t)
	if code := dispatch(context.Background(), e, []string{"diagnostics", "--url", srv.URL, "--output", "json"}); code != exitOK {
		t.Fatalf("exit code = %d\n%s", code, errOut)
	}
	var got map[string]any
	if err := json.Unmarshal(out.Bytes(), &got); err != nil {
		t.Fatalf("--output json did not emit the document: %v\n%s", err, out)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("reading the working directory: %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("--output json left a file behind: %v", entries)
	}
}
