package backend

import (
	"bytes"
	"debug/elf"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// The runner overwrites ACTIONS_RESULTS_URL for every JavaScript action, so the
// only place left to redirect the cache is the node it starts. These tests hold
// the wrapper that does that, and the script that puts it there, to what they
// promise. The wrapper is a small Go program the image build compiles, and the
// tests compile the same file and run the result, so what they hold is what the
// image carries.

const (
	wrapperSource = "../../deploy/node-wrapper.go"
	wrapScript    = "../../deploy/runner-node-wrap.sh"
)

// buildWrapper compiles deploy/node-wrapper.go as the image build does, for the
// host when goos is empty and for that system otherwise, and says where it put
// it. It is built without cgo and with -trimpath so the same source gives the
// same bytes wherever it is built.
func buildWrapper(t *testing.T, goos, goarch string) string {
	t.Helper()
	goTool, err := exec.LookPath("go")
	if err != nil {
		t.Fatalf("the wrapper tests compile the wrapper and found no go on the path: %v", err)
	}
	out := filepath.Join(t.TempDir(), "runner-node-wrapper")
	cmd := exec.Command(goTool, "build", "-trimpath", "-ldflags=-s -w", "-o", out, wrapperSource)
	cmd.Env = append(os.Environ(), "CGO_ENABLED=0")
	if goos != "" {
		cmd.Env = append(cmd.Env, "GOOS="+goos, "GOARCH="+goarch)
	}
	if b, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("building the wrapper: %v\n%s", err, b)
	}
	return out
}

// fakeRunner lays out the part of a runner the installer looks at: a glibc and
// an Alpine node for each of two releases, each a script that reports what it
// was given so a test can see what reached the "real" node.
func fakeRunner(t *testing.T) string {
	t.Helper()
	// The wrapper and its installer are POSIX shell for the Linux runner image,
	// and Windows cannot run a script by its path. The Windows job runs this
	// package, so the tests that run the scripts are for the platforms that do.
	if runtime.GOOS == "windows" {
		t.Skip("the runner image's node wrapper is a shell script; it runs on Linux only")
	}
	dir := t.TempDir()
	report := "#!/bin/sh\nprintf 'results=%s\\ncache=%s\\ntoken=%s\\noptions=%s\\nargs=' \"${ACTIONS_RESULTS_URL-unset}\" \"${ACTIONS_CACHE_URL-unset}\" \"${ACTIONS_RUNTIME_TOKEN-unset}\" \"${NODE_OPTIONS-unset}\"\nfor a in \"$@\"; do printf '[%s]' \"$a\"; done\necho\n"
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
	cmd := exec.Command("sh", wrapScript, runner, buildWrapper(t, "", ""))
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

// preload is a stand-in for the redirect script the image carries; the wrapper
// only loads it, so what it holds does not matter here, only that it is there.
func preload(t *testing.T) string {
	t.Helper()
	f := filepath.Join(t.TempDir(), "runner-cache-redirect.cjs")
	if err := os.WriteFile(f, []byte("// stand-in\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return f
}

func TestTheWrapperLoadsTheCacheRedirectWhenTheAgentSaidSo(t *testing.T) {
	runner := fakeRunner(t)
	if out, err := wrap(t, runner); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	redirect := preload(t)
	out, err := runNode(t, runner, []string{
		EnvCacheServiceURL + "=http://172.17.0.1:41234/",
		"ZOOMIES_CACHE_REDIRECT_FILE=" + redirect,
		"ACTIONS_CACHE_SERVICE_V2=true",
		"ACTIONS_RESULTS_URL=https://results-receiver.actions.githubusercontent.com/",
		"ACTIONS_CACHE_URL=https://artifactcache.actions.githubusercontent.com/abc/",
		"ACTIONS_RUNTIME_TOKEN=jwt-from-github",
	}, "main.js")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"options=--require=" + redirect + "\n",
		// The address is where the artifact actions find their service too, so
		// it must stay GitHub's; the preload redirects the cache's calls alone.
		"results=https://results-receiver.actions.githubusercontent.com/\n",
		"cache=https://artifactcache.actions.githubusercontent.com/abc/\n",
		"token=jwt-from-github\n",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("node saw:\n%s\nwant it to contain %q", out, want)
		}
	}
}

func TestTheWrapperKeepsTheNodeOptionsTheJobAlreadyHad(t *testing.T) {
	runner := fakeRunner(t)
	wrap(t, runner)
	redirect := preload(t)
	out, _ := runNode(t, runner, []string{
		EnvCacheServiceURL + "=http://172.17.0.1:41234/", "ZOOMIES_CACHE_REDIRECT_FILE=" + redirect,
		"ACTIONS_CACHE_SERVICE_V2=true", "NODE_OPTIONS=--max-old-space-size=512",
	}, "main.js")
	if want := "options=--require=" + redirect + " --max-old-space-size=512\n"; !strings.Contains(out, want) {
		t.Fatalf("node saw:\n%s\nwant it to contain %q", out, want)
	}
}

func TestTheWrapperLeavesGitHubsCacheAloneWithoutTheVariable(t *testing.T) {
	// A pool that has not turned the service on gets exactly today's behaviour.
	runner := fakeRunner(t)
	wrap(t, runner)
	redirect := preload(t)
	for name, env := range map[string][]string{
		"unset": {"ZOOMIES_CACHE_REDIRECT_FILE=" + redirect, "ACTIONS_CACHE_SERVICE_V2=true"},
		"empty": {EnvCacheServiceURL + "=", "ZOOMIES_CACHE_REDIRECT_FILE=" + redirect, "ACTIONS_CACHE_SERVICE_V2=true"},
	} {
		out, err := runNode(t, runner, env, "main.js")
		if err != nil || !strings.Contains(out, "options=unset\n") {
			t.Errorf("%s: node saw %q (%v), want no preload", name, out, err)
		}
	}
}

func TestTheWrapperDoesNotRedirectAJobThatIsNotOnCacheServiceV2(t *testing.T) {
	// The service speaks only v2. A job on v1 (an older server, an older
	// action) would be sent somewhere that cannot answer it.
	runner := fakeRunner(t)
	wrap(t, runner)
	out, _ := runNode(t, runner, []string{
		EnvCacheServiceURL + "=http://172.17.0.1:41234/", "ZOOMIES_CACHE_REDIRECT_FILE=" + preload(t)}, "main.js")
	if !strings.Contains(out, "options=unset\n") {
		t.Fatalf("a job without cache service v2 was redirected: %s", out)
	}
}

func TestTheWrapperStillRunsTheActionWhenTheImageLacksThePreload(t *testing.T) {
	// A custom or older image has the wrapper without the script. Losing the
	// cache service is acceptable; failing the action is not.
	runner := fakeRunner(t)
	wrap(t, runner)
	out, err := runNode(t, runner, []string{
		EnvCacheServiceURL + "=http://172.17.0.1:41234/", "ACTIONS_CACHE_SERVICE_V2=true",
		"ZOOMIES_CACHE_REDIRECT_FILE=" + filepath.Join(t.TempDir(), "missing.cjs"),
	}, "main.js")
	if err != nil || !strings.Contains(out, "options=unset\n") || !strings.Contains(out, "args=[main.js]") {
		t.Fatalf("node saw %q (%v)", out, err)
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

func TestTheWrapperKeepsEnvironmentVariablesWhoseNamesAreNotShellNames(t *testing.T) {
	// The runner hands every JavaScript action its inputs as INPUT_<NAME> with
	// the name's hyphens kept, so setup-go reads INPUT_GO-VERSION-FILE. That is
	// not a name a POSIX shell can hold, and dash, which is /bin/sh on Debian and
	// Ubuntu, drops such a variable on the way through a script. This wrapper was
	// a /bin/sh script once, and every action with a hyphenated input then ran in
	// a job in which the input was never given: setup-go and setup-python failed
	// outright, and checkout quietly fell back to its defaults. It is a program
	// now, which has no such limit, and this holds it there.
	//
	// The real node here is a copy of env, not a script, so nothing between the
	// wrapper and the check is a shell that could drop the name by itself.
	envBinary, err := exec.LookPath("env")
	if err != nil {
		t.Skip("no env to stand in for node: " + err.Error())
	}
	inputs := []string{"INPUT_GO-VERSION-FILE=go.mod", "INPUT_PYTHON-VERSION=3.12", "INPUT_TOKEN=x"}
	redirect := preload(t)
	for name, extra := range map[string][]string{
		"without the cache service": nil,
		"with the cache service": {EnvCacheServiceURL + "=http://172.17.0.1:41234/",
			"ZOOMIES_CACHE_REDIRECT_FILE=" + redirect, "ACTIONS_CACHE_SERVICE_V2=true"},
	} {
		runner := fakeRunner(t)
		body, err := os.ReadFile(envBinary)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(runner, "externals", "node20", "bin", "node"), body, 0o755); err != nil {
			t.Fatal(err)
		}
		if out, err := wrap(t, runner); err != nil {
			t.Fatalf("%v\n%s", err, out)
		}
		out, err := runNode(t, runner, append(append([]string{}, inputs...), extra...))
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		for _, want := range inputs {
			if !strings.Contains("\n"+out, "\n"+want+"\n") {
				t.Errorf("%s: the real node's environment was:\n%s\nwant it to contain %q", name, out, want)
			}
		}
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
	want, err := os.ReadFile(buildWrapper(t, "", ""))
	if err != nil {
		t.Fatal(err)
	}
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

func TestTheWrapperIsAStaticProgramSoAJobImageNeedsNoShellOrLibc(t *testing.T) {
	// A `container:` job mounts the runner's externals, this wrapper included,
	// into whatever image the workflow chose and starts node from there. That
	// image may have no bash, no sh, or a libc this was not built against, so the
	// wrapper depends on none of them: it is statically linked and starts no
	// shell. Both architectures the image ships are held to it.
	for _, arch := range []string{"amd64", "arm64"} {
		f, err := elf.Open(buildWrapper(t, "linux", arch))
		if err != nil {
			t.Fatalf("%s: %v", arch, err)
		}
		for _, p := range f.Progs {
			if p.Type == elf.PT_INTERP {
				t.Errorf("%s: the wrapper asks for a dynamic loader, so it needs a libc in the job's image", arch)
			}
		}
		if libs, err := f.ImportedLibraries(); err != nil || len(libs) > 0 {
			t.Errorf("%s: the wrapper links %v (%v); it must link nothing", arch, libs, err)
		}
		f.Close()
	}
}

func TestTheWrapperFindsTheRealNodeWhenItIsStartedByABareName(t *testing.T) {
	// A step in a job container can start node by the name it finds on its
	// PATH, so the first argument is "node" and not where it is. The real node is
	// beside the wrapper, wherever that is, and not beside wherever it was named.
	runner := fakeRunner(t)
	if out, err := wrap(t, runner); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	cmd := exec.Command(filepath.Join(runner, "externals", "node20", "bin", "node"), "main.js")
	cmd.Args[0] = "node"
	cmd.Env = []string{"PATH=/usr/bin:/bin"}
	out, err := cmd.Output()
	if err != nil || !strings.Contains(string(out), "args=[main.js]") {
		t.Fatalf("started as %q the wrapper gave %q (%v)", "node", out, err)
	}
}

func TestTheWrapperSaysWhichNodeItCouldNotFind(t *testing.T) {
	// A runner image whose real node was lost fails every action, and the first
	// place anybody looks is the output of the step that failed.
	runner := fakeRunner(t)
	if out, err := wrap(t, runner); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	real := filepath.Join(runner, "externals", "node20", "bin", "node.real")
	if err := os.Remove(real); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(filepath.Join(runner, "externals", "node20", "bin", "node"), "main.js")
	cmd.Env = []string{"PATH=/usr/bin:/bin"}
	out, err := cmd.CombinedOutput()
	ee, ok := err.(*exec.ExitError)
	if !ok || ee.ExitCode() != 127 {
		t.Fatalf("exit = %v, want 127, the status a shell gives a command it cannot find", err)
	}
	// By name and not by full path: where a temporary directory really is can
	// differ from where it was made on some systems.
	if !strings.Contains(string(out), filepath.Base(real)) {
		t.Fatalf("the wrapper did not say which file was missing:\n%s", out)
	}
}

func TestTheWrapperAndTheBackendAgreeOnTheVariable(t *testing.T) {
	body, err := os.ReadFile(wrapperSource)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), EnvCacheServiceURL) {
		t.Fatalf("deploy/node-wrapper.go does not read %s, the name the backend sets", EnvCacheServiceURL)
	}
}

func TestTheImageBuildsTheWrapperStaticallyAndInstallsItWhereTheInstallerLooks(t *testing.T) {
	dockerfile, err := os.ReadFile("../../deploy/Dockerfile.runner")
	if err != nil {
		t.Fatal(err)
	}
	df := string(dockerfile)
	for _, want := range []string{
		"deploy/node-wrapper.go",
		"CGO_ENABLED=0",
		"--from=nodewrap",
		"/usr/local/lib/zoomies/runner-node-wrapper",
	} {
		if !strings.Contains(df, want) {
			t.Errorf("deploy/Dockerfile.runner does not contain %q: the wrapper must be built without cgo and put where runner-node-wrap.sh looks for it", want)
		}
	}
	installer, err := os.ReadFile(wrapScript)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(installer), `/runner-node-wrapper}`) {
		t.Error("deploy/runner-node-wrap.sh does not default to runner-node-wrapper beside it")
	}
}

func TestTheImageCarriesTheRedirectWhereTheWrapperLooksForIt(t *testing.T) {
	wrapper, err := os.ReadFile(wrapperSource)
	if err != nil {
		t.Fatal(err)
	}
	dockerfile, err := os.ReadFile("../../deploy/Dockerfile.runner")
	if err != nil {
		t.Fatal(err)
	}
	const home = "/usr/local/lib/zoomies/"
	if !strings.Contains(string(wrapper), home+"runner-cache-redirect.cjs") {
		t.Errorf("the wrapper does not look for the redirect in %s", home)
	}
	if !strings.Contains(string(dockerfile), "deploy/runner-cache-redirect.cjs "+home) {
		t.Errorf("deploy/Dockerfile.runner does not copy the redirect into %s", home)
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
