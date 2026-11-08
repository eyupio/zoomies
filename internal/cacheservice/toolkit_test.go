package cacheservice

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestTheRealToolkitClientSavesAndRestores runs GitHub's own @actions/cache
// against the service, because a protocol written from memory is a guess until
// the client it is for has used it. It needs node and the module, so it is
// skipped unless ZOOMIES_ACTIONS_CACHE_MODULE names a node_modules folder that
// holds @actions/cache:
//
//	mkdir /tmp/ac && cd /tmp/ac && npm install @actions/cache
//	ZOOMIES_ACTIONS_CACHE_MODULE=/tmp/ac/node_modules go test ./internal/cacheservice -run Toolkit -v
//
// TOOLKIT_MEGABYTES sets the archive's size; above 128 the client stages
// blocks instead of making one PUT, which is the other upload path.
func TestTheRealToolkitClientSavesAndRestores(t *testing.T) {
	modules := os.Getenv("ZOOMIES_ACTIONS_CACHE_MODULE")
	if modules == "" {
		t.Skip("ZOOMIES_ACTIONS_CACHE_MODULE is not set")
	}
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node is not installed")
	}
	svc, err := New(Options{Dir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	// Log each call's body: when the real client disagrees with this service,
	// what it actually sent is the first thing anyone needs to see.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, twirpPrefix) {
			body, _ := io.ReadAll(r.Body)
			t.Logf("-> %s %s", strings.TrimPrefix(r.URL.Path, twirpPrefix), body)
			r.Body = io.NopCloser(bytes.NewReader(body))
		}
		svc.ServeHTTP(w, r)
	}))
	t.Cleanup(srv.Close)
	script, err := filepath.Abs(filepath.Join("testdata", "toolkit.cjs"))
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(node, script)
	cmd.Env = append(os.Environ(),
		"ACTIONS_RESULTS_URL="+srv.URL+"/",
		"ACTIONS_CACHE_SERVICE_V2=true",
		"ACTIONS_RUNTIME_TOKEN="+mainToken,
		"GITHUB_SERVER_URL=https://github.com",
		"RUNNER_OS=Linux",
	)
	out, err := cmd.CombinedOutput()
	t.Logf("%s", out)
	if err != nil {
		t.Fatalf("the toolkit client failed: %v", err)
	}
	if !strings.Contains(string(out), "toolkit ok") {
		t.Fatalf("the script did not report success:\n%s", out)
	}
}
