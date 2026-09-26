package backend

import (
	"context"
	"github.com/eyupio/zoomies/internal/store"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRepositoryCacheNamesPreserveDistinctIdentities(t *testing.T) {
	seen := map[string]string{}
	for _, repo := range []string{"acme/a.b", "acme/a-b", "acme/" + strings.Repeat("a", 100) + "x", "acme/" + strings.Repeat("a", 100) + "y"} {
		s := Spec{PoolID: "pool_one", Repository: repo, Cache: store.CacheConfig{Enabled: true, Scope: store.CacheScopeRepository}}
		name, err := cacheSource(s)
		if err != nil {
			t.Fatal(err)
		}
		if old := seen[name]; old != "" {
			t.Fatalf("%s aliases %s", repo, old)
		}
		seen[name] = repo
	}
}
func TestToolCacheGenerationsFollowTheResolvedImage(t *testing.T) {
	s := Spec{PoolID: "pool_one", Image: "sha256:first", Cache: store.CacheConfig{Enabled: true, Tools: true, Scope: store.CacheScopePool}}
	a, _ := toolCacheDir(s, t.TempDir())
	s.Image = "sha256:second"
	b, _ := toolCacheDir(s, filepath.Dir(filepath.Dir(filepath.Dir(filepath.Dir(a)))))
	if filepath.Base(a) == filepath.Base(b) {
		t.Fatal("different images reuse a tool generation")
	}
}
func TestCancelledCacheMaintenanceDoesNotEvict(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "blob")
	if err := os.WriteFile(p, []byte("oversized"), 0600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	pruneCacheContext(ctx, dir, 1, quietLogger())
	if _, err := os.Stat(p); err != nil {
		t.Fatal("cancelled prune removed cache")
	}
}
