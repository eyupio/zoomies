package backend

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"
)

// These tests pin the agent to what docs/security.md ("What the agent owns on
// a host") promises a team before they run the enrolment command on their own
// machine. A prune that widens -- to images, to volumes, to a container
// somebody else started -- is a promise broken on a machine that is not ours,
// so each failure names the promise rather than the endpoint.

const agentOwnsPromise = `docs/security.md, "What the agent owns on a host"`

// The one daemon-wide thing the agent does is ask Docker to prune unused
// builder cache down to a target. Decision 25 keeps exactly that and nothing
// broader: no filter that reaches further, no second endpoint, and nothing at
// all when the target is zero, which is the shared-daemon advice.
func TestTheBuilderCachePruneAsksForNothingButUnusedCacheAboveTheTarget(t *testing.T) {
	f := newFakeEngine(t, map[string]http.HandlerFunc{
		"POST " + v + "/build/prune": func(w http.ResponseWriter, r *http.Request) {
			writeJSON(w, http.StatusOK, map[string]any{"SpaceReclaimed": 42})
		},
	})
	b := dockerBackendFor(t, f, DockerOptions{})

	if _, err := b.PruneBuildCache(context.Background(), 0); err != nil {
		t.Fatal(err)
	}
	if len(f.seen) != 0 {
		t.Fatalf("a builder-cache target of 0 sent %d requests; 0 is the shared-daemon advice and must prune nothing (%s)", len(f.seen), agentOwnsPromise)
	}

	freed, err := b.PruneBuildCache(context.Background(), 5<<30)
	if err != nil {
		t.Fatal(err)
	}
	if freed != 42 {
		t.Errorf("freed = %d, want what the daemon reported", freed)
	}
	if len(f.seen) != 1 {
		t.Fatalf("the prune sent %d requests, want exactly one to /build/prune (%s)", len(f.seen), agentOwnsPromise)
	}
	r := f.seen[0]
	if r.Method != http.MethodPost || r.URL.Path != v+"/build/prune" {
		t.Fatalf("the prune sent %s %s; the builder cache is the only thing the agent prunes daemon-wide (%s)", r.Method, r.URL.Path, agentOwnsPromise)
	}
	q := r.URL.Query()
	keys := slices.Sorted(func(yield func(string) bool) {
		for k := range q {
			if !yield(k) {
				return
			}
		}
	})
	if !slices.Equal(keys, []string{"all", "keep-storage"}) {
		t.Errorf("the prune sent parameters %v; anything beyond the target reaches further than decision 25 allows (%s)", keys, agentOwnsPromise)
	}
	if q.Get("keep-storage") != "5368709120" {
		t.Errorf("keep-storage = %q, want the configured target in bytes", q.Get("keep-storage"))
	}
}

// Podman has no builder cache for the agent to prune, and the promise says so.
func TestPodmanPrunesNoBuilderCache(t *testing.T) {
	f := newFakeEngine(t, nil)
	b, err := NewPodman(DockerOptions{Host: "tcp://" + f.Listener.Addr().String(), Logger: quietLogger()})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := b.PruneBuildCache(context.Background(), 5<<30); err != nil {
		t.Fatal(err)
	}
	if len(f.seen) != 0 {
		t.Fatalf("a Podman backend sent %d requests to prune builder cache (%s)", len(f.seen), agentOwnsPromise)
	}
}

// The label is the whole boundary of which containers the agent touches: it
// lists, and therefore reaps, only what it labelled.
func TestTheAgentListsOnlyContainersCarryingItsLabel(t *testing.T) {
	f := newFakeEngine(t, map[string]http.HandlerFunc{
		"GET " + v + "/containers/json": func(w http.ResponseWriter, r *http.Request) {
			writeJSON(w, http.StatusOK, []any{})
		},
	})
	if _, err := dockerBackendFor(t, f, DockerOptions{}).List(context.Background()); err != nil {
		t.Fatal(err)
	}
	r := f.request(http.MethodGet, v+"/containers/json")
	if r == nil {
		t.Fatal("List sent no container listing")
	}
	if !strings.Contains(r.URL.Query().Get("filters"), LabelManaged+"=true") {
		t.Fatalf("List filters = %q; without %s=true the agent would reap containers it did not start (%s)",
			r.URL.Query().Get("filters"), LabelManaged, agentOwnsPromise)
	}
}

// Nothing anywhere in the tree may ask a daemon to prune images, containers,
// volumes or networks, or delete an image or a volume, but for one volume
// delete: APIClient.VolumeRemove, whose only caller removes the externals
// volumes the agent labelled itself (see the test after this one). This reads
// the source rather than a fake daemon because the promise is about every code
// path, including one written next year that no fake here would think to
// exercise.
func TestNothingInTheTreePrunesImagesVolumesOrUnlabelledContainers(t *testing.T) {
	forbidden := []*regexp.Regexp{
		regexp.MustCompile(`"/(images|containers|volumes|networks)/prune"`),
		regexp.MustCompile(`MethodDelete,\s*"/(images|volumes)/`),
		regexp.MustCompile(`"DELETE",\s*"/(images|volumes)/`),
		regexp.MustCompile(`"(docker|podman)",\s*"(image|volume|system|container)",\s*"prune"`),
	}
	// The one exception, matched whole so a second delete beside it still fails.
	allowed := map[string]string{
		filepath.Join("internal", "backend", "dockerapi.go"): `return c.do(ctx, http.MethodDelete, "/volumes/"+name, nil, nil, nil)`,
	}
	root := filepath.Join("..", "..")
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			switch d.Name() {
			case "node_modules", ".git", "web", "site", "dist":
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		body, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(root, path)
		if ok := allowed[rel]; ok != "" && strings.Count(string(body), ok) == 1 {
			body = []byte(strings.Replace(string(body), ok, "", 1))
		}
		for _, re := range forbidden {
			if m := re.Find(body); m != nil {
				t.Errorf("%s: %s -- the agent prunes only unused builder cache, and removes only containers it labelled (%s)", path, m, agentOwnsPromise)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

// The volume delete the test above allows has one caller, removeExternals,
// which SweepExternals reaches only for volumes carrying the agent's externals
// label. A second call site would be a new kind of volume the agent deletes,
// which docs/security.md has to say first.
func TestVolumeRemoveIsCalledOnlyByTheExternalsSweep(t *testing.T) {
	root := filepath.Join("..", "..")
	var sites []string
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			switch d.Name() {
			case "node_modules", ".git", "web", "site", "dist":
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		body, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		for i := 0; i < strings.Count(string(body), ".VolumeRemove("); i++ {
			sites = append(sites, path)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(sites) != 1 || filepath.Base(sites[0]) != "docker.go" {
		t.Fatalf("VolumeRemove is called from %v; its one caller is removeExternals in docker.go (%s)", sites, agentOwnsPromise)
	}
	body, err := os.ReadFile(filepath.Join(root, "internal", "backend", "docker.go"))
	if err != nil {
		t.Fatal(err)
	}
	// A Windows checkout ends its lines with CRLF.
	src := strings.ReplaceAll(string(body), "\r\n", "\n")
	start := strings.Index(src, "func (b *DockerBackend) removeExternals(")
	if start < 0 {
		t.Fatalf("removeExternals is not in docker.go; this test has to follow it (%s)", agentOwnsPromise)
	}
	fn := src[start:]
	if end := strings.Index(fn, "\n}\n"); end >= 0 {
		fn = fn[:end]
	}
	if !strings.Contains(fn, ".VolumeRemove(") {
		t.Fatalf("VolumeRemove is called outside removeExternals (%s)", agentOwnsPromise)
	}
}

// The sweep removes only what the agent labelled, never the newest volume of
// an image reference, and leaves one in use or just handed to a runner.
func TestTheExternalsSweepRemovesOnlySupersededVolumesItLabelled(t *testing.T) {
	label := func(name, ref string) map[string]string {
		return map[string]string{LabelExternals: name, LabelExternalsImage: ref}
	}
	vols := []Volume{
		{Name: "zoomies-externals-old", CreatedAt: "2026-10-20T10:00:00Z", Labels: label("zoomies-externals-old", "runner:latest")},
		{Name: "zoomies-externals-busy", CreatedAt: "2026-10-21T10:00:00Z", Labels: label("zoomies-externals-busy", "runner:latest")},
		{Name: "zoomies-externals-handed", CreatedAt: "2026-10-21T11:00:00Z", Labels: label("zoomies-externals-handed", "runner:latest")},
		{Name: "zoomies-externals-new", CreatedAt: "2026-10-22T10:00:00Z", Labels: label("zoomies-externals-new", "runner:latest")},
		{Name: "zoomies-externals-other", CreatedAt: "2026-10-01T10:00:00Z", Labels: label("zoomies-externals-other", "other:1")},
		// A daemon that ignored the filter: someone else's volumes, one of
		// them dressed in a name that looks like the agent's.
		{Name: "postgres-data", CreatedAt: "2026-01-01T10:00:00Z"},
		{Name: "zoomies-externals-forged", CreatedAt: "2026-01-01T10:00:00Z", Labels: map[string]string{LabelExternalsImage: "runner:latest"}},
	}
	var deleted []string
	f := newFakeEngine(t, map[string]http.HandlerFunc{
		"GET " + v + "/volumes": func(w http.ResponseWriter, r *http.Request) {
			if !strings.Contains(r.URL.Query().Get("filters"), LabelExternals) {
				t.Errorf("volume list filters = %q, want the externals label", r.URL.Query().Get("filters"))
			}
			writeJSON(w, http.StatusOK, map[string]any{"Volumes": vols})
		},
		"DELETE " + v + "/volumes/{name}": func(w http.ResponseWriter, r *http.Request) {
			name := r.PathValue("name")
			if name == "zoomies-externals-busy" {
				writeJSON(w, http.StatusConflict, map[string]string{"message": "volume is in use"})
				return
			}
			deleted = append(deleted, name)
			w.WriteHeader(http.StatusNoContent)
		},
	})
	b := dockerBackendFor(t, f, DockerOptions{})
	b.externalsLock("zoomies-externals-handed")
	b.externalsHanded["zoomies-externals-handed"] = time.Now()

	removed, err := b.SweepExternals(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if removed != 1 || !slices.Equal(deleted, []string{"zoomies-externals-old"}) {
		t.Fatalf("sweep removed %d: %v; want only the superseded, unused volume it labelled (%s)", removed, deleted, agentOwnsPromise)
	}
}
