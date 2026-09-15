package backend

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestRunnerRequiresDockerReadinessBeforeStartingItsListener(t *testing.T) {
	bash, err := exec.LookPath("bash")
	if err != nil {
		t.Skip("bash is not installed")
	}
	if _, err := exec.LookPath("timeout"); err != nil {
		t.Skip("coreutils timeout is not installed")
	}
	source, err := os.ReadFile("../../deploy/runner-entrypoint.sh")
	if err != nil {
		t.Fatal(err)
	}
	// The exit codes are part of the contract: the agent turns 69 and 78 into
	// a sentence on the Runners page, so a change here must change
	// entrypointExitHint with it.
	for _, tc := range []struct {
		name    string
		docker  string
		wait    string
		started bool
		exit    int
	}{
		{"ready", "exit 0", "30", true, 0},
		{"unavailable", "exit 1", "1", false, 69},
		{"hung probe", "exec sleep 30", "1", false, 69},
		{"invalid wait", "exit 0", "invalid", false, 78},
		{"zero wait", "exit 0", "0", false, 78},
		{"leading zero", "exit 0", "08", true, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			write := func(name, body string) {
				t.Helper()
				if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o700); err != nil {
					t.Fatal(err)
				}
			}
			// Only relocate the fixed image work directory. Execute the actual
			// entrypoint logic, with a stub listener and no GitHub credentials.
			write("entrypoint.sh", strings.Replace(string(source), "cd /home/runner", "cd \"$TEST_RUNNER_HOME\"", 1))
			write("docker", "#!/usr/bin/env bash\n"+tc.docker+"\n")
			write("run.sh", "#!/usr/bin/env bash\ntouch listener-started\n")
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			cmd := exec.CommandContext(ctx, bash, filepath.Join(dir, "entrypoint.sh"))
			cmd.Env = append(os.Environ(), "PATH="+dir+string(os.PathListSeparator)+os.Getenv("PATH"),
				"TEST_RUNNER_HOME="+dir, "DOCKER_HOST=tcp://127.0.0.1:2375",
				"ZOOMIES_DOCKER_WAIT="+tc.wait, "ZOOMIES_JITCONFIG=test-only")
			out, err := cmd.CombinedOutput()
			if ctx.Err() != nil {
				t.Fatalf("readiness was not bounded: %s", out)
			}
			_, statErr := os.Stat(filepath.Join(dir, "listener-started"))
			if (statErr == nil) != tc.started || (err == nil) != tc.started {
				t.Fatalf("listener started=%v, exit=%v, want started=%v: %s", statErr == nil, err, tc.started, out)
			}
			if code := cmd.ProcessState.ExitCode(); code != tc.exit {
				t.Fatalf("exit code = %d, want %d: %s", code, tc.exit, out)
			}
		})
	}
}
