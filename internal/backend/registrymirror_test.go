package backend

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// A host that turns the registry mirror on keeps one mirror, on the host's own
// address on each network its sidecars are attached to. These tests give the
// host's address as loopback so no daemon is needed, and give the mirror a
// registry of their own that knows nothing, so none of them reaches Docker Hub.
// They hold what the mirror must and must not be: offered only when asked for,
// one for the host however many networks it serves, and never the reason a
// create fails.

// emptyRegistry is an upstream that has a token service and no images, so a
// request for any image is answered and counted without leaving the machine.
func emptyRegistry(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/token" {
			_, _ = w.Write([]byte(`{"token":"t","expires_in":300}`))
			return
		}
		http.NotFound(w, r)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func newMirrorHost(t *testing.T, shared string, gateway func(context.Context, string) (string, error)) (*registryMirror, *bytes.Buffer) {
	t.Helper()
	notInAContainer(t)
	hub := emptyRegistry(t)
	var logs bytes.Buffer
	m := newRegistryMirror(registryMirrorOptions{
		Enabled:   true,
		SharedDir: shared,
		Log:       slog.New(slog.NewTextHandler(&logs, nil)),
		Gateway:   gateway,
		Upstream:  hub.URL,
		TokenURL:  hub.URL + "/token",
		Service:   "test",
	})
	t.Cleanup(m.close)
	return m, &logs
}

// askAboutAnImage sends a mirror a request it counts, which is how a test tells
// that it reached the mirror and not just an open port.
func askAboutAnImage(t *testing.T, url string) {
	t.Helper()
	resp, err := http.Get(url + "/v2/library/alpine/manifests/latest")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
}

func TestAHostWithTheMirrorOnListensOnItsGatewayAndIsARegistry(t *testing.T) {
	m, _ := newMirrorHost(t, t.TempDir(), loopback)
	url := m.urlFor(context.Background(), "")
	if !strings.HasPrefix(url, "http://127.0.0.1:") || strings.HasSuffix(url, "/") {
		t.Fatalf("url = %q, want the host's address and a port, with no trailing slash (dockerd is given it as --registry-mirror)", url)
	}
	resp, err := http.Get(url + "/v2/")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK || resp.Header.Get("Docker-Distribution-API-Version") != "registry/2.0" {
		t.Fatalf("GET /v2/ = %d %q, want a registry's ping", resp.StatusCode, resp.Header.Get("Docker-Distribution-API-Version"))
	}
}

func TestNothingIsOfferedOrCreatedWhileTheMirrorIsOff(t *testing.T) {
	notInAContainer(t)
	shared := t.TempDir()
	m := newRegistryMirror(registryMirrorOptions{
		Enabled: false, SharedDir: shared, Log: slog.New(slog.NewTextHandler(&bytes.Buffer{}, nil)),
		Gateway: func(context.Context, string) (string, error) {
			t.Error("the host was asked for an address while the mirror is off")
			return "127.0.0.1", nil
		},
	})
	t.Cleanup(m.close)
	if url := m.urlFor(context.Background(), ""); url != "" {
		t.Fatalf("offered %q with the mirror off", url)
	}
	if _, err := os.Stat(filepath.Join(shared, "registry-mirror")); !os.IsNotExist(err) {
		t.Fatalf("a folder was made for a mirror that is off: %v", err)
	}
	if got := m.stats(); got != nil {
		t.Fatalf("stats = %+v with the mirror off, want none so the heartbeat carries nothing", got)
	}
}

func TestTheMirrorKeepsWhatItHoldsUnderTheSharedFolder(t *testing.T) {
	shared := t.TempDir()
	m, _ := newMirrorHost(t, shared, loopback)
	if m.urlFor(context.Background(), "") == "" {
		t.Fatal("no mirror was offered")
	}
	if fi, err := os.Stat(filepath.Join(shared, "registry-mirror")); err != nil || !fi.IsDir() {
		t.Fatalf("the mirror keeps nothing under the shared folder: %v", err)
	}
}

// A second mirror over one folder would count every byte twice and sweep the
// first one's downloads away as leftovers, so a host has one however many
// networks it serves.
func TestTwoNetworksShareOneMirror(t *testing.T) {
	m, _ := newMirrorHost(t, t.TempDir(), func(_ context.Context, network string) (string, error) {
		if network == "second" {
			return "127.0.0.2", nil
		}
		return "127.0.0.1", nil
	})
	first := m.urlFor(context.Background(), "first")
	second := m.urlFor(context.Background(), "second")
	if first == "" || second == "" || first == second {
		t.Fatalf("addresses %q and %q, want a listener for each network", first, second)
	}
	askAboutAnImage(t, first)
	askAboutAnImage(t, second)
	if got := m.stats(); got == nil || got.Requests != 2 {
		t.Fatalf("stats = %+v, want both listeners' requests counted by one mirror", got)
	}
}

func TestTheSameNetworkIsGivenTheSameAddressEveryTime(t *testing.T) {
	m, _ := newMirrorHost(t, t.TempDir(), loopback)
	a, b := m.urlFor(context.Background(), ""), m.urlFor(context.Background(), "")
	if a == "" || a != b {
		t.Fatalf("two sidecars on one network were given %q and %q, want one listener", a, b)
	}
}

// Every way the host cannot offer a mirror is a sidecar that pulls from Docker
// Hub as it did before, with the reason said, and never an error: the mirror is
// only an accelerator.
func TestAHostThatCannotRunTheMirrorLeavesTheSidecarOnDockerHub(t *testing.T) {
	t.Run("no shared folder", func(t *testing.T) {
		m, logs := newMirrorHost(t, "", loopback)
		if url := m.urlFor(context.Background(), ""); url != "" {
			t.Fatalf("offered %q with nowhere to keep anything", url)
		}
		if !strings.Contains(logs.String(), "Docker Hub") {
			t.Fatalf("the reason was not logged: %s", logs.String())
		}
	})
	t.Run("the agent is in a container", func(t *testing.T) {
		m, _ := newMirrorHost(t, t.TempDir(), loopback)
		runningInContainer = func() bool { return true }
		if url := m.urlFor(context.Background(), ""); url != "" {
			t.Fatalf("offered %q from inside a container", url)
		}
	})
	t.Run("no address for the host on the network", func(t *testing.T) {
		m, _ := newMirrorHost(t, t.TempDir(), func(context.Context, string) (string, error) { return "", errors.New("no gateway") })
		if url := m.urlFor(context.Background(), ""); url != "" {
			t.Fatalf("offered %q with no address to listen on", url)
		}
	})
	t.Run("the address cannot be listened on", func(t *testing.T) {
		m, _ := newMirrorHost(t, t.TempDir(), func(context.Context, string) (string, error) { return "203.0.113.9", nil })
		if url := m.urlFor(context.Background(), ""); url != "" {
			t.Fatalf("offered %q on an address this host does not have", url)
		}
	})
	t.Run("the folder cannot be written", func(t *testing.T) {
		shared := t.TempDir()
		// A file where the mirror's folder has to go.
		if err := os.WriteFile(filepath.Join(shared, "registry-mirror"), []byte("not a folder"), 0o600); err != nil {
			t.Fatal(err)
		}
		m, logs := newMirrorHost(t, shared, loopback)
		if url := m.urlFor(context.Background(), ""); url != "" {
			t.Fatalf("offered %q with no folder to keep anything in", url)
		}
		if !strings.Contains(logs.String(), "Docker Hub") {
			t.Fatalf("the reason was not logged: %s", logs.String())
		}
	})
}

// Every sidecar asks, so a host that cannot offer a mirror would otherwise
// repeat itself for every job.
func TestTheReasonForNoMirrorIsSaidOncePerReason(t *testing.T) {
	m, logs := newMirrorHost(t, "", loopback)
	for i := 0; i < 5; i++ {
		m.urlFor(context.Background(), "")
	}
	if n := strings.Count(logs.String(), "Docker Hub"); n != 1 {
		t.Fatalf("the reason was logged %d times for five sidecars, want once:\n%s", n, logs.String())
	}
}

// A counter that falls is read downstream as the agent restarting, so a listener
// that stopped on its own and is started again must carry the mirror's totals on
// and not begin again from zero.
func TestTheMirrorsFiguresSurviveAListenerThatStoppedAndWasStartedAgain(t *testing.T) {
	m, _ := newMirrorHost(t, t.TempDir(), loopback)
	ctx := context.Background()
	url := m.urlFor(ctx, "")
	askAboutAnImage(t, url)

	m.mu.Lock()
	for key, r := range m.running {
		delete(m.running, key)
		_ = r.srv.Close()
	}
	m.mu.Unlock()

	again := m.urlFor(ctx, "")
	if again == "" || again == url {
		t.Fatalf("the listener was not started again (%q)", again)
	}
	askAboutAnImage(t, again)
	if got := m.stats(); got == nil || got.Requests != 2 {
		t.Fatalf("stats = %+v, want the first listener's request kept and the second's added", got)
	}
}

// A listener that fails on its own, as opposed to being closed by the agent, has
// to be forgotten, or the next sidecar would be handed the address of something
// that no longer answers and every pull on this host would wait for it to fail
// before falling back.
func TestAListenerThatStoppedOnItsOwnIsStartedAgainByTheNextSidecar(t *testing.T) {
	m, logs := newMirrorHost(t, t.TempDir(), loopback)
	ctx := context.Background()
	url := m.urlFor(ctx, "")
	askAboutAnImage(t, url)

	m.mu.Lock()
	var ln net.Listener
	for _, r := range m.running {
		ln = r.ln
	}
	m.mu.Unlock()
	// Closing the listener, not the server, is what makes Serve fail with an
	// error of its own and not the one a deliberate shutdown returns.
	_ = ln.Close()
	deadline := time.Now().Add(2 * time.Second)
	for {
		m.mu.Lock()
		forgotten := len(m.running) == 0
		m.mu.Unlock()
		if forgotten {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the mirror still holds a listener that has stopped")
		}
		time.Sleep(10 * time.Millisecond)
	}

	again := m.urlFor(ctx, "")
	if again == "" || again == url {
		t.Fatalf("the next sidecar was given %q, want a listener that answers", again)
	}
	askAboutAnImage(t, again)
	if got := m.stats(); got == nil || got.Requests != 2 {
		t.Fatalf("stats = %+v, want the totals to carry on across the restart", got)
	}
	if !strings.Contains(logs.String(), "will start it again") {
		t.Fatalf("a mirror that stopped on its own said nothing: %s", logs.String())
	}
}

func TestClosingStopsTheMirrorAndNothingStartsAfterwards(t *testing.T) {
	m, _ := newMirrorHost(t, t.TempDir(), loopback)
	url := m.urlFor(context.Background(), "")
	m.close()
	addr := strings.TrimPrefix(url, "http://")
	deadline := time.Now().Add(2 * time.Second)
	for {
		conn, err := net.DialTimeout("tcp", addr, 200*time.Millisecond)
		if err != nil {
			break
		}
		conn.Close()
		if time.Now().After(deadline) {
			t.Fatal("the mirror still accepts connections after close")
		}
		time.Sleep(20 * time.Millisecond)
	}
	// A sidecar being created as the agent shuts down would otherwise open a
	// listener nobody will ever stop.
	if again := m.urlFor(context.Background(), ""); again != "" {
		t.Fatalf("a mirror was started after close: %q", again)
	}
}

// Zero is how an operator says "the default", and there is no unbounded mirror,
// so the bound is always a positive size.
func TestTheMirrorsBoundIsTheSettingOrTenGiB(t *testing.T) {
	for _, tc := range []struct{ set, want int64 }{
		{0, 10 << 30},
		{1 << 30, 1 << 30},
		{20 << 30, 20 << 30},
	} {
		if got := registryMirrorBytes(tc.set); got != tc.want {
			t.Errorf("a setting of %d gives a bound of %d, want %d", tc.set, got, tc.want)
		}
	}
}
