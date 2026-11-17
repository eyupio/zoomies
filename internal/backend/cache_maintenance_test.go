package backend

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/eyupio/zoomies/internal/store"
)

func maintenanceFixture(t *testing.T, state string, fail bool) (*DockerBackend, Spec, string, string) {
	t.Helper()
	shared := t.TempDir()
	spec := serviceSpec("pool_maintenance")
	spec.Cache.Source = filepath.Join(shared, "packages")
	spec.Cache.Tools = true
	spec.Cache.SizeLimit = 150
	identity, _ := cacheIdentity(spec)
	packages, _ := cacheDirectory(spec)
	tools := filepath.Join(shared, "cache", "tools", identity)
	for _, dir := range []string{packages, tools} {
		if err := os.MkdirAll(dir, 0750); err != nil {
			t.Fatal(err)
		}
	}
	f := newFakeEngine(t, map[string]http.HandlerFunc{
		"GET " + v + "/containers/json": func(w http.ResponseWriter, r *http.Request) {
			if fail {
				w.WriteHeader(500)
				return
			}
			rows := []ContainerSummary{}
			if state != "" {
				rows = append(rows, ContainerSummary{ID: "runner", State: state, Labels: map[string]string{LabelPoolID: spec.PoolID}})
			}
			writeJSON(w, 200, rows)
		},
	})
	b := dockerBackendFor(t, f, DockerOptions{SharedDir: shared})
	b.SetCachePolicies([]CachePolicy{{PoolID: spec.PoolID, Backend: store.BackendDocker, Cache: spec.Cache}})
	return b, spec, packages, tools
}

func writeCacheBytes(t *testing.T, dir, name string, size int, age time.Duration) {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.MkdirAll(filepath.Dir(path), 0750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(strings.Repeat("x", size)), 0640); err != nil {
		t.Fatal(err)
	}
	at := time.Now().Add(-age)
	if err := os.Chtimes(path, at, at); err != nil {
		t.Fatal(err)
	}
}

func storageOf(t *testing.T, b *DockerBackend) CacheStorage {
	t.Helper()
	rows := b.CacheStats()
	if len(rows) != 1 || rows[0].Storage == nil {
		t.Fatalf("missing measurement: %+v", rows)
	}
	return *rows[0].Storage
}

func TestIdleMaintenanceBoundsPackagesAndAllToolGenerationsWithoutANewRunner(t *testing.T) {
	b, _, packages, tools := maintenanceFixture(t, "exited", false)
	writeCacheBytes(t, packages, "old", 100, 2*time.Hour)
	writeCacheBytes(t, tools, "image-old/version", 100, time.Hour)
	// Parent mtimes are part of the entry's recency too.
	old := time.Now().Add(-time.Hour)
	_ = os.Chtimes(filepath.Join(tools, "image-old"), old, old)
	b.MaintainCaches(context.Background())
	st := storageOf(t, b)
	if st.PackageBytes != 0 || st.ToolsBytes != 100 || st.TotalBytes != 100 || st.Deferred != "" || st.MeasuredAt.IsZero() {
		t.Fatalf("storage = %+v", st)
	}
}

func TestMaintenanceDefersForLiveContainersUnknownInventoryAndLocalStarts(t *testing.T) {
	for _, tc := range []struct {
		name, state string
		fail, busy  bool
	}{{"running", "running", false, false}, {"starting", "created", false, false}, {"unknown", "", true, false}, {"local fill", "", false, true}} {
		t.Run(tc.name, func(t *testing.T) {
			b, _, packages, _ := maintenanceFixture(t, tc.state, tc.fail)
			writeCacheBytes(t, packages, "keep", 200, time.Hour)
			if tc.busy {
				end := b.beginCacheUse()
				defer end()
			}
			b.MaintainCaches(context.Background())
			if st := storageOf(t, b); st.PackageBytes != 200 || st.Deferred == "" {
				t.Fatalf("storage = %+v", st)
			}
			if _, err := os.Stat(filepath.Join(packages, "keep")); err != nil {
				t.Fatal("active cache deleted", err)
			}
		})
	}
}

func TestMaintenanceSeesChangedLimitsAndFoldersAfterAnAgentRestart(t *testing.T) {
	b, spec, packages, _ := maintenanceFixture(t, "", false)
	writeCacheBytes(t, packages, "entry", 100, time.Hour)
	b.MaintainCaches(context.Background())
	if storageOf(t, b).PackageBytes != 100 {
		t.Fatal("entry below original limit was evicted")
	}
	spec.Cache.SizeLimit = 50
	b.SetCachePolicies([]CachePolicy{{PoolID: spec.PoolID, Backend: store.BackendDocker, Cache: spec.Cache}})
	b.MaintainCaches(context.Background())
	if st := storageOf(t, b); st.PackageBytes != 0 || st.SizeLimit != 50 {
		t.Fatalf("new limit not applied: %+v", st)
	}
}

func TestMaintenanceNeverFollowsASymlinkCacheRoot(t *testing.T) {
	b, _, packages, _ := maintenanceFixture(t, "", false)
	outside := t.TempDir()
	writeCacheBytes(t, outside, "precious", 200, time.Hour)
	if err := os.Remove(packages); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, packages); err != nil {
		t.Skip(err)
	}
	b.MaintainCaches(context.Background())
	if st := storageOf(t, b); st.Deferred == "" || !st.MeasuredAt.IsZero() {
		t.Fatalf("unsafe root counted as measured: %+v", st)
	}
	pruneCache(packages, 1, quietLogger())
	if _, err := os.Stat(filepath.Join(outside, "precious")); err != nil {
		t.Fatal("followed symlink outside cache", err)
	}
}

func TestLiveCacheServiceReceivesTheRoomLeftByPackageAndToolData(t *testing.T) {
	notInAContainer(t)
	b, spec, packages, tools := maintenanceFixture(t, "running", false)
	spec.Cache.SizeLimit = 2000
	b.SetCachePolicies([]CachePolicy{{PoolID: spec.PoolID, Backend: store.BackendDocker, Cache: spec.Cache}})
	b.cacheSvcs.gateway = loopback
	url := b.cacheSvcs.urlFor(context.Background(), spec, "")
	saveEntry(t, url, "archive", []byte(strings.Repeat("s", 1000)))
	writeCacheBytes(t, packages, "package", 900, time.Hour)
	writeCacheBytes(t, tools, "image/version", 900, time.Hour)
	b.MaintainCaches(context.Background())
	st := storageOf(t, b)
	if st.PackageBytes != 900 || st.ToolsBytes != 900 || st.ServiceBytes != 0 || st.TotalBytes != 1800 {
		t.Fatalf("shared limit not applied: %+v", st)
	}
	if restoreFinds(t, url, "archive") {
		t.Fatal("service kept its old standalone budget")
	}
	if b.cacheSvcs.urlFor(context.Background(), spec, "") != url {
		t.Fatal("listener changed")
	}
}

func TestAMissingPolicyDisablesDeletion(t *testing.T) {
	b, _, packages, _ := maintenanceFixture(t, "", false)
	writeCacheBytes(t, packages, "keep", 200, time.Hour)
	b.SetCachePolicies(nil)
	b.MaintainCaches(context.Background())
	if _, err := os.Stat(filepath.Join(packages, "keep")); err != nil {
		t.Fatal(err)
	}
}

func TestARedeliveredCreateCannotRestoreAnOldCacheServiceBudget(t *testing.T) {
	notInAContainer(t)
	b, spec, _, _ := maintenanceFixture(t, "running", false)
	b.cacheSvcs.gateway = loopback
	spec.Cache.SizeLimit = 2000
	url := b.cacheSvcs.urlFor(context.Background(), spec, "")
	// The controller policy is newer than this still-queued create spec.
	policy := spec.Cache
	policy.SizeLimit = 100
	b.SetCachePolicies([]CachePolicy{{PoolID: spec.PoolID, Backend: store.BackendDocker, Cache: policy}})
	b.MaintainCaches(context.Background())
	if b.cacheSvcs.urlFor(context.Background(), spec, "") != url {
		t.Fatal("listener changed")
	}
	out := twirpCall(t, url, "CreateCacheEntry", map[string]string{"key": "small", "version": "v"})
	if out["ok"] != true {
		t.Fatal("small save should still be allowed")
	}
	// Enforce through the public protocol, not an assertion on budget internals.
	req, _ := http.NewRequest(http.MethodPut, out["signed_upload_url"].(string), strings.NewReader(strings.Repeat("s", 200)))
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 400 {
		t.Fatal("old create enlarged the live budget")
	}
}

func TestOverlappingCacheSourcesNeverDeleteServiceOrToolFiles(t *testing.T) {
	b, spec, _, tools := maintenanceFixture(t, "", false)
	spec.Cache.Source = filepath.Join(b.sharedDir, "cache", "tools")
	writeCacheBytes(t, tools, "precious", 200, time.Hour)
	b.SetCachePolicies([]CachePolicy{{PoolID: spec.PoolID, Backend: store.BackendDocker, Cache: spec.Cache}})
	b.MaintainCaches(context.Background())
	b.pruneCacheFor(context.Background(), spec)
	if st := storageOf(t, b); !strings.Contains(st.Deferred, "overlaps") || !st.MeasuredAt.IsZero() {
		t.Fatalf("overlap not refused: %+v", st)
	}
	if _, err := os.Stat(filepath.Join(tools, "precious")); err != nil {
		t.Fatal("overlapping tree was pruned", err)
	}
}

func TestAnUnreadableCacheTreeIsNotAnEmptyMeasurement(t *testing.T) {
	b, _, packages, _ := maintenanceFixture(t, "", false)
	if err := os.Remove(packages); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(packages, []byte("not a directory"), 0640); err != nil {
		t.Fatal(err)
	}
	b.MaintainCaches(context.Background())
	if st := storageOf(t, b); !st.MeasuredAt.IsZero() || st.Deferred == "" {
		t.Fatalf("invalid cache counted as empty: %+v", st)
	}
}
