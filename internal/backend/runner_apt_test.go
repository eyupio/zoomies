package backend

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// An Ubuntu ports mirror that 404ed a package its own index listed cost the
// ubuntu-2404 arm64 variant a whole image build. deploy/runner-apt.sh is what
// recovers from that, and it only helps for as long as every apt installation
// in the image goes through it: a script that calls apt-get itself is back to
// failing a build over a mirror that was mid-sync.
func TestEveryAptInstallInTheRunnerImageGoesThroughTheRetryingHelper(t *testing.T) {
	for _, script := range runnerScripts(t) {
		name := filepath.Base(script)
		// The helper is the one place apt-get is spelled out, and the
		// entrypoint installs nothing.
		if name == "runner-apt.sh" || name == "runner-entrypoint.sh" {
			continue
		}
		// Comments are allowed to name apt-get -- several of them explain
		// what it does -- so only what the shell actually runs is read.
		for i, line := range strings.Split(readFile(t, script), "\n") {
			code, _, _ := strings.Cut(line, "#")
			if strings.Contains(code, "apt-get") {
				t.Errorf("deploy/%s:%d runs apt-get itself; install through runner-apt.sh so a mirror catching up does not fail the build", name, i+1)
			}
		}
	}
}

// The helper is copied next to each script rather than left in the image, so a
// step that starts using it and does not ask for it fails at build time with
// "not found" -- on one architecture of one variant, days after the change.
// Cheaper to catch here.
func TestEveryScriptThatUsesTheAptHelperIsGivenIt(t *testing.T) {
	dockerfile := readFile(t, filepath.Join("..", "..", "deploy", "Dockerfile.runner"))
	for _, script := range runnerScripts(t) {
		name := filepath.Base(script)
		if name == "runner-apt.sh" {
			continue
		}
		if !strings.Contains(readFile(t, script), "runner-apt.sh") {
			continue
		}
		if !strings.Contains(dockerfile, "COPY --chmod=0755 deploy/runner-apt.sh deploy/"+name+" /tmp/") {
			t.Errorf("deploy/%s uses runner-apt.sh, but deploy/Dockerfile.runner does not copy the helper alongside it", name)
		}
	}
}

func runnerScripts(t *testing.T) []string {
	t.Helper()
	scripts, err := filepath.Glob(filepath.Join("..", "..", "deploy", "runner-*.sh"))
	if err != nil {
		t.Fatalf("listing the runner image's scripts: %v", err)
	}
	if len(scripts) == 0 {
		t.Fatal("found no deploy/runner-*.sh scripts")
	}
	return scripts
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading %s: %v", path, err)
	}
	return string(body)
}
