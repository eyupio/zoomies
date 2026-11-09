package backend

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"path/filepath"
	"testing"

	"github.com/eyupio/zoomies/internal/cacheservice"
	"github.com/eyupio/zoomies/internal/store"
)

// twirpCall makes one cache protocol call as a job would, with a token the
// service reads as naming no scopes, which is enough to be counted.
func twirpCall(t *testing.T, base, method string, body any) map[string]any {
	t.Helper()
	raw, _ := json.Marshal(body)
	req, err := http.NewRequest(http.MethodPost, base+"twirp/github.actions.results.api.v1.CacheService/"+method, bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer job")
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var out map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&out)
	return out
}

// saveEntry does a whole small save through the service's own address.
func saveEntry(t *testing.T, base, key string, data []byte) {
	t.Helper()
	out := twirpCall(t, base, "CreateCacheEntry", map[string]string{"key": key, "version": "v"})
	put, _ := http.NewRequest(http.MethodPut, out["signed_upload_url"].(string), bytes.NewReader(data))
	resp, err := http.DefaultClient.Do(put)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	fin := twirpCall(t, base, "FinalizeCacheEntryUpload", map[string]any{"key": key, "version": "v", "sizeBytes": len(data)})
	if fin["ok"] != true {
		t.Fatalf("the save of %q did not finish: %v", key, fin)
	}
}

func restoreFinds(t *testing.T, base, key string) bool {
	t.Helper()
	return twirpCall(t, base, "GetCacheEntryDownloadURL", map[string]any{"key": key, "version": "v"})["ok"] == true
}

// What a pool's services have done is reported under the pool, summed over the
// services it has: a repository-scoped pool runs one for each repository, and
// the controller labels the figures by pool, not by folder.
func TestCacheStatsAreReportedByPoolAndSummedAcrossItsServices(t *testing.T) {
	c, _ := newServices(t, t.TempDir(), loopback)
	ctx := context.Background()
	one, two := serviceSpec("pool_b"), serviceSpec("pool_b")
	one.Cache.Scope, one.Cache.Repository = store.CacheScopeRepository, "acme/widgets"
	two.Cache.Scope, two.Cache.Repository = store.CacheScopeRepository, "acme/gadgets"
	urlOne, urlTwo := c.urlFor(ctx, one, ""), c.urlFor(ctx, two, "")
	urlA := c.urlFor(ctx, serviceSpec("pool_a"), "")
	if urlOne == "" || urlTwo == "" || urlA == "" {
		t.Fatal("a service did not start")
	}

	saveEntry(t, urlOne, "k1", []byte("12345"))
	saveEntry(t, urlTwo, "k2", []byte("1234567"))
	saveEntry(t, urlA, "k3", []byte("123"))
	if !restoreFinds(t, urlOne, "k1") || restoreFinds(t, urlTwo, "k1") {
		t.Fatal("the services did not behave as separate caches")
	}

	got := c.stats()
	if len(got) != 2 || got[0].PoolID != "pool_a" || got[1].PoolID != "pool_b" {
		t.Fatalf("stats = %+v, want pool_a then pool_b", got)
	}
	a, b := got[0].Stats, got[1].Stats
	if a.PutsFinalised != 1 || a.Entries != 1 || a.StoredBytes != 3 || a.RestoreHits != 0 || a.RestoreMisses != 0 {
		t.Errorf("pool_a = %+v", a)
	}
	if b.PutsStarted != 2 || b.PutsFinalised != 2 || b.Entries != 2 || b.StoredBytes != 12 || b.RestoreHits != 1 || b.RestoreMisses != 1 {
		t.Errorf("pool_b = %+v, want two saves, a hit and a miss, summed across its two repositories", b)
	}
}

// A service that stops and is started again must not hand the pool's counters
// back at zero: downstream, a counter that falls means the agent restarted, and
// the controller would then treat everything before it as lost.
func TestCacheStatsSurviveAServiceThatStoppedAndWasStartedAgain(t *testing.T) {
	c, _ := newServices(t, t.TempDir(), loopback)
	ctx := context.Background()
	spec := serviceSpec("pool_1")
	url := c.urlFor(ctx, spec, "")
	saveEntry(t, url, "k", []byte("12345"))
	restoreFinds(t, url, "k")

	c.mu.Lock()
	for key, r := range c.running {
		delete(c.running, key)
		c.retireLocked(r)
		_ = r.srv.Close()
	}
	c.mu.Unlock()

	again := c.urlFor(ctx, spec, "")
	if again == "" || again == url {
		t.Fatalf("the service was not started again (%q)", again)
	}
	restoreFinds(t, again, "k")

	got := c.stats()
	if len(got) != 1 {
		t.Fatalf("stats = %+v", got)
	}
	s := got[0].Stats
	if s.PutsFinalised != 1 || s.RestoreHits != 2 {
		t.Errorf("counters = %+v, want the first service's save and hit kept and the second's hit added", s)
	}
	// The gauges are the folder as it is now, which the new service read back.
	if s.Entries != 1 || s.StoredBytes != 5 {
		t.Errorf("gauges = %d entries, %d bytes, want 1 and 5", s.Entries, s.StoredBytes)
	}
}

// The same folder is served on a second address when runners are attached to a
// second network. Adding the two services' gauges would say the pool holds
// twice what is on its disk.
func TestCacheStatsCountAFolderOnceWhenTwoServicesShareIt(t *testing.T) {
	shared := t.TempDir()
	c, _ := newServices(t, shared, loopback)
	spec := serviceSpec("pool_1")
	url := c.urlFor(context.Background(), spec, "")
	saveEntry(t, url, "k", []byte("12345"))

	identity, err := cacheIdentity(spec)
	if err != nil {
		t.Fatal(err)
	}
	// A second service over the same folder, as a second network would start.
	second, err := cacheservice.New(cacheservice.Options{Dir: filepath.Join(shared, "cache", "service", identity)})
	if err != nil {
		t.Fatal(err)
	}
	c.mu.Lock()
	c.running["second"] = &runningCacheService{svc: second, poolID: spec.PoolID, identity: identity, srv: &http.Server{}}
	c.mu.Unlock()

	got := c.stats()
	if len(got) != 1 || got[0].Entries != 1 || got[0].StoredBytes != 5 {
		t.Errorf("stats = %+v, want the one entry of 5 bytes counted once", got)
	}
}

type statsBackend struct {
	Backend
	kind  store.BackendKind
	stats []CacheStats
}

func (b statsBackend) Kind() store.BackendKind  { return b.kind }
func (b statsBackend) CacheStats() []CacheStats { return b.stats }

type plainBackend struct {
	Backend
	kind store.BackendKind
}

func (b plainBackend) Kind() store.BackendKind { return b.kind }

// A host with no cache service reports nothing at all, so its heartbeat is the
// one an agent without this feature would have sent.
func TestARegistryReportsCacheStatsOnlyFromBackendsThatHaveThem(t *testing.T) {
	var none *Registry
	if got := none.CacheStats(); got != nil {
		t.Errorf("a nil registry reported %+v", got)
	}
	if got := NewRegistry(plainBackend{kind: store.BackendProcess}).CacheStats(); got != nil {
		t.Errorf("a backend with no services reported %+v", got)
	}
	reg := NewRegistry(
		plainBackend{kind: store.BackendProcess},
		statsBackend{kind: store.BackendDocker, stats: []CacheStats{{PoolID: "pool_b"}, {PoolID: "pool_a"}}},
	)
	got := reg.CacheStats()
	if len(got) != 2 || got[0].PoolID != "pool_a" || got[1].PoolID != "pool_b" {
		t.Errorf("registry stats = %+v, want both pools in order", got)
	}
}
