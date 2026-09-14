//go:build e2e

package e2e

import (
	"context"
	"fmt"
	"net/http"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// This scenario proves the other half of the product: not "does a runner run a
// job", but "does `zoomies init` turn a bare machine into a working
// controller, and does `zoomies uninstall` give the machine back".
//
// It is the half that unit tests cannot reach. Setting a host up means making
// a system account, writing into /etc and /var/lib, sealing a key at mode
// 0600 and creating an administrator in a real database -- none of which can
// be asserted honestly against a temporary directory, because the guards that
// matter are the ones keyed to the real system paths. `serviceUserToRemove`
// is the clearest case: it refuses to delete the account unless the install
// really is the one that owns /etc/zoomies, so a test pointed somewhere else
// proves nothing about the code that runs for an operator.
//
// So the machine is a throwaway container, and the installer is let loose on
// it exactly as it would be on a VM.

const installerScenario = "installer_provisions_and_removes_a_host"

// Where a default install puts things. They are spelled out rather than read
// from the installer, because the point is to check the paths an operator was
// promised against the paths that appeared.
const (
	sysConfigDir = "/etc/zoomies"
	sysStateDir  = "/var/lib/zoomies"
	sysKeyFile   = sysConfigDir + "/encryption.key"
	sysConfig    = sysConfigDir + "/zoomies.yaml"
	sysDatabase  = sysStateDir + "/zoomies.db"
	// serviceAccount is the unprivileged account the installer creates, and
	// the only one uninstall will ever delete.
	serviceAccount = "zoomies"
)

func TestInstallerProvisionsAndRemovesAHost(t *testing.T) {
	runID := newRunID()
	res := &Result{
		Scenario: installerScenario, RunID: runID, Commit: commit(),
		StartedAt: time.Now(), Category: NotRun,
	}
	t.Cleanup(func() {
		if !requested() {
			return
		}
		res.FinishedAt = time.Now()
		if err := res.write(resultsDir(t)); err != nil {
			t.Errorf("writing the result record: %v", err)
		}
	})

	if !requested() {
		t.Skip("set ZOOMIES_E2E=1 to run the end-to-end tests; see test/e2e/README.md")
	}

	// Deliberately not preflight(): this scenario touches nothing on GitHub,
	// so a gate with no App installed should still run it.
	if missing := installerPreflight(); len(missing) > 0 {
		res.Category, res.Reason = Blocked, strings.Join(missing, "; ")
		if required() {
			t.Fatalf("blocked: %s", res.Reason)
		}
		t.Skipf("blocked: %s", res.Reason)
	}

	// Registered before the container exists, so that it runs *after* the
	// container's own removal: cleanups run last-registered-first, and a
	// throwaway host that could not be removed is litter on the machine
	// running the test. A scenario that passed and left one behind has not
	// finished, so its failure has to reach the category.
	t.Cleanup(func() {
		if res.Category == NotRun && !t.Failed() {
			res.Category = Passed
		}
		if t.Failed() && res.Category != Blocked {
			res.Category = Failed
			if res.Reason == "" {
				res.Reason = "an assertion failed; see the test output"
			}
		}
	})

	port := freePort(t)
	host := newInstallerHost(t, runID, port)

	// 1. Set the machine up, unattended, from an answer file -- which is the
	//    path configuration management takes and the only one a test can
	//    drive, since the interactive one waits for a person.
	host.write("/tmp/answers.yaml", installerAnswers())
	out, err := host.run("zoomies", "init", "--non-interactive", "--answers", "/tmp/answers.yaml")
	if err != nil {
		t.Fatalf("zoomies init failed: %v\n%s", err, out)
	}
	t.Logf("installer output:\n%s", out)

	// The summary is what the operator is left reading, so it has to name the
	// key they now have to back up. Losing it loses every stored secret.
	if !strings.Contains(out, sysKeyFile) {
		t.Errorf("the installer never told the operator where the encryption key is:\n%s", out)
	}

	// 2. What it actually put on the machine, asked of the filesystem rather
	//    than of the installer's own summary.
	for _, want := range []struct {
		path, mode, owner, why string
	}{
		{sysConfigDir, "750", "zoomies:zoomies", "the configuration directory"},
		{sysStateDir, "750", "zoomies:zoomies", "the state directory"},
		// 0600 is the whole promise about the key: anything that can read the
		// config must not thereby be able to decrypt the App's private key.
		{sysKeyFile, "600", "zoomies:zoomies", "the encryption key"},
		{sysConfig, "640", "zoomies:zoomies", "the configuration"},
	} {
		got, err := host.run("stat", "-c", "%a %U:%G", want.path)
		if err != nil {
			t.Errorf("%s (%s) was not created: %v\n%s", want.path, want.why, err, got)
			continue
		}
		if fields := strings.Fields(strings.TrimSpace(got)); len(fields) != 2 {
			t.Errorf("stat of %s returned %q", want.path, got)
		} else if fields[0] != want.mode || fields[1] != want.owner {
			t.Errorf("%s is mode %s owned by %s, want mode %s owned by %s",
				want.path, fields[0], fields[1], want.mode, want.owner)
		}
	}

	// 3. The key is in its own file and nowhere else. A key written into
	//    zoomies.yaml would be copied by every backup and every configuration
	//    management run that touches the config.
	cfg, err := host.run("cat", sysConfig)
	if err != nil {
		t.Fatalf("reading the configuration the installer wrote: %v\n%s", err, cfg)
	}
	if !strings.Contains(cfg, `encryption_key: ""`) {
		t.Errorf("the configuration does not carry an empty encryption_key, so the key may have been written into it:\n%s", cfg)
	}
	if !strings.Contains(cfg, "encryption_key_file: "+sysKeyFile) {
		t.Errorf("the configuration does not point at %s:\n%s", sysKeyFile, cfg)
	}

	// 4. The service account: a real system user that cannot be logged into.
	//    An account with a shell is a way onto the machine that the runners'
	//    own isolation was supposed to have closed.
	passwd, err := host.run("getent", "passwd", serviceAccount)
	if err != nil {
		t.Fatalf("the installer did not create the %s system account: %v\n%s", serviceAccount, err, passwd)
	}
	if !strings.Contains(passwd, "nologin") && !strings.Contains(passwd, "/bin/false") {
		t.Errorf("the %s account has a login shell, which it must not: %s", serviceAccount, strings.TrimSpace(passwd))
	}

	// 5. The database exists and carries the administrator that was asked for.
	if out, err := host.run("test", "-s", sysDatabase); err != nil {
		t.Fatalf("no database at %s: %v\n%s", sysDatabase, err, out)
	}

	// 6. The decisive one: the controller this install configured actually
	//    serves. Everything above is evidence about files; this is the
	//    deployment working, asked from outside the container over the port
	//    the operator was told to use.
	host.startController(t)
	base := fmt.Sprintf("http://127.0.0.1:%d", port)
	waitFor(t, waitInstalledControllerHealthy, "the installed controller to become healthy", func() bool {
		resp, err := http.Get(base + "/healthz")
		if err != nil {
			return false
		}
		resp.Body.Close()
		return resp.StatusCode == http.StatusOK
	})

	// And it must be serving with authentication on. The safe configuration is
	// the default, so an install that left the API open is a failed install
	// however healthy it reports.
	resp, err := http.Get(base + "/api/v1/pools")
	if err != nil {
		t.Fatalf("asking the installed controller for its pools: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("GET /api/v1/pools on a fresh install answered %d, want 401: "+
			"a default install must not leave the API open", resp.StatusCode)
	}
	host.stopController(t)

	// 7. Give the machine back. An installer that cannot be undone is one
	//    nobody can safely try.
	out, err = host.run("zoomies", "uninstall", "--yes", "--non-interactive", "--deregister=false")
	if err != nil {
		t.Fatalf("zoomies uninstall failed: %v\n%s", err, out)
	}
	t.Logf("uninstall output:\n%s", out)
	if !strings.Contains(out, "nothing was left behind") {
		t.Errorf("uninstall did not report a clean removal:\n%s", out)
	}

	// 8. And prove it, by asking the machine rather than believing the report.
	//    This is the same principle as the GitHub scenario's last two checks:
	//    "did it clean up?" answered by the thing that was supposed to clean
	//    up is not evidence.
	for _, path := range []string{sysConfigDir, sysStateDir} {
		if out, err := host.run("test", "-e", path); err == nil {
			t.Errorf("%s is still on the machine after uninstall:\n%s", path, out)
		}
	}
	// The account is the one uninstall is most cautious about, because
	// deleting the wrong one is unrecoverable. A default install at the
	// default paths is the case where it may, and must.
	if out, err := host.run("getent", "passwd", serviceAccount); err == nil {
		t.Errorf("the %s account outlived the install it was created for: %s",
			serviceAccount, strings.TrimSpace(out))
	}
}

// installerAnswers is the answer file the scenario installs from.
//
// It asks for the process backend and no pool, because this scenario is about
// what setup does to the machine rather than about running a job -- that is
// the other scenario's subject, and it needs a GitHub App this one
// deliberately does without.
func installerAnswers() string {
	return strings.Join([]string{
		"mode: single",
		"deployment: native",
		"backend: process",
		"capacity: 1",
		// The container publishes this port, so the check comes from outside.
		"bind: 0.0.0.0:8099",
		"external_url: http://127.0.0.1:8099",
		"tls:",
		`  mode: "off"`,
		"github:",
		"  skip: true",
		"admin:",
		"  username: e2e-admin",
		"  password: a-long-enough-password-123",
		"pool:",
		"  skip: true",
		"service:",
		"  manager: none",
		"",
	}, "\n")
}

// --------------------------------------------------------------------------
// the throwaway machine
// --------------------------------------------------------------------------

// installerHost is a disposable container the installer is allowed to change
// however it likes. Everything the scenario asserts is asked of it from
// outside, through docker.
type installerHost struct {
	t    *testing.T
	name string
}

// newInstallerHost starts the container and registers its removal. The
// container is the cleanup: whatever the installer did to it goes when it
// does, which is why this scenario can afford to let a real installer run at
// the real system paths.
func newInstallerHost(t *testing.T, runID string, port int) *installerHost {
	t.Helper()
	h := &installerHost{t: t, name: "zoomies-e2e-installer-" + runID}

	bin, err := filepath.Abs(builtBinary())
	if err != nil {
		t.Fatalf("resolving the binary's path: %v", err)
	}

	// Pull first and separately, so that a slow or failed pull is reported as
	// what it is rather than as the container failing to start.
	pullCtx, cancelPull := context.WithTimeout(context.Background(), waitInstallerImage)
	defer cancelPull()
	if out, err := exec.CommandContext(pullCtx, "docker", "pull", installerImage()).CombinedOutput(); err != nil {
		t.Fatalf("pulling %s within %s: %v\n%s", installerImage(), waitInstallerImage, err, out)
	}

	out, err := exec.Command("docker", "run", "-d", "--name", h.name,
		"-p", fmt.Sprintf("127.0.0.1:%d:8099", port),
		"-v", bin+":/usr/local/bin/zoomies:ro",
		installerImage(), "sleep", "infinity").CombinedOutput()
	if err != nil {
		t.Fatalf("starting the throwaway host: %v\n%s", err, out)
	}
	t.Cleanup(func() {
		if out, err := exec.Command("docker", "rm", "-f", h.name).CombinedOutput(); err != nil {
			// This is litter on the machine running the test, so it is worth
			// a failure rather than a log line.
			t.Errorf("could not remove the throwaway host %s: %v\n%s", h.name, err, out)
		}
	})
	return h
}

// run executes a command on the machine and returns its combined output.
//
// Every command is bounded by waitInstallerSteps rather than left to run for
// ever: an installer that hangs waiting for a prompt it should never have
// reached would otherwise be reported as the whole suite timing out, which
// names nothing. The budget is shared with the Makefile's -timeout through
// budget_test.go.
func (h *installerHost) run(name string, args ...string) (string, error) {
	h.t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), waitInstallerSteps)
	defer cancel()
	full := append([]string{"exec", h.name, name}, args...)
	out, err := exec.CommandContext(ctx, "docker", full...).CombinedOutput()
	if ctx.Err() != nil {
		return string(out), fmt.Errorf("%s did not finish within %s: %w", name, waitInstallerSteps, ctx.Err())
	}
	return string(out), err
}

// write puts a file on the machine. It goes through a here-document rather
// than `docker cp` so that the scenario needs no temporary file on the host
// running it.
func (h *installerHost) write(path, body string) {
	h.t.Helper()
	cmd := exec.Command("docker", "exec", "-i", h.name, "sh", "-c", "cat > "+path)
	cmd.Stdin = strings.NewReader(body)
	if out, err := cmd.CombinedOutput(); err != nil {
		h.t.Fatalf("writing %s on the throwaway host: %v\n%s", path, err, out)
	}
}

// startController runs the controller the install configured, in the
// background, reading the configuration the installer wrote and nothing else.
// Passing no flags is the point: what is under test is whether the file the
// installer produced is enough to run from.
func (h *installerHost) startController(t *testing.T) {
	t.Helper()
	// The PID is recorded rather than found again later with pkill, which is
	// in procps and so is not in a slim base image. Depending on it would
	// make this scenario fail on the image it names by default.
	out, err := h.run("sh", "-c",
		"nohup zoomies controller --config "+sysConfig+" > /tmp/controller.log 2>&1 & echo $! > /tmp/controller.pid")
	if err != nil {
		t.Fatalf("starting the installed controller: %v\n%s", err, out)
	}
}

// stopController stops it again, so that uninstall is not asked to delete a
// database out from under a running process.
func (h *installerHost) stopController(t *testing.T) {
	t.Helper()
	if out, err := h.run("sh", "-c", "kill -TERM \"$(cat /tmp/controller.pid)\" 2>/dev/null || true"); err != nil {
		t.Logf("stopping the installed controller: %v\n%s", err, out)
	}
	// The listener has to be gone before uninstall runs, and a controller
	// stops in well under a second once it has been asked to.
	time.Sleep(2 * time.Second)
	if log, err := h.run("cat", "/tmp/controller.log"); err == nil && t.Failed() {
		t.Logf("installed controller log:\n%s", log)
	}
}
