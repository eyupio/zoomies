package backend

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net"
	"net/http"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/eyupio/zoomies/internal/store"
)

// The sidecar is where a job's pulls happen, so it is where the mirror is named:
// the daemon is started with --registry-mirror, and an address behind a proxy is
// kept out of the proxy, because dockerd sends it through one otherwise and a
// proxy cannot reach a host's private address. These tests hold what is named
// and when, and that a mirror which cannot be offered is never why a runner is
// not created.

const mirrorAddress = "http://172.17.0.1:41234"

var defaultDinDCommand = []string{"dockerd", "--host=tcp://127.0.0.1:2375", "--host=unix:///var/run/docker.sock"}

func mirrorFlag(cmd []string) string {
	for _, a := range cmd {
		if strings.HasPrefix(a, "--registry-mirror=") {
			return strings.TrimPrefix(a, "--registry-mirror=")
		}
	}
	return ""
}

func TestADockerSidecarIsToldWhereTheMirrorIs(t *testing.T) {
	cfg := buildDinDConfig(jitSpec(), dockerFlavor(), containerOptions{Now: time.Now(), DinDImage: DefaultDinDImage, RegistryMirrorURL: mirrorAddress})
	if got := mirrorFlag(cfg.Cmd); got != mirrorAddress {
		t.Fatalf("command = %v, want --registry-mirror=%s", cfg.Cmd, mirrorAddress)
	}
	// What it was started with before is still there, in the same order.
	if !slices.Equal(cfg.Cmd[:len(defaultDinDCommand)], defaultDinDCommand) {
		t.Fatalf("command = %v, want it to start with %v", cfg.Cmd, defaultDinDCommand)
	}
}

func TestASidecarThatIsOfferedNoMirrorIsStartedExactlyAsItWas(t *testing.T) {
	cfg := buildDinDConfig(jitSpec(), dockerFlavor(), containerOptions{Now: time.Now(), DinDImage: DefaultDinDImage})
	if !slices.Equal(cfg.Cmd, defaultDinDCommand) {
		t.Fatalf("command = %v, want %v: a mirror that is not offered changes nothing", cfg.Cmd, defaultDinDCommand)
	}
	for _, e := range cfg.Env {
		if strings.HasPrefix(strings.ToUpper(e), "NO_PROXY=") {
			t.Fatalf("env = %v, want no NO_PROXY added for a mirror that is not offered", cfg.Env)
		}
	}
}

// dockerd honours the proxy variables for the mirror's address as for any other
// registry, and a proxy cannot reach the host's private address, so a mirror
// that is not exempt is a mirror that never answers. Both spellings are set
// because the daemon and the tools around it disagree about which they read.
func TestTheMirrorsAddressIsKeptOutOfTheProxy(t *testing.T) {
	for _, tc := range []struct {
		name    string
		agent   map[string]string
		pool    map[string]string
		wantNo  string // the value of NO_PROXY and no_proxy; empty means neither is set
		wantSet bool
	}{
		{"a proxy and no exemptions", map[string]string{"HTTPS_PROXY": "http://proxy:3128"}, nil, "172.17.0.1", true},
		{"a proxy and an exemption of its own", map[string]string{"HTTPS_PROXY": "http://proxy:3128", "NO_PROXY": ".corp"}, nil, ".corp,172.17.0.1", true},
		{"the lower case spellings", map[string]string{"https_proxy": "http://proxy:3128", "no_proxy": ".corp"}, nil, ".corp,172.17.0.1", true},
		{"the pool's exemption wins over the agent's", map[string]string{"HTTPS_PROXY": "http://proxy:3128", "NO_PROXY": ".agent"}, map[string]string{"NO_PROXY": ".pool"}, ".pool,172.17.0.1", true},
		{"already exempt", map[string]string{"HTTP_PROXY": "http://proxy:3128", "NO_PROXY": "172.17.0.1,.corp"}, nil, "172.17.0.1,.corp", true},
		{"no proxy at all", nil, nil, "", false},
		{"a pool that opts out of the proxy", map[string]string{"HTTPS_PROXY": "http://proxy:3128"}, map[string]string{"HTTPS_PROXY": "", "HTTP_PROXY": ""}, "", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			spec := jitSpec()
			spec.Env = tc.pool
			o := containerOptions{
				Now: time.Now(), DinDImage: DefaultDinDImage, RegistryMirrorURL: mirrorAddress,
				ProxyEnv: inheritedProxyEnv(proxyLookup(tc.agent), spec.Env),
			}
			env := buildDinDConfig(spec, dockerFlavor(), o).Env
			for _, key := range []string{"NO_PROXY", "no_proxy"} {
				var seen []string
				for _, e := range env {
					if k, v, _ := strings.Cut(e, "="); k == key {
						seen = append(seen, v)
					}
				}
				switch {
				case !tc.wantSet && len(seen) > 0 && seen[len(seen)-1] != "":
					t.Errorf("%s = %v, want it unset: nothing here is behind a proxy", key, seen)
				case tc.wantSet && (len(seen) != 1 || seen[0] != tc.wantNo):
					t.Errorf("%s = %v, want exactly one entry, %q (the daemon keeps the last of a repeated name, so a second one would hide the first)", key, seen, tc.wantNo)
				}
			}
		})
	}
}

// createADinDRunner creates a docker-in-docker runner against a fake daemon and
// returns the body its sidecar was created with, so what the sidecar is started
// with is what the create path decided and not what a test handed the builder.
func createADinDRunner(t *testing.T, opts DockerOptions, mutate func(*Spec)) (*DockerBackend, ContainerCreateRequest, error) {
	t.Helper()
	notInAContainer(t)
	const sidecarID = "5a3d9e1c7b2f4a6d8c0e2f4a6b8d0c2e4f6a8b0d2c4e6f8a0b2d4c6e8f0a2b4d"
	const runnerID = "7c2b4d6e8f0a2c4e6f8a0b2d4f6a8c0e2f4a6b8d0c2e4f6a8b0d2c4e6f8a0b2d"
	spec := jitSpec()
	spec.DockerMode = store.DockerDinD
	if mutate != nil {
		mutate(&spec)
	}
	runner := containerName(spec.Name)
	sidecar := dindName(runner)
	var (
		mu          sync.Mutex
		sidecarBody ContainerCreateRequest
		created     bool
		started     = map[string]bool{}
	)
	f := newFakeEngine(t, map[string]http.HandlerFunc{
		"GET " + v + "/images/{ref...}": func(w http.ResponseWriter, r *http.Request) {
			writeJSON(w, 200, map[string]string{"Id": "sha256:cached"})
		},
		// The host's address on the sidecar's network: loopback, so the mirror
		// can really listen on it.
		"GET " + v + "/networks/{name}": func(w http.ResponseWriter, r *http.Request) {
			writeJSON(w, 200, map[string]any{"IPAM": map[string]any{"Config": []map[string]string{{"Subnet": "127.0.0.0/8", "Gateway": "127.0.0.1"}}}})
		},
		"POST " + v + "/containers/create": func(w http.ResponseWriter, r *http.Request) {
			var body ContainerCreateRequest
			_ = json.NewDecoder(r.Body).Decode(&body)
			switch r.Form.Get("name") {
			case sidecar:
				mu.Lock()
				sidecarBody, created = body, true
				mu.Unlock()
				writeJSON(w, 201, map[string]string{"Id": sidecarID})
			case runner:
				writeJSON(w, 201, map[string]string{"Id": runnerID})
			default:
				w.WriteHeader(500)
			}
		},
		"GET " + v + "/containers/{id}/json": func(w http.ResponseWriter, r *http.Request) {
			mu.Lock()
			defer mu.Unlock()
			switch id := r.PathValue("id"); id {
			case sidecarID, sidecar:
				// Not there until it has been created, as on a real daemon: a name
				// that already answers is a name somebody else holds.
				if !created {
					w.WriteHeader(404)
					return
				}
				insp := echoInspect(sidecarID, sidecar, sidecarBody, "")
				if started[sidecarID] {
					insp.State = &ContainerState{Status: "running", Running: true, Health: &ContainerHealth{Status: "healthy"}}
				}
				writeJSON(w, 200, insp)
			case runnerID:
				writeJSON(w, 200, ContainerInspect{ID: runnerID, State: &ContainerState{Status: "running", Running: true}})
			default:
				w.WriteHeader(404)
			}
		},
		"POST " + v + "/containers/{id}/start": func(w http.ResponseWriter, r *http.Request) {
			mu.Lock()
			started[r.PathValue("id")] = true
			mu.Unlock()
			w.WriteHeader(204)
		},
	})
	b := dockerBackendFor(t, f, opts)
	t.Cleanup(b.Close)
	_, err := b.CreateWithResult(context.Background(), spec)
	mu.Lock()
	defer mu.Unlock()
	return b, sidecarBody, err
}

func TestASidecarOnTheDefaultImageIsGivenTheHostsMirror(t *testing.T) {
	b, sidecar, err := createADinDRunner(t, DockerOptions{RegistryMirror: true, SharedDir: t.TempDir()}, nil)
	if err != nil {
		t.Fatal(err)
	}
	addr := mirrorFlag(sidecar.Cmd)
	if !strings.HasPrefix(addr, "http://127.0.0.1:") {
		t.Fatalf("command = %v, want --registry-mirror on the host's address on the sidecar's network", sidecar.Cmd)
	}
	resp, err := http.Get(addr + "/v2/")
	if err != nil {
		t.Fatalf("the address the sidecar was given does not answer: %v", err)
	}
	resp.Body.Close()
	if b.regMirror.stats() == nil {
		t.Fatal("the host has no mirror running")
	}
}

// A custom image is where an operator keeps a daemon.json of their own, and a
// mirror named in two places stops the daemon starting, so a sidecar that runs
// one is left exactly as the operator made it.
func TestASidecarOnACustomImageIsGivenNoMirror(t *testing.T) {
	const custom = "registry.example.com/ci/dind:27"
	for name, tc := range map[string]struct {
		opts   DockerOptions
		mutate func(*Spec)
	}{
		"the host's image":       {DockerOptions{DinDImage: custom}, nil},
		"the pool's own image":   {DockerOptions{}, func(s *Spec) { s.DinDImage = custom }},
		"the pool's over host's": {DockerOptions{DinDImage: "registry.example.com/other:1"}, func(s *Spec) { s.DinDImage = custom }},
	} {
		t.Run(name, func(t *testing.T) {
			tc.opts.RegistryMirror, tc.opts.SharedDir = true, t.TempDir()
			b, sidecar, err := createADinDRunner(t, tc.opts, tc.mutate)
			if err != nil {
				t.Fatal(err)
			}
			if got := mirrorFlag(sidecar.Cmd); got != "" {
				t.Fatalf("command = %v, want no mirror beside an image the operator chose", sidecar.Cmd)
			}
			if !slices.Equal(sidecar.Cmd, defaultDinDCommand) {
				t.Fatalf("command = %v, want it untouched: %v", sidecar.Cmd, defaultDinDCommand)
			}
			if b.regMirror.stats() != nil {
				t.Fatal("a mirror was started for a host whose only sidecar uses a custom image")
			}
		})
	}
}

// The size an operator set has to be the size the mirror is started with, and
// the mirror is bounded the moment it exists, so a setting that stopped at the
// backend's door would leave every host on the default without saying so. The
// start-up line names the bound it was given.
func TestTheSizeAnOperatorSetIsTheSizeTheMirrorIsStartedWith(t *testing.T) {
	for name, tc := range map[string]struct {
		set  int64
		want string
	}{
		"a size set":       {20 << 30, "limit_bytes=21474836480"},
		"the default size": {0, "limit_bytes=10737418240"},
	} {
		t.Run(name, func(t *testing.T) {
			var logs bytes.Buffer
			_, _, err := createADinDRunner(t, DockerOptions{
				RegistryMirror: true, RegistryMirrorMaxBytes: tc.set, SharedDir: t.TempDir(),
				Logger: slog.New(slog.NewTextHandler(&logs, nil)),
			}, nil)
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(logs.String(), tc.want) {
				t.Fatalf("the mirror was not started with %s:\n%s", tc.want, logs.String())
			}
		})
	}
}

// A listener nobody will ever stop is a listener on the host's private address
// that outlives the agent that opened it, so closing the backend closes the
// mirror along with the cache services.
func TestClosingTheBackendStopsItsMirror(t *testing.T) {
	b, sidecar, err := createADinDRunner(t, DockerOptions{RegistryMirror: true, SharedDir: t.TempDir()}, nil)
	if err != nil {
		t.Fatal(err)
	}
	addr := strings.TrimPrefix(mirrorFlag(sidecar.Cmd), "http://")
	if conn, err := net.DialTimeout("tcp", addr, time.Second); err != nil {
		t.Fatalf("the mirror is not listening before the backend is closed: %v", err)
	} else {
		conn.Close()
	}
	b.Close()
	deadline := time.Now().Add(2 * time.Second)
	for {
		conn, err := net.DialTimeout("tcp", addr, 200*time.Millisecond)
		if err != nil {
			return
		}
		conn.Close()
		if time.Now().After(deadline) {
			t.Fatal("the mirror still accepts connections after the backend was closed")
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func TestASidecarIsGivenNoMirrorWhileTheMirrorIsOff(t *testing.T) {
	b, sidecar, err := createADinDRunner(t, DockerOptions{RegistryMirror: false, SharedDir: t.TempDir()}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(sidecar.Cmd, defaultDinDCommand) {
		t.Fatalf("command = %v, want %v", sidecar.Cmd, defaultDinDCommand)
	}
	if b.regMirror.stats() != nil {
		t.Fatal("a mirror was started while the setting is off")
	}
}

// The mirror only slows a pull down when it is missing, so the one thing it may
// never do is stop a runner being made.
func TestAMirrorThatCannotBeOfferedDoesNotFailTheCreate(t *testing.T) {
	_, sidecar, err := createADinDRunner(t, DockerOptions{RegistryMirror: true, SharedDir: ""}, nil)
	if err != nil {
		t.Fatalf("a host with no shared folder failed the create: %v", err)
	}
	if !slices.Equal(sidecar.Cmd, defaultDinDCommand) {
		t.Fatalf("command = %v, want the sidecar started without a mirror it was never given", sidecar.Cmd)
	}
}

func TestTheSidecarsProxyExemptsTheMirrorThatIsActuallyOffered(t *testing.T) {
	t.Setenv("HTTPS_PROXY", "http://proxy.corp:3128")
	t.Setenv("NO_PROXY", ".corp")
	_, sidecar, err := createADinDRunner(t, DockerOptions{RegistryMirror: true, SharedDir: t.TempDir()}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := lastValue(sidecar.Env, "NO_PROXY"); got != ".corp,127.0.0.1" {
		t.Fatalf("NO_PROXY = %q in %v, want the host's address added to the exemption the agent had", got, sidecar.Env)
	}
}
