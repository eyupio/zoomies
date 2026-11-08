package backend

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/eyupio/zoomies/internal/store"
)

// A pool that turns its cache service on gets one service on the host, on the
// host's own address on the network its runners are attached to. These tests
// give the host's address as loopback so no daemon is needed, and hold what the
// service must and must not be: shared with exactly the runners the pool cache
// is shared with, reachable, and never the reason a create fails.

func serviceSpec(pool string) Spec {
	spec := jitSpec()
	spec.PoolID = pool
	spec.PoolName = pool
	spec.Cache = store.CacheConfig{Enabled: true, Scope: store.CacheScopePool, Service: true}
	return spec
}

func loopback(context.Context, string) (string, error) { return "127.0.0.1", nil }

func notInAContainer(t *testing.T) {
	t.Helper()
	was := runningInContainer
	runningInContainer = func() bool { return false }
	t.Cleanup(func() { runningInContainer = was })
}

func newServices(t *testing.T, shared string, gateway func(context.Context, string) (string, error)) (*cacheServices, *bytes.Buffer) {
	t.Helper()
	notInAContainer(t)
	var logs bytes.Buffer
	c := newCacheServices(shared, "", slog.New(slog.NewTextHandler(&logs, nil)), gateway)
	t.Cleanup(c.close)
	return c, &logs
}

func TestAPoolsCacheServiceListensOnTheHostAndAnswers(t *testing.T) {
	c, _ := newServices(t, t.TempDir(), loopback)
	url := c.urlFor(context.Background(), serviceSpec("pool_1"), "")
	if !strings.HasPrefix(url, "http://127.0.0.1:") || !strings.HasSuffix(url, "/") {
		t.Fatalf("url = %q, want the host's address, a port and a trailing slash (the toolkit joins an absolute path to it)", url)
	}
	// A call with no token is refused by the cache protocol itself, which is how
	// this knows it reached the service and not just an open port.
	resp, err := http.Post(url+"twirp/github.actions.results.api.v1.CacheService/GetCacheEntryDownloadURL", "application/json", strings.NewReader(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var body struct{ Code string }
	_ = json.NewDecoder(resp.Body).Decode(&body)
	if resp.StatusCode != http.StatusUnauthorized || body.Code != "unauthenticated" {
		t.Fatalf("answered %d %q, want the cache service's own refusal", resp.StatusCode, body.Code)
	}
}

func TestRunnersOfOnePoolShareOneServiceAndAnotherPoolHasItsOwn(t *testing.T) {
	shared := t.TempDir()
	c, _ := newServices(t, shared, loopback)
	ctx := context.Background()
	a1 := c.urlFor(ctx, serviceSpec("pool_a"), "")
	a2 := c.urlFor(ctx, serviceSpec("pool_a"), "")
	b := c.urlFor(ctx, serviceSpec("pool_b"), "")
	if a1 == "" || a1 != a2 {
		t.Fatalf("two runners of one pool were given %q and %q, want one service", a1, a2)
	}
	if b == "" || b == a1 {
		t.Fatalf("another pool was given %q, want a service of its own, not %q", b, a1)
	}
	// Each keeps its entries under the identity the pool cache uses, so the two
	// are shared with the same runners and no others.
	for _, pool := range []string{"pool_a", "pool_b"} {
		id, err := cacheIdentity(serviceSpec(pool))
		if err != nil {
			t.Fatal(err)
		}
		if fi, err := os.Stat(filepath.Join(shared, "cache", "service", id, "blobs")); err != nil || !fi.IsDir() {
			t.Errorf("pool %s keeps nothing under its cache identity: %v", pool, err)
		}
	}
}

func TestARepositoryScopedServiceIsNotSharedWithAnotherRepository(t *testing.T) {
	c, _ := newServices(t, t.TempDir(), loopback)
	one, two := serviceSpec("pool_1"), serviceSpec("pool_1")
	one.Cache.Scope, one.Cache.Repository = store.CacheScopeRepository, "acme/widgets"
	two.Cache.Scope, two.Cache.Repository = store.CacheScopeRepository, "acme/gadgets"
	a, b := c.urlFor(context.Background(), one, ""), c.urlFor(context.Background(), two, "")
	if a == "" || b == "" || a == b {
		t.Fatalf("two repositories were given %q and %q, want a service each", a, b)
	}
}

func TestNoServiceIsOfferedUnlessThePoolAskedForOne(t *testing.T) {
	c, _ := newServices(t, t.TempDir(), func(context.Context, string) (string, error) {
		t.Error("the host was asked for an address for a pool that wants no service")
		return "127.0.0.1", nil
	})
	for name, mutate := range map[string]func(*Spec){
		"service off": func(s *Spec) { s.Cache.Service = false },
		"cache off":   func(s *Spec) { s.Cache.Enabled = false },
	} {
		spec := serviceSpec("pool_1")
		mutate(&spec)
		if url := c.urlFor(context.Background(), spec, ""); url != "" {
			t.Errorf("%s: offered %q", name, url)
		}
	}
}

// Every way the host cannot offer a service is a runner on GitHub's cache, with
// the reason said once, and never an error: the cache is only an accelerator.
func TestAHostThatCannotRunTheServiceLeavesTheRunnerOnGitHubsCache(t *testing.T) {
	t.Run("no shared folder", func(t *testing.T) {
		c, logs := newServices(t, "", loopback)
		if url := c.urlFor(context.Background(), serviceSpec("pool_1"), ""); url != "" {
			t.Fatalf("offered %q with nowhere to keep entries", url)
		}
		if !strings.Contains(logs.String(), "GitHub's cache") {
			t.Fatalf("the reason was not logged: %s", logs.String())
		}
	})
	t.Run("the agent is in a container", func(t *testing.T) {
		c, _ := newServices(t, t.TempDir(), loopback)
		runningInContainer = func() bool { return true }
		if url := c.urlFor(context.Background(), serviceSpec("pool_1"), ""); url != "" {
			t.Fatalf("offered %q from inside a container", url)
		}
	})
	t.Run("no address for the host on the network", func(t *testing.T) {
		c, _ := newServices(t, t.TempDir(), func(context.Context, string) (string, error) { return "", errors.New("no gateway") })
		if url := c.urlFor(context.Background(), serviceSpec("pool_1"), ""); url != "" {
			t.Fatalf("offered %q with no address to listen on", url)
		}
	})
	t.Run("the address cannot be listened on", func(t *testing.T) {
		c, _ := newServices(t, t.TempDir(), func(context.Context, string) (string, error) { return "203.0.113.9", nil })
		if url := c.urlFor(context.Background(), serviceSpec("pool_1"), ""); url != "" {
			t.Fatalf("offered %q on an address this host does not have", url)
		}
	})
}

func TestTheReasonForNoServiceIsSaidOncePerPool(t *testing.T) {
	c, logs := newServices(t, "", loopback)
	for i := 0; i < 5; i++ {
		c.urlFor(context.Background(), serviceSpec("pool_1"), "")
	}
	if n := strings.Count(logs.String(), "GitHub's cache"); n != 1 {
		t.Fatalf("the reason was logged %d times for five runners, want once:\n%s", n, logs.String())
	}
}

func TestClosingStopsTheServices(t *testing.T) {
	c, _ := newServices(t, t.TempDir(), loopback)
	url := c.urlFor(context.Background(), serviceSpec("pool_1"), "")
	c.close()
	addr := strings.TrimSuffix(strings.TrimPrefix(url, "http://"), "/")
	deadline := time.Now().Add(2 * time.Second)
	for {
		conn, err := net.DialTimeout("tcp", addr, 200*time.Millisecond)
		if err != nil {
			return
		}
		conn.Close()
		if time.Now().After(deadline) {
			t.Fatal("the service still accepts connections after close")
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func TestARunnerIsToldWhereItsCacheServiceIs(t *testing.T) {
	spec := jitSpec()
	cfg := buildRunnerConfig(spec, dockerFlavor(), containerOptions{Now: time.Now(), CacheServiceURL: "http://172.17.0.1:41234/"})
	if got := envMap(cfg.Env)[EnvCacheServiceURL]; got != "http://172.17.0.1:41234/" {
		t.Fatalf("%s = %q", EnvCacheServiceURL, got)
	}
	// A pool's own env comes earlier, so it cannot send the runner elsewhere.
	spec.Env = map[string]string{EnvCacheServiceURL: "http://attacker.invalid/"}
	cfg = buildRunnerConfig(spec, dockerFlavor(), containerOptions{Now: time.Now(), CacheServiceURL: "http://172.17.0.1:41234/"})
	if got := lastValue(cfg.Env, EnvCacheServiceURL); got != "http://172.17.0.1:41234/" {
		t.Fatalf("a pool's env won: %s = %q", EnvCacheServiceURL, got)
	}
}

func TestARunnerWithoutACacheServiceGetsNoVariable(t *testing.T) {
	cfg := buildRunnerConfig(jitSpec(), dockerFlavor(), containerOptions{Now: time.Now()})
	if v, ok := envMap(cfg.Env)[EnvCacheServiceURL]; ok {
		t.Fatalf("%s = %q with no service, so the wrapper must see it unset", EnvCacheServiceURL, v)
	}
}

// lastValue is what the daemon does with a repeated name: the last one wins.
func lastValue(env []string, name string) string {
	val := ""
	for _, e := range env {
		if k, v, _ := strings.Cut(e, "="); k == name {
			val = v
		}
	}
	return val
}
