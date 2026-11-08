package cacheservice

import (
	"bytes"
	"encoding/pem"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
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

// TestTheRunnersOwnNodeSendsOnlyTheCacheToTheService is the check that the node
// wrapper and its preload do their job on the real thing: the node binaries from
// the pinned actions/runner release, wrapped by the same installer the image
// build uses, running GitHub's cache client and an artifact-style call.
//
// GitHub's results service is stood in for by an HTTPS server, as it is in
// production, and it answers artifact calls and refuses cache calls. So a cache
// save can only succeed if it was sent to the service, which a control run
// without ZOOMIES_CACHE_SERVICE_URL shows by failing; and the artifact call
// must still arrive at "GitHub", never at the cache service, because a cache
// service sent artifact calls would fail every job that uploads an artifact.
//
// It needs an extracted actions/runner release as well as the toolkit:
//
//	curl -fsSL https://github.com/actions/runner/releases/download/v2.338.2/actions-runner-linux-x64-2.338.2.tar.gz | tar xz -C /tmp/runner
//	ZOOMIES_RUNNER_DIR=/tmp/runner ZOOMIES_ACTIONS_CACHE_MODULE=/tmp/ac/node_modules go test ./internal/cacheservice -run RunnersOwnNode -v
func TestTheRunnersOwnNodeSendsOnlyTheCacheToTheService(t *testing.T) {
	runner := os.Getenv("ZOOMIES_RUNNER_DIR")
	modules := os.Getenv("ZOOMIES_ACTIONS_CACHE_MODULE")
	if runner == "" || modules == "" {
		t.Skip("ZOOMIES_RUNNER_DIR and ZOOMIES_ACTIONS_CACHE_MODULE are both needed")
	}
	abs := func(parts ...string) string {
		p, err := filepath.Abs(filepath.Join(parts...))
		if err != nil {
			t.Fatal(err)
		}
		return p
	}
	toolkit, artifact := abs("testdata", "toolkit.cjs"), abs("testdata", "artifact.cjs")
	wrap, redirect := abs("..", "..", "deploy", "runner-node-wrap.sh"), abs("..", "..", "deploy", "runner-cache-redirect.cjs")

	// Copy only the node binaries; the rest of the runner is not needed and the
	// installer must work on whatever tree it is given.
	tree := t.TempDir()
	for _, d := range []string{"node20", "node24"} {
		src := filepath.Join(runner, "externals", d, "bin", "node")
		// A runner directory the installer has already been run on holds the
		// wrapper as node, and the binary beside it as node.real.
		if _, err := os.Stat(src + ".real"); err == nil {
			src += ".real"
		}
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

	// The cache service, counting what it is asked.
	svc, err := New(Options{Dir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	var cacheCalls, artifactAtService atomic.Int32
	service := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "ArtifactService") {
			artifactAtService.Add(1)
		}
		if strings.HasPrefix(r.URL.Path, twirpPrefix) {
			cacheCalls.Add(1)
		}
		svc.ServeHTTP(w, r)
	}))
	t.Cleanup(service.Close)

	// GitHub's results service: artifacts yes, the cache no.
	var artifactAtGitHub, cacheAtGitHub atomic.Int32
	github := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.Contains(r.URL.Path, "ArtifactService"):
			artifactAtGitHub.Add(1)
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"artifacts":[]}`))
		case strings.HasPrefix(r.URL.Path, twirpPrefix):
			cacheAtGitHub.Add(1)
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"code":"not_found","msg":"the stand-in for GitHub has no cache"}`))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(github.Close)
	ca := filepath.Join(t.TempDir(), "github-ca.pem")
	if err := os.WriteFile(ca, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: github.Certificate().Raw}), 0o600); err != nil {
		t.Fatal(err)
	}

	for _, d := range []string{"node20", "node24"} {
		node := filepath.Join(tree, "externals", d, "bin", "node")
		// What the runner gives an action: GitHub's address, GitHub's flag and
		// token. The address is left as it is; the wrapper adds the preload.
		base := append(os.Environ(),
			"ZOOMIES_ACTIONS_CACHE_MODULE="+modules,
			"ZOOMIES_CACHE_REDIRECT_FILE="+redirect,
			"NODE_EXTRA_CA_CERTS="+ca,
			"ACTIONS_RESULTS_URL="+github.URL+"/",
			"ACTIONS_CACHE_SERVICE_V2=true",
			"ACTIONS_RUNTIME_TOKEN="+mainToken,
			"GITHUB_SERVER_URL=https://github.com",
			"RUNNER_OS=Linux",
		)

		control := exec.Command(node, toolkit)
		control.Env = base
		controlOut, controlErr := control.CombinedOutput()
		if controlErr == nil {
			t.Fatalf("%s: the control run, without ZOOMIES_CACHE_SERVICE_URL, succeeded; the test proves nothing:\n%s", d, controlOut)
		}
		if cacheAtGitHub.Load() == 0 {
			t.Fatalf("%s: the control run never asked GitHub's stand-in for the cache, so it failed for another reason:\n%s", d, controlOut)
		}
		cacheAtGitHub.Store(0)

		withService := append(base, "ZOOMIES_CACHE_SERVICE_URL="+service.URL+"/")
		run := exec.Command(node, toolkit)
		run.Env = withService
		out, err := run.CombinedOutput()
		version, _ := exec.Command(node, "--version").Output()
		t.Logf("%s %s: %s", d, strings.TrimSpace(string(version)), lastLine(string(out)))
		if err != nil || !strings.Contains(string(out), "toolkit ok") {
			t.Fatalf("%s: the wrapped node did not reach the cache service: %v\n%s", d, err, out)
		}
		if n := cacheAtGitHub.Load(); n != 0 {
			t.Fatalf("%s: %d cache calls still went to GitHub", d, n)
		}

		// An artifact call, with the service on: it must reach GitHub, not us.
		before := artifactAtGitHub.Load()
		art := exec.Command(node, artifact)
		art.Env = withService
		out, err = art.CombinedOutput()
		if err != nil {
			t.Fatalf("%s: the artifact call failed with the cache service on: %v\n%s", d, err, out)
		}
		if artifactAtGitHub.Load() != before+1 {
			t.Fatalf("%s: the artifact call did not reach GitHub", d)
		}
		if artifactAtService.Load() != 0 {
			t.Fatalf("%s: the cache service was sent an artifact call; that would fail every job that stores an artifact", d)
		}
	}
	if cacheCalls.Load() == 0 {
		t.Fatal("the cache service was never called")
	}
}

func lastLine(s string) string {
	lines := strings.Split(strings.TrimSpace(s), "\n")
	return lines[len(lines)-1]
}
