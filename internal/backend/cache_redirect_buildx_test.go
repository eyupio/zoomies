package backend

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// docker/build-push-action runs `docker buildx build` as a child of the Node
// action, and buildx finds the type=gha cache in ACTIONS_RESULTS_URL in its own
// environment. These tests run the real preload under a real Node and check
// what a child process is given.
const redirectPreload = "../../deploy/runner-cache-redirect.cjs"

const childProbe = `
const cp = require('child_process');
const docker = process.argv[2];
const text = (b) => b.toString().trim();
const lines = ['self:' + process.env.ACTIONS_RESULTS_URL];
lines.push(text(cp.spawnSync(docker, ['buildx', 'build']).stdout));
lines.push(text(cp.spawnSync(docker, { env: { ...process.env, KEEP: '1' } }).stdout));
lines.push(text(cp.execFileSync(docker, ['x'], { encoding: 'utf8' })));
lines.push(text(cp.execFileSync(docker)));
cp.execFile(docker, ['x'], (e, out) => {
  lines.push(text(out));
  const child = cp.spawn(docker, ['x']);
  let b = '';
  child.stdout.on('data', (d) => (b += d));
  child.on('close', () => {
    lines.push(b.trim());
    lines.push(text(cp.spawnSync('sh', ['-c', 'echo other:$ACTIONS_RESULTS_URL']).stdout));
    console.log(lines.join('\n'));
  });
});
`

func runChildProbe(t *testing.T, env ...string) []string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("the probe is a shell script")
	}
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node is not installed here; the runner image has its own")
	}
	dir := t.TempDir()
	docker := filepath.Join(dir, "docker")
	if err := os.WriteFile(docker, []byte("#!/bin/sh\necho docker:$ACTIONS_RESULTS_URL\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	probe := filepath.Join(dir, "probe.js")
	if err := os.WriteFile(probe, []byte(childProbe), 0o644); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(node, "--require="+redirectPreload, probe, docker)
	cmd.Env = append([]string{"PATH=" + os.Getenv("PATH"), "ACTIONS_RESULTS_URL=https://results.example/", "ZOOMIES_CACHE_SERVICE_URL=http://10.0.0.1:4455"}, env...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("node failed: %v\n%s", err, out)
	}
	return strings.Split(strings.TrimSpace(string(out)), "\n")
}

func TestDockerChildrenAreSentToTheServiceOnlyWhenThePoolOptedIn(t *testing.T) {
	lines := runChildProbe(t, "ZOOMIES_CACHE_SERVICE_BUILDX=true")
	// self, then six docker calls (every way Node starts a process), then a
	// command that is not docker.
	if len(lines) != 8 {
		t.Fatalf("probe printed %d lines: %q", len(lines), lines)
	}
	if lines[0] != "self:https://results.example/" {
		t.Errorf("the action's own address changed to %q: its artifact calls would follow", lines[0])
	}
	for _, l := range lines[1:7] {
		if l != "docker:http://10.0.0.1:4455" {
			t.Errorf("a docker child was given %q, want the host's service", l)
		}
	}
	if lines[7] != "other:https://results.example/" {
		t.Errorf("a command that is not docker was given %q", lines[7])
	}
}

func TestDockerChildrenKeepGitHubsAddressWithoutTheOptIn(t *testing.T) {
	lines := runChildProbe(t)
	for _, l := range lines[1:7] {
		if l != "docker:https://results.example/" {
			t.Errorf("a docker child was given %q without the pool opting in", l)
		}
	}
}
