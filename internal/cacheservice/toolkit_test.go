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

// TestTheRunnersOwnNodeIsRedirectedToTheService is the check that the node
// wrapper does its job on the real thing: the node binaries from the pinned
// actions/runner release, wrapped by the same installer the image build uses,
// running GitHub's cache client. The address the runner would have handed the
// action points at a host that cannot be resolved, so a restore can only
// succeed if the wrapper sent it to the service. Without the variable the same
// run must fail, which is what proves that.
//
// It needs an extracted actions/runner release as well as the toolkit:
//
//	curl -fsSL https://github.com/actions/runner/releases/download/v2.338.2/actions-runner-linux-x64-2.338.2.tar.gz | tar xz -C /tmp/runner
//	ZOOMIES_RUNNER_DIR=/tmp/runner ZOOMIES_ACTIONS_CACHE_MODULE=/tmp/ac/node_modules go test ./internal/cacheservice -run RunnersOwnNode -v
func TestTheRunnersOwnNodeIsRedirectedToTheService(t *testing.T) {
	runner := os.Getenv("ZOOMIES_RUNNER_DIR")
	modules := os.Getenv("ZOOMIES_ACTIONS_CACHE_MODULE")
	if runner == "" || modules == "" {
		t.Skip("ZOOMIES_RUNNER_DIR and ZOOMIES_ACTIONS_CACHE_MODULE are both needed")
	}
	script, err := filepath.Abs(filepath.Join("testdata", "toolkit.cjs"))
	if err != nil {
		t.Fatal(err)
	}
	wrap, err := filepath.Abs(filepath.Join("..", "..", "deploy", "runner-node-wrap.sh"))
	if err != nil {
		t.Fatal(err)
	}

	// Copy only the node binaries; the rest of the runner is not needed and the
	// installer must work on whatever tree it is given.
	tree := t.TempDir()
	for _, d := range []string{"node20", "node24"} {
		src := filepath.Join(runner, "externals", d, "bin", "node")
		if _, err := os.Stat(src); err != nil {
			t.Skipf("%s is missing from the runner: %v", d, err)
		}
		dst := filepath.Join(tree, "externals", d, "bin", "node")
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			t.Fatal(err)
		}
		in, err := os.Open(src)
		if err != nil {
			t.Fatal(err)
		}
		out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY, 0o755)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := io.Copy(out, in); err != nil {
			t.Fatal(err)
		}
		in.Close()
		out.Close()
	}
	if out, err := exec.Command("sh", wrap, tree).CombinedOutput(); err != nil {
		t.Fatalf("the installer failed: %v\n%s", err, out)
	}

	svc, err := New(Options{Dir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(svc)
	t.Cleanup(srv.Close)

	for _, d := range []string{"node20", "node24"} {
		node := filepath.Join(tree, "externals", d, "bin", "node")
		// What the runner gives an action: a GitHub address, GitHub's flag and
		// token. The host cannot be resolved, so only a redirect can succeed.
		base := append(os.Environ(),
			"ZOOMIES_ACTIONS_CACHE_MODULE="+modules,
			"ACTIONS_RESULTS_URL=http://github-results.invalid/",
			"ACTIONS_CACHE_SERVICE_V2=true",
			"ACTIONS_RUNTIME_TOKEN="+mainToken,
			"GITHUB_SERVER_URL=https://github.com",
			"RUNNER_OS=Linux",
		)

		control := exec.Command(node, script)
		control.Env = base
		if out, err := control.CombinedOutput(); err == nil {
			t.Fatalf("%s: the control run, without ZOOMIES_CACHE_SERVICE_URL, succeeded; the test proves nothing:\n%s", d, out)
		}

		run := exec.Command(node, script)
		run.Env = append(base, "ZOOMIES_CACHE_SERVICE_URL="+srv.URL+"/")
		out, err := run.CombinedOutput()
		version, _ := exec.Command(node, "--version").Output()
		t.Logf("%s %s: %s", d, strings.TrimSpace(string(version)), lastLine(string(out)))
		if err != nil || !strings.Contains(string(out), "toolkit ok") {
			t.Fatalf("%s: the wrapped node did not reach the service: %v\n%s", d, err, out)
		}
	}
}

func lastLine(s string) string {
	lines := strings.Split(strings.TrimSpace(s), "\n")
	return lines[len(lines)-1]
}
