package backend

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/eyupio/zoomies/internal/store"
)

// A host behind a TLS-intercepting proxy hands its runners the proxy's CA.
// The runner needs the file where the entrypoint looks for it, and read-only,
// because the source is the host's file and a job must not write through it
// to what every later runner on the host trusts.
func TestARunnerIsGivenTheHostsExtraCAReadOnly(t *testing.T) {
	o := containerOptions{Now: time.Now(), ExtraCAFile: "/etc/acme/proxy-ca.pem"}
	spec := jitSpec()
	spec.Env = map[string]string{EnvExtraCAFile: "/somewhere/else"}
	cfg := buildRunnerConfig(spec, dockerFlavor(), o)

	if !slices.Contains(cfg.HostConfig.Binds, "/etc/acme/proxy-ca.pem:"+ExtraCAPath+":ro") {
		t.Fatalf("binds = %v, want the CA mounted read-only at %s", cfg.HostConfig.Binds, ExtraCAPath)
	}
	// A pool's env comes earlier, so the mount's path is the one that wins.
	if got := envMap(cfg.Env)[EnvExtraCAFile]; got != ExtraCAPath {
		t.Fatalf("%s = %q, want %q", EnvExtraCAFile, got, ExtraCAPath)
	}
}

func TestPodmanRelabelsTheExtraCAMount(t *testing.T) {
	o := containerOptions{Now: time.Now(), ExtraCAFile: "/etc/acme/proxy-ca.pem"}
	cfg := buildRunnerConfig(jitSpec(), podmanFlavor(), o)
	if !slices.Contains(cfg.HostConfig.Binds, "/etc/acme/proxy-ca.pem:"+ExtraCAPath+":ro,z") {
		t.Fatalf("binds = %v, want a read-only relabelled mount", cfg.HostConfig.Binds)
	}
}

// Nothing changes for a host that did not ask: the setting is new, and an
// upgrade must leave every existing runner's container exactly as it was.
func TestARunnerWithoutAnExtraCAGetsNoMountAndNoVariable(t *testing.T) {
	cfg := buildRunnerConfig(jitSpec(), dockerFlavor(), containerOptions{Now: time.Now()})
	for _, b := range cfg.HostConfig.Binds {
		if strings.Contains(b, ExtraCADir) {
			t.Fatalf("unexpected CA mount %q", b)
		}
	}
	if _, ok := envMap(cfg.Env)[EnvExtraCAFile]; ok {
		t.Fatalf("%s set with no extra CA configured", EnvExtraCAFile)
	}
	dind := buildDinDConfig(jitSpec(), dockerFlavor(), containerOptions{Now: time.Now(), DinDImage: DefaultDinDImage})
	if _, ok := envMap(dind.Env)["SSL_CERT_DIR"]; ok {
		t.Fatal("the sidecar's SSL_CERT_DIR was set with no extra CA configured")
	}
}

// The sidecar is what pulls a dind job's images, so it must trust the proxy
// too, and it must keep the image's own roots while it does.
func TestTheDockerSidecarTrustsTheExtraCAAlongsideItsOwnRoots(t *testing.T) {
	spec := jitSpec()
	spec.DockerMode = store.DockerDinD
	cfg := buildDinDConfig(spec, dockerFlavor(), containerOptions{Now: time.Now(), DinDImage: DefaultDinDImage, ExtraCAFile: "/etc/acme/proxy-ca.pem"})
	if !slices.Contains(cfg.HostConfig.Binds, "/etc/acme/proxy-ca.pem:"+ExtraCAPath+":ro") {
		t.Fatalf("binds = %v", cfg.HostConfig.Binds)
	}
	if got := envMap(cfg.Env)["SSL_CERT_DIR"]; got != "/etc/ssl/certs:"+ExtraCADir {
		t.Fatalf("SSL_CERT_DIR = %q", got)
	}
}

// The entrypoint is the other half: run the real script with stubbed trust
// store tools and check the CA reaches the store and the listener's
// environment before the listener starts.
func TestTheEntrypointTrustsTheExtraCABeforeStartingItsListener(t *testing.T) {
	bash, err := exec.LookPath("bash")
	if err != nil {
		t.Skip("bash is not installed")
	}
	source, err := os.ReadFile("../../deploy/runner-entrypoint.sh")
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name    string
		ca      bool // write the CA file the variable names
		setVar  bool
		exit    int
		trusted bool
	}{
		{"trusted", true, true, 0, true},
		{"unreadable", false, true, 78, false},
		{"not configured", false, false, 0, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			anchors := filepath.Join(dir, "anchors")
			if err := os.Mkdir(anchors, 0o700); err != nil {
				t.Fatal(err)
			}
			write := func(name, body string) {
				t.Helper()
				if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o700); err != nil {
					t.Fatal(err)
				}
			}
			script := strings.Replace(string(source), "cd /home/runner", "cd \"$TEST_RUNNER_HOME\"", 1)
			script = strings.Replace(script, "apt_anchors=/usr/local/share/ca-certificates", "apt_anchors=\"$TEST_RUNNER_HOME/anchors\"", 1)
			script = strings.Replace(script, "apt_bundle=/etc/ssl/certs/ca-certificates.crt", "apt_bundle=\"$TEST_RUNNER_HOME/bundle.crt\"", 1)
			write("entrypoint.sh", script)
			write("sudo", "#!/usr/bin/env bash\n[ \"$1\" = -n ] && shift\nexec \"$@\"\n")
			write("update-ca-certificates", "#!/usr/bin/env bash\ntouch \"$TEST_RUNNER_HOME/store-updated\"\n")
			write("run.sh", "#!/usr/bin/env bash\nenv > listener-env\n")
			ca := filepath.Join(dir, "proxy-ca.pem")
			if tc.ca {
				write("proxy-ca.pem", "-----BEGIN CERTIFICATE-----\n")
			}

			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			defer cancel()
			cmd := exec.CommandContext(ctx, bash, filepath.Join(dir, "entrypoint.sh"))
			cmd.Env = append(os.Environ(), "PATH="+dir+string(os.PathListSeparator)+os.Getenv("PATH"),
				"TEST_RUNNER_HOME="+dir, "ZOOMIES_JITCONFIG=test-only",
				// Empty counts as unset to the script; the machine running
				// the test may well have set these for its own proxy.
				"REQUESTS_CA_BUNDLE=", "SSL_CERT_FILE=", "NODE_EXTRA_CA_CERTS=")
			if tc.setVar {
				cmd.Env = append(cmd.Env, EnvExtraCAFile+"="+ca)
			}
			out, _ := cmd.CombinedOutput()
			if code := cmd.ProcessState.ExitCode(); code != tc.exit {
				t.Fatalf("exit code = %d, want %d: %s", code, tc.exit, out)
			}
			env, envErr := os.ReadFile(filepath.Join(dir, "listener-env"))
			if tc.exit != 0 {
				if envErr == nil {
					t.Fatalf("the listener started without its CA: %s", out)
				}
				return
			}
			_, copyErr := os.Stat(filepath.Join(anchors, "zoomies-extra-ca.crt"))
			_, storeErr := os.Stat(filepath.Join(dir, "store-updated"))
			if (copyErr == nil) != tc.trusted || (storeErr == nil) != tc.trusted {
				t.Fatalf("copied=%v store updated=%v, want %v: %s", copyErr == nil, storeErr == nil, tc.trusted, out)
			}
			for _, want := range []string{"NODE_EXTRA_CA_CERTS=" + ca, "REQUESTS_CA_BUNDLE=" + filepath.Join(dir, "bundle.crt")} {
				if strings.Contains(string(env), want+"\n") != tc.trusted {
					t.Fatalf("listener env has %q = %v, want %v", want, !tc.trusted, tc.trusted)
				}
			}
		})
	}
}
