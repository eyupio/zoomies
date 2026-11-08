package backend

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// The runner overwrites ACTIONS_RESULTS_URL for every JavaScript action, so the
// only place left to redirect the cache is the node it starts. These tests hold
// the wrapper that does that, and the script that puts it there, to what they
// promise. The wrapper is shell, so they run the real scripts.

const (
	wrapperScript = "../../deploy/runner-node-wrapper.sh"
	wrapScript    = "../../deploy/runner-node-wrap.sh"
)

// fakeRunner lays out the part of a runner the installer looks at: a glibc and
// an Alpine node for each of two releases, each a script that reports what it
// was given so a test can see what reached the "real" node.
func fakeRunner(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	report := "#!/bin/sh\nprintf 'results=%s\\ncache=%s\\ntoken=%s\\nargs=' \"${ACTIONS_RESULTS_URL-unset}\" \"${ACTIONS_CACHE_URL-unset}\" \"${ACTIONS_RUNTIME_TOKEN-unset}\"\nfor a in \"$@\"; do printf '[%s]' \"$a\"; done\necho\n"
	for _, d := range []string{"node20", "node24", "node20_alpine", "node24_alpine"} {
		bin := filepath.Join(dir, "externals", d, "bin")
		if err := os.MkdirAll(bin, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(bin, "node"), []byte(report), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func wrap(t *testing.T, runner string) ([]byte, error) {
	t.Helper()
	cmd := exec.Command("sh", wrapScript, runner, wrapperScript)
	return cmd.CombinedOutput()
}

// runNode runs the wrapped node with exactly the given environment.
func runNode(t *testing.T, runner string, env []string, args ...string) (string, error) {
	t.Helper()
	cmd := exec.Command(filepath.Join(runner, "externals", "node20", "bin", "node"), args...)
	cmd.Env = append([]string{"PATH=/usr/bin:/bin"}, env...)
	out, err := cmd.Output()
	return string(out), err
}

func TestTheWrapperSendsTheCacheToTheHostWhenTheAgentSaidSo(t *testing.T) {
	runner := fakeRunner(t)
	if out, err := wrap(t, runner); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	out, err := runNode(t, runner, []string{
		EnvCacheServiceURL + "=http://172.17.0.1:41234/",
		"ACTIONS_CACHE_SERVICE_V2=true",
		"ACTIONS_RESULTS_URL=https://results-receiver.actions.githubusercontent.com/",
		"ACTIONS_CACHE_URL=https://artifactcache.actions.githubusercontent.com/abc/",
		"ACTIONS_RUNTIME_TOKEN=jwt-from-github",
	}, "main.js")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"results=http://172.17.0.1:41234/\n",
		// Only the v2 address moves; the v1 one and the token are GitHub's.
		"cache=https://artifactcache.actions.githubusercontent.com/abc/\n",
		"token=jwt-from-github\n",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("node saw:\n%s\nwant it to contain %q", out, want)
		}
	}
}

func TestTheWrapperLeavesGitHubsCacheAloneWithoutTheVariable(t *testing.T) {
	// A pool that has not turned the service on gets exactly today's behaviour.
	runner := fakeRunner(t)
	wrap(t, runner)
	for name, env := range map[string][]string{
		"unset": {"ACTIONS_CACHE_SERVICE_V2=true", "ACTIONS_RESULTS_URL=https://gh/"},
		"empty": {EnvCacheServiceURL + "=", "ACTIONS_CACHE_SERVICE_V2=true", "ACTIONS_RESULTS_URL=https://gh/"},
	} {
		out, err := runNode(t, runner, env, "main.js")
		if err != nil || !strings.Contains(out, "results=https://gh/\n") {
			t.Errorf("%s: node saw %q (%v), want GitHub's address", name, out, err)
		}
	}
}

func TestTheWrapperDoesNotRedirectAJobThatIsNotOnCacheServiceV2(t *testing.T) {
	// The service speaks only v2. A job on v1 (an older server, an older
	// action) would be sent somewhere that cannot answer it.
	runner := fakeRunner(t)
	wrap(t, runner)
	out, _ := runNode(t, runner, []string{
		EnvCacheServiceURL + "=http://172.17.0.1:41234/", "ACTIONS_RESULTS_URL=https://gh/"}, "main.js")
	if !strings.Contains(out, "results=https://gh/\n") {
		t.Fatalf("a job without cache service v2 was redirected: %s", out)
	}
}

func TestTheWrapperRunsTheRealNodeWithItsArgumentsIntact(t *testing.T) {
	runner := fakeRunner(t)
	wrap(t, runner)
	out, err := runNode(t, runner, nil, "--no-maglev", "/home/runner/_work/_actions/a b/main.js", "", `it's "quoted"`, "-e", "$HOME;*")
	if err != nil {
		t.Fatal(err)
	}
	want := "args=[--no-maglev][/home/runner/_work/_actions/a b/main.js][][it's \"quoted\"][-e][$HOME;*]\n"
	if !strings.HasSuffix(out, want) {
		t.Fatalf("node was given:\n%s\nwant it to end with %q", out, want)
	}
}

func TestTheWrapperPassesOnTheExitCodeAndStandardInput(t *testing.T) {
	// The runner reads node's exit code as the step's, and a few actions read
	// their input from stdin, so neither may be lost to the extra process.
	runner := fakeRunner(t)
	real := filepath.Join(runner, "externals", "node20", "bin", "node")
	if err := os.WriteFile(real, []byte("#!/bin/sh\ncat\nexit 7\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	wrap(t, runner)
	cmd := exec.Command(real)
	cmd.Env = []string{"PATH=/usr/bin:/bin"}
	cmd.Stdin = strings.NewReader("from the runner")
	var out bytes.Buffer
	cmd.Stdout = &out
	err := cmd.Run()
	if ee, ok := err.(*exec.ExitError); !ok || ee.ExitCode() != 7 {
		t.Fatalf("exit = %v, want 7", err)
	}
	if out.String() != "from the runner" {
		t.Fatalf("stdin did not reach node: %q", out.String())
	}
}

func TestTheInstallerWrapsTheGlibcNodesAndLeavesTheAlpineOnesAlone(t *testing.T) {
	runner := fakeRunner(t)
	if out, err := wrap(t, runner); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	want, _ := os.ReadFile(wrapperScript)
	for _, d := range []string{"node20", "node24"} {
		bin := filepath.Join(runner, "externals", d, "bin")
		got, _ := os.ReadFile(filepath.Join(bin, "node"))
		if !bytes.Equal(got, want) {
			t.Errorf("%s/bin/node is not the wrapper", d)
		}
		if fi, err := os.Stat(filepath.Join(bin, "node")); err != nil || fi.Mode().Perm()&0o111 == 0 {
			t.Errorf("%s/bin/node is not executable", d)
		}
		if _, err := os.Stat(filepath.Join(bin, "node.real")); err != nil {
			t.Errorf("%s: the real node was not kept: %v", d, err)
		}
	}
	for _, d := range []string{"node20_alpine", "node24_alpine"} {
		if _, err := os.Stat(filepath.Join(runner, "externals", d, "bin", "node.real")); err == nil {
			t.Errorf("%s was wrapped; the Alpine builds are left alone", d)
		}
	}
}

func TestTheInstallerIsSafeToRunTwiceAndFailsWhenThereIsNoNode(t *testing.T) {
	runner := fakeRunner(t)
	wrap(t, runner)
	first, _ := os.ReadFile(filepath.Join(runner, "externals", "node20", "bin", "node.real"))
	if out, err := wrap(t, runner); err != nil {
		t.Fatalf("a second run failed: %v\n%s", err, out)
	}
	again, _ := os.ReadFile(filepath.Join(runner, "externals", "node20", "bin", "node.real"))
	if !bytes.Equal(first, again) {
		t.Fatal("a second run replaced the real node with the wrapper")
	}
	// A runner release that moved its externals must break the build, not ship
	// an image without the cache service.
	if out, err := wrap(t, t.TempDir()); err == nil {
		t.Fatalf("an empty runner directory was accepted:\n%s", out)
	}
}

func TestTheWrapperAndTheBackendAgreeOnTheVariable(t *testing.T) {
	body, err := os.ReadFile(wrapperScript)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), EnvCacheServiceURL) {
		t.Fatalf("deploy/runner-node-wrapper.sh does not read %s, the name the backend sets", EnvCacheServiceURL)
	}
}

func TestTheRunnerImageWrapsNodeWhereItUnpacksTheRunner(t *testing.T) {
	// Renamed files in a later layer would copy node into it twice over, so the
	// wrap must be in the RUN that unpacks the archive.
	body, err := os.ReadFile("../../deploy/Dockerfile.runner")
	if err != nil {
		t.Fatal(err)
	}
	df := string(body)
	unpack := strings.Index(df, "tar xzf runner.tar.gz")
	wrapCall := strings.Index(df, "runner-node-wrap.sh /home/runner")
	chown := strings.Index(df[unpack:], "chown -R runner:runner /home/runner")
	if unpack < 0 || wrapCall < unpack || chown < 0 || unpack+chown < wrapCall {
		t.Fatal("deploy/Dockerfile.runner must call runner-node-wrap.sh after unpacking the runner and before the chown, in the same RUN")
	}
	if between := df[unpack:wrapCall]; strings.Contains(between, "\n\n") {
		t.Fatalf("the wrap is not in the RUN that unpacks the runner:\n%s", between)
	}
}
