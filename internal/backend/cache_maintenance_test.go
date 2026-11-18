package backend

import (
	"context"
	"io/fs"
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

func TestSwitchingBackendsKeepsTheOtherDaemonsActiveCacheSafe(t *testing.T) {
	for _, tc := range []struct {
		name, state string
		fail, busy  bool
	}{{"runner", "running", false, false}, {"starting runner", "created", false, false}, {"tool fill", "", false, true}, {"unknown old daemon", "", true, false}, {"finished old job", "exited", false, false}} {
		t.Run(tc.name, func(t *testing.T) {
			old, spec, packages, tools := maintenanceFixture(t, tc.state, tc.fail)
			next, _, _, _ := maintenanceFixture(t, "", false)
			next.fl = podmanFlavor()
			next.sharedDir = old.sharedDir
			next.cacheSvcs.sharedDir = old.sharedDir
			reg := NewRegistry(old, &PodmanBackend{DockerBackend: next})
			writeCacheBytes(t, packages, "package", 200, time.Hour)
			writeCacheBytes(t, tools, "image/version", 200, time.Hour)
			reg.SetCachePolicies([]CachePolicy{{PoolID: spec.PoolID, Backend: store.BackendPodman, Cache: spec.Cache}})
			if tc.busy {
				end := old.beginCacheUse()
				defer end()
			}
			reg.MaintainCaches(context.Background())
			st := storageOf(t, next)
			if tc.name == "finished old job" {
				if st.TotalBytes != 0 || st.Deferred != "" {
					t.Fatalf("idle cache not reclaimed: %+v", st)
				}
			} else {
				if st.PackageBytes != 200 || st.ToolsBytes != 200 || st.Deferred == "" {
					t.Fatalf("active cache not protected across daemons: %+v", st)
				}
				if _, err := os.Stat(filepath.Join(tools, "image/version")); err != nil {
					t.Fatal("old daemon lost its live tool cache", err)
				}
				next.pruneCacheFor(context.Background(), spec)
				if _, err := os.Stat(filepath.Join(packages, "package")); err != nil {
					t.Fatal("create-time prune ignored the other daemon", err)
				}
			}
		})
	}
}

func TestSwitchingBackendsReusesTheServiceInsteadOfReopeningItsLiveIndex(t *testing.T) {
	notInAContainer(t)
	old, spec, _, _ := maintenanceFixture(t, "running", false)
	next, _, _, _ := maintenanceFixture(t, "", false)
	next.fl = podmanFlavor()
	next.sharedDir, next.cacheSvcs.sharedDir = old.sharedDir, old.sharedDir
	old.cacheSvcs.gateway, next.cacheSvcs.gateway = loopback, loopback
	reg := NewRegistry(old, &PodmanBackend{DockerBackend: next})
	spec.Cache.SizeLimit = 2000
	reg.SetCachePolicies([]CachePolicy{{PoolID: spec.PoolID, Backend: store.BackendPodman, Cache: spec.Cache}})
	original := old.cacheServiceURL(context.Background(), spec, "")
	saveEntry(t, original, "kept", []byte("archive"))
	migrated := next.cacheServiceURL(context.Background(), spec, "")
	if migrated != original || !restoreFinds(t, migrated, "kept") {
		t.Fatal("backend switch reopened a service over the same folder")
	}
	var puts uint64
	for _, row := range reg.CacheStats() {
		puts += row.PutsFinalised
	}
	if puts != 1 {
		t.Fatalf("service counters counted %d saves after a backend switch, want one", puts)
	}
}

// A data directory kept on another disk is reached through a link, and so is
// everything under /var on macOS. Refusing those left every cache on such a
// host growing past its limit; the link is the operator's own, so the pool's
// directory beyond it is still the pool's.
func TestCacheRootsFollowALinkAboveThePoolsOwnDirectory(t *testing.T) {
	target := t.TempDir()
	child := filepath.Join(target, "child")
	writeCacheBytes(t, child, "old", 200, time.Hour)
	link := filepath.Join(t.TempDir(), "parent")
	if err := os.Symlink(target, link); err != nil {
		t.Skip(err)
	}
	root, err := openCacheRoot(filepath.Join(link, "child"))
	if err != nil {
		t.Fatal("refused a cache below the operator's own link:", err)
	}
	root.Close()
	pruneCache(filepath.Join(link, "child"), 1, quietLogger())
	if _, err := os.Stat(filepath.Join(child, "old")); !os.IsNotExist(err) {
		t.Fatal("the size limit was not enforced through a linked parent:", err)
	}
}

// The refusal that remains is one an operator can act on, so it has to say
// what to change rather than that something could not be read.
func TestALinkedCacheDirectorySaysWhatToChange(t *testing.T) {
	b, _, packages, _ := maintenanceFixture(t, "", false)
	if err := os.Remove(packages); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(t.TempDir(), packages); err != nil {
		t.Skip(err)
	}
	b.MaintainCaches(context.Background())
	if st := storageOf(t, b); !strings.Contains(st.Deferred, "symbolic link") {
		t.Fatalf("the reason does not name the link: %q", st.Deferred)
	}
}

type vanishedEntry struct{ fs.DirEntry }

func (vanishedEntry) Info() (fs.FileInfo, error) { return nil, fs.ErrNotExist }
func (vanishedEntry) IsDir() bool                { return false }

// A job deletes its temporary files while maintenance walks the cache. That
// used to fail the whole measurement, so a busy pool read "size unknown" and
// its limit was skipped.
func TestAFileThatVanishesDuringMeasurementWeighsNothing(t *testing.T) {
	e := cacheDiskEntry{size: 7}
	if err := e.weigh(vanishedEntry{}, nil); err != nil || e.size != 7 {
		t.Fatalf("a file gone before it was read failed the walk: %v, size %d", err, e.size)
	}
	if err := e.weigh(nil, fs.ErrNotExist); err != nil {
		t.Fatal("a folder gone before it was listed failed the walk:", err)
	}
	if err := e.weigh(nil, fs.ErrPermission); err == nil {
		t.Fatal("an unreadable folder was measured as empty")
	}
}

// A runner start waits on the gate maintenance takes. Walking a package cache
// of millions of files with it held stopped every start on the host for the
// length of the walk, so the measuring half has to finish while somebody else
// holds it.
func TestMeasuringACacheDoesNotWaitOnTheGateARunnerStartHolds(t *testing.T) {
	b, spec, packages, _ := maintenanceFixture(t, "", false)
	writeCacheBytes(t, packages, "package", 200, time.Hour)
	b.cacheGate.Lock()
	defer b.cacheGate.Unlock()
	done := make(chan *cachePass, 1)
	go func() {
		done <- b.measureCache(context.Background(), CachePolicy{PoolID: spec.PoolID, Backend: store.BackendDocker, Cache: spec.Cache})
	}()
	select {
	case pass := <-done:
		for _, root := range pass.roots {
			root.Close()
		}
		if pass.st.PackageBytes != 200 {
			t.Fatalf("measured %+v, want the 200 bytes on disk", pass.st)
		}
		if _, err := os.Stat(filepath.Join(packages, "package")); err != nil {
			t.Fatal("measuring deleted from the cache:", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("measuring waited for the gate a runner start was holding")
	}
}

// A link above a cache directory is followed, which makes two spellings of one
// folder possible. A package cache whose source reaches the service's folder
// through a link is that folder, and evicting its "entries" would delete the
// service's blobs and index behind the index that manages them.
func TestAPackageCacheThatReachesServiceStorageThroughALinkIsNotEvicted(t *testing.T) {
	b, spec, _, _ := maintenanceFixture(t, "", false)
	real := t.TempDir()
	link := filepath.Join(t.TempDir(), "link")
	if err := os.Symlink(real, link); err != nil {
		t.Skip(err)
	}
	spec.Cache.Source = filepath.Join(link, "src")
	packages, ok := cacheDirectory(spec)
	if !ok {
		t.Fatal("the fixture's package cache is not a directory")
	}
	rel, err := filepath.Rel(link, packages)
	if err != nil {
		t.Fatal(err)
	}
	// The shared folder sits inside the package cache as the disk has it, and
	// nowhere near it as the two are written.
	b.sharedDir = filepath.Join(real, rel, "shared")
	b.cacheSvcs.sharedDir = b.sharedDir
	identity, _ := cacheIdentity(spec)
	service := filepath.Join(b.sharedDir, "cache", "service", identity)
	writeCacheBytes(t, service, "blobs/one", 200, time.Hour)
	if pathsOverlap(packages, service) {
		t.Fatal("the two overlap as written, so this would prove nothing")
	}
	b.SetCachePolicies([]CachePolicy{{PoolID: spec.PoolID, Backend: store.BackendDocker, Cache: spec.Cache}})
	b.MaintainCaches(context.Background())
	if _, err := os.Stat(filepath.Join(service, "blobs/one")); err != nil {
		t.Fatal("the service's own storage was evicted as package entries:", err)
	}
	if st := storageOf(t, b); !strings.Contains(st.Deferred, "overlaps") {
		t.Fatalf("the reason does not name the overlap: %+v", st)
	}
	b.pruneCacheFor(context.Background(), spec)
	if _, err := os.Stat(filepath.Join(service, "blobs/one")); err != nil {
		t.Fatal("the start-time prune evicted the service's storage:", err)
	}
}

// Measuring holds no gate, so a heartbeat can land in the middle of it. One
// that pauses maintenance or raises a limit has to win: evicting on the policy
// the pass started with deletes after it was told to stop.
func TestAPolicyChangedWhileMeasuringEvictsNothing(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change func(*DockerBackend, CachePolicy)
	}{
		{"withdrawn", func(b *DockerBackend, _ CachePolicy) { b.SetCachePolicies(nil) }},
		{"limit raised", func(b *DockerBackend, p CachePolicy) {
			p.Cache.SizeLimit = 1000
			b.SetCachePolicies([]CachePolicy{p})
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			b, spec, packages, _ := maintenanceFixture(t, "", false)
			writeCacheBytes(t, packages, "package", 200, time.Hour)
			policy := CachePolicy{PoolID: spec.PoolID, Backend: store.BackendDocker, Cache: spec.Cache}
			pass := b.measureCache(context.Background(), policy)
			defer func() {
				for _, root := range pass.roots {
					root.Close()
				}
			}()
			tc.change(b, policy)
			b.evictCaches(context.Background(), []*cachePass{pass})
			if _, err := os.Stat(filepath.Join(packages, "package")); err != nil {
				t.Fatal("evicted on a policy that no longer stands:", err)
			}
			if !pass.stale {
				t.Fatal("the pass was not marked as overtaken")
			}
		})
	}
}
