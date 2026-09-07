package backend

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/eyupio/zoomies/internal/store"
)

func TestCacheMountConstructionAndDisabledBehavior(t *testing.T) {
	s := Spec{PoolID: "pool_one", Cache: store.CacheConfig{Enabled: true, Scope: store.CacheScopePool}}
	cfg := buildRunnerConfig(s, dockerFlavor(), containerOptions{})
	want := "zoomies-cache-pool-one:" + RunnerCacheMount
	if len(cfg.HostConfig.Binds) != 1 || cfg.HostConfig.Binds[0] != want {
		t.Fatalf("binds = %v, want %q", cfg.HostConfig.Binds, want)
	}
	s.Cache.Enabled = false
	if got := buildRunnerConfig(s, dockerFlavor(), containerOptions{}).HostConfig.Binds; len(got) != 0 {
		t.Fatalf("disabled cache mounted: %v", got)
	}
}

func TestCacheScopesAreIsolated(t *testing.T) {
	pool := Spec{PoolID: "pool_one", Repository: "acme/one", Cache: store.CacheConfig{Enabled: true, Scope: store.CacheScopePool}}
	a, _ := cacheSource(pool)
	pool.Repository = "acme/two"
	b, _ := cacheSource(pool)
	if a != b {
		t.Fatalf("pool scope changed by repository: %q != %q", a, b)
	}
	pool.Cache.Scope = store.CacheScopeRepository
	a, _ = cacheSource(pool)
	pool.Repository = "acme/one"
	b, _ = cacheSource(pool)
	if a == b {
		t.Fatalf("repository caches unexpectedly shared: %q", a)
	}
}

func TestCacheRejectsUnsafeInput(t *testing.T) {
	for _, source := range []string{"../escape", "bad/name", "name:option"} {
		_, err := cacheSource(Spec{PoolID: "p", Cache: store.CacheConfig{Enabled: true, Scope: store.CacheScopePool, Source: source}})
		if err == nil || !strings.Contains(err.Error(), "unsafe") {
			t.Errorf("source %q: err = %v", source, err)
		}
	}
}

func TestPodmanCacheMountGetsSELinuxSuffix(t *testing.T) {
	s := Spec{PoolID: "p", Cache: store.CacheConfig{Enabled: true, Scope: store.CacheScopePool}}
	got := buildRunnerConfig(s, podmanFlavor(), containerOptions{}).HostConfig.Binds[0]
	if got != "zoomies-cache-p:"+RunnerCacheMount+":z" {
		t.Fatalf("bind = %q", got)
	}
}

// An organisation-wide installation gives the spec the organisation as its
// repository, which is not a repository at all. The pool names the repository
// its cache is for, and that is what has to reach the mount.
func TestRepositoryCacheUsesThePoolsConfiguredRepository(t *testing.T) {
	org := Spec{PoolID: "pool_one", Repository: "acme", Cache: store.CacheConfig{
		Enabled: true, Scope: store.CacheScopeRepository, Repository: "acme/widgets",
	}}
	got, err := cacheSource(org)
	if err != nil {
		t.Fatalf("cacheSource: %v", err)
	}
	if want := "zoomies-cache-pool-one-acme-widgets"; got != want {
		t.Fatalf("cache source = %q, want %q", got, want)
	}
	// A repository-targeted installation keeps supplying it, unchanged.
	repo := Spec{PoolID: "pool_one", Repository: "acme/widgets", Cache: store.CacheConfig{
		Enabled: true, Scope: store.CacheScopeRepository,
	}}
	same, err := cacheSource(repo)
	if err != nil {
		t.Fatalf("cacheSource: %v", err)
	}
	if same != got {
		t.Fatalf("the same repository got two caches: %q and %q", got, same)
	}
}

func TestRepositoryCacheRefusesAnOrganisationWithNoRepository(t *testing.T) {
	_, err := cacheSource(Spec{PoolID: "p", Repository: "acme", Cache: store.CacheConfig{
		Enabled: true, Scope: store.CacheScopeRepository,
	}})
	if err == nil || !strings.Contains(err.Error(), "owner/name") {
		t.Fatalf("err = %v, want a message naming what to set", err)
	}
}

// A cache the operator asked to cap has to actually be capped, or the number in
// the pool form is decoration and the host's disk is the thing that runs out.
func TestPruneCacheEvictsTheLeastRecentlyUsedEntriesFirst(t *testing.T) {
	dir := t.TempDir()
	write := func(name string, size int, age time.Duration) {
		sub := filepath.Join(dir, name)
		if err := os.MkdirAll(sub, 0o755); err != nil {
			t.Fatal(err)
		}
		file := filepath.Join(sub, "blob")
		if err := os.WriteFile(file, make([]byte, size), 0o644); err != nil {
			t.Fatal(err)
		}
		when := time.Now().Add(-age)
		for _, p := range []string{file, sub} {
			if err := os.Chtimes(p, when, when); err != nil {
				t.Fatal(err)
			}
		}
	}
	write("old", 4000, 3*time.Hour)
	write("middle", 4000, 2*time.Hour)
	write("fresh", 4000, time.Minute)

	pruneCache(dir, 9000, slog.New(slog.DiscardHandler))

	names := map[string]bool{}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		names[e.Name()] = true
	}
	if names["old"] {
		t.Fatal("the oldest cache entry survived a prune that had to free space")
	}
	if !names["middle"] || !names["fresh"] {
		t.Fatalf("prune removed more than it needed to: %v", names)
	}
}

func TestPruneCacheLeavesACacheUnderItsLimitAlone(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "blob"), make([]byte, 100), 0o644); err != nil {
		t.Fatal(err)
	}
	pruneCache(dir, 1000, slog.New(slog.DiscardHandler))
	pruneCache(dir, 0, slog.New(slog.DiscardHandler)) // zero is unlimited, not "empty it"
	if _, err := os.Stat(filepath.Join(dir, "blob")); err != nil {
		t.Fatalf("a cache within its limit was pruned: %v", err)
	}
}

// Only a host directory can be measured, so only a host directory is offered up
// for eviction; a named volume must not be mistaken for a relative path.
func TestCacheDirectoryOnlyRecognisesHostPaths(t *testing.T) {
	volume := Spec{PoolID: "p", Cache: store.CacheConfig{Enabled: true, Scope: store.CacheScopePool}}
	if _, ok := cacheDirectory(volume); ok {
		t.Fatal("a named volume was treated as a directory this agent can prune")
	}
	host := Spec{PoolID: "p", Cache: store.CacheConfig{
		Enabled: true, Scope: store.CacheScopePool, Source: "/var/lib/zoomies/cache",
	}}
	dir, ok := cacheDirectory(host)
	if !ok || dir != "/var/lib/zoomies/cache/p" {
		t.Fatalf("cacheDirectory = %q, %v", dir, ok)
	}
}

// cacheFixture writes one oversized cache entry and returns the directory and a
// spec whose cache is that directory with a limit it already exceeds.
func cacheFixture(t *testing.T) (string, Spec) {
	t.Helper()
	root := t.TempDir()
	spec := Spec{
		PoolID: "pool_one",
		Cache: store.CacheConfig{
			Enabled:   true,
			Scope:     store.CacheScopePool,
			Source:    root,
			SizeLimit: 1000,
		},
	}
	dir, ok := cacheDirectory(spec)
	if !ok {
		t.Fatal("the fixture's cache must be a host directory, or there is nothing to prune")
	}
	entry := filepath.Join(dir, "blob")
	if err := os.MkdirAll(entry, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(entry, "big"), make([]byte, 4000), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir, spec
}

// cacheUsers builds an engine that answers the "who is using this cache?"
// listing with containers in the given states, and checks the question asked.
func cacheUsers(t *testing.T, dir string, states ...string) *fakeEngine {
	t.Helper()
	return newFakeEngine(t, map[string]http.HandlerFunc{
		"GET " + v + "/containers/json": func(w http.ResponseWriter, r *http.Request) {
			var filters map[string][]string
			if err := json.Unmarshal([]byte(r.Form.Get("filters")), &filters); err != nil {
				t.Errorf("filters not JSON: %v", err)
			}
			// The daemon must be asked about this cache, not about everything:
			// a listing of the whole host would let another pool's runner keep
			// this cache from ever being pruned.
			if !slices.Contains(filters["label"], LabelCacheVolume+"="+dir) {
				t.Errorf("the listing must be scoped to this cache, got %v", filters)
			}
			out := make([]ContainerSummary, 0, len(states))
			for i, state := range states {
				out = append(out, ContainerSummary{
					ID: fmt.Sprint("c", i), State: state,
					Labels: map[string]string{LabelCacheVolume: dir},
				})
			}
			writeJSON(w, 200, out)
		},
	})
}

func cacheStillHasItsEntry(t *testing.T, dir string) bool {
	t.Helper()
	_, err := os.Stat(filepath.Join(dir, "blob"))
	return err == nil
}

// The eviction happens as a runner is created because that was taken to be the
// moment the cache is idle. On a pool that runs two runners it is not: the
// second runner's start would delete the files the first runner's job is part
// way through using, which is a build failure an operator cannot explain and
// cannot reproduce.
func TestACacheAnotherRunnerIsUsingIsNotPruned(t *testing.T) {
	dir, spec := cacheFixture(t)
	b := dockerBackendFor(t, cacheUsers(t, dir, "running"), DockerOptions{})

	b.pruneCacheFor(context.Background(), spec)

	if !cacheStillHasItsEntry(t, dir) {
		t.Fatal("evicted from a cache a running job is using")
	}
}

// A container that exists but has finished is not reading anything, and a pool
// that has just run a job would otherwise never prune again.
func TestACacheOnlyFinishedRunnersHeldIsPruned(t *testing.T) {
	dir, spec := cacheFixture(t)
	b := dockerBackendFor(t, cacheUsers(t, dir, "exited", "dead"), DockerOptions{})

	b.pruneCacheFor(context.Background(), spec)

	if cacheStillHasItsEntry(t, dir) {
		t.Fatal("a cache over its limit that nothing is using was left alone")
	}
}

// A container the daemon has created but not started is about to read the
// cache, so it counts. Guessing the other way costs a job for no gain.
func TestACacheAStartingRunnerHoldsIsNotPruned(t *testing.T) {
	dir, spec := cacheFixture(t)
	b := dockerBackendFor(t, cacheUsers(t, dir, "created"), DockerOptions{})

	b.pruneCacheFor(context.Background(), spec)

	if !cacheStillHasItsEntry(t, dir) {
		t.Fatal("evicted from a cache a starting runner is about to use")
	}
}

// A daemon that will not answer says nothing about whether the cache is idle.
// Carrying the excess for one more runner costs disk; guessing wrong costs a
// job, so the unprovable case keeps the files.
func TestACacheIsNotPrunedWhenTheDaemonWillNotSayWhoIsUsingIt(t *testing.T) {
	dir, spec := cacheFixture(t)
	f := newFakeEngine(t, map[string]http.HandlerFunc{
		"GET " + v + "/containers/json": func(w http.ResponseWriter, r *http.Request) {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"message": "daemon is unwell"})
		},
	})
	b := dockerBackendFor(t, f, DockerOptions{})

	b.pruneCacheFor(context.Background(), spec)

	if !cacheStillHasItsEntry(t, dir) {
		t.Fatal("evicted from a cache without being able to tell whether it was in use")
	}
}

// With no limit there is nothing to enforce, so the daemon is not asked at all.
func TestACacheWithNoLimitAsksTheDaemonNothing(t *testing.T) {
	dir, spec := cacheFixture(t)
	spec.Cache.SizeLimit = 0
	f := newFakeEngine(t, map[string]http.HandlerFunc{
		"GET " + v + "/containers/json": func(w http.ResponseWriter, r *http.Request) {
			t.Error("a cache with no size limit must not cost a listing")
			writeJSON(w, 200, []ContainerSummary{})
		},
	})
	b := dockerBackendFor(t, f, DockerOptions{})

	b.pruneCacheFor(context.Background(), spec)

	if !cacheStillHasItsEntry(t, dir) {
		t.Fatal("a cache with no limit was pruned")
	}
}
