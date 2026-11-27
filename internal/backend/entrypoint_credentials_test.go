package backend

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// The entrypoint takes the runner's credentials out of its environment so that
// a job's steps, which are its descendants and run as the same user, cannot
// read them. Unsetting them in the shell is not enough: the kernel keeps serving
// the environment a process was started with at /proc/<pid>/environ, and the
// entrypoint is the container's first process. A step that read that file got the
// organisation's registration token, good for an hour and enough to register a
// runner of its own.
func TestTheEntrypointsCredentialsAreNotInItsOwnEnvironment(t *testing.T) {
	t.Parallel()
	if runtime.GOOS != "linux" {
		t.Skip("reads /proc")
	}
	bash, err := exec.LookPath("bash")
	if err != nil {
		t.Skip("bash is not installed")
	}
	source, err := os.ReadFile("../../deploy/runner-entrypoint.sh")
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name   string
		env    []string
		secret string
		// arg is what the stub listener or the registration must still be given:
		// the credential is hidden from the environment, not from the runner.
		arg string
	}{
		{"a just-in-time configuration", []string{"ZOOMIES_JITCONFIG=jit-secret-value-0123456789"}, "jit-secret-value-0123456789", "--jitconfig"},
		{"a registration token", []string{"ZOOMIES_RUNNER_TOKEN=reg-secret-value-0123456789", "ZOOMIES_RUNNER_URL=https://github.com/acme"}, "reg-secret-value-0123456789", "--token"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			write := func(name, body string) {
				t.Helper()
				if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o700); err != nil {
					t.Fatal(err)
				}
			}
			write("entrypoint.sh", strings.Replace(string(source), "cd /home/runner", "cd \"$TEST_RUNNER_HOME\"", 1))
			// The listener stands for a job's step: it reads what any process the
			// entrypoint starts could read, its own environment and its parent's,
			// and records what it was asked to do.
			write("run.sh", "#!/usr/bin/env bash\n"+
				"env > child-env\n"+
				"tr '\\0' '\\n' < /proc/$PPID/environ > parent-environ\n"+
				"printf '%s\\n' \"$@\" > run-args\n")
			write("config.sh", "#!/usr/bin/env bash\nprintf '%s\\n' \"$@\" >> config-args\n")

			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			cmd := exec.CommandContext(ctx, bash, filepath.Join(dir, "entrypoint.sh"))
			cmd.Env = append(os.Environ(), "PATH="+dir+string(os.PathListSeparator)+os.Getenv("PATH"), "TEST_RUNNER_HOME="+dir)
			cmd.Env = append(cmd.Env, tc.env...)
			if out, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("the entrypoint failed: %v\n%s", err, out)
			}

			read := func(name string) string {
				t.Helper()
				b, err := os.ReadFile(filepath.Join(dir, name))
				if err != nil {
					t.Fatalf("%s was not written: %v", name, err)
				}
				return string(b)
			}
			for _, name := range []string{"child-env", "parent-environ"} {
				if got := read(name); strings.Contains(got, tc.secret) {
					t.Errorf("%s holds the credential, so a step could read it:\n%s", name, got)
				}
			}
			// The runner itself still gets it, which is the point of hiding it
			// from everything else.
			given := read("run-args")
			if tc.arg == "--token" {
				given = read("config-args")
			}
			if !strings.Contains(given, tc.arg) || !strings.Contains(given, tc.secret) {
				t.Errorf("the runner was not given %s %s:\n%s", tc.arg, tc.secret, given)
			}
		})
	}
}
